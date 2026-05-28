package vm

import (
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdOSMetadata = StdModuleInfo{
	Name: "os",
	Methods: map[string]StdMethodInfo{
		"name": {
			Name:        "name",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns the operating system name.",
		},
		"arch": {
			Name:        "arch",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns the current architecture.",
		},
	},
}

var stdOSMethods map[string]StdModuleFunc

func init() {
	stdOSMethods = map[string]StdModuleFunc{
		"name": osName,
		"arch": osArch,
	}
	registerStdModule(stdOSMetadata)
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

	vm.push(NewNative(runtime.GOOS))
}

func osArch(vm *VM, args []Value) {
	dontExpectArgs(vm, "os.arch", args)

	vm.push(NewNative(runtime.GOARCH))
}
