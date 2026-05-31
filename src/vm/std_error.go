package vm

import (
	. "language.com/src/tinyerrors"
)

var stdErrorMetadata = StdModuleInfo{
	Name: "error",
}

var stdErrorMethods map[string]StdModuleFunc

func init() {
	stdErrorMethods = map[string]StdModuleFunc{
		"new": stdErrorNew,
	}
	registerStdModule(stdErrorMetadata)
}

func (vm *VM) callStdError(method string, args []TinyValue) {
	fn, ok := stdErrorMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown error function: %s", method)
		return
	}
	fn(vm, args)
}

func stdErrorNew(vm *VM, args []TinyValue) {
	expectArgs(vm, "error.new", args, 2)
	kind := argString(vm, "error.new", args, 0)
	message := argString(vm, "error.new", args, 1)
	vm.push(NewNative(ErrorValue{
		Kind:    kind,
		Message: message,
	}))
}
