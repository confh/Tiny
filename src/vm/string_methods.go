package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callStringMethod(value string, method string, args []Value) {
	switch method {
	case "length":
		vm.push(len(value))
	default:
		LangError(ErrorName, "unknown buffer method: %s", method)
	}
}
