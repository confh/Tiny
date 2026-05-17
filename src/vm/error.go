package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdError(method string, args []Value) {
	switch method {
	case "new":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "error.new expects 2 arguments")
		}

		kind := asString(args[0])
		message := asString(args[1])

		vm.push(ErrorValue{
			Kind:    kind,
			Message: message,
		})

	default:
		vm.runtimeError(ErrorName, "unknown error function: %s", method)
	}
}
