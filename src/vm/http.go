package vm

import (
	"bytes"
	"io"
	"net/http"
	"strings"

	jsoniter "github.com/json-iterator/go"
	. "language.com/src/tinyerrors"
)

var jsonn = jsoniter.ConfigCompatibleWithStandardLibrary

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
	case map[Value]Value:
		// Nested object/map -> clean it recursively
		return cleanMapForJSON(v)

	case []any:
		// Nested array/slice -> clean every element inside it
		cleanedSlice := make([]any, len(v))
		for i, item := range v {
			cleanedSlice[i] = cleanValueForJSON(item)
		}
		return cleanedSlice

	default:
		// Primitive types (string, int, float, bool, nil) are already JSON-safe
		return v
	}
}

func (vm *VM) callStdHttp(method string, args []Value) {
	switch method {
	case "server":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "http.server expects 1 argument")
		}

		port := asInt(args[0])

		server := &NativeServerValue{
			Port:       port,
			GetRoutes:  map[string]Value{},
			PostRoutes: map[string]Value{},
		}

		vm.push(server)

	case "get":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "http.get expects 2 argument")
		}

		url := asString(args[0])
		extra := asObject(args[1])

		var headers map[string]Value
		if h, hasHeaders := extra["headers"]; hasHeaders {
			headers, _ = h.(map[string]Value)
		}

		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "http.get request creation failed: %s", err.Error())
		}

		for key, value := range headers {
			valStr, ok := value.(string)
			if ok {
				req.Header.Set(key, valStr)
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
			result["headers"].(map[string]Value)[k] = strings.Join(v, ",")
		}

		vm.push(result)

	case "post":
		if len(args) < 2 || len(args) > 3 {
			vm.runtimeError(ErrorRuntime, "http.post expects 2 or 3 more arguments")
		}

		url := asString(args[0])
		data := asObject(args[1])

		var headers ObjectValue

		if len(args) == 3 {
			extra := asObject(args[2])
			if h, hasHeaders := extra["headers"]; hasHeaders {
				headers, _ = h.(ObjectValue)
			}
		}

		cleanedData := cleanMapForJSON(data)

		jsonData, err := jsonn.Marshal(cleanedData)
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

	default:
		vm.runtimeError(ErrorName, "unknown http function: %s", method)
	}
}
