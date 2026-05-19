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
	case "newBuilder":
		if len(args) != 0 {
			vm.runtimeError(ErrorRuntime, "string.newBuilder expects 0 arguments")
		}

		sb := &strings.Builder{}
		vm.push(&NativeStringBuilderValue{
			Builder: sb,
		})

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

	default:
		vm.runtimeError(ErrorName, "unknown String function: %s", method)
	}
}
