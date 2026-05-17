package vm

import (
	"slices"
	"strings"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callArrayMethod(array *ArrayValue, method string, args []Value) {
	switch method {
	case "length":
		vm.push(len(array.Elements))

	case "push":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.push expects 1 argument")
		}

		array.Elements = append(array.Elements, args[0])

		vm.push(array)

	case "pop":
		if len(array.Elements) == 0 {
			vm.push(UndefinedValue{})
			return
		}

		last := array.Elements[len(array.Elements)-1]
		array.Elements = array.Elements[:len(array.Elements)-1]

		vm.push(last)

	case "get":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.get expects 1 argument")
		}

		index := asInt(args[0])

		if index < 0 || index >= len(array.Elements) {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		}

		vm.push(array.Elements[index])

	case "set":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "array.set expects 2 arguments")
		}

		index := asInt(args[0])

		if index < 0 || index >= len(array.Elements) {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		}

		array.Elements[index] = args[1]

		vm.push(array)

	case "contains":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.contains expects 1 argument")
		}

		element := args[0]

		vm.push(slices.Contains(array.Elements, element))

	case "join":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.join expects 1 argument")
		}

		separator := asString(args[0])

		var sb strings.Builder

		for i, value := range array.Elements {
			if i == len(array.Elements)-1 {
				sb.WriteString(valueToString(value))
			} else {
				sb.WriteString(valueToString(value))
				sb.WriteString(separator)
			}
		}

		vm.push(sb.String())

	case "reverse":
		slices.Reverse(array.Elements)

		vm.push(array.Elements)

	case "map":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.map expects 1 argument")
		}

		value := args[0]

		fn, ok := value.(FunctionValue)
		if !ok {
			vm.runtimeError(ErrorType, "array.map expects function, got %s", typeName(value))
		}

		mappedArray := &ArrayValue{
			Elements: make([]Value, 0, len(array.Elements)),
		}

		for i, v := range array.Elements {
			result := vm.callFunctionValue(fn, []Value{i, v})

			mappedArray.Elements = append(mappedArray.Elements, result)
		}

		vm.push(mappedArray)

	case "forEach":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.forEach expects 1 argument")
		}

		value := args[0]

		fn, ok := value.(FunctionValue)
		if !ok {
			vm.runtimeError(ErrorType, "array.forEach expects function, got %s", typeName(value))
		}

		for i, v := range array.Elements {
			vm.callFunctionValue(fn, []Value{i, v})
		}

		vm.push(true)

	case "filter":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "array.filter expects 1 argument")
		}

		value := args[0]

		fn, ok := value.(FunctionValue)
		if !ok {
			vm.runtimeError(ErrorType, "array.filter expects function, got %s", typeName(value))
		}

		filteredArray := &ArrayValue{
			Elements: []Value{},
		}

		for i, v := range array.Elements {
			result := vm.callFunctionValue(fn, []Value{i, v})

			if isTruthy(result) {
				filteredArray.Elements = append(filteredArray.Elements, v)
			}
		}

		vm.push(filteredArray)

	case "clear":
		array.Elements = []Value{}

		vm.push(true)

	default:
		vm.runtimeError(ErrorName, "unknown array method: %s", method)
	}
}
