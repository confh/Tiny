package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdRuntimeMethods = map[string]StdModuleFunc{
	"lockThread":   stdRuntimeLockThread,
	"unlockThread": stdRuntimeUnlockThread,
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
