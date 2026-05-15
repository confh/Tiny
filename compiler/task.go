package main

func (vm *VM) callTaskModule(method string, args []Value) {
	switch method {
	case "run":
		if len(args) != 1 {
			langError(ErrorRuntime, "task.run expects 1 argument")
		}

		value := args[0]

		fn, ok := value.(FunctionValue)
		if !ok {
			langError(ErrorType, "task.run expects function, got %s", typeName(value))
		}

		task := &NativeTaskValue{
			Done: make(chan TaskResult, 1),
		}

		taskVM := vm.CloneForTask()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					task.Done <- TaskResult{
						Error: r,
					}
				}
			}()

			result := taskVM.callFunctionValue(fn, []Value{})

			task.Done <- TaskResult{
				Value: result,
			}
		}()

		vm.push(task)

	case "await":
		if len(args) != 1 {
			langError(ErrorRuntime, "task.await expects 1 argument")
		}

		value := args[0]

		task, ok := value.(*NativeTaskValue)
		if !ok {
			langError(ErrorType, "task.await expects task, got %s", typeName(value))
		}

		result := <-task.Done

		if result.Error != nil {
			panic(result.Error)
		}

		vm.push(result.Value)

	default:
		langError(ErrorName, "unknown task function: %s", method)
	}
}
