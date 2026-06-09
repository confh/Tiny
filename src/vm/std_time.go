package vm

import (
	"time"

	. "language.com/src/tinyerrors"
)

var stdTimeMetadata = StdModuleInfo{
	Name: "time",
}

var stdTimeMethods map[string]StdModuleFunc

func init() {
	stdTimeMethods = map[string]StdModuleFunc{
		"sleep":  stdTimeSleep,
		"nowNs":  stdTimeNowNs,
		"nowMs":  stdTimeNowMs,
		"nowSec": stdTimeNowSec,
		"clock":  stdTimeClock,
	}
	registerStdModule(stdTimeMetadata)
}

func (vm *VM) callStdTime(method string, args []TinyValue) {
	fn, ok := stdTimeMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown time function: %s", method)
		return
	}
	fn(vm, args)
}

func stdTimeSleep(vm *VM, args []TinyValue) {
	expectArgs(vm, "time.sleep", args, 1)
	ms := argInt(vm, "time.sleep", args, 0)
	time.Sleep(time.Duration(ms) * time.Millisecond)
	vm.push(NewNull())
}

func stdTimeNowNs(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "time.nowNs", args)
	vm.push(NewInt(int(time.Now().UnixNano())))
}

func stdTimeNowMs(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "time.nowMs", args)
	vm.push(NewInt(int(time.Now().UnixMilli())))
}

func stdTimeNowSec(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "time.nowSec", args)
	vm.push(NewInt(int(time.Now().Unix())))
}

func stdTimeClock(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "time.clock", args)
	vm.push(NewNative(int(time.Now().UnixMilli() - vm.start)))
}
