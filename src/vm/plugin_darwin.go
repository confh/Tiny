//go:build darwin
// +build darwin

package vm

import (
	"os"
	"path/filepath"
	"slices"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"

	. "language.com/src/tinyerrors"
)

var pluginSearchPaths []string

func SetPluginSearchPaths(paths []string) {
	pluginSearchPaths = append([]string{}, paths...)
}

type loadedNativePluginFuncs struct {
	callJSON func(method string, argsJSON string) *byte
	freeJSON func(ptr *byte)

	callMsgPack func(bytes *byte, length int) *byte
	freeMsgPack func(ptr *byte)
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

		if vm.allowedStdlib != nil && !vm.allowedStdlib[name] {
			vm.fatalError(ErrorRuntime, "standard module '%s' is not allowed in this VM", name)
		}

		if slices.Contains(AvailablePlugins, name) {
			vm.push(NewNative(&StandardModuleValue{Name: name}))
			return
		}

		vm.fatalError(ErrorName, "unknown standard module: %s", name)

	case "load":
		if argCount != 1 {
			vm.fatalError(ErrorRuntime, "Plugin.load expects 1 argument")
		}

		path := asString(vm.pop(), vm)
		path = resolvePluginPath(path, ".dylib")

		handle, err := purego.Dlopen(path, purego.RTLD_NOW)
		if err != nil {
			vm.fatalError(ErrorRuntime, "failed to load plugin %s: %v", path, err)
		}

		// 1. Try to find MsgPack functions
		callPtr, err := purego.Dlsym(handle, "TinyPluginCallMsgPack")
		isMsgPack := true
		var freePtr uintptr

		// 2. Fall back to JSON if MsgPack is missing
		if err != nil || callPtr == 0 {
			isMsgPack = false
			callPtr, err = purego.Dlsym(handle, "TinyPluginCall")
			if err != nil || callPtr == 0 {
				_ = purego.Dlclose(handle)
				vm.fatalError(ErrorRuntime, "plugin missing both TinyPluginCallMsgPack and TinyPluginCall")
			}
			freePtr, err = purego.Dlsym(handle, "TinyPluginFree")
			if err != nil || freePtr == 0 {
				_ = purego.Dlclose(handle)
				vm.fatalError(ErrorRuntime, "plugin missing TinyPluginFree")
			}
		} else {
			// Found MsgPack call, find the corresponding free function
			freePtr, err = purego.Dlsym(handle, "TinyPluginFreeMsgPack")
			if err != nil || freePtr == 0 {
				_ = purego.Dlclose(handle)
				vm.fatalError(ErrorRuntime, "plugin missing TinyPluginFreeMsgPack")
			}
		}

		// 3. Register the appropriate Go function wrapper
		var fns loadedNativePluginFuncs
		if isMsgPack {
			var callFn func(bytes *byte, length int) *byte
			var freeFn func(ptr *byte)
			purego.RegisterFunc(&callFn, callPtr)
			purego.RegisterFunc(&freeFn, freePtr)
			fns.callMsgPack = callFn
			fns.freeMsgPack = freeFn
		} else {
			var callFn func(method string, argsJSON string) *byte
			var freeFn func(ptr *byte)
			purego.RegisterFunc(&callFn, callPtr)
			purego.RegisterFunc(&freeFn, freePtr)
			fns.callJSON = callFn
			fns.freeJSON = freeFn
		}

		// Store wrappers in map
		nativePluginFuncs.Lock()
		nativePluginFuncs.byCallPtr[callPtr] = fns
		nativePluginFuncs.Unlock()

		// 4. Push the unified struct
		vm.push(NewNative(&NativePluginValue{
			Path:      path,
			Handle:    unsafe.Pointer(handle),
			Call:      callPtr,
			Free:      freePtr,
			IsMsgPack: isMsgPack,
		}))

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

func (vm *VM) callNativePlugin(plugin *NativePluginValue, method string, args []TinyValue) {
	nativePluginFuncs.RLock()
	fns, ok := nativePluginFuncs.byCallPtr[plugin.Call]
	nativePluginFuncs.RUnlock()

	if !ok {
		vm.fatalError(ErrorRuntime, "plugin %s is not loaded correctly", plugin.Path)
	}

	if plugin.IsMsgPack {
		if fns.callMsgPack == nil || fns.freeMsgPack == nil {
			vm.fatalError(ErrorRuntime, "plugin MsgPack functions not loaded correctly")
		}

		// 1. Encode parameters to MessagePack
		msgpackBytes := vm.encodeToMsgPack(method, args)

		// 2. Call the native function
		resultPtr := fns.callMsgPack(&msgpackBytes[0], len(msgpackBytes))
		if resultPtr == nil {
			vm.fatalError(ErrorRuntime, "plugin returned null")
		}

		// 3. Convert C byte pointer to Go slice and free C memory
		resultBytes := cBytesToGo(uintptr(unsafe.Pointer(resultPtr)))
		fns.freeMsgPack(resultPtr)

		// 4. Decode the result and push to stack
		vm.push(vm.decodeFromMsgPack(resultBytes))
	} else {
		// Legacy JSON path
		if fns.callJSON == nil || fns.freeJSON == nil {
			vm.fatalError(ErrorRuntime, "plugin JSON functions not loaded correctly")
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

		resultPtr := fns.callJSON(method, string(payloadBytes))
		if resultPtr == nil {
			vm.fatalError(ErrorRuntime, "plugin returned null")
		}

		resultText := cStringToGo(resultPtr)
		fns.freeJSON(resultPtr)

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
