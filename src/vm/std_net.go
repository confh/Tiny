package vm

import (
	. "language.com/src/tinyerrors"
)

var stdNetMetadata = StdModuleInfo{
	Name: "net",
	Methods: map[string]StdMethodInfo{
		"tcpServer": {
			Name: "tcpServer",
			Args: []StdArg{
				{
					Name: "host",
					Type: "string",
				},
				{
					Name: "port",
					Type: "number",
				},
			},
			Returns:     "tcpServerObject",
			Description: "Creates and returns a tcpServerObject.",
		},
	},
}

var stdNetMethods map[string]StdModuleFunc

func init() {
	stdNetMethods = map[string]StdModuleFunc{
		"tcpServer": netTcpServer,
	}

	registerStdModule(stdNetMetadata)
}

func (vm *VM) callStdNet(method string, args []Value) {
	fn, ok := stdNetMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown net function: %s", method)
		return
	}

	fn(vm, args)
}

func netTcpServer(vm *VM, args []Value) {
	expectArgs(vm, "net.tcpServer", args, 2)

	host := argString(vm, "net.tcpServer", args, 0)
	port := argInt(vm, "net.tcpServer", args, 1)

	connectionValue := &NativeTcpServerValue{
		Host:              host,
		Port:              port,
		Listener:          nil,
		ConnectionHandler: nil,
	}

	vm.push(NewNative(connectionValue))
}
