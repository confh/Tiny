//go:build windows
// +build windows

package vm

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unsafe"

	. "language.com/src/tinyerrors"
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
			LangError(ErrorRuntime, "Plugin.std expects 1 argument")
		}

		name := asString(vm.pop())

		availablePlugins := []string{"array", "math", "string", "json", "fs", "app", "buffer", "regex", "io", "process", "time", "error", "http"}

		if slices.Contains(availablePlugins, name) {
			vm.push(&StandardModuleValue{Name: name})
		} else {
			LangError(ErrorName, "unknown standard module: %s", name)
		}
	case "load":
		if argCount != 1 {
			LangError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop())
		path = resolvePluginPath(path, ".dll")

		dll := syscall.NewLazyDLL(path)

		callProc := dll.NewProc("TinyPluginCall")
		freeProc := dll.NewProc("TinyPluginFree")

		if err := dll.Load(); err != nil {
			LangError(ErrorRuntime, "failed to load plugin %s: %v", path, err)
		}

		if err := callProc.Find(); err != nil {
			LangError(ErrorRuntime, "plugin missing TinyPluginCall: %v", err)
		}

		if err := freeProc.Find(); err != nil {
			LangError(ErrorRuntime, "plugin missing TinyPluginFree: %v", err)
		}

		vm.push(&NativePluginValue{
			Path: path,
			Call: callProc.Addr(),
			Free: freeProc.Addr(),
		})

	default:
		LangError(ErrorName, "unknown Plugin function: %s", method)
	}
}

func resolvePluginPath(path string, ext string) string {
	path = defaultPluginPath(path, ext)

	if filepath.IsAbs(path) {
		return path
	}

	// 1. Try relative to the current working directory first.
	// This is what you want when running:
	// tiny main.tiny
	cwd, err := os.Getwd()
	if err == nil {
		candidate := filepath.Join(cwd, path)

		if fileExists(candidate) {
			return candidate
		}
	}

	// 2. Try relative to the executable folder.
	// This is useful for packed/dist apps:
	// dist/app.exe
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidate := filepath.Join(exeDir, path)

		if fileExists(candidate) {
			return candidate
		}
	}

	// 3. Fallback to original path so the error message stays clear.
	return path
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
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
		LangError(ErrorRuntime, "failed to encode plugin call: %v", err)
	}

	methodPtr, err := syscall.BytePtrFromString(method)
	if err != nil {
		LangError(ErrorRuntime, "invalid plugin method: %v", err)
	}

	argsPtr, err := syscall.BytePtrFromString(string(payloadBytes))
	if err != nil {
		LangError(ErrorRuntime, "invalid plugin args: %v", err)
	}

	resultPtr, _, _ := syscall.SyscallN(
		plugin.Call,
		uintptr(unsafe.Pointer(methodPtr)),
		uintptr(unsafe.Pointer(argsPtr)),
	)

	if resultPtr == 0 {
		LangError(ErrorRuntime, "plugin returned null")
	}

	resultText := cStringToGoString(resultPtr)

	syscall.SyscallN(plugin.Free, resultPtr)

	var result any

	err = json.Unmarshal([]byte(resultText), &result)
	if err != nil {
		LangError(ErrorRuntime, "plugin returned invalid JSON: %v", err)
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

				LangError(ErrorKind(kind), "%s", message)
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
