package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callTextBuilderMethod(sb *NativeStringBuilderValue, method string, args []Value) {
	switch method {
	case "writeString":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "stringBuilder.writeString expects 1 argument")
		}

		str := asString(args[0], vm)
		sb.Builder.WriteString(str)

		vm.push(UndefinedValue{})

	case "string":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "stringBuilder.string expects = arguments")
		}

		vm.push(sb.Builder.String())

	default:
		vm.runtimeError(ErrorName, "unknown stringBuilder method: %s", method)
	}
}
