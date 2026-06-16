package vm

import (
	"fmt"
	"os"
	"sync/atomic"
	"time"

	. "language.com/src/tinyerrors"
)

var stdTimeMethods map[string]StdModuleFunc

func init() {
	stdTimeMethods = map[string]StdModuleFunc{
		"sleep":    stdTimeSleep,
		"nowNs":    stdTimeNowNs,
		"nowMs":    stdTimeNowMs,
		"nowSec":   stdTimeNowSec,
		"clock":    stdTimeClock,
		"timeout":  stdTimeTimeout,
		"interval": stdTimeInterval,
	}
	registerStdEnum("time", "TimerType", ObjectValue{
		"Timer":  NewNative("Timer"),
		"Ticker": NewNative("Ticker"),
	})
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

func handleAsyncTimerPanic(where string, r any) {
	fmt.Fprintf(os.Stderr, "uncaught async error in %s callback: %v\n", where, r)
}

func stdTimeTimeout(vm *VM, args []TinyValue) {
	expectArgs(vm, "time.timeout", args, 2)

	ms := argInt(vm, "time.timeout", args, 0)
	fn := argFn(vm, "time.timeout", args, 1)

	if ms <= 0 {
		vm.runtimeError(ErrorRuntime, "time.timeout duration must be greater than 0")
		return
	}

	duration := time.Duration(ms) * time.Millisecond

	timerValue := &NativeTimerValue{
		Type: Timer,
	}

	timer := time.AfterFunc(duration, func() {
		if timerValue.IsCancelled() {
			return
		}

		taskVM := vm.taskPool.Get()
		defer vm.taskPool.Put(taskVM)

		defer func() {
			if r := recover(); r != nil {
				handleAsyncTimerPanic("time.timeout", r)
			}
		}()

		taskVM.callFunctionValue(fn, []TinyValue{})
	})

	timerValue.Timer = timer

	vm.push(NewNative(timerValue))
}

func stdTimeInterval(vm *VM, args []TinyValue) {
	expectArgs(vm, "time.interval", args, 2)

	ms := argInt(vm, "time.interval", args, 0)
	fn := argFn(vm, "time.interval", args, 1)

	if ms <= 0 {
		vm.runtimeError(ErrorRuntime, "time.interval duration must be greater than 0")
		return
	}

	duration := time.Duration(ms) * time.Millisecond

	timerValue := &NativeTimerValue{
		Type:   Ticker,
		Ticker: time.NewTicker(duration),
		Quit:   make(chan bool),
	}

	var running atomic.Bool

	go func() {
		for {
			select {
			case <-timerValue.Ticker.C:
				if timerValue.IsCancelled() {
					return
				}

				if !running.CompareAndSwap(false, true) {
					continue
				}

				go func() {
					defer running.Store(false)

					if timerValue.IsCancelled() {
						return
					}

					taskVM := vm.taskPool.Get()
					defer vm.taskPool.Put(taskVM)

					defer func() {
						if r := recover(); r != nil {
							handleAsyncTimerPanic("time.interval", r)

							timerValue.Cancel()
						}
					}()

					taskVM.callFunctionValue(fn, []TinyValue{})
				}()

			case <-timerValue.Quit:
				return
			}
		}
	}()

	vm.push(NewNative(timerValue))
}
