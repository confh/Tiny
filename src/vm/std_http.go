package vm

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	json "github.com/goccy/go-json"

	. "language.com/src/tinyerrors"
)

var stdHttpMethods = map[string]StdModuleFunc{
	"server": stdHttpServer,
	"get":    stdHttpGet,
	"post":   stdHttpPost,
}

func (vm *VM) callStdHttp(method string, args []Value) {
	fn, ok := stdHttpMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown http function: %s", method)
		return
	}
	fn(vm, args)
}

func stdHttpServer(vm *VM, args []Value) {
	expectArgs(vm, "http.server", args, 1)

	port := asInt(args[0])
	server := &NativeServerValue{
		Port:       port,
		GetRoutes:  map[string]Value{},
		PostRoutes: map[string]Value{},
	}
	vm.push(server)
}

func stdHttpGet(vm *VM, args []Value) {
	expectArgs(vm, "http.get", args, 2)

	url := argString(vm, "http.get", args, 0)
	extra := asObject(args[1], vm)

	var headers ObjectValue
	if h, hasHeaders := extra["headers"]; hasHeaders {
		headers, _ = h.(ObjectValue)
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get request creation failed: %s", err.Error())
	}
	for key, value := range headers {
		valStr, ok := value.(string)
		if ok {
			req.Header.Set(valueToString(key), valStr)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get failed: %s", err.Error())
	}

	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get read response failed: %s", err.Error())
	}

	result := ObjectValue{
		"status":  resp.StatusCode,
		"headers": ObjectValue{},
		"body":    string(bodyBytes),
	}

	for k, v := range resp.Header {
		result["headers"].(ObjectValue)[k] = strings.Join(v, ",")
	}

	vm.push(result)
}

func stdHttpPost(vm *VM, args []Value) {
	expectArgsRange(vm, "http.post", args, 2, 3)

	url := argString(vm, "http.post", args, 0)
	data := asObject(args[1], vm)

	var headers ObjectValue
	if len(args) == 3 {
		extra := asObject(args[2], vm)
		if h, hasHeaders := extra["headers"]; hasHeaders {
			headers, _ = h.(ObjectValue)
		}
	}

	cleanedData := cleanMapForJSON(data)
	jsonData, err := json.Marshal(cleanedData)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post failed to encode JSON data: %s", err.Error())
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post request creation failed: %s", err.Error())
	}

	req.Header.Set("Content-Type", "application/json")

	for key, value := range headers {
		valStr, ok := value.(string)
		if ok {
			req.Header.Set(valueToString(key), valStr)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post failed: %s", err.Error())
	}

	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post read response failed: %s", err.Error())
	}

	result := ObjectValue{
		"status":  resp.StatusCode,
		"headers": ObjectValue{},
		"body":    string(bodyBytes),
	}

	for k, v := range resp.Header {
		result["headers"].(ObjectValue)[k] = strings.Join(v, ",")
	}

	vm.push(result)
}

func cleanMapForJSON(vmMap map[Value]Value) map[string]any {
	clean := make(map[string]any, len(vmMap))

	for k, v := range vmMap {
		keyStr := valueToString(k)
		clean[keyStr] = cleanValueForJSON(v)
	}

	return clean
}

func cleanValueForJSON(val any) any {
	switch v := val.(type) {
	case ObjectValue:
		return cleanMapForJSON(v)
	case ArrayValue:
		cleanedSlice := make([]any, len(v.Elements))
		for i, item := range v.Elements {
			cleanedSlice[i] = cleanValueForJSON(item)
		}
		return cleanedSlice
	case *ArrayValue:
		cleanedSlice := make([]any, len(v.Elements))
		for i, item := range v.Elements {
			cleanedSlice[i] = cleanValueForJSON(item)
		}
		return cleanedSlice
	default:
		return v
	}
}
