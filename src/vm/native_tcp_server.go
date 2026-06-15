package vm

import (
	"fmt"
	"net"
	"strconv"

	. "language.com/src/tinyerrors"
)

var tcpMethods map[string]NativeModuleFunc[*NativeTcpServerValue]

func init() {
	tcpMethods = map[string]NativeModuleFunc[*NativeTcpServerValue]{
		"start":        tcpStart,
		"onConnection": tcpOnConnection,
	}
}

func handleConn(worker *VM, tcp *NativeTcpServerValue, conn net.Conn) {
	defer conn.Close()

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("[tcp handler panic] %v\n", r)
		}
	}()

	if tcp.ConnectionHandler == nil {
		return
	}

	worker.callFunctionValue(*tcp.ConnectionHandler, []TinyValue{
		NewNative(&NativeTcpConnectionValue{
			Connection: conn,
		}),
	})
}

func (vm *VM) callTcpServerMethod(tcp *NativeTcpServerValue, method string, args []TinyValue) {
	fn, ok := tcpMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown tcpServer method: %s", method)
		return
	}
	fn(vm, tcp, args)
}

func tcpOnConnection(vm *VM, tcp *NativeTcpServerValue, args []TinyValue) {
	expectArgs(vm, "tcp.onConnection", args, 1)

	callback := argFn(vm, "tcp.onConnection", args, 0)

	tcp.ConnectionHandler = &callback

	vm.push(NewNull())
}

func tcpStart(vm *VM, tcp *NativeTcpServerValue, args []TinyValue) {
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

	if tcp.Workers == nil {
		tcp.Workers = NewVMPool(16, 8, func() *VM {
			return vm.CloneForTask()
		})
	}

	acceptLoop := func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				continue
			}

			worker := tcp.Workers.Get()

			go func() {
				defer tcp.Workers.Put(worker)
				handleConn(worker, tcp, conn)
			}()
		}
	}

	if async {
		go acceptLoop()
		vm.push(NewNative(true))
		return
	}

	acceptLoop()

	vm.push(NewNull())
}
