package vm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	. "language.com/src/tinyerrors"
)

var stdProcessMethods = map[string]StdModuleFunc{
	"args":     processArgs,
	"exit":     processExit,
	"close":    processClose,
	"cwd":      processCwd,
	"getEnv":   processGetEnv,
	"setEnv":   processSetEnv,
	"unsetEnv": processUnsetEnv,
	"halt":     processHalt,
	"run":      processRun,
	"shell":    processShell,
	"start":    processStart,
}

func (vm *VM) callStdProcess(method string, args []Value) {
	fn, ok := stdProcessMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown process function: %s", method)
		return
	}
	fn(vm, args)
}

func processArgs(vm *VM, args []Value) {
	expectArgs(vm, "process.args", args, 0)

	argsArray := &ArrayValue{Elements: make([]Value, 0, len(vm.cliArgs))}
	for _, v := range vm.cliArgs {
		argsArray.Elements = append(argsArray.Elements, v)
	}
	vm.push(argsArray)
}

func processExit(vm *VM, args []Value) {
	expectArgs(vm, "process.exit", args, 1)

	value := argInt(vm, "process.exit", args, 0)
	os.Exit(value)
}

func processClose(vm *VM, args []Value) {
	expectArgs(vm, "process.close", args, 0)

	os.Exit(0)
}

func processCwd(vm *VM, args []Value) {
	expectArgs(vm, "process.cwd", args, 0)

	root, err := os.Getwd()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "Error getting current directory: %s", err)
		return
	}
	vm.push(root)
}

func processGetEnv(vm *VM, args []Value) {
	expectArgs(vm, "process.getEnv", args, 1)

	value := argString(vm, "process.getEnv", args, 0)
	vm.push(os.Getenv(value))
}

func processSetEnv(vm *VM, args []Value) {
	expectArgs(vm, "process.setEnv", args, 2)

	key := argString(vm, "process.setEnv", args, 0)
	value := argString(vm, "process.setEnv", args, 1)
	os.Setenv(key, value)
}

func processUnsetEnv(vm *VM, args []Value) {
	expectArgs(vm, "process.unsetEnv", args, 1)

	key := argString(vm, "process.unsetEnv", args, 0)
	os.Unsetenv(key)
}

func processHalt(vm *VM, args []Value) {
	expectArgs(vm, "process.halt", args, 0)

	fmt.Println("Press Enter to exit...")
	reader := bufio.NewReader(os.Stdin)
	_, _ = reader.ReadString('\n')
	vm.push(UndefinedValue{})
}

func processRun(vm *VM, args []Value) {
	expectArgsRange(vm, "process.run", args, 1, 3)

	commandName := argString(vm, "process.run", args, 0)
	cmdArgs := []string{}
	cwd := ""
	captureStdout := false
	captureStderr := false

	if len(args) > 1 {
		attachedArgs := argArray(vm, "process.run", args, 1)
		for _, el := range attachedArgs.Elements {
			if str, ok := el.(string); ok {
				cmdArgs = append(cmdArgs, str)
			} else {
				vm.runtimeError(ErrorType, "process.run expects an array of strings as second argument.")
			}
		}
	}

	if len(args) > 2 {
		options := argObject(vm, "process.run", args, 2)
		if value, ok := options["cwd"]; ok {
			cwd = asString(value, vm)
		}
		if value, ok := options["stdout"]; ok {
			captureStdout = asBool(value, vm)
		}
		if value, ok := options["stderr"]; ok {
			captureStderr = asBool(value, vm)
		}
	}

	cmd := exec.Command(commandName, cmdArgs...)
	if len(cwd) > 0 {
		cmd.Dir = cwd
	}

	stdout := ""
	stderr := ""
	var err error

	if captureStdout {
		var stdoutValue []byte
		stdoutValue, err = cmd.Output()
		stdout = string(stdoutValue)
		if captureStderr && err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = string(exitErr.Stderr)
			}
		}
	} else {
		err = cmd.Run()
	}

	exitCode := -1
	success := false
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
		success = cmd.ProcessState.Success()
	}

	vm.push(ObjectValue{
		"exitCode": exitCode,
		"stdout":   stdout,
		"stderr":   stderr,
		"success":  success,
	})
}

func processShell(vm *VM, args []Value) {
	expectArgsRange(vm, "process.shell", args, 1, 2)

	commandName := argString(vm, "process.shell", args, 0)
	cwd := ""
	captureStdout := false
	captureStderr := false

	if len(args) > 1 {
		options := argObject(vm, "process.shell", args, 1)
		if value, ok := options["cwd"]; ok {
			cwd = asString(value, vm)
		}
		if value, ok := options["stdout"]; ok {
			captureStdout = asBool(value, vm)
		}
		if value, ok := options["stderr"]; ok {
			captureStderr = asBool(value, vm)
		}
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		shellArgs := []string{"/c", commandName}
		cmd = exec.Command("cmd.exe", shellArgs...)
	case "linux":
		shellArgs := []string{"-c", commandName}
		cmd = exec.Command("/bin/sh", shellArgs...)
	default:
		vm.runtimeError(ErrorInternal, "process.shell is not supported on %s.", runtime.GOOS)
		return
	}

	if len(cwd) > 0 {
		cmd.Dir = cwd
	}

	stdout := ""
	stderr := ""
	var err error

	if captureStdout {
		var stdoutValue []byte
		stdoutValue, err = cmd.Output()
		stdout = string(stdoutValue)
		if captureStderr && err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				stderr = string(exitErr.Stderr)
			}
		}
	} else {
		err = cmd.Run()
	}

	exitCode := -1
	success := false
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
		success = cmd.ProcessState.Success()
	}

	vm.push(ObjectValue{
		"exitCode": exitCode,
		"stdout":   stdout,
		"stderr":   stderr,
		"success":  success,
	})
}

func processStart(vm *VM, args []Value) {
	expectArgsRange(vm, "process.start", args, 1, 3)

	commandName := argString(vm, "process.start", args, 0)
	cmdArgs := []string{}
	cwd := ""
	captureStdout := false
	captureStderr := false

	if len(args) > 1 {
		attachedArgs := argArray(vm, "process.start", args, 1)
		for _, el := range attachedArgs.Elements {
			if str, ok := el.(string); ok {
				cmdArgs = append(cmdArgs, str)
			} else {
				vm.runtimeError(ErrorType, "process.start expects an array of strings as second argument.")
			}
		}
	}

	if len(args) > 2 {
		options := argObject(vm, "process.start", args, 2)
		if value, ok := options["cwd"]; ok {
			cwd = asString(value, vm)
		}
		if value, ok := options["stdout"]; ok {
			captureStdout = asBool(value, vm)
		}
		if value, ok := options["stderr"]; ok {
			captureStderr = asBool(value, vm)
		}
	}

	cmd := exec.Command(commandName, cmdArgs...)
	if len(cwd) > 0 {
		cmd.Dir = cwd
	}

	if captureStdout {
		cmd.Stdout = os.Stdout
	}
	if captureStderr {
		cmd.Stderr = os.Stderr
	}

	cmd.Start()

	processValue := &NativeProcessValue{
		Cmd:     cmd,
		Running: true,
	}

	vm.push(processValue)
}
