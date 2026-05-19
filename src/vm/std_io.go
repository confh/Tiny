package vm

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdIO(method string, args []Value) {
	switch method {
	case "println":
		for i, arg := range args {
			if i > 0 {
				fmt.Print(" ")
			}

			fmt.Print(valueToString(arg))
		}

		fmt.Println()

		vm.push(UndefinedValue{})

	case "print":
		for _, arg := range args {
			fmt.Print(valueToString(arg))
		}

		vm.push(UndefinedValue{})

	case "input":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "io.input expects 1 argument")
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print(args[0])

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		vm.push(input)

	case "readLine":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "io.readLine expects 0 arguments")
		}

		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		vm.push(line)

	default:
		vm.runtimeError(ErrorName, "unknown io function: %s", method)
	}
}
