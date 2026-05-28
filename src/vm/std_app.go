package vm

import . "language.com/src/tinyerrors"

var stdAppMetadata = StdModuleInfo{
	Name: "app",
	Methods: map[string]StdMethodInfo{
		"new": {
			Name: "new",
			Args: []StdArg{
				{Name: "name", Type: "string", Optional: false},
			},
			Returns:     "App",
			Description: "Creates a new app object.",
		},
	},
}

var stdAppMethods map[string]StdModuleFunc

func init() {
	stdAppMethods = map[string]StdModuleFunc{
		"new": stdAppNew,
	}
	registerStdModule(stdAppMetadata)
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

	vm.push(NewNative(&NativeAppValue{
		Name:     name,
		Commands: map[string]FunctionValue{},
	}))
}
