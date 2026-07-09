package vm

import (
	"fmt"
	"strings"
	"time"

	. "language.com/src/tinyerrors"
)

type RuntimeBytecodeProgram struct {
	MainInstructions []Instruction
	MainDebugInfo    []DebugInfo
	Functions        map[string]Function
	Classes          map[string]Class
	Interfaces       map[string]Interface
	GlobalIndex      map[string]int
}

type RuntimeBytecodeLoader func([]byte) RuntimeBytecodeProgram
type RuntimeSourceLoader func(source string, file string) RuntimeBytecodeProgram

var runtimeBytecodeLoader RuntimeBytecodeLoader
var runtimeSourceLoader RuntimeSourceLoader

func SetRuntimeBytecodeLoader(loader RuntimeBytecodeLoader) {
	runtimeBytecodeLoader = loader
}

func SetRuntimeSourceLoader(loader RuntimeSourceLoader) {
	runtimeSourceLoader = loader
}

var runtimeVMMethods map[string]NativeModuleFunc[*NativeVMValue]

func init() {
	runtimeVMMethods = map[string]NativeModuleFunc[*NativeVMValue]{
		"loadBytecode":   runtimeVMLoadBytecode,
		"loadSource":     runtimeVMLoadSource,
		"run":            runtimeVMRun,
		"call":           runtimeVMCall,
		"stop":           runtimeVMStop,
		"reset":          runtimeVMReset,
		"setGlobal":      runtimeVMSetGlobal,
		"exposeFunction": runtimeVMExposeFunction,
		"info":           runtimeVMInfo,
		"functionExists": runtimeVMFunctionExists,
		"listFunctions":  runtimeVMListFunctions,
	}
}

func (vm *VM) callRuntimeVMMethod(child *NativeVMValue, method string, args []TinyValue) {
	fn, ok := runtimeVMMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown runtime.VM method: %s", method)
		return
	}

	defer func() {
		if r := recover(); r != nil {
			switch err := r.(type) {
			case LangErrorType:
				msg := err.Message
				if err.File != "" && err.Line > 0 {
					msg = fmt.Sprintf("%s:%d:%d: %s", err.File, err.Line, err.Column, err.Message)
				}
				vm.runtimeError(err.Kind, "%s", msg)
			case *LangErrorType:
				msg := err.Message
				if err.File != "" && err.Line > 0 {
					msg = fmt.Sprintf("%s:%d:%d: %s", err.File, err.Line, err.Column, err.Message)
				}
				vm.runtimeError(err.Kind, "%s", msg)
			default:
				panic(r)
			}
		}
	}()

	fn(vm, child, args)
}

func newNativeVMValue(options runtimeVMOptions) *NativeVMValue {
	child := NewVM(VMInfo{
		JITDisabled:   options.JITDisabled,
		Isolated:      options.Isolated,
		AllowedStdlib: options.AllowedStdlib,
	})
	child.SetCLIArgs(options.CLIArgs)

	native := &NativeVMValue{
		VM:             child,
		Isolated:       options.Isolated,
		RunMainOnLoad:  options.RunMainOnLoad,
		AllowedStdlib:  cloneStringBoolMap(options.AllowedStdlib),
		InjectedGlobal: ObjectValue{},
	}

	for rawName, value := range options.Globals {
		name, ok := rawName.(string)
		if !ok {
			continue
		}
		setRuntimeVMGlobal(child, name, value)
		native.InjectedGlobal[name] = cloneValue(value)
	}

	return native
}

func runtimeVMLoadBytecode(vm *VM, child *NativeVMValue, args []TinyValue) {
	expectArgs(vm, "runtime.VM.loadBytecode", args, 1)

	if source, ok := args[0].Value.(string); ok && looksLikeTinySource(source) {
		loadRuntimeVMSource(vm, child, source, "<runtime>")
		vm.push(NewNative(child))
		return
	}

	if runtimeBytecodeLoader == nil {
		vm.runtimeError(ErrorRuntime, "runtime bytecode loader is not installed")
		return
	}

	loadRuntimeVMProgram(child, runtimeBytecodeLoader(runtimeVMByteSlice(vm, args[0])))
	if child.RunMainOnLoad {
		runRuntimeVMMain(vm, child)
		child.MainRan = true
	}

	vm.push(NewNative(child))
}

func runtimeVMLoadSource(vm *VM, child *NativeVMValue, args []TinyValue) {
	if len(args) < 1 || len(args) > 2 {
		vm.runtimeError(ErrorRuntime, "runtime.VM.loadSource expects 1 or 2 arguments, got %d", len(args))
		return
	}

	source := argString(vm, "runtime.VM.loadSource", args, 0)
	file := "<runtime>"
	if len(args) == 2 && !isNullish(args[1]) {
		file = argString(vm, "runtime.VM.loadSource", args, 1)
	}

	loadRuntimeVMSource(vm, child, source, file)
	vm.push(NewNative(child))
}

func loadRuntimeVMSource(vm *VM, child *NativeVMValue, source string, file string) {
	if runtimeSourceLoader == nil {
		vm.runtimeError(ErrorRuntime, "runtime source loader is not installed")
		return
	}

	loadRuntimeVMProgram(child, runtimeSourceLoader(source, file))
	if child.RunMainOnLoad {
		runRuntimeVMMain(vm, child)
		child.MainRan = true
	}
}

func loadRuntimeVMProgram(child *NativeVMValue, program RuntimeBytecodeProgram) {
	if child.AllowedStdlib != nil {
		for i, instr := range program.MainInstructions {
			if instr.Op == OP_BUILTIN_CALL {
				if info, ok := instr.Value.(BuiltinCallInfo); ok && info.Object == "Plugin" && info.Method == "std" {
					if i > 0 && program.MainInstructions[i-1].Op == OP_CONST {
						if name, ok := program.MainInstructions[i-1].Value.(string); ok {
							if !child.AllowedStdlib[name] {
								dbg := DebugInfo{}
								if i < len(program.MainDebugInfo) {
									dbg = program.MainDebugInfo[i]
								}
								panic(LangErrorType{
									Kind:    ErrorRuntime,
									Message: fmt.Sprintf("standard module '%s' is not allowed in this VM", name),
									File:    dbg.File,
									Line:    dbg.Line,
									Column:  dbg.Column,
								})
							}
						}
					}
				}
			}
		}
	}

	child.VM.Close()
	child.VM = NewVM(VMInfo{
		MainInstructions: program.MainInstructions,
		MainDebugInfo:    program.MainDebugInfo,
		Functions:        program.Functions,
		Classes:          program.Classes,
		Interfaces:       program.Interfaces,
		JITDisabled:      child.VM.jitDisabled,
		Isolated:         child.Isolated,
		AllowedStdlib:    child.AllowedStdlib,
	})

	for name, slot := range program.GlobalIndex {
		child.VM.globalNames[name] = slot
	}
	for rawName, value := range child.InjectedGlobal {
		name, ok := rawName.(string)
		if !ok {
			continue
		}
		setRuntimeVMGlobal(child.VM, name, value)
	}

	child.Loaded = true
	child.MainRan = false
}

func runtimeVMRun(vm *VM, child *NativeVMValue, args []TinyValue) {
	dontExpectArgs(vm, "runtime.VM.run", args)
	runRuntimeVMMain(vm, child)
	child.MainRan = true
	vm.push(NewNull())
}

func runtimeVMListFunctions(vm *VM, child *NativeVMValue, args []TinyValue) {
	dontExpectArgs(vm, "runtime.VM.listFunctions", args)

	ensureRuntimeVMLoaded(vm, child)

	functionNames := []TinyValue{}

	for name := range child.VM.FunctionNames() {
		functionNames = append(functionNames, NewNative(name))
	}

	vm.push(NewArray(functionNames))
}

func runtimeVMFunctionExists(vm *VM, child *NativeVMValue, args []TinyValue) {
	expectArgs(vm, "runtime.VM.functionExists", args, 1)

	ensureRuntimeVMLoaded(vm, child)

	fnName := argString(vm, "runtime.VM.functionExists", args, 0)

	vm.push(NewNative(child.VM.HasFunction(fnName)))
}

func runtimeVMCall(vm *VM, child *NativeVMValue, args []TinyValue) {
	expectArgsRange(vm, "runtime.VM.call", args, 1, 2)

	ensureRuntimeVMLoaded(vm, child)

	name := argString(vm, "runtime.VM.call", args, 0)
	callArgs := []TinyValue{}

	if len(args) == 2 {
		array := argArray(vm, "runtime.VM.call", args, 1)
		callArgs = make([]TinyValue, len(array.Elements))
		for i, arg := range array.Elements {
			callArgs[i] = wrapFunctionsForHostVM(arg, vm)
		}
	}

	fn, ok := child.VM.functions[name]
	if !ok {
		if exportedFn, exportedOK := child.VM.functions["export "+name]; exportedOK {
			fn = exportedFn
			ok = true
		}
	}
	if !ok {
		vm.runtimeError(ErrorName, "runtime.VM.call unknown function: %s", name)
		return
	}

	if !child.MainRan {
		runRuntimeVMMain(vm, child)
		child.MainRan = true
	}

	result := child.VM.callFunctionValue(FunctionValue{ID: fn.ID, Name: fn.Name}, callArgs)
	vm.push(wrapFunctionsForHostVM(result, child.VM))
}

func runtimeVMReset(vm *VM, child *NativeVMValue, args []TinyValue) {
	dontExpectArgs(vm, "runtime.VM.reset", args)
	child.VM.Stop()
	if !child.VM.WaitIdle(2 * time.Second) {
		vm.runtimeError(ErrorRuntime, "runtime.VM.reset timed out waiting for %d active execution(s); call stop() and let the VM finish before resetting", child.VM.ActiveExecutions())
		return
	}
	if child.VM.taskPool != nil && !child.VM.taskPool.WaitIdle(2*time.Second) {
		vm.runtimeError(ErrorRuntime, "runtime.VM.reset timed out waiting for %d active task(s); call stop() and let tasks finish before resetting", child.VM.taskPool.Active())
		return
	}
	child.VM.ResetForRequest()
	if child.VM.stopped != nil {
		child.VM.stopped.Store(false)
	}
	child.MainRan = false
	vm.push(NewNative(child))
}

func runtimeVMStop(vm *VM, child *NativeVMValue, args []TinyValue) {
	dontExpectArgs(vm, "runtime.VM.stop", args)
	child.VM.Stop()
	vm.push(NewNative(child))
}

func runtimeVMSetGlobal(vm *VM, child *NativeVMValue, args []TinyValue) {
	expectArgs(vm, "runtime.VM.setGlobal", args, 2)
	name := argString(vm, "runtime.VM.setGlobal", args, 0)
	value := cloneValue(args[1])

	setRuntimeVMGlobal(child.VM, name, value)
	child.InjectedGlobal[name] = value

	vm.push(NewNative(child))
}

func runtimeVMExposeFunction(vm *VM, child *NativeVMValue, args []TinyValue) {
	expectArgs(vm, "runtime.VM.exposeFunction", args, 2)
	name := argString(vm, "runtime.VM.exposeFunction", args, 0)

	var fn FunctionValue
	switch raw := args[1].Value.(type) {
	case FunctionValue:
		fn = raw
	case *FunctionValue:
		fn = *raw
	default:
		vm.runtimeError(ErrorType, "runtime.VM.exposeFunction expected function, got %s", TypeName(args[1]))
		return
	}
	value := NewNative(&HostFunctionValue{
		VM:       vm,
		Function: fn,
		Name:     name,
	})
	setRuntimeVMGlobal(child.VM, name, value)
	child.InjectedGlobal[name] = value

	vm.push(NewNative(child))
}

func runtimeVMInfo(vm *VM, child *NativeVMValue, args []TinyValue) {
	dontExpectArgs(vm, "runtime.VM.info", args)

	stdlib := ObjectValue{}
	if child.AllowedStdlib == nil {
		stdlib["*"] = NewNative(true)
	} else {
		for name, allowed := range child.AllowedStdlib {
			stdlib[name] = NewNative(allowed)
		}
	}

	vm.push(NewNative(ObjectValue{
		"loaded":        NewNative(child.Loaded),
		"isolated":      NewNative(child.Isolated),
		"runMainOnLoad": NewNative(child.RunMainOnLoad),
		"functions":     NewInt(len(child.VM.functions)),
		"classes":       NewInt(len(child.VM.classes)),
		"interfaces":    NewInt(len(child.VM.interfaces)),
		"allowedStdlib": NewNative(stdlib),
	}))
}

func runtimeVMByteSlice(vm *VM, value TinyValue) []byte {
	switch raw := value.Value.(type) {
	case *BufferValue:
		bytes := make([]byte, len(raw.Bytes))
		copy(bytes, raw.Bytes)
		return bytes
	case BufferValue:
		bytes := make([]byte, len(raw.Bytes))
		copy(bytes, raw.Bytes)
		return bytes
	case string:
		return []byte(raw)
	default:
		vm.runtimeError(ErrorType, "runtime.VM.loadBytecode expected buffer or string, got %s", TypeName(value))
		return nil
	}
}

func looksLikeTinySource(source string) bool {
	trimmed := strings.TrimSpace(source)
	if trimmed == "" {
		return false
	}
	if strings.HasPrefix(trimmed, "TBC") || strings.HasPrefix(trimmed, "{") {
		return false
	}
	return strings.ContainsAny(trimmed, "\n;(){}") ||
		strings.HasPrefix(trimmed, "import ") ||
		strings.HasPrefix(trimmed, "export ") ||
		strings.HasPrefix(trimmed, "fn ") ||
		strings.HasPrefix(trimmed, "const ") ||
		strings.HasPrefix(trimmed, "let ")
}

func runRuntimeVMMain(vm *VM, child *NativeVMValue) {
	ensureRuntimeVMLoaded(vm, child)
	child.VM.ResetForRequest()
	child.VM.execute(-1)
}

func ensureRuntimeVMLoaded(vm *VM, child *NativeVMValue) {
	if child == nil || child.VM == nil || !child.Loaded {
		vm.runtimeError(ErrorRuntime, "runtime.VM has no loaded bytecode")
	}
}

func setRuntimeVMGlobal(vm *VM, name string, value TinyValue) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}

	slot, exists := vm.globalNames[name]
	if !exists {
		slot = vm.nextGlobalSlot()
		vm.globalNames[name] = slot
	}

	for len(*vm.globals) <= slot {
		*vm.globals = append(*vm.globals, NewNull())
	}
	(*vm.globals)[slot] = cloneValue(value)
	vm.globalConstants[name] = false
	vm.globalVersion++
}

func (vm *VM) nextGlobalSlot() int {
	slot := len(*vm.globals)
	for _, existingSlot := range vm.globalNames {
		if existingSlot >= slot {
			slot = existingSlot + 1
		}
	}
	return slot
}
