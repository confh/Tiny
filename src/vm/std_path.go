package vm

import (
	"os"
	"path/filepath"

	. "language.com/src/tinyerrors"
)

var stdPathMetadata = StdModuleInfo{
	Name: "path",
}

var stdPathMethods map[string]StdModuleFunc

func init() {
	stdPathMethods = map[string]StdModuleFunc{
		"join":     pathJoin,
		"baseName": pathBaseName,
		"dirName":  pathDirName,
		"extName":  pathExtName,
		"cwd":      pathCwd,
	}
	registerStdModule(stdPathMetadata)
}

func (vm *VM) callStdPath(method string, args []TinyValue) {
	fn, ok := stdPathMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown path function: %s", method)
		return
	}

	fn(vm, args)
}

func pathJoin(vm *VM, args []TinyValue) {
	expectArgsMin(vm, "path.join", args, 1)

	parts := make([]string, len(args))
	for i := 0; i < len(args); i++ {
		parts[i] = argString(vm, "path.join", args, i)
	}

	joined := filepath.Join(parts...)
	vm.push(NewNative(joined))
}

func pathBaseName(vm *VM, args []TinyValue) {
	expectArgs(vm, "path.baseName", args, 1)

	path := argString(vm, "path.baseName", args, 0)

	vm.push(NewNative(filepath.Base(path)))
}

func pathDirName(vm *VM, args []TinyValue) {
	expectArgs(vm, "path.dirName", args, 1)

	directoryPath := argString(vm, "path.dirName", args, 0)

	vm.push(NewNative(filepath.Dir(directoryPath)))
}

func pathExtName(vm *VM, args []TinyValue) {
	expectArgs(vm, "path.extName", args, 1)

	path := argString(vm, "path.extName", args, 0)

	vm.push(NewNative(filepath.Ext(path)))
}

func pathCwd(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "path.cwd", args)

	dir, err := os.Getwd()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "could not get current working directory: %s", err)
		vm.push(NewNull())
		return
	}

	vm.push(NewNative(dir))
}
