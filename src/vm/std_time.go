package vm

import (
	"time"

	. "language.com/src/tinyerrors"
)

var stdTimeMethods = map[string]StdModuleFunc{
	"sleep":  stdTimeSleep,
	"nowMs":  stdTimeNowMs,
	"nowSec": stdTimeNowSec,
	"clock":  stdTimeClock,
}

func (vm *VM) callStdTime(method string, args []Value) {
	fn, ok := stdTimeMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown time function: %s", method)
		return
	}
	fn(vm, args)
}

func stdTimeSleep(vm *VM, args []Value) {
	expectArgs(vm, "time.sleep", args, 1)
	ms := argInt(vm, "time.sleep", args, 0)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	vm.push(UndefinedValue{})
}

func stdTimeNowMs(vm *VM, args []Value) {
	dontExpectArgs(vm, "time.nowMs", args)
	vm.push(time.Now().UnixMilli())
}

func stdTimeNowSec(vm *VM, args []Value) {
	dontExpectArgs(vm, "time.nowSec", args)
	vm.push(time.Now().Unix())
}

func stdTimeClock(vm *VM, args []Value) {
	dontExpectArgs(vm, "time.clock", args)
	vm.push(int(time.Now().UnixMilli() - vm.start))
}
