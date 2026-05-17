package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callTaskMethod(task *NativeTaskValue, method string, args []Value) {
	switch method {
	case "await":
		result := <-task.Done

		if result.Error != nil {
			panic(result.Error)
		}

		vm.push(result.Value)

	default:
		LangError(ErrorName, "unknown task function: %s", method)
	}
}
