package vm

import (
	"errors"
	"os"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdFs(method string, args []Value) {
	switch method {
	case "open":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "fs.open expects 1 argument")
		}

		path := asString(args[0])

		file, err := os.Open(path)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "failed to open file: %v", err)
		}

		vm.push(&NativeFileValue{
			File: file,
			Path: path,
		})

	case "readFile":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "fs.readFile expects 1 argument")
		}

		fileName := asString(args[0])

		data, err := os.ReadFile(fileName)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
		}

		vm.push(string(data))

	case "writeFile":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "fs.writeFile expects 2 arguments")
		}

		fileName := asString(args[0])
		data := valueToString(args[1])

		err := os.WriteFile(fileName, []byte(data), 0644)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
		}

		vm.push(true)

	case "writeBytes":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "fs.writeFile expects 2 arguments")
		}

		fileName := asString(args[0])
		data := asBuffer(args[1])

		err := os.WriteFile(fileName, data.Bytes, 0644)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
		}

		vm.push(true)

	case "exists":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "fs.exists expects 1 argument")
		}

		fileName := asString(args[0])

		_, err := os.Stat(fileName)

		if err == nil {
			vm.push(true)
		} else if errors.Is(err, os.ErrNotExist) {
			vm.push(false)
		} else {
			vm.runtimeError(ErrorRuntime, "something went wrong: %s", err)
		}

	case "readDir":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "fs.readDir expects 1 argument")
		}

		dirName := asString(args[0])

		files, err := os.ReadDir(dirName)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "error reading directory: %s", err)
		}

		fileNames := &ArrayValue{
			Elements: []Value{},
		}

		for _, file := range files {
			fileNames.Elements = append(fileNames.Elements, file.Name())
		}

		vm.push(fileNames)
	default:
		vm.runtimeError(ErrorName, "unknown fs function: %s", method)
	}
}
