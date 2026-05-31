package vm

import (
	"os"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var stdJsonMetadata = StdModuleInfo{
	Name: "json",
}

var stdJsonMethods map[string]StdModuleFunc

func init() {
	stdJsonMethods = map[string]StdModuleFunc{
		"stringify": stdJsonStringify,
		"pretty":    stdJsonPretty,
		"parse":     stdJsonParse,
		"readFile":  stdJsonReadFile,
		"writeFile": stdJsonWriteFile,
	}
	registerStdModule(stdJsonMetadata)
}

func (vm *VM) callStdJson(method string, args []TinyValue) {
	fn, ok := stdJsonMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown json function: %s", method)
		return
	}
	fn(vm, args)
}

func stdJsonStringify(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.stringify", args, 1)

	switch value := args[0].Value.(type) {
	case ObjectValue, ArrayValue, *ArrayValue:
		jsonValue := valueToJSONCompatible(ToValue(value))
		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			vm.fatalError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}
		vm.push(NewNative(string(bytes)))
	default:
		vm.fatalError(ErrorType, "json.stringify expected an array or an object, got %s", TypeName(ToValue(value)))
	}
}

func stdJsonPretty(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.pretty", args, 1)

	switch value := args[0].Value.(type) {
	case ObjectValue, ArrayValue, *ArrayValue:
		jsonValue := valueToJSONCompatible(ToValue(value))
		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			vm.fatalError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}
		vm.push(NewNative(string(bytes)))
	default:
		vm.fatalError(ErrorType, "json.pretty expected an array or an object, got %s", TypeName(ToValue(value)))
	}
}

func stdJsonParse(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.parse", args, 1)

	stringified := argString(vm, "json.parse", args, 0)

	var result any

	err := json.Unmarshal([]byte(stringified), &result)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid JSON: %v", err)
		vm.push(NewNull())
		return
	}
	vm.push(jsonToTinyValue(result))
}

func stdJsonReadFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.readFile", args, 1)

	fileName := argString(vm, "json.readFile", args, 0)

	data, err := os.ReadFile(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
		vm.push(NewNull())
		return
	}

	var result any
	err = json.Unmarshal([]byte(data), &result)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "could not parse file '%s' as json", fileName)
		vm.push(NewNull())
		return
	}

	vm.push(jsonToTinyValue(result))
}

func stdJsonWriteFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.writeFile", args, 2)

	value := argObject(vm, "json.writeFile", args, 0)
	jsonValue := valueToJSONCompatible(NewNative(value))
	bytes, err := json.MarshalIndent(jsonValue, "", "  ")
	fileName := argString(vm, "json.writeFile", args, 1)

	err = os.WriteFile(fileName, bytes, 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing json file: %s", err)
	}
	vm.push(NewNull())
}
