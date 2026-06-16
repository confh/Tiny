package vm

import (
	"time"

	. "language.com/src/tinyerrors"
)

var timerMethods map[string]NativeModuleFunc[*NativeTimerValue]

func init() {
	timerMethods = map[string]NativeModuleFunc[*NativeTimerValue]{
		"cancel": timerCancel,
		"reset":  timerReset,
		"type":   timerType,
	}
}

func (vm *VM) callNativeTimerMethod(timer *NativeTimerValue, method string, args []TinyValue) {
	fn, ok := timerMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown timer method: %s", method)
		return
	}
	fn(vm, timer, args)
}

func timerCancel(vm *VM, timer *NativeTimerValue, args []TinyValue) {
	dontExpectArgs(vm, "timer.cancel", args)

	switch timer.Type {
	case Timer:
		timer.Timer.Stop()
	case Ticker:
		timer.Ticker.Stop()
		timer.Quit <- true
	}

	vm.push(NewNull())
}

func timerReset(vm *VM, timer *NativeTimerValue, args []TinyValue) {
	expectArgs(vm, "timer.reset", args, 1)

	ms := argInt(vm, "timer.reset", args, 0)

	duration := time.Duration(ms) * time.Millisecond

	switch timer.Type {
	case Timer:
		timer.Timer.Reset(duration)
	case Ticker:
		timer.Ticker.Reset(duration)
	}

	vm.push(NewNull())
}

func timerType(vm *VM, timer *NativeTimerValue, args []TinyValue) {
	dontExpectArgs(vm, "timer.type", args)

	switch timer.Type {
	case Timer:
		vm.push(NewNative("Timer"))
	case Ticker:
		vm.push(NewNative("Ticker"))
	}
}
