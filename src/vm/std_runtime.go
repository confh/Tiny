package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdRuntimeMetadata = StdModuleInfo{
	Name: "runtime",
	Methods: map[string]StdMethodInfo{
		"lockThread": {
			Name:        "lockThread",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Locks the current goroutine to its current operating system thread.",
		},
		"unlockThread": {
			Name:        "unlockThread",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Unlocks the current goroutine from its operating system thread.",
		},
		"onFatal": {
			Name: "onFatal",
			Args: []StdArg{
				{Name: "callback", Type: "function"},
			},
			Returns:     "undefined",
			Description: "Registers a callback function to be executed when a fatal error occurs.",
		},
		"clearFatalHandler": {
			Name:        "clearFatalHandler",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Clears any previously registered fatal error callback.",
		},
		"memoryStats": {
			Name:        "memoryStats",
			Args:        []StdArg{},
			Returns:     "object",
			Description: "Returns current memory usage statistics for the Go runtime including alloc, totalAlloc, sys, and numGC.",
		},
		"gc": {
			Name:        "gc",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Manually triggers garbage collection.",
		},
	},
}

var stdRuntimeMethods map[string]StdModuleFunc

func init() {
	stdRuntimeMethods = map[string]StdModuleFunc{
		"lockThread":        stdRuntimeLockThread,
		"unlockThread":      stdRuntimeUnlockThread,
		"onFatal":           stdRuntimeOnFatal,
		"clearFatalHandler": stdRuntimeClearOnFatal,
		"memoryStats":       stdRuntimeMemoryStats,
		"gc":                stdRuntimeGC,
	}
	registerStdModule(stdRuntimeMetadata)
}

func (vm *VM) callStdRuntime(method string, args []Value) {
	fn, ok := stdRuntimeMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown runtime function: %s", method)
		return
	}
	fn(vm, args)
}

func stdRuntimeGC(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.gc", args)
	runtime.GC()
	vm.push(NewUndefined())
}

func stdRuntimeLockThread(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.lockThread", args)
	runtime.LockOSThread()
	vm.push(NewUndefined())
}

func stdRuntimeUnlockThread(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.unlockThread", args)
	runtime.UnlockOSThread()
	vm.push(NewUndefined())
}

func stdRuntimeMemoryStats(vm *VM, args []Value) {
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

func stdRuntimeOnFatal(vm *VM, args []Value) {
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

			vm.callFunctionValue(fn, []Value{errObj})
		}()

		return true
	})

	vm.push(NewUndefined())
}

func stdRuntimeClearOnFatal(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.clearFatalHandler", args)
	ClearFatalHook()
	vm.push(NewUndefined())
}
