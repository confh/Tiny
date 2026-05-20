package vm

import (
	"errors"
	"os"

	. "language.com/src/tinyerrors"
)

var stdFsMethods = map[string]StdModuleFunc{
	"open":       stdFsOpen,
	"readFile":   stdFsReadFile,
	"writeFile":  stdFsWriteFile,
	"writeBytes": stdFsWriteBytes,
	"exists":     stdFsExists,
	"readDir":    stdFsReadDir,
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
