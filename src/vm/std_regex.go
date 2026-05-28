package vm

import (
	"regexp"

	. "language.com/src/tinyerrors"
)

var stdRegexMetadata = StdModuleInfo{
	Name: "regex",
	Methods: map[string]StdMethodInfo{
		"matchString": {
			Name: "matchString",
			Args: []StdArg{
				{Name: "input", Type: "string", Optional: false},
				{Name: "pattern", Type: "string", Optional: false},
			},
			Returns:     "bool",
			Description: "Returns true if the input string matches the regex pattern.",
		},
		"findString": {
			Name: "findString",
			Args: []StdArg{
				{Name: "input", Type: "string", Optional: false},
				{Name: "pattern", Type: "string", Optional: false},
			},
			Returns:     "string",
			Description: "Returns the first substring match for the regex pattern in the input string.",
		},
	},
}

var stdRegexMethods map[string]StdModuleFunc

func init() {
	stdRegexMethods = map[string]StdModuleFunc{
		"matchString": stdRegexMatchString,
		"findString":  stdRegexFindString,
	}
	registerStdModule(stdRegexMetadata)
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
		return
	}
	vm.push(NewNative(re.MatchString(str)))
}

func stdRegexFindString(vm *VM, args []Value) {
	expectArgs(vm, "regex.findString", args, 2)

	str := argString(vm, "regex.findString", args, 0)
	pattern := argString(vm, "regex.findString", args, 1)

	re, err := regexp.Compile(pattern)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid regex: %s", err.Error())
		return
	}
	vm.push(NewNative(re.FindString(str)))
}
