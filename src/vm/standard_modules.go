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

	case "regex":
		vm.callStdRegex(method, args)

	case "io":
		vm.callStdIO(method, args)

	case "process":
		vm.callStdProcess(method, args)

	case "time":
		vm.callStdTime(method, args)

	case "error":
		vm.callStdError(method, args)

	case "http":
		vm.callStdHttp(method, args)

	case "os":
		vm.callStdOS(method, args)

	case "runtime":
		vm.callStdRuntime(method, args)

	case "net":
		vm.callStdNet(method, args)

	case "path":
		vm.callStdPath(method, args)

	case "object":
		vm.callStdObject(method, args)

	case "desktop":
		vm.callStdDesktop(method, args)

	case "sync":
		vm.callStdSync(method, args)

	default:
		vm.fatalError(ErrorName, "unknown standard module: %s", module)
	}
}
