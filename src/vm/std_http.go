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
				{Name: "extra", Type: "Object", Optional: false},
			},
			Returns:     "object",
			Description: "Sends an HTTP GET request to the given URL with optional headers.",
		},
		"post": {
			Name: "post",
			Args: []StdArg{
				{Name: "url", Type: "string", Optional: false},
				{Name: "data", Type: "Object", Optional: false},
				{Name: "extra", Type: "Object", Optional: true},
			},
			Returns:     "object",
			Description: "Sends an HTTP POST request to the given URL with a data object and optional headers.",
		},
		"json": {
			Name: "json",
			Args: []StdArg{
				{Name: "data", Type: "Object", Optional: false},
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

var stdHttpMethods = map[string]StdModuleFunc{
	"server":       stdHttpServer,
	"get":          stdHttpGet,
	"post":         stdHttpPost,
	"json":         stdHttpJsonResponse,
	"text":         stdHttpTextResponse,
	"downloadFile": stdHttpDownloadFile,
}

func init() {
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
	vm.push(server)
}

func stdHttpGet(vm *VM, args []Value) {
	expectArgs(vm, "http.get", args, 2)

	url := argString(vm, "http.get", args, 0)
	extra := argObject(vm, "http.get", args, 1)

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
	data := argObject(vm, "http.post", args, 1)

	var headers ObjectValue
	returnBytes := false
	if len(args) == 3 {
		options := asObject(args[2], vm)
		if h, hasHeaders := options["headers"]; hasHeaders {
			headers, _ = h.(ObjectValue)
		}

		if h, hasHeaders := options["bytes"]; hasHeaders {
			returnBytes, _ = h.(bool)
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
	}

	if returnBytes {
		result["body"] = bodyBytes
	} else {
		result["body"] = string(bodyBytes)
	}

	for k, v := range resp.Header {
		result["headers"].(ObjectValue)[k] = strings.Join(v, ",")
	}

	vm.push(result)
}

func stdHttpJsonResponse(vm *VM, args []Value) {
	expectArgs(vm, "http.json", args, 1)

	jsonValue := argObject(vm, "http.json", args, 0)

	vm.push(NativeHttpResponseValue{
		Type:  HttpJson,
		Value: jsonValue,
	})
}

func stdHttpTextResponse(vm *VM, args []Value) {
	expectArgs(vm, "http.text", args, 1)

	strValue := argString(vm, "http.text", args, 0)

	vm.push(NativeHttpResponseValue{
		Type:  HttpText,
		Value: strValue,
	})
}

func stdHttpDownloadFile(vm *VM, args []Value) {
	expectArgs(vm, "http.downloadFile", args, 2)

	path := argString(vm, "http.downloadFile", args, 0)
	url := argString(vm, "http.downloadFile", args, 1)

	out, err := os.Create(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while creating file to download: %s", err)
	}
	defer out.Close()

	resp, err := http.Get(url)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while downloading file: %s", err)
	}
	defer resp.Body.Close()

	_, err = io.Copy(out, resp.Body)

	vm.push(true)
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
