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
	},
}

var stdRuntimeMethods map[string]StdModuleFunc

func init() {
	stdRuntimeMethods = map[string]StdModuleFunc{
		"lockThread":        stdRuntimeLockThread,
		"unlockThread":      stdRuntimeUnlockThread,
		"onFatal":           stdRuntimeOnFatal,
		"clearFatalHandler": stdRuntimeClearOnFatal,
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

func stdRuntimeLockThread(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.lockThread", args)

	runtime.LockOSThread()
	vm.push(UndefinedValue{})
}

func stdRuntimeUnlockThread(vm *VM, args []Value) {
	dontExpectArgs(vm, "runtime.unlockThread", args)

	runtime.UnlockOSThread()
	vm.push(UndefinedValue{})
}

func stdRuntimeOnFatal(vm *VM, args []Value) {
	expectArgs(vm, "runtime.onFatal", args, 1)

	fn, ok := args[0].(FunctionValue)
	if !ok {
		vm.fatalError(ErrorType, "runtime.onFatal expects function")
	}

	SetFatalHook(func(info FatalCrashInfo) bool {
		errObj := ObjectValue{
			"kind":    string(info.Kind),
			"message": info.Message,
			"file":    info.File,
			"line":    info.Line,
			"column":  info.Column,
			"fatal":   true,
		}

		func() {
			defer func() {
				recover()
			}()

			vm.callFunctionValue(fn, []Value{errObj})
		}()

		return true
	})

	vm.push(UndefinedValue{})
}

func stdRuntimeClearOnFatal(vm *VM, args []Value) {
	expectArgs(vm, "runtime.clearFatalHandler", args, 0)

	ClearFatalHook()
	vm.push(UndefinedValue{})
}
