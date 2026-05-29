package vm

import . "language.com/src/tinyerrors"

var appNativeMetadata = NativeTypeInfo{
	Name: "app",
	Methods: map[string]StdMethodInfo{
		"command": {
			Name:        "command",
			Args:        []StdArg{{Name: "name", Type: "string"}, {Name: "callback", Type: "function"}},
			Returns:     "App",
			Description: "Registers a command with the given name and function.",
		},
		"run": {
			Name:        "run",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Runs the app with command-line arguments.",
		},
	},
}

var appMethods map[string]NativeModuleFunc[*NativeAppValue]

func init() {
	appMethods = map[string]NativeModuleFunc[*NativeAppValue]{
		"command": appCommand,
		"run":     appRun,
	}
	registerNativeType(appNativeMetadata)
}

func (vm *VM) callNativeAppMethod(app *NativeAppValue, method string, args []Value) {
	fn, ok := appMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown app method: %s", method)
		return
	}
	fn(vm, app, args)
}

func appCommand(vm *VM, app *NativeAppValue, args []Value) {
	expectArgs(vm, "app.command", args, 2)
	name := argString(vm, "app.command", args, 0)
	fn := argFn(vm, "app.command", args, 1)

	app.Commands[name] = fn
	vm.push(NewNative(app))
}

func appRun(vm *VM, app *NativeAppValue, args []Value) {
	expectArgs(vm, "app.run", args, 0)
	vm.runNativeApp(app)
	vm.push(NewUndefined())
}
