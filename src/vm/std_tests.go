package vm

import (
	"fmt"
	"time"

	. "language.com/src/tinyerrors"
)

var stdTestMetadata = StdModuleInfo{
	Name: "tests",
}

var stdTestMethods map[string]StdModuleFunc

func init() {
	stdTestMethods = map[string]StdModuleFunc{
		"assert":    testAssert,
		"equal":     testEqual,
		"notEqual":  testNotEqual,
		"run":       testRun,
		"measureMs": testMeasureMs,
	}
	registerStdModule(stdTestMetadata)
}

func (vm *VM) callStdTest(method string, args []TinyValue) {
	fn, ok := stdTestMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown test function: %s", method)
		return
	}

	fn(vm, args)
}

func testAssert(vm *VM, args []TinyValue) {
	expectArgs(vm, "test.assert", args, 2)

	condition := argBool(vm, "test.assert", args, 0)
	message := argString(vm, "test.assert", args, 1)

	if !condition {
		vm.runtimeError(ErrorRuntime, "%s", message)
	}

	vm.push(NewNull())
}

func testEqual(vm *VM, args []TinyValue) {
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

	vm.push(NewNull())
}

func testNotEqual(vm *VM, args []TinyValue) {
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

	vm.push(NewNull())
}

func testRun(vm *VM, args []TinyValue) {
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

		vm.callFunctionValue(fn, []TinyValue{})
	}()

	if testFailed {
		fmt.Printf("❌ FAIL: %s\n%s\n", name, failureMessage)
	} else {
		fmt.Printf("✅ PASS: %s\n", name)
	}

	vm.push(NewNull())
}

func testMeasureMs(vm *VM, args []TinyValue) {
	expectArgs(vm, "test.measureMs", args, 1)

	fn := argFn(vm, "test.measureMs", args, 0)

	start := time.Now().UnixMilli()

	func() {
		defer func() {
			if r := recover(); r != nil {
				if langErr, ok := r.(LangErrorType); ok {
					vm.runtimeError(ErrorRuntime, "%s", langErr.Message)
				} else {
					vm.runtimeError(ErrorRuntime, "Go System Panic: %v", r)
				}
			}
		}()

		vm.callFunctionValue(fn, []TinyValue{})
	}()

	end := time.Now().UnixMilli()

	vm.push(NewInt(int(end - start)))
}
