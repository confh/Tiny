package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdOS(method string, args []Value) {
	switch method {
	case "name":
		vm.push(runtime.GOOS)

	case "arch":
		vm.push(runtime.GOARCH)

	default:
		vm.runtimeError(ErrorName, "unknown buffer function: %s", method)
	}
}
