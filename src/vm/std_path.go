package vm

import (
	"os"
	"path/filepath"

	. "language.com/src/tinyerrors"
)

var stdPathMetadata = StdModuleInfo{
	Name: "path",
	Methods: map[string]StdMethodInfo{
		"join": {
			Name: "join",
			Args: []StdArg{
				{Name: "paths", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Joins one or more path segments into a single path.",
		},
		"baseName": {
			Name: "baseName",
			Args: []StdArg{
				{Name: "path", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Returns the last element of the path. Trailing slashes are removed before extracting the last element.",
		},
		"dirName": {
			Name: "dirName",
			Args: []StdArg{
				{Name: "path", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Returns all but the last element of the path, usually the directory.",
		},
		"extName": {
			Name: "extName",
			Args: []StdArg{
				{Name: "path", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Returns the file name extension used by path.",
		},
		"cwd": {
			Name:        "cwd",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns the current working directory.",
		},
	},
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

func (vm *VM) callStdPath(method string, args []Value) {
	fn, ok := stdPathMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown path function: %s", method)
		return
	}

	fn(vm, args)
}

func pathJoin(vm *VM, args []Value) {
	expectArgsMin(vm, "path.join", args, 1)

	parts := make([]string, len(args))
	for i := 0; i < len(args); i++ {
		parts[i] = argString(vm, "path.join", args, i)
	}

	joined := filepath.Join(parts...)
	vm.push(joined)
}

func pathBaseName(vm *VM, args []Value) {
	expectArgs(vm, "path.baseName", args, 1)

	path := argString(vm, "path.baseName", args, 0)

	vm.push(filepath.Base(path))
}

func pathDirName(vm *VM, args []Value) {
	expectArgs(vm, "path.dirName", args, 1)

	directoryPath := argString(vm, "path.dirName", args, 0)

	vm.push(filepath.Dir(directoryPath))
}

func pathExtName(vm *VM, args []Value) {
	expectArgs(vm, "path.extName", args, 1)

	path := argString(vm, "path.extName", args, 0)

	vm.push(filepath.Ext(path))
}

func pathCwd(vm *VM, args []Value) {
	dontExpectArgs(vm, "path.cwd", args)

	dir, err := os.Getwd()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "could not get current working directory: %s", err)
	}

	vm.push(dir)
}
