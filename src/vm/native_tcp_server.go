package vm

import (
	"fmt"
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
	defer conn.Close()

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[tcp handler panic] %v\n", r)
		}
	}()

	if tcp.ConnectionHandler == nil {
		return
	}

	vm.callFunctionValue(*tcp.ConnectionHandler, []Value{
		&NativeTcpConnectionValue{
			Connection: conn,
		},
	})
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

	acceptLoop := func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}

			connVm := vm.CloneForTask()

			go handleConn(connVm, tcp, conn)
		}
	}

	if async {
		go acceptLoop()
		vm.push(true)
		return
	}

	acceptLoop()

	vm.push(UndefinedValue{})
}
