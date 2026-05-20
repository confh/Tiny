package vm

import (
	. "language.com/src/tinyerrors"
)

var stdArrayMetadata = StdModuleInfo{
	Name: "array",
	Methods: map[string]StdMethodInfo{
		"range": {
			Name: "range",
			Args: []StdArg{
				{Name: "min", Type: "int", Optional: false},
				{Name: "max", Type: "int", Optional: false},
			},
			Returns:     "Array",
			Description: "Creates an array containing all integers from min to max (inclusive).",
		},
		"isArray": {
			Name: "isArray",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: false},
			},
			Returns:     "bool",
			Description: "Returns true if value is an array.",
		},
		"from": {
			Name: "from",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: false},
			},
			Returns:     "Array",
			Description: "Converts a string or array-like value into an Array.",
		},
	},
}

var stdArrayMethods = map[string]StdModuleFunc{
	"range":   stdArrayRange,
	"isArray": stdArrayIsArray,
	"from":    stdArrayFrom,
}

func init() {
	registerStdModule(stdArrayMetadata)
}

func (vm *VM) callStdArray(method string, args []Value) {
	fn, ok := stdArrayMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown array function: %s", method)
		return
	}
	fn(vm, args)
}

func stdArrayRange(vm *VM, args []Value) {
	expectArgs(vm, "array.range", args, 2)

	min := argInt(vm, "array.range", args, 0)
	max := argInt(vm, "array.range", args, 1)

	capacity := 0
	if max >= min {
		capacity = max - min + 1
	}
	array := &ArrayValue{
		Elements: make([]Value, 0, capacity),
	}

	for i := min; i <= max; i++ {
		array.Elements = append(array.Elements, i)
	}

	vm.push(array)
}

func stdArrayIsArray(vm *VM, args []Value) {
	expectArgs(vm, "array.isArray", args, 1)

	_, ok := args[0].(*ArrayValue)
	vm.push(ok)
}

func stdArrayFrom(vm *VM, args []Value) {
	expectArgs(vm, "array.from", args, 1)

	switch v := args[0].(type) {
	case string:
		strArr := make([]Value, 0, len(v))
		for _, r := range v {
			strArr = append(strArr, string(r))
		}
		vm.push(&ArrayValue{Elements: strArr})

	case *ArrayValue:
		dst := make([]Value, len(v.Elements))
		copy(dst, v.Elements)
		vm.push(&ArrayValue{Elements: dst})

	default:
		vm.runtimeError(ErrorType, "type %s cannot be turned into an array", typeName(v))
	}
}
