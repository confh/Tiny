package vm

import . "language.com/src/tinyerrors"

var taskMethods map[string]NativeModuleFunc[*NativeTaskValue]

func init() {
	taskMethods = map[string]NativeModuleFunc[*NativeTaskValue]{
		"await": taskAwait,
	}
}

func (vm *VM) callTaskMethod(task *NativeTaskValue, method string, args []Value) {
	fn, ok := taskMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown task method: %s", method)
		return
	}

	fn(vm, task, args)
}

func taskAwait(vm *VM, task *NativeTaskValue, args []Value) {
	expectArgs(vm, "task.await", args, 0)

	result := <-task.Done

	if result.Error != nil {
		panic(result.Error)
	}

	vm.push(result.Value)
}
