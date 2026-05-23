package vm

import (
	"bufio"
	"io"

	. "language.com/src/tinyerrors"
)

var tcpConnMethods map[string]NativeModuleFunc[*NativeTcpConnectionValue]

func init() {
	tcpConnMethods = map[string]NativeModuleFunc[*NativeTcpConnectionValue]{
		"readLine": tcpConnReadLine,
		"read":     tcpConnRead,
		"write":    tcpConnWrite,
		"close":    tcpConnClose,
		"address":  tcpConnAddress,
	}
}

func (vm *VM) callTcpConnMethod(tcp *NativeTcpConnectionValue, method string, args []Value) {
	fn, ok := tcpConnMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown tcpConnection method: %s", method)
		return
	}
	fn(vm, tcp, args)
}

func tcpConnReadLine(vm *VM, tcp *NativeTcpConnectionValue, args []Value) {
	dontExpectArgs(vm, "tcp.readLine", args)

	var reader *bufio.Reader

	if tcp.Reader != nil {
		reader = tcp.Reader
	} else {
		reader = bufio.NewReader(tcp.Connection)
		tcp.Reader = reader
	}

	expectArgs(vm, "conn.readLine", args, 0)

	line, err := reader.ReadString('\n')
	if err != nil {
		if err == io.EOF {
			if line == "" {
				vm.push(UndefinedValue{})
				return
			}

			vm.push(line)
			return
		}

		vm.runtimeError(ErrorRuntime, "error reading tcp connection: %v", err)
		return
	}

	vm.push(line)

}

func tcpConnRead(vm *VM, tcp *NativeTcpConnectionValue, args []Value) {
	expectArgs(vm, "tcp.read", args, 1)

	size := argInt(vm, "tcp.read", args, 0)

	buf := make([]byte, size)

	_, err := io.ReadFull(tcp.Connection, buf)
	if err != nil {
		if err == io.ErrUnexpectedEOF {
			vm.runtimeError(ErrorRuntime, "connection closed early, only read partial data: %v", err)
		} else {
			vm.runtimeError(ErrorRuntime, "error reading tcp connection: %v", err)
		}
		return
	}

	vm.push(string(buf))
}

func tcpConnAddress(vm *VM, tcp *NativeTcpConnectionValue, args []Value) {
	dontExpectArgs(vm, "tcp.write", args)

	vm.push(tcp.Connection.RemoteAddr().String())
}

func tcpConnWrite(vm *VM, tcp *NativeTcpConnectionValue, args []Value) {
	expectArgs(vm, "tcp.write", args, 1)

	switch v := args[0].(type) {
	case string:
		tcp.Connection.Write([]byte(v))

	case *BufferValue:
		tcp.Connection.Write(v.Bytes)

	default:
		vm.runtimeError(ErrorRuntime, "tcp.write expects string or buffer, got %s.", TypeName(v))
	}

	vm.push(UndefinedValue{})
}

func tcpConnClose(vm *VM, tcp *NativeTcpConnectionValue, args []Value) {
	dontExpectArgs(vm, "tcp.close", args)

	tcp.Connection.Close()

	vm.push(UndefinedValue{})
}
