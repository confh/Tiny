package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdOSMethods = map[string]StdModuleFunc{
	"name": osName,
	"arch": osArch,
}

func (vm *VM) callStdOS(method string, args []Value) {
	fn, ok := stdOSMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown os function: %s", method)
		return
	}
	fn(vm, args)
}

func osName(vm *VM, args []Value) {
	dontExpectArgs(vm, "os.name", args)

	vm.push(runtime.GOOS)
}

func osArch(vm *VM, args []Value) {
	dontExpectArgs(vm, "os.arch", args)

	vm.push(runtime.GOARCH)
}
