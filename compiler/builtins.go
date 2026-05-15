package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func (vm *VM) callCore(method string, argCount int) {
	switch method {
	case "error":
		if argCount != 2 {
			langError(ErrorRuntime, "Core.error expects 2 arguments")
		}

		args := vm.popArgs(argCount)

		kind := asString(args[0])
		message := asString(args[1])

		vm.push(ErrorValue{
			Kind:    kind,
			Message: message,
		})

	case "typeOf":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.typeOf expects 1 argument")
		}

		value := vm.pop()

		vm.push(typeName(value))
	case "server":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.server expects 1 argument")
		}

		port := asInt(vm.pop())

		server := &NativeServerValue{
			Port:   port,
			Routes: map[string]Value{},
		}

		vm.push(server)
	case "has":
		if argCount != 2 {
			langError(ErrorRuntime, "Core.has expects 2 arguments")
		}

		key := asString(vm.pop())
		objectValue := vm.pop()

		object, ok := objectValue.(ObjectValue)
		if !ok {
			langError(ErrorType, "Core.has expected object, got %s", typeName(objectValue))
		}

		_, exists := object[key]
		vm.push(exists)
	case "len":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.len expects 1 argument")
		}

		value := vm.pop()

		switch n := value.(type) {
		case string:
			vm.push(len(n))
		case ArrayValue:
			vm.push(len(n.Elements))
		default:
			langError(ErrorRuntime, "argument does not have a length.")
		}
	case "clock":
		if argCount != 0 {
			langError(ErrorRuntime, "Core.clock expects 0 arguments")
		}

		vm.push(int(time.Now().UnixMilli() - vm.start))
	case "time":
		if argCount != 0 {
			langError(ErrorRuntime, "Core.time expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli())
	case "sleep":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.sleep expects 1 argument")
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
			langError(ErrorRuntime, "Core.sleeinputp expects 1 argument")
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print(vm.pop())

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		vm.push(input)
	case "close":
		if argCount != 0 {
			langError(ErrorRuntime, "Core.close expects 0 arguments")
		}

		os.Exit(0)

	case "exit":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.exit expects 1 argument")
		}

		os.Exit(asInt(vm.pop()))
	case "halt":
		if argCount != 0 {
			langError(ErrorRuntime, "Core.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		vm.push(0)

	default:
		langError(ErrorName, "unknown core function: %s", method)
	}
}

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	switch object {
	case "Core":
		vm.callCore(method, argCount)

	case "Plugin":
		vm.callPluginModule(method, argCount)

	default:
		langError(ErrorName, "unknown builtin module: %s", object)
	}
}
