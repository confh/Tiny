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
			Returns:     "array:string",
			Description: "Splits the string into an array of substrings using the specified separator.",
		},
		"includes": {
			Name:        "includes",
			Args:        []StdArg{{Name: "search", Type: "string"}},
			Returns:     "bool",
			Description: "Returns true if the string contains the given substring.",
		},
		"startsWith": {
			Name:        "startsWith",
			Args:        []StdArg{{Name: "prefix", Type: "string"}},
			Returns:     "bool",
			Description: "Returns true if the string starts with the specified prefix.",
		},
		"endsWith": {
			Name:        "endsWith",
			Args:        []StdArg{{Name: "suffix", Type: "string"}},
			Returns:     "bool",
			Description: "Returns true if the string ends with the specified suffix.",
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
		"substring": {
			Name: "substring",
			Args: []StdArg{
				{Name: "start", Type: "number"},
				{Name: "end", Type: "number"},
			},
			Returns:     "string",
			Description: "Returns a substring from start to end. Negative indexes are treated as 0, indexes beyond the string length are clamped, and start/end are swapped if start is greater than end.",
		},
		"slice": {
			Name: "slice",
			Args: []StdArg{
				{Name: "start", Type: "number"},
				{Name: "end", Type: "number"},
			},
			Returns:     "string",
			Description: "Returns a slice from start to end. Negative indexes count from the end, indexes are clamped, and an empty string is returned if start is greater than end.",
		},
	},
}

func clampInt(n, min, max int) int {
	if n < min {
		return min
	}

	if n > max {
		return max
	}

	return n
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
		"startsWith":  stringStartsWith,
		"endsWith":    stringEndsWith,
		"trim":        stringTrim,
		"replace":     stringReplace,
		"replaceAll":  stringReplaceAll,
		"substring":   stringSubstring,
		"slice":       stringSlice,
	}
}

var stringMethods map[string]NativeModuleFunc[string]

func (vm *VM) callStringMethod(value string, method string, args []TinyValue) {
	fn, ok := stringMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown string method: %s", method)
		return
	}
	fn(vm, value, args)
}

func stringLength(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.length", args, 0)
	vm.push(NewInt(len(value)))
}

func stringToUpperCase(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.toUpperCase", args, 0)
	vm.push(NewNative(strings.ToUpper(value)))
}

func stringToLowerCase(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.toLowerCase", args, 0)
	vm.push(NewNative(strings.ToLower(value)))
}

func stringUpper(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.upper", args, 0)

	result := strings.ToUpper(value[:1]) + value[1:]

	vm.push(NewNative(result))
}

func stringLower(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.lower", args, 0)

	result := strings.ToLower(value[:1]) + value[1:]

	vm.push(NewNative(result))
}

func stringSplit(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.split", args, 1)
	separator := argString(vm, "string.split", args, 0)

	if separator == "" {
		runes := []rune(value)
		elements := make([]TinyValue, len(runes))
		for i, r := range runes {
			elements[i] = NewNative(string(r))
		}
		vm.push(NewNative(&ArrayValue{Elements: elements}))
		return
	}

	count := strings.Count(value, separator) + 1
	elements := make([]TinyValue, 0, count)

	for {
		idx := strings.Index(value, separator)
		if idx == -1 {
			elements = append(elements, NewNative(value))
			break
		}
		elements = append(elements, NewNative(value[:idx]))
		value = value[idx+len(separator):]
	}

	vm.push(NewNative(&ArrayValue{Elements: elements}))
}

func stringIncludes(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.includes", args, 1)

	search := argString(vm, "string.includes", args, 0)
	vm.push(NewNative(strings.Contains(value, search)))
}

func stringStartsWith(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.startsWith", args, 1)

	prefix := argString(vm, "string.startsWith", args, 0)
	vm.push(NewNative(strings.HasPrefix(value, prefix)))
}

func stringEndsWith(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.endsWith", args, 1)

	suffix := argString(vm, "string.endsWith", args, 0)
	vm.push(NewNative(strings.HasSuffix(value, suffix)))
}

func stringTrim(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.trim", args, 0)

	vm.push(NewNative(strings.TrimSpace(value)))
}

func stringReplace(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.replace", args, 2)

	oldText := argString(vm, "string.replace", args, 0)
	newText := argString(vm, "string.replace", args, 1)
	vm.push(NewNative(strings.Replace(value, oldText, newText, 1)))
}

func stringReplaceAll(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.replaceAll", args, 2)

	oldText := argString(vm, "string.replaceAll", args, 0)
	newText := argString(vm, "string.replaceAll", args, 1)
	vm.push(NewNative(strings.ReplaceAll(value, oldText, newText)))
}

func stringSubstring(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.substring", args, 2)

	start := argInt(vm, "string.substring", args, 0)
	end := argInt(vm, "string.substring", args, 1)

	runes := []rune(value)
	length := len(runes)

	start = clampInt(start, 0, length)
	end = clampInt(end, 0, length)

	if start > end {
		start, end = end, start
	}

	vm.push(NewNative(string(runes[start:end])))
}

func stringSlice(vm *VM, value string, args []TinyValue) {
	expectArgs(vm, "string.slice", args, 2)

	start := argInt(vm, "string.slice", args, 0)
	end := argInt(vm, "string.slice", args, 1)

	runes := []rune(value)
	length := len(runes)

	if start < 0 {
		start = length + start
	}

	if end < 0 {
		end = length + end
	}

	start = clampInt(start, 0, length)
	end = clampInt(end, 0, length)

	if start > end {
		vm.push(NewNative(""))
		return
	}

	vm.push(NewNative(string(runes[start:end])))
}
