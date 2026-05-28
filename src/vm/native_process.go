package vm

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"slices"

	. "language.com/src/tinyerrors"
)

var processNativeMetadata = NativeTypeInfo{
	Name: "process",
	Methods: map[string]StdMethodInfo{
		"pid": {
			Name:        "pid",
			Returns:     "number",
			Description: "Returns the process PID.",
		},
		"wait": {
			Name:        "wait",
			Returns:     "void",
			Description: "Waits for the process to exit.",
		},
		"kill": {
			Name:        "kill",
			Returns:     "void",
			Description: "Kills the process.",
		},
		"killTree": {
			Name:        "killTree",
			Returns:     "void",
			Description: "Kills the process and its child processes.",
		},
		"interrupt": {
			Name:        "interrupt",
			Returns:     "void",
			Description: "Interrupts the process.",
		},
		"isRunning": {
			Name:        "isRunning",
			Returns:     "bool",
			Description: "Returns true if the process is still running.",
		},
		"signal": {
			Name: "signal",
			Args: []StdArg{
				{Name: "signal", Type: "string"},
			},
			Returns:     "void",
			Description: "Sends a signal to the process (linux only).",
		},
	},
}

var processMethods map[string]NativeModuleFunc[*NativeProcessValue]

func init() {
	processMethods = map[string]NativeModuleFunc[*NativeProcessValue]{
		"pid":       processPid,
		"wait":      processWait,
		"kill":      processKill,
		"killTree":  processKillTree,
		"interrupt": processInterrupt,
		"isRunning": processIsRunning,
		"signal":    processSignal,
	}
	registerNativeType(processNativeMetadata)
}

func (vm *VM) callProcessMethod(process *NativeProcessValue, method string, args []Value) {
	fn, ok := processMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown process method: %s", method)
		return
	}
	fn(vm, process, args)
}

func processPid(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.pid", args, 0)
	vm.push(NewInt(process.Cmd.Process.Pid))
}

func processWait(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.wait", args, 0)
	process.Cmd.Wait()
	vm.push(NewUndefined())
}

func processKill(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.kill", args, 0)
	err := process.Cmd.Process.Kill()
	if err != nil {
		vm.runtimeError(ErrorInternal, "could not kill process: %d", process.Cmd.Process.Pid)
	}
	process.Running = false
	vm.push(NewUndefined())
}

func processKillTree(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.killTree", args, 0)
	switch runtime.GOOS {
	case "windows":
		_ = exec.Command("taskkill", "/F", "/T", "/PID", fmt.Sprint(process.Cmd.Process.Pid)).Run()
	case "linux":
		_ = process.Cmd.Process.Signal(os.Interrupt)
	default:
		vm.runtimeError(ErrorInternal, "process.killTree is not supported on %s", runtime.GOOS)
	}
	process.Running = false
	vm.push(NewUndefined())
}

func processInterrupt(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.interrupt", args, 0)
	switch runtime.GOOS {
	case "windows":
		_ = process.Cmd.Process.Kill()
	case "linux":
		_ = process.Cmd.Process.Signal(os.Interrupt)
	default:
		vm.runtimeError(ErrorInternal, "process.killTree is not supported on %s", runtime.GOOS)
	}
	process.Running = false
	vm.push(NewUndefined())
}

func processIsRunning(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.isRunning", args, 0)
	vm.push(NewNative(process.Running))
}

func processSignal(vm *VM, process *NativeProcessValue, args []Value) {
	expectArgs(vm, "process.signal", args, 1)
	if runtime.GOOS != "linux" {
		vm.runtimeError(ErrorRuntime, "process.signal is only supported on linux.")
		return
	}

	availableSignals := []string{
		"interrupt",
		"kill",
	}
	signal := argString(vm, "process.signal", args, 0)

	if !slices.Contains(availableSignals, signal) {
		vm.runtimeError(ErrorRuntime, "signal \"%s\" is not a valid signal.", signal)
		return
	}

	switch signal {
	case "interrupt":
		_ = process.Cmd.Process.Signal(os.Interrupt)
	case "kill":
		_ = process.Cmd.Process.Signal(os.Kill)
	}
	vm.push(NewUndefined())
}
