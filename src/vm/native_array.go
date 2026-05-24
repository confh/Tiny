package vm

import (
	"slices"
	"strings"

	. "language.com/src/tinyerrors"
)

var arrayNativeMetadata = NativeTypeInfo{
	Name: "array",
	Methods: map[string]StdMethodInfo{
		"length": {
			Name:        "length",
			Returns:     "number",
			Description: "Returns the array length.",
		},
		"push": {
			Name: "push",
			Args: []StdArg{
				{Name: "value", Type: "any"},
			},
			Returns:     "array",
			Description: "Adds a value to the array.",
		},
		"get": {
			Name: "get",
			Args: []StdArg{
				{Name: "index", Type: "number"},
			},
			Returns:     "any",
			Description: "Gets an item by index.",
		},
		"pop": {
			Name:        "pop",
			Returns:     "any",
			Description: "Removes the last element from the array and returns it.",
		},
		"set": {
			Name: "set",
			Args: []StdArg{
				{Name: "index", Type: "number"},
				{Name: "value", Type: "any"},
			},
			Returns:     "array",
			Description: "Sets the value at the given index.",
		},
		"contains": {
			Name: "contains",
			Args: []StdArg{
				{Name: "value", Type: "any"},
			},
			Returns:     "bool",
			Description: "Returns true if the array contains the value.",
		},
		"join": {
			Name: "join",
			Args: []StdArg{
				{Name: "separator", Type: "string"},
			},
			Returns:     "string",
			Description: "Joins array elements into a string, separated by the given separator.",
		},
		"reverse": {
			Name:        "reverse",
			Returns:     "array",
			Description: "Reverses the array elements in place.",
		},
		"map": {
			Name: "map",
			Args: []StdArg{
				{Name: "fn", Type: "function"},
			},
			Returns:     "array",
			Description: "Returns a new array with the results of calling a function on every element.",
		},
		"forEach": {
			Name: "forEach",
			Args: []StdArg{
				{Name: "fn", Type: "function"},
			},
			Returns:     "bool",
			Description: "Calls a function for each element in the array.",
		},
		"filter": {
			Name: "filter",
			Args: []StdArg{
				{Name: "fn", Type: "function"},
			},
			Returns:     "array",
			Description: "Returns a new array with the elements that pass the test implemented by the function.",
		},
		"clear": {
			Name:        "clear",
			Returns:     "bool",
			Description: "Removes all elements from the array.",
		},
		"remove": {
			Name:    "remove",
			Returns: "bool",
			Args: []StdArg{
				{Name: "index", Type: "number"},
			},
			Description: "Removes the specificed index of the element from the array.",
		},
	},
}

func init() {
	registerNativeType(arrayNativeMetadata)
}

var arrayMethods map[string]NativeModuleFunc[*ArrayValue]

func init() {
	arrayMethods = map[string]NativeModuleFunc[*ArrayValue]{
		"length":   arrayLength,
		"push":     arrayPush,
		"pop":      arrayPop,
		"get":      arrayGet,
		"set":      arraySet,
		"contains": arrayContains,
		"join":     arrayJoin,
		"reverse":  arrayReverse,
		"map":      arrayMap,
		"forEach":  arrayForEach,
		"filter":   arrayFilter,
		"clear":    arrayClear,
		"remove":   arrayRemove,
	}

	registerNativeType(arrayNativeMetadata)
}

func (vm *VM) callArrayMethod(array *ArrayValue, method string, args []Value) {
	fn, ok := arrayMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown array method: %s", method)
		return
	}
	fn(vm, array, args)
}

func arrayLength(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.length", args, 0)
	vm.push(len(array.Elements))
}

func arrayPush(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.push", args, 1)
	array.Elements = append(array.Elements, args[0])
	vm.push(array)
}

func arrayPop(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.pop", args, 0)
	if len(array.Elements) == 0 {
		vm.push(UndefinedValue{})
		return
	}
	last := array.Elements[len(array.Elements)-1]
	array.Elements = array.Elements[:len(array.Elements)-1]
	vm.push(last)
}

func arrayGet(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.get", args, 1)
	index := argInt(vm, "array.get", args, 0)
	if index < 0 || index >= len(array.Elements) {
		vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		return
	}
	vm.push(array.Elements[index])
}

func arraySet(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.set", args, 2)
	index := argInt(vm, "array.set", args, 0)
	if index < 0 || index >= len(array.Elements) {
		vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		return
	}
	array.Elements[index] = args[1]
	vm.push(array)
}

func arrayContains(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.contains", args, 1)
	element := args[0]
	vm.push(slices.Contains(array.Elements, element))
}

func arrayJoin(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.join", args, 1)
	separator := argString(vm, "array.join", args, 0)
	var sb strings.Builder
	for i, value := range array.Elements {
		sb.WriteString(valueToString(value))
		if i != len(array.Elements)-1 {
			sb.WriteString(separator)
		}
	}
	vm.push(sb.String())
}

func arrayReverse(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.reverse", args, 0)
	slices.Reverse(array.Elements)
	vm.push(array.Elements)
}

func arrayMap(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.map", args, 1)
	fn, ok := args[0].(FunctionValue)
	if !ok {
		vm.runtimeError(ErrorType, "array.map expects function, got %s", TypeName(args[0]))
		return
	}
	mappedArray := &ArrayValue{
		Elements: make([]Value, 0, len(array.Elements)),
	}
	for i, v := range array.Elements {
		result := vm.callFunctionValue(fn, []Value{i, v})
		mappedArray.Elements = append(mappedArray.Elements, result)
	}
	vm.push(mappedArray)
}

func arrayForEach(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.forEach", args, 1)
	fn, ok := args[0].(FunctionValue)
	if !ok {
		vm.runtimeError(ErrorType, "array.forEach expects function, got %s", TypeName(args[0]))
		return
	}
	for i, v := range array.Elements {
		vm.callFunctionValue(fn, []Value{i, v})
	}
	vm.push(true)
}

func arrayFilter(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.filter", args, 1)
	fn, ok := args[0].(FunctionValue)
	if !ok {
		vm.runtimeError(ErrorType, "array.filter expects function, got %s", TypeName(args[0]))
		return
	}
	filteredArray := &ArrayValue{
		Elements: make([]Value, 0, len(array.Elements)),
	}
	for i, v := range array.Elements {
		result := vm.callFunctionValue(fn, []Value{i, v})
		if isTruthy(result) {
			filteredArray.Elements = append(filteredArray.Elements, v)
		}
	}
	vm.push(filteredArray)
}

func arrayClear(vm *VM, array *ArrayValue, args []Value) {
	dontExpectArgs(vm, "array.clear", args)
	array.Elements = array.Elements[:0]
	vm.push(true)
}

func arrayRemove(vm *VM, array *ArrayValue, args []Value) {
	expectArgs(vm, "array.remove", args, 1)

	index := argInt(vm, "array.remove", args, 0)

	if index < 0 || index >= len(array.Elements) {
		vm.fatalError(ErrorIndex, "array.remove index out of bounds: %d", index)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			vm.fatalError(ErrorIndex, "failed to remove element at index %d: %v", index, r)
		}
	}()

	array.Elements = slices.Delete(array.Elements, index, index+1)

	vm.push(true)
}
