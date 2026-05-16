package vm

import (
	"errors"
	"io"
	"os"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdFs(method string, args []Value) {
	switch method {
	case "open":
		if len(args) != 1 {
			LangError(ErrorRuntime, "fs.open expects 1 argument")
		}

		path := asString(args[0])

		file, err := os.Open(path)
		if err != nil {
			LangError(ErrorRuntime, "failed to open file: %v", err)
		}

		vm.push(&NativeFileValue{
			File: file,
			Path: path,
		})

	case "read":
		if len(args) != 2 {
			LangError(ErrorRuntime, "fs.read expects 2 arguments")
		}

		file, ok := args[0].(*NativeFileValue)
		if !ok {
			LangError(ErrorType, "fs.read expects file, got %s", typeName(args[0]))
		}

		if file.Closed {
			LangError(ErrorRuntime, "cannot read closed file")
		}

		size := asInt(args[1])

		if size <= 0 {
			LangError(ErrorRuntime, "read size must be greater than 0")
		}

		buffer := make([]byte, size)

		n, err := file.File.Read(buffer)
		if err != nil && !errors.Is(err, io.EOF) {
			LangError(ErrorRuntime, "failed to read file: %v", err)
		}

		vm.push(string(buffer[:n]))

	case "close":
		if len(args) != 1 {
			LangError(ErrorRuntime, "fs.close expects 1 argument")
		}

		file, ok := args[0].(*NativeFileValue)
		if !ok {
			LangError(ErrorType, "fs.close expects file, got %s", typeName(args[0]))
		}

		if !file.Closed {
			err := file.File.Close()
			if err != nil {
				LangError(ErrorRuntime, "failed to close file: %v", err)
			}

			file.Closed = true
		}

		vm.push(true)

	case "readFile":
		if len(args) != 1 {
			LangError(ErrorRuntime, "fs.readFile expects 1 argument")
		}

		fileName := asString(args[0])

		data, err := os.ReadFile(fileName)
		if err != nil {
			LangError(ErrorRuntime, "error reading file: %s", err)
		}

		vm.push(string(data))

	case "writeFile":
		if len(args) != 2 {
			LangError(ErrorRuntime, "fs.writeFile expects 2 arguments")
		}

		fileName := asString(args[0])
		data := valueToString(args[1])

		err := os.WriteFile(fileName, []byte(data), 0644)
		if err != nil {
			LangError(ErrorRuntime, "error writing file: %s", err)
		}

		vm.push(true)

	case "writeBytes":
		if len(args) != 2 {
			LangError(ErrorRuntime, "fs.writeFile expects 2 arguments")
		}

		fileName := asString(args[0])
		data := asBuffer(args[1])

		err := os.WriteFile(fileName, data.Bytes, 0644)
		if err != nil {
			LangError(ErrorRuntime, "error writing file: %s", err)
		}

		vm.push(true)

	case "exists":
		if len(args) != 1 {
			LangError(ErrorRuntime, "fs.exists expects 1 argument")
		}

		fileName := asString(args[0])

		_, err := os.Stat(fileName)

		if err == nil {
			vm.push(true)
		} else if errors.Is(err, os.ErrNotExist) {
			vm.push(false)
		} else {
			LangError(ErrorRuntime, "something went wrong: %s", err)
		}

	case "readDir":
		if len(args) != 1 {
			LangError(ErrorRuntime, "fs.readDir expects 1 argument")
		}

		dirName := asString(args[0])

		files, err := os.ReadDir(dirName)
		if err != nil {
			LangError(ErrorRuntime, "error reading directory: %s", err)
		}

		fileNames := &ArrayValue{
			Elements: []Value{},
		}

		for _, file := range files {
			fileNames.Elements = append(fileNames.Elements, file.Name())
		}

		vm.push(fileNames)
	default:
		LangError(ErrorName, "unknown fs function: %s", method)
	}
}
