package vm

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"

	json "github.com/goccy/go-json"

	. "language.com/src/tinyerrors"
)

var stdHttpMetadata = StdModuleInfo{
	Name: "http",
	Methods: map[string]StdMethodInfo{
		"server": {
			Name: "server",
			Args: []StdArg{
				{Name: "port", Type: "int", Optional: false},
			},
			Returns:     "server",
			Description: "Creates a new HTTP server object on the given port.",
		},
		"get": {
			Name: "get",
			Args: []StdArg{
				{Name: "url", Type: "string", Optional: false},
				{Name: "options", Type: "object", Optional: true},
			},
			Returns:     "object",
			Description: "Sends an HTTP GET request to the given URL with optional headers.",
		},
		"post": {
			Name: "post",
			Args: []StdArg{
				{Name: "url", Type: "string", Optional: false},
				{Name: "data", Type: "object", Optional: false},
				{Name: "extra", Type: "object", Optional: true},
			},
			Returns:     "object",
			Description: "Sends an HTTP POST request to the given URL with a data object and optional headers.",
		},
		"json": {
			Name: "json",
			Args: []StdArg{
				{Name: "data", Type: "object", Optional: false},
			},
			Returns:     "httpResponse",
			Description: "Creates a JSON HTTP response value with the given data.",
		},
		"text": {
			Name: "text",
			Args: []StdArg{
				{Name: "data", Type: "string", Optional: false},
			},
			Returns:     "httpResponse",
			Description: "Creates a text HTTP response value with the given string.",
		},
		"downloadFile": {
			Name: "downloadFile",
			Args: []StdArg{
				{Name: "filePath", Type: "string", Optional: false},
				{Name: "url", Type: "string", Optional: false},
			},
			Returns:     "bool",
			Description: "Downloads a file from the given URL and saves it to the specified file path. Returns true on success, throws error on failure.",
		},
	},
}

var stdHttpMethods map[string]StdModuleFunc

func init() {
	stdHttpMethods = map[string]StdModuleFunc{
		"server":       stdHttpServer,
		"get":          stdHttpGet,
		"post":         stdHttpPost,
		"json":         stdHttpJsonResponse,
		"text":         stdHttpTextResponse,
		"downloadFile": stdHttpDownloadFile,
	}
	registerStdModule(stdHttpMetadata)
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
	vm.push(NewNative(server))
}

func stdHttpGet(vm *VM, args []Value) {
	expectArgsRange(vm, "http.get", args, 1, 2)

	url := argString(vm, "http.get", args, 0)

	var headers ObjectValue = ObjectValue{}

	if len(args) > 1 {
		extra := argObject(vm, "http.get", args, 1)

		if h, hasHeaders := extra["headers"]; hasHeaders {
			if val, ok := h.Value.(ObjectValue); ok {
				headers = val
			}
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get request creation failed: %s", err.Error())
		return
	}
	for key, value := range headers {
		strKey := valueToString(ToValue(key))
		var valStr string
		if s, ok := value.Value.(string); ok {
			valStr = s
		} else if value.IsInt {
			valStr = valueToString(value)
		} else {
			valStr = valueToString(value)
		}
		req.Header.Set(strKey, valStr)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get failed: %s", err.Error())
		return
	}

	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.get read response failed: %s", err.Error())
		return
	}

	headersObj := ObjectValue{}
	for k, v := range resp.Header {
		headersObj[k] = NewNative(strings.Join(v, ","))
	}

	result := ObjectValue{
		"status":  NewInt(resp.StatusCode),
		"headers": NewNative(headersObj),
		"body":    NewNative(string(bodyBytes)),
	}

	vm.push(NewNative(result))
}

func stdHttpPost(vm *VM, args []Value) {
	expectArgsRange(vm, "http.post", args, 2, 3)

	url := argString(vm, "http.post", args, 0)
	data := argObject(vm, "http.post", args, 1)

	var headers ObjectValue = ObjectValue{}
	returnBytes := false
	if len(args) == 3 {
		options := asObject(args[2], vm)
		if h, hasHeaders := options["headers"]; hasHeaders {
			if val, ok := h.Value.(ObjectValue); ok {
				headers = val
			}
		}

		if h, hasHeaders := options["bytes"]; hasHeaders {
			if val, ok := h.Value.(bool); ok {
				returnBytes = val
			}
		}
	}

	cleanedData := cleanMapForJSON(data)
	jsonData, err := json.Marshal(cleanedData)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post failed to encode JSON data: %s", err.Error())
		return
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post request creation failed: %s", err.Error())
		return
	}

	req.Header.Set("Content-Type", "application/json")

	for key, value := range headers {
		strKey := valueToString(ToValue(key))
		var valStr string
		if s, ok := value.Value.(string); ok {
			valStr = s
		} else if value.IsInt {
			valStr = valueToString(value)
		} else {
			valStr = valueToString(value)
		}
		req.Header.Set(strKey, valStr)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post failed: %s", err.Error())
		return
	}

	defer resp.Body.Close()
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "http.post read response failed: %s", err.Error())
		return
	}

	headersObj := ObjectValue{}
	for k, v := range resp.Header {
		headersObj[k] = NewNative(strings.Join(v, ","))
	}

	result := ObjectValue{
		"status":  NewInt(resp.StatusCode),
		"headers": NewNative(headersObj),
	}

	if returnBytes {
		result["body"] = NewNative(bodyBytes)
	} else {
		result["body"] = NewNative(string(bodyBytes))
	}

	vm.push(NewNative(result))
}

func stdHttpJsonResponse(vm *VM, args []Value) {
	expectArgs(vm, "http.json", args, 1)

	jsonValue := argObject(vm, "http.json", args, 0)

	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpJson,
		Value: NewNative(jsonValue),
	}))
}

func stdHttpTextResponse(vm *VM, args []Value) {
	expectArgs(vm, "http.text", args, 1)

	strValue := argString(vm, "http.text", args, 0)

	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpText,
		Value: NewNative(strValue),
	}))
}

func stdHttpDownloadFile(vm *VM, args []Value) {
	expectArgs(vm, "http.downloadFile", args, 2)

	path := argString(vm, "http.downloadFile", args, 0)
	url := argString(vm, "http.downloadFile", args, 1)

	out, err := os.Create(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while creating file to download: %s", err)
		return
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while downloading file: %s", err)
		return
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)

	vm.push(NewNative(true))
}

func cleanMapForJSON(vmMap ObjectValue) map[string]any {
	clean := make(map[string]any, len(vmMap))

	for k, v := range vmMap {
		keyStr := valueToString(ToValue(k))
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
