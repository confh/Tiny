package vm

import (
	"math/rand"
	"strings"

	. "language.com/src/tinyerrors"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

func generateRandomString(length int) string {
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

func isDigit(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func (vm *VM) callStdString(method string, args []Value) {
	switch method {
	case "isDigit":
		if (len(args)) != 1 {
			vm.runtimeError(ErrorRuntime, "string.isDigit expects 1 argument")
		}

		value := asString(args[0], vm)

		vm.push(isDigit(value))
	case "random":
		if (len(args)) != 1 {
			vm.runtimeError(ErrorRuntime, "string.random expects 1 argument")
		}

		length := asInt(args[0])

		vm.push(generateRandomString(length))

	case "upper":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "String.upper expects 1 argument")
		}

		text := asString(args[0], vm)
		vm.push(strings.ToUpper(text))

	case "lower":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "String.lower expects 1 argument")
		}

		text := asString(args[0], vm)
		vm.push(strings.ToLower(text))

	case "trim":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "String.trim expects 1 argument")
		}

		text := asString(args[0], vm)
		vm.push(strings.TrimSpace(text))

	case "contains":
		if len(args) != 2 {
			vm.runtimeError(ErrorRuntime, "String.contains expects 2 arguments")
		}

		text := asString(args[0], vm)
		search := asString(args[1], vm)

		vm.push(strings.Contains(text, search))

	case "replace":
		if len(args) != 3 {
			vm.runtimeError(ErrorRuntime, "String.replace expects 3 arguments")
		}

		text := asString(args[0], vm)
		oldText := asString(args[1], vm)
		newText := asString(args[2], vm)

		vm.push(strings.Replace(text, oldText, newText, 1))

	case "replaceAll":
		if len(args) != 3 {
			vm.runtimeError(ErrorRuntime, "String.replaceAll expects 3 arguments")
		}

		text := asString(args[0], vm)
		oldText := asString(args[1], vm)
		newText := asString(args[2], vm)

		vm.push(strings.ReplaceAll(text, oldText, newText))

	case "len":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "String.len expects 1 argument")
		}

		text := asString(args[0], vm)
		vm.push(len(text))

	default:
		vm.runtimeError(ErrorName, "unknown String function: %s", method)
	}
}
