package main

func (vm *VM) callStandardModule(module string, method string, args []Value) {
	switch module {
	case "array":
		vm.callStdArray(method, args)

	default:
		langError(ErrorName, "unknown standard module: %s", module)
	}
}

func (vm *VM) callStdArray(method string, args []Value) {
	switch method {
	case "push":
		if len(args) != 2 {
			langError(ErrorRuntime, "array.push expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.push expects array, got %s", typeName(args[0]))
		}

		arr.Elements = append(arr.Elements, args[1])

		vm.push(arr)

	case "pop":
		if len(args) != 1 {
			langError(ErrorRuntime, "array.pop expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.pop expects array, got %s", typeName(args[0]))
		}

		if len(arr.Elements) == 0 {
			vm.push(UndefinedValue{})
			return
		}

		last := arr.Elements[len(arr.Elements)-1]
		arr.Elements = arr.Elements[:len(arr.Elements)-1]

		vm.push(last)

	case "len":
		if len(args) != 1 {
			langError(ErrorRuntime, "array.len expects 1 argument")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.len expects array, got %s", typeName(args[0]))
		}

		vm.push(len(arr.Elements))

	case "get":
		if len(args) != 2 {
			langError(ErrorRuntime, "array.get expects 2 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.get expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			langError(ErrorRuntime, "array index out of range: %d", index)
		}

		vm.push(arr.Elements[index])

	case "set":
		if len(args) != 3 {
			langError(ErrorRuntime, "array.set expects 3 arguments")
		}

		arr, ok := args[0].(*ArrayValue)
		if !ok {
			langError(ErrorType, "array.set expects array, got %s", typeName(args[0]))
		}

		index := asInt(args[1])

		if index < 0 || index >= len(arr.Elements) {
			langError(ErrorRuntime, "array index out of range: %d", index)
		}

		arr.Elements[index] = args[2]

		vm.push(arr)

	default:
		langError(ErrorName, "unknown array function: %s", method)
	}
}
