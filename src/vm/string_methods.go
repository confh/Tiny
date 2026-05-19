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
			vm.runtimeError(ErrorRuntime, "string.split expects 1 argument")
		}

		separator := asString(args[0], vm)

		splitStrings := strings.Split(value, separator)
		elements := make([]Value, len(splitStrings))
		for i, s := range splitStrings {
			elements[i] = s
		}
		array := &ArrayValue{
			Elements: elements,
		}

		vm.push(array)

	case "includes":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "string.includes expects 1 argument")
		}

		search := asString(args[0], vm)

		vm.push(strings.Contains(value, search))

	case "trim":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "String.trim expects 0 arguments")
		}

		vm.push(strings.TrimSpace(value))

	case "replace":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "String.replace expects 2 arguments")
		}

		oldText := asString(args[0], vm)
		newText := asString(args[1], vm)

		vm.push(strings.Replace(value, oldText, newText, 1))

	case "replaceAll":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "String.replaceAll expects 2 arguments")
		}

		oldText := asString(args[0], vm)
		newText := asString(args[1], vm)

		vm.push(strings.ReplaceAll(value, oldText, newText))

	default:
		vm.runtimeError(ErrorName, "unknown string method: %s", method)
	}
}
