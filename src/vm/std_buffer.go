package vm

import . "language.com/src/tinyerrors"

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
	default:
		vm.runtimeError(ErrorName, "unknown buffer function: %s", method)
	}
}
