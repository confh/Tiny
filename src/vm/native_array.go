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
		"find": {
			Name: "find",
			Args: []StdArg{
				{Name: "callback", Type: "function"},
			},
			Returns:     "any",
			Description: "Returns the first array element for which the callback returns true, or null if none match.",
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
			Returns:     "any",
			Description: "Returns a new array with the results of calling a function on every element.",
		},
		"forEach": {
			Name: "forEach",
			Args: []StdArg{
				{Name: "fn", Type: "function"},
			},
			Returns:     "null",
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
		"reduce": {
			Name: "reduce",
			Args: []StdArg{
				{Name: "callback", Type: "function"},
				{Name: "initialValue", Type: "any", Optional: true},
			},
			Returns:     "any",
			Description: "Reduces the array to a single value by calling a function on each element. An optional initial value can be provided.",
		},
		"sort": {
			Name: "sort",
			Args: []StdArg{
				{Name: "comparator", Type: "function", Optional: true},
			},
			Returns:     "array",
			Description: "Sorts the array in place. An optional comparator function (a, b) => number can be provided.",
		},
		"slice": {
			Name: "slice",
			Args: []StdArg{
				{Name: "start", Type: "number"},
				{Name: "end", Type: "number", Optional: true},
			},
			Returns:     "array",
			Description: "Returns a shallow copy of a portion of the array from start to end (exclusive). Negative indices count from the end.",
		},
		"flat": {
			Name:        "flat",
			Args:        []StdArg{{Name: "depth", Type: "number", Optional: true}},
			Returns:     "array",
			Description: "Flattens nested arrays into a single array. An optional depth specifies how deep to flatten (default 1).",
		},
		"flatMap": {
			Name: "flatMap",
			Args: []StdArg{
				{Name: "fn", Type: "function"},
			},
			Returns:     "array",
			Description: "Maps each element using a function, then flattens the result by one level.",
		},
		"findIndex": {
			Name: "findIndex",
			Args: []StdArg{
				{Name: "callback", Type: "function"},
			},
			Returns:     "number",
			Description: "Returns the index of the first element for which the callback returns true, or -1 if none match.",
		},
	},
}

var arrayMethods map[string]NativeModuleFunc[*ArrayValue]

func init() {
	arrayMethods = map[string]NativeModuleFunc[*ArrayValue]{
		"length":    arrayLength,
		"push":      arrayPush,
		"pop":       arrayPop,
		"get":       arrayGet,
		"set":       arraySet,
		"contains":  arrayContains,
		"find":      arrayFind,
		"join":      arrayJoin,
		"reverse":   arrayReverse,
		"map":       arrayMap,
		"forEach":   arrayForEach,
		"filter":    arrayFilter,
		"clear":     arrayClear,
		"remove":    arrayRemove,
		"reduce":    arrayReduce,
		"sort":      arraySort,
		"slice":     arraySlice,
		"flat":      arrayFlat,
		"flatMap":   arrayFlatMap,
		"findIndex": arrayFindIndex,
	}

	registerNativeType(arrayNativeMetadata)
}

func (vm *VM) callArrayMethod(array *ArrayValue, method string, args []TinyValue) {
	fn, ok := arrayMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown array method: %s", method)
		return
	}
	fn(vm, array, args)
}

func arrayLength(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.length", args, 0)
	vm.push(NewInt(len(array.Elements)))
}

func arrayPush(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.push", args, 1)
	array.Elements = append(array.Elements, args[0])
	vm.push(NewNative(array))
}

func arrayPop(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.pop", args, 0)
	if len(array.Elements) == 0 {
		vm.push(NewNull())
		return
	}
	last := array.Elements[len(array.Elements)-1]
	array.Elements = array.Elements[:len(array.Elements)-1]
	vm.push(last)
}

func arrayGet(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.get", args, 1)
	index := argInt(vm, "array.get", args, 0)
	if index < 0 || index >= len(array.Elements) {
		vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		return
	}
	vm.push(array.Elements[index])
}

func arraySet(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.set", args, 2)
	index := argInt(vm, "array.set", args, 0)
	if index < 0 || index >= len(array.Elements) {
		vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
		return
	}
	array.Elements[index] = args[1]
	vm.push(NewNative(array))
}

func arrayContains(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.contains", args, 1)

	element := args[0]
	vm.push(NewNative(slices.Contains(array.Elements, element)))
}

func arrayFind(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.find", args, 1)

	fn := argFn(vm, "array.find", args, 0)

	found := false
	var value TinyValue

	for i, v := range array.Elements {
		val := vm.callFunctionValue(fn, []TinyValue{NewInt(i), v})
		if isTruthy(val) {
			found = true
			value = v
			break
		}
	}

	if found {
		vm.push(value)
	} else {
		vm.push(NewNull())
	}
}

func arrayJoin(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.join", args, 1)

	separator := argString(vm, "array.join", args, 0)

	var sb strings.Builder
	for i, value := range array.Elements {
		sb.WriteString(valueToString(value))
		if i != len(array.Elements)-1 {
			sb.WriteString(separator)
		}
	}
	vm.push(NewNative(sb.String()))
}

func arrayReverse(vm *VM, array *ArrayValue, args []TinyValue) {
	dontExpectArgs(vm, "array.reverse", args)

	slices.Reverse(array.Elements)
	vm.push(NewNative(array))
}

func arrayMap(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.map", args, 1)

	fn := argFn(vm, "array.map", args, 0)
	mappedArray := &ArrayValue{
		Elements: make([]TinyValue, 0, len(array.Elements)),
	}

	mapArgs := make([]TinyValue, 2)

	for i, v := range array.Elements {
		mapArgs[0] = NewInt(i)
		mapArgs[1] = v

		result := vm.callFunctionValue(fn, mapArgs)
		mappedArray.Elements = append(mappedArray.Elements, result)
	}
	vm.push(NewNative(mappedArray))
}

func arrayForEach(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.forEach", args, 1)
	fn := argFn(vm, "array.forEach", args, 0)

	for i, v := range array.Elements {
		vm.callFunctionValue(fn, []TinyValue{NewInt(i), v})
	}
	vm.push(NewNull())
}

func arrayFilter(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.filter", args, 1)
	fn := argFn(vm, "array.filter", args, 0)

	filteredArray := &ArrayValue{
		Elements: make([]TinyValue, 0, len(array.Elements)),
	}
	for i, v := range array.Elements {
		result := vm.callFunctionValue(fn, []TinyValue{NewInt(i), v})
		if isTruthy(result) {
			filteredArray.Elements = append(filteredArray.Elements, v)
		}
	}
	vm.push(NewNative(filteredArray))
}

func arrayClear(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.clear", args, 0)
	clear(array.Elements)
	array.Elements = array.Elements[:0]
	vm.push(NewNative(true))
}

func arrayRemove(vm *VM, array *ArrayValue, args []TinyValue) {
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

	vm.push(NewNative(true))
}

func arrayReduce(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgsRange(vm, "array.reduce", args, 1, 2)

	fn := argFn(vm, "array.reduce", args, 0)

	hasInitial := len(args) > 1
	startIndex := 0
	var accumulator TinyValue

	if hasInitial {
		accumulator = args[1]
	} else {
		if len(array.Elements) == 0 {
			vm.runtimeError(ErrorRuntime, "array.reduce of empty array with no initial value")
			return
		}
		accumulator = array.Elements[0]
		startIndex = 1
	}

	for i := startIndex; i < len(array.Elements); i++ {
		accumulator = vm.callFunctionValue(fn, []TinyValue{accumulator, array.Elements[i]})
	}

	vm.push(accumulator)
}

func arraySort(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgsRange(vm, "array.sort", args, 0, 1)

	if len(args) > 0 {
		fn := argFn(vm, "array.sort", args, 0)

		slices.SortFunc(array.Elements, func(a, b TinyValue) int {
			result := vm.callFunctionValue(fn, []TinyValue{a, b})
			if result.IsInt {
				return result.AsInt
			}
			if n, ok := result.Value.(float64); ok {
				return int(n)
			}
			return 0
		})
	} else {
		slices.SortFunc(array.Elements, func(a, b TinyValue) int {
			return compareTinyValues(a, b)
		})
	}

	vm.push(NewNative(array))
}

func arraySlice(vm *VM, array *ArrayValue, args []TinyValue) {
	if len(args) < 1 {
		vm.fatalError(ErrorRuntime, "array.slice requires at least 1 argument (start)")
		return
	}

	length := len(array.Elements)
	start := argInt(vm, "array.slice", args, 0)
	end := length

	if len(args) > 1 {
		end = argInt(vm, "array.slice", args, 1)
	}

	if start < 0 {
		start = length + start
		if start < 0 {
			start = 0
		}
	}
	if start > length {
		start = length
	}

	if end < 0 {
		end = length + end
		if end < 0 {
			end = 0
		}
	}
	if end > length {
		end = length
	}

	if start >= end {
		vm.push(NewNative(&ArrayValue{Elements: []TinyValue{}}))
		return
	}

	result := make([]TinyValue, end-start)
	copy(result, array.Elements[start:end])
	vm.push(NewNative(&ArrayValue{Elements: result}))
}

func arrayFlat(vm *VM, array *ArrayValue, args []TinyValue) {
	depth := 1
	if len(args) > 0 {
		depth = argInt(vm, "array.flat", args, 0)
	}

	var result []TinyValue
	var flatten func(elements []TinyValue, d int)
	flatten = func(elements []TinyValue, d int) {
		for _, elem := range elements {
			if d > 0 {
				if arr, ok := elem.Value.(*ArrayValue); ok {
					flatten(arr.Elements, d-1)
					continue
				}
				if arr, ok := elem.Value.(ArrayValue); ok {
					flatten(arr.Elements, d-1)
					continue
				}
			}
			result = append(result, elem)
		}
	}

	flatten(array.Elements, depth)
	vm.push(NewNative(&ArrayValue{Elements: result}))
}

func arrayFlatMap(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.flatMap", args, 1)

	fn := argFn(vm, "array.flatMap", args, 0)
	var result []TinyValue

	for i, v := range array.Elements {
		mapped := vm.callFunctionValue(fn, []TinyValue{NewInt(i), v})
		if arr, ok := mapped.Value.(*ArrayValue); ok {
			result = append(result, arr.Elements...)
		} else if arr, ok := mapped.Value.(ArrayValue); ok {
			result = append(result, arr.Elements...)
		} else {
			result = append(result, mapped)
		}
	}

	vm.push(NewNative(&ArrayValue{Elements: result}))
}

func arrayFindIndex(vm *VM, array *ArrayValue, args []TinyValue) {
	expectArgs(vm, "array.findIndex", args, 1)

	fn := argFn(vm, "array.findIndex", args, 0)

	for i, v := range array.Elements {
		val := vm.callFunctionValue(fn, []TinyValue{v})
		if isTruthy(val) {
			vm.push(NewInt(i))
			return
		}
	}

	vm.push(NewInt(-1))
}

func compareTinyValues(a, b TinyValue) int {
	if a.IsInt && b.IsInt {
		if a.AsInt < b.AsInt {
			return -1
		} else if a.AsInt > b.AsInt {
			return 1
		}
		return 0
	}

	aFloat := toFloat64(a)
	bFloat := toFloat64(b)

	if aFloat < bFloat {
		return -1
	} else if aFloat > bFloat {
		return 1
	}
	return 0
}

func toFloat64(v TinyValue) float64 {
	if v.IsInt {
		return float64(v.AsInt)
	}
	switch val := v.Value.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	default:
		return 0
	}
}
