package vm

import (
	"time"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdTime(method string, args []Value) {
	switch method {
	case "sleep":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "time.sleep expects 1 argument")
		}

		time.Sleep(time.Duration(asInt(args[0])) * time.Millisecond)

		vm.push(UndefinedValue{})

	case "nowMs":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "time.nowMs expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli())

	case "nowSec":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "time.nowSec expects 0 arguments")
		}

		vm.push(time.Now().Unix())

	case "clock":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "time.clock() expects 0 arguments")
		}

		vm.push(int(time.Now().UnixMilli() - vm.start))

	default:
		vm.runtimeError(ErrorName, "unknown time function: %s", method)
	}
}
