package vm

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callProcessMethod(process *NativeProcessValue, method string, args []Value) {
	switch method {
	case "pid":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.pid expects 0 arguments")
		}

		vm.push(process.Cmd.Process.Pid)

	case "wait":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.wait expects 0 arguments")
		}

		process.Cmd.Wait()

		vm.push(UndefinedValue{})

	case "kill":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.kill expects 0 arguments")
		}

		err := process.Cmd.Process.Kill()
		if err != nil {
			vm.runtimeError(ErrorInternal, "could not kill process: %d", process.Cmd.Process.Pid)
		}

		process.Running = false

		vm.push(UndefinedValue{})

	case "killTree":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.killTree expects 0 arguments")
		}

		switch runtime.GOOS {
		case "windows":
			exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(process.Cmd.Process.Pid)).Run()

		case "linux":
			process.Cmd.Process.Signal(os.Interrupt)

		default:
			vm.runtimeError(ErrorInternal, "process.killTree is not supported on %s", runtime.GOOS)
		}

		process.Running = false

		vm.push(UndefinedValue{})

	case "interrupt":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.interrupt expects 0 arguments")
		}

		switch runtime.GOOS {
		case "windows":
			process.Cmd.Process.Kill()

		case "linux":
			process.Cmd.Process.Signal(os.Interrupt)

		default:
			vm.runtimeError(ErrorInternal, "process.killTree is not supported on %s", runtime.GOOS)
		}

		process.Running = false

		vm.push(UndefinedValue{})

	case "isRunning":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.isRunning expects 0 arguments")
		}

		vm.push(process.Running)

	case "signal":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.signal expects 1 argument")
		}

		if runtime.GOOS != "linux" {
			vm.runtimeError(ErrorRuntime, "process.signal is only supported on linux.")
		}

		availableSignals := []string{
			"interrupt",
			"kill",
		}

		signal := asString(args[0], vm)

		if slices.Contains(availableSignals, signal) {
			vm.runtimeError(ErrorRuntime, "signal \"%s\" is not a valid signal.", signal)
		}

		switch signal {
		case "interrupt":
			process.Cmd.Process.Signal(os.Interrupt)

		case "kill":
			process.Cmd.Process.Signal(os.Kill)
		}

		vm.push(UndefinedValue{})

	default:
		vm.runtimeError(ErrorName, "unknown process method: %s", method)
	}
}
