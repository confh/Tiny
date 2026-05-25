package vm

import . "language.com/src/tinyerrors"

type StdModuleFunc func(vm *VM, args []Value)

type NativeModuleFunc[T any] func(vm *VM, value T, args []Value)

func dontExpectArgs(vm *VM, fnName string, args []Value) {
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

func expectArgs(vm *VM, fnName string, args []Value, count int) {
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

func expectArgsRange(vm *VM, fnName string, args []Value, min int, max int) {
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

func expectArgsMin(vm *VM, fnName string, args []Value, min int) {
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

func argString(vm *VM, fnName string, args []Value, index int) string {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	str, ok := args[index].(string)
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

func argFn(vm *VM, fnName string, args []Value, index int) FunctionValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	fn, ok := args[index].(FunctionValue)
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

func argBool(vm *VM, fnName string, args []Value, index int) bool {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	str, ok := args[index].(bool)
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

func argArray(vm *VM, fnName string, args []Value, index int) *ArrayValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(
			ErrorRuntime,
			"%s missing argument %d",
			fnName,
			index+1,
		)
	}

	array, ok := args[index].(*ArrayValue)
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

func argObject(vm *VM, fnName string, args []Value, index int) ObjectValue {
	if index < 0 || index >= len(args) {
		vm.runtimeError(
			ErrorRuntime,
			"%s missing argument %d",
			fnName,
			index+1,
		)
	}

	object, ok := args[index].(ObjectValue)
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

func argInt(vm *VM, fnName string, args []Value, index int) int {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	value, ok := args[index].(int)
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

func argFloat64(vm *VM, fnName string, args []Value, index int) float64 {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index)
	}

	value, ok := args[index].(float64)
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
