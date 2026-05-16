package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callStdMath(method string, args []Value) {
	switch method {
	case "toFloat":
		if len(args) != 1 {
			LangError(ErrorRuntime, "math.toFloat expects 1 argument")
		}

		vm.push(asFloat(args[0]))

	case "toInt":
		if len(args) != 1 {
			LangError(ErrorRuntime, "math.toInt expects 1 argument")
		}

		vm.push(int(asFloat(args[0])))

	default:
		LangError(ErrorName, "unknown math function: %s", method)
	}
}
