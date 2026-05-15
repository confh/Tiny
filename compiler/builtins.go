package main

import (
	"bufio"
	"encoding/json"
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
	case "toPrettyJSON":
		if argCount != 1 {
			langError(ErrorRuntime, "Core.toPrettyJSON expects 1 argument")
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
			langError(ErrorRuntime, "Core.toJSON expects 1 argument")
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

func (vm *VM) callMath(method string, argCount int) {
	switch method {
	case "toFloat":
		if argCount != 1 {
			langError(ErrorRuntime, "Math.toFloat expects 1 argument")
		}

		vm.push(asFloat(vm.pop()))

	case "toInt":
		if argCount != 1 {
			langError(ErrorRuntime, "Math.toInt expects 1 argument")
		}

		vm.push(int(asFloat(vm.pop())))

	default:
		langError(ErrorName, "unknown math function: %s", method)
	}
}

func (vm *VM) callArray(method string, argCount int) {
	switch method {
	case "push":
		if argCount != 2 {
			langError(ErrorRuntime, "Array.push expects 2 arguments")
		}

		args := vm.popArgs(argCount)

		array, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "Array.push expects array, got %s", typeName(args[0]))
		}

		value := args[1]

		array.Elements = append(array.Elements, value)

		vm.push(0)

	case "pop":
		if argCount != 1 {
			langError(ErrorRuntime, "Array.pop expects 1 argument")
		}

		args := vm.popArgs(argCount)

		array, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "Array.pop expects array, got %s", typeName(args[0]))
		}

		last := array.Elements[len(array.Elements)-1]

		array.Elements = array.Elements[:len(array.Elements)-1]

		vm.push(last)

	case "len":
		if argCount != 1 {
			langError(ErrorRuntime, "Array.len expects 1 argument")
		}

		array := asArray(vm.pop())
		vm.push(len(array.Elements))

	case "join":
		if argCount != 2 {
			langError(ErrorRuntime, "Array.join expects 2 arguments")
		}

		args := vm.popArgs(argCount)

		array, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "Array.join expects array, got %s", typeName(args[0]))
		}

		separator, ok := args[1].(string)
		if !ok {
			langError(ErrorType, "Array.join expects separator as string, got %s", typeName(args[1]))
		}
		var joined string

		for i, elem := range array.Elements {
			if i > 0 {
				joined += separator
			}
			joined += valueToString(elem)
		}

		vm.push(joined)

	default:
		langError(ErrorName, "unknown array function: %s", method)
	}
}

func (vm *VM) callString(method string, argCount int) {
	switch method {
	case "upper":
		if argCount != 1 {
			langError(ErrorRuntime, "String.upper expects 1 argument")
		}

		text := asString(vm.pop())
		vm.push(strings.ToUpper(text))

	case "lower":
		if argCount != 1 {
			langError(ErrorRuntime, "String.lower expects 1 argument")
		}

		text := asString(vm.pop())
		vm.push(strings.ToLower(text))

	case "trim":
		if argCount != 1 {
			langError(ErrorRuntime, "String.trim expects 1 argument")
		}

		text := asString(vm.pop())
		vm.push(strings.TrimSpace(text))

	case "contains":
		if argCount != 2 {
			langError(ErrorRuntime, "String.contains expects 2 arguments")
		}

		args := vm.popArgs(argCount)

		text := asString(args[0])
		search := asString(args[1])

		vm.push(strings.Contains(text, search))

	case "replace":
		if argCount != 3 {
			langError(ErrorRuntime, "String.replace expects 3 arguments")
		}

		args := vm.popArgs(argCount)

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.Replace(text, oldText, newText, 1))

	case "replaceAll":
		if argCount != 3 {
			langError(ErrorRuntime, "String.replaceAll expects 3 arguments")
		}

		args := vm.popArgs(argCount)

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.ReplaceAll(text, oldText, newText))

	case "len":
		if argCount != 1 {
			langError(ErrorRuntime, "String.len expects 1 argument")
		}

		text := asString(vm.pop())
		vm.push(len(text))

	default:
		langError(ErrorName, "unknown String function: %s", method)
	}
}

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	switch object {
	case "Core":
		vm.callCore(method, argCount)

	case "Math":
		vm.callMath(method, argCount)

	// case "Array":
	// 	vm.callArray(method, argCount)

	case "String":
		vm.callString(method, argCount)

	case "Plugin":
		vm.callPluginModule(method, argCount)

	default:
		langError(ErrorName, "unknown builtin module: %s", object)
	}
}
