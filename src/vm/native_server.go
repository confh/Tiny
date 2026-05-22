package vm

import (
	"io"
	"net/http"
	"strconv"
	"strings"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var serverNativeMetadata = NativeTypeInfo{
	Name: "server",
	Methods: map[string]StdMethodInfo{
		"getPrettyJSON": {
			Name:        "getPrettyJSON",
			Args:        []StdArg{{Name: "path", Type: "string"}, {Name: "value", Type: "any"}},
			Returns:     "void",
			Description: "Register a GET route that responds with pretty-printed JSON.",
		},
		"getJSON": {
			Name:        "getJSON",
			Args:        []StdArg{{Name: "path", Type: "string"}, {Name: "value", Type: "any"}},
			Returns:     "void",
			Description: "Register a GET route that responds with minified JSON.",
		},
		"get": {
			Name:        "get",
			Args:        []StdArg{{Name: "path", Type: "string"}, {Name: "handler", Type: "string|function"}},
			Returns:     "void",
			Description: "Register a GET route. Handler can be a string or a function.",
		},
		"post": {
			Name:        "post",
			Args:        []StdArg{{Name: "path", Type: "string"}, {Name: "handler", Type: "string|function"}},
			Returns:     "void",
			Description: "Register a POST route. Handler can be a string or a function.",
		},
		"stop": {
			Name:        "stop",
			Returns:     "bool",
			Description: "Stops the server.",
		},
		"start": {
			Name:        "start",
			Args:        []StdArg{{Name: "async", Type: "bool", Optional: true}},
			Returns:     "void",
			Description: "Starts the server. Pass 'true' to run asynchronously.",
		},
	},
}

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
	registerNativeType(serverNativeMetadata)
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
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if server.closed {
			return
		}

		var handler Value
		var params ObjectValue
		var found bool

		switch r.Method {
		case http.MethodGet:
			handler, params, found = findRoute(server.GetRoutes, r.URL.Path)

		case http.MethodPost:
			handler, params, found = findRoute(server.PostRoutes, r.URL.Path)

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		if !found {
			http.NotFound(w, r)
			return
		}

		switch h := handler.(type) {
		case string:
			writeServerResponse(w, h, HttpText)

		case FunctionValue:
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "failed to read request body: %v", err)
				return
			}

			body := string(bodyBytes)

			obj := ObjectValue{
				"path":   r.URL.Path,
				"method": r.Method,
				"body":   body,
				"params": params,
			}

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

			reqObj := Value(obj)

			vm.mu.Lock()
			defer vm.mu.Unlock()

			var result Value
			if async {
				requestVM := vm.CloneForTask()
				result = requestVM.callFunctionValue(h, []Value{reqObj})
			} else {
				result = vm.callFunctionValue(h, []Value{reqObj})
			}

			httpResponseObject, ok := result.(NativeHttpResponseValue)
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

	vm.push(UndefinedValue{})
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

			params[paramName] = actualPart
			continue
		}

		if patternPart != actualPart {
			return false, params
		}
	}

	return true, params
}

func findRoute(routes map[string]Value, actualPath string) (Value, ObjectValue, bool) {
	// 1. Exact match first.
	if handler, ok := routes[actualPath]; ok {
		return handler, ObjectValue{}, true
	}

	// 2. Dynamic match.
	for pattern, handler := range routes {
		matched, params := matchRoute(pattern, actualPath)
		if matched {
			return handler, params, true
		}
	}

	return nil, ObjectValue{}, false
}
