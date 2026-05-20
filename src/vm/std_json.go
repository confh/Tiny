package vm

import (
	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var stdJsonMethods = map[string]StdModuleFunc{
	"stringify": stdJsonStringify,
	"pretty":    stdJsonPretty,
	"parse":     stdJsonParse,
}

func (vm *VM) callStdJson(method string, args []Value) {
	fn, ok := stdJsonMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown json function: %s", method)
		return
	}
	fn(vm, args)
}

func stdJsonStringify(vm *VM, args []Value) {
	expectArgs(vm, "json.stringify", args, 1)

	value := args[0]
	jsonValue := valueToJSONCompatible(value)
	bytes, err := json.Marshal(jsonValue)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
	}
	vm.push(string(bytes))
}

func stdJsonPretty(vm *VM, args []Value) {
	expectArgs(vm, "json.pretty", args, 1)

	value := args[0]
	jsonValue := valueToJSONCompatible(value)
	bytes, err := json.MarshalIndent(jsonValue, "", "  ")
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
	}
	vm.push(string(bytes))
}

func stdJsonParse(vm *VM, args []Value) {
	expectArgs(vm, "json.parse", args, 1)

	stringified := argString(vm, "json.parse", args, 0)
	var result any
	err := json.Unmarshal([]byte(stringified), &result)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid JSON: %v", err)
	}
	vm.push(jsonToTinyValue(result))
}
