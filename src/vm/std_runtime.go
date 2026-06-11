package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdRuntimeMetadata = StdModuleInfo{
	Name: "runtime",
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
		"isPacked":          stdIsPacked,
	}
	registerStdModule(stdRuntimeMetadata)
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
