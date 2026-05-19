package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callStdMath(method string, args []Value) {
	switch method {
	case "toFloat":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "math.toFloat expects 1 argument")
		}

		vm.push(asFloat(args[0]))

	case "toInt":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "math.toInt expects 1 argument")
		}

		vm.push(int(asFloat(args[0])))

	default:
		vm.runtimeError(ErrorName, "unknown math function: %s", method)
	}
}
