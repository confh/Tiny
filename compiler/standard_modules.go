package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
)

func (vm *VM) callStandardModule(module string, method string, args []Value) {
	switch module {
	case "array":
		vm.callStdArray(method, args)

	case "math":
		vm.callStdMath(method, args)

	case "string":
		vm.callStdString(method, args)

	case "json":
		vm.callStdJson(method, args)

	case "fs":
		vm.callStdFs(method, args)

	case "app":
		vm.callStdApp(method, args)

	case "task":
		vm.callTaskModule(method, args)

	default:
		langError(ErrorName, "unknown standard module: %s", module)
	}
}

func (vm *VM) callStdApp(method string, args []Value) {
	switch method {
	case "new":
		if len(args) != 1 {
			langError(ErrorRuntime, "app.new expects 1 argument")
		}

		name := asString(args[0])

		vm.push(&NativeAppValue{
			Name:     name,
			Commands: map[string]FunctionValue{},
		})

	default:
		langError(ErrorName, "unknown app function: %s", method)
	}
}

func (vm *VM) callStdFs(method string, args []Value) {
	switch method {
	case "open":
		if len(args) != 1 {
			langError(ErrorRuntime, "fs.open expects 1 argument")
		}

		path := asString(args[0])

		file, err := os.Open(path)
		if err != nil {
			langError(ErrorRuntime, "failed to open file: %v", err)
		}

		vm.push(&NativeFileValue{
			File: file,
			Path: path,
		})

	case "read":
		if len(args) != 2 {
			langError(ErrorRuntime, "fs.read expects 2 arguments")
		}

		file, ok := args[0].(*NativeFileValue)
		if !ok {
			langError(ErrorType, "fs.read expects file, got %s", typeName(args[0]))
		}

		if file.Closed {
			langError(ErrorRuntime, "cannot read closed file")
		}

		size := asInt(args[1])

		if size <= 0 {
			langError(ErrorRuntime, "read size must be greater than 0")
		}

		buffer := make([]byte, size)

		n, err := file.File.Read(buffer)
		if err != nil && !errors.Is(err, io.EOF) {
			langError(ErrorRuntime, "failed to read file: %v", err)
		}

		vm.push(string(buffer[:n]))

	case "close":
		if len(args) != 1 {
			langError(ErrorRuntime, "fs.close expects 1 argument")
		}

		file, ok := args[0].(*NativeFileValue)
		if !ok {
			langError(ErrorType, "fs.close expects file, got %s", typeName(args[0]))
		}

		if !file.Closed {
			err := file.File.Close()
			if err != nil {
				langError(ErrorRuntime, "failed to close file: %v", err)
			}

			file.Closed = true
		}

		vm.push(true)

	case "readFile":
		if len(args) != 1 {
			langError(ErrorRuntime, "fs.readFile expects 1 argument")
		}

		fileName := asString(args[0])

		data, err := os.ReadFile(fileName)
		if err != nil {
			langError(ErrorRuntime, "error reading file: %s", err)
		}

		vm.push(string(data))

	case "writeFile":
		if len(args) != 2 {
			langError(ErrorRuntime, "fs.writeFile expects 2 arguments")
		}

		fileName := asString(args[0])
		data := valueToString(args[1])

		err := os.WriteFile(fileName, []byte(data), 0644)
		if err != nil {
			langError(ErrorRuntime, "error writing file: %s", err)
		}

		vm.push(0)

	case "exists":
		if len(args) != 1 {
			langError(ErrorRuntime, "fs.exists expects 1 argument")
		}

		fileName := asString(args[0])

		_, err := os.Stat(fileName)

		if err == nil {
			vm.push(true)
		} else if errors.Is(err, os.ErrNotExist) {
			vm.push(false)
		} else {
			langError(ErrorRuntime, "something went wrong: %s", err)
		}

	case "readDir":
		if len(args) != 1 {
			langError(ErrorRuntime, "fs.readDir expects 1 argument")
		}

		dirName := asString(args[0])

		files, err := os.ReadDir(dirName)
		if err != nil {
			langError(ErrorRuntime, "error reading directory: %s", err)
		}

		fileNames := &ArrayValue{
			Elements: []Value{},
		}

		for _, file := range files {
			fileNames.Elements = append(fileNames.Elements, file.Name())
		}

		vm.push(fileNames)
	default:
		langError(ErrorName, "unknown fs function: %s", method)
	}
}

func (vm *VM) callStdJson(method string, args []Value) {
	switch method {
	case "stringify":
		if len(args) != 1 {
			langError(ErrorRuntime, "json.stringify expects 1 argument")
		}

		value := args[0]

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))

	case "pretty":
		if len(args) != 1 {
			langError(ErrorRuntime, "json.pretty expects 1 argument")
		}

		value := args[0]

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))
	case "parse":
		if len(args) != 1 {
			langError(ErrorRuntime, "json.parse expects 1 argument")
		}

		stringified := asString(args[0])

		var result any

		err := json.Unmarshal([]byte(stringified), &result)
		if err != nil {
			langError(ErrorRuntime, "invalid JSON: %v", err)
		}

		vm.push(jsonToTinyValue(result))
	default:
		langError(ErrorName, "unknown json function: %s", method)
	}
}

func (vm *VM) callStdString(method string, args []Value) {
	switch method {
	case "upper":
		if len(args) != 1 {
			langError(ErrorRuntime, "String.upper expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.ToUpper(text))

	case "lower":
		if len(args) != 1 {
			langError(ErrorRuntime, "String.lower expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.ToLower(text))

	case "trim":
		if len(args) != 1 {
			langError(ErrorRuntime, "String.trim expects 1 argument")
		}

		text := asString(args[0])
		vm.push(strings.TrimSpace(text))

	case "contains":
		if len(args) != 2 {
			langError(ErrorRuntime, "String.contains expects 2 arguments")
		}

		text := asString(args[0])
		search := asString(args[1])

		vm.push(strings.Contains(text, search))

	case "replace":
		if len(args) != 3 {
			langError(ErrorRuntime, "String.replace expects 3 arguments")
		}

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.Replace(text, oldText, newText, 1))

	case "replaceAll":
		if len(args) != 3 {
			langError(ErrorRuntime, "String.replaceAll expects 3 arguments")
		}

		text := asString(args[0])
		oldText := asString(args[1])
		newText := asString(args[2])

		vm.push(strings.ReplaceAll(text, oldText, newText))

	case "len":
		if len(args) != 1 {
			langError(ErrorRuntime, "String.len expects 1 argument")
		}

		text := asString(args[0])
		vm.push(len(text))

	default:
		langError(ErrorName, "unknown String function: %s", method)
	}
}

func (vm *VM) callStdMath(method string, args []Value) {
	switch method {
	case "toFloat":
		if len(args) != 1 {
			langError(ErrorRuntime, "math.toFloat expects 1 argument")
		}

		vm.push(asFloat(args[0]))

	case "toInt":
		if len(args) != 1 {
			langError(ErrorRuntime, "math.toInt expects 1 argument")
		}

		vm.push(int(asFloat(args[0])))

	default:
		langError(ErrorName, "unknown math function: %s", method)
	}
}

func (vm *VM) callStdArray(method string, args []Value) {
	switch method {
	case "push":
		if len(args) != 2 {
			langError(ErrorRuntime, "array.push expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.push expects array, got %s", typeName(args[0]))
		}

		arr.Elements = append(arr.Elements, args[1])

		vm.push(arr)

	case "pop":
		if len(args) != 1 {
			langError(ErrorRuntime, "array.pop expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.pop expects array, got %s", typeName(args[0]))
		}

		if len(arr.Elements) == 0 {
			vm.push(UndefinedValue{})
			return
		}

		last := arr.Elements[len(arr.Elements)-1]
		arr.Elements = arr.Elements[:len(arr.Elements)-1]

		vm.push(last)

	case "len":
		if len(args) != 1 {
			langError(ErrorRuntime, "array.len expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.len expects array, got %s", typeName(args[0]))
		}

		vm.push(len(arr.Elements))

	case "get":
		if len(args) != 2 {
			langError(ErrorRuntime, "array.get expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.get expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			langError(ErrorRuntime, "array index out of range: %d", index)
		}

		vm.push(arr.Elements[index])

	case "set":
		if len(args) != 3 {
			langError(ErrorRuntime, "array.set expects 3 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.set expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			langError(ErrorRuntime, "array index out of range: %d", index)
		}

		arr.Elements[index] = args[2]

		vm.push(arr)

	case "contains":
		if len(args) != 2 {
			langError(ErrorRuntime, "array.contains expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.contains expects array, got %s", typeName(args[0]))
		}

		element := args[1]

		vm.push(slices.Contains(arr.Elements, element))

	default:
		langError(ErrorName, "unknown array function: %s", method)
	}
}
