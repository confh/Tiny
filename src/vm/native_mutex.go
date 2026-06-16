package vm

import . "language.com/src/tinyerrors"

var mutexNativeData = NativeTypeInfo{
	Name: "mutex",
}

var mutexMethods map[string]NativeModuleFunc[*NativeMutexValue]

func init() {
	mutexMethods = map[string]NativeModuleFunc[*NativeMutexValue]{
		"lock":   mutexLock,
		"unlock": mutexUnlock,
	}
}

func (vm *VM) callNativeMutexMethod(mutex *NativeMutexValue, method string, args []TinyValue) {
	fn, ok := mutexMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown mutex method: %s", method)
		return
	}
	fn(vm, mutex, args)
}

func mutexLock(vm *VM, mutex *NativeMutexValue, args []TinyValue) {
	dontExpectArgs(vm, "mutex.lock", args)

	mutex.Lock()

	vm.push(NewNull())
}

func mutexUnlock(vm *VM, mutex *NativeMutexValue, args []TinyValue) {
	dontExpectArgs(vm, "mutex.unlock", args)

	mutex.Unlock()

	vm.push(NewNull())
}
