package vm

import (
	"fmt"

	. "language.com/src/tinyerrors"
)

var stdTestMetadata = StdModuleInfo{
	Name: "test",
	Methods: map[string]StdMethodInfo{
		"assert": {
			Name:        "assert",
			Args:        []StdArg{{Name: "condition", Type: "bool"}, {Name: "message", Type: "string"}},
			Returns:     "undefined",
			Description: "Asserts that a condition is true; throws with the given message if not.",
		},
		"equal": {
			Name:        "equal",
			Args:        []StdArg{{Name: "actual", Type: "any"}, {Name: "expected", Type: "any"}, {Name: "message", Type: "string"}},
			Returns:     "undefined",
			Description: "Asserts that two values are equal (including int equality); throws with the given message if not.",
		},
		"notEqual": {
			Name:        "notEqual",
			Args:        []StdArg{{Name: "actual", Type: "any"}, {Name: "expected", Type: "any"}, {Name: "message", Type: "string"}},
			Returns:     "undefined",
			Description: "Asserts that two values are not equal; throws with the given message if they are equal.",
		},
		"run": {
			Name:        "run",
			Args:        []StdArg{{Name: "name", Type: "string"}, {Name: "fn", Type: "function"}},
			Returns:     "undefined",
			Description: "Runs a test function and prints PASS/FAIL messages. Catches assertion failures.",
		},
	},
}

var stdTestMethods map[string]StdModuleFunc

func init() {
	stdTestMethods = map[string]StdModuleFunc{
		"assert":   testAssert,
		"equal":    testEqual,
		"notEqual": testNotEqual,
		"run":      testRun,
	}
	registerStdModule(stdTestMetadata)
}

func (vm *VM) callStdTest(method string, args []Value) {
	fn, ok := stdTestMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown test function: %s", method)
		return
	}

	fn(vm, args)
}

func testAssert(vm *VM, args []Value) {
	expectArgs(vm, "test.assert", args, 2)

	condition := argBool(vm, "test.assert", args, 0)
	message := argString(vm, "test.assert", args, 1)

	if !condition {
		vm.runtimeError(ErrorRuntime, "%s", message)
	}

	vm.push(NewUndefined())
}

func testEqual(vm *VM, args []Value) {
	expectArgs(vm, "test.equal", args, 3)

	actual := args[0]
	expected := args[1]
	message := argString(vm, "test.equal", args, 2)

	equal := false

	if actual.IsInt && expected.IsInt && actual.AsInt == expected.AsInt {
		equal = true
	}

	if actual.Value == expected.Value {
		equal = true
	}

	if !equal {
		vm.runtimeError(ErrorRuntime, "%s", message)
	}

	vm.push(NewUndefined())
}

func testNotEqual(vm *VM, args []Value) {
	expectArgs(vm, "test.notEqual", args, 3)

	actual := args[0]
	expected := args[1]
	message := argString(vm, "test.notEqual", args, 2)

	equal := false

	if actual.IsInt && expected.IsInt && actual.AsInt == expected.AsInt {
		equal = true
	}

	if actual.Value == expected.Value {
		equal = true
	}

	if equal {
		vm.runtimeError(ErrorRuntime, "%s", message)
	}

	vm.push(NewUndefined())
}

func testRun(vm *VM, args []Value) {
	expectArgs(vm, "test.run", args, 2)
	name := argString(vm, "test.run", args, 0)
	fn := argFn(vm, "test.run", args, 1)

	testFailed := false
	var failureMessage string

	func() {
		defer func() {
			if r := recover(); r != nil {
				testFailed = true
				if langErr, ok := r.(LangErrorType); ok {
					failureMessage = langErr.Message
				} else {
					failureMessage = fmt.Sprintf("Go System Panic: %v", r)
				}
			}
		}()

		vm.callFunctionValue(fn, []Value{})
	}()

	if testFailed {
		fmt.Printf("❌ FAIL: %s\n%s\n", name, failureMessage)
	} else {
		fmt.Printf("✅ PASS: %s\n", name)
	}

	vm.push(NewUndefined())
}
