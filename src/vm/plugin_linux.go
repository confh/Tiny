//go:build linux
// +build linux

package vm

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	json "github.com/goccy/go-json"

	. "language.com/src/tinyerrors"
)

var pluginSearchPaths []string

type loadedNativePluginFuncs struct {
	call func(method string, argsJSON string) *byte
	free func(ptr *byte)
}

var nativePluginFuncs = struct {
	sync.RWMutex
	byCallPtr map[uintptr]loadedNativePluginFuncs
}{
	byCallPtr: map[uintptr]loadedNativePluginFuncs{},
}

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
			vm.fatalError(ErrorRuntime, "Plugin.std expects 1 argument")
		}

		name := asString(vm.pop(), vm)

		availablePlugins := []string{
			"array", "math", "string", "json", "fs", "app", "buffer",
			"regex", "io", "process", "time", "error", "http", "os",
			"runtime", "net", "path", "object",
		}

		if slices.Contains(availablePlugins, name) {
			vm.push(&StandardModuleValue{Name: name})
			return
		}

		vm.fatalError(ErrorName, "unknown standard module: %s", name)

	case "load":
		if argCount != 1 {
			vm.fatalError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop(), vm)
		path = resolvePluginPath(path, ".so")

		handle, err := purego.Dlopen(path, purego.RTLD_NOW)
		if err != nil {
			vm.fatalError(ErrorRuntime, "failed to load plugin %s: %v", path, err)
		}

		callPtr, err := purego.Dlsym(handle, "TinyPluginCall")
		if err != nil || callPtr == 0 {
			_ = purego.Dlclose(handle)
			if err != nil {
				vm.fatalError(ErrorRuntime, "plugin missing TinyPluginCall: %v", err)
			}
			vm.fatalError(ErrorRuntime, "plugin missing TinyPluginCall")
		}

		freePtr, err := purego.Dlsym(handle, "TinyPluginFree")
		if err != nil || freePtr == 0 {
			_ = purego.Dlclose(handle)
			if err != nil {
				vm.fatalError(ErrorRuntime, "plugin missing TinyPluginFree: %v", err)
			}
			vm.fatalError(ErrorRuntime, "plugin missing TinyPluginFree")
		}

		var callFn func(method string, argsJSON string) *byte
		var freeFn func(ptr *byte)

		purego.RegisterFunc(&callFn, callPtr)
		purego.RegisterFunc(&freeFn, freePtr)

		nativePluginFuncs.Lock()
		nativePluginFuncs.byCallPtr[callPtr] = loadedNativePluginFuncs{
			call: callFn,
			free: freeFn,
		}
		nativePluginFuncs.Unlock()

		vm.push(&NativePluginValue{
			Path:   path,
			Handle: unsafe.Pointer(handle),
			Call:   callPtr,
			Free:   freePtr,
		})

	default:
		vm.fatalError(ErrorName, "unknown Plugin function: %s", method)
	}
}

func resolvePluginPath(path string, ext string) string {
	path = defaultPluginPath(path, ext)

	if filepath.IsAbs(path) {
		return path
	}

	candidates := []string{}

	cwd, err := os.Getwd()
	if err == nil {
		candidates = append(candidates, filepath.Join(cwd, path))
	}

	exePath, err := os.Executable()
	if err == nil {
		exeDir := filepath.Dir(exePath)
		candidates = append(candidates, filepath.Join(exeDir, path))
	}

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
	nativePluginFuncs.RLock()
	fns, ok := nativePluginFuncs.byCallPtr[plugin.Call]
	nativePluginFuncs.RUnlock()

	if !ok || fns.call == nil || fns.free == nil {
		vm.fatalError(ErrorRuntime, "plugin %s is not loaded correctly", plugin.Path)
	}

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
		vm.fatalError(ErrorRuntime, "failed to encode plugin call: %v", err)
	}

	resultPtr := fns.call(method, string(payloadBytes))
	if resultPtr == nil {
		vm.fatalError(ErrorRuntime, "plugin returned null")
	}

	resultText := cStringToGo(resultPtr)
	fns.free(resultPtr)

	var result any
	err = json.Unmarshal([]byte(resultText), &result)
	if err != nil {
		vm.fatalError(ErrorRuntime, "plugin returned invalid JSON: %v", err)
	}

	if obj, ok := result.(map[string]any); ok {
		if errValue, exists := obj["error"]; exists {
			if errObj, ok := errValue.(map[string]any); ok {
				kind, _ := errObj["kind"].(string)
				message, _ := errObj["message"].(string)

				if kind == "" {
					kind = string(ErrorRuntime)
				}

				if message == "" {
					message = "plugin returned an error"
				}

				vm.fatalError(ErrorKind(kind), "%s", message)
			}
		}
	}

	vm.push(jsonToTinyValue(result))
}

func cStringToGo(ptr *byte) string {
	if ptr == nil {
		return ""
	}

	n := 0
	base := uintptr(unsafe.Pointer(ptr))

	for {
		b := *(*byte)(unsafe.Pointer(base + uintptr(n)))
		if b == 0 {
			break
		}
		n++
	}

	return string(unsafe.Slice(ptr, n))
}
