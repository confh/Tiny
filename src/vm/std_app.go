package vm

import . "language.com/src/tinyerrors"

func (v *NativeAppValue) TinyTypeName() string {
	return "app.App"
}

var stdAppMethods map[string]StdModuleFunc

func init() {
	stdAppMethods = map[string]StdModuleFunc{
		"new": stdAppNew,
	}
}

func (vm *VM) callStdApp(method string, args []TinyValue) {
	fn, ok := stdAppMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown app function: %s", method)
		return
	}
	fn(vm, args)
}

func stdAppNew(vm *VM, args []TinyValue) {
	expectArgs(vm, "app.new", args, 1)

	name := argString(vm, "app.new", args, 0)

	vm.push(NewNative(&NativeAppValue{
		Name:     name,
		Commands: map[string]FunctionValue{},
	}))
}
