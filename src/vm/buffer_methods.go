package vm

import (
	"encoding/hex"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callBufferMethod(buffer *BufferValue, method string, args []Value) {
	switch method {
	case "toString":
		vm.push(string(buffer.Bytes))

	case "toHex":
		vm.push(hex.EncodeToString(buffer.Bytes))

	case "length":
		vm.push(len(buffer.Bytes))

	case "getU8":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "buffer.getU8 expects 1 argument")
		}

		offset := asInt(args[0])

		vm.push(buffer.Bytes[offset])

	case "setU8":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "array.setU8 expects 2 argument")
		}

		offset := asInt(args[0])
		value := args[1]
		valueIsNumber := isNumber(value)
		if !valueIsNumber {
			vm.runtimeError(ErrorType, "expected number, got %s", typeName(value))
		}
		switch n := value.(type) {
		case int:
			buffer.Bytes[offset] = byte(n)
		case int64:
			buffer.Bytes[offset] = byte(n)
		case float64:
			buffer.Bytes[offset] = byte(n)
		}

		vm.push(true)

	default:
		vm.runtimeError(ErrorName, "unknown buffer method: %s", method)
	}
}
