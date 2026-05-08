package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	if object != "core" {
		langError(ErrorName, "unknown builtin module: %s", object)
	}

	switch method {
	case "server":
		if argCount != 1 {
			langError(ErrorRuntime, "core.server expects 1 argument")
		}

		port := asInt(vm.pop())

		server := &NativeServerValue{
			Port:   port,
			Routes: map[string]string{},
		}

		vm.push(server)
	case "toPrettyJSON":
		if argCount != 1 {
			langError(ErrorRuntime, "core.toPrettyJSON expects 1 argument")
		}

		value := vm.pop()

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))
	case "toJSON":
		if argCount != 1 {
			langError(ErrorRuntime, "core.toJSON expects 1 argument")
		}

		value := vm.pop()

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))
	case "has":
		if argCount != 2 {
			langError(ErrorRuntime, "core.has expects 2 arguments")
		}

		key := asString(vm.pop())
		objectValue := vm.pop()

		object, ok := objectValue.(ObjectValue)
		if !ok {
			langError(ErrorType, "core.has expected object, got %s", typeName(objectValue))
		}

		_, exists := object[key]
		vm.push(exists)
	case "len":
		if argCount != 1 {
			langError(ErrorRuntime, "core.len expects 1 argument")
		}

		value := vm.pop()

		switch n := value.(type) {
		case string:
			vm.push(len(n))
		case ArrayValue:
			vm.push(len(n))
		default:
			langError(ErrorRuntime, "argument does not have a length.")
		}
	case "clock":
		if argCount != 0 {
			langError(ErrorRuntime, "core.clock expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli() - vm.start)
	case "time":
		if argCount != 0 {
			langError(ErrorRuntime, "core.time expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli())
	case "sleep":
		if argCount != 1 {
			langError(ErrorRuntime, "core.sleep expects 1 argument")
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
			langError(ErrorRuntime, "core.sleeinputp expects 1 argument")
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print(vm.pop())

		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)

		vm.push(input)
	case "close":
		if argCount != 0 {
			langError(ErrorRuntime, "core.close expects 0 arguments")
		}

		os.Exit(0)

	case "exit":
		if argCount != 1 {
			langError(ErrorRuntime, "core.exit expects 1 argument")
		}

		os.Exit(asInt(vm.pop()))
	case "halt":
		if argCount != 0 {
			langError(ErrorRuntime, "core.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		// Builtin calls are expressions, so push a dummy return value.
		vm.push(0)

	default:
		langError(ErrorName, "unknown core function: %s", method)
	}
}
