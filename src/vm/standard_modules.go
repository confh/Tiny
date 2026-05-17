package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callStandardModule(module string, method string, args []Value) {
	switch module {
	case "array":
		vm.callStdArray(method, args)

	case "math":
		vm.callStdMath(method, args)

	case "string":
		vm.callStdString(method, args)

	case "json":
		vm.callStdJson(method, args)

	case "fs":
		vm.callStdFs(method, args)

	case "app":
		vm.callStdApp(method, args)

	case "buffer":
		vm.callStdBuffer(method, args)

	default:
		LangError(ErrorName, "unknown standard module: %s", module)
	}
}
