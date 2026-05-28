package vm

import (
	. "language.com/src/tinyerrors"
)

var stdErrorMetadata = StdModuleInfo{
	Name: "error",
	Methods: map[string]StdMethodInfo{
		"new": {
			Name:        "new",
			Args:        []StdArg{{Name: "kind", Type: "string", Optional: false}, {Name: "message", Type: "string", Optional: false}},
			Returns:     "Error",
			Description: "Creates a new error object with kind and message.",
		},
	},
}

var stdErrorMethods map[string]StdModuleFunc

func init() {
	stdErrorMethods = map[string]StdModuleFunc{
		"new": stdErrorNew,
	}
	registerStdModule(stdErrorMetadata)
}

func (vm *VM) callStdError(method string, args []Value) {
	fn, ok := stdErrorMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown error function: %s", method)
		return
	}
	fn(vm, args)
}

func stdErrorNew(vm *VM, args []Value) {
	expectArgs(vm, "error.new", args, 2)
	kind := argString(vm, "error.new", args, 0)
	message := argString(vm, "error.new", args, 1)
	vm.push(NewNative(ErrorValue{
		Kind:    kind,
		Message: message,
	}))
}
