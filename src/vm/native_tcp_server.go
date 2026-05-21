package vm

import (
	"net"
	"strconv"

	. "language.com/src/tinyerrors"
)

var tcpNativeMetadata = NativeTypeInfo{
	Name: "tcpServerObject",
	Methods: map[string]StdMethodInfo{
		"start": {
			Name: "start",
			Args: []StdArg{
				{Name: "async", Type: "bool", Optional: true},
			},
			Returns:     "undefined",
			Description: "Reads up to the specified number of bytes from the file.",
		},
		"onConnection": {
			Name: "onConnection",
			Args: []StdArg{
				{Name: "callback", Type: "function"},
			},
			Returns:     "undefined",
			Description: "Reads up to the specified number of bytes from the file.",
		},
	},
}

var tcpMethods map[string]NativeModuleFunc[*NativeTcpServerValue]

func init() {
	tcpMethods = map[string]NativeModuleFunc[*NativeTcpServerValue]{
		"start":        tcpStart,
		"onConnection": tcpOnConnection,
	}
	registerNativeType(tcpNativeMetadata)
}

func handleConn(vm *VM, tcp *NativeTcpServerValue, conn net.Conn) {
	if tcp.ConnectionHandler != nil {
		vm.callFunctionValue(*tcp.ConnectionHandler, []Value{&NativeTcpConnectionValue{
			Connection: conn,
		}})
	}
}

func (vm *VM) callTcpServerMethod(tcp *NativeTcpServerValue, method string, args []Value) {
	fn, ok := tcpMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown tcpServer method: %s", method)
		return
	}
	fn(vm, tcp, args)
}

func tcpOnConnection(vm *VM, tcp *NativeTcpServerValue, args []Value) {
	expectArgs(vm, "tcp.onConnection", args, 1)

	callback := argFn(vm, "tcp.onConnection", args, 0)

	tcp.ConnectionHandler = &callback

	vm.push(UndefinedValue{})
}

func tcpStart(vm *VM, tcp *NativeTcpServerValue, args []Value) {
	expectArgsRange(vm, "tcp.start", args, 0, 1)

	async := false

	if len(args) == 1 {
		async = argBool(vm, "tcp.start", args, 0)
	}

	listener, err := net.Listen("tcp", tcp.Host+":"+strconv.Itoa(tcp.Port))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to start TCP server on %s:%d: %v", tcp.Host, tcp.Port, err)
	}

	tcp.Listener = &listener

	clonedVm := vm.CloneForTask()

	if async {
		go func() {
			for {
				conn, _ := listener.Accept()
				go handleConn(clonedVm, tcp, conn)
			}
		}()
	} else {
		for {
			conn, _ := listener.Accept()
			go handleConn(clonedVm, tcp, conn)
		}
	}

	vm.push(UndefinedValue{})
}
