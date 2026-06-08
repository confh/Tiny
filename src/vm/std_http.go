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

func (v *NativeServerValue) TinyTypeName() string {
	return "http.Server"
}

var stdHttpMetadata = StdModuleInfo{
	Name: "http",
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

func (vm *VM) callStdHttp(method string, args []TinyValue) {
	fn, ok := stdHttpMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown http function: %s", method)
		return
	}
	fn(vm, args)
}

func stdHttpServer(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.server", args, 1)

	port := asInt(args[0])
	server := &NativeServerValue{
		Port:         port,
		GetRoutes:    map[string]TinyValue{},
		PostRoutes:   map[string]TinyValue{},
		GenericRoute: NewNull(),
	}
	vm.push(NewNative(server))
}

func stdHttpGet(vm *VM, args []TinyValue) {
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

func stdHttpPost(vm *VM, args []TinyValue) {
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

func stdHttpJsonResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.json", args, 1)

	jsonValue := argObject(vm, "http.json", args, 0)

	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpJson,
		Value: NewNative(jsonValue),
	}))
}

func stdHttpTextResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.text", args, 1)

	strValue := argString(vm, "http.text", args, 0)

	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpText,
		Value: NewNative(strValue),
	}))
}

func stdHttpDownloadFile(vm *VM, args []TinyValue) {
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
