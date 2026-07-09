package vm

import (
	"fmt"
	"net"

	. "language.com/src/tinyerrors"
)

func (v *NativeTcpConnectionValue) TinyTypeName() string {
	return "net.TcpConnection"
}

func (v *NativeTcpServerValue) TinyTypeName() string {
	return "net.TcpServer"
}

var stdNetMetadata = StdModuleInfo{
	Name: "net",
}

var stdNetMethods map[string]StdModuleFunc

func init() {
	stdNetMethods = map[string]StdModuleFunc{
		"tcpServer": netTcpServer,
		"tcpClient": netTcpClient,
	}

	registerStdModule(stdNetMetadata)
}

func (vm *VM) callStdNet(method string, args []TinyValue) {
	fn, ok := stdNetMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown net function: %s", method)
		return
	}

	fn(vm, args)
}

func netTcpServer(vm *VM, args []TinyValue) {
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

func netTcpClient(vm *VM, args []TinyValue) {
	expectArgs(vm, "net.tcpClient", args, 2)

	host := argString(vm, "net.tcpClient", args, 0)
	port := argInt(vm, "net.tcpClient", args, 1)

	address := fmt.Sprintf("%s:%d", host, port)
	conn, err := net.Dial("tcp", address)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to connect to %s: %v", address, err)
		return
	}

	connectionValue := &NativeTcpConnectionValue{
		Connection: conn,
		Reader:     nil,
	}

	vm.push(NewNative(connectionValue))
}
