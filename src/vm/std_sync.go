package vm

import (
	. "language.com/src/tinyerrors"
)

var stdSyncMetadata = StdModuleInfo{
	Name: "sync",
	Methods: map[string]StdMethodInfo{
		"mutex": {
			Name:        "mutex",
			Args:        []StdArg{},
			Returns:     "mutex",
			Description: "Creates and returns a new mutex object for concurrency control. Allows locking and unlocking to coordinate access across tasks.",
		},
	},
}

var stdSyncMethods map[string]StdModuleFunc

func init() {
	stdSyncMethods = map[string]StdModuleFunc{
		"mutex": syncMakeMutex,
	}
	registerStdModule(stdSyncMetadata)
}

func (vm *VM) callStdSync(method string, args []Value) {
	fn, ok := stdSyncMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown sync function: %s", method)
		return
	}

	fn(vm, args)
}

func syncMakeMutex(vm *VM, args []Value) {
	dontExpectArgs(vm, "sync.mutex", args)

	vm.push(&NativeMutexValue{})
}
