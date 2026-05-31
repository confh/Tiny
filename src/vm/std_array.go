package vm

import (
	. "language.com/src/tinyerrors"
)

var stdArrayMetadata = StdModuleInfo{
	Name: "array",
}

var stdArrayMethods map[string]StdModuleFunc

func init() {
	stdArrayMethods = map[string]StdModuleFunc{
		"range": stdArrayRange,
		"from":  stdArrayFrom,
	}
	registerStdModule(stdArrayMetadata)
}

func (vm *VM) callStdArray(method string, args []TinyValue) {
	fn, ok := stdArrayMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown array function: %s", method)
		return
	}
	fn(vm, args)
}

func stdArrayRange(vm *VM, args []TinyValue) {
	expectArgs(vm, "array.range", args, 2)

	min := argInt(vm, "array.range", args, 0)
	max := argInt(vm, "array.range", args, 1)

	capacity := 0
	if max >= min {
		capacity = max - min + 1
	}
	array := &ArrayValue{
		Elements: make([]TinyValue, capacity),
	}

	for i := 0; i < capacity; i++ {
		array.Elements[i] = NewInt(min + i)
	}

	vm.push(NewNative(array))
}

func stdArrayFrom(vm *VM, args []TinyValue) {
	expectArgs(vm, "array.from", args, 1)

	switch v := args[0].Value.(type) {
	case string:
		strArr := make([]TinyValue, 0, len(v))
		for _, r := range v {
			strArr = append(strArr, NewNative(string(r)))
		}
		vm.push(NewNative(&ArrayValue{Elements: strArr}))

	case *ArrayValue:
		dst := make([]TinyValue, len(v.Elements))
		copy(dst, v.Elements)
		vm.push(NewNative(&ArrayValue{Elements: dst}))

	default:
		vm.runtimeError(ErrorType, "type %s cannot be turned into an array", TypeName(ToValue(v)))
	}
}
