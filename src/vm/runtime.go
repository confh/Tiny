package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdRuntime(method string, args []Value) {
	switch method {
	case "lockThread":
		runtime.LockOSThread()
		vm.push(UndefinedValue{})

	case "unlockThread":
		runtime.UnlockOSThread()
		vm.push(UndefinedValue{})

	default:
		vm.runtimeError(ErrorName, "unknown runtime function: %s", method)
	}
}
