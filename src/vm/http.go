package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdHttp(method string, args []Value) {
	switch method {
	case "server":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "http.server expects 1 argument")
		}

		port := asInt(args[0])

		server := &NativeServerValue{
			Port:       port,
			GetRoutes:  map[string]Value{},
			PostRoutes: map[string]Value{},
		}

		vm.push(server)

	default:
		vm.runtimeError(ErrorName, "unknown http function: %s", method)
	}
}
