//go:build windows
// +build windows

package vm

import (
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"unsafe"

	json "github.com/goccy/go-json"

	. "language.com/src/tinyerrors"
)

var pluginSearchPaths []string

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

		name := asString(vm.pop(), vm)

		availablePlugins := []string{"array", "math", "string", "json", "fs", "app", "buffer", "regex", "io", "process", "time", "error", "http", "os", "runtime", "net", "path"}

		if slices.Contains(availablePlugins, name) {
			vm.push(&StandardModuleValue{Name: name})
		} else {
			LangError(ErrorName, "unknown standard module: %s", name)
		}
	case "load":
		if argCount != 1 {
			LangError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop(), vm)
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

	candidates := []string{}

	// 1. Try relative to current working directory.
	cwd, err := os.Getwd()
	if err == nil {
		candidates = append(candidates, filepath.Join(cwd, path))
	}

	// 2. Try relative to executable folder.
	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, path))
	}

	// 3. Try registered source/import folders.
	for _, base := range pluginSearchPaths {
		candidates = append(candidates, filepath.Join(base, path))
	}

	for _, candidate := range candidates {
		if fileExists(candidate) {
			return candidate
		}
	}

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
