package vm

import (
	"io"
	"net/http"
	"strconv"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callServerMethod(server *NativeServerValue, method string, args []Value) {
	switch method {
	case "getPrettyJSON":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0], vm)
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			LangError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.GetRoutes[path] = string(bytes)

		vm.push(UndefinedValue{})
	case "getJSON":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0], vm)
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			LangError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.GetRoutes[path] = string(bytes)

		vm.push(UndefinedValue{})
	case "get":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.get expects 2 arguments")
		}

		path := asString(args[0], vm)
		handler := args[1]

		switch handler.(type) {
		case string:
			server.GetRoutes[path] = handler
		case FunctionValue:
			server.GetRoutes[path] = handler
		default:
			LangError(ErrorType, "server.get expects string or function as second argument")
		}

		vm.push(UndefinedValue{})

	case "post":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.post expects 2 arguments")
		}

		path := asString(args[0], vm)
		handler := args[1]

		switch handler.(type) {
		case string:
			server.PostRoutes[path] = handler
		case FunctionValue:
			server.PostRoutes[path] = handler
		default:
			LangError(ErrorType, "server.post expects string or function as second argument")
		}

		vm.push(UndefinedValue{})

	case "stop":
		if len(args) != 0 {
			LangError(ErrorRuntime, "server.stop expects 0 arguments")
		}

		server.closed = true

		vm.push(true)

	case "start":
		if len(args) > 1 {
			LangError(ErrorRuntime, "server.start expects 0 or 1 argument")
		}

		async := false

		if len(args) == 1 {
			async = asBool(args[0], vm)
		}

		mux := http.NewServeMux()

		server.mux = mux

		for path, handler := range server.GetRoutes {
			if server.closed {
				return
			}

			routeHandler := handler

			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodGet {
					return
				}
				switch h := routeHandler.(type) {
				case string:
					writeServerResponse(w, h)

				case FunctionValue:
					bodyBytes, err := io.ReadAll(r.Body)
					if err != nil {
						LangError(ErrorRuntime, "failed to read request body: %v", err)
					}
					body := string(bodyBytes)

					reqObj := ObjectValue{
						"path":   r.URL.Path,
						"method": r.Method,
						"query": func() ObjectValue {
							queryMap := make(ObjectValue)
							for key, values := range r.URL.Query() {
								if len(values) > 0 {
									queryMap[key] = values[0]
								} else {
									queryMap[key] = ""
								}
							}
							return queryMap
						}(),

						"headers": func() ObjectValue {
							headers := make(ObjectValue)
							for k, v := range r.Header {
								if len(v) > 0 {
									headers[k] = v[0]
								} else {
									headers[k] = ""
								}
							}
							return headers
						}(),

						"body": body,
					}

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
					LangError(ErrorType, "invalid route handler: %s", typeName(routeHandler))
				}
			})
		}

		for path, handler := range server.PostRoutes {
			if server.closed {
				return
			}

			routeHandler := handler

			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost {
					return
				}
				switch h := routeHandler.(type) {
				case string:
					writeServerResponse(w, h)

				case FunctionValue:
					bodyBytes, err := io.ReadAll(r.Body)
					if err != nil {
						LangError(ErrorRuntime, "failed to read request body: %v", err)
					}
					body := string(bodyBytes)

					reqObj := ObjectValue{
						"path":   r.URL.Path,
						"method": r.Method,
						"query": func() map[string]string {
							queryMap := make(map[string]string)
							for key, values := range r.URL.Query() {
								if len(values) > 0 {
									queryMap[key] = values[0]
								} else {
									queryMap[key] = ""
								}
							}
							return queryMap
						}(),

						"headers": func() map[string]string {
							headers := make(map[string]string)
							for k, v := range r.Header {
								if len(v) > 0 {
									headers[k] = v[0]
								} else {
									headers[k] = ""
								}
							}
							return headers
						}(),

						"body": body,
					}

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
					vm.runtimeError(ErrorType, "invalid route handler: %s", typeName(routeHandler))
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

	default:
		vm.runtimeError(ErrorName, "unknown server method: %s", method)
	}
}
