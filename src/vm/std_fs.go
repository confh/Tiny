package vm

import (
	"errors"
	"os"

	. "language.com/src/tinyerrors"
)

var stdFsMetadata = StdModuleInfo{
	Name: "fs",
	Methods: map[string]StdMethodInfo{
		"open": {
			Name: "open",
			Args: []StdArg{
				{Name: "path", Type: "string", Optional: false},
			},
			Returns:     "File",
			Description: "Opens a file and returns a file object.",
		},
		"readFile": {
			Name: "readFile",
			Args: []StdArg{
				{Name: "fileName", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Reads an entire file and returns its contents as a string.",
		},
		"writeFile": {
			Name: "writeFile",
			Args: []StdArg{
				{Name: "fileName", Type: "string", Optional: false},
				{Name: "data", Type: "string", Optional: false},
			},
			Returns:     "bool",
			Description: "Writes the provided string to a file, returning true if successful.",
		},
		"writeBytes": {
			Name: "writeBytes",
			Args: []StdArg{
				{Name: "fileName", Type: "string", Optional: false},
				{Name: "buffer", Type: "buffer", Optional: false},
			},
			Returns:     "bool",
			Description: "Writes the buffer's bytes to a file, returning true if successful.",
		},
		"exists": {
			Name: "exists",
			Args: []StdArg{
				{Name: "fileName", Type: "string", Optional: false},
			},
			Returns:     "bool",
			Description: "Returns true if the file (or directory) exists, false otherwise.",
		},
		"readDir": {
			Name: "readDir",
			Args: []StdArg{
				{Name: "dirName", Type: "string", Optional: false},
			},
			Returns:     "Array",
			Description: "Returns an array of filenames in the given directory.",
		},
		"mkDir": {
			Name: "mkDir",
			Args: []StdArg{
				{Name: "dirName", Type: "string", Optional: false},
			},
			Returns:     "undefined",
			Description: "Creates a new directory.",
		},
	},
}

var stdFsMethods = map[string]StdModuleFunc{
	"open":       stdFsOpen,
	"readFile":   stdFsReadFile,
	"writeFile":  stdFsWriteFile,
	"writeBytes": stdFsWriteBytes,
	"exists":     stdFsExists,
	"readDir":    stdFsReadDir,
	"mkDir":      stdFsMkDir,
}

func init() {
	registerStdModule(stdFsMetadata)
}

func (vm *VM) callStdFs(method string, args []Value) {
	fn, ok := stdFsMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown fs function: %s", method)
		return
	}
	fn(vm, args)
}

func stdFsOpen(vm *VM, args []Value) {
	expectArgs(vm, "fs.open", args, 1)

	path := argString(vm, "fs.open", args, 0)
	file, err := os.Open(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to open file: %v", err)
	}
	vm.push(&NativeFileValue{
		File: file,
		Path: path,
	})
}

func stdFsReadFile(vm *VM, args []Value) {
	expectArgs(vm, "fs.readFile", args, 1)

	fileName := argString(vm, "fs.readFile", args, 0)
	data, err := os.ReadFile(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
	}
	vm.push(string(data))
}

func stdFsWriteFile(vm *VM, args []Value) {
	expectArgs(vm, "fs.writeFile", args, 2)

	fileName := argString(vm, "fs.writeFile", args, 0)
	data := argString(vm, "fs.writeFile", args, 1)
	err := os.WriteFile(fileName, []byte(data), 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
	}
	vm.push(true)
}

func stdFsWriteBytes(vm *VM, args []Value) {
	expectArgs(vm, "fs.writeBytes", args, 2)

	fileName := argString(vm, "fs.writeBytes", args, 0)
	data := asBuffer(args[1], vm)
	err := os.WriteFile(fileName, data.Bytes, 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
	}
	vm.push(true)
}

func stdFsExists(vm *VM, args []Value) {
	expectArgs(vm, "fs.exists", args, 1)

	fileName := argString(vm, "fs.exists", args, 0)
	_, err := os.Stat(fileName)
	if err == nil {
		vm.push(true)
	} else if errors.Is(err, os.ErrNotExist) {
		vm.push(false)
	} else {
		vm.runtimeError(ErrorRuntime, "something went wrong: %s", err)
	}
}

func stdFsReadDir(vm *VM, args []Value) {
	expectArgs(vm, "fs.readDir", args, 1)

	dirName := argString(vm, "fs.readDir", args, 0)
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
}

func stdFsMkDir(vm *VM, args []Value) {
	expectArgs(vm, "fs.mkDir", args, 1)

	dirName := argString(vm, "fs.mkDir", args, 0)
	err := os.Mkdir(dirName, 0755)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error creating directory: %s", err)
	}

	vm.push(UndefinedValue{})
}
