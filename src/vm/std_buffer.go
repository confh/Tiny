package vm

import (
	"unsafe"

	. "language.com/src/tinyerrors"
)

var stdBufferMethods = map[string]StdModuleFunc{
	"fromString": bufferFromString,
	"fromArray":  bufferFromArray,
	"alloc":      bufferAlloc,
}

func (vm *VM) callStdBuffer(method string, args []Value) {
	fn, ok := stdBufferMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown buffer function: %s", method)
		return
	}

	fn(vm, args)
}

func bufferFromString(vm *VM, args []Value) {
	expectArgs(vm, "buffer.fromString", args, 1)

	text := argString(vm, "buffer.fromString", args, 0)

	vm.push(&BufferValue{
		Bytes: []byte(text),
	})
}

func bufferFromArray(vm *VM, args []Value) {
	expectArgs(vm, "buffer.fromArray", args, 1)

	array := argArray(vm, "buffer.fromArray", args, 0)

	floats := make([]float64, len(array.Elements))
	for i, val := range array.Elements {
		floats[i] = asFloat64(val)
	}

	var byteSlice []byte
	if len(floats) > 0 {
		byteSlice = unsafe.Slice((*byte)(unsafe.Pointer(&floats[0])), len(floats)*8)
	}

	vm.push(&BufferValue{
		Bytes: byteSlice,
	})
}

func bufferAlloc(vm *VM, args []Value) {
	expectArgs(vm, "buffer.alloc", args, 2)

	totalElements := argInt(vm, "buffer.alloc", args, 0)
	defaultValue := argFloat64(vm, "buffer.alloc", args, 1)

	data := make([]float64, totalElements)
	for i := range data {
		data[i] = defaultValue
	}

	vm.push(&BufferValue{
		Bytes: unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*8),
	})
}
