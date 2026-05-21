package vm

import (
	. "language.com/src/tinyerrors"
)

var objectMetadata = StdModuleInfo{
	Name: "object",
	Methods: map[string]StdMethodInfo{
		"get": {
			Name:        "get",
			Args:        []StdArg{{Name: "key", Type: "string"}},
			Returns:     "any",
			Description: "Returns the value for the given key, or undefined if the key is not present.",
		},
		"set": {
			Name: "set",
			Args: []StdArg{
				{Name: "key", Type: "string"},
				{Name: "value", Type: "any"},
			},
			Returns:     "undefined",
			Description: "Sets the value for the given key in the object.",
		},
		"has": {
			Name:        "has",
			Args:        []StdArg{{Name: "key", Type: "string"}},
			Returns:     "bool",
			Description: "Returns true if the key exists in the object.",
		},
		"delete": {
			Name:        "delete",
			Args:        []StdArg{{Name: "key", Type: "string"}},
			Returns:     "bool",
			Description: "Deletes the key from the object and returns true if the key existed, false otherwise.",
		},
		"keys": {
			Name:        "keys",
			Args:        []StdArg{},
			Returns:     "Array",
			Description: "Returns an array of keys in the object.",
		},
		"values": {
			Name:        "values",
			Args:        []StdArg{},
			Returns:     "Array",
			Description: "Returns an array of values in the object.",
		},
		"enteries": {
			Name:        "enteries",
			Args:        []StdArg{},
			Returns:     "Array",
			Description: "Returns an array of [key, value] pairs for each entry in the object.",
		},
		"length": {
			Name:        "length",
			Args:        []StdArg{},
			Returns:     "int",
			Description: "Returns the number of keys in the object.",
		},
		"clear": {
			Name:        "clear",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Removes all keys and values from the object.",
		},
	},
}

var objectMethods map[string]NativeModuleFunc[ObjectValue]

func init() {
	objectMethods = map[string]NativeModuleFunc[ObjectValue]{
		"get":      objectGet,
		"set":      objectSet,
		"has":      objectHas,
		"delete":   objectDelete,
		"keys":     objectKeys,
		"values":   objectValues,
		"enteries": objectEnteries,
		"length":   objectLength,
		"clear":    objectClear,
	}

	registerStdModule(objectMetadata)
}

func (vm *VM) callObjectMethod(obj ObjectValue, method string, args []Value) {
	fn, ok := objectMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown object method: %s", method)
		return
	}
	fn(vm, obj, args)
}

func objectGet(vm *VM, obj ObjectValue, args []Value) {
	expectArgs(vm, "object.get", args, 1)

	key := argString(vm, "object.get", args, 0)

	vm.push(obj[key])
}

func objectSet(vm *VM, obj ObjectValue, args []Value) {
	expectArgs(vm, "object.set", args, 2)

	key := argString(vm, "object.set", args, 0)
	value := args[1]

	obj[key] = value

	vm.push(UndefinedValue{})
}

func objectHas(vm *VM, obj ObjectValue, args []Value) {
	expectArgs(vm, "object.has", args, 1)

	key := argString(vm, "object.has", args, 0)

	found := false
	_, found = obj[key]
	vm.push(found)
}

func objectDelete(vm *VM, obj ObjectValue, args []Value) {
	expectArgs(vm, "object.delete", args, 1)

	key := argString(vm, "object.delete", args, 0)

	_, found := obj[key]
	if found {
		delete(obj, key)
	}
	vm.push(found)
}

func objectKeys(vm *VM, obj ObjectValue, args []Value) {
	dontExpectArgs(vm, "object.keys", args)

	keys := make([]Value, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	vm.push(&ArrayValue{Elements: keys})
}

func objectValues(vm *VM, obj ObjectValue, args []Value) {
	dontExpectArgs(vm, "object.values", args)

	values := make([]Value, 0, len(obj))
	for _, v := range obj {
		values = append(values, v)
	}
	vm.push(&ArrayValue{Elements: values})
}

func objectEnteries(vm *VM, obj ObjectValue, args []Value) {
	dontExpectArgs(vm, "object.enteries", args)

	entries := make([]Value, 0, len(obj))
	for k, v := range obj {
		entry := &ArrayValue{Elements: []Value{k, v}}
		entries = append(entries, entry)
	}
	vm.push(&ArrayValue{Elements: entries})
}

func objectLength(vm *VM, obj ObjectValue, args []Value) {
	dontExpectArgs(vm, "object.length", args)
	vm.push(len(obj))
}

func objectClear(vm *VM, obj ObjectValue, args []Value) {
	dontExpectArgs(vm, "object.clear", args)
	for k := range obj {
		delete(obj, k)
	}
	vm.push(UndefinedValue{})
}
