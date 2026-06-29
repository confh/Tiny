package vm

import (
	"fmt"
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdRuntimeMetadata = StdModuleInfo{
	Name: "runtime",
}

var stdRuntimeMethods map[string]StdModuleFunc

type CompileSourceFunc func(source string, file string) []byte
type CompileFileFunc func(path string) []byte

var CompileSource CompileSourceFunc
var CompileFile CompileFileFunc

func SetCompileSourceFunc(fn CompileSourceFunc) {
	CompileSource = fn
}

func SetCompileFileFunc(fn CompileFileFunc) {
	CompileFile = fn
}

func init() {
	stdRuntimeMethods = map[string]StdModuleFunc{
		"newVM":             stdRuntimeNewVM,
		"lockThread":        stdRuntimeLockThread,
		"unlockThread":      stdRuntimeUnlockThread,
		"onFatal":           stdRuntimeOnFatal,
		"clearFatalHandler": stdRuntimeClearOnFatal,
		"memoryStats":       stdRuntimeMemoryStats,
		"gc":                stdRuntimeGC,
		"isPacked":          stdIsPacked,
		"compileSource":     stdRuntimeCompileSource,
		"compileFile":       stdRuntimeCompileFile,
	}
	registerStdModule(stdRuntimeMetadata)
}

type runtimeVMOptions struct {
	Isolated      bool
	JITDisabled   bool
	RunMainOnLoad bool
	AllowedStdlib map[string]bool
	CLIArgs       []string
	Globals       ObjectValue
}

func (vm *VM) callStdRuntime(method string, args []TinyValue) {
	fn, ok := stdRuntimeMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown runtime function: %s", method)
		return
	}
	fn(vm, args)
}

func stdRuntimeGC(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.gc", args)
	runtime.GC()
	vm.push(NewNull())
}

func stdRuntimeNewVM(vm *VM, args []TinyValue) {
	if len(args) > 1 {
		vm.runtimeError(ErrorRuntime, "runtime.newVM expects 0 or 1 arguments, got %d", len(args))
		return
	}

	options := runtimeVMOptions{
		AllowedStdlib: nil,
		Globals:       ObjectValue{},
	}

	if len(args) == 1 && !isNullish(args[0]) {
		obj := asObject(args[0], vm)
		options = parseRuntimeVMOptions(vm, obj)
	}

	vm.push(NewNative(newNativeVMValue(options)))
}

func parseRuntimeVMOptions(vm *VM, obj ObjectValue) runtimeVMOptions {
	options := runtimeVMOptions{
		Globals: ObjectValue{},
	}

	if value, ok := objectField(obj, "isolated"); ok {
		options.Isolated = boolOption(vm, "isolated", value)
	}
	if value, ok := objectField(obj, "disableJIT"); ok {
		options.JITDisabled = boolOption(vm, "disableJIT", value)
	}
	if value, ok := objectField(obj, "runMainOnLoad"); ok {
		options.RunMainOnLoad = boolOption(vm, "runMainOnLoad", value)
	}

	if options.Isolated {
		options.AllowedStdlib = map[string]bool{
			"runtime": true,
		}
	}

	if value, ok := objectField(obj, "allowedStdlib"); ok && !isNullish(value) {
		allowed := asObject(value, vm)
		options.AllowedStdlib = map[string]bool{}
		for rawName, rawAllowed := range allowed {
			name, ok := rawName.(string)
			if !ok {
				continue
			}
			options.AllowedStdlib[name] = boolOption(vm, "allowedStdlib."+name, rawAllowed)
		}
		if options.Isolated {
			options.AllowedStdlib["runtime"] = true
		}
	}

	if value, ok := objectField(obj, "cliArgs"); ok && !isNullish(value) {
		array := asArray(value, vm)
		options.CLIArgs = make([]string, len(array.Elements))
		for i, arg := range array.Elements {
			str, ok := arg.Value.(string)
			if !ok {
				vm.runtimeError(ErrorType, "runtime.newVM cliArgs[%d] expected string, got %s", i, TypeName(arg))
				return options
			}
			options.CLIArgs[i] = str
		}
	}

	if value, ok := objectField(obj, "globals"); ok && !isNullish(value) {
		globals := asObject(value, vm)
		for rawName, globalValue := range globals {
			name, ok := rawName.(string)
			if !ok || name == "" {
				continue
			}
			options.Globals[name] = cloneValue(globalValue)
		}
	}

	return options
}

func objectField(obj ObjectValue, name string) (TinyValue, bool) {
	value, ok := obj[name]
	return value, ok
}

func boolOption(vm *VM, name string, value TinyValue) bool {
	if b, ok := value.Value.(bool); ok {
		return b
	}
	vm.runtimeError(ErrorType, "runtime.newVM option %s expected bool, got %s", name, TypeName(value))
	return false
}

func stdIsPacked(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.isPacked", args)
	vm.push(NewNative(vm.packed))
}

func stdRuntimeLockThread(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.lockThread", args)
	runtime.LockOSThread()
	vm.push(NewNull())
}

func stdRuntimeUnlockThread(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.unlockThread", args)
	runtime.UnlockOSThread()
	vm.push(NewNull())
}

func stdRuntimeMemoryStats(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.memoryStats", args)

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	vm.push(NewNative(ObjectValue{
		"alloc":      NewNative(float64(m.Alloc)),
		"totalAlloc": NewNative(float64(m.TotalAlloc)),
		"sys":        NewNative(float64(m.Sys)),
		"numGC":      NewInt(int(m.NumGC)),
	}))
}

func stdRuntimeOnFatal(vm *VM, args []TinyValue) {
	expectArgs(vm, "runtime.onFatal", args, 1)

	fn := argFn(vm, "runtime.onFatal", args, 0)

	SetFatalHook(func(info FatalCrashInfo) bool {
		errObj := NewNative(ObjectValue{
			"kind":    NewNative(string(info.Kind)),
			"message": NewNative(info.Message),
			"file":    NewNative(info.File),
			"line":    NewInt(info.Line),
			"column":  NewInt(info.Column),
			"fatal":   NewNative(true),
		})

		func() {
			defer func() {
				recover()
			}()

			vm.callFunctionValue(fn, []TinyValue{errObj})
		}()

		return true
	})

	vm.push(NewNull())
}

func stdRuntimeClearOnFatal(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "runtime.clearFatalHandler", args)
	ClearFatalHook()
	vm.push(NewNull())
}

func stdRuntimeCompileSource(vm *VM, args []TinyValue) {
	if len(args) < 1 || len(args) > 2 {
		vm.runtimeError(ErrorRuntime, "runtime.compileSource expects 1 or 2 arguments, got %d", len(args))
		return
	}

	source := argString(vm, "runtime.compileSource", args, 0)
	file := "<runtime>"
	if len(args) == 2 && !isNullish(args[1]) {
		file = argString(vm, "runtime.compileSource", args, 1)
	}

	if CompileSource == nil {
		vm.runtimeError(ErrorRuntime, "compiler is not available")
		return
	}

	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(LangErrorType); ok {
				msg := err.Message
				if err.File != "" && err.Line > 0 {
					msg = fmt.Sprintf("%s:%d:%d: %s", err.File, err.Line, err.Column, err.Message)
				}
				vm.runtimeError(err.Kind, "%s", msg)
				return
			}
			panic(r)
		}
	}()

	bytes := CompileSource(source, file)
	vm.push(NewNative(&BufferValue{Bytes: bytes}))
}

func stdRuntimeCompileFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "runtime.compileFile", args, 1)

	path := argString(vm, "runtime.compileFile", args, 0)

	if vm.allowedStdlib != nil && !vm.allowedStdlib["fs"] {
		vm.runtimeError(ErrorRuntime, "runtime.compileFile is not allowed because 'fs' module is restricted")
		return
	}

	if CompileFile == nil {
		vm.runtimeError(ErrorRuntime, "compiler is not available")
		return
	}

	defer func() {
		if r := recover(); r != nil {
			if err, ok := r.(LangErrorType); ok {
				msg := err.Message
				if err.File != "" && err.Line > 0 {
					msg = fmt.Sprintf("%s:%d:%d: %s", err.File, err.Line, err.Column, err.Message)
				}
				vm.runtimeError(err.Kind, "%s", msg)
				return
			}
			panic(r)
		}
	}()

	bytes := CompileFile(path)
	vm.push(NewNative(&BufferValue{Bytes: bytes}))
}
