package vm

import (
	"github.com/tetratelabs/wazero/api"
	. "language.com/src/tinyerrors"
)

var wasmNativeMetadata = NativeTypeInfo{
	Name: "wasm",
}

var wasmMethods map[string]NativeModuleFunc[*NativeWasmModuleValue]

func init() {
	wasmMethods = map[string]NativeModuleFunc[*NativeWasmModuleValue]{
		"call":        wasmCall,
		"readMemory":  wasmReadMemory,
		"writeMemory": wasmWriteMemory,
	}
}

func (v *NativeWasmModuleValue) TinyTypeName() string {
	return "wasm.Module"
}

func (vm *VM) callNativeWasmMethod(wasm *NativeWasmModuleValue, method string, args []TinyValue) {
	fn, ok := wasmMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown wasm method: %s", method)
		return
	}
	fn(vm, wasm, args)
}

func wasmCall(vm *VM, wasm *NativeWasmModuleValue, args []TinyValue) {
	expectArgsRange(vm, "wasm.Module.call", args, 1, 2)
	funcName := argString(vm, "wasm.Module.call", args, 0)

	var callArgs []TinyValue
	if len(args) > 1 && !isNullish(args[1]) {
		callArgs = asArray(args[1], vm).Elements
	}

	exportedFn := wasm.Module.ExportedFunction(funcName)
	if exportedFn == nil {
		vm.runtimeError(ErrorName, "unknown exported function: %s", funcName)
		return
	}

	paramTypes := exportedFn.Definition().ParamTypes()
	resultTypes := exportedFn.Definition().ResultTypes()

	if len(callArgs) < len(paramTypes) {
		vm.runtimeError(ErrorRuntime, "wasm function %s expects %d arguments, got %d", funcName, len(paramTypes), len(callArgs))
		return
	}

	wasmStack := make([]uint64, len(paramTypes))
	for i, pType := range paramTypes {
		wasmStack[i] = tinyValueToWasmStackValue(callArgs[i], pType)
	}

	err := exportedFn.CallWithStack(wasm.Ctx, wasmStack)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while calling wasm function %s: %s", funcName, err)
		return
	}

	if len(resultTypes) > 0 {
		resVal := wasmStackValueToTinyValue(wasmStack[0], resultTypes[0])
		vm.push(resVal)
	} else {
		vm.push(NewNull())
	}
}

func wasmReadMemory(vm *VM, wasm *NativeWasmModuleValue, args []TinyValue) {
	expectArgs(vm, "wasm.Module.readMemory", args, 2)
	offset := argInt(vm, "wasm.Module.readMemory", args, 0)
	length := argInt(vm, "wasm.Module.readMemory", args, 1)

	mem := wasm.Module.Memory()
	if mem == nil {
		vm.runtimeError(ErrorRuntime, "wasm module has no memory section")
		return
	}

	bytes, ok := mem.Read(uint32(offset), uint32(length))
	if !ok {
		vm.runtimeError(ErrorRuntime, "out of bounds memory read: offset %d, length %d", offset, length)
		return
	}

	copied := make([]byte, len(bytes))
	copy(copied, bytes)
	vm.push(NewNative(&BufferValue{Bytes: copied}))
}

func wasmWriteMemory(vm *VM, wasm *NativeWasmModuleValue, args []TinyValue) {
	expectArgs(vm, "wasm.Module.writeMemory", args, 2)
	offset := argInt(vm, "wasm.Module.writeMemory", args, 0)
	buf := argBuffer(vm, "wasm.Module.writeMemory", args, 1)

	mem := wasm.Module.Memory()
	if mem == nil {
		vm.runtimeError(ErrorRuntime, "wasm module has no memory section")
		return
	}

	ok := mem.Write(uint32(offset), buf.Bytes)
	if !ok {
		vm.runtimeError(ErrorRuntime, "out of bounds memory write: offset %d, length %d", offset, len(buf.Bytes))
		return
	}

	vm.push(NewNull())
}

func wasmStackValueToTinyValue(val uint64, t api.ValueType) TinyValue {
	switch t {
	case api.ValueTypeI32:
		return NewInt(int(int32(val)))
	case api.ValueTypeI64:
		return NewInt(int(int64(val)))
	case api.ValueTypeF32:
		return NewNative(float64(api.DecodeF32(val)))
	case api.ValueTypeF64:
		return NewNative(api.DecodeF64(val))
	default:
		return NewNull()
	}
}

func tinyValueToWasmStackValue(val TinyValue, t api.ValueType) uint64 {
	switch t {
	case api.ValueTypeI32:
		if val.IsInt {
			return uint64(uint32(val.AsInt))
		}
		if f, ok := val.Value.(float64); ok {
			return uint64(uint32(f))
		}
		return 0
	case api.ValueTypeI64:
		if val.IsInt {
			return uint64(val.AsInt)
		}
		if f, ok := val.Value.(float64); ok {
			return uint64(f)
		}
		return 0
	case api.ValueTypeF32:
		if f, ok := val.Value.(float64); ok {
			return uint64(api.EncodeF32(float32(f)))
		}
		if val.IsInt {
			return uint64(api.EncodeF32(float32(val.AsInt)))
		}
		return 0
	case api.ValueTypeF64:
		if f, ok := val.Value.(float64); ok {
			return api.EncodeF64(f)
		}
		if val.IsInt {
			return api.EncodeF64(float64(val.AsInt))
		}
		return 0
	default:
		return 0
	}
}
