package vm

import (
	"unsafe"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdBuffer(method string, args []Value) {
	switch method {
	case "fromString":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "buffer.fromString expects 1 argument")
		}

		text := asString(args[0], vm)

		vm.push(&BufferValue{
			Bytes: []byte(text),
		})

	case "fromArray":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "buffer.fromArray expects 1 argument")
		}

		array := asArray(args[0], vm)

		bufferValue := &BufferValue{
			Bytes: []byte{},
		}

		for _, val := range array.Elements {
			switch n := val.(type) {
			case int:
				bufferValue.Bytes = append(bufferValue.Bytes, byte(n))
			case int64:
				bufferValue.Bytes = append(bufferValue.Bytes, byte(n))
			case float64:
				bufferValue.Bytes = append(bufferValue.Bytes, byte(n))
			default:
				vm.runtimeError(ErrorRuntime, "buffer.fromArray expects array of numbers")
			}
		}

		vm.push(bufferValue)

	case "alloc":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "buffer.alloc expects 2 arguments")
		}
		totalElements := asInt(args[0])
		defaultValue := asFloat64(args[1])

		data := make([]float64, totalElements)
		for i := range data {
			data[i] = defaultValue
		}

		vm.push(&BufferValue{
			Bytes: unsafe.Slice((*byte)(unsafe.Pointer(&data[0])), len(data)*8),
		})
	default:
		vm.runtimeError(ErrorName, "unknown buffer function: %s", method)
	}
}
