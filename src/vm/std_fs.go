package vm

import (
	"errors"
	"io"
	"os"
	"path/filepath"

	. "language.com/src/tinyerrors"
)

func (v *NativeFileValue) TinyTypeName() string {
	return "fs.File"
}

var stdFsMetadata = StdModuleInfo{
	Name: "fs",
}

var stdFsMethods map[string]StdModuleFunc

func init() {
	stdFsMethods = map[string]StdModuleFunc{
		"open":       stdFsOpen,
		"readFile":   stdFsReadFile,
		"writeFile":  stdFsWriteFile,
		"writeBytes": stdFsWriteBytes,
		"exists":     stdFsExists,
		"readDir":    stdFsReadDir,
		"mkDir":      stdFsMkDir,
		"stat":       stdFsStat,
		"copy":       stdFsCopy,
		"remove":     stdFsRemove,
	}
	registerStdModule(stdFsMetadata)
}

func (vm *VM) callStdFs(method string, args []TinyValue) {
	fn, ok := stdFsMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown fs function: %s", method)
		return
	}
	fn(vm, args)
}

func stdFsOpen(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.open", args, 1)

	path := argString(vm, "fs.open", args, 0)
	file, err := os.Open(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to open file: %v", err)
	}
	vm.push(NewNative(&NativeFileValue{
		File: file,
		Path: path,
	}))
}

func stdFsReadFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.readFile", args, 1)

	fileName := argString(vm, "fs.readFile", args, 0)
	data, err := os.ReadFile(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
	}
	vm.push(NewNative(string(data)))
}

func stdFsWriteFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.writeFile", args, 2)

	fileName := argString(vm, "fs.writeFile", args, 0)
	data := argString(vm, "fs.writeFile", args, 1)
	err := os.WriteFile(fileName, []byte(data), 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
	}
	vm.push(NewNative(true))
}

func stdFsWriteBytes(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.writeBytes", args, 2)

	fileName := argString(vm, "fs.writeBytes", args, 0)
	data := asBuffer(args[1], vm)
	err := os.WriteFile(fileName, data.Bytes, 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing file: %s", err)
	}
	vm.push(NewNative(true))
}

func stdFsExists(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.exists", args, 1)

	fileName := argString(vm, "fs.exists", args, 0)
	_, err := os.Stat(fileName)
	if err == nil {
		vm.push(NewNative(true))
	} else if errors.Is(err, os.ErrNotExist) {
		vm.push(NewNative(false))
	} else {
		vm.runtimeError(ErrorRuntime, "something went wrong: %s", err)
	}
}

func stdFsReadDir(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.readDir", args, 1)

	dirName := argString(vm, "fs.readDir", args, 0)
	files, err := os.ReadDir(dirName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading directory: %s", err)
	}
	fileNames := &ArrayValue{
		Elements: []TinyValue{},
	}
	for _, file := range files {
		fileNames.Elements = append(fileNames.Elements, NewNative(file.Name()))
	}
	vm.push(NewNative(fileNames))
}

func stdFsMkDir(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.mkDir", args, 1)

	dirName := argString(vm, "fs.mkDir", args, 0)
	err := os.Mkdir(dirName, 0755)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error creating directory: %s", err)
	}

	vm.push(NewNull())
}

func stdFsStat(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.stat", args, 1)

	path := argString(vm, "fs.stat", args, 0)
	fileInfo, err := os.Stat(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error checking file stat: %s", err)
	}

	vm.push(NewNative(ObjectValue{
		"name":    NewNative(fileInfo.Name()),
		"size":    NewInt(int(fileInfo.Size())),
		"isDir":   NewNative(fileInfo.IsDir()),
		"modTime": NewNative(fileInfo.ModTime()),
	}))
}

func stdFsCopy(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.copy", args, 2)

	src := argString(vm, "fs.copy", args, 0)
	dst := argString(vm, "fs.copy", args, 1)

	srcAbs, err := filepath.Abs(src)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to resolve source path: %v", err)
		return
	}

	dstAbs, err := filepath.Abs(dst)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to resolve destination path: %v", err)
		return
	}

	if srcAbs == dstAbs {
		vm.runtimeError(ErrorRuntime, "fs.copy source and destination are the same file: %s", src)
		return
	}

	srcInfo, err := os.Stat(srcAbs)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to stat source file: %v", err)
		return
	}

	if srcInfo.IsDir() {
		vm.runtimeError(ErrorRuntime, "fs.copy source is a directory: %s", src)
		return
	}

	if srcInfo.Size() == 0 {
		vm.runtimeError(ErrorRuntime, "fs.copy source file is empty: %s", src)
		return
	}

	err = os.MkdirAll(filepath.Dir(dstAbs), 0755)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to create destination directory: %v", err)
		return
	}

	source, err := os.Open(srcAbs)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while opening source file: %v", err)
		return
	}
	defer source.Close()

	destination, err := os.Create(dstAbs)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while creating destination file: %v", err)
		return
	}
	defer destination.Close()

	n, err := io.Copy(destination, source)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while copying file: %v", err)
		return
	}

	err = destination.Sync()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while flushing destination file: %v", err)
		return
	}

	vm.push(NewInt(int(n)))
}

func stdFsRemove(vm *VM, args []TinyValue) {
	expectArgs(vm, "fs.remove", args, 1)

	path := argString(vm, "fs.remove", args, 0)

	err := os.Remove(path)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while removing file: %s", err)
	}

	vm.push(NewNull())
}
