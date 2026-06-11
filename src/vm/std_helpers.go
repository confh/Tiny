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

	array, ok := args[index].Value.(*ArrayValue)
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

func argObject(vm *VM, fnName string, args []TinyValue, index int) ObjectValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(
			ErrorRuntime,
			"%s missing argument %d",
			fnName,
			index+1,
		)
	}

	object, ok := args[index].Value.(ObjectValue)
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

	if !args[index].IsInt {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected number, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return args[index].AsInt
}

func argFloat64(vm *VM, fnName string, args []TinyValue, index int) float64 {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	value, ok := args[index].Value.(float64)
	if !ok {
		vm.runtimeError(
			ErrorType,
			"%s argument %d expected number, got %s",
			fnName,
			index+1,
			TypeName(args[index]),
		)
	}

	return value
}
