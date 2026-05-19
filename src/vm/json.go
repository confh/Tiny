package vm

import (
	. "language.com/src/tinyerrors"
)

func (vm *VM) callStdJson(method string, args []Value) {
	switch method {
	case "stringify":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "json.stringify expects 1 argument")
		}

		value := args[0]

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))

	case "pretty":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "json.pretty expects 1 argument")
		}

		value := args[0]

		jsonValue := valueToJSONCompatible(value)

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		vm.push(string(bytes))
	case "parse":
		if len(args) != 1 {
			vm.runtimeError(ErrorRuntime, "json.parse expects 1 argument")
		}

		stringified := asString(args[0], vm)

		var result any

		err := json.Unmarshal([]byte(stringified), &result)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "invalid JSON: %v", err)
		}

		vm.push(jsonToTinyValue(result))
	default:
		vm.runtimeError(ErrorName, "unknown json function: %s", method)
	}
}
