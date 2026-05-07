package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	if object != "core" {
		panic(fmt.Sprintf("unknown builtin module: %s", object))
	}

	switch method {
	case "len":
		if argCount != 1 {
			panic("core.len expects 1 argument")
		}

		value := vm.pop()

		switch n := value.(type) {
		case string:
			vm.push(len(n))
		case ArrayValue:
			vm.push(len(n))
		default:
			panic("argument does not have a length.")
		}
	case "clock":
		if argCount != 0 {
			panic("core.clock expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli() - vm.start)
	case "time":
		if argCount != 0 {
			panic("core.time expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli())
	case "sleep":
		if argCount != 1 {
			panic("core.sleep expects 1 argument")
		}

		time.Sleep(time.Duration(asInt(vm.pop())) * time.Millisecond)

		vm.push(0)
	case "print":
		args := vm.popArgs(argCount)

		for _, arg := range args {
			fmt.Print(valueToString(arg))
		}

		vm.push(0)

	case "println":
		args := vm.popArgs(argCount)

		for i, arg := range args {
			if i > 0 {
				fmt.Print(" ")
			}

			fmt.Print(valueToString(arg))
		}

		fmt.Println()

		vm.push(0)
	case "input":
		if argCount != 1 {
			panic("core.input expects 1 argument")
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print(vm.pop())

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		vm.push(input)
	case "close":
		if argCount != 0 {
			panic("core.close expects 0 arguments")
		}

		os.Exit(0)

	case "exit":
		if argCount != 1 {
			panic("core.exit expects 1 argument")
		}

		os.Exit(asInt(vm.pop()))
	case "halt":
		if argCount != 0 {
			panic("core.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		// Builtin calls are expressions, so push a dummy return value.
		vm.push(0)

	default:
		panic(fmt.Sprintf("unknown core function: %s", method))
	}
}
