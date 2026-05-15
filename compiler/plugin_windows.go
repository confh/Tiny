//go:build windows
// +build windows

package main

import (
	"encoding/json"
	"path/filepath"
	"syscall"
	"unsafe"
)

func defaultPluginPath(path string, ext string) string {
	if filepath.Ext(path) == "" {
		return path + ext
	}

	return path
}

func (vm *VM) callPluginModule(method string, argCount int) {
	switch method {
	case "std":
		if argCount != 1 {
			langError(ErrorRuntime, "Plugin.std expects 1 argument")
		}

		name := asString(vm.pop())

		switch name {
		case "array":
			vm.push(&StandardModuleValue{Name: name})

		default:
			langError(ErrorName, "unknown standard module: %s", name)
		}
	case "load":
		if argCount != 1 {
			langError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop())
		path = defaultPluginPath(path, ".dll")

		dll := syscall.NewLazyDLL(path)

		callProc := dll.NewProc("TinyPluginCall")
		freeProc := dll.NewProc("TinyPluginFree")

		if err := dll.Load(); err != nil {
			langError(ErrorRuntime, "failed to load plugin %s: %v", path, err)
		}

		if err := callProc.Find(); err != nil {
			langError(ErrorRuntime, "plugin missing TinyPluginCall: %v", err)
		}

		if err := freeProc.Find(); err != nil {
			langError(ErrorRuntime, "plugin missing TinyPluginFree: %v", err)
		}

		vm.push(&NativePluginValue{
			Path: path,
			Call: callProc.Addr(),
			Free: freeProc.Addr(),
		})

	default:
		langError(ErrorName, "unknown Plugin function: %s", method)
	}
}

func (vm *VM) callNativePlugin(plugin *NativePluginValue, method string, args []Value) {
	jsonArgs := make([]any, len(args))

	for i, arg := range args {
		jsonArgs[i] = valueToJSONCompatible(arg)
	}

	payload := map[string]any{
		"method": method,
		"args":   jsonArgs,
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		langError(ErrorRuntime, "failed to encode plugin call: %v", err)
	}

	methodPtr, err := syscall.BytePtrFromString(method)
	if err != nil {
		langError(ErrorRuntime, "invalid plugin method: %v", err)
	}

	argsPtr, err := syscall.BytePtrFromString(string(payloadBytes))
	if err != nil {
		langError(ErrorRuntime, "invalid plugin args: %v", err)
	}

	resultPtr, _, _ := syscall.SyscallN(
		plugin.Call,
		uintptr(unsafe.Pointer(methodPtr)),
		uintptr(unsafe.Pointer(argsPtr)),
	)

	if resultPtr == 0 {
		langError(ErrorRuntime, "plugin returned null")
	}

	resultText := cStringToGoString(resultPtr)

	syscall.SyscallN(plugin.Free, resultPtr)

	var result any

	err = json.Unmarshal([]byte(resultText), &result)
	if err != nil {
		langError(ErrorRuntime, "plugin returned invalid JSON: %v", err)
	}

	if obj, ok := result.(map[string]any); ok {
		if errValue, exists := obj["error"]; exists {
			errObj, ok := errValue.(map[string]any)
			if ok {
				kind, _ := errObj["kind"].(string)
				message, _ := errObj["message"].(string)

				if kind == "" {
					kind = "PluginError"
				}

				langError(ErrorKind(kind), "%s", message)
			}
		}
	}

	vm.push(jsonToTinyValue(result))
}

func cStringToGoString(ptr uintptr) string {
	if ptr == 0 {
		return ""
	}

	bytes := []byte{}

	for {
		b := *(*byte)(unsafe.Pointer(ptr))
		if b == 0 {
			break
		}

		bytes = append(bytes, b)
		ptr++
	}

	return string(bytes)
}
