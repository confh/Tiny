package vm

import (
	. "language.com/src/tinyerrors"
)

var stdObjectMetadata = StdModuleInfo{
	Name: "object",
	Methods: map[string]StdMethodInfo{
		"get": {
			Name: "get",
			Args: []StdArg{
				{Name: "object", Type: "object"},
				{Name: "key", Type: "string"},
			},
			Returns:     "any",
			Description: "Returns the value for the given key, or undefined if the key is not present.",
		},
		"set": {
			Name: "set",
			Args: []StdArg{
				{Name: "object", Type: "object"},
				{Name: "key", Type: "string"},
				{Name: "value", Type: "any"},
			},
			Returns:     "undefined",
			Description: "Sets the value for the given key in the object.",
		},
		"has": {
			Name: "has",
			Args: []StdArg{
				{Name: "object", Type: "object"},
				{Name: "key", Type: "string"},
			},
			Returns:     "bool",
			Description: "Returns true if the key exists in the object.",
		},
		"delete": {
			Name: "delete",
			Args: []StdArg{
				{Name: "object", Type: "object"},
				{Name: "key", Type: "string"},
			},
			Returns:     "bool",
			Description: "Deletes the key from the object and returns true if the key existed, false otherwise.",
		},
		"keys": {
			Name: "keys",
			Args: []StdArg{
				{Name: "object", Type: "object"},
			},
			Returns:     "array",
			Description: "Returns an array of keys in the object.",
		},
		"values": {
			Name: "values",
			Args: []StdArg{
				{Name: "object", Type: "object"},
			},
			Returns:     "array",
			Description: "Returns an array of values in the object.",
		},
		"enteries": {
			Name: "enteries",
			Args: []StdArg{
				{Name: "object", Type: "object"},
			},
			Returns:     "array",
			Description: "Returns an array of [key, value] pairs for each entry in the object.",
		},
		"length": {
			Name: "length",
			Args: []StdArg{
				{Name: "object", Type: "object"},
			},
			Returns:     "int",
			Description: "Returns the number of keys in the object.",
		},
		"clear": {
			Name: "clear",
			Args: []StdArg{
				{Name: "object", Type: "object"},
			},
			Returns:     "undefined",
			Description: "Removes all keys and values from the object.",
		},
	},
}

var stdObjectMethods map[string]StdModuleFunc

func init() {
	stdObjectMethods = map[string]StdModuleFunc{
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
	registerStdModule(stdObjectMetadata)
}

func (vm *VM) callStdObject(method string, args []Value) {
	fn, ok := stdObjectMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown object method: %s", method)
		return
	}
	fn(vm, args)
}

func objectGet(vm *VM, args []Value) {
	expectArgs(vm, "object.get", args, 2)

	obj := argObject(vm, "object.get", args, 0)
	key := argString(vm, "object.get", args, 1)

	val, ok := obj[key]
	if ok {
		vm.push(val)
	} else {
		vm.push(NewUndefined())
	}
}

func objectSet(vm *VM, args []Value) {
	expectArgs(vm, "object.set", args, 3)

	obj := argObject(vm, "object.set", args, 0)
	key := argString(vm, "object.set", args, 1)
	value := args[2]

	obj[key] = value

	vm.push(NewUndefined())
}

func objectHas(vm *VM, args []Value) {
	expectArgs(vm, "object.has", args, 2)

	obj := argObject(vm, "object.has", args, 0)
	key := argString(vm, "object.has", args, 1)

	_, found := obj[key]
	vm.push(NewNative(found))
}

func objectDelete(vm *VM, args []Value) {
	expectArgs(vm, "object.delete", args, 2)

	obj := argObject(vm, "object.delete", args, 0)
	key := argString(vm, "object.delete", args, 1)

	_, found := obj[key]
	if found {
		delete(obj, key)
	}
	vm.push(NewNative(found))
}

func objectKeys(vm *VM, args []Value) {
	expectArgs(vm, "object.keys", args, 1)

	obj := argObject(vm, "object.keys", args, 0)

	keys := make([]Value, 0, len(obj))
	for k := range obj {
		keys = append(keys, NewNative(k))
	}
	vm.push(NewNative(&ArrayValue{Elements: keys}))
}

func objectValues(vm *VM, args []Value) {
	expectArgs(vm, "object.values", args, 1)

	obj := argObject(vm, "object.values", args, 0)

	values := make([]Value, 0, len(obj))
	for _, v := range obj {
		values = append(values, v)
	}
	vm.push(NewNative(&ArrayValue{Elements: values}))
}

func objectEnteries(vm *VM, args []Value) {
	expectArgs(vm, "object.enteries", args, 1)

	obj := argObject(vm, "object.enteries", args, 0)

	entries := make([]Value, 0, len(obj))
	for k, v := range obj {
		entry := NewNative(&ArrayValue{Elements: []Value{NewNative(k), v}})
		entries = append(entries, entry)
	}
	vm.push(NewNative(&ArrayValue{Elements: entries}))
}

func objectLength(vm *VM, args []Value) {
	expectArgs(vm, "object.length", args, 1)

	obj := argObject(vm, "object.length", args, 0)
	vm.push(NewInt(len(obj)))
}

func objectClear(vm *VM, args []Value) {
	expectArgs(vm, "object.clear", args, 1)

	obj := argObject(vm, "object.clear", args, 0)
	for k := range obj {
		delete(obj, k)
	}
	vm.push(NewUndefined())
}
