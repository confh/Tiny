package vm

import (
	"math/rand"
	"strings"

	. "language.com/src/tinyerrors"
)

const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

var stdStringMethods = map[string]StdModuleFunc{
	"newBuilder": stdStringNewBuilder,
	"isDigit":    stdStringIsDigit,
	"random":     stdStringRandom,
}

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
	fn, ok := stdStringMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown String function: %s", method)
		return
	}
	fn(vm, args)
}

func stdStringNewBuilder(vm *VM, args []Value) {
	dontExpectArgs(vm, "string.newBuilder", args)

	sb := &strings.Builder{}
	vm.push(&NativeStringBuilderValue{
		Builder: sb,
	})
}

func stdStringIsDigit(vm *VM, args []Value) {
	expectArgs(vm, "string.isDigit", args, 1)

	value := argString(vm, "string.isDigit", args, 0)
	vm.push(isDigit(value))
}

func stdStringRandom(vm *VM, args []Value) {
	expectArgs(vm, "string.random", args, 1)

	length := argInt(vm, "string.random", args, 0)
	vm.push(generateRandomString(length))
}
