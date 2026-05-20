package vm

import (
	"strings"

	. "language.com/src/tinyerrors"
)

var stringNativeMetadata = NativeTypeInfo{
	Name: "string",
	Methods: map[string]StdMethodInfo{
		"length": {
			Name:        "length",
			Returns:     "number",
			Description: "Returns the number of characters in the string.",
		},
		"toUpperCase": {
			Name:        "toUpperCase",
			Returns:     "string",
			Description: "Returns the string with all characters converted to upper case.",
		},
		"toLowerCase": {
			Name:        "toLowerCase",
			Returns:     "string",
			Description: "Returns the string with all characters converted to lower case.",
		},
		"upper": {
			Name:        "upper",
			Returns:     "string",
			Description: "Returns the string with the first character in upper case.",
		},
		"lower": {
			Name:        "lower",
			Returns:     "string",
			Description: "Returns the string with the first character in lower case.",
		},
		"split": {
			Name:        "split",
			Args:        []StdArg{{Name: "separator", Type: "string"}},
			Returns:     "array",
			Description: "Splits the string into an array of substrings using the specified separator.",
		},
		"includes": {
			Name:        "includes",
			Args:        []StdArg{{Name: "search", Type: "string"}},
			Returns:     "bool",
			Description: "Returns true if the string contains the given substring.",
		},
		"trim": {
			Name:        "trim",
			Returns:     "string",
			Description: "Removes whitespace from both ends of the string.",
		},
		"replace": {
			Name: "replace",
			Args: []StdArg{
				{Name: "oldValue", Type: "string"},
				{Name: "newValue", Type: "string"},
			},
			Returns:     "string",
			Description: "Replaces the first occurrence of oldValue with newValue in the string.",
		},
		"replaceAll": {
			Name: "replaceAll",
			Args: []StdArg{
				{Name: "oldValue", Type: "string"},
				{Name: "newValue", Type: "string"},
			},
			Returns:     "string",
			Description: "Replaces all occurrences of oldValue with newValue in the string.",
		},
	},
}

func init() {
	registerNativeType(stringNativeMetadata)

	stringMethods = map[string]NativeModuleFunc[string]{
		"length":      stringLength,
		"toUpperCase": stringToUpperCase,
		"toLowerCase": stringToLowerCase,
		"upper":       stringUpper,
		"lower":       stringLower,
		"split":       stringSplit,
		"includes":    stringIncludes,
		"trim":        stringTrim,
		"replace":     stringReplace,
		"replaceAll":  stringReplaceAll,
	}
}

var stringMethods map[string]NativeModuleFunc[string]

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

func stringUpper(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.upper", args, 0)

	result := strings.ToUpper(value[:1]) + value[1:]

	vm.push(result)
}

func stringLower(vm *VM, value string, args []Value) {
	expectArgs(vm, "string.lower", args, 0)

	result := strings.ToLower(value[:1]) + value[1:]

	vm.push(result)
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
