package vm

import (
	. "language.com/src/tinyerrors"
)

func (v *NativeMutexValue) TinyTypeName() string {
	return "sync.Mutex"
}

var stdSyncMetadata = StdModuleInfo{
	Name: "sync",
}

var stdSyncMethods map[string]StdModuleFunc

func init() {
	stdSyncMethods = map[string]StdModuleFunc{
		"mutex": syncMakeMutex,
	}
	registerStdModule(stdSyncMetadata)
}

func (vm *VM) callStdSync(method string, args []TinyValue) {
	fn, ok := stdSyncMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown sync function: %s", method)
		return
	}

	fn(vm, args)
}

func syncMakeMutex(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "sync.mutex", args)

	vm.push(NewNative(&NativeMutexValue{}))
}
