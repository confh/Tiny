package vm

import (
	"io"
	"net/http"
	"strconv"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var serverMethods map[string]NativeModuleFunc[*NativeServerValue]

func init() {
	serverMethods = map[string]NativeModuleFunc[*NativeServerValue]{
		"getPrettyJSON": serverGetPrettyJSON,
		"getJSON":       serverGetJSON,
		"get":           serverGet,
		"post":          serverPost,
		"stop":          serverStop,
		"start":         serverStart,
	}
}

func (vm *VM) callServerMethod(server *NativeServerValue, method string, args []Value) {
	fn, ok := serverMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown server method: %s", method)
		return
	}
	fn(vm, server, args)
}

func serverGetPrettyJSON(vm *VM, server *NativeServerValue, args []Value) {
	expectArgs(vm, "server.getPrettyJSON", args, 2)
	path := argString(vm, "server.getPrettyJSON", args, 0)
	jsonValue := valueToJSONCompatible(args[1])
	bytes, err := json.MarshalIndent(jsonValue, "", "  ")
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		return
	}
	server.GetRoutes[path] = string(bytes)
	vm.push(UndefinedValue{})
}

func serverGetJSON(vm *VM, server *NativeServerValue, args []Value) {
	expectArgs(vm, "server.getJSON", args, 2)
	path := argString(vm, "server.getJSON", args, 0)
	jsonValue := valueToJSONCompatible(args[1])
	bytes, err := json.Marshal(jsonValue)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		return
	}
	server.GetRoutes[path] = string(bytes)
	vm.push(UndefinedValue{})
}

func serverGet(vm *VM, server *NativeServerValue, args []Value) {
	expectArgs(vm, "server.get", args, 2)
	path := argString(vm, "server.get", args, 0)
	handler := args[1]
	switch handler.(type) {
	case string:
		server.GetRoutes[path] = handler
	case FunctionValue:
		server.GetRoutes[path] = handler
	default:
		vm.runtimeError(ErrorType, "server.get expects string or function as second argument")
		return
	}
	vm.push(UndefinedValue{})
}

func serverPost(vm *VM, server *NativeServerValue, args []Value) {
	expectArgs(vm, "server.post", args, 2)
	path := argString(vm, "server.post", args, 0)
	handler := args[1]
	switch handler.(type) {
	case string:
		server.PostRoutes[path] = handler
	case FunctionValue:
		server.PostRoutes[path] = handler
	default:
		vm.runtimeError(ErrorType, "server.post expects string or function as second argument")
		return
	}
	vm.push(UndefinedValue{})
}

func serverStop(vm *VM, server *NativeServerValue, args []Value) {
	expectArgs(vm, "server.stop", args, 0)
	server.closed = true
	vm.push(true)
}

func serverStart(vm *VM, server *NativeServerValue, args []Value) {
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

	// Collect all unique paths for GET and POST
	pathMap := make(map[string]struct{})
	for path := range server.GetRoutes {
		pathMap[path] = struct{}{}
	}
	for path := range server.PostRoutes {
		pathMap[path] = struct{}{}
	}

	for path := range pathMap {
		getHandler, hasGet := server.GetRoutes[path]
		postHandler, hasPost := server.PostRoutes[path]

		mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
			if server.closed {
				return
			}

			var handler any
			switch r.Method {
			case http.MethodGet:
				if !hasGet {
					http.NotFound(w, r)
					return
				}
				handler = getHandler
			case http.MethodPost:
				if !hasPost {
					http.NotFound(w, r)
					return
				}
				handler = postHandler
			default:
				// Only support GET and POST
				http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
				return
			}

			switch h := handler.(type) {
			case string:
				writeServerResponse(w, h)

			case FunctionValue:
				bodyBytes, err := io.ReadAll(r.Body)
				if err != nil {
					vm.runtimeError(ErrorRuntime, "failed to read request body: %v", err)
					return
				}
				body := string(bodyBytes)

				var reqObj Value
				obj := ObjectValue{
					"path":   r.URL.Path,
					"method": r.Method,
					"body":   body,
				}
				// Always ObjectValue keys, compact logic.
				queryMap := make(ObjectValue)
				for key, values := range r.URL.Query() {
					if len(values) > 0 {
						queryMap[key] = values[0]
					} else {
						queryMap[key] = ""
					}
				}
				obj["query"] = queryMap

				headers := make(ObjectValue)
				for k, v := range r.Header {
					if len(v) > 0 {
						headers[k] = v[0]
					} else {
						headers[k] = ""
					}
				}
				obj["headers"] = headers

				reqObj = obj

				vm.mu.Lock()
				defer vm.mu.Unlock()

				var result Value
				if async {
					requestVM := vm.CloneForTask()
					result = requestVM.callFunctionValue(h, []Value{reqObj})
				} else {
					result = vm.callFunctionValue(h, []Value{reqObj})
				}

				writeServerResponse(w, valueToString(result))

			default:
				vm.runtimeError(ErrorType, "invalid route handler: %s", typeName(handler))
			}
		})
	}

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

	vm.push(UndefinedValue{})
}
