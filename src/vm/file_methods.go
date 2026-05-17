package vm

import (
	"errors"
	"io"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callFileMethod(file *NativeFileValue, method string, args []Value) {
	switch method {
	case "read":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "fs.read expects 1 argument")
		}

		if file.Closed {
			vm.runtimeError(ErrorRuntime, "cannot read closed file")
		}

		size := asInt(args[0])

		if size <= 0 {
			vm.runtimeError(ErrorRuntime, "read size must be greater than 0")
		}

		buffer := make([]byte, size)

		n, err := file.File.Read(buffer)
		if err != nil && !errors.Is(err, io.EOF) {
			vm.runtimeError(ErrorRuntime, "failed to read file: %v", err)
		}

		vm.push(string(buffer[:n]))

	case "close":
		if !file.Closed {
			err := file.File.Close()
			if err != nil {
				vm.runtimeError(ErrorRuntime, "failed to close file: %v", err)
			}

			file.Closed = true
		}

		vm.push(true)

	default:
		vm.runtimeError(ErrorName, "unknown file method: %s", method)
	}
}
