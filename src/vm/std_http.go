package vm

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "language.com/src/tinyerrors"
)

func (v *NativeServerValue) TinyTypeName() string {
	return "http.Server"
}

var stdHttpMetadata = StdModuleInfo{
	Name: "http",
}

var stdHttpMethods map[string]StdModuleFunc

var stdHTTPClient = &http.Client{
	Transport: &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		IdleConnTimeout:       90 * time.Second,
	},
}

func init() {
	stdHttpMethods = map[string]StdModuleFunc{
		"server":       stdHttpServer,
		"request":      stdHttpRequest,
		"get":          stdHttpGet,
		"post":         stdHttpPost,
		"put":          stdHttpPut,
		"patch":        stdHttpPatch,
		"delete":       stdHttpDelete,
		"json":         stdHttpJsonResponse,
		"text":         stdHttpTextResponse,
		"html":         stdHttpHtmlResponse,
		"status":       stdHttpStatusResponse,
		"response":     stdHttpResponse,
		"redirect":     stdHttpRedirect,
		"noContent":    stdHttpNoContent,
		"file":         stdHttpFile,
		"download":     stdHttpDownload,
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

	server := &NativeServerValue{
		Host:           "",
		ReadTimeoutMs:  0,
		WriteTimeoutMs: 0,
		MaxBodySize:    0,
		Routes:         map[string]map[string]TinyValue{},
		GetRoutes:      map[string]TinyValue{},
		PostRoutes:     map[string]TinyValue{},
		StaticRoutes:   map[string]string{},
		GenericRoute:   NewNull(),
	}

	if args[0].IsInt {
		server.Port = args[0].AsInt
	} else if config, ok := vm.valueAsObjectForRead(args[0]); ok {
		server.Port = objectInt(vm, config, "port", 0)
		server.Host = objectString(config, "host", "")
		server.ReadTimeoutMs = objectInt(vm, config, "readTimeoutMs", 0)
		server.WriteTimeoutMs = objectInt(vm, config, "writeTimeoutMs", 0)
		server.MaxBodySize = int64(objectInt(vm, config, "maxBodySize", 0))
	} else {
		vm.runtimeError(ErrorType, "http.server expects port number or options object")
		return
	}

	if server.Port == 0 {
		vm.runtimeError(ErrorRuntime, "http.server requires a non-zero port")
		return
	}

	ensureServerRoutes(server)
	vm.push(NewNative(server))
}

func stdHttpGet(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.get", args, 1, 2)
	url := argString(vm, "http.get", args, 0)
	options := optionalObjectArg(vm, args, 1)
	vm.push(doHTTPRequest(vm, "http.get", http.MethodGet, url, NewNull(), options))
}

func stdHttpPost(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.post", args, 2, 3)
	url := argString(vm, "http.post", args, 0)
	options := optionalObjectArg(vm, args, 2)
	vm.push(doHTTPRequest(vm, "http.post", http.MethodPost, url, args[1], options))
}

func stdHttpPut(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.put", args, 2, 3)
	url := argString(vm, "http.put", args, 0)
	options := optionalObjectArg(vm, args, 2)
	vm.push(doHTTPRequest(vm, "http.put", http.MethodPut, url, args[1], options))
}

func stdHttpPatch(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.patch", args, 2, 3)
	url := argString(vm, "http.patch", args, 0)
	options := optionalObjectArg(vm, args, 2)
	vm.push(doHTTPRequest(vm, "http.patch", http.MethodPatch, url, args[1], options))
}

func stdHttpDelete(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.delete", args, 1, 2)
	url := argString(vm, "http.delete", args, 0)
	options := optionalObjectArg(vm, args, 1)
	vm.push(doHTTPRequest(vm, "http.delete", http.MethodDelete, url, NewNull(), options))
}

func stdHttpRequest(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.request", args, 1)
	config := argObject(vm, "http.request", args, 0)

	method := strings.ToUpper(objectString(config, "method", http.MethodGet))
	url := objectString(config, "url", "")
	if url == "" {
		vm.runtimeError(ErrorRuntime, "http.request requires url")
		return
	}

	body := NewNull()
	if value, ok := config["body"]; ok {
		body = value
	}

	vm.push(doHTTPRequest(vm, "http.request", method, url, body, config))
}

func requestBodyBytes(vm *VM, body TinyValue) ([]byte, string, error) {
	if isNullish(body) {
		return nil, "", nil
	}

	if body.IsInt {
		return []byte(valueToString(body)), "text/plain; charset=utf-8", nil
	}

	switch v := body.Value.(type) {
	case string:
		return []byte(v), "text/plain; charset=utf-8", nil

	case []byte:
		return v, "application/octet-stream", nil

	case *BufferValue:
		return v.Bytes, "application/octet-stream", nil

	case WasmObjectValue:
		obj, _ := vm.valueAsObjectForRead(body)
		if isMultipartRequestObject(obj) {
			return multipartRequestBodyBytes(vm, obj)
		}
		cleaned := cleanValueForJSON(NewNative(obj))

		jsonData, err := json.Marshal(cleaned)
		if err != nil {
			return nil, "", err
		}

		return jsonData, "application/json", nil

	case ObjectValue:
		if isMultipartRequestObject(v) {
			return multipartRequestBodyBytes(vm, v)
		}
		cleaned := cleanValueForJSON(body)

		jsonData, err := json.Marshal(cleaned)
		if err != nil {
			return nil, "", err
		}

		return jsonData, "application/json", nil

	case ArrayValue, *ArrayValue:
		cleaned := cleanValueForJSON(body)

		jsonData, err := json.Marshal(cleaned)
		if err != nil {
			return nil, "", err
		}

		return jsonData, "application/json", nil

	default:
		return []byte(valueToString(body)), "text/plain; charset=utf-8", nil
	}
}

func isMultipartRequestObject(obj ObjectValue) bool {
	if multipartValue, ok := obj["multipart"]; ok {
		if b, ok := multipartValue.Value.(bool); ok && b {
			return true
		}
	}
	_, hasForm := obj["form"]
	_, hasFiles := obj["files"]
	return hasForm || hasFiles
}

func multipartRequestBodyBytes(vm *VM, obj ObjectValue) ([]byte, string, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	if formValue, ok := obj["form"]; ok && !isNullish(formValue) {
		form, ok := vm.valueAsObjectForRead(formValue)
		if !ok {
			return nil, "", fmt.Errorf("multipart form must be an object")
		}
		for key, value := range form {
			field := valueToString(ToValue(key))
			if arr, ok := value.Value.(*ArrayValue); ok {
				for _, elem := range arr.Elements {
					if err := writer.WriteField(field, valueToString(elem)); err != nil {
						return nil, "", err
					}
				}
				continue
			}
			if err := writer.WriteField(field, valueToString(value)); err != nil {
				return nil, "", err
			}
		}
	}

	if filesValue, ok := obj["files"]; ok && !isNullish(filesValue) {
		if err := writeMultipartFiles(vm, writer, filesValue); err != nil {
			return nil, "", err
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", err
	}
	return body.Bytes(), writer.FormDataContentType(), nil
}

func writeMultipartFiles(vm *VM, writer *multipart.Writer, filesValue TinyValue) error {
	switch files := filesValue.Value.(type) {
	case *ArrayValue:
		for _, fileValue := range files.Elements {
			if err := writeMultipartFile(vm, writer, "", fileValue); err != nil {
				return err
			}
		}
		return nil
	case ArrayValue:
		for _, fileValue := range files.Elements {
			if err := writeMultipartFile(vm, writer, "", fileValue); err != nil {
				return err
			}
		}
		return nil
	}

	filesObject, ok := vm.valueAsObjectForRead(filesValue)
	if !ok {
		return fmt.Errorf("multipart files must be an array or object")
	}
	for fieldKey, fieldValue := range filesObject {
		field := valueToString(ToValue(fieldKey))
		if arr, ok := fieldValue.Value.(*ArrayValue); ok {
			for _, fileValue := range arr.Elements {
				if err := writeMultipartFile(vm, writer, field, fileValue); err != nil {
					return err
				}
			}
			continue
		}
		if err := writeMultipartFile(vm, writer, field, fieldValue); err != nil {
			return err
		}
	}
	return nil
}

func writeMultipartFile(vm *VM, writer *multipart.Writer, fallbackField string, fileValue TinyValue) error {
	file, ok := vm.valueAsObjectForRead(fileValue)
	if !ok {
		return fmt.Errorf("multipart file entries must be objects")
	}

	field := objectString(file, "field", fallbackField)
	if field == "" {
		return fmt.Errorf("multipart file entry requires field")
	}

	filename := objectString(file, "filename", "")
	contentType := objectString(file, "contentType", "")
	var data []byte

	if pathValue, ok := file["path"]; ok && !isNullish(pathValue) {
		path := valueToString(pathValue)
		if filename == "" {
			filename = filepath.Base(path)
		}
		read, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		data = read
	} else if bytesValue, ok := file["bytes"]; ok && !isNullish(bytesValue) {
		switch v := bytesValue.Value.(type) {
		case *BufferValue:
			data = v.Bytes
		case BufferValue:
			data = v.Bytes
		case []byte:
			data = v
		case string:
			data = []byte(v)
		default:
			return fmt.Errorf("multipart file bytes must be buffer, bytes, or string")
		}
	} else if textValue, ok := file["text"]; ok && !isNullish(textValue) {
		data = []byte(valueToString(textValue))
	} else {
		return fmt.Errorf("multipart file entry requires path, bytes, or text")
	}

	if filename == "" {
		filename = field
	}

	var part io.Writer
	var err error
	if contentType == "" {
		part, err = writer.CreateFormFile(field, filename)
	} else {
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, field, filename))
		header.Set("Content-Type", contentType)
		part, err = writer.CreatePart(header)
	}
	if err != nil {
		return err
	}
	_, err = part.Write(data)
	return err
}

func doHTTPRequest(vm *VM, name string, method string, url string, body TinyValue, options ObjectValue) TinyValue {
	bodyBytes, contentType, err := requestBodyBytes(vm, body)
	if err != nil {
		vm.runtimeError(ErrorType, "%s body error: %s", name, err.Error())
		return NewNull()
	}

	timeoutMs := objectInt(vm, options, "timeoutMs", 30000)
	if timeoutMs <= 0 {
		timeoutMs = 30000
	}

	timeout := time.Duration(timeoutMs) * time.Millisecond

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var reader io.Reader
	if bodyBytes != nil {
		reader = bytes.NewReader(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "%s request creation failed: %s", name, err.Error())
		return NewNull()
	}

	if bodyBytes != nil {
		req.ContentLength = int64(len(bodyBytes))
	}

	headers := ObjectValue{}
	if h, hasHeaders := options["headers"]; hasHeaders {
		if val, ok := vm.valueAsObjectForRead(h); ok {
			headers = val
		}
	}

	for key, value := range headers {
		req.Header.Set(valueToString(ToValue(key)), valueToString(value))
	}

	if contentType != "" && req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := stdHTTPClient.Do(req)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "%s failed: %s", name, err.Error())
		return NewNull()
	}
	defer resp.Body.Close()

	respBodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "%s read response failed: %s", name, err.Error())
		return NewNull()
	}

	headersObj := ObjectValue{}
	for k, v := range resp.Header {
		headersObj[k] = NewNative(strings.Join(v, ","))
	}

	result := ObjectValue{
		"status":     NewInt(resp.StatusCode),
		"statusText": NewNative(resp.Status),
		"headers":    NewNative(headersObj),
	}

	if objectBool(options, "bytes", false) {
		result["body"] = NewNative(respBodyBytes)
	} else {
		result["body"] = NewNative(string(respBodyBytes))
	}

	return NewNative(result)
}

func stdHttpJsonResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.json", args, 1)
	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpJson,
		Value: args[0],
	}))
}

func stdHttpTextResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.text", args, 1)
	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpText,
		Value: args[0],
	}))
}

func stdHttpHtmlResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.html", args, 1)
	vm.push(NewNative(NativeHttpResponseValue{
		Type:  HttpHtml,
		Value: args[0],
	}))
}

func stdHttpStatusResponse(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.status", args, 2)
	vm.push(NewNative(NativeHttpResponseValue{
		Type:   HttpResponse,
		Status: vm.asInt(args[0]),
		Value:  args[1],
	}))
}

func stdHttpResponse(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.response", args, 2, 3)
	headers := ObjectValue{}
	if len(args) == 3 {
		headers = argObject(vm, "http.response", args, 2)
	}
	vm.push(NewNative(NativeHttpResponseValue{
		Type:    HttpResponse,
		Status:  vm.asInt(args[0]),
		Value:   args[1],
		Headers: headers,
	}))
}

func stdHttpRedirect(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.redirect", args, 1, 2)
	status := http.StatusFound
	if len(args) == 2 {
		status = vm.asInt(args[1])
	}
	vm.push(NewNative(NativeHttpResponseValue{
		Type:        HttpRedirect,
		Status:      status,
		RedirectURL: argString(vm, "http.redirect", args, 0),
	}))
}

func stdHttpNoContent(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.noContent", args, 0)
	vm.push(NewNative(NativeHttpResponseValue{
		Type:   HttpNoContent,
		Status: http.StatusNoContent,
	}))
}

func stdHttpFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "http.file", args, 1)
	vm.push(NewNative(NativeHttpResponseValue{
		Type: HttpFile,
		Path: argString(vm, "http.file", args, 0),
	}))
}

func stdHttpDownload(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "http.download", args, 1, 2)
	name := ""
	if len(args) == 2 && !isNullish(args[1]) {
		name = argString(vm, "http.download", args, 1)
	}
	vm.push(NewNative(NativeHttpResponseValue{
		Type:         HttpDownload,
		Path:         argString(vm, "http.download", args, 0),
		DownloadName: name,
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
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while saving downloaded file: %s", err)
		return
	}

	vm.push(NewNative(true))
}

func optionalObjectArg(vm *VM, args []TinyValue, index int) ObjectValue {
	if len(args) <= index || isNullish(args[index]) {
		return ObjectValue{}
	}
	if object, ok := vm.valueAsObjectForRead(args[index]); ok {
		return object
	}
	return ObjectValue{}
}

func objectString(object ObjectValue, key string, fallback string) string {
	if value, ok := object[key]; ok {
		return valueToString(value)
	}
	return fallback
}

func objectInt(vm *VM, object ObjectValue, key string, fallback int) int {
	if value, ok := object[key]; ok {
		return vm.asInt(value)
	}
	return fallback
}

func objectBool(object ObjectValue, key string, fallback bool) bool {
	if value, ok := object[key]; ok {
		if boolValue, ok := value.Value.(bool); ok {
			return boolValue
		}
	}
	return fallback
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
	case TinyValue:
		if v.IsInt {
			return v.AsInt
		}
		return cleanValueForJSON(v.Value)
	case NullValue:
		return nil
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
