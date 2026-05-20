package vm

import . "language.com/src/tinyerrors"

var stdAppMethods = map[string]StdModuleFunc{
	"new": stdAppNew,
}

func (vm *VM) callStdApp(method string, args []Value) {
	fn, ok := stdAppMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown app function: %s", method)
		return
	}
	fn(vm, args)
}

func stdAppNew(vm *VM, args []Value) {
	expectArgs(vm, "app.new", args, 1)

	name := argString(vm, "app.new", args, 0)

	vm.push(&NativeAppValue{
		Name:     name,
		Commands: map[string]FunctionValue{},
	})
}
