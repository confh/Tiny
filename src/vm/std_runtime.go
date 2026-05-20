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
			Returns:     "void",
			Description: "Locks the current goroutine to its current operating system thread.",
		},
		"unlockThread": {
			Name:        "unlockThread",
			Args:        []StdArg{},
			Returns:     "void",
			Description: "Unlocks the current goroutine from its operating system thread.",
		},
	},
}

var stdRuntimeMethods = map[string]StdModuleFunc{
	"lockThread":   stdRuntimeLockThread,
	"unlockThread": stdRuntimeUnlockThread,
}

func init() {
	registerStdModule(stdRuntimeMetadata)
}

func (vm *VM) callStdRuntime(method string, args []Value) {
	fn, ok := stdRuntimeMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown runtime function: %s", method)
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
