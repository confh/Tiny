package vm

import (
	"net/url"

	. "language.com/src/tinyerrors"
)

var stdUrlMetadata = StdModuleInfo{
	Name: "url",
}

var stdUrlMethods map[string]StdModuleFunc

func init() {
	stdUrlMethods = map[string]StdModuleFunc{
		"encode": urlEncode,
		"decode": urlDecode,
	}
	registerStdModule(stdUrlMetadata)
}

func (vm *VM) callStdUrl(method string, args []TinyValue) {
	fn, ok := stdUrlMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown url function: %s", method)
		return
	}

	fn(vm, args)
}

func urlEncode(vm *VM, args []TinyValue) {
	expectArgs(vm, "url.encode", args, 1)

	str := argString(vm, "url.encode", args, 0)

	encoded := url.QueryEscape(str)

	vm.push(NewNative(encoded))
}

func urlDecode(vm *VM, args []TinyValue) {
	expectArgs(vm, "url.decode", args, 1)

	str := argString(vm, "url.decode", args, 0)

	decoded, err := url.QueryUnescape(str)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while decoding '%s': %s", str, err.Error())
		vm.push(NewNull())
		return
	}

	vm.push(NewNative(decoded))
}
