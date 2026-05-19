package vm

import (
	"strings"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdArray(method string, args []Value) {
	switch method {
	case "range":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "array.range expects 2 arguments")
		}

		min := asInt(args[0])
		max := asInt(args[1])

		array := &ArrayValue{
			Elements: make([]Value, 0, max-min),
		}

		for i := min; i < max+1; i++ {
			array.Elements = append(array.Elements, i)
		}

		vm.push(array)

	case "isArray":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.isArray expects 1 argument")
		}

		_, ok := args[0].(*ArrayValue)

		vm.push(ok)

	case "from":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.isArray expects 1 argument")
		}

		switch v := args[0].(type) {
		case string:
			vm.push(strings.Split(v, ""))
		case *ArrayValue:
			dst := make([]Value, len(v.Elements))

			copy(dst, v.Elements)

			vm.push(dst)
		default:
			vm.runtimeError(ErrorType, "type %s cannot be turned into an array", typeName(v))
		}
	default:
		vm.runtimeError(ErrorName, "unknown array function: %s", method)
	}
}
