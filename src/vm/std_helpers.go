package vm

import . "language.com/src/tinyerrors"

type StdModuleFunc func(vm *VM, args []TinyValue)

type NativeModuleFunc[T any] func(vm *VM, value T, args []TinyValue)

func dontExpectArgs(vm *VM, fnName string, args []TinyValue) {
	if len(args) != 0 {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects %d argument(s), got %d",
			fnName,
			0,
			len(args),
		)
	}
}

func expectArgs(vm *VM, fnName string, args []TinyValue, count int) {
	if len(args) != count {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects %d argument(s), got %d",
			fnName,
			count,
			len(args),
		)
	}
}

func expectArgsRange(vm *VM, fnName string, args []TinyValue, min int, max int) {
	if len(args) < min || len(args) > max {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects %d to %d argument(s), got %d",
			fnName,
			min,
			max,
			len(args),
		)
	}
}

func expectArgsMin(vm *VM, fnName string, args []TinyValue, min int) {
	if len(args) < min {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects at least %d argument(s), got %d",
			fnName,
			min,
			len(args),
		)
	}
}

func argString(vm *VM, fnName string, args []TinyValue, index int) string {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	str, ok := args[index].Value.(string)
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected string, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return str
}

func argFn(vm *VM, fnName string, args []TinyValue, index int) FunctionValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	fn, ok := args[index].Value.(FunctionValue)
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected function, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return fn
}

func argBuffer(vm *VM, fnName string, args []TinyValue, index int) *BufferValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	buffer, ok := args[index].Value.(*BufferValue)
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected buffer, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return buffer
}

func argBool(vm *VM, fnName string, args []TinyValue, index int) bool {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	str, ok := args[index].Value.(bool)
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected bool, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return str
}

func argArray(vm *VM, fnName string, args []TinyValue, index int) *ArrayValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(
			ErrorRuntime,
			"%s missing argument %d",
			fnName,
			index+1,
		)
	}

	array, ok := vm.valueAsArrayForRead(args[index])
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected array, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return array
}

func (vm *VM) valueAsArrayForRead(value TinyValue) (*ArrayValue, bool) {
	if value.IsInt {
		return nil, false
	}

	switch arr := value.Value.(type) {
	case *ArrayValue:
		return arr, arr != nil
	case ArrayValue:
		copyArr := arr
		return &copyArr, true
	case WasmArrayValue:
		source := vm
		if source == nil {
			source = arr.VM
		}
		if source == nil {
			return nil, false
		}
		return source.wasmArrayToArrayValue(arr)
	case *WasmArrayValue:
		if arr == nil {
			return nil, false
		}
		source := vm
		if source == nil {
			source = arr.VM
		}
		if source == nil {
			return nil, false
		}
		return source.wasmArrayToArrayValue(*arr)
	default:
		return nil, false
	}
}

func (vm *VM) valueAsObjectForRead(value TinyValue) (ObjectValue, bool) {
	if value.IsInt {
		return nil, false
	}

	switch obj := value.Value.(type) {
	case *InstanceValue:
		if obj == nil {
			return nil, false
		}
		return obj.Fields, true

	case ObjectValue:
		return obj, true

	case *ObjectValue:
		if obj == nil {
			return nil, false
		}
		return *obj, true

	case WasmObjectValue:
		source := vm
		if source == nil {
			source = obj.VM
		}
		if source == nil {
			return nil, false
		}
		return source.wasmObjectToObjectValue(obj)

	case *WasmObjectValue:
		if obj == nil {
			return nil, false
		}
		source := vm
		if source == nil {
			source = obj.VM
		}
		if source == nil {
			return nil, false
		}
		return source.wasmObjectToObjectValue(*obj)
	}

	return nil, false
}

type objectWriteTarget struct {
	vm     *VM
	native ObjectValue
	inst   *InstanceValue
	wasm   *WasmObjectValue
}

func (vm *VM) valueAsObjectForWrite(value TinyValue) (objectWriteTarget, bool) {
	if value.IsInt {
		return objectWriteTarget{}, false
	}

	switch obj := value.Value.(type) {
	case *InstanceValue:
		if obj == nil {
			return objectWriteTarget{}, false
		}
		return objectWriteTarget{vm: vm, native: obj.Fields, inst: obj}, true
	case ObjectValue:
		return objectWriteTarget{vm: vm, native: obj}, true
	case *ObjectValue:
		if obj == nil {
			return objectWriteTarget{}, false
		}
		return objectWriteTarget{vm: vm, native: *obj}, true
	case WasmObjectValue:
		source := vm
		if source == nil {
			source = obj.VM
		}
		if source == nil {
			return objectWriteTarget{}, false
		}
		obj.VM = source
		return objectWriteTarget{vm: source, wasm: &obj}, true
	case *WasmObjectValue:
		if obj == nil {
			return objectWriteTarget{}, false
		}
		source := vm
		if source == nil {
			source = obj.VM
		}
		if source == nil {
			return objectWriteTarget{}, false
		}
		obj.VM = source
		return objectWriteTarget{vm: source, wasm: obj}, true
	default:
		return objectWriteTarget{}, false
	}
}

func (target objectWriteTarget) isWasm() bool {
	return target.wasm != nil
}

func (target objectWriteTarget) isInstance() bool {
	return target.inst != nil
}

func (target objectWriteTarget) materialize() ObjectValue {
	if target.wasm != nil {
		if target.vm == nil {
			return nil
		}
		obj, ok := target.vm.wasmObjectToObjectValue(*target.wasm)
		if !ok {
			return nil
		}
		return obj
	}
	if target.inst != nil {
		return target.inst.Fields
	}
	return target.native
}

func (target objectWriteTarget) wasmShapeHasField(name string) bool {
	if target.wasm == nil || target.vm == nil || target.vm.jitModule == nil {
		return false
	}
	shapeIDF, ok := target.vm.readWasmFloatMaybe(uint32(target.wasm.Address) + 8)
	if !ok {
		return false
	}
	shapeID := int(shapeIDF)
	if shapeID < 0 || shapeID >= len(target.vm.objectShapes) {
		return false
	}
	for _, field := range target.vm.objectShapes[shapeID] {
		if field == name {
			return true
		}
	}
	return false
}

func (target objectWriteTarget) has(name string) bool {
	if target.wasm != nil {
		return target.wasmShapeHasField(name)
	}
	_, ok := target.native[name]
	return ok
}

func (target objectWriteTarget) get(name string) (TinyValue, bool) {
	if target.wasm != nil {
		if !target.wasmShapeHasField(name) || target.vm == nil {
			return TinyValue{}, false
		}
		return target.vm.getProperty(NewNative(*target.wasm), name, false), true
	}
	val, ok := target.native[name]
	return val, ok
}

func (target objectWriteTarget) set(name string, value TinyValue) {
	if target.wasm != nil {
		if target.vm == nil {
			return
		}
		if !target.wasmShapeHasField(name) {
			target.vm.runtimeError(ErrorRuntime, "cannot add new property %q to JIT object with fixed shape", name)
			return
		}
		offset := target.vm.getPropertyOffset(name)
		target.vm.WriteWasmTaggedValue(uint32(target.wasm.Address)+offset, value)
		return
	}
	target.native[name] = value
	if target.vm != nil {
		target.vm.invalidateJitObjectMirror(target.native)
	}
}

func (target objectWriteTarget) delete(name string) bool {
	if target.wasm != nil {
		if target.vm != nil {
			target.vm.runtimeError(ErrorRuntime, "cannot delete property %q from JIT object with fixed shape", name)
		}
		return false
	}
	_, found := target.native[name]
	if found {
		delete(target.native, name)
		if target.vm != nil {
			target.vm.invalidateJitObjectMirror(target.native)
		}
	}
	return found
}

func (target objectWriteTarget) clear() {
	if target.wasm != nil {
		if target.vm != nil {
			target.vm.runtimeError(ErrorRuntime, "cannot clear JIT object with fixed shape")
		}
		return
	}
	for key := range target.native {
		delete(target.native, key)
	}
	if target.vm != nil {
		target.vm.invalidateJitObjectMirror(target.native)
	}
}

func argObject(vm *VM, fnName string, args []TinyValue, index int) ObjectValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(
			ErrorRuntime,
			"%s missing argument %d",
			fnName,
			index+1,
		)
	}

	object, ok := vm.valueAsObjectForRead(args[index])
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected object, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return object
}

func argInt(vm *VM, fnName string, args []TinyValue, index int) int {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	if args[index].IsInt {
		return args[index].AsInt
	}

	if val, ok := args[index].Value.(float64); ok {
		return int(val)
	}

	vm.runtimeError(
		ErrorType,
		"%s argument %d expected number, got %s",
		fnName,
		index+1,
		TypeName(args[index]),
	)
	return 0
}

func argFloat64(vm *VM, fnName string, args []TinyValue, index int) float64 {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	if args[index].IsInt {
		return float64(args[index].AsInt)
	}

	if val, ok := args[index].Value.(float64); ok {
		return val
	}

	vm.runtimeError(
		ErrorType,
		"%s argument %d expected number, got %s",
		fnName,
		index+1,
		TypeName(args[index]),
	)

	return 0
}
