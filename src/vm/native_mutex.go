package vm

import . "language.com/src/tinyerrors"

var mutexNativeData = NativeTypeInfo{
	Name: "mutex",
	Methods: map[string]StdMethodInfo{
		"lock": {
			Name:        "lock",
			Args:        []StdArg{},
			Returns:     "void",
			Description: "Acquires the mutex lock. Blocks other tasks until the lock is released.",
		},
		"unlock": {
			Name:        "unlock",
			Args:        []StdArg{},
			Returns:     "void",
			Description: "Releases the mutex lock.",
		},
	},
}

var mutexMethods map[string]NativeModuleFunc[*NativeMutexValue]

func init() {
	mutexMethods = map[string]NativeModuleFunc[*NativeMutexValue]{
		"lock":   mutexLock,
		"unlock": mutexUnlock,
	}
	registerNativeType(mutexNativeData)
}

func (vm *VM) callNativeMutexMethod(mutex *NativeMutexValue, method string, args []Value) {
	fn, ok := mutexMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown mutex method: %s", method)
		return
	}
	fn(vm, mutex, args)
}

func mutexLock(vm *VM, mutex *NativeMutexValue, args []Value) {
	dontExpectArgs(vm, "mutex.lock", args)

	mutex.Lock()

	vm.push(UndefinedValue{})
}

func mutexUnlock(vm *VM, mutex *NativeMutexValue, args []Value) {
	dontExpectArgs(vm, "mutex.unlock", args)

	mutex.Unlock()

	vm.push(UndefinedValue{})
}
