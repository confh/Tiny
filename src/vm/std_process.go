package vm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdProcess(method string, args []Value) {
	switch method {
	case "args":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.args expects 0 arguments")
		}

		argsArray := &ArrayValue{
			Elements: make([]Value, 0, len(vm.cliArgs)),
		}

		for _, v := range vm.cliArgs {
			argsArray.Elements = append(argsArray.Elements, v)
		}

		vm.push(argsArray)

	case "exit":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.exit expects 1 argument")
		}

		os.Exit(asInt(args[0]))

	case "close":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.close expects 0 arguments")
		}

		os.Exit(0)

	case "cwd":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.cwd expects 0 arguments")
		}

		root, err := os.Getwd()
		if err != nil {
			vm.runtimeError(ErrorRuntime, "Error getting current directory: %s", err)
			return
		}

		vm.push(root)

	case "getEnv":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.getEnv expects 1 argument")
		}

		vm.push(os.Getenv(asString(args[0], vm)))

	case "setEnv":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "process.setEnv expects 2 arguments")
		}

		key := asString(args[0], vm)
		value := asString(args[1], vm)

		os.Setenv(key, value)

	case "unsetEnv":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.unsetEnv expects 1 argument")
		}

		key := asString(args[0], vm)

		os.Unsetenv(key)

	case "halt":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		vm.push(UndefinedValue{})

	case "run":
		if len(args) > 3 || len(args) < 1 {
			vm.runtimeError(ErrorRuntime, "process.run expects at least one argument and maximum 3 arguments")
		}

		commandName := asString(args[0], vm)
		cmdArgs := []string{}
		cwd := ""
		captureStdout := false
		captureStderr := false

		if len(args) > 1 {
			attachedArgs := asArray(args[1], vm)
			elements := attachedArgs.Elements

			for _, el := range elements {
				if str, ok := el.(string); ok {
					cmdArgs = append(cmdArgs, str)
				} else {
					vm.runtimeError(ErrorType, "process.run expects an array of strings as second argument.")
				}
			}
		}

		if len(args) > 2 {
			options := asObject(args[2], vm)
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

	case "shell":
		if len(args) > 2 || len(args) < 1 {
			vm.runtimeError(ErrorRuntime, "process.shell expects at least one argument and maximum 2 arguments")
		}

		commandName := asString(args[0], vm)
		cwd := ""
		captureStdout := false
		captureStderr := false

		if len(args) > 1 {
			options := asObject(args[1], vm)
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

	case "start":
		if len(args) > 3 || len(args) < 1 {
			vm.runtimeError(ErrorRuntime, "process.start expects at least one argument and maximum 3 arguments")
		}

		commandName := asString(args[0], vm)
		cmdArgs := []string{}
		cwd := ""
		captureStdout := false
		captureStderr := false

		if len(args) > 1 {
			attachedArgs := asArray(args[1], vm)
			elements := attachedArgs.Elements

			for _, el := range elements {
				if str, ok := el.(string); ok {
					cmdArgs = append(cmdArgs, str)
				} else {
					vm.runtimeError(ErrorType, "process.start expects an array of strings as second argument.")
				}
			}
		}

		if len(args) > 2 {
			options := asObject(args[2], vm)
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

	default:
		vm.runtimeError(ErrorName, "unknown process function: %s", method)
	}
}
