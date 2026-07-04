package vm

import (
	"sort"

	. "language.com/src/tinyerrors"
)

var stdObjectMetadata = StdModuleInfo{
	Name: "object",
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
		"entries":  objectEntries,
		"enteries": objectEnteries,
		"length":   objectLength,
		"clear":    objectClear,
		"forEach":  objectForEach,
		"pick":     objectPick,
		"omit":     objectOmit,
	}
	registerStdModule(stdObjectMetadata)
}

func (vm *VM) callStdObject(method string, args []TinyValue) {
	fn, ok := stdObjectMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown object method: %s", method)
		return
	}
	fn(vm, args)
}

func objectGet(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.get", args, 2)

	obj, ok := vm.valueAsObjectForWrite(args[0])
	if !ok {
		vm.runtimeError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(args[0]))
		return
	}
	key := argString(vm, "object.get", args, 1)

	val, ok := obj.get(key)
	if ok {
		vm.push(val)
	} else {
		vm.push(NewNull())
	}
}

func objectSet(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.set", args, 3)

	obj, ok := vm.valueAsObjectForWrite(args[0])
	if !ok {
		vm.runtimeError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(args[0]))
		return
	}
	key := argString(vm, "object.set", args, 1)
	obj.set(key, args[2])

	vm.push(NewNull())
}

func objectHas(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.has", args, 2)

	obj, ok := vm.valueAsObjectForWrite(args[0])
	if !ok {
		vm.runtimeError(ErrorType, "object.has argument 1 expected object, got %s", TypeName(args[0]))
		return
	}
	key := argString(vm, "object.has", args, 1)

	vm.push(NewNative(obj.has(key)))
}

func objectDelete(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.delete", args, 2)

	obj, ok := vm.valueAsObjectForWrite(args[0])
	if !ok {
		vm.runtimeError(ErrorType, "object.delete argument 1 expected object, got %s", TypeName(args[0]))
		return
	}
	key := argString(vm, "object.delete", args, 1)

	vm.push(NewNative(obj.delete(key)))
}

func objectKeys(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.keys", args, 1)

	obj := argObject(vm, "object.keys", args, 0)

	keys := make([]TinyValue, 0, len(obj))
	strKeys := make([]string, 0, len(obj))
	otherKeys := make([]any, 0)
	for k := range obj {
		if s, ok := k.(string); ok {
			strKeys = append(strKeys, s)
		} else {
			otherKeys = append(otherKeys, k)
		}
	}
	sort.Strings(strKeys)
	for _, k := range strKeys {
		keys = append(keys, NewNative(k))
	}
	for _, k := range otherKeys {
		keys = append(keys, ToValue(k))
	}
	vm.push(NewNative(&ArrayValue{Elements: keys}))
}

func objectValues(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.values", args, 1)

	obj := argObject(vm, "object.values", args, 0)

	values := make([]TinyValue, 0, len(obj))
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if s, ok := k.(string); ok {
			keys = append(keys, s)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		values = append(values, obj[k])
	}
	vm.push(NewNative(&ArrayValue{Elements: values}))
}

func objectEnteries(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.enteries", args, 1)

	objectEntriesForName(vm, "object.enteries", args)
}

func objectEntries(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.entries", args, 1)

	objectEntriesForName(vm, "object.entries", args)
}

func objectEntriesForName(vm *VM, fnName string, args []TinyValue) {
	obj := argObject(vm, fnName, args, 0)

	entries := make([]TinyValue, 0, len(obj))
	keys := make([]string, 0, len(obj))
	for k := range obj {
		if s, ok := k.(string); ok {
			keys = append(keys, s)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		entry := NewNative(&ArrayValue{Elements: []TinyValue{NewNative(k), obj[k]}})
		entries = append(entries, entry)
	}
	vm.push(NewNative(&ArrayValue{Elements: entries}))
}

func objectLength(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.length", args, 1)

	obj := argObject(vm, "object.length", args, 0)
	vm.push(NewInt(len(obj)))
}

func objectClear(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.clear", args, 1)

	obj, ok := vm.valueAsObjectForWrite(args[0])
	if !ok {
		vm.runtimeError(ErrorType, "object.clear argument 1 expected object, got %s", TypeName(args[0]))
		return
	}
	obj.clear()
	vm.push(NewNull())
}

func objectForEach(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.forEach", args, 2)

	obj := argObject(vm, "object.forEach", args, 0)
	fn := argFn(vm, "object.forEach", args, 1)

	keys := make([]string, 0, len(obj))
	for k := range obj {
		if s, ok := k.(string); ok {
			keys = append(keys, s)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		vm.callFunctionValue(fn, []TinyValue{NewNative(k), obj[k]})
	}

	vm.push(NewNull())
}

func objectPick(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.pick", args, 2)

	obj := argObject(vm, "object.pick", args, 0)
	arr := argArray(vm, "object.pick", args, 1)

	values := ObjectValue{}

	for _, v := range arr.Elements {
		key := valueToString(v)
		if val, ok := obj[key]; ok {
			values[key] = val
		}
	}

	vm.push(NewNative(values))
}

func objectOmit(vm *VM, args []TinyValue) {
	expectArgs(vm, "object.omit", args, 2)

	obj := argObject(vm, "object.omit", args, 0)
	arr := argArray(vm, "object.omit", args, 1)

	omitted := make(map[string]struct{}, len(arr.Elements))
	for _, v := range arr.Elements {
		omitted[valueToString(v)] = struct{}{}
	}

	values := ObjectValue{}
	for key, val := range obj {
		if _, skip := omitted[valueToString(ToValue(key))]; !skip {
			values[key] = val
		}
	}

	vm.push(NewNative(values))
}
