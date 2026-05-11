//go:build linux && cgo
// +build linux,cgo

package main

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdlib.h>

typedef char* (*tiny_call_fn)(const char*, const char*);
typedef void (*tiny_free_fn)(char*);

static void* tiny_open(const char* path) {
	return dlopen(path, RTLD_NOW);
}

static void* tiny_sym(void* handle, const char* name) {
	return dlsym(handle, name);
}

static const char* tiny_err() {
	const char* err = dlerror();
	return err;
}

static char* tiny_call(void* fn, const char* method, const char* args) {
	return ((tiny_call_fn)fn)(method, args);
}

static void tiny_call_free(void* fn, char* ptr) {
	((tiny_free_fn)fn)(ptr);
}

static int tiny_close(void* handle) {
	return dlclose(handle);
}
*/
import "C"

import (
	"encoding/json"
	"path/filepath"
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
	case "load":
		if argCount != 1 {
			langError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop())
		path = defaultPluginPath(path, ".so")

		cpath := C.CString(path)
		defer C.free(unsafe.Pointer(cpath))

		handle := C.tiny_open(cpath)
		if handle == nil {
			errText := C.GoString(C.tiny_err())
			langError(ErrorRuntime, "failed to load plugin %s: %s", path, errText)
		}

		callName := C.CString("TinyPluginCall")
		defer C.free(unsafe.Pointer(callName))

		freeName := C.CString("TinyPluginFree")
		defer C.free(unsafe.Pointer(freeName))

		callPtr := C.tiny_sym(handle, callName)
		if callPtr == nil {
			langError(ErrorRuntime, "plugin missing TinyPluginCall")
		}

		freePtr := C.tiny_sym(handle, freeName)
		if freePtr == nil {
			langError(ErrorRuntime, "plugin missing TinyPluginFree")
		}

		vm.push(&NativePluginValue{
			Path:   path,
			Handle: handle,
			Call:   uintptr(callPtr),
			Free:   uintptr(freePtr),
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

	cmethod := C.CString(method)
	defer C.free(unsafe.Pointer(cmethod))

	cargs := C.CString(string(payloadBytes))
	defer C.free(unsafe.Pointer(cargs))

	resultPtr := C.tiny_call(unsafe.Pointer(plugin.Call), cmethod, cargs)
	if resultPtr == nil {
		langError(ErrorRuntime, "plugin returned null")
	}

	resultText := C.GoString(resultPtr)

	C.tiny_call_free(unsafe.Pointer(plugin.Free), resultPtr)

	var result any
	err = json.Unmarshal([]byte(resultText), &result)
	if err != nil {
		langError(ErrorRuntime, "plugin returned invalid JSON: %v", err)
	}

	if obj, ok := result.(map[string]any); ok {
		if errValue, exists := obj["error"]; exists {
			if errObj, ok := errValue.(map[string]any); ok {
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
