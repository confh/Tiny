package vm

import (
	"errors"
	"io"

	. "language.com/src/tinyerrors"
)

var fileMethods map[string]NativeModuleFunc[*NativeFileValue]

func init() {
	fileMethods = map[string]NativeModuleFunc[*NativeFileValue]{
		"read":  fileRead,
		"close": fileClose,
	}
}

func (vm *VM) callFileMethod(file *NativeFileValue, method string, args []Value) {
	fn, ok := fileMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown file method: %s", method)
		return
	}
	fn(vm, file, args)
}

func fileRead(vm *VM, file *NativeFileValue, args []Value) {
	expectArgs(vm, "fs.read", args, 1)

	if file.Closed {
		vm.runtimeError(ErrorRuntime, "cannot read closed file")
		return
	}

	size := argInt(vm, "fs.read", args, 0)
	if size <= 0 {
		vm.runtimeError(ErrorRuntime, "read size must be greater than 0")
		return
	}

	buffer := make([]byte, size)
	n, err := file.File.Read(buffer)
	if err != nil && !errors.Is(err, io.EOF) {
		vm.runtimeError(ErrorRuntime, "failed to read file: %v", err)
		return
	}

	vm.push(string(buffer[:n]))
}

func fileClose(vm *VM, file *NativeFileValue, args []Value) {
	expectArgs(vm, "fs.close", args, 0)

	if !file.Closed {
		err := file.File.Close()
		if err != nil {
			vm.runtimeError(ErrorRuntime, "failed to close file: %v", err)
			return
		}
		file.Closed = true
	}

	vm.push(true)
}
