package vm

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	. "language.com/src/tinyerrors"
)

var serverMethods map[string]NativeModuleFunc[*NativeServerValue]

func init() {
	serverMethods = map[string]NativeModuleFunc[*NativeServerValue]{
		"get":       serverGet,
		"post":      serverPost,
		"onRequest": serverOnRequest,
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
	expectArgs(vm, "server.get", args, 2)
	path := argString(vm, "server.get", args, 0)
	handler := args[1]
	switch handler.Value.(type) {
	case string, FunctionValue:
		server.GetRoutes[path] = handler
	default:
		vm.runtimeError(ErrorType, "server.get expects string or function as second argument")
		return
	}
	vm.push(NewNull())
}

func serverPost(vm *VM, server *NativeServerValue, args []TinyValue) {
	expectArgs(vm, "server.post", args, 2)
	path := argString(vm, "server.post", args, 0)
	handler := args[1]
	switch handler.Value.(type) {
	case string, FunctionValue:
		server.PostRoutes[path] = handler
	default:
		vm.runtimeError(ErrorType, "server.post expects string or function as second argument")
		return
	}
	vm.push(NewNull())
}

func serverStop(vm *VM, server *NativeServerValue, args []TinyValue) {
	expectArgs(vm, "server.stop", args, 0)
	server.closed = true
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

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if server.closed {
			return
		}

		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[http server handler panic] %v\n", r)

				http.Error(w, "Internal Server Error", 500)
			}
		}()

		var handler TinyValue
		var params ObjectValue
		var found bool

		switch r.Method {
		case http.MethodGet:
			handler, params, found = findRoute(server.GetRoutes, server.GenericRoute, r.URL.Path)
		case http.MethodPost:
			handler, params, found = findRoute(server.PostRoutes, server.GenericRoute, r.URL.Path)
		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if !found {
			http.NotFound(w, r)
			return
		}

		switch h := handler.Value.(type) {
		case string:
			writeServerResponse(w, NewNative(h), HttpText)
		case FunctionValue:
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "failed to read request body: %v", err)
				return
			}
			body := string(bodyBytes)

			obj := ObjectValue{
				"path":   NewNative(r.URL.Path),
				"method": NewNative(r.Method),
				"body":   NewNative(body),
				"params": NewNative(params),
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

			reqObj := NewNative(obj)

			requestVM := vm.CloneForTask()
			result := requestVM.callFunctionValue(h, []TinyValue{reqObj})

			httpResponseObject, ok := result.Value.(NativeHttpResponseValue)
			if !ok {
				vm.runtimeError(ErrorRuntime, "expected httpResponse, got %s.", TypeName(result))
				return
			}

			writeServerResponse(w, httpResponseObject.Value, httpResponseObject.Type)

		default:
			vm.runtimeError(ErrorType, "invalid route handler: %s", TypeName(handler))
		}
	})

	addr := ":" + strconv.Itoa(server.Port)

	if async {
		go func() {
			err := http.ListenAndServe(addr, mux)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "server failed: %v", err)
			}
		}()
	} else {
		err := http.ListenAndServe(addr, mux)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "server failed: %v", err)
		}
	}

	vm.push(NewNull())
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
	// exact match
	if handler, ok := routes[actualPath]; ok {
		return handler, ObjectValue{}, true
	}

	// dynamic match
	for pattern, handler := range routes {
		matched, params := matchRoute(pattern, actualPath)
		if matched {
			return handler, params, true
		}
	}

	// // fallback to generic handler
	if !isNullish(genericRoute) {
		return genericRoute, ObjectValue{}, true
	}

	return NewNull(), ObjectValue{}, false
}
