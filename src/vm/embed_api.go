package vm

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

func (vm *VM) InstallGlobalIndex(globalIndex map[string]int) {
	maps.Copy(vm.globalNames, globalIndex)
}

func (vm *VM) SetGlobalValue(name string, value TinyValue) {
	setRuntimeVMGlobal(vm, name, value)
}

func (vm *VM) SetCallbackFunction(name string, callback func([]TinyValue) (TinyValue, error)) {
	vm.SetGlobalValue(name, NewNative(&CallbackFunctionValue{
		Name:     name,
		Callback: callback,
	}))
}

func (vm *VM) CallFunctionValueByName(name string, args []TinyValue) (TinyValue, error) {
	fn, ok := vm.functions[name]
	if !ok {
		if exportedFn, exportedOK := vm.functions["export "+name]; exportedOK {
			fn = exportedFn
			ok = true
		}
	}
	if !ok {
		return NewNull(), fmt.Errorf("unknown function: %s", name)
	}

	return vm.callFunctionValue(FunctionValue{ID: fn.ID, Name: fn.Name}, args), nil
}

func (vm *VM) HasFunction(name string) bool {
	if _, ok := vm.functions[name]; ok {
		return true
	}
	_, ok := vm.functions["export "+name]
	return ok
}

func (vm *VM) FunctionNames() []string {
	names := make([]string, 0, len(vm.functions))
	seen := map[string]bool{}
	for name := range vm.functions {
		displayName := strings.TrimPrefix(name, "export ")
		if seen[displayName] {
			continue
		}
		seen[displayName] = true
		names = append(names, displayName)
	}
	sort.Strings(names)
	return names
}

func (vm *VM) ResetEmbedState() {
	vm.ResetForRequest()
}

func TinyValueFromJSONBytes(data []byte) (TinyValue, error) {
	if len(strings.TrimSpace(string(data))) == 0 {
		return NewNull(), nil
	}

	var decoded any
	if err := json.Unmarshal(data, &decoded); err != nil {
		return NewNull(), err
	}

	return ToValue(decoded), nil
}

func TinyValueToJSONBytes(value TinyValue) ([]byte, error) {
	return json.Marshal(valueToJSONCompatible(value))
}
