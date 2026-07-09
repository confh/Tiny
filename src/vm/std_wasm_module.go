package vm

import (
	"context"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	. "language.com/src/tinyerrors"
)

var stdWasmMetadata = StdModuleInfo{
	Name: "wasm",
}

var stdWasmMethods map[string]StdModuleFunc

func init() {
	stdWasmMethods = map[string]StdModuleFunc{
		"instantiate": wasmInstantiate,
	}
	registerStdModule(stdWasmMetadata)
}

func (vm *VM) callStdWasm(method string, args []TinyValue) {
	fn, ok := stdWasmMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown wasm function: %s", method)
		return
	}

	fn(vm, args)
}

type goFuncWrapper struct {
	fn func(ctx context.Context, stack []uint64)
}

func (w goFuncWrapper) Call(ctx context.Context, stack []uint64) {
	w.fn(ctx, stack)
}

func wasmInstantiate(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "wasm.instantiate", args, 1, 2)

	wasmBytes := argBuffer(vm, "wasm.instantiate", args, 0)

	var userImports ObjectValue
	if len(args) > 1 && !isNullish(args[1]) {
		if obj, ok := vm.valueAsObjectForRead(args[1]); ok {
			userImports = obj
		} else {
			vm.runtimeError(ErrorType, "wasm.instantiate expected object as second argument")
			return
		}
	}

	wasmVal := &NativeWasmModuleValue{
		Ctx: context.Background(),
	}

	r := wazero.NewRuntime(wasmVal.Ctx)
	wasmVal.Runtime = r

	compiled, err := r.CompileModule(wasmVal.Ctx, wasmBytes.Bytes)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while compiling wasm module: %s", err)
		return
	}

	importGroups := make(map[string][]api.FunctionDefinition)
	for _, f := range compiled.ImportedFunctions() {
		modName, _, ok := f.Import()
		if ok {
			importGroups[modName] = append(importGroups[modName], f)
		}
	}

	for hostModuleName, fnDefs := range importGroups {
		if hostModuleName == "wasi_snapshot_preview1" {
			_, err := wasi_snapshot_preview1.Instantiate(wasmVal.Ctx, r)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "failed to instantiate WASI host module: %s", err)
				return
			}
			continue
		}

		var userHostModule ObjectValue
		if userImports != nil {
			if val, ok := userImports[hostModuleName]; ok {
				if obj, ok := vm.valueAsObjectForRead(val); ok {
					userHostModule = obj
				}
			}
		}

		builder := r.NewHostModuleBuilder(hostModuleName)
		hasAnyExport := false

		for _, def := range fnDefs {
			_, funcName, ok := def.Import()
			if !ok {
				continue
			}

			var tinyFunc TinyValue
			var hasFunc bool
			if userHostModule != nil {
				tinyFunc, hasFunc = userHostModule[funcName]
			}
			if !hasFunc {
				continue
			}

			capturedFunc, ok := tinyFunc.Value.(FunctionValue)
			if !ok {
				continue
			}

			paramTypes := def.ParamTypes()
			resultTypes := def.ResultTypes()

			goFunc := func(ctx context.Context, stack []uint64) {
				tinyArgs := make([]TinyValue, len(paramTypes))
				for i, pType := range paramTypes {
					tinyArgs[i] = wasmStackValueToTinyValue(stack[i], pType)
				}

				tinyResult := vm.callFunctionValue(capturedFunc, tinyArgs)

				if len(resultTypes) > 0 {
					stack[0] = tinyValueToWasmStackValue(tinyResult, resultTypes[0])
				}
			}

			builder.NewFunctionBuilder().
				WithGoFunction(goFuncWrapper{fn: goFunc}, paramTypes, resultTypes).
				Export(funcName)
			hasAnyExport = true
		}

		if hasAnyExport {
			_, err = builder.Instantiate(wasmVal.Ctx)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "failed to instantiate host module %s: %s", hostModuleName, err)
				return
			}
		}
	}

	// Disable default _start function execution (runs main() and exits)
	// by specifying _initialize as the start function.
	config := wazero.NewModuleConfig().WithStartFunctions("_initialize")
	module, err := r.InstantiateModule(wasmVal.Ctx, compiled, config)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while instantiating wasm module: %s", err)
		return
	}

	wasmVal.Module = module

	vm.push(NewNative(wasmVal))
}
