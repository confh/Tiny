package vm

import (
	"slices"
	"strings"

	. "language.com/src/tinyerrors"
)

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
	}
}

func (vm *VM) callArrayMethod(array *ArrayValue, method string, args []Value) {
	fn, ok := arrayMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown array method: %s", method)
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
		vm.runtimeError(ErrorType, "array.map expects function, got %s", typeName(args[0]))
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
		vm.runtimeError(ErrorType, "array.forEach expects function, got %s", typeName(args[0]))
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
		vm.runtimeError(ErrorType, "array.filter expects function, got %s", typeName(args[0]))
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
	expectArgs(vm, "array.clear", args, 0)
	array.Elements = []Value{}
	vm.push(true)
}
