package vm

import (
	"regexp"

	. "language.com/src/tinyerrors"
)

var stdRegexMethods = map[string]StdModuleFunc{
	"matchString": stdRegexMatchString,
	"findString":  stdRegexFindString,
}

func (vm *VM) callStdRegex(method string, args []Value) {
	fn, ok := stdRegexMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown regex function: %s", method)
		return
	}
	fn(vm, args)
}

func stdRegexMatchString(vm *VM, args []Value) {
	expectArgs(vm, "regex.matchString", args, 2)

	str := argString(vm, "regex.matchString", args, 0)
	pattern := argString(vm, "regex.matchString", args, 1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid regex: %s", err.Error())
	}
	vm.push(re.MatchString(str))
}

func stdRegexFindString(vm *VM, args []Value) {
	expectArgs(vm, "regex.findString", args, 2)

	str := argString(vm, "regex.findString", args, 0)
	pattern := argString(vm, "regex.findString", args, 1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid regex: %s", err.Error())
	}
	vm.push(re.FindString(str))
}
