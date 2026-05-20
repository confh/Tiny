package vm

import (
	. "language.com/src/tinyerrors"
)

var stringBuilderMethods map[string]NativeModuleFunc[*NativeStringBuilderValue]

func init() {
	stringBuilderMethods = map[string]NativeModuleFunc[*NativeStringBuilderValue]{
		"writeString": stringBuilderWriteString,
		"string":      stringBuilderString,
	}
}

func (vm *VM) callTextBuilderMethod(sb *NativeStringBuilderValue, method string, args []Value) {
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
	vm.push(UndefinedValue{})
}

func stringBuilderString(vm *VM, sb *NativeStringBuilderValue, args []Value) {
	expectArgs(vm, "stringBuilder.string", args, 0)

	vm.push(sb.Builder.String())
}
