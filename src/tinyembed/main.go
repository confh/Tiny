package main

/*
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

typedef const char* (*TinyCallback)(const char* args_json);

typedef struct TinyBuffer {
	unsigned char* data;
	int length;
} TinyBuffer;

static inline const char* callTinyCallback(TinyCallback cb, const char* args_json) {
	return cb(args_json);
}
*/
import "C"

import (
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"

	"language.com/src/bytecode"
	tinycompiler "language.com/src/compiler"
	tinyloader "language.com/src/loader"
	"language.com/src/vm"
)

type handle struct {
	mu *sync.Mutex
	vm *vm.VM
}

var handles = map[uint64]*handle{}
var nextHandle uint64 = 1
var handlesMu sync.Mutex
var lastErrorMu sync.Mutex
var lastError string

func init() {
	vm.SetRuntimeBytecodeLoader(func(data []byte) vm.RuntimeBytecodeProgram {
		mainInstructions, functions, classes, interfaces, globalIndex := bytecode.LoadBytecodeFromBytes(data)
		return vm.RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})

	vm.SetRuntimeSourceLoader(func(source string, file string) vm.RuntimeBytecodeProgram {
		return compileRuntimeProgram(source, file)
	})

	vm.SetCompileSourceFunc(func(source string, file string) []byte {
		program := compileRuntimeProgram(source, file)
		return bytecode.SaveBytecodeToBytes(program.MainInstructions, program.Functions, program.Classes, program.Interfaces, program.GlobalIndex, false, false)
	})

	vm.SetCompileFileFunc(func(path string) []byte {
		program := tinyloader.LoadProgram(path)
		compiled := compileProgram(program)
		return bytecode.SaveBytecodeToBytes(compiled.MainInstructions, compiled.Functions, compiled.Classes, compiled.Interfaces, compiled.GlobalIndex, false, false)
	})
}

func setLastError(err string) {
	lastErrorMu.Lock()
	lastError = err
	lastErrorMu.Unlock()
}

func clearLastError() {
	setLastError("")
}

func handlePanic(ok *bool) {
	if r := recover(); r != nil {
		setLastError(fmt.Sprint(r))
		*ok = false
	}
}

func getHandle(id C.uint64_t) *handle {
	handlesMu.Lock()
	h := handles[uint64(id)]
	handlesMu.Unlock()
	return h
}

func emptyTinyBuffer() C.TinyBuffer {
	return C.TinyBuffer{data: nil, length: 0}
}

func makeTinyBuffer(data []byte) C.TinyBuffer {
	if len(data) == 0 {
		return emptyTinyBuffer()
	}
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		setLastError("failed to allocate TinyBuffer")
		return emptyTinyBuffer()
	}
	copy(unsafe.Slice((*byte)(ptr), len(data)), data)
	return C.TinyBuffer{
		data:   (*C.uchar)(ptr),
		length: C.int(len(data)),
	}
}

func loadBytecode(data []byte) C.uint64_t {
	mainInstructions, functions, classes, interfaces, globalIndex := bytecode.LoadBytecodeFromBytes(data)

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
	})
	tinyVM.InstallGlobalIndex(globalIndex)

	handlesMu.Lock()
	id := nextHandle
	nextHandle++
	handles[id] = &handle{mu: &sync.Mutex{}, vm: tinyVM}
	handlesMu.Unlock()

	return C.uint64_t(id)
}

func compileProgram(program vm.Program) vm.RuntimeBytecodeProgram {
	compiler := tinycompiler.NewCompiler()
	compiler.SetPreserveAllFunctions(true)
	mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	return vm.RuntimeBytecodeProgram{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		GlobalIndex:      globalIndex,
	}
}

func compileRuntimeProgram(source string, file string) vm.RuntimeBytecodeProgram {
	lexer := vm.NewLexer(source, file)
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()
	return compileProgram(program)
}

func compileSourceBytes(source string, file string) []byte {
	program := compileRuntimeProgram(source, file)
	return bytecode.SaveBytecodeToBytes(program.MainInstructions, program.Functions, program.Classes, program.Interfaces, program.GlobalIndex, false, false)
}

func compileFileBytes(path string) []byte {
	program := tinyloader.LoadProgram(path)
	compiled := compileProgram(program)
	return bytecode.SaveBytecodeToBytes(compiled.MainInstructions, compiled.Functions, compiled.Classes, compiled.Interfaces, compiled.GlobalIndex, false, false)
}

func loadSource(source string, file string) C.uint64_t {
	program := compileRuntimeProgram(source, file)

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: program.MainInstructions,
		Functions:        program.Functions,
		Classes:          program.Classes,
		Interfaces:       program.Interfaces,
		Packed:           false,
	})
	tinyVM.InstallGlobalIndex(program.GlobalIndex)

	handlesMu.Lock()
	id := nextHandle
	nextHandle++
	handles[id] = &handle{mu: &sync.Mutex{}, vm: tinyVM}
	handlesMu.Unlock()

	return C.uint64_t(id)
}

//export TinyLoadBytecode
func TinyLoadBytecode(ptr *C.uchar, length C.int) C.uint64_t {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if ptr == nil || length <= 0 {
		setLastError("TinyLoadBytecode received empty bytecode")
		return 0
	}

	data := C.GoBytes(unsafe.Pointer(ptr), length)
	id := loadBytecode(data)
	if !ok {
		return 0
	}
	return id
}

//export TinyLoadSource
func TinyLoadSource(source *C.char) C.uint64_t {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if source == nil {
		setLastError("TinyLoadSource received null source")
		return 0
	}

	id := loadSource(C.GoString(source), "<embedded>")
	if !ok {
		return 0
	}
	return id
}

//export TinyLoadFile
func TinyLoadFile(path *C.char) C.uint64_t {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if path == nil {
		setLastError("TinyLoadFile received null path")
		return 0
	}

	program := tinyloader.LoadProgram(C.GoString(path))
	compiled := compileProgram(program)
	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: compiled.MainInstructions,
		Functions:        compiled.Functions,
		Classes:          compiled.Classes,
		Interfaces:       compiled.Interfaces,
		Packed:           false,
	})
	tinyVM.InstallGlobalIndex(compiled.GlobalIndex)

	handlesMu.Lock()
	id := nextHandle
	nextHandle++
	handles[id] = &handle{mu: &sync.Mutex{}, vm: tinyVM}
	handlesMu.Unlock()

	if !ok {
		return 0
	}
	return C.uint64_t(id)
}

//export TinyRun
func TinyRun(id C.uint64_t) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vm.Run()
	if !ok {
		return -1
	}
	return 0
}

//export TinyRunSource
func TinyRunSource(source *C.char) C.int {
	id := TinyLoadSource(source)
	if id == 0 {
		return -1
	}
	defer TinyFree(id)
	return TinyRun(id)
}

//export TinySetGlobalJson
func TinySetGlobalJson(id C.uint64_t, name *C.char, jsonValue *C.char) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if name == nil || jsonValue == nil {
		setLastError("TinySetGlobalJson received null name or value")
		return -1
	}

	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	value, err := vm.TinyValueFromJSONBytes([]byte(C.GoString(jsonValue)))
	if err != nil {
		setLastError(err.Error())
		return -1
	}

	h.vm.SetGlobalValue(C.GoString(name), value)
	if !ok {
		return -1
	}
	return 0
}

//export TinySetGlobalString
func TinySetGlobalString(id C.uint64_t, name *C.char, value *C.char) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if name == nil || value == nil {
		setLastError("TinySetGlobalString received null name or value")
		return -1
	}
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vm.SetGlobalValue(C.GoString(name), vm.NewNative(C.GoString(value)))
	if !ok {
		return -1
	}
	return 0
}

//export TinySetGlobalNumber
func TinySetGlobalNumber(id C.uint64_t, name *C.char, value C.double) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if name == nil {
		setLastError("TinySetGlobalNumber received null name")
		return -1
	}
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vm.SetGlobalValue(C.GoString(name), vm.NewNative(float64(value)))
	if !ok {
		return -1
	}
	return 0
}

//export TinySetGlobalBool
func TinySetGlobalBool(id C.uint64_t, name *C.char, value C.int) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if name == nil {
		setLastError("TinySetGlobalBool received null name")
		return -1
	}
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vm.SetGlobalValue(C.GoString(name), vm.NewNative(value != 0))
	if !ok {
		return -1
	}
	return 0
}

//export TinyCallJson
func TinyCallJson(id C.uint64_t, functionName *C.char, argsJson *C.char) *C.char {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if functionName == nil {
		setLastError("TinyCallJson received null function name")
		return nil
	}

	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	args := []vm.TinyValue{}
	if argsJson != nil {
		argValue, err := vm.TinyValueFromJSONBytes([]byte(C.GoString(argsJson)))
		if err != nil {
			setLastError(err.Error())
			return nil
		}
		array, ok := argValue.Value.(*vm.ArrayValue)
		if !ok {
			setLastError("TinyCallJson expects argsJson to be a JSON array")
			return nil
		}
		args = array.Elements
	}

	result, err := h.vm.CallFunctionValueByName(C.GoString(functionName), args)
	if err != nil {
		setLastError(err.Error())
		return nil
	}

	resultJSON, err := vm.TinyValueToJSONBytes(result)
	if err != nil {
		setLastError(err.Error())
		return nil
	}
	if !ok {
		return nil
	}
	return C.CString(string(resultJSON))
}

//export TinyCallString
func TinyCallString(id C.uint64_t, functionName *C.char, arg *C.char) *C.char {
	clearLastError()
	if functionName == nil {
		setLastError("TinyCallString received null function name")
		return nil
	}
	argsJSON := "[]"
	if arg != nil {
		encoded, err := json.Marshal([]string{C.GoString(arg)})
		if err != nil {
			setLastError(err.Error())
			return nil
		}
		argsJSON = string(encoded)
	}
	cArgs := C.CString(argsJSON)
	defer C.free(unsafe.Pointer(cArgs))
	return TinyCallJson(id, functionName, cArgs)
}

//export TinyHasFunction
func TinyHasFunction(id C.uint64_t, functionName *C.char) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if functionName == nil {
		setLastError("TinyHasFunction received null function name")
		return 0
	}
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return 0
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !ok {
		return 0
	}
	if h.vm.HasFunction(C.GoString(functionName)) {
		return 1
	}
	return 0
}

//export TinyListExports
func TinyListExports(id C.uint64_t) *C.char {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return nil
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	data, err := json.Marshal(h.vm.FunctionNames())
	if err != nil {
		setLastError(err.Error())
		return nil
	}
	if !ok {
		return nil
	}
	return C.CString(string(data))
}

//export TinyReset
func TinyReset(id C.uint64_t) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vm.ResetEmbedState()
	if !ok {
		return -1
	}
	return 0
}

//export TinyCompileSource
func TinyCompileSource(source *C.char) C.TinyBuffer {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if source == nil {
		setLastError("TinyCompileSource received null source")
		return emptyTinyBuffer()
	}
	data := compileSourceBytes(C.GoString(source), "<embedded>")
	if !ok {
		return emptyTinyBuffer()
	}
	return makeTinyBuffer(data)
}

//export TinyCompileFile
func TinyCompileFile(path *C.char) C.TinyBuffer {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if path == nil {
		setLastError("TinyCompileFile received null path")
		return emptyTinyBuffer()
	}
	data := compileFileBytes(C.GoString(path))
	if !ok {
		return emptyTinyBuffer()
	}
	return makeTinyBuffer(data)
}

//export TinyCompileSourcePtr
func TinyCompileSourcePtr(source *C.char, outLength *C.int) *C.char {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if source == nil || outLength == nil {
		setLastError("TinyCompileSourcePtr received null source or outLength")
		return nil
	}
	data := compileSourceBytes(C.GoString(source), "<embedded>")
	if !ok || len(data) == 0 {
		return nil
	}
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		setLastError("failed to allocate memory for bytecode compilation")
		return nil
	}
	C.memcpy(ptr, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	*outLength = C.int(len(data))
	return (*C.char)(ptr)
}

//export TinyCompileFilePtr
func TinyCompileFilePtr(path *C.char, outLength *C.int) *C.char {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if path == nil || outLength == nil {
		setLastError("TinyCompileFilePtr received null path or outLength")
		return nil
	}
	data := compileFileBytes(C.GoString(path))
	if !ok || len(data) == 0 {
		return nil
	}
	ptr := C.malloc(C.size_t(len(data)))
	if ptr == nil {
		setLastError("failed to allocate memory for file compilation")
		return nil
	}
	C.memcpy(ptr, unsafe.Pointer(&data[0]), C.size_t(len(data)))
	*outLength = C.int(len(data))
	return (*C.char)(ptr)
}

//export TinyFreeBuffer
func TinyFreeBuffer(buffer C.TinyBuffer) {
	if buffer.data != nil {
		C.free(unsafe.Pointer(buffer.data))
	}
}

//export TinySetCallback
func TinySetCallback(id C.uint64_t, name *C.char, callback C.TinyCallback) C.int {
	clearLastError()
	ok := true
	defer handlePanic(&ok)
	if name == nil || callback == nil {
		setLastError("TinySetCallback received null name or callback")
		return -1
	}

	h := getHandle(id)
	if h == nil {
		setLastError("unknown Tiny VM handle")
		return -1
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	callbackName := C.GoString(name)
	h.vm.SetCallbackFunction(callbackName, func(args []vm.TinyValue) (vm.TinyValue, error) {
		argsJSON, err := vm.TinyValueToJSONBytes(vm.NewNative(&vm.ArrayValue{Elements: args}))
		if err != nil {
			return vm.NewNull(), err
		}
		cArgs := C.CString(string(argsJSON))
		defer C.free(unsafe.Pointer(cArgs))

		rawResult := C.callTinyCallback(callback, cArgs)
		if rawResult == nil {
			return vm.NewNull(), nil
		}
		return vm.TinyValueFromJSONBytes([]byte(C.GoString(rawResult)))
	})

	if !ok {
		return -1
	}
	return 0
}

//export TinyLastError
func TinyLastError() *C.char {
	lastErrorMu.Lock()
	defer lastErrorMu.Unlock()
	return C.CString(lastError)
}

//export TinyFreeString
func TinyFreeString(ptr *C.char) {
	if ptr != nil {
		C.free(unsafe.Pointer(ptr))
	}
}

//export TinyFree
func TinyFree(id C.uint64_t) {
	handlesMu.Lock()
	delete(handles, uint64(id))
	handlesMu.Unlock()
}

func main() {}
