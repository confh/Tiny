package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callStdApp(method string, args []Value) {
	switch method {
	case "new":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "app.new expects 1 argument")
		}

		name := asString(args[0], vm)

		vm.push(&NativeAppValue{
			Name:     name,
			Commands: map[string]FunctionValue{},
		})

	default:
		vm.runtimeError(ErrorName, "unknown app function: %s", method)
	}
}
