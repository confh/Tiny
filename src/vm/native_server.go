package vm

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	. "language.com/src/tinyerrors"
)

type VMPool struct {
	idle   chan *VM
	makeVM func() *VM

	mu     sync.Mutex
	cond   *sync.Cond
	active int

	minActive int
	maxActive int
	maxIdle   int
}

func NewVMPool(maxActive int, maxIdle int, makeVM func() *VM) *VMPool {
	if maxActive < 1 {
		maxActive = 1
	}
	if maxIdle < 1 {
		maxIdle = 1
	}
	if maxIdle > maxActive {
		maxIdle = maxActive
	}

	minActive := runtime.GOMAXPROCS(0) * 8
	if minActive < 16 {
		minActive = 16
	}
	if minActive > maxActive {
		minActive = maxActive
	}

	p := &VMPool{
		idle:      make(chan *VM, maxIdle),
		makeVM:    makeVM,
		minActive: minActive,
		maxActive: maxActive,
		maxIdle:   maxIdle,
	}

	p.cond = sync.NewCond(&p.mu)
	return p
}

func defaultTaskPoolLimits() (maxActive int, maxIdle int) {
	cores := runtime.GOMAXPROCS(0)

	// Safe default:
	// 4-core machine  -> 64 active
	// 8-core machine  -> 128 active
	// 16-core machine -> 256 active
	maxActive = cores * 16

	if maxActive < 32 {
		maxActive = 32
	}
	if maxActive > 256 {
		maxActive = 256
	}

	maxIdle = maxActive / 4
	if maxIdle < 16 {
		maxIdle = 16
	}

	return maxActive, maxIdle
}

func (p *VMPool) currentLimitLocked() int {
	limit := p.maxActive

	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	heapMB := mem.Alloc / 1024 / 1024
	sysMB := mem.Sys / 1024 / 1024

	// If Go memory is getting chunky, reduce task concurrency.
	// This prevents the Windows VirtualAlloc/terminal-death situation.
	if heapMB > 768 || sysMB > 1536 {
		limit = limit / 2
	}
	if heapMB > 1200 || sysMB > 2400 {
		limit = limit / 4
	}

	if limit < p.minActive {
		limit = p.minActive
	}
	if limit < 1 {
		limit = 1
	}

	return limit
}

func (p *VMPool) Get() *VM {
	p.mu.Lock()

	for p.active >= p.currentLimitLocked() {
		p.cond.Wait()
	}

	p.active++

	p.mu.Unlock()

	select {
	case vm := <-p.idle:
		return vm
	default:
		return p.makeVM()
	}
}

func (p *VMPool) Put(vm *VM) {
	if vm != nil {
		vm.ResetForRequest()

		select {
		case p.idle <- vm:
			// keep for reuse
		default:
			// idle pool full, drop it
			// Do not call vm.Close() if task VMs share parent runtime/modules.
		}
	}

	p.mu.Lock()
	if p.active > 0 {
		p.active--
	}
	p.mu.Unlock()

	p.cond.Signal()
}

var serverMethods map[string]NativeModuleFunc[*NativeServerValue]

func init() {
	serverMethods = map[string]NativeModuleFunc[*NativeServerValue]{
		"get":       serverGet,
		"post":      serverPost,
		"put":       serverPut,
		"patch":     serverPatch,
		"delete":    serverDelete,
		"onRequest": serverOnRequest,
		"static":    serverStatic,
		"stop":      serverStop,
		"start":     serverStart,
	}
}

func (vm *VM) callServerMethod(server *NativeServerValue, method string, args []TinyValue) {
	fn, ok := serverMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown server method: %s", method)
		return
	}
	fn(vm, server, args)
}

func serverOnRequest(vm *VM, server *NativeServerValue, args []TinyValue) {
	expectArgs(vm, "server.onRequest", args, 1)
	handler := args[0]
	switch handler.Value.(type) {
	case string, FunctionValue:
		server.GenericRoute = handler
	default:
		vm.runtimeError(ErrorType, "server.onRequest expects string or function as second argument")
		return
	}
	vm.push(NewNull())
}

func serverGet(vm *VM, server *NativeServerValue, args []TinyValue) {
	serverRoute(vm, server, http.MethodGet, "server.get", args)
}

func serverPost(vm *VM, server *NativeServerValue, args []TinyValue) {
	serverRoute(vm, server, http.MethodPost, "server.post", args)
}

func serverPut(vm *VM, server *NativeServerValue, args []TinyValue) {
	serverRoute(vm, server, http.MethodPut, "server.put", args)
}

func serverPatch(vm *VM, server *NativeServerValue, args []TinyValue) {
	serverRoute(vm, server, http.MethodPatch, "server.patch", args)
}

func serverDelete(vm *VM, server *NativeServerValue, args []TinyValue) {
	serverRoute(vm, server, http.MethodDelete, "server.delete", args)
}

func serverRoute(vm *VM, server *NativeServerValue, method string, name string, args []TinyValue) {
	expectArgs(vm, name, args, 2)
	routePath := argString(vm, name, args, 0)
	handler := args[1]
	switch handler.Value.(type) {
	case string, FunctionValue:
		ensureServerRoutes(server)
		server.Routes[method][routePath] = handler
		if method == http.MethodGet {
			server.GetRoutes[routePath] = handler
		}
		if method == http.MethodPost {
			server.PostRoutes[routePath] = handler
		}
	default:
		vm.runtimeError(ErrorType, "%s expects string or function as second argument", name)
		return
	}
	vm.push(NewNull())
}

func serverStatic(vm *VM, server *NativeServerValue, args []TinyValue) {
	expectArgs(vm, "server.static", args, 2)
	prefix := argString(vm, "server.static", args, 0)
	dir := argString(vm, "server.static", args, 1)
	if prefix == "" {
		prefix = "/"
	}
	if !strings.HasPrefix(prefix, "/") {
		prefix = "/" + prefix
	}
	if server.StaticRoutes == nil {
		server.StaticRoutes = map[string]string{}
	}
	server.StaticRoutes[prefix] = dir
	vm.push(NewNull())
}

func serverStop(vm *VM, server *NativeServerValue, args []TinyValue) {
	expectArgs(vm, "server.stop", args, 0)
	server.closed = true
	if server.httpServer != nil {
		_ = server.httpServer.Close()
	}
	vm.push(NewNative(true))
}

func serverStart(vm *VM, server *NativeServerValue, args []TinyValue) {
	if len(args) > 1 {
		vm.runtimeError(ErrorRuntime, "server.start expects 0 or 1 argument")
		return
	}

	async := false
	if len(args) == 1 {
		async = argBool(vm, "server.start", args, 0)
	}

	mux := http.NewServeMux()
	server.mux = mux
	ensureServerRoutes(server)

	if server.Workers == nil {
		server.Workers = NewVMPool(16, 8, func() *VM {
			return vm.CloneForTask()
		})
	}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if server.closed {
			return
		}

		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[http server handler panic] %v\n", r)
				http.Error(w, "Internal Server Error", http.StatusInternalServerError)
			}
		}()

		if serveStaticRoute(w, r, server.StaticRoutes) {
			return
		}

		handler, params, found := findRoute(server.Routes[r.Method], server.GenericRoute, r.URL.Path)
		if !found {
			http.NotFound(w, r)
			return
		}

		switch h := handler.Value.(type) {
		case string:
			writeServerResponse(w, r, NativeHttpResponseValue{
				Type:  HttpText,
				Value: NewNative(h),
			})
		case FunctionValue:
			bodyReader := r.Body
			if server.MaxBodySize > 0 {
				bodyReader = http.MaxBytesReader(w, r.Body, server.MaxBodySize)
			}

			bodyBytes, err := io.ReadAll(bodyReader)
			if err != nil {
				http.Error(w, "Request Entity Too Large", http.StatusRequestEntityTooLarge)
				return
			}

			reqObj := NewNative(requestObjectFromHTTP(r, string(bodyBytes), params))

			requestVM := server.Workers.Get()
			defer server.Workers.Put(requestVM)

			result := requestVM.callFunctionValue(h, []TinyValue{reqObj})

			httpResponseObject, ok := result.Value.(NativeHttpResponseValue)
			if !ok {
				writeServerResponse(w, r, NativeHttpResponseValue{
					Type:  HttpText,
					Value: result,
				})
				return
			}

			writeServerResponse(w, r, httpResponseObject)

		default:
			vm.runtimeError(ErrorType, "invalid route handler: %s", TypeName(handler))
		}
	})

	addr := server.Host + ":" + strconv.Itoa(server.Port)
	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	if server.ReadTimeoutMs > 0 {
		httpServer.ReadTimeout = time.Duration(server.ReadTimeoutMs) * time.Millisecond
	}
	if server.WriteTimeoutMs > 0 {
		httpServer.WriteTimeout = time.Duration(server.WriteTimeoutMs) * time.Millisecond
	}
	server.httpServer = httpServer

	if async {
		go func() {
			err := httpServer.ListenAndServe()
			if err != nil && err != http.ErrServerClosed {
				vm.runtimeError(ErrorRuntime, "server failed: %v", err)
			}
		}()
	} else {
		err := httpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			vm.runtimeError(ErrorRuntime, "server failed: %v", err)
		}
	}

	vm.push(NewNull())
}

func ensureServerRoutes(server *NativeServerValue) {
	if server.Routes == nil {
		server.Routes = map[string]map[string]TinyValue{}
	}
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		if server.Routes[method] == nil {
			server.Routes[method] = map[string]TinyValue{}
		}
	}
	if server.GetRoutes == nil {
		server.GetRoutes = map[string]TinyValue{}
	}
	if server.PostRoutes == nil {
		server.PostRoutes = map[string]TinyValue{}
	}
	for route, handler := range server.GetRoutes {
		server.Routes[http.MethodGet][route] = handler
	}
	for route, handler := range server.PostRoutes {
		server.Routes[http.MethodPost][route] = handler
	}
	if server.StaticRoutes == nil {
		server.StaticRoutes = map[string]string{}
	}
}

func serveStaticRoute(w http.ResponseWriter, r *http.Request, routes map[string]string) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	for prefix, dir := range routes {
		cleanPrefix := prefix
		if !strings.HasSuffix(cleanPrefix, "/") {
			cleanPrefix += "/"
		}
		trimmedPrefix := strings.TrimSuffix(cleanPrefix, "/")
		if r.URL.Path == trimmedPrefix || strings.HasPrefix(r.URL.Path, cleanPrefix) {
			trimmed := strings.TrimPrefix(r.URL.Path, trimmedPrefix)
			trimmed = strings.TrimPrefix(trimmed, "/")
			http.ServeFile(w, r, filepath.Join(dir, trimmed))
			return true
		}
	}
	return false
}

func requestObjectFromHTTP(r *http.Request, body string, params ObjectValue) ObjectValue {
	obj := ObjectValue{
		"path":          NewNative(r.URL.Path),
		"url":           NewNative(r.URL.String()),
		"method":        NewNative(r.Method),
		"body":          NewNative(body),
		"params":        NewNative(params),
		"contentLength": NewNative(r.ContentLength),
		"remoteAddr":    NewNative(r.RemoteAddr),
	}

	queryMap := make(ObjectValue)
	for key, values := range r.URL.Query() {
		if len(values) > 0 {
			queryMap[key] = NewNative(values[0])
		} else {
			queryMap[key] = NewNative("")
		}
	}
	obj["query"] = NewNative(queryMap)

	headers := make(ObjectValue)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = NewNative(v[0])
		} else {
			headers[k] = NewNative("")
		}
	}
	obj["headers"] = NewNative(headers)

	return obj
}

func matchRoute(pattern string, actualPath string) (bool, ObjectValue) {
	params := ObjectValue{}

	pattern = strings.Trim(pattern, "/")
	actualPath = strings.Trim(actualPath, "/")

	patternParts := []string{}
	actualParts := []string{}

	if pattern != "" {
		patternParts = strings.Split(pattern, "/")
	}

	if actualPath != "" {
		actualParts = strings.Split(actualPath, "/")
	}

	if len(patternParts) != len(actualParts) {
		return false, params
	}

	for i := 0; i < len(patternParts); i++ {
		patternPart := patternParts[i]
		actualPart := actualParts[i]

		if strings.HasPrefix(patternPart, ":") {
			paramName := strings.TrimPrefix(patternPart, ":")
			if paramName == "" {
				return false, params
			}

			params[paramName] = NewNative(actualPart)
			continue
		}

		if patternPart != actualPart {
			return false, params
		}
	}

	return true, params
}

func findRoute(routes map[string]TinyValue, genericRoute TinyValue, actualPath string) (TinyValue, ObjectValue, bool) {
	if handler, ok := routes[actualPath]; ok {
		return handler, ObjectValue{}, true
	}

	for pattern, handler := range routes {
		matched, params := matchRoute(pattern, actualPath)
		if matched {
			return handler, params, true
		}
	}

	if !isNullish(genericRoute) {
		return genericRoute, ObjectValue{}, true
	}

	return NewNull(), ObjectValue{}, false
}
