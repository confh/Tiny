package vm

import (
	"encoding/hex"

	. "language.com/src/tinyerrors"
)

var bufferNativeMetadata = NativeTypeInfo{
	Name: "buffer",
	Methods: map[string]StdMethodInfo{
		"toHex": {
			Name:        "toHex",
			Returns:     "string",
			Description: "Returns the buffer as a hexadecimal string.",
		},
		"length": {
			Name:        "length",
			Returns:     "number",
			Description: "Returns the length of the buffer.",
		},
		"getU8": {
			Name: "getU8",
			Args: []StdArg{
				{Name: "offset", Type: "number"},
			},
			Returns:     "number",
			Description: "Gets the unsigned 8-bit integer at the specified offset.",
		},
		"setU8": {
			Name: "setU8",
			Args: []StdArg{
				{Name: "offset", Type: "number"},
				{Name: "value", Type: "number"},
			},
			Returns:     "bool",
			Description: "Sets the unsigned 8-bit integer at the specified offset.",
		},
	},
}

var bufferMethods map[string]NativeModuleFunc[*BufferValue]

func init() {
	bufferMethods = map[string]NativeModuleFunc[*BufferValue]{
		"toHex":  bufferToHex,
		"length": bufferLength,
		"getU8":  bufferGetU8,
		"setU8":  bufferSetU8,
	}

	registerNativeType(bufferNativeMetadata)
}

func (vm *VM) callBufferMethod(buffer *BufferValue, method string, args []Value) {
	fn, ok := bufferMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown buffer method: %s", method)
		return
	}

	fn(vm, buffer, args)
}

func bufferToHex(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.toHex", args, 0)

	vm.push(NewNative(hex.EncodeToString(buffer.Bytes)))
}

func bufferLength(vm *VM, buffer *BufferValue, args []Value) {
	expectArgs(vm, "buffer.length", args, 0)

	vm.push(NewInt(len(buffer.Bytes)))
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

	vm.push(NewInt(int(buffer.Bytes[offset])))
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

	vm.push(NewNative(true))
}
