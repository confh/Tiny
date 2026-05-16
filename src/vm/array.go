package vm

import (
	"slices"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdArray(method string, args []Value) {
	switch method {
	case "push":
		if len(args) != 2 {
			LangError(ErrorRuntime, "array.push expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.push expects array, got %s", typeName(args[0]))
		}

		arr.Elements = append(arr.Elements, args[1])

		vm.push(arr)

	case "pop":
		if len(args) != 1 {
			LangError(ErrorRuntime, "array.pop expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.pop expects array, got %s", typeName(args[0]))
		}

		if len(arr.Elements) == 0 {
			vm.push(UndefinedValue{})
			return
		}

		last := arr.Elements[len(arr.Elements)-1]
		arr.Elements = arr.Elements[:len(arr.Elements)-1]

		vm.push(last)

	case "len":
		if len(args) != 1 {
			LangError(ErrorRuntime, "array.len expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.len expects array, got %s", typeName(args[0]))
		}

		vm.push(len(arr.Elements))

	case "get":
		if len(args) != 2 {
			LangError(ErrorRuntime, "array.get expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.get expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			LangError(ErrorRuntime, "array index out of range: %d", index)
		}

		vm.push(arr.Elements[index])

	case "set":
		if len(args) != 3 {
			LangError(ErrorRuntime, "array.set expects 3 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.set expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			LangError(ErrorRuntime, "array index out of range: %d", index)
		}

		arr.Elements[index] = args[2]

		vm.push(arr)

	case "contains":
		if len(args) != 2 {
			LangError(ErrorRuntime, "array.contains expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			LangError(ErrorType, "array.contains expects array, got %s", typeName(args[0]))
		}

		element := args[1]

		vm.push(slices.Contains(arr.Elements, element))

	default:
		LangError(ErrorName, "unknown array function: %s", method)
	}
}
