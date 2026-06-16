package vm

import (
	. "language.com/src/tinyerrors"
)

var stringBuilderNativeMetadata = NativeTypeInfo{
	Name: "stringBuilder",
}

var stringBuilderMethods map[string]NativeModuleFunc[*NativeStringBuilderValue]

func init() {
	stringBuilderMethods = map[string]NativeModuleFunc[*NativeStringBuilderValue]{
		"writeString": stringBuilderWriteString,
		"stringify":   stringBuilderString,
	}
}

func (vm *VM) callStringBuilderMethod(sb *NativeStringBuilderValue, method string, args []TinyValue) {
	fn, ok := stringBuilderMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown stringBuilder method: %s", method)
		return
	}
	fn(vm, sb, args)
}

func stringBuilderWriteString(vm *VM, sb *NativeStringBuilderValue, args []TinyValue) {
	expectArgs(vm, "stringBuilder.writeString", args, 1)
	str := argString(vm, "stringBuilder.writeString", args, 0)
	sb.Builder.WriteString(str)
	vm.push(NewNull())
}

func stringBuilderString(vm *VM, sb *NativeStringBuilderValue, args []TinyValue) {
	expectArgs(vm, "stringBuilder.string", args, 0)
	vm.push(NewNative(sb.Builder.String()))
}
