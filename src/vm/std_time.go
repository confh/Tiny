package vm

import (
	"time"

	. "language.com/src/tinyerrors"
)

var stdTimeMetadata = StdModuleInfo{
	Name: "time",
	Methods: map[string]StdMethodInfo{
		"sleep": {
			Name:        "sleep",
			Args:        []StdArg{{Name: "ms", Type: "number", Optional: false}},
			Returns:     "undefined",
			Description: "Sleeps for the given number of milliseconds.",
		},
		"nowNs": {
			Name:        "nowNs",
			Args:        []StdArg{},
			Returns:     "number",
			Description: "Returns the current Unix epoch time in nanoseconds.",
		},
		"nowMs": {
			Name:        "nowMs",
			Args:        []StdArg{},
			Returns:     "number",
			Description: "Returns the current Unix epoch time in milliseconds.",
		},
		"nowSec": {
			Name:        "nowSec",
			Args:        []StdArg{},
			Returns:     "number",
			Description: "Returns the current Unix epoch time in seconds.",
		},
		"clock": {
			Name:        "clock",
			Args:        []StdArg{},
			Returns:     "number",
			Description: "Returns the milliseconds elapsed since VM start.",
		},
	},
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

func stdTimeNowNs(vm *VM, args []Value) {
	dontExpectArgs(vm, "time.nowNs", args)
	vm.push(time.Now().UnixNano())
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
