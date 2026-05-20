package vm

import (
	"encoding/hex"

	. "language.com/src/tinyerrors"
)

var bufferMethods map[string]NativeModuleFunc[*BufferValue]

func init() {
	bufferMethods = map[string]NativeModuleFunc[*BufferValue]{
		"toString": bufferToString,
		"toHex":    bufferToHex,
		"length":   bufferLength,
		"getU8":    bufferGetU8,
		"setU8":    bufferSetU8,
	}
}

func (vm *VM) callBufferMethod(buffer *BufferValue, method string, args []Value) {
	fn, ok := bufferMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown buffer method: %s", method)
		return
	}

	fn(vm, buffer, args)
}

func bufferToString(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.toString", args, 0)

	vm.push(string(buffer.Bytes))
}

func bufferToHex(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.toHex", args, 0)

	vm.push(hex.EncodeToString(buffer.Bytes))
}

func bufferLength(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.length", args, 0)

	vm.push(len(buffer.Bytes))
}

func bufferGetU8(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.getU8", args, 1)

	offset := argInt(vm, "buffer.getU8", args, 0)

	if offset < 0 || offset >= len(buffer.Bytes) {
		vm.runtimeError(
			ErrorRuntime,
			"buffer.getU8 offset out of range: %d, buffer length is %d",
			offset,
			len(buffer.Bytes),
		)
		return
	}

	vm.push(int(buffer.Bytes[offset]))
}

func bufferSetU8(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.setU8", args, 2)

	offset := argInt(vm, "buffer.setU8", args, 0)
	value := argInt(vm, "buffer.setU8", args, 1)

	if offset < 0 || offset >= len(buffer.Bytes) {
		vm.runtimeError(
			ErrorRuntime,
			"buffer.setU8 offset out of range: %d, buffer length is %d",
			offset,
			len(buffer.Bytes),
		)
		return
	}

	if value < 0 || value > 255 {
		vm.runtimeError(
			ErrorRuntime,
			"buffer.setU8 value must be between 0 and 255, got %d",
			value,
		)
		return
	}

	buffer.Bytes[offset] = byte(value)

	vm.push(true)
}
