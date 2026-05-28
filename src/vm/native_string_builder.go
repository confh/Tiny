package vm

import (
	. "language.com/src/tinyerrors"
)

var stringBuilderNativeMetadata = NativeTypeInfo{
	Name: "stringBuilder",
	Methods: map[string]StdMethodInfo{
		"writeString": {
			Name: "writeString",
			Args: []StdArg{
				{Name: "str", Type: "string"},
			},
			Returns:     "void",
			Description: "Appends the given string to the builder.",
		},
		"string": {
			Name:        "string",
			Returns:     "string",
			Description: "Returns the accumulated string value from the builder.",
		},
	},
}

var stringBuilderMethods map[string]NativeModuleFunc[*NativeStringBuilderValue]

func init() {
	stringBuilderMethods = map[string]NativeModuleFunc[*NativeStringBuilderValue]{
		"writeString": stringBuilderWriteString,
		"string":      stringBuilderString,
	}
	registerNativeType(stringBuilderNativeMetadata)
}

func (vm *VM) callStringBuilderMethod(sb *NativeStringBuilderValue, method string, args []Value) {
	fn, ok := stringBuilderMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown stringBuilder method: %s", method)
		return
	}
	fn(vm, sb, args)
}

func stringBuilderWriteString(vm *VM, sb *NativeStringBuilderValue, args []Value) {
	expectArgs(vm, "stringBuilder.writeString", args, 1)
	str := argString(vm, "stringBuilder.writeString", args, 0)
	sb.Builder.WriteString(str)
	vm.push(NewUndefined())
}

func stringBuilderString(vm *VM, sb *NativeStringBuilderValue, args []Value) {
	expectArgs(vm, "stringBuilder.string", args, 0)
	vm.push(NewNative(sb.Builder.String()))
}
