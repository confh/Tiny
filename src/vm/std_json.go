package vm

import (
	"os"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var stdJsonMetadata = StdModuleInfo{
	Name: "json",
	Methods: map[string]StdMethodInfo{
		"stringify": {
			Name: "stringify",
			Args: []StdArg{
				{Name: "value", Type: "object", Optional: false},
			},
			Returns:     "string",
			Description: "Serializes an object value as a JSON string.",
		},
		"pretty": {
			Name: "pretty",
			Args: []StdArg{
				{Name: "value", Type: "object", Optional: false},
			},
			Returns:     "string",
			Description: "Serializes an object value as a pretty-printed JSON string.",
		},
		"parse": {
			Name: "parse",
			Args: []StdArg{
				{Name: "stringified", Type: "string", Optional: false},
			},
			Returns:     "object",
			Description: "Parses a JSON string and returns the corresponding object.",
		},
		"readFile": {
			Name: "readFile",
			Args: []StdArg{
				{Name: "fileName", Type: "string", Optional: false},
			},
			Returns:     "object",
			Description: "Reads and parses a JSON file, returning its contents as an object.",
		},
		"writeFile": {
			Name: "writeFile",
			Args: []StdArg{
				{Name: "value", Type: "object", Optional: false},
				{Name: "fileName", Type: "string", Optional: false},
			},
			Returns:     "undefined",
			Description: "Serializes an object value as pretty-printed JSON and writes it to a file.",
		},
	},
}

var stdJsonMethods = map[string]StdModuleFunc{
	"stringify": stdJsonStringify,
	"pretty":    stdJsonPretty,
	"parse":     stdJsonParse,
	"readFile":  stdJsonReadFile,
	"writeFile": stdJsonWriteFile,
}

func init() {
	registerStdModule(stdJsonMetadata)
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

	value := argObject(vm, "json.stringify", args, 0)
	jsonValue := valueToJSONCompatible(value)
	bytes, err := json.Marshal(jsonValue)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "failed to convert value to JSON: %v", err)
	}
	vm.push(string(bytes))
}

func stdJsonPretty(vm *VM, args []Value) {
	expectArgs(vm, "json.pretty", args, 1)

	value := argObject(vm, "json.pretty", args, 0)
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

func stdJsonReadFile(vm *VM, args []Value) {
	expectArgs(vm, "json.readFile", args, 1)

	fileName := argString(vm, "json.readFile", args, 0)

	data, err := os.ReadFile(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
	}

	var result any
	err = json.Unmarshal([]byte(data), &result)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "could not parse file '%s' as json", fileName)
	}

	vm.push(jsonToTinyValue(result))
}

func stdJsonWriteFile(vm *VM, args []Value) {
	expectArgs(vm, "json.writeFile", args, 2)

	value := argObject(vm, "json.writeFile", args, 0)
	jsonValue := valueToJSONCompatible(value)
	bytes, err := json.MarshalIndent(jsonValue, "", "  ")
	fileName := argString(vm, "json.writeFile", args, 1)

	err = os.WriteFile(fileName, bytes, 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing json file: %s", err)
	}

	vm.push(UndefinedValue{})
}
