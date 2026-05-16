package vm

import (
	"strings"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStringMethod(value string, method string, args []Value) {
	switch method {
	case "length":
		vm.push(len(value))

	case "toUpperCase":
		vm.push(strings.ToUpper(value))

	case "toLowerCase":
		vm.push(strings.ToLower(value))

	case "split":
		if len(args) != 1 {
			LangError(ErrorRuntime, "string.split expects 1 argument")
		}

		separator := asString(args[0])

		splitStrings := strings.Split(value, separator)
		elements := make([]Value, len(splitStrings))
		for i, s := range splitStrings {
			elements[i] = s
		}
		array := &ArrayValue{
			Elements: elements,
		}

		vm.push(array)
	default:
		LangError(ErrorName, "unknown buffer method: %s", method)
	}
}
