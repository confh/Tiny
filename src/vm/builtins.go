package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	switch object {
	case "Plugin":
		vm.callPluginModule(method, argCount)

	default:
		vm.runtimeError(ErrorName, "unknown builtin module: %s", object)
	}
}
