package vm

import (
	"bufio"
	"fmt"
	"os"

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
			vm.runtimeError(ErrorRuntime, "Error getting current directory:", err)
			return
		}

		vm.push(root)

	case "getEnv":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.getEnv expects 1 argument")
		}

		vm.push(os.Getenv(asString(args[0])))

	case "setEnv":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "process.setEnv expects 2 arguments")
		}

		key := asString(args[0])
		value := asString(args[1])

		os.Setenv(key, value)

	case "unsetEnv":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "process.unsetEnv expects 1 argument")
		}

		key := asString(args[0])

		os.Unsetenv(key)

	case "halt":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "process.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		vm.push(UndefinedValue{})

	default:
		vm.runtimeError(ErrorName, "unknown process function: %s", method)
	}
}
