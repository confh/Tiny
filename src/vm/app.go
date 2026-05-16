package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callStdApp(method string, args []Value) {
	switch method {
	case "new":
		if len(args) != 1 {
			LangError(ErrorRuntime, "app.new expects 1 argument")
		}

		name := asString(args[0])

		vm.push(&NativeAppValue{
			Name:     name,
			Commands: map[string]FunctionValue{},
		})

	default:
		LangError(ErrorName, "unknown app function: %s", method)
	}
}
