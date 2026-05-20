package vm

import (
	"strings"

	. "language.com/src/tinyerrors"
)

var stringMethods map[string]NativeModuleFunc[string]

func init() {
	stringMethods = map[string]NativeModuleFunc[string]{
		"length":      stringLength,
		"toUpperCase": stringToUpperCase,
		"toLowerCase": stringToLowerCase,
		"split":       stringSplit,
		"includes":    stringIncludes,
		"trim":        stringTrim,
		"replace":     stringReplace,
		"replaceAll":  stringReplaceAll,
	}
}

func (vm *VM) callStringMethod(value string, method string, args []Value) {
	fn, ok := stringMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown string method: %s", method)
		return
	}
	fn(vm, value, args)
}

func stringLength(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.length", args, 0)

	vm.push(len(value))
}

func stringToUpperCase(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.toUpperCase", args, 0)

	vm.push(strings.ToUpper(value))
}

func stringToLowerCase(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.toLowerCase", args, 0)

	vm.push(strings.ToLower(value))
}

func stringSplit(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.split", args, 1)

	separator := argString(vm, "string.split", args, 0)
	splitStrings := strings.Split(value, separator)
	elements := make([]Value, len(splitStrings))
	for i, s := range splitStrings {
		elements[i] = s
	}
	vm.push(&ArrayValue{Elements: elements})
}

func stringIncludes(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.includes", args, 1)

	search := argString(vm, "string.includes", args, 0)
	vm.push(strings.Contains(value, search))
}

func stringTrim(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.trim", args, 0)

	vm.push(strings.TrimSpace(value))
}

func stringReplace(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.replace", args, 2)

	oldText := argString(vm, "string.replace", args, 0)
	newText := argString(vm, "string.replace", args, 1)
	vm.push(strings.Replace(value, oldText, newText, 1))
}

func stringReplaceAll(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.replaceAll", args, 2)

	oldText := argString(vm, "string.replaceAll", args, 0)
	newText := argString(vm, "string.replaceAll", args, 1)
	vm.push(strings.ReplaceAll(value, oldText, newText))
}
