package vm

import (
	"context"
	"encoding/binary"
	"fmt"
	"maps"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"

	stdjson "encoding/json"
	"io"

	. "language.com/src/tinyerrors"
)

type stdJSONWrapper struct{}

func (stdJSONWrapper) Marshal(v any) ([]byte, error) {
	return stdjson.Marshal(v)
}

func (stdJSONWrapper) Unmarshal(data []byte, v any) error {
	return stdjson.Unmarshal(data, v)
}

func (stdJSONWrapper) MarshalIndent(v any, prefix, indent string) ([]byte, error) {
	return stdjson.MarshalIndent(v, prefix, indent)
}

func (stdJSONWrapper) NewDecoder(r io.Reader) *stdjson.Decoder {
	return stdjson.NewDecoder(r)
}

func (stdJSONWrapper) NewEncoder(w io.Writer) *stdjson.Encoder {
	return stdjson.NewEncoder(w)
}

var json stdJSONWrapper

type NativeCallFrame struct {
	Name   string
	File   string
	Line   int
	Column int
}


type TryHandler struct {
	CatchIP int
	Name    string
	Slot    int
	IsLocal bool

	FrameDepth int
}

type DeferHandler struct {
	Function   FunctionValue
	FrameDepth int
}

type globalPairInlineCache struct {
	Version     uint64
	GlobalSlotA int
	GlobalSlotB int
	ValueA      TinyValue
	ValueB      TinyValue
}

type Frame struct {
	function     Function
	ip           int
	cellSlab     []Cell
	locals       []*Cell
	instructions []Instruction
	methodClass  string
	methodCache  []MethodCallCache

	lockedMutexes []*NativeMutexValue

	returnOverride    TinyValue
	hasReturnOverride bool
	hasEscapedLocals  bool

	hasMainCallSite bool
	mainCallSite    NativeCallFrame
}

type VMInfo struct {
	MainInstructions []Instruction
	MainDebugInfo    []DebugInfo
	Functions        map[string]Function
	Classes          map[string]Class
	Interfaces       map[string]Interface
	Packed           bool
	JITDisabled      bool
	Isolated         bool
	AllowedStdlib    map[string]bool
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	mainDebugInfo    []DebugInfo
	functions        map[string]Function
	classes          map[string]Class
	interfaces       map[string]Interface
	framePool        []*Frame
	functionList     []Function

	taskPool *VMPool
	stopped  *atomic.Bool
	active   *atomic.Int64

	observerStats *ObserverRuntimeStats

	packed bool

	jitDisabled bool
	isolated    bool

	allowedStdlib map[string]bool

	mainMethodCache []MethodCallCache

	jitFunctions map[string]*JitFunction

	jitArrayMirrorCache  map[*ArrayValue]jitArrayMirror
	jitObjectMirrorCache map[uintptr]jitObjectMirror

	jitHeapTop        uint32
	jitInitialHeapTop uint32

	objectShapes   [][]string
	objectShapeIDs map[string]uint32

	jitStrings   []string
	jitStringMap map[string]uint32

	jitWasmBytes []byte
	jitMetas     []JitFunctionMeta

	jitStringAddrs map[string]uint32

	propertyOffsets    map[string]uint32
	nextPropertyOffset uint32

	nativeFrames []NativeCallFrame
	currentInstr Instruction

	top int

	stoppedCheckCounter int

	globalTypes map[string]TypeHint

	cliArgs []string

	tryHandlers   []TryHandler
	deferHandlers []DeferHandler

	mu *sync.RWMutex

	ip int

	stack           []TinyValue
	globals         *[]TinyValue
	globalNames     map[string]int
	globalConstants map[string]bool
	globalVersion   uint64
	globalPairIC    map[uint64]globalPairInlineCache

	frames []*Frame

	wazeroRuntime    wazero.Runtime
	wazeroCtx        context.Context
	wasmModule       api.Module
	jitModule        api.Module
	jitHeapTopGlobal api.Global
	wasmMu           *sync.Mutex

	writeScratch  [8]byte
	stringScratch [16]byte
}

func intToString(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + uintToString(uint64(-(n+1))+1)
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}

	return string(buf[i:])
}

func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + uintToString(uint64(-(n+1))+1)
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}

	return string(buf[i:])
}

func uintToString(n uint64) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}

	return string(buf[i:])
}

func FloatToString(val float64) string {
	return strconv.FormatFloat(val, 'f', 6, 64)
}

func (vm *VM) mainDebugInfoAt(ip int) DebugInfo {
	if ip >= 0 && ip < len(vm.mainDebugInfo) {
		return vm.mainDebugInfo[ip]
	}
	return DebugInfo{}
}

func frameDebugInfo(frame *Frame, ip int) DebugInfo {
	if frame != nil && ip >= 0 && ip < len(frame.function.DebugInfo) {
		return frame.function.DebugInfo[ip]
	}
	return DebugInfo{}
}

func instanceValue(value TinyValue) (*InstanceValue, bool) {
	if value.IsInt {
		return nil, false
	}
	inst, ok := value.Value.(*InstanceValue)
	return inst, ok && inst != nil
}

func NewVM(info VMInfo) *VM {
	mainInstructions := info.MainInstructions
	mainDebugInfo := info.MainDebugInfo
	functions := info.Functions
	classes := info.Classes
	interfaces := info.Interfaces
	packed := info.Packed
	jitDisabled := info.JITDisabled
	mainInstructions, functions, functionList, mainDebugInfo := normalizeFunctionIDs(mainInstructions, functions, mainDebugInfo)

	globalsSlice := make([]TinyValue, 0, 256)

	vm := &VM{
		start:                time.Now().UnixMilli(),
		mainInstructions:     mainInstructions,
		mainDebugInfo:        mainDebugInfo,
		functions:            functions,
		interfaces:           interfaces,
		functionList:         functionList,
		classes:              classes,
		globals:              &globalsSlice,
		mainMethodCache:      make([]MethodCallCache, len(mainInstructions)),
		jitFunctions:         map[string]*JitFunction{},
		jitArrayMirrorCache:  map[*ArrayValue]jitArrayMirror{},
		jitObjectMirrorCache: map[uintptr]jitObjectMirror{},
		globalNames:          map[string]int{},
		globalConstants:      map[string]bool{},
		globalPairIC:         map[uint64]globalPairInlineCache{},
		mu:                   &sync.RWMutex{},
		cliArgs:              []string{},
		globalTypes:          map[string]TypeHint{},
		observerStats:        newObserverRuntimeStats(),
		stopped:              &atomic.Bool{},
		active:               &atomic.Int64{},
		packed:               packed,
		jitDisabled:          jitDisabled,
		isolated:             info.Isolated,
		allowedStdlib:        cloneStringBoolMap(info.AllowedStdlib),
		top:                  0,
		stack:                make([]TinyValue, 1024),
		framePool:            make([]*Frame, 0, 1024),
		frames:               []*Frame{},
		wazeroCtx:            context.Background(),
		wasmMu:               &sync.Mutex{},
		propertyOffsets:      map[string]uint32{},
		nextPropertyOffset:   16,
		objectShapes:         [][]string{},
		objectShapeIDs:       map[string]uint32{},
		jitStrings:           []string{},
		jitStringMap:         map[string]uint32{},
		jitStringAddrs:       map[string]uint32{},
	}

	EnsureStdModuleMethods()

	if !jitDisabled {
		vm.CompileAllJit()
	}

	maxActive, maxIdle := defaultTaskPoolLimits()

	vm.taskPool = NewVMPool(maxActive, maxIdle, func() *VM {
		return vm.CloneForTask()
	})

	return vm
}

func cloneStringBoolMap(src map[string]bool) map[string]bool {
	if src == nil {
		return nil
	}
	dst := make(map[string]bool, len(src))
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func (vm *VM) RegisterJitString(s string) float64 {
	if vm.jitStringMap == nil {
		vm.jitStringMap = make(map[string]uint32)
	}
	if id, exists := vm.jitStringMap[s]; exists {
		return float64(id)
	}
	id := uint32(len(vm.jitStrings))
	vm.jitStrings = append(vm.jitStrings, s)
	vm.jitStringMap[s] = id
	return float64(id)
}

func normalizeFunctionIDs(
	mainInstructions []Instruction,
	functions map[string]Function,
	mainDebugInfo []DebugInfo,
) ([]Instruction, map[string]Function, []Function, []DebugInfo) {
	names := make([]string, 0, len(functions))

	for name := range functions {
		names = append(names, name)
	}

	sort.Strings(names)

	ids := map[string]int{}
	functionList := make([]Function, len(names))

	for id, name := range names {
		ids[name] = id

		fn := functions[name]
		if fn.Name == "" || strings.HasPrefix(name, "__jit_region_") {
			fn.Name = name
		}
		fn.ID = id
		functions[name] = fn
	}

	remapDirectCallIDs(mainInstructions, ids)

	for name, fn := range functions {
		remapDirectCallIDs(fn.Instructions, ids)

		id := ids[name]
		fn.ID = id
		functions[name] = fn
		functionList[id] = fn
	}

	return mainInstructions, functions, functionList, mainDebugInfo
}

func remapDirectCallIDs(instructions []Instruction, ids map[string]int) {
	for i := range instructions {
		switch instructions[i].Op {
		case OP_CALL_DIRECT:
			info, ok := instructions[i].Value.(DirectCallInfo)
			if !ok {
				continue
			}

			id, exists := ids[info.Name]
			if !exists {
				continue
			}

			info.ID = id
			instructions[i].Value = info

		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instructions[i].Value.(CallDirectSubConstInfo)
			if !ok {
				continue
			}

			id, exists := ids[info.FnName]
			if !exists {
				continue
			}

			info.FnID = id
			instructions[i].Value = info
		}
	}
}

func methodOwnerClass(functionName string) string {
	dot := strings.LastIndex(functionName, ".")
	if dot == -1 {
		return ""
	}

	return functionName[:dot]
}

func (vm *VM) currentMethodClass() string {
	if len(vm.frames) == 0 {
		return ""
	}

	frame := vm.frames[len(vm.frames)-1]
	return frame.methodClass
}

func (vm *VM) SetCLIArgs(args []string) {
	vm.cliArgs = args
}

func isJitCompatible(val TinyValue) bool {
	if val.IsInt {
		return true
	}
	switch val.Value.(type) {
	case float64, float32, int, int64, bool, string,
		ObjectValue, *ObjectValue, WasmObjectValue,
		ArrayValue, *ArrayValue, WasmArrayValue:
		return true
	default:
		return false
	}
}

type JitDeoptError struct {
	FunctionName string
	Reason       string
	DeoptIP      int
	Locals       []TinyValue
	Constants    []bool
	LocalTypes   []TypeHint
	Stack        []TinyValue
	StackTop     int
}

func (e JitDeoptError) Error() string {
	if e.Reason != "" {
		return "jit deopt: " + e.Reason
	}
	return "jit deopt"
}

func jitDeoptFromError(err error) (JitDeoptError, bool) {
	if err == nil {
		return JitDeoptError{}, false
	}

	switch e := err.(type) {
	case JitDeoptError:
		return e, true
	case *JitDeoptError:
		if e == nil {
			return JitDeoptError{}, false
		}
		return *e, true
	default:
		return JitDeoptError{}, false
	}
}

func (vm *VM) resumeJitDeopt(fn Function, args []TinyValue, deopt JitDeoptError) {
	if deopt.FunctionName != "" && deopt.FunctionName != fn.Name {
		vm.fatalError(ErrorInternal, "JIT deopt function mismatch: got %s, expected %s", deopt.FunctionName, fn.Name)
	}
	if deopt.DeoptIP < 0 || deopt.DeoptIP > len(fn.Instructions) {
		vm.fatalError(ErrorInternal, "JIT deopt IP out of range for %s: %d", fn.Name, deopt.DeoptIP)
	}

	frame := vm.getFrame(fn)
	frame.ip = deopt.DeoptIP

	for i, arg := range args {
		if i >= len(frame.locals) {
			break
		}
		setCellValue(frame.locals[i], arg)
		frame.locals[i].Constant = false
		if i < len(fn.Params) {
			frame.locals[i].TypeHint = fn.Params[i].TypeHint
		}
	}

	for i, local := range deopt.Locals {
		if i >= len(frame.locals) {
			break
		}
		setCellValue(frame.locals[i], local)
	}
	for i, constant := range deopt.Constants {
		if i >= len(frame.locals) {
			break
		}
		frame.locals[i].Constant = constant
	}
	for i, typ := range deopt.LocalTypes {
		if i >= len(frame.locals) {
			break
		}
		frame.locals[i].TypeHint = typ
	}

	stack := deopt.Stack
	if deopt.StackTop > 0 && deopt.StackTop <= len(stack) {
		stack = stack[:deopt.StackTop]
	}
	for _, value := range stack {
		vm.push(value)
	}

	vm.pushFrame(frame)
}

func (vm *VM) argsMatchJit(jitFn *JitFunction, args []TinyValue) bool {
	if len(args) != jitFn.paramCount {
		return false
	}
	for i, arg := range args {
		if !isJitCompatible(arg) {
			return false
		}
		expected := jitFn.expectedParamType(i)
		if expected != stackTypeUnknown && !jitValueMatchesType(arg, expected) {
			return false
		}
	}
	return true
}

func (vm *VM) stackArgsMatchJit(jitFn *JitFunction, argCount int) bool {
	if vm.top < argCount {
		return false
	}
	if argCount != jitFn.paramCount {
		return false
	}
	start := vm.top - argCount
	for i := 0; i < argCount; i++ {
		arg := vm.stack[start+i]
		if !isJitCompatible(arg) {
			return false
		}
		expected := jitFn.expectedParamType(i)
		if expected != stackTypeUnknown && !jitValueMatchesType(arg, expected) {
			return false
		}
	}
	return true
}

func (vm *VM) currentMainCallSite() (NativeCallFrame, bool) {
	if len(vm.frames) != 0 {
		return NativeCallFrame{}, false
	}

	ip := vm.ip - 1
	if ip < 0 || ip >= len(vm.mainInstructions) {
		return NativeCallFrame{}, false
	}

	dbg := vm.mainDebugInfoAt(ip)

	return NativeCallFrame{
		Name:   "<main>",
		File:   dbg.File,
		Line:   dbg.Line,
		Column: dbg.Column,
	}, true
}

func (vm *VM) pushFrame(frame *Frame) {
	vm.frames = append(vm.frames, frame)
}

func (vm *VM) getFrame(fn Function) *Frame {
	var frame *Frame

	if len(vm.framePool) > 0 {
		last := len(vm.framePool) - 1
		frame = vm.framePool[last]
		vm.framePool = vm.framePool[:last]

		if cap(frame.cellSlab) < fn.LocalCount {
			frame.cellSlab = make([]Cell, fn.LocalCount)
			frame.locals = make([]*Cell, fn.LocalCount)
			for i := range frame.cellSlab {
				frame.locals[i] = &frame.cellSlab[i]
			}
		} else {
			frame.cellSlab = frame.cellSlab[:fn.LocalCount]
			frame.locals = frame.locals[:fn.LocalCount]
		}

		frame.function = fn
		frame.ip = 0
		frame.instructions = fn.Instructions
		frame.methodClass = ""
		frame.returnOverride = TinyValue{}
		frame.hasReturnOverride = false
		frame.hasEscapedLocals = false

		if len(frame.methodCache) != len(frame.instructions) {
			frame.methodCache = make([]MethodCallCache, len(frame.instructions))
		} else {
			for i := range frame.methodCache {
				frame.methodCache[i] = MethodCallCache{}
			}
		}

		return frame
	}

	cellSlab := make([]Cell, fn.LocalCount)
	locals := make([]*Cell, fn.LocalCount)
	for i := range cellSlab {
		locals[i] = &cellSlab[i]
	}

	frame = &Frame{
		cellSlab:     cellSlab,
		locals:       locals,
		function:     fn,
		instructions: fn.Instructions,
		methodCache:  make([]MethodCallCache, len(fn.Instructions)),
	}

	return frame
}

func (vm *VM) releaseFrame(frame *Frame) {
	if frame.hasEscapedLocals {
		return
	}

	if len(vm.framePool) >= 1024 {
		return
	}

	for i := range frame.cellSlab {
		frame.cellSlab[i] = Cell{}
	}

	frame.function = Function{}
	frame.instructions = nil
	frame.ip = 0

	vm.framePool = append(vm.framePool, frame)
}

func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneObjectShapes(in [][]string) [][]string {
	out := make([][]string, len(in))
	for i := range in {
		out[i] = append([]string(nil), in[i]...)
	}
	return out
}

func (vm *VM) hasJitMetaFor(name string) bool {
	for _, meta := range vm.jitMetas {
		if meta.Name == name {
			return true
		}
	}
	return false
}

func (vm *VM) ensureJitReadyFor(name string) {
	if vm.jitDisabled {
		return
	}
	if vm.jitModule != nil {
		return
	}
	if len(vm.jitWasmBytes) == 0 {
		return
	}
	if !vm.hasJitMetaFor(name) {
		return
	}

	vm.InstantiateJitModule()
}

func (vm *VM) CloneForTask() *VM {
	task := &VM{
		start:            vm.start,
		mainInstructions: vm.mainInstructions,
		mainDebugInfo:    vm.mainDebugInfo,
		functions:        vm.functions,
		classes:          vm.classes,
		interfaces:       vm.interfaces,
		functionList:     vm.functionList,
		observerStats:    vm.observerStats,
		taskPool:         vm.taskPool,
		stopped:          vm.stopped,
		active:           vm.active,

		stack:       make([]TinyValue, 256),
		framePool:   make([]*Frame, 0, 256),
		frames:      []*Frame{},
		tryHandlers: []TryHandler{},

		globals:         vm.globals,
		globalNames:     vm.globalNames,
		globalConstants: vm.globalConstants,
		globalVersion:   atomic.LoadUint64(&vm.globalVersion),
		globalPairIC:    map[uint64]globalPairInlineCache{},
		globalTypes:     vm.globalTypes,

		packed:      vm.packed,
		jitDisabled: true, // Force JIT disabled for spawned tasks to avoid deadlocks on wasmMu with long-running main thread JIT calls

		mu: vm.mu,

		cliArgs: vm.cliArgs,

		jitWasmBytes: vm.jitWasmBytes,
		jitMetas:     vm.jitMetas,

		jitStringAddrs: cloneMap(vm.jitStringAddrs),
		jitStrings:     append([]string(nil), vm.jitStrings...),
		jitStringMap:   cloneMap(vm.jitStringMap),

		jitFunctions:         map[string]*JitFunction{},
		jitArrayMirrorCache:  map[*ArrayValue]jitArrayMirror{},
		jitObjectMirrorCache: map[uintptr]jitObjectMirror{},

		jitHeapTop:        vm.jitHeapTop,
		jitInitialHeapTop: vm.jitInitialHeapTop,

		propertyOffsets:    cloneMap(vm.propertyOffsets),
		nextPropertyOffset: vm.nextPropertyOffset,
		objectShapes:       cloneObjectShapes(vm.objectShapes),
		objectShapeIDs:     cloneMap(vm.objectShapeIDs),

		wazeroRuntime:    vm.wazeroRuntime,
		wazeroCtx:        vm.wazeroCtx,
		wasmModule:       vm.wasmModule,
		jitModule:        nil,
		jitHeapTopGlobal: nil,
		wasmMu:           &sync.Mutex{},
	}

	return task
}

func cloneValue(value TinyValue) TinyValue {
	var raw any
	if value.IsInt {
		raw = value.AsInt
	} else {
		raw = value.Value
	}

	switch v := raw.(type) {
	case *InstanceValue:
		if v == nil {
			return NewNull()
		}
		copyFields := ObjectValue{}
		for key, val := range v.Fields {
			copyFields[key] = cloneValue(val)
		}
		return NewNative(&InstanceValue{
			ClassName:      v.ClassName,
			Fields:         copyFields,
			ConstFields:    cloneMap(v.ConstFields),
			PrivateFields:  cloneMap(v.PrivateFields),
			PrivateMethods: cloneMap(v.PrivateMethods),
		})

	case ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range v {
			copyObj[key] = cloneValue(val)
		}

		return NewNative(copyObj)

	case *ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range *v {
			copyObj[key] = cloneValue(val)
		}

		return NewNative(copyObj)

	case *ArrayValue:
		copyArr := &ArrayValue{
			Elements: make([]TinyValue, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return NewNative(copyArr)

	case ArrayValue:
		copyArr := ArrayValue{
			Elements: make([]TinyValue, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return NewNative(copyArr)

	case *BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return NewNative(&BufferValue{
			Bytes: bytes,
		})

	case BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return NewNative(&BufferValue{
			Bytes: bytes,
		})

	case WasmArrayValue:
		return value

	default:
		return value
	}
}

func wrapFunctionArgsForHostVM(args []TinyValue, owner *VM) []TinyValue {
	if len(args) == 0 {
		return args
	}
	wrapped := make([]TinyValue, len(args))
	for i, arg := range args {
		wrapped[i] = wrapFunctionsForHostVM(arg, owner)
	}
	return wrapped
}

func (vm *VM) callHostFunctionValue(host *HostFunctionValue, args []TinyValue) (TinyValue, bool) {
	if host == nil || host.VM == nil {
		vm.fatalError(ErrorRuntime, "host function is not attached to a VM")
	}

	callArgs := wrapFunctionArgsForHostVM(args, vm)
	if host.HasReceiver {
		callArgs = append([]TinyValue{host.Receiver}, callArgs...)
	}

	hostFrameDepth := len(host.VM.frames)
	hostStackDepth := host.VM.top
	hostTryDepth := len(host.VM.tryHandlers)
	hostDeferDepth := len(host.VM.deferHandlers)
	hostNativeDepth := len(host.VM.nativeFrames)

	var result TinyValue
	handledError := false
	func() {
		defer func() {
			if r := recover(); r != nil {
				switch err := r.(type) {
				case LangErrorType:
					host.VM.discardInterruptedCall(hostFrameDepth, hostStackDepth, hostTryDepth, hostDeferDepth, hostNativeDepth)
					vm.runtimeError(err.Kind, "%s", err.Message)
					result = NewNull()
					handledError = true
				case *LangErrorType:
					host.VM.discardInterruptedCall(hostFrameDepth, hostStackDepth, hostTryDepth, hostDeferDepth, hostNativeDepth)
					vm.runtimeError(err.Kind, "%s", err.Message)
					result = NewNull()
					handledError = true
				default:
					panic(r)
				}
			}
		}()
		result = host.VM.callFunctionValue(host.Function, callArgs)
	}()
	return result, !handledError
}

func (vm *VM) discardInterruptedCall(frameDepth int, stackDepth int, tryDepth int, deferDepth int, nativeDepth int) {
	for len(vm.frames) > frameDepth {
		frame := vm.frames[len(vm.frames)-1]
		for _, m := range frame.lockedMutexes {
			m.Unlock()
		}
		vm.frames = vm.frames[:len(vm.frames)-1]
		vm.releaseFrame(frame)
	}

	for i := stackDepth; i < vm.top && i < len(vm.stack); i++ {
		vm.stack[i] = TinyValue{}
	}
	if stackDepth < vm.top {
		vm.top = stackDepth
	}

	if len(vm.tryHandlers) > tryDepth {
		vm.tryHandlers = vm.tryHandlers[:tryDepth]
	}
	if len(vm.deferHandlers) > deferDepth {
		vm.deferHandlers = vm.deferHandlers[:deferDepth]
	}
	if len(vm.nativeFrames) > nativeDepth {
		vm.nativeFrames = vm.nativeFrames[:nativeDepth]
	}
}

func wrapFunctionsForHostVM(value TinyValue, owner *VM) TinyValue {
	return wrapFunctionsForHostVMSeen(value, owner, map[*InstanceValue]*InstanceValue{}, map[*ArrayValue]*ArrayValue{}, map[*ObjectValue]*ObjectValue{})
}

func wrapFunctionsForHostVMSeen(value TinyValue, owner *VM, seenInstances map[*InstanceValue]*InstanceValue, seenArrays map[*ArrayValue]*ArrayValue, seenObjects map[*ObjectValue]*ObjectValue) TinyValue {
	if owner == nil || value.IsInt {
		return value
	}

	switch v := value.Value.(type) {
	case FunctionValue:
		return NewNative(&HostFunctionValue{VM: owner, Function: v, Name: v.Name})
	case *FunctionValue:
		if v == nil {
			return NewNull()
		}
		return NewNative(&HostFunctionValue{VM: owner, Function: *v, Name: v.Name})
	case *HostFunctionValue, *CallbackFunctionValue:
		return value
	case *InstanceValue:
		if v == nil {
			return value
		}
		if wrapped, ok := seenInstances[v]; ok {
			return NewNative(wrapped)
		}
		wrapped := &InstanceValue{
			ClassName:      v.ClassName,
			Fields:         ObjectValue{},
			ConstFields:    cloneMap(v.ConstFields),
			PrivateFields:  cloneMap(v.PrivateFields),
			PrivateMethods: cloneMap(v.PrivateMethods),
		}
		seenInstances[v] = wrapped
		receiver := NewNative(v)
		for key, field := range v.Fields {
			switch fn := field.Value.(type) {
			case FunctionValue:
				hasReceiver := methodOwnerClass(fn.Name) != ""
				host := &HostFunctionValue{VM: owner, Function: fn, Name: fn.Name}
				if hasReceiver {
					host.Receiver = receiver
					host.HasReceiver = true
				}
				wrapped.Fields[key] = NewNative(host)
			case *FunctionValue:
				if fn == nil {
					wrapped.Fields[key] = NewNull()
				} else {
					hasReceiver := methodOwnerClass(fn.Name) != ""
					host := &HostFunctionValue{VM: owner, Function: *fn, Name: fn.Name}
					if hasReceiver {
						host.Receiver = receiver
						host.HasReceiver = true
					}
					wrapped.Fields[key] = NewNative(host)
				}
			default:
				wrapped.Fields[key] = wrapFunctionsForHostVMSeen(field, owner, seenInstances, seenArrays, seenObjects)
			}
		}
		return NewNative(wrapped)
	case ObjectValue:
		wrapped := ObjectValue{}
		for key, field := range v {
			wrapped[key] = wrapFunctionsForHostVMSeen(field, owner, seenInstances, seenArrays, seenObjects)
		}
		return NewNative(wrapped)
	case *ObjectValue:
		if v == nil {
			return value
		}
		if wrapped, ok := seenObjects[v]; ok {
			return NewNative(wrapped)
		}
		wrapped := ObjectValue{}
		seenObjects[v] = &wrapped
		for key, field := range *v {
			wrapped[key] = wrapFunctionsForHostVMSeen(field, owner, seenInstances, seenArrays, seenObjects)
		}
		return NewNative(wrapped)
	case *ArrayValue:
		if v == nil {
			return value
		}
		if wrapped, ok := seenArrays[v]; ok {
			return NewNative(wrapped)
		}
		wrapped := &ArrayValue{Elements: make([]TinyValue, len(v.Elements))}
		seenArrays[v] = wrapped
		for i, item := range v.Elements {
			wrapped.Elements[i] = wrapFunctionsForHostVMSeen(item, owner, seenInstances, seenArrays, seenObjects)
		}
		return NewNative(wrapped)
	case ArrayValue:
		array := &ArrayValue{Elements: make([]TinyValue, len(v.Elements))}
		for i, item := range v.Elements {
			array.Elements[i] = wrapFunctionsForHostVMSeen(item, owner, seenInstances, seenArrays, seenObjects)
		}
		return NewNative(array)
	default:
		return value
	}
}

func cellValue(cell *Cell) TinyValue {
	if cell.IsInt {
		return NewInt(cell.Int)
	}
	return cell.Value
}

func setCellValue(cell *Cell, value TinyValue) {
	if value.IsInt {
		cell.Int = value.AsInt
		cell.Value = TinyValue{}
		cell.IsInt = true
	} else {
		cell.Value = value
		cell.Int = 0
		cell.IsInt = false
	}
}

func frameLocalValue(frame *Frame, slot int, op string) TinyValue {
	if slot < 0 || slot >= len(frame.locals) {
		LangError(ErrorInternal, "local slot out of range in %s", op)
	}

	cell := frame.locals[slot]
	if cell == nil {
		LangError(ErrorInternal, "local cell is nil in %s", op)
	}

	return cellValue(cell)
}

func propertyValue(vm *VM, objectValue TinyValue, name string) TinyValue {
	if obj, ok := objectValue.Value.(WasmObjectValue); ok {
		offset := vm.getPropertyOffset(name)
		addr := uint32(obj.Address) + offset

		tag := vm.ReadWasmFloat(addr)
		val := vm.ReadWasmFloat(addr + 8)

		switch tag {
		case 1.0:
			return NewNative(val)
		case 2.0:
			return NewNative(val != 0.0)
		case 4.0:
			return NewNative(WasmObjectValue{Address: val, VM: vm})
		case 5.0:
			return NewNative(WasmArrayValue{Address: val, VM: vm})
		case 6.0:
			strVal, ok := vm.readWasmStringMaybe(uint32(val))
			if ok {
				return NewNative(strVal)
			}
			return NewNull()
		case 0.0:
			return NewNull()
		}
	}

	if inst, ok := instanceValue(objectValue); ok {
		if !vm.canAccessField(inst, name) {
			vm.fatalError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := inst.Fields[name]
		if !exists {
			return NewNull()
		}

		return value
	}

	if object, ok := vm.valueAsObjectForRead(objectValue); ok {
		value, exists := object[name]
		if !exists {
			return NewNull()
		}

		return value
	}

	if ns, ok := objectValue.Value.(NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	if ns, ok := objectValue.Value.(*NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
	return NewNull()
}

func resolveNamespaceValue(vm *VM, value TinyValue) TinyValue {
	if ref, ok := value.Value.(NamespaceMemberRef); ok {
		return vm.resolveNamespaceMemberRef(ref.GlobalName)
	}

	if ref, ok := value.Value.(*NamespaceMemberRef); ok {
		return vm.resolveNamespaceMemberRef(ref.GlobalName)
	}

	return value
}

func (vm *VM) resolveNamespaceMemberRef(globalName string) TinyValue {
	slot, exists := vm.globalNames[globalName]
	if exists {
		value := vm.getGlobal(slot)
		if !isUnboundNamespaceExternal(value) {
			return value
		}
	}

	if dot := strings.LastIndex(globalName, "."); dot >= 0 {
		shortName := globalName[dot+1:]
		slot, exists = vm.globalNames[shortName]
		if exists {
			return vm.getGlobal(slot)
		}
	}

	vm.fatalError(ErrorName, "undefined namespace global: %s", globalName)
	return NewNull()
}

func isUnboundNamespaceExternal(value TinyValue) bool {
	if value.IsInt {
		return false
	}
	if value.Value == nil {
		return true
	}
	return isNullish(value)
}

func (vm *VM) multiplyByInt(value TinyValue, factor int) TinyValue {
	if value.IsInt {
		return NewInt(value.AsInt * factor)
	}

	switch v := value.Value.(type) {
	case float64:
		return NewNative(v * float64(factor))
	case float32:
		return NewNative(v * float32(factor))
	default:
		vm.runtimeError(ErrorType, "cannot multiply %s and number", TypeName(value))
		return TinyValue{}
	}
}

type WasmObjectValue struct {
	Address float64
	VM      *VM
}

func (o WasmObjectValue) TinyTypeName() string {
	return "object"
}

type WasmArrayValue struct {
	Address float64
	VM      *VM
}

func (a WasmArrayValue) TinyTypeName() string {
	return "array"
}

func (vm *VM) wasmTaggedValueToTinyValue(tag float64, val float64, depth int) TinyValue {
	if depth > 64 {
		return NewNull()
	}

	switch tag {
	case 0.0:
		return NewNull()
	case 1.0:
		return ToValue(val)
	case 2.0:
		return NewNative(val != 0.0)
	case 4.0:
		return NewNative(WasmObjectValue{Address: val, VM: vm})
	case 5.0:
		arr := WasmArrayValue{Address: val, VM: vm}
		if native, ok := vm.wasmArrayToArrayValueDepth(arr, depth+1); ok {
			return NewNative(native)
		}
		return NewNative(arr)
	case 6.0:
		strVal, ok := vm.readWasmStringMaybe(uint32(val))
		if ok {
			return NewNative(strVal)
		}
		return NewNull()
	default:
		return ToValue(val)
	}
}

func (vm *VM) wasmObjectToObjectValue(obj WasmObjectValue) (ObjectValue, bool) {
	return vm.wasmObjectToObjectValueDepth(obj, 0)
}

func (vm *VM) wasmObjectToObjectValueDepth(obj WasmObjectValue, depth int) (ObjectValue, bool) {
	source := vm
	if source == nil {
		source = obj.VM
	}
	if source == nil || source.jitModule == nil || depth > 64 {
		return nil, false
	}

	base := uint32(obj.Address)
	if tag, ok := source.readWasmFloatMaybe(base); ok && tag != 4.0 {
		return nil, false
	}

	shapeIDF, ok := source.readWasmFloatMaybe(base + 8)
	if !ok {
		return nil, false
	}
	shapeID := int(shapeIDF)
	if shapeID < 0 || shapeID >= len(source.objectShapes) {
		return nil, false
	}

	out := ObjectValue{}
	for _, name := range source.objectShapes[shapeID] {
		offset := source.getPropertyOffset(name)
		tag, okTag := source.readWasmFloatMaybe(base + offset)
		val, okVal := source.readWasmFloatMaybe(base + offset + 8)
		if !okTag || !okVal {
			out[name] = NewNull()
			continue
		}
		if tag == 4.0 && depth < 64 {
			child := WasmObjectValue{Address: val, VM: source}
			if childObj, ok := source.wasmObjectToObjectValueDepth(child, depth+1); ok {
				out[name] = NewNative(childObj)
				continue
			}
		}
		out[name] = source.wasmTaggedValueToTinyValue(tag, val, depth+1)
	}

	return out, true
}

func (vm *VM) wasmArrayToArrayValue(arr WasmArrayValue) (*ArrayValue, bool) {
	return vm.wasmArrayToArrayValueDepth(arr, 0)
}

func (vm *VM) wasmArrayToArrayValueDepth(arr WasmArrayValue, depth int) (*ArrayValue, bool) {
	source := vm
	if source == nil {
		source = arr.VM
	}
	if source == nil || source.jitModule == nil || depth > 64 {
		return nil, false
	}

	base := uint32(arr.Address)
	if tag, ok := source.readWasmFloatMaybe(base); ok && tag != 5.0 {
		return nil, false
	}

	lengthF, ok := source.readWasmFloatMaybe(base + 8)
	if !ok || lengthF < 0 {
		return nil, false
	}
	length := int(lengthF)
	if length == 0 {
		return &ArrayValue{Elements: []TinyValue{}}, true
	}

	elemPtrF, ok := source.readWasmFloatMaybe(base + 16)
	if !ok || elemPtrF == 0 {
		return nil, false
	}
	elemPtr := uint32(elemPtrF)

	out := &ArrayValue{Elements: make([]TinyValue, length)}
	for i := 0; i < length; i++ {
		addr := elemPtr + uint32(i*16)
		tag, okTag := source.readWasmFloatMaybe(addr)
		val, okVal := source.readWasmFloatMaybe(addr + 8)
		if !okTag || !okVal {
			out.Elements[i] = NewNull()
			continue
		}
		out.Elements[i] = source.wasmTaggedValueToTinyValue(tag, val, depth+1)
	}

	return out, true
}

func (vm *VM) readWasmFloatMaybe(addr uint32) (float64, bool) {
	if vm == nil || vm.jitModule == nil {
		return 0, false
	}
	bytes, ok := vm.jitModule.Memory().Read(addr, 8)
	if !ok {
		return 0, false
	}
	bits := binary.LittleEndian.Uint64(bytes)
	return math.Float64frombits(bits), true
}

func (vm *VM) readWasmStringMaybe(addr uint32) (string, bool) {
	if vm == nil || vm.jitModule == nil {
		return "", false
	}
	lenF, ok := vm.readWasmFloatMaybe(addr + 8)
	if !ok {
		return "", false
	}
	length := uint32(lenF)
	if length == 0 {
		return "", true
	}
	bytes, ok := vm.jitModule.Memory().Read(addr+16, length)
	if !ok {
		return "", false
	}
	return string(bytes), true
}

func (vm *VM) writeWasmString(s string) float64 {
	mod := vm.jitModule
	if mod == nil {
		vm.fatalError(ErrorInternal, "JIT module not initialized")
	}
	bytes := []byte(s)
	size := uint32(16 + len(bytes))
	size = (size + 7) &^ 7 // Align size to 8-byte boundary

	const bitsetRange = 128 * 1024 * 1024
	const bitsetSize = bitsetRange / 64

	var addr uint32
	heapTopGlobal := mod.ExportedGlobal("__heap_top")
	if heapTopGlobal != nil {
		addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
	} else {
		vm.fatalError(ErrorInternal, "JIT heap top global not found")
	}

	// Mark allocator bitset
	bitIdx := addr / 8
	byteIdx := bitIdx / 8
	bitOffset := bitIdx % 8
	if byteIdx < bitsetSize {
		buf, okBuf := mod.Memory().Read(byteIdx, 1)
		if okBuf {
			buf[0] |= (1 << bitOffset)
			mod.Memory().Write(byteIdx, buf)
		}
	}

	newTop := addr + size
	currentPages := mod.Memory().Size() / 65536
	newPagesNeeded := (newTop + 65535) / 65536
	if newPagesNeeded > currentPages {
		mod.Memory().Grow(newPagesNeeded - currentPages)
	}
	if heapTopGlobal != nil {
		if mg, ok := heapTopGlobal.(api.MutableGlobal); ok {
			mg.Set(api.EncodeF64(float64(newTop)))
		}
	}

	// Write Tag 6.0 and Length
	binary.LittleEndian.PutUint64(vm.stringScratch[0:8], math.Float64bits(6.0))
	mod.Memory().Write(addr, vm.stringScratch[0:8])

	binary.LittleEndian.PutUint64(vm.stringScratch[8:16], math.Float64bits(float64(len(bytes))))
	mod.Memory().Write(addr+8, vm.stringScratch[8:16])

	// Write string bytes
	mod.Memory().Write(addr+16, bytes)

	return float64(addr)
}

func (a WasmArrayValue) String() string {
	if a.VM == nil {
		return "[]"
	}

	lengthF, ok := a.VM.readWasmFloatMaybe(uint32(a.Address) + 8)
	if !ok {
		return "[]"
	}
	elemPtrF, ok := a.VM.readWasmFloatMaybe(uint32(a.Address) + 16)
	if !ok {
		return "[]"
	}

	length := int(lengthF)
	elemPtr := uint32(elemPtrF)
	var parts []string

	for i := 0; i < length; i++ {
		addr := elemPtr + uint32(i*16)
		tag, ok1 := a.VM.readWasmFloatMaybe(addr)
		val, ok2 := a.VM.readWasmFloatMaybe(addr + 8)
		if !ok1 || !ok2 {
			parts = append(parts, "null")
			continue
		}

		switch tag {
		case 1.0:
			parts = append(parts, fmt.Sprintf("%g", val))
		case 2.0:
			if val != 0.0 {
				parts = append(parts, "true")
			} else {
				parts = append(parts, "false")
			}
		case 4.0:
			parts = append(parts, WasmObjectValue{Address: val, VM: a.VM}.String())
		case 5.0:
			parts = append(parts, WasmArrayValue{Address: val, VM: a.VM}.String())
		case 6.0:
			strVal, ok := a.VM.readWasmStringMaybe(uint32(val))
			if ok {
				parts = append(parts, "\""+strVal+"\"")
			} else {
				parts = append(parts, "null")
			}
		default:
			parts = append(parts, "null")
		}
	}

	return "[" + strings.Join(parts, ", ") + "]"
}

func (vm *VM) getPropertyOffset(name string) uint32 {
	if offset, ok := vm.propertyOffsets[name]; ok {
		return offset
	}
	offset := vm.nextPropertyOffset
	vm.propertyOffsets[name] = offset
	vm.nextPropertyOffset += 16
	return offset
}

func (vm *VM) ReadWasmFloat(addr uint32) float64 {
	mod := vm.jitModule
	if mod == nil {
		vm.fatalError(ErrorInternal, "JIT module not initialized")
	}
	bytes, ok := mod.Memory().Read(addr, 8)
	if !ok {
		vm.fatalError(ErrorRuntime, "Wasm memory read out of bounds: 0x%x", addr)
	}
	bits := binary.LittleEndian.Uint64(bytes)
	return math.Float64frombits(bits)
}

func (vm *VM) WriteWasmFloat(addr uint32, val float64) {
	mod := vm.jitModule
	if mod == nil {
		vm.fatalError(ErrorInternal, "JIT module not initialized")
	}
	bits := math.Float64bits(val)
	binary.LittleEndian.PutUint64(vm.writeScratch[:], bits)
	ok := mod.Memory().Write(addr, vm.writeScratch[:])
	if !ok {
		vm.fatalError(ErrorRuntime, "Wasm memory write out of bounds: 0x%x", addr)
	}
}

func (vm *VM) WriteWasmTaggedValue(addr uint32, value TinyValue) {
	tag := 1.0
	val := 0.0

	if value.IsInt {
		val = float64(value.AsInt)
	} else {
		switch v := value.Value.(type) {
		case float64:
			val = v
		case float32:
			val = float64(v)
		case int:
			val = float64(v)
		case int64:
			val = float64(v)
		case bool:
			tag = 2.0
			if v {
				val = 1.0
			}
		case string:
			tag = 6.0
			val = vm.writeWasmString(v)
		case WasmObjectValue:
			tag = 4.0
			val = v.Address
		case *WasmObjectValue:
			if v == nil {
				tag = 0.0
			} else {
				tag = 4.0
				val = v.Address
			}
		case ObjectValue:
			tag = 4.0
			val = vm.allocateJitObject(vm.jitModule, v)
		case *ObjectValue:
			if v == nil {
				tag = 0.0
			} else {
				tag = 4.0
				val = vm.allocateJitObject(vm.jitModule, *v)
			}
		case WasmArrayValue:
			tag = 5.0
			val = v.Address
		case *WasmArrayValue:
			if v == nil {
				tag = 0.0
			} else {
				tag = 5.0
				val = v.Address
			}
		case *ArrayValue:
			if v == nil {
				tag = 0.0
			} else {
				tag = 5.0
				val = vm.allocateJitArray(vm.jitModule, v)
			}
		case ArrayValue:
			arrCopy := v
			tag = 5.0
			val = vm.allocateJitArrayFresh(vm.jitModule, &arrCopy)
		case NullValue:
			tag = 0.0
		case *NullValue:
			tag = 0.0
		case nil:
			tag = 0.0
		default:
			vm.fatalError(ErrorType, "cannot store %s in JIT object", TypeName(value))
		}
	}

	vm.WriteWasmFloat(addr, tag)
	vm.WriteWasmFloat(addr+8, val)
}

func (o WasmObjectValue) String() string {
	if o.VM == nil {
		return "{}"
	}

	shapeIDF, ok := o.VM.readWasmFloatMaybe(uint32(o.Address) + 8)
	if !ok {
		return "{}"
	}

	shapeID := int(shapeIDF)
	if shapeID < 0 || shapeID >= len(o.VM.objectShapes) {
		return "{}"
	}

	names := o.VM.objectShapes[shapeID]
	var parts []string

	for _, name := range names {
		offset := o.VM.getPropertyOffset(name)
		addr := uint32(o.Address) + offset

		tag, ok1 := o.VM.readWasmFloatMaybe(addr)
		val, ok2 := o.VM.readWasmFloatMaybe(addr + 8)
		if !ok1 || !ok2 {
			continue
		}

		switch tag {
		case 1.0:
			parts = append(parts, fmt.Sprintf("%s: %g", name, val))
		case 2.0:
			if val != 0.0 {
				parts = append(parts, fmt.Sprintf("%s: true", name))
			} else {
				parts = append(parts, fmt.Sprintf("%s: false", name))
			}
		case 4.0:
			parts = append(parts, fmt.Sprintf("%s: %s", name, WasmObjectValue{Address: val, VM: o.VM}.String()))
		case 5.0:
			parts = append(parts, fmt.Sprintf("%s: %s", name, WasmArrayValue{Address: val, VM: o.VM}.String()))
		case 6.0:
			strVal, ok := o.VM.readWasmStringMaybe(uint32(val))
			if ok {
				parts = append(parts, fmt.Sprintf("%s: %s", name, strVal))
			} else {
				parts = append(parts, fmt.Sprintf("%s: null", name))
			}
		}
	}

	return fmt.Sprintf("{%s}", strings.Join(parts, ", "))
}

func asFloat64T(val TinyValue) (float64, bool) {
	if val.IsInt {
		return float64(val.AsInt), true
	}
	switch v := val.Value.(type) {
	case float64:
		return v, true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	}
	return 0, false
}

func fastIndexInt(value TinyValue) (int, bool) {
	if value.IsInt {
		return value.AsInt, true
	}
	switch v := value.Value.(type) {
	case int:
		return v, true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	default:
		return 0, false
	}
}

func fastNumericValue(value TinyValue) (float64, bool) {
	if value.IsInt {
		return float64(value.AsInt), true
	}
	switch v := value.Value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint64:
		return float64(v), true
	default:
		return 0, false
	}
}

func constToTinyValue(value any) TinyValue {
	if tv, ok := value.(TinyValue); ok {
		return tv
	}
	if value == nil {
		return NewNull()
	}
	return ToValue(value)
}

func (vm *VM) applyBinaryOp(left TinyValue, right TinyValue, op OpCode) TinyValue {
	if l, ok := fastNumericValue(left); ok {
		if r, ok := fastNumericValue(right); ok {
			switch op {
			case OP_ADD:
				return NewNative(l + r)
			case OP_SUB:
				return NewNative(l - r)
			case OP_MUL:
				return NewNative(l * r)
			case OP_DIV:
				if r == 0 {
					vm.runtimeError(ErrorRuntime, "cannot divide by zero")
				}
				return NewNative(l / r)
			case OP_MOD:
				if r == 0 {
					vm.runtimeError(ErrorRuntime, "cannot modulo by zero")
				}
				return NewNative(math.Mod(l, r))
			}
		}
	}

	switch op {
	case OP_ADD:
		return vm.addValues(left, right)
	case OP_SUB:
		return vm.subValues(left, right)
	case OP_MUL:
		return vm.mulValues(left, right)
	case OP_DIV:
		return vm.divValues(left, right)
	case OP_MOD:
		return vm.modValues(left, right)
	case OP_AND_BIT, OP_OR_BIT, OP_XOR, OP_LSHIFT, OP_RSHIFT:
		return vm.bitwiseValues(left, right, op)
	default:
		vm.fatalError(ErrorInternal, "unsupported binary op in superinstruction: %s", op.String())
		return TinyValue{}
	}
}

func (vm *VM) bitwiseInt(value TinyValue) int {
	if value.IsInt {
		return value.AsInt
	}

	switch v := value.Value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case uint64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	default:
		vm.fatalError(ErrorType, "expected number for bitwise operation, got %s", TypeName(value))
		return 0
	}
}

func (vm *VM) bitwiseValues(left TinyValue, right TinyValue, op OpCode) TinyValue {
	l := vm.bitwiseInt(left)
	r := vm.bitwiseInt(right)

	switch op {
	case OP_AND_BIT:
		return NewInt(l & r)
	case OP_OR_BIT:
		return NewInt(l | r)
	case OP_XOR:
		return NewInt(l ^ r)
	case OP_LSHIFT:
		if r < 0 {
			vm.runtimeError(ErrorRuntime, "cannot shift by negative count")
			return TinyValue{}
		}
		return NewInt(l << r)
	case OP_RSHIFT:
		if r < 0 {
			vm.runtimeError(ErrorRuntime, "cannot shift by negative count")
			return TinyValue{}
		}
		return NewInt(l >> r)
	default:
		vm.fatalError(ErrorInternal, "unsupported bitwise op: %s", op.String())
		return TinyValue{}
	}
}

func (vm *VM) addValues(left TinyValue, right TinyValue) TinyValue {
	toConcatString := func(v TinyValue) (string, bool) {
		if v.IsInt {
			return intToString(v.AsInt), true
		}

		switch x := v.Value.(type) {
		case string:
			return x, true
		case bool:
			if x {
				return "true", true
			}
			return "false", true
		case int:
			return intToString(x), true
		case int64:
			return int64ToString(x), true
		case uint64:
			return uintToString(x), true
		case float32:
			return FloatToString(float64(x)), true
		case float64:
			return FloatToString(x), true
		}

		return "", false
	}

	leftIsString := false
	rightIsString := false

	if !left.IsInt {
		_, leftIsString = left.Value.(string)
	}

	if !right.IsInt {
		_, rightIsString = right.Value.(string)
	}

	if leftIsString || rightIsString {
		l, okL := toConcatString(left)
		r, okR := toConcatString(right)

		if okL && okR {
			return NewNative(l + r)
		}

		vm.runtimeError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		return TinyValue{}
	}

	lNum, lOK := asFloat64T(left)
	rNum, rOK := asFloat64T(right)

	if lOK && rOK {
		return NewNative(lNum + rNum)
	}

	vm.runtimeError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
	return TinyValue{}
}

func (vm *VM) subValues(left TinyValue, right TinyValue) TinyValue {
	lNum, lOK := asFloat64T(left)
	rNum, rOK := asFloat64T(right)
	if lOK && rOK {
		return NewNative(lNum - rNum)
	}
	vm.runtimeError(ErrorType, "cannot subtract %s and %s", TypeName(left), TypeName(right))
	return TinyValue{}
}

func (vm *VM) mulValues(left TinyValue, right TinyValue) TinyValue {
	lNum, lOK := asFloat64T(left)
	rNum, rOK := asFloat64T(right)
	if lOK && rOK {
		return NewNative(lNum * rNum)
	}
	vm.runtimeError(ErrorType, "cannot multiply %s and %s", TypeName(left), TypeName(right))
	return TinyValue{}
}

func (vm *VM) divValues(left TinyValue, right TinyValue) TinyValue {
	lNum, lOK := asFloat64T(left)
	rNum, rOK := asFloat64T(right)
	if lOK && rOK {
		if rNum == 0 {
			vm.runtimeError(ErrorRuntime, "cannot divide by zero")
		}
		return NewNative(lNum / rNum)
	}
	vm.runtimeError(ErrorType, "cannot divide %s and %s", TypeName(left), TypeName(right))
	return TinyValue{}
}

func (vm *VM) modValues(left TinyValue, right TinyValue) TinyValue {
	lNum, lOK := asFloat64T(left)
	rNum, rOK := asFloat64T(right)
	if lOK && rOK {
		if rNum == 0 {
			vm.runtimeError(ErrorRuntime, "cannot modulo by zero")
		}
		return NewNative(math.Mod(lNum, rNum))
	}
	vm.runtimeError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
	return TinyValue{}
}

func objectShapeKey(names []string) string {
	return strings.Join(names, "\x00")
}

func (vm *VM) getObjectShapeID(names []string) uint32 {
	key := objectShapeKey(names)
	if id, ok := vm.objectShapeIDs[key]; ok {
		return id
	}

	id := uint32(len(vm.objectShapes))
	copied := make([]string, len(names))
	copy(copied, names)

	vm.objectShapes = append(vm.objectShapes, copied)
	vm.objectShapeIDs[key] = id
	return id
}

func (vm *VM) getGlobalUnlocked(slot int) TinyValue {
	if slot < 0 || slot >= len(*vm.globals) {
		return NewNull()
	}
	return (*vm.globals)[slot]
}

func (vm *VM) setGlobalUnlocked(slot int, value TinyValue) {
	if slot >= len(*vm.globals) {
		newSize := slot + 1
		if newSize < len(*vm.globals)*2 {
			newSize = len(*vm.globals) * 2
		}
		newGlobals := make([]TinyValue, newSize)
		copy(newGlobals, *vm.globals)
		*vm.globals = newGlobals
	}
	(*vm.globals)[slot] = value
	atomic.AddUint64(&vm.globalVersion, 1)
	vm.clearJitMemoCaches()
}

func (vm *VM) getGlobal(slot int) TinyValue {
	if vm.isolated {
		if slot < 0 || slot >= len(*vm.globals) {
			return NewNull()
		}
		return (*vm.globals)[slot]
	}
	vm.mu.RLock()
	if slot < 0 || slot >= len(*vm.globals) {
		vm.mu.RUnlock()
		return NewNull()
	}
	value := (*vm.globals)[slot]
	vm.mu.RUnlock()
	return value
}

func (vm *VM) setGlobal(slot int, value TinyValue) {
	if vm.isolated {
		vm.setGlobalUnlocked(slot, value)
		return
	}
	vm.mu.Lock()
	defer vm.mu.Unlock()

	vm.setGlobalUnlocked(slot, value)
}

func (vm *VM) currentInlineCacheKey() uint64 {
	if len(vm.frames) > 0 {
		frame := vm.frames[len(vm.frames)-1]
		ip := frame.ip - 1
		if ip < 0 {
			ip = 0
		}
		return uint64(uint32(frame.function.ID))<<32 | uint64(uint32(ip))
	}
	ip := vm.ip - 1
	if ip < 0 {
		ip = 0
	}
	return uint64(^uint32(0))<<32 | uint64(uint32(ip))
}

func (vm *VM) getCachedGlobalPair(globalSlotA int, globalSlotB int) (TinyValue, TinyValue) {
	version := atomic.LoadUint64(&vm.globalVersion)
	key := vm.currentInlineCacheKey()

	if vm.globalPairIC != nil {
		if cache, ok := vm.globalPairIC[key]; ok && cache.Version == version && cache.GlobalSlotA == globalSlotA && cache.GlobalSlotB == globalSlotB {
			return cache.ValueA, cache.ValueB
		}
	}

	vm.mu.RLock()
	valueA := vm.getGlobalUnlocked(globalSlotA)
	valueB := vm.getGlobalUnlocked(globalSlotB)
	version = atomic.LoadUint64(&vm.globalVersion)
	vm.mu.RUnlock()

	if vm.globalPairIC == nil {
		vm.globalPairIC = make(map[uint64]globalPairInlineCache, 32)
	}
	vm.globalPairIC[key] = globalPairInlineCache{
		Version:     version,
		GlobalSlotA: globalSlotA,
		GlobalSlotB: globalSlotB,
		ValueA:      valueA,
		ValueB:      valueB,
	}

	return valueA, valueB
}

func (vm *VM) canAccessField(inst *InstanceValue, field string) bool {
	if inst == nil {
		return true
	}
	if _, fieldIsPrivate := inst.PrivateFields[field]; fieldIsPrivate {
		return vm.currentMethodClass() == inst.ClassName
	}

	return true
}

func (vm *VM) canAccessMethod(inst *InstanceValue, method string) bool {
	if inst == nil {
		return true
	}
	if _, methodIsPrivate := inst.PrivateMethods[method]; methodIsPrivate {
		return vm.currentMethodClass() == inst.ClassName
	}

	return true
}

func (vm *VM) getObjectLocalPropertyFast(frame *Frame, objectSlot int, name string, op string) TinyValue {
	objectValue := frameLocalValue(frame, objectSlot, op)

	if inst, ok := instanceValue(objectValue); ok {
		if !vm.canAccessField(inst, name) {
			vm.fatalError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := inst.Fields[name]
		if !exists {
			return NewNull()
		}
		return value
	}

	if object, ok := vm.valueAsObjectForRead(objectValue); ok {
		value, exists := object[name]
		if !exists {
			return NewNull()
		}
		return value
	}

	return vm.getProperty(objectValue, name, false)
}

func (vm *VM) setObjectLocalPropertyFast(frame *Frame, objectSlot int, name string, value TinyValue, op string) {
	objectValue := frameLocalValue(frame, objectSlot, op)

	object, ok := vm.valueAsObjectForWrite(objectValue)
	if !ok {
		vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
		return
	}

	if object.isInstance() {
		inst := object.inst
		if _, isConstant := inst.ConstFields[name]; isConstant {
			vm.fatalError(ErrorRuntime, "cannot assign to constant field: %s", name)
		}
		if !vm.canAccessField(inst, name) {
			vm.fatalError(ErrorRuntime, "cannot assign private field: %s", name)
		}
	}

	object.set(name, value)
}

func (vm *VM) getProperty(objectValue TinyValue, name string, safe bool) TinyValue {
	if obj, ok := objectValue.Value.(WasmObjectValue); ok {
		offset := vm.getPropertyOffset(name)
		addr := uint32(obj.Address) + offset

		tag := vm.ReadWasmFloat(addr)
		val := vm.ReadWasmFloat(addr + 8)

		switch tag {
		case 1.0:
			return NewNative(val)
		case 2.0:
			return NewNative(val != 0.0)
		case 4.0:
			return NewNative(WasmObjectValue{Address: val, VM: vm})
		case 5.0:
			return NewNative(WasmArrayValue{Address: val, VM: vm})
		case 6.0:
			strVal, ok := vm.readWasmStringMaybe(uint32(val))
			if ok {
				return NewNative(strVal)
			}
			return NewNull()
		case 0.0:
			return NewNull()
		}
	}

	if safe && isNullish(objectValue) {
		return NewNull()
	}

	if module, ok := objectValue.Value.(*StandardModuleValue); ok {
		if value, exists := getStdModuleProperty(module.Name, name); exists {
			return value
		}
		if safe {
			return NewNull()
		}
		vm.nameError("standard module %s has no property: %s", module.Name, name)
	}

	if value, ok := errorPropertyValue(objectValue.Value, name); ok {
		return value
	}

	if ns, ok := objectValue.Value.(NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			if safe {
				return NewNull()
			}
			vm.nameError("namespace %s has no member: %s", ns.Name, name)
		}

		return resolveNamespaceValue(vm, value)
	}

	if ns, ok := objectValue.Value.(*NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			if safe {
				return NewNull()
			}
			vm.nameError("namespace %s has no member: %s", ns.Name, name)
		}

		return resolveNamespaceValue(vm, value)
	}

	if inst, ok := instanceValue(objectValue); ok {
		if !vm.canAccessField(inst, name) {
			vm.fatalError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := inst.Fields[name]
		if !exists {
			return NewNull()
		}

		return value
	}

	object, ok := vm.valueAsObjectForRead(objectValue)
	if !ok {
		if safe {
			return NewNull()
		}
		vm.typeError("expected object, got %s", TypeName(objectValue))
	}

	value, exists := object[name]
	if !exists {
		return NewNull()
	}

	return value
}

func errorPropertyValue(value any, name string) (TinyValue, bool) {
	var err ErrorValue
	switch v := value.(type) {
	case ErrorValue:
		err = v
	case *ErrorValue:
		if v == nil {
			return TinyValue{}, false
		}
		err = *v
	default:
		return TinyValue{}, false
	}

	switch name {
	case "kind", "name":
		return NewNative(err.Kind), true
	case "message":
		return NewNative(err.Message), true
	default:
		return TinyValue{}, false
	}
}

func (vm *VM) callClassWithArgs(class Class, args []TinyValue) {
	instance := &InstanceValue{
		ClassName:      class.Name,
		Fields:         ObjectValue{},
		ConstFields:    map[string]bool{},
		PrivateFields:  map[string]bool{},
		PrivateMethods: map[string]bool{},
	}

	for methodName, functionName := range class.Methods {
		instance.Fields[methodName] = NewNative(FunctionValue{
			Name: functionName,
		})

		if class.PrivateMethods[methodName] {
			instance.PrivateMethods[methodName] = true
		}
	}

	for _, field := range class.Fields {
		instance.Fields[field.Name] = cloneValue(field.Value)
		if field.Constant {
			instance.ConstFields[field.Name] = true
		}

		if field.Private {
			instance.PrivateFields[field.Name] = true
		}
	}

	if initName, exists := class.Methods["init"]; exists {
		fn, ok := vm.functions[initName]
		if !ok {
			vm.fatalError(ErrorName, "undefined init function: %s", initName)
		}

		paramOffset := 1
		expected := len(fn.Params) - paramOffset
		isVariadic := expected > 0 && fn.Params[len(fn.Params)-1].Variadic

		if isVariadic {
			minArgs := expected - 1
			if len(args) < minArgs {
				vm.runtimeError(
					ErrorRuntime,
					"class %s constructor expects at least %d arguments, got %d",
					class.Name,
					minArgs,
					len(args),
				)
			}
		} else if fn.HasDefaults {
			args = vm.applyDefaultArgs(fn, args, paramOffset, "class "+class.Name+" constructor")
		} else if len(args) != expected {
			vm.runtimeError(
				ErrorRuntime,
				"class %s constructor expects %d arguments, got %d",
				class.Name,
				expected,
				len(args),
			)
		}

		frameDepthBefore := len(vm.frames)

		frame := vm.getFrame(fn)

		frame.methodClass = class.Name

		setCellValue(frame.locals[0], NewNative(instance))
		frame.locals[0].Constant = true

		if isVariadic {
			fixedCount := expected - 1

			for i := range fixedCount {
				paramIndex := paramOffset + i
				param := fn.Params[paramIndex]
				arg := args[i]

				vm.checkCallableArgType(fn, "method", "init", "parameter", param, arg)
				setCellValue(frame.locals[paramIndex], arg)
				frame.locals[paramIndex].Constant = false
				frame.locals[paramIndex].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
			}

			restSlot := paramOffset + fixedCount
			restParam := fn.Params[restSlot]
			rest := &ArrayValue{
				Elements: make([]TinyValue, 0, len(args)-fixedCount),
			}

			for i := fixedCount; i < len(args); i++ {
				arg := args[i]

				vm.checkCallableArgType(fn, "method", "init", "rest parameter", restParam, arg)
				rest.Elements = append(rest.Elements, arg)
			}

			setCellValue(frame.locals[restSlot], NewNative(rest))
			frame.locals[restSlot].Constant = false
			frame.locals[restSlot].TypeHint = TypeHint{Name: "array"}
		} else {
			for i, arg := range args {
				paramIndex := paramOffset + i
				param := fn.Params[paramIndex]

				vm.checkCallableArgType(fn, "method", "init", "parameter", param, arg)
				setCellValue(frame.locals[paramIndex], arg)
				frame.locals[paramIndex].Constant = false
				frame.locals[paramIndex].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
			}
		}

		vm.pushFrame(frame)

		if vm.execute(frameDepthBefore) {
			vm.fatalError(ErrorRuntime, "program halted while running constructor")
		}

		if vm.top > 0 {
			vm.pop()
		}
	}

	vm.push(NewNative(instance))
}

func (vm *VM) callClassByName(name string, args []TinyValue) {
	class, exists := vm.classes[name]
	if !exists {
		vm.fatalError(ErrorName, "undefined class: %s", name)
	}

	vm.callClassWithArgs(class, args)
}

func (vm *VM) checkTypeHint(value TinyValue, hint TypeHint) (bool, string) {
	var globals []TinyValue
	if vm.globals != nil {
		globals = *vm.globals
	}
	ok, reason := CheckTypeHintWithGlobals(value, hint, vm.interfaces, globals, vm.globalNames)
	if ok {
		return true, ""
	}
	if vm.acceptsRuntimeClassAlias(value, hint) {
		return true, ""
	}
	return false, reason
}

func (vm *VM) acceptsRuntimeClassAlias(value TinyValue, hint TypeHint) bool {
	if vm == nil || vm.isolated {
		return false
	}

	inst, ok := instanceValue(value)
	if !ok || inst.ClassName == "" {
		return false
	}

	for _, typ := range hint.AllTypes() {
		if typ == "" || strings.Contains(typ, ":") {
			continue
		}
		if inst.ClassName == typ || strings.HasSuffix(inst.ClassName, "."+typ) {
			return true
		}
		if class, exists := vm.resolveRuntimeClassAlias(typ); exists {
			if inst.ClassName == class.Name || runtimeInstanceMatchesClassShape(inst, class) {
				return true
			}
		} else if strings.Contains(typ, ".") && classBaseName(inst.ClassName) == classBaseName(typ) {
			return true
		}
		if classBaseName(inst.ClassName) == classBaseName(typ) {
			if class, exists := vm.classes[typ]; exists && runtimeInstanceMatchesClassShape(inst, class) {
				return true
			}
		}
	}

	return false
}

func (vm *VM) resolveRuntimeClassAlias(typeName string) (Class, bool) {
	typeName = strings.TrimSpace(typeName)
	if class, ok := vm.classes[typeName]; ok {
		return class, true
	}
	if !strings.Contains(typeName, ".") || vm.globalNames == nil || vm.globals == nil {
		return Class{}, false
	}

	parts := strings.Split(typeName, ".")
	for i := len(parts) - 1; i > 0; i-- {
		nsName := strings.Join(parts[:i], ".")
		memberPath := parts[i:]
		slot, exists := vm.globalNames[nsName]
		if !exists || slot < 0 || slot >= len(*vm.globals) {
			continue
		}

		value := (*vm.globals)[slot]
		for _, memberName := range memberPath {
			ns, ok := namespaceValueFromTinyValue(value)
			if !ok {
				value = TinyValue{}
				break
			}
			member, exists := ns.Members[memberName]
			if !exists {
				value = TinyValue{}
				break
			}
			value = resolveNamespaceValue(vm, member)
		}
		if class, ok := classFromTinyValue(value); ok {
			return class, true
		}
	}

	return Class{}, false
}

func namespaceValueFromTinyValue(value TinyValue) (NamespaceValue, bool) {
	if value.IsInt {
		return NamespaceValue{}, false
	}
	switch ns := value.Value.(type) {
	case NamespaceValue:
		return ns, true
	case *NamespaceValue:
		if ns == nil {
			return NamespaceValue{}, false
		}
		return *ns, true
	default:
		return NamespaceValue{}, false
	}
}

func classFromTinyValue(value TinyValue) (Class, bool) {
	if value.IsInt {
		return Class{}, false
	}
	switch class := value.Value.(type) {
	case Class:
		return class, true
	case *Class:
		if class == nil {
			return Class{}, false
		}
		return *class, true
	default:
		return Class{}, false
	}
}

func classBaseName(name string) string {
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		return name[idx+1:]
	}
	return name
}

func runtimeInstanceMatchesClassShape(inst *InstanceValue, class Class) bool {
	if inst == nil {
		return false
	}

	for _, field := range class.Fields {
		if _, exists := inst.Fields[field.Name]; !exists {
			return false
		}
	}

	for methodName := range class.Methods {
		if _, exists := inst.Fields[methodName]; !exists {
			return false
		}
	}

	return true
}

func (vm *VM) genericTypeParamsForFunction(fn Function) []string {
	params := []string{}

	ownerClass := methodOwnerClass(fn.Name)
	if ownerClass != "" {
		if class, exists := vm.classes[ownerClass]; exists {
			params = append(params, class.TypeParameters...)
		}
	}

	params = append(params, fn.TypeParameters...)
	return params
}

func runtimeTypeHintFromName(name string) TypeHint {
	name = strings.TrimSpace(name)
	if name == "" {
		return TypeHint{}
	}

	parts := []string{name}
	if strings.Contains(name, "|") {
		parts = strings.Split(name, "|")
		for i, part := range parts {
			parts[i] = strings.TrimSpace(part)
		}
	}

	return TypeHint{
		Name:  name,
		Types: parts,
	}
}

func eraseGenericTypeHintForRuntime(hint TypeHint, genericParams []string) TypeHint {
	if hint.IsEmpty() || len(genericParams) == 0 {
		return hint
	}

	erasedName := eraseGenericTypeNameForRuntime(hint.Name, genericParams)
	return runtimeTypeHintFromName(erasedName)
}

func eraseGenericTypeNameForRuntime(name string, genericParams []string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return name
	}

	for _, param := range genericParams {
		if name == param {
			return "any"
		}
	}

	if strings.Contains(name, "|") {
		parts := strings.Split(name, "|")
		for i, part := range parts {
			parts[i] = eraseGenericTypeNameForRuntime(part, genericParams)
		}
		return strings.Join(parts, "|")
	}

	if strings.HasPrefix(name, "array:") {
		elementType := strings.TrimPrefix(name, "array:")
		return "array:" + eraseGenericTypeNameForRuntime(elementType, genericParams)
	}

	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")
		for i, part := range parts {
			parts[i] = eraseGenericTypeNameForRuntime(part, genericParams)
		}
		return strings.Join(parts, ":")
	}

	return name
}

func (vm *VM) checkFunctionTypeHint(fn Function, value TinyValue, hint TypeHint) (bool, string) {
	hint = eraseGenericTypeHintForRuntime(hint, vm.genericTypeParamsForFunction(fn))
	return vm.checkTypeHint(value, hint)
}

func (vm *VM) checkCallableArgType(fn Function, callableType string, callableName string, parameterKind string, param Param, arg TinyValue) {
	if !fn.HasTypeHints || param.TypeHint.IsEmpty() {
		return
	}

	if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
		vm.fatalError(
			ErrorType,
			"%s %s %s %s expected %s, got %s%s",
			callableType,
			callableName,
			parameterKind,
			param.Name,
			param.TypeHint.String(),
			TypeName(arg),
			reason,
		)
	}
}

func (vm *VM) stackTrace() string {
	lines := []string{}

	for i := len(vm.nativeFrames) - 1; i >= 0; i-- {
		frame := vm.nativeFrames[i]

		location := ""
		if frame.File != "" && frame.Line > 0 {
			location = fmt.Sprintf(" (%s:%d", frame.File, frame.Line)

			if frame.Column > 0 {
				location += fmt.Sprintf(":%d", frame.Column)
			}

			location += ")"
		}

		lines = append(lines, "  at "+frame.Name+location)
	}

	if len(vm.frames) == 0 {
		location := ""

		ip := vm.ip - 1
		dbg := vm.mainDebugInfoAt(ip)

		if dbg.File != "" && dbg.Line > 0 {
			location = fmt.Sprintf(" (%s:%d", dbg.File, dbg.Line)

			if dbg.Column > 0 {
				location += fmt.Sprintf(":%d", dbg.Column)
			}

			location += ")"
		}

		lines = append(lines, "  at <main>"+location)
		return strings.Join(lines, "\n")
	}

	for i := len(vm.frames) - 1; i >= 0; i-- {
		frame := vm.frames[i]

		name := frame.function.Name
		if name == "" {
			name = "<anonymous>"
		}

		location := ""

		ip := frame.ip - 1
		dbg := frameDebugInfo(frame, ip)

		if dbg.File != "" && dbg.Line > 0 {
			location = fmt.Sprintf(" (%s:%d", dbg.File, dbg.Line)

			if dbg.Column > 0 {
				location += fmt.Sprintf(":%d", dbg.Column)
			}

			location += ")"
		}

		lines = append(lines, "  at "+name+location)

		if i == 0 && frame.hasMainCallSite {
			site := frame.mainCallSite

			mainLocation := ""
			if site.File != "" && site.Line > 0 {
				mainLocation = fmt.Sprintf(" (%s:%d", site.File, site.Line)

				if site.Column > 0 {
					mainLocation += fmt.Sprintf(":%d", site.Column)
				}

				mainLocation += ")"
			}

			lines = append(lines, "  at "+site.Name+mainLocation)
		}
	}

	return strings.Join(lines, "\n")
}

func (vm *VM) typeError(format string, args ...any) {
	vm.fatalError(ErrorType, format, args...)
}

func (vm *VM) nameError(format string, args ...any) {
	vm.fatalError(ErrorName, format, args...)
}

func (vm *VM) internalError(format string, args ...any) {
	vm.fatalError(ErrorInternal, format, args...)
}

func tinyErrorMessageHasStackTrace(message string) bool {
	return strings.Contains(message, "\nStack trace:\n") || strings.HasPrefix(message, "Stack trace:\n")
}

func tinyErrorMessageWithStackTrace(message string, trace string) string {
	if trace == "" || tinyErrorMessageHasStackTrace(message) {
		return message
	}
	return message + "\n\nStack trace:\n" + trace
}

func (vm *VM) fatalError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)
	message = tinyErrorMessageWithStackTrace(message, vm.stackTrace())

	panic(LangErrorType{
		Kind:    kind,
		Message: message,
	})
}

func (vm *VM) runtimeError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)

	errObj := ObjectValue{
		"kind":    NewNative(string(kind)),
		"message": NewNative(message),
	}

	vm.throwValue(NewNative(errObj))
}

func (vm *VM) isInstanceOf(value TinyValue, className string) bool {
	inst, ok := instanceValue(value)
	if !ok {
		return false
	}

	return vm.instanceIsOrEmbedsClass(inst, className)
}

func (vm *VM) instanceIsOrEmbedsClass(inst *InstanceValue, className string) bool {
	if inst == nil {
		return false
	}
	if inst.ClassName == className {
		return true
	}

	class, exists := vm.classes[inst.ClassName]
	if !exists {
		return false
	}

	for _, fieldName := range class.Embeds {
		fieldValue, exists := inst.Fields[fieldName]
		if !exists {
			continue
		}

		embeddedInstance, ok := instanceValue(fieldValue)
		if !ok {
			continue
		}

		if vm.instanceIsOrEmbedsClass(embeddedInstance, className) {
			return true
		}
	}

	return false
}

func (vm *VM) findEmbeddedMethodInstance(inst *InstanceValue, method string) (*InstanceValue, FunctionValue, bool) {
	if inst == nil {
		return nil, FunctionValue{}, false
	}

	class, ok := vm.classes[inst.ClassName]
	if !ok {
		return nil, FunctionValue{}, false
	}

	for _, fieldName := range class.Embeds {
		fieldValue, exists := inst.Fields[fieldName]
		if !exists {
			continue
		}

		embeddedInstance, ok := instanceValue(fieldValue)
		if !ok {
			continue
		}

		methodValue, exists := embeddedInstance.Fields[method]
		if !exists {
			if receiver, fn, ok := vm.findEmbeddedMethodInstance(embeddedInstance, method); ok {
				return receiver, fn, true
			}

			continue
		}

		fnValue, ok := methodValue.Value.(FunctionValue)
		if !ok {
			continue
		}

		return embeddedInstance, fnValue, true
	}

	return nil, FunctionValue{}, false
}

func (vm *VM) callFunctionDirectFromStack(fn Function, argCount int, callableName string) {
	expected := len(fn.Params)
	isVariadic := expected > 0 && fn.Params[expected-1].Variadic

	if !vm.jitDisabled && !isVariadic {
		jitName := fn.Name
		if strings.HasPrefix(callableName, "function __jit_region_") {
			jitName = strings.TrimPrefix(callableName, "function ")
		}
		jitCheckFn := fn
		jitCheckFn.Name = jitName
		jitUnsafe := jitHasLoopStringConcat(jitCheckFn, jitInstructionLoopMap(jitCheckFn.Instructions))
		if !jitUnsafe {
			vm.ensureJitReadyFor(jitName)
		}
		var jitFn *JitFunction
		if !jitUnsafe {
			jitFn = vm.jitFunctions[jitName]
		}

		if jitFn != nil && argCount == jitFn.paramCount && vm.stackArgsMatchJit(jitFn, argCount) {
			if jitCallDebugEnabled() {
				fmt.Fprintf(os.Stderr, "[JIT DEBUG] Calling JIT function %s directly!\n", fn.Name)
			}
			args := vm.popArgs(argCount)
			res, err := jitFn.Call(vm.wazeroCtx, args)
			if err == nil {
				vm.push(res)
				return
			}
			if _, ok := err.(JitExceptionThrownError); ok {
				vm.callFunctionDirectInterpreted(fn, args)
				return
			}

			if deopt, ok := jitDeoptFromError(err); ok {
				vm.resumeJitDeopt(fn, args, deopt)
				return
			}

			vm.callFunctionDirectInterpreted(fn, args)
			return
		}
	}

	if fn.HasDefaults && !isVariadic {
		args := vm.popArgs(argCount)
		args = vm.applyDefaultArgs(fn, args, 0, callableName)
		vm.callFunctionDirect(fn, args)
		return
	}

	if isVariadic {
		minArgs := expected - 1

		if argCount < minArgs {
			vm.runtimeError(
				ErrorRuntime,
				"%s expects at least %d arguments, got %d",
				callableName,
				minArgs,
				argCount,
			)
			return
		}
	} else if argCount != expected {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects %d arguments, got %d",
			callableName,
			expected,
			argCount,
		)
		return
	}

	if vm.top < argCount {
		vm.handleUnderflow()
		return
	}

	frame := vm.getFrame(fn)

	start := vm.top - argCount

	if isVariadic {
		fixedCount := expected - 1

		for i := 0; i < fixedCount; i++ {
			arg := vm.stack[start+i]
			param := fn.Params[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.locals[i].Constant = false
			frame.locals[i].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))

			vm.stack[start+i] = TinyValue{}
		}

		restParam := fn.Params[fixedCount]
		rest := &ArrayValue{
			Elements: make([]TinyValue, 0, argCount-fixedCount),
		}

		for i := fixedCount; i < argCount; i++ {
			arg := vm.stack[start+i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, restParam.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"function %s rest parameter %s expected %s, got %s%s",
						fn.Name,
						restParam.Name,
						restParam.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			rest.Elements = append(rest.Elements, arg)
			vm.stack[start+i] = TinyValue{}
		}

		setCellValue(frame.locals[fixedCount], NewNative(rest))
		frame.locals[fixedCount].Constant = false
		frame.locals[fixedCount].TypeHint = TypeHint{Name: "array"}

		vm.top = start
		vm.pushFrame(frame)
		return
	}

	if fn.HasTypeHints {
		for i := 0; i < argCount; i++ {
			arg := vm.stack[start+i]
			param := fn.Params[i]

			if !param.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.locals[i].Constant = false
			frame.locals[i].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))

			vm.stack[start+i] = TinyValue{}
		}
	} else {
		for i := 0; i < argCount; i++ {
			setCellValue(frame.locals[i], vm.stack[start+i])
			frame.locals[i].Constant = false

			vm.stack[start+i] = TinyValue{}
		}
	}

	vm.top = start
	vm.pushFrame(frame)
}

func isNullish(value TinyValue) bool {
	if value.IsInt {
		return false
	}
	switch value.Value.(type) {
	case NullValue:
		return true
	default:
		return false
	}
}

func (vm *VM) throwValue(value TinyValue) {
	errorObject := makeErrorObject(value)

	if len(vm.tryHandlers) == 0 {
		var rawMsg any
		if errorObject["message"].IsInt {
			rawMsg = errorObject["message"].AsInt
		} else {
			rawMsg = errorObject["message"].Value
		}

		var rawKind any
		if errorObject["kind"].IsInt {
			rawKind = errorObject["kind"].AsInt
		} else {
			rawKind = errorObject["kind"].Value
		}

		message := valueToString(NewNative(rawMsg))
		kind := valueToString(NewNative(rawKind))

		trace := vm.stackTrace()
		message = tinyErrorMessageWithStackTrace(message, trace)

		vm.runDefersAboveDepth(0)

		panic(LangErrorType{
			Kind:    ErrorKind(kind),
			Message: message,
		})
	}

	handler := vm.tryHandlers[len(vm.tryHandlers)-1]
	vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

	vm.runDefersAboveDepth(handler.FrameDepth)

	for len(vm.frames) > handler.FrameDepth {
		frame := vm.frames[len(vm.frames)-1]
		for _, m := range frame.lockedMutexes {
			m.Unlock()
		}
		vm.frames = vm.frames[:len(vm.frames)-1]
	}

	if handler.IsLocal {
		if handler.FrameDepth == 0 {
			vm.fatalError(ErrorInternal, "local catch handler has no frame")
		}

		frame := vm.frames[handler.FrameDepth-1]

		if handler.Slot < 0 || handler.Slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "catch local slot out of range")
		}

		setCellValue(frame.locals[handler.Slot], NewNative(errorObject))
		frame.locals[handler.Slot].Constant = false
	} else {
		vm.setGlobal(handler.Slot, NewNative(errorObject))
		vm.globalConstants[handler.Name] = false
	}

	if handler.FrameDepth == 0 {
		vm.ip = handler.CatchIP
	} else {
		vm.frames[handler.FrameDepth-1].ip = handler.CatchIP
	}
}

func makeErrorObject(value TinyValue) ObjectValue {
	var raw any
	if value.IsInt {
		raw = value.AsInt
	} else {
		raw = value.Value
	}

	switch err := raw.(type) {
	case ErrorValue:
		return ObjectValue{
			"kind":    NewNative(err.Kind),
			"message": NewNative(err.Message),
		}

	case *ErrorValue:
		return ObjectValue{
			"kind":    NewNative(err.Kind),
			"message": NewNative(err.Message),
		}

	case ObjectValue:
		return err

	case string:
		return ObjectValue{
			"kind":    NewNative("Error"),
			"message": NewNative(err),
		}

	default:
		return ObjectValue{
			"kind":    NewNative("Error"),
			"message": NewNative(valueToString(value)),
		}
	}
}

func (vm *VM) callFunctionValueWithArgs(fnValue FunctionValue, args []TinyValue) {
	if vm.observerStats != nil {
		vm.observerStats.FunctionCalled(fnValue.Name)
	}

	fn, ok := vm.functions[fnValue.Name]
	if ok {
		vm.ensureJitReadyFor(fn.Name)

		expected := len(fn.Params)
		isVariadic := expected > 0 && fn.Params[expected-1].Variadic
		if fn.HasDefaults && !isVariadic {
			args = vm.applyDefaultArgs(fn, args, 0, fn.Name)
		}

		jitFn := vm.jitFunctions[fn.Name]

		if !vm.jitDisabled && jitFn != nil && vm.argsMatchJit(jitFn, args) {
			if jitCallDebugEnabled() {
				fmt.Fprintf(os.Stderr, "[JIT DEBUG] Calling JIT function %s!\n", fn.Name)
			}
			res, err := jitFn.Call(vm.wazeroCtx, args)
			if err == nil {
				vm.push(res)
				return
			}
			if _, ok := err.(JitExceptionThrownError); ok {
				// Do not silently return from a function-value call without pushing a result.
				// Continue into the normal interpreter path below.
			} else if deopt, ok := jitDeoptFromError(err); ok {
				vm.resumeJitDeopt(fn, args, deopt)
				return
			}
		}
	}

	if vm.wasmModule != nil {
		sanitizedName := strings.ReplaceAll(fnValue.Name, ".", "_")
		fn := vm.wasmModule.ExportedFunction(sanitizedName)
		if fn != nil {
			returnType := "string"
			if v, ok := vm.functions[fnValue.Name]; ok {
				returnType = v.ReturnType.Name
			}
			vm.executeNativeWasmCall(fn, args, returnType)
			return
		}
	}

	fn, ok = vm.functions[fnValue.Name]
	if !ok {
		vm.fatalError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	expected := len(fn.Params)
	isVariadic := expected > 0 && fn.Params[expected-1].Variadic

	if isVariadic {
		minArgs := expected - 1

		if len(args) < minArgs {
			vm.runtimeError(
				ErrorRuntime,
				"function %s expects at least %d arguments, got %d",
				fn.Name,
				minArgs,
				len(args),
			)
		}
	} else {
		if fn.HasDefaults {
			if len(args) < expected {
				args = vm.applyDefaultArgs(fn, args, 0, fn.Name)
			}
		} else if len(args) != expected {
			vm.runtimeError(
				ErrorRuntime,
				"function %s expects %d arguments, got %d",
				fn.Name,
				expected,
				len(args),
			)
		}
	}

	frame := vm.getFrame(fn)

	// Restore the class context from the closure so private field access works
	// even when the closure is called from a spawned goroutine or outside the class.
	if frame.methodClass == "" && fnValue.MethodClass != "" {
		frame.methodClass = fnValue.MethodClass
	}

	if len(fnValue.Captures) > 0 {
		frame.hasEscapedLocals = true
	}

	for slot, cell := range fnValue.Captures {
		if slot < 0 || slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "capture slot out of range in function value: %d", slot)
		}

		frame.locals[slot] = cell
	}

	if isVariadic {
		fixedCount := expected - 1

		for i := range fixedCount {
			setCellValue(frame.locals[i], args[i])
			frame.locals[i].Constant = false
		}

		rest := &ArrayValue{
			Elements: make([]TinyValue, 0, len(args)-fixedCount),
		}

		for i := fixedCount; i < len(args); i++ {
			rest.Elements = append(rest.Elements, args[i])
		}

		setCellValue(frame.locals[fixedCount], NewNative(rest))
		frame.locals[fixedCount].Constant = false
	} else {
		for i, arg := range args {
			param := fn.Params[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.locals[i].Constant = false
			frame.locals[i].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
		}
	}

	vm.pushFrame(frame)
}

func (vm *VM) runFunctionToCompletion(fn Function, args []TinyValue) TinyValue {
	frameDepthBefore := len(vm.frames)
	stackDepthBefore := vm.top

	vm.callFunctionDirect(fn, args)

	// The call may complete immediately through JIT and push its result without
	// adding a frame. In that case, do not execute the outer program by accident.
	if len(vm.frames) == frameDepthBefore {
		if vm.top <= stackDepthBefore {
			return NewNull()
		}
		return vm.pop()
	}

	vm.execute(frameDepthBefore)

	if vm.top <= stackDepthBefore {
		return NewNull()
	}
	return vm.pop()
}

func (vm *VM) runFrameToCompletion(frame *Frame) TinyValue {
	vm.pushFrame(frame)

	targetDepth := len(vm.frames) - 1

	vm.execute(targetDepth)

	return vm.pop()
}

func (vm *VM) callFunctionDirectInterpreted(fn Function, args []TinyValue) {
	if fn.HasDefaults {
		args = vm.applyDefaultArgs(fn, args, 0, fn.Name)
	}

	frame := vm.getFrame(fn)

	for i, arg := range args {
		param := fn.Params[i]

		if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
			if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
				vm.fatalError(
					ErrorType,
					"function %s parameter %s expected %s, got %s%s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
					reason,
				)
			}
		}

		setCellValue(frame.locals[i], arg)
		frame.locals[i].Constant = false
		frame.locals[i].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
	}

	vm.pushFrame(frame)
}

func (vm *VM) callFunctionDirect(fn Function, args []TinyValue) {
	if vm.observerStats != nil {
		vm.observerStats.FunctionCalled(fn.Name)
	}

	expected := len(fn.Params)
	isVariadic := expected > 0 && fn.Params[expected-1].Variadic

	if !vm.jitDisabled && !isVariadic {
		vm.ensureJitReadyFor(fn.Name)
		jitFn := vm.jitFunctions[fn.Name]

		if jitFn != nil && vm.argsMatchJit(jitFn, args) {
			res, err := jitFn.Call(vm.wazeroCtx, args)
			if err == nil {
				vm.push(res)
				return
			}
			if _, ok := err.(JitExceptionThrownError); ok {
				vm.callFunctionDirectInterpreted(fn, args)
				return
			}
			if deopt, ok := jitDeoptFromError(err); ok {
				vm.resumeJitDeopt(fn, args, deopt)
				return
			}
		}
	}

	if fn.HasDefaults && !isVariadic {
		args = vm.applyDefaultArgs(fn, args, 0, fn.Name)
	}

	vm.callFunctionDirectInterpreted(fn, args)
}

func (vm *VM) callFunctionValue(fnValue FunctionValue, args []TinyValue) TinyValue {
	frameDepthBefore := len(vm.frames)
	stackDepthBefore := vm.top

	vm.callFunctionValueWithArgs(fnValue, args)

	if vm.execute(frameDepthBefore) {
		vm.fatalError(ErrorRuntime, "program halted while running function value")
	}

	if vm.top <= stackDepthBefore {
		return NewNull()
	}

	return vm.pop()
}

func (vm *VM) Run() {
	defer vm.Close()
	defer printJitCallDebugSummary()
	vm.execute(-1)
}

func (vm *VM) Close() {
	CleanupHotKeysForVM(vm)
	if vm.wazeroRuntime != nil {
		vm.wazeroRuntime.Close(vm.wazeroCtx)
	}
}

func (vm *VM) runDefersAboveDepth(targetDepth int) {
	for len(vm.deferHandlers) > 0 {
		handler := vm.deferHandlers[len(vm.deferHandlers)-1]

		if handler.FrameDepth > targetDepth {
			vm.deferHandlers = vm.deferHandlers[:len(vm.deferHandlers)-1]

			vm.callFunctionValue(handler.Function, nil)
		} else {
			break
		}
	}
}

func (vm *VM) ResetForRequest() {
	vm.clearJitMemoCaches()
	vm.top = 0
	vm.frames = vm.frames[:0]
	vm.tryHandlers = vm.tryHandlers[:0]
	vm.deferHandlers = vm.deferHandlers[:0]
	vm.nativeFrames = vm.nativeFrames[:0]
	vm.currentInstr = Instruction{}
	vm.stoppedCheckCounter = 0

	if vm.jitModule != nil {
		if vm.jitHeapTop > vm.jitInitialHeapTop {
			startByte := vm.jitInitialHeapTop / 64
			endByte := (vm.jitHeapTop + 63) / 64
			if endByte > startByte {
				length := endByte - startByte
				if length > uint32(len(jitZeroBuf)) {
					length = uint32(len(jitZeroBuf))
				}
				vm.jitModule.Memory().Write(startByte, jitZeroBuf[:length])
			}
		}
		vm.jitHeapTop = vm.jitInitialHeapTop
		if heapTopGlobal := vm.jitModule.ExportedGlobal("__heap_top"); heapTopGlobal != nil {
			if mg, ok := heapTopGlobal.(api.MutableGlobal); ok {
				mg.Set(api.EncodeF64(float64(vm.jitInitialHeapTop)))
			}
		}
	}
}

func (vm *VM) Stop() {
	if vm.stopped != nil {
		vm.stopped.Store(true)
	}
}

func (vm *VM) IsStopped() bool {
	return vm.stopped != nil && vm.stopped.Load()
}

func (vm *VM) ActiveExecutions() int64 {
	if vm.active == nil {
		return 0
	}
	return vm.active.Load()
}

func (vm *VM) WaitIdle(timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for vm.ActiveExecutions() > 0 {
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(time.Millisecond)
	}
	return true
}

func (vm *VM) execute(targetDepth int) bool {
	if vm.active != nil {
		vm.active.Add(1)
		defer vm.active.Add(-1)
	}

	var cfFrame *Frame
	var cfInstructions []Instruction
	var cfIP int
	var cfMethodCache []MethodCallCache

	loadState := func() {
		if len(vm.frames) == 0 {
			cfFrame = nil
			cfInstructions = vm.mainInstructions
			cfIP = vm.ip
			cfMethodCache = vm.mainMethodCache
		} else {
			cfFrame = vm.frames[len(vm.frames)-1]
			cfInstructions = cfFrame.instructions
			cfIP = cfFrame.ip
			cfMethodCache = cfFrame.methodCache
		}
	}

	saveState := func() {
		if cfFrame == nil {
			vm.ip = cfIP
		} else {
			cfFrame.ip = cfIP
		}
	}

	loadState()

	for {
		vm.stoppedCheckCounter++
		if vm.stoppedCheckCounter >= 1024 {
			vm.stoppedCheckCounter = 0
			if vm.IsStopped() {
				saveState()
				vm.runDefersAboveDepth(targetDepth)
				return len(vm.frames) == 0
			}
		}

		// Sync local state at the start of loop iteration
		newTopFrame := (*Frame)(nil)
		if len(vm.frames) > 0 {
			newTopFrame = vm.frames[len(vm.frames)-1]
		}

		if cfFrame != newTopFrame {
			if cfFrame == nil {
				vm.ip = cfIP
			} else {
				cfFrame.ip = cfIP
			}
			loadState()
		} else {
			if cfFrame == nil {
				if vm.ip != cfIP {
					cfIP = vm.ip
				}
			} else {
				if cfFrame.ip != cfIP {
					cfIP = cfFrame.ip
				}
			}
		}

		if len(vm.frames) <= targetDepth {
			saveState()
			break
		}

		if cfIP < 0 || cfIP >= len(cfInstructions) {
			saveState()
			vm.fatalError(ErrorInternal, "instruction pointer out of range: ip=%d len=%d", cfIP, len(cfInstructions))
		}

		instr := cfInstructions[cfIP]
		cfIP++

		if cfFrame != nil {
			cfFrame.ip = cfIP
		} else {
			vm.ip = cfIP
		}

		switch instr.Op {
		case OP_JSON_STRINGIFY:
			if vm.top < 1 {
				vm.handleUnderflow()
				break
			}

			value := vm.pop()
			vm.push(NewNative(stringifyTinyJSONFast(value)))

		case OP_JSON_PARSE:
			if vm.top < 1 {
				vm.handleUnderflow()
				break
			}

			value := vm.pop()

			var raw any
			if value.IsInt {
				raw = value.AsInt
			} else {
				raw = value.Value
			}

			str, ok := raw.(string)
			if !ok {
				vm.fatalError(ErrorType, "json.parse expected string, got %s", TypeName(value))
			}

			result, err := parseTinyJSONDirect(str)
			if err != nil {
				vm.runtimeError(ErrorRuntime, "invalid JSON: %v", err)
				vm.push(NewNull())
				break
			}

			vm.push(result)

		case OP_LOAD_WASM:
			wasmBytes := instr.Value.([]byte)

			vm.wazeroRuntime = wazero.NewRuntime(vm.wazeroCtx)

			wasi_snapshot_preview1.MustInstantiate(vm.wazeroCtx, vm.wazeroRuntime)

			var err error
			vm.wasmModule, err = vm.wazeroRuntime.InstantiateWithConfig(
				vm.wazeroCtx,
				wasmBytes,
				wazero.NewModuleConfig().WithStartFunctions("_initialize"),
			)
			if err != nil {
				vm.fatalError(ErrorRuntime, "failed to load WebAssembly module: %v", err)
			}

		case OP_NATIVE_CALL:
			info := instr.Value.(NativeCallInfo)

			args := vm.popArgs(info.ArgCount)

			fn := vm.wasmModule.ExportedFunction(info.Name)
			if fn == nil {
				vm.fatalError(ErrorName, "undefined native function: %s", info.Name)
			}

			vm.executeNativeWasmCall(fn, args, info.ReturnType)

		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)
			frame := vm.frames[len(vm.frames)-1]

			if frame.locals[info.DestSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			leftCell := frame.locals[info.SlotA]
			rightCell := frame.locals[info.SlotB]
			destCell := frame.locals[info.DestSlot]
			if leftCell == nil || rightCell == nil || destCell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_ADD_LOCAL_LOCAL_STORE")
			}

			left := cellValue(leftCell)
			right := cellValue(rightCell)
			setCellValue(destCell, vm.applyBinaryOp(left, right, OP_ADD))

		case OP_LOCAL_CONST_OP_STORE:
			info := instr.Value.(LocalConstOpInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.Slot < 0 || info.Slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_LOCAL_CONST_OP_STORE")
			}
			if frame.locals[info.Slot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}
			cell := frame.locals[info.Slot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_LOCAL_CONST_OP_STORE")
			}
			left := cellValue(cell)
			right := constToTinyValue(info.Const)
			setCellValue(cell, vm.applyBinaryOp(left, right, info.Op))

		case OP_LOCAL_CONST_OP:
			info := instr.Value.(LocalConstOpInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.Slot < 0 || info.Slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_LOCAL_CONST_OP")
			}
			cell := frame.locals[info.Slot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_LOCAL_CONST_OP")
			}
			vm.push(vm.applyBinaryOp(cellValue(cell), constToTinyValue(info.Const), info.Op))

		case OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
			info := instr.Value.(AddLocalGlobalGlobalStoreInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.LocalSlot < 0 || info.LocalSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE")
			}
			if frame.locals[info.LocalSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}
			cell := frame.locals[info.LocalSlot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE")
			}

			globalA, globalB := vm.getCachedGlobalPair(info.GlobalSlotA, info.GlobalSlotB)

			first := vm.applyBinaryOp(cellValue(cell), globalA, OP_ADD)
			setCellValue(cell, vm.applyBinaryOp(first, globalB, OP_ADD))

		case OP_JUMP_LOCAL_GT_LOCAL:
			info := instr.Value.(JumpLocalGTLocalInfo)
			frame := vm.frames[len(vm.frames)-1]

			leftCell := frame.locals[info.SlotA]
			rightCell := frame.locals[info.SlotB]
			if leftCell == nil || rightCell == nil {
				vm.fatalError(ErrorInternal, "nil local in OP_JUMP_LOCAL_GT_LOCAL")
			}

			left := cellValue(leftCell)
			right := cellValue(rightCell)
			leftNum, leftOK := fastNumericValue(left)
			rightNum, rightOK := fastNumericValue(right)
			if !leftOK || !rightOK {
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

			if leftNum > rightNum {
				frame.ip = info.Target
			}

		case OP_CALL_DIRECT_SUB_CONST:
			info := instr.Value.(CallDirectSubConstInfo)

			currentFrame := vm.frames[len(vm.frames)-1]
			if info.Slot < 0 || info.Slot >= len(currentFrame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_CALL_DIRECT_SUB_CONST")
			}
			cell := currentFrame.locals[info.Slot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "local cell is nil in OP_CALL_DIRECT_SUB_CONST")
			}

			var val int
			if cell.IsInt {
				val = cell.Int
			} else {
				val = vm.asInt(cellValue(cell))
			}

			fn, exists := vm.functions[info.FnName]
			if !exists {
				vm.fatalError(ErrorRuntime, "undefined function: %s", info.FnName)
			}

			if info.ArgCount != 1 {
				vm.fatalError(ErrorInternal, "OP_CALL_DIRECT_SUB_CONST expected ArgCount=1, got %d for %s", info.ArgCount, info.FnName)
			}

			vm.callFunctionDirect(fn, []TinyValue{NewInt(val - info.SubValue)})

		case OP_JUMP_LOCAL_GT_CONST:
			info := instr.Value.(JumpLocalGTConstInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.Slot < 0 || info.Slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_LOCAL_GT_CONST")
			}
			cell := frame.locals[info.Slot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "local cell is nil in OP_JUMP_LOCAL_GT_CONST")
			}

			value := cellValue(cell)
			num, ok := fastNumericValue(value)
			if !ok {
				vm.fatalError(ErrorType, "cannot compare %s and number", TypeName(value))
			}

			if num > float64(info.Value) {
				if len(vm.frames) == 0 {
					vm.ip = info.Target
				} else {
					vm.frames[len(vm.frames)-1].ip = info.Target
				}
			}

		case OP_JUMP_LOCAL_GE_LOCAL:
			info := instr.Value.(JumpLocalGELocalInfo)
			frame := vm.frames[len(vm.frames)-1]

			leftCell := frame.locals[info.LeftSlot]
			rightCell := frame.locals[info.RightSlot]
			if leftCell == nil || rightCell == nil {
				vm.fatalError(ErrorInternal, "nil local in OP_JUMP_LOCAL_GE_LOCAL")
			}

			left := cellValue(leftCell)
			right := cellValue(rightCell)
			leftNum, leftOK := fastNumericValue(left)
			rightNum, rightOK := fastNumericValue(right)
			if !leftOK || !rightOK {
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

			if leftNum >= rightNum {
				frame.ip = info.Target
			}

		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info := instr.Value.(JumpModLocalLocalNotZeroInfo)
			frame := vm.frames[len(vm.frames)-1]

			leftCell := frame.locals[info.LeftSlot]
			rightCell := frame.locals[info.RightSlot]
			if leftCell == nil || rightCell == nil {
				vm.fatalError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO")
			}

			left := cellValue(leftCell)
			right := cellValue(rightCell)
			rightNum, rightOK := fastNumericValue(right)
			if !rightOK {
				vm.fatalError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
			}
			if rightNum == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			if left.IsInt && right.IsInt {
				if left.AsInt%right.AsInt != 0 {
					frame.ip = info.Target
				}
				break
			}

			leftNum, leftOK := fastNumericValue(left)
			if !leftOK {
				vm.fatalError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
			}
			if math.Mod(leftNum, rightNum) != 0 {
				frame.ip = info.Target
			}

		case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
			info := instr.Value.(JumpModLocalConstNotZeroInfo)
			frame := vm.frames[len(vm.frames)-1]

			if info.LeftSlot < 0 || info.LeftSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
			}
			if info.Right == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			leftCell := frame.locals[info.LeftSlot]
			if leftCell == nil {
				vm.fatalError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
			}

			left := cellValue(leftCell)
			if left.IsInt {
				if left.AsInt%info.Right != 0 {
					frame.ip = info.Target
				}
				break
			}

			leftNum, ok := fastNumericValue(left)
			if !ok {
				vm.fatalError(ErrorType, "cannot modulo %s and number", TypeName(left))
			}
			if math.Mod(leftNum, float64(info.Right)) != 0 {
				frame.ip = info.Target
			}

		case OP_ADD_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)
			frame := vm.frames[len(vm.frames)-1]

			if frame.locals[info.TargetSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			targetCell := frame.locals[info.TargetSlot]
			sourceCell := frame.locals[info.SourceSlot]
			if targetCell == nil || sourceCell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_ADD_ASSIGN_LOCAL")
			}

			if targetCell.IsInt && sourceCell.IsInt {
				targetCell.Int += sourceCell.Int
				break
			}

			left := cellValue(targetCell)
			right := cellValue(sourceCell)
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					setCellValue(targetCell, NewNative(l+r))
					break
				}
			}

			result := vm.addValues(left, right)
			setCellValue(targetCell, result)

		case OP_SUB_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)
			frame := vm.frames[len(vm.frames)-1]

			if info.TargetSlot < 0 || info.TargetSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "target local slot out of range in OP_SUB_ASSIGN_LOCAL")
			}
			if info.SourceSlot < 0 || info.SourceSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "source local slot out of range in OP_SUB_ASSIGN_LOCAL")
			}
			if frame.locals[info.TargetSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			targetCell := frame.locals[info.TargetSlot]
			sourceCell := frame.locals[info.SourceSlot]
			if targetCell == nil || sourceCell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_SUB_ASSIGN_LOCAL")
			}

			target := cellValue(targetCell)
			source := cellValue(sourceCell)
			targetNum, targetOK := fastNumericValue(target)
			sourceNum, sourceOK := fastNumericValue(source)
			if !targetOK || !sourceOK {
				vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(target), TypeName(source))
			}

			if target.IsInt && source.IsInt {
				targetCell.Int = target.AsInt - source.AsInt
				targetCell.Value = TinyValue{}
				targetCell.IsInt = true
			} else {
				setCellValue(targetCell, NewNative(targetNum-sourceNum))
			}

		case OP_JUMP_LOCAL_GE_CONST:
			info := instr.Value.(JumpLocalGEConstInfo)
			frame := vm.frames[len(vm.frames)-1]

			if info.Slot < 0 || info.Slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_LOCAL_GE_CONST")
			}
			cell := frame.locals[info.Slot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "local cell is nil in OP_JUMP_LOCAL_GE_CONST")
			}

			value := cellValue(cell)
			num, ok := fastNumericValue(value)
			if !ok {
				vm.fatalError(ErrorType, "cannot compare %s and number", TypeName(value))
			}

			if num >= float64(info.Value) {
				if len(vm.frames) == 0 {
					vm.ip = info.Target
				} else {
					vm.frames[len(vm.frames)-1].ip = info.Target
				}
			}

		case OP_STRING_JOIN:
			count := instr.IntArg

			if vm.top < count {
				vm.handleUnderflow()
			}

			start := vm.top - count

			var builder strings.Builder

			for i := start; i < vm.top; i++ {
				builder.WriteString(valueToString(vm.stack[i]))
				vm.stack[i] = TinyValue{}
			}

			vm.top = start
			vm.push(NewNative(builder.String()))

		case OP_CALL_DIRECT:
			info := instr.Value.(DirectCallInfo)

			var fn Function
			var ok bool

			if info.ID >= 0 && info.ID < len(vm.functionList) {
				fn = vm.functionList[info.ID]

				if fn.Name != info.Name {
					fn, ok = vm.functions[info.Name]
					if !ok {
						vm.runtimeError(ErrorName, "undefined function: %s", info.Name)
						break
					}
				}
			} else {
				fn, ok = vm.functions[info.Name]
				if !ok {
					vm.runtimeError(ErrorName, "invalid function id for %s", info.Name)
					break
				}
			}

			if fn.Async {
				args := vm.popArgs(info.ArgCount)
				task := &NativeTaskValue{
					Done: make(chan TaskResult, 1),
				}

				taskVM := vm.CloneForTask()
				vm.observerStats.TaskStarted()

				go func() {
					defer func() {
						if r := recover(); r != nil {
							vm.observerStats.TaskFailed()
							task.Done <- TaskResult{
								Error: r,
							}
						}
					}()

					result := taskVM.runFunctionToCompletion(fn, args)
					vm.observerStats.TaskCompleted()

					task.Done <- TaskResult{
						Value: result,
					}
				}()

				vm.push(NewNative(task))
			} else {
				vm.callFunctionDirectFromStack(fn, info.ArgCount, "function "+info.Name)
			}

			break

		case OP_OBJECT_IN:
			keyValue := vm.popFast()
			objectValue := asObject(vm.popFast(), vm)

			var rawKey any
			if keyValue.IsInt {
				rawKey = keyValue.AsInt
			} else {
				rawKey = keyValue.Value
			}

			found := false
			_, found = objectValue[rawKey]

			vm.push(NewNative(found))

		case OP_INSTANCEOF:
			classValue := vm.popFast()
			objectValue := vm.popFast()

			var className string
			var rawClass any
			if classValue.IsInt {
				rawClass = classValue.AsInt
			} else {
				rawClass = classValue.Value
			}

			switch c := rawClass.(type) {
			case Class:
				className = c.Name

			case *Class:
				className = c.Name

			default:
				vm.fatalError(ErrorType, "right side of instanceof must be class, got %s", TypeName(classValue))
			}

			vm.push(NewNative(vm.isInstanceOf(objectValue, className)))

		case OP_AWAIT:
			value := vm.popFast()

			if task, ok := value.Value.(*NativeTaskValue); ok {
				result := <-task.Done

				if result.Error != nil {
					panic(result.Error)
				}

				vm.push(result.Value)
			} else if array, ok := vm.valueAsArrayForRead(value); ok {
				for _, e := range array.Elements {
					task, ok := e.Value.(*NativeTaskValue)

					if ok {
						result := <-task.Done
						if result.Error != nil {
							panic(result.Error)
						}
					}
				}
				vm.push(value)
			} else {
				vm.push(value)
			}

		case OP_LOCK_MUTEX:
			value := vm.popFast()

			mutex, ok := value.Value.(*NativeMutexValue)
			if !ok {
				vm.fatalError(ErrorType, "lock mutex expects mutex, got %s", TypeName(value))
			}

			mutex.Lock()

			if len(vm.frames) > 0 {
				frame := vm.frames[len(vm.frames)-1]
				frame.lockedMutexes = append(frame.lockedMutexes, mutex)
			}

			vm.push(NewNull())

		case OP_UNLOCK_MUTEX:
			value := vm.popFast()

			mutex, ok := value.Value.(*NativeMutexValue)
			if !ok {
				vm.fatalError(ErrorType, "unlock mutex expects mutex, got %s", TypeName(value))
			}

			mutex.Unlock()

			if len(vm.frames) > 0 {
				frame := vm.frames[len(vm.frames)-1]
				for i, m := range frame.lockedMutexes {
					if m == mutex {
						frame.lockedMutexes = append(frame.lockedMutexes[:i], frame.lockedMutexes[i+1:]...)
						break
					}
				}
			}

			vm.push(NewNull())

		case OP_DEFER:
			value := vm.popFast()

			fn, ok := value.Value.(FunctionValue)
			if !ok {
				vm.fatalError(ErrorType, "defer expects function, got %s", TypeName(value))
			}

			vm.deferHandlers = append(vm.deferHandlers, DeferHandler{
				Function:   fn,
				FrameDepth: len(vm.frames),
			})

			vm.push(NewNull())

		case OP_SPAWN:
			value := vm.popFast()
			args := vm.popArgs(instr.Value.(int))

			fn, ok := value.Value.(FunctionValue)
			if !ok {
				vm.fatalError(ErrorType, "spawn expects function, got %s", TypeName(value))
			}

			task := &NativeTaskValue{
				Done: make(chan TaskResult, 1),
			}

			if vm.taskPool == nil {
				vm.fatalError(ErrorRuntime, "spawn is not available in this VM context")
			}

			vm.observerStats.TaskStarted()
			go func() {
				taskVM := vm.taskPool.Get()
				defer vm.taskPool.Put(taskVM)
				defer func() {
					if r := recover(); r != nil {
						vm.observerStats.TaskFailed()
						task.Done <- TaskResult{
							Error: r,
						}
					}
				}()

				result := taskVM.callFunctionValue(fn, args)
				vm.observerStats.TaskCompleted()

				task.Done <- TaskResult{
					Value: result,
				}
			}()

			vm.push(NewNative(task))

		case OP_TYPEOF:
			value := vm.popFast()
			vm.push(NewNative(TypeName(value)))

		case OP_NEGATE:
			value := vm.popFast()

			if value.IsInt {
				vm.push(NewInt(-value.AsInt))
				break
			}

			switch v := value.Value.(type) {
			case int:
				vm.push(NewInt(-v))

			case int64:
				vm.push(NewNative(-v))

			case float64:
				vm.push(NewNative(-v))

			case float32:
				vm.push(NewNative(-v))

			default:
				vm.fatalError(ErrorType, "cannot negate %s", TypeName(value))
			}
		case OP_CLOSURE:
			info := instr.Value.(ClosureInfo)

			captures := map[int]*Cell{}

			if len(info.Captures) > 0 {
				if len(vm.frames) == 0 {
					vm.fatalError(ErrorInternal, "closure has captures but no current function frame")
				}

				frame := vm.currentFrame()
				frame.hasEscapedLocals = true

				for _, capture := range info.Captures {
					if capture.OuterSlot < 0 || capture.OuterSlot >= len(frame.locals) {
						vm.fatalError(
							ErrorInternal,
							"capture slot out of range: function=%s outerSlot=%d locals=%d",
							frame.function.Name,
							capture.OuterSlot,
							len(frame.locals),
						)
					}

					if frame.locals[capture.OuterSlot] == nil {
						vm.fatalError(
							ErrorInternal,
							"captured local is nil: function=%s outerSlot=%d",
							frame.function.Name,
							capture.OuterSlot,
						)
					}

					captures[capture.InnerSlot] = frame.locals[capture.OuterSlot]
				}
			}

			vm.push(NewNative(FunctionValue{
				Name:        info.Name,
				Captures:    captures,
				MethodClass: vm.currentMethodClass(), // inherit enclosing class for private field access
			}))

		case OP_CONST:
			vm.push(ToValue(instr.Value))

		case OP_SET_PROPERTY:
			name := instr.Value.(string)

			value := vm.popFast()
			objectValue := vm.popFast()

			if obj, ok := objectValue.Value.(WasmObjectValue); ok {
				offset := vm.getPropertyOffset(name)
				addr := uint32(obj.Address) + offset

				tag := 1.0
				val := 0.0

				if value.IsInt {
					tag = 1.0
					val = float64(value.AsInt)
				} else {
					switch v := value.Value.(type) {
					case float64:
						tag = 1.0
						val = v
					case bool:
						tag = 2.0
						if v {
							val = 1.0
						} else {
							val = 0.0
						}
					case string:
						tag = 6.0
						val = vm.writeWasmString(v)
					case WasmObjectValue:
						tag = 4.0
						val = v.Address
					case WasmArrayValue:
						tag = 5.0
						val = v.Address
					case NullValue:
						tag = 0.0
						val = 0.0
					default:
						vm.fatalError(ErrorType, "cannot store %s in JIT object", TypeName(value))
					}
				}

				vm.WriteWasmFloat(addr, tag)
				vm.WriteWasmFloat(addr+8, val)
				break
			}

			if inst, ok := instanceValue(objectValue); ok {
				if _, isConstant := inst.ConstFields[name]; isConstant {
					vm.runtimeError(ErrorRuntime, "cannot assign to constant field: %s", name)
				}

				if !vm.canAccessField(inst, name) {
					vm.runtimeError(ErrorRuntime, "cannot assign private field: %s", name)
				}

				inst.Fields[name] = value
				break
			}

			object, ok := vm.valueAsObjectForRead(objectValue)
			if !ok {
				vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
			}

			object[name] = value
			vm.invalidateJitObjectMirror(object)

		case OP_METHOD_CALL_SAFE:
			info := instr.Value.(MethodCallInfo)

			args := vm.popCallArgs(info.ArgCount, info.SpreadArgs)
			objectValue := vm.popFast()

			if isNullish(objectValue) {
				vm.push(NewNull())
				break
			}

			vm.callMethodResolved(info.Method, objectValue, args)

		case OP_COALESCE_JUMP:
			right := vm.popFast()
			left := vm.popFast()

			if isNullish(left) {
				vm.push(right)
			} else {
				vm.push(left)
			}

		case OP_GET_PROPERTY_SAFE:
			name := instr.Value.(string)
			objectValue := vm.popFast()
			vm.push(vm.getProperty(objectValue, name, true))

		case OP_LOAD_GLOBAL:
			var slot int
			if info, ok := instr.Value.(VariableInfo); ok {
				slot = info.Slot
			} else if s, ok := asIntInternal(instr.Value); ok {
				slot = s
			} else {
				vm.fatalError(ErrorInternal, "unexpected type for OP_LOAD_GLOBAL: %T", instr.Value)
			}

			if slot < 0 {
				vm.push(NewNull())
				break
			}

			vm.push(vm.getGlobal(slot))

		case OP_SETUP_TRY:
			info := instr.Value.(TryInfo)

			vm.tryHandlers = append(vm.tryHandlers, TryHandler{
				CatchIP:    info.CatchIP,
				Name:       info.Name,
				Slot:       info.Slot,
				IsLocal:    info.IsLocal,
				FrameDepth: len(vm.frames),
			})

		case OP_POP_TRY:
			if len(vm.tryHandlers) == 0 {
				vm.fatalError(ErrorInternal, "try handler stack underflow")
			}

			vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

		case OP_STORE_GLOBAL:
			info := instr.Value.(VariableInfo)
			value := vm.popFast()

			if info.Slot < 0 {
				break
			}

			if !info.Uninitialized {
				if ok, reason := vm.checkTypeHint(value, info.TypeHint); !ok {
					vm.fatalError(
						ErrorType,
						"variable %s expected %s, got %s%s",
						info.Name,
						info.TypeHint.Name,
						TypeName(value),
						reason,
					)
				}
			}

			vm.mu.Lock()

			if vm.globalNames == nil {
				vm.globalNames = map[string]int{}
			}
			vm.globalNames[info.Name] = info.Slot

			vm.setGlobalUnlocked(info.Slot, value)

			vm.globalConstants[info.Name] = info.Constant
			vm.globalTypes[info.Name] = info.TypeHint
			vm.mu.Unlock()

		case OP_LOAD_LOCAL:
			slot := instr.IntArg

			if slot < 0 {
				if info, ok := instr.Value.(VariableInfo); ok {
					vm.mu.RLock()
					vm.push(vm.getGlobal(info.Slot))
					vm.mu.RUnlock()
				}
				break
			}

			frame := vm.frames[len(vm.frames)-1]

			if slot >= len(frame.locals) {
				vm.fatalError(
					ErrorInternal,
					"local slot out of range: function=%s slot=%d locals=%d",
					frame.function.Name,
					slot,
					len(frame.locals),
				)
			}

			vm.push(cellValue(frame.locals[slot]))

		case OP_LOAD_LOCAL_0:
			frame := vm.frames[len(vm.frames)-1]
			cell := frame.locals[0]
			if cell.IsInt {
				vm.push(NewInt(cell.Int))
			} else {
				vm.push(cell.Value)
			}

		case OP_LOAD_LOCAL_1:
			frame := vm.frames[len(vm.frames)-1]
			cell := frame.locals[1]
			if cell.IsInt {
				vm.push(NewInt(cell.Int))
			} else {
				vm.push(cell.Value)
			}

		case OP_LOAD_LOCAL_2:
			frame := vm.frames[len(vm.frames)-1]
			cell := frame.locals[2]
			if cell.IsInt {
				vm.push(NewInt(cell.Int))
			} else {
				vm.push(cell.Value)
			}

		case OP_LOAD_LOCAL_3:
			frame := vm.frames[len(vm.frames)-1]
			cell := frame.locals[3]
			if cell.IsInt {
				vm.push(NewInt(cell.Int))
			} else {
				vm.push(cell.Value)
			}

		case OP_STORE_LOCAL:
			info := instr.Value.(VariableInfo)
			value := vm.popFast()

			if info.Slot < 0 {
				vm.mu.Lock()
				vm.setGlobalUnlocked(info.Slot, value)
				vm.globalConstants[info.Name] = info.Constant
				vm.mu.Unlock()
				break
			}

			frame := vm.currentFrame()

			if info.Slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range: %d", info.Slot)
			}

			if !info.TypeHint.IsEmpty() && !info.Uninitialized {
				if ok, reason := vm.checkTypeHint(value, info.TypeHint); !ok {
					vm.fatalError(
						ErrorType,
						"variable %s expected %s, got %s%s",
						info.Name,
						info.TypeHint.Name,
						TypeName(value),
						reason,
					)
				}
			}

			cell := frame.locals[info.Slot]
			if cell == nil {
				cell = &Cell{}
				frame.locals[info.Slot] = cell
			}
			setCellValue(cell, value)
			frame.locals[info.Slot].Constant = info.Constant
			frame.locals[info.Slot].TypeHint = info.TypeHint

		case OP_ASSIGN_GLOBAL:
			value := vm.popFast()

			var slot int
			var name string

			vm.mu.RLock()

			if info, ok := instr.Value.(VariableInfo); ok {
				slot = info.Slot
				name = info.Name
			} else if s, ok := instr.Value.(string); ok {
				name = s
				var exists bool
				slot, exists = vm.globalNames[name]
				if !exists {
					vm.mu.RUnlock()
					vm.fatalError(ErrorName, "undefined global variable: %s", name)
				}
			} else {
				vm.fatalError(ErrorInternal, "unexpected type for OP_ASSIGN_GLOBAL: %T", instr.Value)
			}

			if slot < 0 {
				vm.mu.RUnlock()
				break
			}

			if vm.globalConstants[name] {
				vm.mu.RUnlock()
				vm.fatalError(ErrorConst, "cannot assign to constant global")
			}

			hint := vm.globalTypes[name]
			vm.mu.RUnlock()

			if !hint.IsEmpty() {
				if ok, reason := vm.checkTypeHint(value, hint); !ok {
					vm.fatalError(
						ErrorType,
						"global %s expected %s, got %s%s",
						name,
						hint.Name,
						TypeName(value),
						reason,
					)
				}
			}

			vm.setGlobal(slot, value)

		case OP_INC_LOCAL:
			var slot int
			intAmount := 1
			floatAmount := 1.0
			isFloat := false

			if s, ok := asIntInternal(instr.Value); ok {
				slot = s
			} else if info, ok := instr.Value.(IncrementInfo); ok {
				slot = info.Slot
				intAmount = info.IntAmount
				floatAmount = info.FloatAmount
				isFloat = info.IsFloat

				if !isFloat && floatAmount == 0 && intAmount != 0 {
					floatAmount = float64(intAmount)
				}
			} else {
				vm.fatalError(ErrorInternal, "unexpected type for OP_INC_LOCAL: %T", instr.Value)
			}

			frame := vm.frames[len(vm.frames)-1]

			if slot < 0 || slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_INC_LOCAL")
			}

			cell := frame.locals[slot]

			if cell == nil {
				vm.fatalError(ErrorInternal, "local cell is nil in OP_INC_LOCAL")
			}

			if frame.locals[slot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			if cell.IsInt && !isFloat {
				cell.Int += intAmount
				break
			}

			value := cellValue(cell)
			var rawVal any
			if value.IsInt {
				rawVal = value.AsInt
			} else {
				rawVal = value.Value
			}

			switch v := rawVal.(type) {
			case int:
				cell.Int = v + intAmount
				cell.IsInt = true

			case int64:
				setCellValue(cell, NewNative(v+int64(intAmount)))

			case float64:
				setCellValue(cell, NewNative(v+floatAmount))

			case float32:
				setCellValue(cell, NewNative(v+float32(floatAmount)))

			default:
				vm.runtimeError(ErrorType, "cannot increment %s", TypeName(value))
			}

		case OP_DEC_LOCAL:
			var slot int
			intAmount := 1
			floatAmount := 1.0
			isFloat := false

			switch info := instr.Value.(type) {
			case int:
				slot = info
			case int64:
				slot = int(info)
			case DecrementInfo:
				slot = info.Slot
				intAmount = info.IntAmount
				floatAmount = info.FloatAmount
				isFloat = info.IsFloat
			default:
				vm.fatalError(ErrorInternal, "unexpected type for OP_DEC_LOCAL: %T", instr.Value)
			}

			frame := vm.frames[len(vm.frames)-1]

			if slot < 0 || slot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_DEC_LOCAL")
			}

			cell := frame.locals[slot]

			if cell == nil {
				vm.fatalError(ErrorInternal, "local cell is nil in OP_DEC_LOCAL")
			}

			if frame.locals[slot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			if cell.IsInt && !isFloat {
				cell.Int -= intAmount
				break
			}

			value := cellValue(cell)
			var rawVal any
			if value.IsInt {
				rawVal = value.AsInt
			} else {
				rawVal = value.Value
			}

			switch v := rawVal.(type) {
			case int:
				cell.Int = v - intAmount
				cell.IsInt = true

			case int64:
				setCellValue(cell, NewNative(v-int64(intAmount)))

			case float64:
				setCellValue(cell, NewNative(v-floatAmount))

			case float32:
				setCellValue(cell, NewNative(v-float32(floatAmount)))

			default:
				vm.runtimeError(ErrorType, "cannot decrement %s", TypeName(value))
			}

		case OP_INC_GLOBAL:
			var name string
			intAmount := 1
			floatAmount := 1.0
			isFloat := false

			switch info := instr.Value.(type) {
			case string:
				name = info
			case IncrementInfo:
				name = info.Name
				intAmount = info.IntAmount
				floatAmount = info.FloatAmount
				isFloat = info.IsFloat
			default:
				vm.fatalError(ErrorInternal, "unexpected type for OP_INC_GLOBAL: %T", instr.Value)
			}

			vm.mu.Lock()

			if vm.globalConstants[name] {
				vm.mu.Unlock()
				vm.fatalError(ErrorConst, "cannot increment constant global")
			}

			slot, exists := vm.globalNames[name]
			if !exists {
				vm.mu.Unlock()
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}

			if slot < 0 || slot >= len(*vm.globals) {
				vm.mu.Unlock()
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}

			value := (*vm.globals)[slot]
			var rawVal any
			if value.IsInt {
				rawVal = value.AsInt
			} else {
				rawVal = value.Value
			}

			switch v := rawVal.(type) {
			case int:
				if isFloat {
					vm.setGlobalUnlocked(slot, NewNative(float64(v)+floatAmount))
				} else {
					vm.setGlobalUnlocked(slot, NewInt(v+intAmount))
				}
			case float64:
				if isFloat {
					vm.setGlobalUnlocked(slot, NewNative(v+floatAmount))
				} else {
					vm.setGlobalUnlocked(slot, NewNative(v+float64(intAmount)))
				}
			default:
				vm.mu.Unlock()
				vm.fatalError(ErrorType, "cannot increment %s", TypeName(value))
			}
			vm.mu.Unlock()

		case OP_DEC_GLOBAL:
			var name string
			intAmount := 1
			floatAmount := 1.0
			isFloat := false

			switch info := instr.Value.(type) {
			case string:
				name = info
			case DecrementInfo:
				name = info.Name
				intAmount = info.IntAmount
				floatAmount = info.FloatAmount
				isFloat = info.IsFloat
			default:
				vm.fatalError(ErrorInternal, "unexpected type for OP_DEC_GLOBAL: %T", instr.Value)
			}

			vm.mu.Lock()

			if vm.globalConstants[name] {
				vm.mu.Unlock()
				vm.fatalError(ErrorConst, "cannot decrement constant global")
			}

			slot, exists := vm.globalNames[name]
			if !exists {
				vm.mu.Unlock()
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}

			if slot < 0 || slot >= len(*vm.globals) {
				vm.mu.Unlock()
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}

			value := (*vm.globals)[slot]
			var rawVal any
			if value.IsInt {
				rawVal = value.AsInt
			} else {
				rawVal = value.Value
			}

			switch v := rawVal.(type) {
			case int:
				if isFloat {
					vm.setGlobalUnlocked(slot, NewNative(float64(v)-floatAmount))
				} else {
					vm.setGlobalUnlocked(slot, NewInt(v-intAmount))
				}
			case float64:
				if isFloat {
					vm.setGlobalUnlocked(slot, NewNative(v-floatAmount))
				} else {
					vm.setGlobalUnlocked(slot, NewNative(v-float64(intAmount)))
				}
			default:
				vm.mu.Unlock()
				vm.fatalError(ErrorType, "cannot decrement %s", TypeName(value))
			}
			vm.mu.Unlock()

		case OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			value := vm.popFast()

			if slot < 0 {
				// This case should ideally be handled by OP_ASSIGN_GLOBAL,
				// but we handle it here for robustness if the compiler emits it.
				if info, ok := instr.Value.(VariableInfo); ok {
					vm.mu.Lock()
					vm.setGlobalUnlocked(info.Slot, value)
					vm.mu.Unlock()
				}
				break
			}

			frame := vm.frames[len(vm.frames)-1]

			if slot >= len(frame.locals) {
				vm.fatalError(
					ErrorInternal,
					"local slot out of range: function=%s slot=%d locals=%d",
					frame.function.Name,
					slot,
					len(frame.locals),
				)
			}

			if frame.locals[slot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}

			hint := frame.locals[slot].TypeHint

			if !hint.IsEmpty() {
				if ok, reason := vm.checkTypeHint(value, hint); !ok {
					vm.fatalError(
						ErrorType,
						"local variable expected %s, got %s%s",
						hint.Name,
						TypeName(value),
						reason,
					)
				}
			}

			setCellValue(frame.locals[slot], value)

		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			args := vm.popCallArgs(info.ArgCount, info.SpreadArgs)

			if info.NewLine {
				for i, arg := range args {
					if i > 0 {
						fmt.Print(" ")
					}
					fmt.Print(valueToString(arg, true))
				}
				fmt.Println()
			} else {
				for _, arg := range args {
					fmt.Print(valueToString(arg, true))
				}
			}

			vm.push(NewNull())

		case OP_MUL_LOCAL_CONST:
			info := instr.Value.(LocalConstInfo)
			frame := vm.frames[len(vm.frames)-1]
			vm.push(vm.multiplyByInt(frameLocalValue(frame, info.Slot, "OP_MUL_LOCAL_CONST"), info.Value))

		case OP_MATH_FLOOR:
			val := vm.popFast()
			x := vm.asFloat64(val)
			vm.push(NewNative(math.Floor(x)))

		case OP_MATH_CEIL:
			val := vm.popFast()
			x := vm.asFloat64(val)
			vm.push(NewNative(math.Ceil(x)))

		case OP_MATH_SQRT:
			val := vm.popFast()
			x := vm.asFloat64(val)
			vm.push(NewNative(math.Sqrt(x)))

		case OP_MATH_ABS:
			val := vm.popFast()
			x := vm.asFloat64(val)
			vm.push(NewNative(math.Abs(x)))

		case OP_MATH_POW:
			arg1 := vm.popFast()
			arg0 := vm.popFast()
			base := vm.asFloat64(arg0)
			exp := vm.asFloat64(arg1)
			vm.push(NewNative(math.Pow(base, exp)))

		case OP_ADD:
			right := vm.popFast()
			left := vm.popFast()
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					vm.push(NewNative(l + r))
					break
				}
			}
			vm.push(vm.addValues(left, right))

		case OP_SUB:
			right := vm.popFast()
			left := vm.popFast()
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					vm.push(NewNative(l - r))
					break
				}
			}
			vm.push(vm.subValues(left, right))

		case OP_MUL:
			right := vm.popFast()
			left := vm.popFast()
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					vm.push(NewNative(l * r))
					break
				}
			}
			vm.push(vm.mulValues(left, right))

		case OP_DIV:
			right := vm.popFast()
			left := vm.popFast()
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					if r == 0 {
						vm.runtimeError(ErrorRuntime, "cannot divide by zero")
						break
					}
					vm.push(NewNative(l / r))
					break
				}
			}
			vm.push(vm.divValues(left, right))

		case OP_MOD:
			right := vm.popFast()
			left := vm.popFast()
			if left.IsInt && right.IsInt {
				if right.AsInt == 0 {
					vm.runtimeError(ErrorRuntime, "cannot modulo by zero")
					break
				}
				vm.push(NewNative(float64(left.AsInt % right.AsInt)))
				break
			}
			if l, ok := fastNumericValue(left); ok {
				if r, ok := fastNumericValue(right); ok {
					if r == 0 {
						vm.runtimeError(ErrorRuntime, "cannot modulo by zero")
						break
					}
					vm.push(NewNative(math.Mod(l, r)))
					break
				}
			}
			vm.push(vm.modValues(left, right))

		case OP_AND_BIT, OP_OR_BIT, OP_XOR, OP_LSHIFT, OP_RSHIFT:
			right := vm.popFast()
			left := vm.popFast()
			vm.push(vm.bitwiseValues(left, right, instr.Op))

		case OP_EQ:
			right := vm.popFast()
			left := vm.popFast()
			vm.push(NewNative(valuesEqual(left, right)))

		case OP_NEQ:
			right := vm.popFast()
			left := vm.popFast()
			vm.push(NewNative(!valuesEqual(left, right)))

		case OP_LT:
			right := vm.popFast()
			left := vm.popFast()

			if left.IsInt && right.IsInt {
				vm.push(NewNative(left.AsInt < right.AsInt))
				break
			}

			var leftVal any
			if left.IsInt {
				leftVal = left.AsInt
			} else {
				leftVal = left.Value
			}

			var rightVal any
			if right.IsInt {
				rightVal = right.AsInt
			} else {
				rightVal = right.Value
			}

			switch l := leftVal.(type) {
			case int:
				switch r := rightVal.(type) {
				case int:
					vm.push(NewNative(l < r))

				case float64:
					vm.push(NewNative(float64(l) < r))

				default:
					vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
				}

			case float64:
				switch r := rightVal.(type) {
				case int:
					vm.push(NewNative(l < float64(r)))

				case float64:
					vm.push(NewNative(l < r))

				default:
					vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
				}

			default:
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		case OP_GT:
			right := vm.popFast()
			left := vm.popFast()

			if left.IsInt && right.IsInt {
				vm.push(NewNative(left.AsInt > right.AsInt))
				break
			}

			if !isNumber(left) || !isNumber(right) {
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

			vm.push(NewNative(asFloat(left, vm) > asFloat(right, vm)))

		case OP_LTE:
			right := vm.popFast()
			left := vm.popFast()

			if left.IsInt && right.IsInt {
				vm.push(NewNative(left.AsInt <= right.AsInt))
				break
			}

			if !isNumber(left) || !isNumber(right) {
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

			vm.push(NewNative(asFloat(left, vm) <= asFloat(right, vm)))

		case OP_GTE:
			right := vm.popFast()
			left := vm.popFast()

			if left.IsInt && right.IsInt {
				vm.push(NewNative(left.AsInt >= right.AsInt))
				break
			}

			if !isNumber(left) || !isNumber(right) {
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

			vm.push(NewNative(asFloat(left, vm) >= asFloat(right, vm)))

		case OP_AND:
			right := vm.popFast()
			left := vm.popFast()
			vm.push(NewNative(isTruthy(left) && isTruthy(right)))

		case OP_OR:
			right := vm.popFast()
			left := vm.popFast()
			vm.push(NewNative(isTruthy(left) || isTruthy(right)))

		case OP_JUMP:
			target := instr.IntArg
			vm.setIP(target)

		case OP_JUMP_IF_FALSE:
			target := instr.IntArg
			condition := vm.popFast()

			if !isTruthy(condition) {
				vm.setIP(target)
			}

		case OP_JUMP_IF_TRUE:
			target := instr.IntArg
			condition := vm.popFast()

			if isTruthy(condition) {
				vm.setIP(target)
			}

		case OP_METHOD_CALL:
			info := instr.Value.(MethodCallInfo)

			cacheIdx := cfIP - 1
			var cache *MethodCallCache
			if cacheIdx >= 0 && cacheIdx < len(cfMethodCache) {
				cache = &cfMethodCache[cacheIdx]
			}

			if cache != nil && cache.CachedFunc != nil {
				if len(info.SpreadArgs) > 0 {
					args := vm.popCallArgs(info.ArgCount, info.SpreadArgs)
					objectValue := vm.popFast()
					if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
						popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
						cache.CachedFunc(vm, args)
						popNative()
						break
					}
					vm.callMethodResolved(info.Method, objectValue, args)
				} else {
					switch info.ArgCount {
					case 0:
						objectValue := vm.popFast()
						if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
							popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
							cache.CachedFunc(vm, nil)
							popNative()
							break
						}
						if !vm.callZeroArgNativeMethod(info.Method, objectValue) {
							vm.callMethodResolved(info.Method, objectValue, nil)
						}
					case 1:
						arg0 := vm.popFast()
						objectValue := vm.popFast()
						if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
							popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
							cache.CachedFunc(vm, []TinyValue{arg0})
							popNative()
							break
						}
						if !vm.callStdObjectFast1(info.Method, objectValue, arg0) {
							if !vm.callOneArgNativeMethod(info.Method, objectValue, arg0) {
								vm.callMethodResolved(info.Method, objectValue, []TinyValue{arg0})
							}
						}
					case 2:
						arg1 := vm.popFast()
						arg0 := vm.popFast()
						objectValue := vm.popFast()
						if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
							popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
							cache.CachedFunc(vm, []TinyValue{arg0, arg1})
							popNative()
							break
						}
						if !vm.callStdObjectFast2(info.Method, objectValue, arg0, arg1) {
							if !vm.callTwoArgNativeMethod(info.Method, objectValue, arg0, arg1) {
								vm.callMethodResolved(info.Method, objectValue, []TinyValue{arg0, arg1})
							}
						}
					case 3:
						arg2 := vm.popFast()
						arg1 := vm.popFast()
						arg0 := vm.popFast()
						objectValue := vm.popFast()
						if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
							popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
							cache.CachedFunc(vm, []TinyValue{arg0, arg1, arg2})
							popNative()
							break
						}
						if !vm.callStdObjectFast3(info.Method, objectValue, arg0, arg1, arg2) {
							vm.callMethodResolved(info.Method, objectValue, []TinyValue{arg0, arg1, arg2})
						}
					default:
						args := vm.popArgs(info.ArgCount)
						objectValue := vm.popFast()
						if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && stdVal.Name == cache.CachedModule {
							popNative := vm.pushNativeFrame(stdVal.Name + "." + cache.Method)
							cache.CachedFunc(vm, args)
							popNative()
							break
						}
						vm.callMethodResolved(info.Method, objectValue, args)
					}
				}
				break
			}

			// Slow Path (First run)
			if len(info.SpreadArgs) > 0 {
				args := vm.popCallArgs(info.ArgCount, info.SpreadArgs)
				objectValue := vm.popFast()
				if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok && cache != nil {
					fn := vm.resolveStdModuleFunc(stdVal.Name, info.Method)
					if fn != nil {
						cache.Method = info.Method
						cache.ArgCount = info.ArgCount
						cache.SpreadArgs = info.SpreadArgs
						cache.CachedFunc = fn
						cache.CachedModule = stdVal.Name
					}
				}
				vm.callMethodResolved(info.Method, objectValue, args)
			} else {
				objIdx := vm.top - info.ArgCount - 1
				if objIdx >= 0 && objIdx < vm.top && cache != nil {
					objectValue := vm.stack[objIdx]
					if stdVal, ok := objectValue.Value.(*StandardModuleValue); ok {
						fn := vm.resolveStdModuleFunc(stdVal.Name, info.Method)
						if fn != nil {
							cache.Method = info.Method
							cache.ArgCount = info.ArgCount
							cache.SpreadArgs = info.SpreadArgs
							cache.CachedFunc = fn
							cache.CachedModule = stdVal.Name
						}
					}
				}
				vm.callMethod(info.Method, info.ArgCount)
			}

		case OP_METHOD_CALL_LOCAL_0:
			info := instr.Value.(MethodLocalCallInfo)
			frame := vm.frames[len(vm.frames)-1]
			objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_0")

			if info.Method == "toString" {
				if obj, ok := vm.valueAsObjectForRead(objectValue); ok {
					if _, ok := obj["toString"]; !ok {
						vm.push(NewNative(valueToString(objectValue)))
						break
					}
				} else {
					vm.push(NewNative(valueToString(objectValue)))
					break
				}
			}

			if vm.callZeroArgNativeMethod(info.Method, objectValue) {
				break
			}
			vm.callMethodResolved(info.Method, objectValue, nil)

		case OP_METHOD_CALL_LOCAL_1:
			info := instr.Value.(MethodLocalCallInfo)
			frame := vm.frames[len(vm.frames)-1]
			objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_1")
			arg := frameLocalValue(frame, info.ArgSlot, "OP_METHOD_CALL_LOCAL_1")

			if vm.callOneArgNativeMethod(info.Method, objectValue, arg) {
				break
			}
			vm.callMethodResolved(info.Method, objectValue, []TinyValue{arg})

		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			info := instr.Value.(AddLocalArrayIndexStoreInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.LocalSlot < 0 || info.LocalSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_ADD_LOCAL_ARRAY_INDEX_STORE")
			}
			if frame.locals[info.LocalSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}
			targetCell := frame.locals[info.LocalSlot]
			if targetCell == nil {
				vm.fatalError(ErrorInternal, "nil target local in OP_ADD_LOCAL_ARRAY_INDEX_STORE")
			}

			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ADD_LOCAL_ARRAY_INDEX_STORE")
			indexValue := frameLocalValue(frame, info.IndexSlot, "OP_ADD_LOCAL_ARRAY_INDEX_STORE")
			index, ok := fastIndexInt(indexValue)
			if !ok {
				index = vm.asInt(indexValue)
			}

			var element TinyValue
			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				if index < 0 || index >= len(array.Elements) {
					vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
					break
				}
				element = array.Elements[index]
			} else if array, ok := arrayValue.Value.(ArrayValue); ok {
				if index < 0 || index >= len(array.Elements) {
					vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
					break
				}
				element = array.Elements[index]
			} else {
				element = vm.getIndexValue(arrayValue, indexValue)
			}

			setCellValue(targetCell, vm.applyBinaryOp(cellValue(targetCell), element, OP_ADD))

		case OP_ARRAY_INDEX_CONST_OP_STORE:
			info := instr.Value.(ArrayIndexConstOpInfo)
			frame := vm.frames[len(vm.frames)-1]
			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_INDEX_CONST_OP_STORE")
			indexValue := frameLocalValue(frame, info.IndexSlot, "OP_ARRAY_INDEX_CONST_OP_STORE")
			index, ok := fastIndexInt(indexValue)
			if !ok {
				index = vm.asInt(indexValue)
			}
			amount := constToTinyValue(info.Const)

			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				if index < 0 || index >= len(array.Elements) {
					vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
					break
				}
				array.Elements[index] = vm.applyBinaryOp(array.Elements[index], amount, info.Op)
				vm.invalidateJitArrayMirror(array)
				break
			}

			if array, ok := arrayValue.Value.(ArrayValue); ok {
				if index < 0 || index >= len(array.Elements) {
					vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
					break
				}
				array.Elements[index] = vm.applyBinaryOp(array.Elements[index], amount, info.Op)
				setCellValue(frame.locals[info.ArraySlot], NewNative(array))
				break
			}

			// Fall back to the normal dynamic path for non-Tiny arrays.
			vm.push(arrayValue)
			vm.push(indexValue)
			vm.push(vm.applyBinaryOp(vm.getIndexValue(arrayValue, indexValue), amount, info.Op))
			// OP_SET_INDEX semantics without recursive dispatch.
			value := vm.popFast()
			idx := vm.popFast()
			obj := vm.popFast()
			vm.setIndexValue(obj, idx, value)

		case OP_ARRAY_LEN_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			frame := vm.frames[len(vm.frames)-1]
			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_LEN_LOCAL")

			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				vm.push(NewInt(len(array.Elements)))
				break
			}
			vm.callMethodResolved("length", arrayValue, nil)

		case OP_ARRAY_GET_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			frame := vm.frames[len(vm.frames)-1]
			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_GET_LOCAL")
			indexValue := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_GET_LOCAL")

			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				var index int
				if indexValue.IsInt {
					index = indexValue.AsInt
				} else {
					var ok bool
					index, ok = indexValue.Value.(int)
					if !ok {
						vm.runtimeError(ErrorType, "array.get argument 1 expected number, got %s", TypeName(indexValue))
					}
				}
				if index < 0 || index >= len(array.Elements) {
					vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
				}
				vm.push(array.Elements[index])
				break
			}
			vm.callMethodResolved("get", arrayValue, []TinyValue{indexValue})

		case OP_ARRAY_PUSH_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			frame := vm.frames[len(vm.frames)-1]
			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL")
			value := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL")

			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				array.Elements = append(array.Elements, value)
				vm.invalidateJitArrayMirror(array)
				vm.push(arrayValue)
				break
			}
			vm.callMethodResolved("push", arrayValue, []TinyValue{value})

		case OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			info := instr.Value.(ArrayLocalMulConstInfo)
			frame := vm.frames[len(vm.frames)-1]
			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST")
			arg := vm.multiplyByInt(frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST"), info.Factor)

			if array, ok := arrayValue.Value.(*ArrayValue); ok {
				array.Elements = append(array.Elements, arg)
				vm.invalidateJitArrayMirror(array)
				vm.push(arrayValue)
				break
			}
			vm.callMethodResolved("push", arrayValue, []TinyValue{arg})

		case OP_LEN:
			value := vm.popFast()

			var rawVal any
			if value.IsInt {
				rawVal = value.AsInt
			} else {
				rawVal = value.Value
			}

			switch v := rawVal.(type) {
			case *ArrayValue:
				vm.push(NewInt(len(v.Elements)))

			case ArrayValue:
				vm.push(NewInt(len(v.Elements)))

			case string:
				vm.push(NewInt(len([]rune(v))))

			case BufferValue:
				vm.push(NewInt(len(v.Bytes)))

			case *BufferValue:
				vm.push(NewInt(len(v.Bytes)))

			default:
				vm.fatalError(ErrorType, "cannot get length of %s", TypeName(value))
			}

		case OP_CALL:
			info := instr.Value.(CallInfo)

			if class, exists := vm.classes[info.Name]; exists {
				vm.callClass(class, info.ArgCount)
				break
			}

			vm.callFunction(info.Name, info.ArgCount)

			break

		case OP_CALL_VALUE:
			info := instr.Value.(CallInfo)

			args := vm.popArgs(info.ArgCount)

			callee := vm.popFast()

			switch v := callee.Value.(type) {
			case FunctionValue:
				result := vm.callFunctionValue(v, args)
				vm.push(result)

			case *FunctionValue:
				result := vm.callFunctionValue(*v, args)
				vm.push(result)

			case *HostFunctionValue:
				if result, ok := vm.callHostFunctionValue(v, args); ok {
					vm.push(result)
				}

			case *CallbackFunctionValue:
				result, err := v.Callback(args)
				if err != nil {
					vm.fatalError(ErrorRuntime, "callback %s failed: %s", v.Name, err.Error())
				}
				vm.push(result)

			case Class:
				vm.callClassByName(v.Name, args)

			case *Class:
				vm.callClassByName(v.Name, args)

			default:
				vm.fatalError(ErrorType, "expected function or class, got %s", TypeName(callee))
			}

			break

		case OP_CALL_VALUE_SPREAD:
			var args []TinyValue
			if info, ok := instr.Value.(SpreadCallInfo); ok {
				args = vm.popCallArgs(len(info.SpreadArgs), info.SpreadArgs)
			} else {
				arrayValue := vm.popFast()

				array, ok := vm.valueAsArrayForRead(arrayValue)
				if !ok {
					vm.fatalError(ErrorType, "spread operator expects array, got %s", TypeName(arrayValue))
				}

				args = array.Elements
			}

			callee := vm.popFast()

			var result TinyValue
			switch v := callee.Value.(type) {
			case FunctionValue:
				result = vm.callFunctionValue(v, args)
			case *FunctionValue:
				result = vm.callFunctionValue(*v, args)
			case *HostFunctionValue:
				var ok bool
				result, ok = vm.callHostFunctionValue(v, args)
				if !ok {
					continue
				}
			case *CallbackFunctionValue:
				callbackResult, err := v.Callback(args)
				if err != nil {
					vm.fatalError(ErrorRuntime, "callback %s failed: %s", v.Name, err.Error())
				}
				result = callbackResult
			case Class:
				vm.callClassByName(v.Name, args)
				continue
			case *Class:
				vm.callClassByName(v.Name, args)
				continue
			default:
				vm.fatalError(ErrorType, "expected function in spread call, got %s", TypeName(callee))
			}

			vm.push(result)

		case OP_BUILTIN_CALL:
			info := instr.Value.(BuiltinCallInfo)
			vm.callBuiltin(info.Object, info.Method, info.ArgCount)

		case OP_ARRAY:
			info := instr.Value.(ArrayInfo)

			if vm.top < info.Count {
				vm.handleUnderflow()
			}

			elements := make([]TinyValue, info.Count)
			start := vm.top - info.Count

			copy(elements, vm.stack[start:vm.top])
			for i := start; i < vm.top; i++ {
				vm.stack[i] = TinyValue{}
			}
			vm.top = start

			vm.push(NewNative(&ArrayValue{Elements: elements}))

		case OP_ARRAY_INDEX_LOCAL_STORE:
			info := instr.Value.(ArrayIndexLocalStoreInfo)
			frame := vm.frames[len(vm.frames)-1]

			if info.ArraySlot < 0 || info.ArraySlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "array local slot out of range in OP_ARRAY_INDEX_LOCAL_STORE: %d", info.ArraySlot)
			}
			if info.IndexSlot < 0 || info.IndexSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "index local slot out of range in OP_ARRAY_INDEX_LOCAL_STORE: %d", info.IndexSlot)
			}
			if info.DestSlot < 0 || info.DestSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "destination local slot out of range in OP_ARRAY_INDEX_LOCAL_STORE: %d", info.DestSlot)
			}

			arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_INDEX_LOCAL_STORE")
			indexValue := frameLocalValue(frame, info.IndexSlot, "OP_ARRAY_INDEX_LOCAL_STORE")
			result := vm.getIndexValue(arrayValue, indexValue)

			cell := frame.locals[info.DestSlot]
			if cell == nil {
				cell = &Cell{}
				frame.locals[info.DestSlot] = cell
			}
			setCellValue(cell, result)

		case OP_INDEX:
			indexValue := vm.popFast()
			objectValue := vm.popFast()

			if !objectValue.IsInt {
				if obj, ok := objectValue.Value.(*ArrayValue); ok {
					var index int
					if indexValue.IsInt {
						index = indexValue.AsInt
					} else {
						var ok bool
						index, ok = fastIndexInt(indexValue)
						if !ok {
							index = vm.asInt(indexValue)
						}
					}

					if index < 0 || index >= len(obj.Elements) {
						vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
						break
					}

					vm.push(obj.Elements[index])
					break
				}

				if obj, ok := objectValue.Value.(WasmArrayValue); ok {
					var index int
					if indexValue.IsInt {
						index = indexValue.AsInt
					} else {
						var ok bool
						index, ok = fastIndexInt(indexValue)
						if !ok {
							index = vm.asInt(indexValue)
						}
					}

					length := int(vm.ReadWasmFloat(uint32(obj.Address) + 8))
					if index < 0 || index >= length {
						vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
						break
					}

					elemPtr := uint32(vm.ReadWasmFloat(uint32(obj.Address) + 16))
					addr := elemPtr + uint32(index*16)
					tag := vm.ReadWasmFloat(addr)
					val := vm.ReadWasmFloat(addr + 8)

					switch tag {
					case 1.0:
						vm.push(NewNative(val))
					case 2.0:
						vm.push(NewNative(val != 0.0))
					case 4.0:
						vm.push(NewNative(WasmObjectValue{Address: val, VM: vm}))
					case 5.0:
						vm.push(NewNative(WasmArrayValue{Address: val, VM: vm}))
					case 6.0:
						strVal, ok := vm.readWasmStringMaybe(uint32(val))
						if ok {
							vm.push(NewNative(strVal))
						} else {
							vm.push(NewNull())
						}
					default:
						vm.push(NewNull())
					}
					break
				}

				if obj, ok := objectValue.Value.(ArrayValue); ok {
					var index int
					if indexValue.IsInt {
						index = indexValue.AsInt
					} else {
						var ok bool
						index, ok = fastIndexInt(indexValue)
						if !ok {
							index = vm.asInt(indexValue)
						}
					}

					if index < 0 || index >= len(obj.Elements) {
						vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
						break
					}

					vm.push(obj.Elements[index])
					break
				}

				if obj, ok := vm.valueAsObjectForRead(objectValue); ok {
					var key string
					if indexValue.IsInt {
						key = intToString(indexValue.AsInt)
					} else {
						key = valueToString(indexValue)
					}

					value, exists := obj[key]
					if !exists {
						vm.push(NewNull())
						break
					}

					vm.push(value)
					break
				}
			}

			vm.fatalError(ErrorType, "cannot index %s", TypeName(objectValue))

		case OP_SET_INDEX:
			value := vm.popFast()
			indexValue := vm.popFast()
			objectValue := vm.popFast()

			if !objectValue.IsInt {
				if obj, ok := objectValue.Value.(*ArrayValue); ok {
					var index int
					if indexValue.IsInt {
						index = indexValue.AsInt
					} else {
						var ok bool
						index, ok = fastIndexInt(indexValue)
						if !ok {
							index = vm.asInt(indexValue)
						}
					}

					if index < 0 || index >= len(obj.Elements) {
						vm.fatalError(ErrorRuntime, "array index out of range: %d", index)
					}

					obj.Elements[index] = value
					vm.invalidateJitArrayMirror(obj)
					break
				}

				if obj, ok := objectValue.Value.(WasmArrayValue); ok {
					var index int
					if indexValue.IsInt {
						index = indexValue.AsInt
					} else {
						var ok bool
						index, ok = fastIndexInt(indexValue)
						if !ok {
							index = vm.asInt(indexValue)
						}
					}

					length := int(vm.ReadWasmFloat(uint32(obj.Address) + 8))
					if index < 0 || index >= length {
						vm.fatalError(ErrorRuntime, "array index out of range: %d", index)
					}

					elemPtr := uint32(vm.ReadWasmFloat(uint32(obj.Address) + 16))
					addr := elemPtr + uint32(index*16)
					vm.WriteWasmTaggedValue(addr, value)
					break
				}

				if inst, ok := instanceValue(objectValue); ok {
					vm.runtimeError(ErrorRuntime, "cannot modify class '%s' by index operator.", inst.ClassName)
					break
				}

				if obj, ok := vm.valueAsObjectForRead(objectValue); ok {
					var key string
					if indexValue.IsInt {
						key = intToString(indexValue.AsInt)
					} else {
						key = valueToString(indexValue)
					}

					obj[key] = value
					vm.invalidateJitObjectMirror(obj)
					break
				}
			}

			vm.fatalError(ErrorType, "cannot index assign %s", TypeName(objectValue))

		case OP_RETURN:
			var returnValue TinyValue

			if vm.top == 0 {
				returnValue = NewNull()
			} else {
				returnValue = vm.popFast()
			}

			if len(vm.frames) == 0 {
				vm.push(returnValue)
				return true // Halt
			}

			// Fast path: no defers, no return type hint, no override
			frame := vm.frames[len(vm.frames)-1]
			if len(vm.deferHandlers) == 0 && frame.function.ReturnType.IsEmpty() && !frame.hasReturnOverride {
				vm.frames = vm.frames[:len(vm.frames)-1]
				vm.releaseFrame(frame)
				vm.push(returnValue)
				break
			}

			if len(vm.deferHandlers) > 0 {
				vm.runDefersAboveDepth(len(vm.frames) - 1)
			}

			vm.frames = vm.frames[:len(vm.frames)-1]

			if !frame.function.ReturnType.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(frame.function, returnValue, frame.function.ReturnType); !ok {
					vm.fatalError(
						ErrorType,
						"function %s should return %s, got %s%s",
						frame.function.Name,
						frame.function.ReturnType.Name,
						TypeName(returnValue),
						reason,
					)
				}
			}

			vm.releaseFrame(frame)

			if frame.hasReturnOverride {
				vm.push(frame.returnOverride)
			} else {
				vm.push(returnValue)
			}

		case OP_THROW:
			value := vm.popFast()
			vm.throwValue(value)

		case OP_POP:
			vm.popFast()

		case OP_INTERPOLATE:
			info := instr.Value.(InterpolateInfo)

			if info.ExprCount == 1 {
				value := vm.popFast()
				vm.push(NewNative(info.Parts[0] + valueToString(value) + info.Parts[1]))
				break
			}

			if vm.top < info.ExprCount {
				vm.handleUnderflow()
			}

			start := vm.top - info.ExprCount

			var builder strings.Builder

			for i := 0; i < info.ExprCount; i++ {
				builder.WriteString(info.Parts[i])
				builder.WriteString(valueToString(vm.stack[start+i]))
				vm.stack[start+i] = TinyValue{}
			}

			vm.top = start
			builder.WriteString(info.Parts[len(info.Parts)-1])

			vm.push(NewNative(builder.String()))

		case OP_OBJECT:
			info := instr.Value.(ObjectInfo)

			if vm.top < len(info.Names) {
				vm.handleUnderflow()
			}

			object := make(ObjectValue, len(info.Names))
			start := vm.top - len(info.Names)

			for i, fieldInfo := range info.Names {
				if fieldInfo.Copy {
					obj, ok := vm.valueAsObjectForRead(vm.stack[start+i])

					if !ok {
						vm.fatalError(ErrorType, "expected an object to copy with {...%s}, but got %s", fieldInfo.Name, TypeName(vm.stack[start+i]))
					}

					maps.Copy(object, obj)
				} else {
					object[fieldInfo.Name] = vm.stack[start+i]
				}
				vm.stack[start+i] = TinyValue{}
			}
			vm.top = start

			vm.push(NewNative(object))

		case OP_NOT:
			value := vm.popFast()
			vm.push(NewNative(!isTruthy(value)))

		case OP_NOT_BIT:
			value := vm.popFast()
			vm.push(NewInt(^vm.bitwiseInt(value)))

		case OP_GET_PROPERTY_LOCAL:
			info := instr.Value.(PropertyLocalInfo)
			frame := vm.frames[len(vm.frames)-1]
			vm.push(propertyValue(vm, frameLocalValue(frame, info.Slot, "OP_GET_PROPERTY_LOCAL"), info.Name))

		case OP_JUMP_PROPERTY_LOCAL_FALSE:
			info := instr.Value.(JumpPropertyLocalInfo)
			if len(vm.frames) == 0 {
				vm.fatalError(ErrorInternal, "OP_JUMP_PROPERTY_LOCAL_FALSE used outside function frame")
			}
			frame := vm.frames[len(vm.frames)-1]
			value := vm.getObjectLocalPropertyFast(frame, info.Slot, info.Name, "OP_JUMP_PROPERTY_LOCAL_FALSE")
			if !isTruthy(value) {
				frame.ip = info.Target
			}

		case OP_JUMP_PROPERTY_LOCAL_TRUE:
			info := instr.Value.(JumpPropertyLocalInfo)
			if len(vm.frames) == 0 {
				vm.fatalError(ErrorInternal, "OP_JUMP_PROPERTY_LOCAL_TRUE used outside function frame")
			}
			frame := vm.frames[len(vm.frames)-1]
			value := vm.getObjectLocalPropertyFast(frame, info.Slot, info.Name, "OP_JUMP_PROPERTY_LOCAL_TRUE")
			if isTruthy(value) {
				frame.ip = info.Target
			}

		case OP_ADD_PROPERTY_LOCAL_LOCAL:
			info := instr.Value.(PropertyLocalAssignInfo)
			frame := vm.frames[len(vm.frames)-1]
			objectValue := frameLocalValue(frame, info.ObjectSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")

			if obj, ok := objectValue.Value.(WasmObjectValue); ok {
				current := propertyValue(vm, objectValue, info.Name)
				source := frameLocalValue(frame, info.SourceSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
				newValue := vm.addValues(current, source)

				offset := vm.getPropertyOffset(info.Name)
				addr := uint32(obj.Address) + offset

				tag := 1.0
				val := 0.0

				if newValue.IsInt {
					tag = 1.0
					val = float64(newValue.AsInt)
				} else {
					switch v := newValue.Value.(type) {
					case float64:
						tag = 1.0
						val = v
					case bool:
						tag = 2.0
						if v {
							val = 1.0
						} else {
							val = 0.0
						}
					case WasmObjectValue:
						tag = 4.0
						val = v.Address
					case WasmArrayValue:
						tag = 5.0
						val = v.Address
					case NullValue:
						tag = 0.0
						val = 0.0
					default:
						vm.fatalError(ErrorType, "cannot store %s in JIT object", TypeName(newValue))
					}
				}

				vm.WriteWasmFloat(addr, tag)
				vm.WriteWasmFloat(addr+8, val)
				break
			}

			if inst, ok := instanceValue(objectValue); ok {
				if _, isConstant := inst.ConstFields[info.Name]; isConstant {
					vm.fatalError(ErrorRuntime, "cannot assign to constant field: %s", info.Name)
				}
				if !vm.canAccessField(inst, info.Name) {
					vm.fatalError(ErrorRuntime, "cannot assign private field: %s", info.Name)
				}

				current, exists := inst.Fields[info.Name]
				if !exists {
					vm.fatalError(ErrorName, "object has no property: %s", info.Name)
				}
				source := frameLocalValue(frame, info.SourceSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
				if l, ok := fastNumericValue(current); ok {
					if r, ok := fastNumericValue(source); ok {
						inst.Fields[info.Name] = NewNative(l + r)
						break
					}
				}
				inst.Fields[info.Name] = vm.addValues(current, source)
				break
			}

			object, ok := vm.valueAsObjectForRead(objectValue)
			if !ok {
				vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
			}

			current, exists := object[info.Name]
			if !exists {
				vm.fatalError(ErrorName, "object has no property: %s", info.Name)
			}
			source := frameLocalValue(frame, info.SourceSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
			if l, ok := fastNumericValue(current); ok {
				if r, ok := fastNumericValue(source); ok {
					object[info.Name] = NewNative(l + r)
					break
				}
			}
			object[info.Name] = vm.addValues(current, source)

		case OP_ADD_PROPERTY_LOCAL_CONST:
			info := instr.Value.(PropertyLocalConstAssignInfo)
			frame := vm.frames[len(vm.frames)-1]
			current := vm.getObjectLocalPropertyFast(frame, info.ObjectSlot, info.Name, "OP_ADD_PROPERTY_LOCAL_CONST")
			newValue := vm.applyBinaryOp(current, constToTinyValue(info.Const), info.Op)
			vm.setObjectLocalPropertyFast(frame, info.ObjectSlot, info.Name, newValue, "OP_ADD_PROPERTY_LOCAL_CONST")

		case OP_ADD_PROPERTY_LOCAL_PROPERTY:
			info := instr.Value.(PropertyLocalPropertyAssignInfo)
			frame := vm.frames[len(vm.frames)-1]
			current := vm.getObjectLocalPropertyFast(frame, info.ObjectSlot, info.Name, "OP_ADD_PROPERTY_LOCAL_PROPERTY")
			source := vm.getObjectLocalPropertyFast(frame, info.ObjectSlot, info.SourceName, "OP_ADD_PROPERTY_LOCAL_PROPERTY")
			newValue := vm.applyBinaryOp(current, source, info.Op)
			vm.setObjectLocalPropertyFast(frame, info.ObjectSlot, info.Name, newValue, "OP_ADD_PROPERTY_LOCAL_PROPERTY")

		case OP_ADD_LOCAL_PROPERTIES_STORE:
			info := instr.Value.(AddLocalPropertiesStoreInfo)
			frame := vm.frames[len(vm.frames)-1]
			if info.LocalSlot < 0 || info.LocalSlot >= len(frame.locals) {
				vm.fatalError(ErrorInternal, "local slot out of range in OP_ADD_LOCAL_PROPERTIES_STORE")
			}
			if frame.locals[info.LocalSlot].Constant {
				vm.fatalError(ErrorConst, "cannot assign to constant local")
			}
			cell := frame.locals[info.LocalSlot]
			if cell == nil {
				vm.fatalError(ErrorInternal, "nil local cell in OP_ADD_LOCAL_PROPERTIES_STORE")
			}

			result := cellValue(cell)
			for _, name := range info.Names {
				value := vm.getObjectLocalPropertyFast(frame, info.ObjectSlot, name, "OP_ADD_LOCAL_PROPERTIES_STORE")
				result = vm.applyBinaryOp(result, value, OP_ADD)
			}
			setCellValue(cell, result)

		case OP_GET_PROPERTY:
			name := instr.Value.(string)
			objectValue := vm.popFast()
			vm.push(vm.getProperty(objectValue, name, false))

		case OP_HALT:
			saveState()
			return true

		default:
			vm.fatalError(ErrorInternal, "unknown opcode: %d", instr.Op)
		}

	}
	return false
}

func writeServerResponse(w http.ResponseWriter, r *http.Request, response NativeHttpResponseValue) {
	status := response.Status
	if status == 0 {
		status = http.StatusOK
	}

	for key, value := range response.Headers {
		w.Header().Set(valueToString(ToValue(key)), valueToString(value))
	}

	switch response.Type {
	case HttpJson:
		w.Header().Set("Content-Type", "application/json")
		jsonStr := stringifyTinyJSONFast(ToValue(response.Value))
		w.WriteHeader(status)
		io.WriteString(w, jsonStr)

	case HttpText:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		stringValue := valueToString(response.Value)
		trimmed := strings.TrimSpace(stringValue)
		w.WriteHeader(status)
		fmt.Fprint(w, trimmed)

	case HttpHtml:
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		fmt.Fprint(w, valueToString(response.Value))

	case HttpResponse:
		w.WriteHeader(status)
		fmt.Fprint(w, valueToString(response.Value))

	case HttpRedirect:
		redirectStatus := status
		if redirectStatus == http.StatusOK {
			redirectStatus = http.StatusFound
		}
		http.Redirect(w, r, response.RedirectURL, redirectStatus)

	case HttpNoContent:
		w.WriteHeader(http.StatusNoContent)

	case HttpFile:
		http.ServeFile(w, r, response.Path)

	case HttpDownload:
		name := response.DownloadName
		if name == "" {
			parts := strings.Split(strings.ReplaceAll(response.Path, "\\", "/"), "/")
			name = parts[len(parts)-1]
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", name))
		http.ServeFile(w, r, response.Path)
	}
}

func (vm *VM) callNamespaceMethod(ns NamespaceValue, method string, args []TinyValue) {
	value, exists := ns.Members[method]
	if !exists {
		vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, method)
	}

	value = resolveNamespaceValue(vm, value)

	var rawVal any
	if value.IsInt {
		rawVal = value.AsInt
	} else {
		rawVal = value.Value
	}

	switch v := rawVal.(type) {
	case FunctionValue:
		result := vm.callFunctionValue(v, args)
		vm.push(result)

	case *FunctionValue:
		result := vm.callFunctionValue(*v, args)
		vm.push(result)

	case *HostFunctionValue:
		if result, ok := vm.callHostFunctionValue(v, args); ok {
			vm.push(result)
		}

	case *CallbackFunctionValue:
		result, err := v.Callback(args)
		if err != nil {
			vm.fatalError(ErrorRuntime, "callback %s failed: %s", v.Name, err.Error())
		}
		vm.push(result)

	case Class:
		vm.callClassByName(v.Name, args)

	case *Class:
		vm.callClassByName(v.Name, args)

	default:
		vm.fatalError(ErrorType, "namespace member %s is not callable: %T", method, rawVal)
	}
}

func (vm *VM) callZeroArgNativeMethod(method string, objectValue TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *StandardModuleValue:
		if value.Name == "time" {
			switch method {
			case "nowMs":
				vm.push(NewNative(float64(time.Now().UnixMilli())))
				return true
			case "nowNs":
				vm.push(NewNative(float64(time.Now().UnixNano())))
				return true
			case "nowSec":
				vm.push(NewNative(float64(time.Now().Unix())))
				return true
			}
		}
	case *ArrayValue:
		vm.callArrayMethod(value, method, []TinyValue{})
		return true
	case string:
		vm.callStringMethod(value, method, []TinyValue{})
		return true
	case *NativeMutexValue:
		vm.callNativeMutexMethod(value, method, []TinyValue{})
		return true
	}
	return false
}

func (vm *VM) callOneArgNativeMethod(method string, objectValue TinyValue, arg TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *ArrayValue:
		vm.callArrayMethod(value, method, []TinyValue{arg})
		return true
	case string:
		vm.callStringMethod(value, method, []TinyValue{arg})
		return true
	}
	return false
}

func (vm *VM) callTwoArgNativeMethod(method string, objectValue TinyValue, arg0 TinyValue, arg1 TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *ArrayValue:
		vm.callArrayMethod(value, method, []TinyValue{arg0, arg1})
		return true
	case string:
		vm.callStringMethod(value, method, []TinyValue{arg0, arg1})
		return true
	}
	return false
}

func (vm *VM) callStdObjectFast1(method string, objectValue TinyValue, arg0 TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "length" {
		return false
	}

	var rawArg any
	if arg0.IsInt {
		rawArg = arg0.AsInt
	} else {
		rawArg = arg0.Value
	}

	obj, ok := vm.valueAsObjectForRead(ToValue(rawArg))
	if !ok {
		vm.fatalError(ErrorType, "object.length argument 1 expected object, got %s", TypeName(arg0))
		return true
	}
	vm.push(NewInt(len(obj)))
	return true
}

func (vm *VM) callStdObjectFast2(method string, objectValue TinyValue, arg0 TinyValue, arg1 TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "get" {
		return false
	}

	var rawArg0 any
	if arg0.IsInt {
		rawArg0 = arg0.AsInt
	} else {
		rawArg0 = arg0.Value
	}

	obj, ok := vm.valueAsObjectForRead(ToValue(rawArg0))
	if !ok {
		vm.fatalError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(arg0))
		return true
	}

	var rawArg1 any
	if arg1.IsInt {
		rawArg1 = arg1.AsInt
	} else {
		rawArg1 = arg1.Value
	}

	key, ok := rawArg1.(string)
	if !ok {
		vm.fatalError(ErrorType, "object.get argument 2 expected string, got %s", TypeName(arg1))
		return true
	}
	if val, ok := obj[key]; ok {
		vm.push(val)
	} else {
		vm.push(NewNull())
	}
	return true
}

func (vm *VM) callStdObjectFast3(method string, objectValue TinyValue, arg0 TinyValue, arg1 TinyValue, arg2 TinyValue) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "set" {
		return false
	}

	obj, ok := vm.valueAsObjectForWrite(arg0)
	if !ok {
		vm.fatalError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(arg0))
		return true
	}

	var rawArg1 any
	if arg1.IsInt {
		rawArg1 = arg1.AsInt
	} else {
		rawArg1 = arg1.Value
	}

	key, ok := rawArg1.(string)
	if !ok {
		vm.fatalError(ErrorType, "object.set argument 2 expected string, got %s", TypeName(arg1))
		return true
	}

	obj.set(key, arg2)

	vm.push(NewNull())
	return true
}

func (vm *VM) callMethodFast(method string, argCount int) {
	switch argCount {
	case 0:
		objectValue := vm.popFast()
		if vm.callZeroArgNativeMethod(method, objectValue) {
			return
		}
		vm.callMethodResolved(method, objectValue, nil)
		return

	case 1:
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast1(method, objectValue, arg0) {
			return
		}
		if vm.callOneArgNativeMethod(method, objectValue, arg0) {
			return
		}
		vm.callMethodResolved(method, objectValue, []TinyValue{arg0})
		return

	case 2:
		arg1 := vm.popFast()
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast2(method, objectValue, arg0, arg1) {
			return
		}
		if vm.callTwoArgNativeMethod(method, objectValue, arg0, arg1) {
			return
		}
		vm.callMethodResolved(method, objectValue, []TinyValue{arg0, arg1})
		return

	case 3:
		arg2 := vm.popFast()
		arg1 := vm.popFast()
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast3(method, objectValue, arg0, arg1, arg2) {
			return
		}
		vm.callMethodResolved(method, objectValue, []TinyValue{arg0, arg1, arg2})
		return

	default:
		args := vm.popArgs(argCount)
		objectValue := vm.popFast()
		vm.callMethodResolved(method, objectValue, args)
		return
	}
}

func (vm *VM) callMethodResolved(method string, objectValue TinyValue, args []TinyValue) {
	if method == "toString" {
		if obj, ok := vm.valueAsObjectForRead(objectValue); ok {
			if _, ok := obj["toString"]; !ok {
				vm.push(NewNative(valueToString(objectValue)))
				return
			}
		} else {
			vm.push(NewNative(valueToString(objectValue)))
			return
		}
	}

	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch val := rawVal.(type) {
	case NamespaceValue:
		vm.callNamespaceMethod(val, method, args)
		return

	case *NamespaceValue:
		vm.callNamespaceMethod(*val, method, args)
		return

	case *NativePluginValue:
		popNative := vm.pushNativeFrame("plugin." + method)
		defer popNative()

		vm.callNativePlugin(val, method, args)
		return

	case *NativeAppValue:
		vm.callNativeAppMethod(val, method, args)
		return

	case *NativeWebViewValue:
		vm.callNativeWebviewMethod(val, method, args)
		return

	case *NativeTrayValue:
		vm.callNativeTrayMethod(val, method, args)
		return

	case *StandardModuleValue:
		popNative := vm.pushNativeFrame(val.Name + "." + method)
		defer popNative()

		vm.callStandardModule(val.Name, method, args)
		return

	case *NativeServerValue:
		vm.callServerMethod(val, method, args)
		return

	case *NativeSqliteValue:
		vm.callSqliteMethod(val, method, args)
		return

	case *NativeTcpServerValue:
		vm.callTcpServerMethod(val, method, args)
		return

	case *NativeTcpConnectionValue:
		vm.callTcpConnMethod(val, method, args)
		return

	case *NativeWebsocketServerValue:
		vm.callNativeWebsocketServerMethod(val, method, args)
		return

	case *NativeWebsocketConnValue:
		vm.callNativeWebsocketConnMethod(val, method, args)
		return

	case *NativeMutexValue:
		vm.callNativeMutexMethod(val, method, args)
		return

	case *NativeValidateTop:
		vm.callNativeSchemaTopMethod(val, method, args)
		return

	case *NativeValidateType:
		vm.callNativeSchemaTypeMethod(val, method, args)
		return

	case *BufferValue:
		vm.callBufferMethod(val, method, args)
		return

	case *NativeFileValue:
		vm.callFileMethod(val, method, args)
		return

	case *NativeTimerValue:
		vm.callNativeTimerMethod(val, method, args)
		return

	case *ArrayValue:
		vm.callArrayMethod(val, method, args)
		return

	case *NativeProcessValue:
		vm.callProcessMethod(val, method, args)
		return

	case *NativeVMValue:
		vm.callRuntimeVMMethod(val, method, args)
		return

	case *NativeWasmModuleValue:
		vm.callNativeWasmMethod(val, method, args)
		return

	case *NativeStringBuilderValue:
		vm.callStringBuilderMethod(val, method, args)
		return

	case string:
		vm.callStringMethod(val, method, args)
		return

	case WasmArrayValue:
		vm.callWasmArrayMethod(val, method, args)
		return

	case WasmObjectValue:
		valObj := vm.getProperty(NewNative(val), method, false)
		if isNullish(valObj) {
			vm.fatalError(ErrorName, "object has no method: %s", method)
		}
		fnVal, ok := valObj.Value.(FunctionValue)
		if !ok {
			vm.fatalError(ErrorType, "property %s is not a function", method)
		}
		result := vm.callFunctionValue(fnVal, args)
		vm.push(result)
		return
	}

	objectValueForRead := ToValue(rawVal)
	object, ok := vm.valueAsObjectForRead(objectValueForRead)
	if !ok {
		vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
	}

	receiverValue := objectValueForRead
	receiverInstance, receiverIsInstance := instanceValue(objectValueForRead)

	methodValue, exists := object[method]

	var fnValue FunctionValue

	if exists {
		if hostFn, ok := methodValue.Value.(*HostFunctionValue); ok {
			if result, ok := vm.callHostFunctionValue(hostFn, args); ok {
				vm.push(result)
			}
			return
		}

		var ok bool
		fnValue, ok = methodValue.Value.(FunctionValue)
		if !ok {
			vm.fatalError(ErrorType, "property %s is not callable", method)
		}
	} else {
		if receiverIsInstance {
			embeddedReceiver, embeddedFn, ok := vm.findEmbeddedMethodInstance(receiverInstance, method)
			if !ok {
				vm.fatalError(ErrorName, "object has no method: %s", method)
			}

			receiverValue = NewNative(embeddedReceiver)
			receiverInstance = embeddedReceiver
			receiverIsInstance = true
			fnValue = embeddedFn
		} else {
			vm.fatalError(ErrorName, "object has no method: %s", method)
		}
	}

	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		vm.fatalError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	ownerClass := methodOwnerClass(fnValue.Name)
	if ownerClass == "" && receiverIsInstance {
		ownerClass = receiverInstance.ClassName
	}

	if receiverIsInstance && !vm.canAccessMethod(receiverInstance, method) {
		vm.fatalError(ErrorRuntime, "cannot access private method %s in class %s", method, ownerClass)
	}

	paramOffset := 0
	if len(fn.Params) > 0 && fn.Params[0].Name == "this" {
		paramOffset = 1
	}

	userParamCount := len(fn.Params) - paramOffset
	isVariadic := userParamCount > 0 && fn.Params[len(fn.Params)-1].Variadic

	if isVariadic {
		minArgs := userParamCount - 1

		if len(args) < minArgs {
			vm.fatalError(
				ErrorRuntime,
				"method %s expects at least %d arguments, got %d",
				method,
				minArgs,
				len(args),
			)
		}
	} else {
		if fn.HasDefaults {
			args = vm.applyDefaultArgs(fn, args, paramOffset, "method "+method)
		} else if len(args) != userParamCount {
			vm.fatalError(
				ErrorRuntime,
				"method %s expects %d arguments, got %d",
				method,
				userParamCount,
				len(args),
			)
		}
	}

	frame := vm.getFrame(fn)
	frame.methodClass = ownerClass

	setCellValue(frame.locals[0], receiverValue)
	frame.locals[0].Constant = true

	if isVariadic {
		fixedCount := userParamCount - 1

		for i := range fixedCount {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]
			arg := args[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"method %s parameter %s expected %s, got %s%s",
						method,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.locals[paramIndex].Constant = false
			frame.locals[paramIndex].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
		}

		restSlot := paramOffset + fixedCount
		restParam := fn.Params[restSlot]

		rest := &ArrayValue{
			Elements: make([]TinyValue, 0, len(args)-fixedCount),
		}

		for i := fixedCount; i < len(args); i++ {
			arg := args[i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, restParam.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"method %s rest parameter %s expected %s, got %s%s",
						method,
						restParam.Name,
						restParam.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			rest.Elements = append(rest.Elements, arg)
		}

		setCellValue(frame.locals[restSlot], NewNative(rest))
		frame.locals[restSlot].Constant = false
		frame.locals[restSlot].TypeHint = TypeHint{
			Name: "array",
		}
	} else {
		for i, arg := range args {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
					vm.runtimeError(
						ErrorType,
						"method %s parameter %s expected %s, got %s%s",
						method,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.locals[paramIndex].Constant = false
			frame.locals[paramIndex].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
		}
	}

	if fn.Async {
		task := &NativeTaskValue{
			Done: make(chan TaskResult, 1),
		}

		taskVM := vm.CloneForTask()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					task.Done <- TaskResult{Error: r}
				}
			}()

			result := taskVM.runFrameToCompletion(frame)

			task.Done <- TaskResult{
				Value: result,
			}
		}()

		vm.push(NewNative(task))

		return
	}

	vm.pushFrame(frame)
}

func (vm *VM) callMethod(method string, argCount int) {
	vm.callMethodFast(method, argCount)
}

type MethodCallCache struct {
	Method       string
	ArgCount     int
	SpreadArgs   []bool
	CachedFunc   StdModuleFunc
	CachedModule string
}

func (vm *VM) resolveStdModuleFunc(module string, method string) StdModuleFunc {
	if methods, ok := stdModuleMethods[module]; ok {
		return methods[method]
	}
	return nil
}

func (vm *VM) runNativeApp(app *NativeAppValue) {
	if len(vm.cliArgs) == 0 {
		fmt.Println("Available commands:")

		for name := range app.Commands {
			fmt.Println("  " + name)
		}

		return
	}

	commandName := vm.cliArgs[0]
	commandArgs := vm.cliArgs[1:]

	fn, exists := app.Commands[commandName]
	if !exists {
		vm.fatalError(ErrorRuntime, "unknown command: %s", commandName)
	}

	tinyArgs := &ArrayValue{
		Elements: make([]TinyValue, len(commandArgs)),
	}

	for i, arg := range commandArgs {
		tinyArgs.Elements[i] = NewNative(arg)
	}

	vm.callFunctionValue(fn, []TinyValue{NewNative(tinyArgs)})
}

func (vm *VM) setIP(value int) {
	if len(vm.frames) == 0 {
		vm.ip = value
		return
	}

	vm.frames[len(vm.frames)-1].ip = value
}

func (vm *VM) callFunction(name string, argCount int) {
	if vm.observerStats != nil {
		vm.observerStats.FunctionCalled(name)
	}

	fn, exists := vm.functions[name]
	if !exists {
		vm.fatalError(ErrorName, "undefined function: %s", name)
	}

	args := vm.popArgs(argCount)

	args = vm.applyDefaultArgs(fn, args, 0, "function "+fn.Name)

	frame := vm.getFrame(fn)

	for i, arg := range args {
		param := fn.Params[i]

		if !param.TypeHint.IsEmpty() {
			if ok, reason := vm.checkFunctionTypeHint(fn, arg, param.TypeHint); !ok {
				vm.runtimeError(
					ErrorType,
					"function %s parameter %s expected %s, got %s%s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
					reason,
				)
			}
		}

		setCellValue(frame.locals[i], arg)
		frame.locals[i].Constant = false
		frame.locals[i].TypeHint = eraseGenericTypeHintForRuntime(param.TypeHint, vm.genericTypeParamsForFunction(fn))
	}

	vm.pushFrame(frame)
}

func (vm *VM) callClass(class Class, argCount int) {
	args := vm.popArgs(argCount)
	vm.callClassWithArgs(class, args)
}

func (vm *VM) pushNativeFrame(name string) func() {
	var dbg DebugInfo
	if len(vm.frames) == 0 {
		dbg = vm.mainDebugInfoAt(vm.ip - 1)
	} else {
		frame := vm.frames[len(vm.frames)-1]
		dbg = frameDebugInfo(frame, frame.ip-1)
	}

	vm.nativeFrames = append(vm.nativeFrames, NativeCallFrame{
		Name:   name,
		File:   dbg.File,
		Line:   dbg.Line,
		Column: dbg.Column,
	})

	return func() {
		if len(vm.nativeFrames) > 0 {
			vm.nativeFrames = vm.nativeFrames[:len(vm.nativeFrames)-1]
		}
	}
}

func (vm *VM) fetchInstruction() Instruction {
	if len(vm.frames) == 0 {
		ip := vm.ip
		instructions := vm.mainInstructions

		if ip < 0 || ip >= len(instructions) {
			vm.fatalError(ErrorInternal, "instruction pointer out of range: ip=%d len=%d", ip, len(instructions))
		}

		instr := instructions[ip]
		vm.ip = ip + 1
		return instr
	}

	frame := vm.frames[len(vm.frames)-1]
	ip := frame.ip
	instructions := frame.instructions

	if ip < 0 || ip >= len(instructions) {
		vm.fatalError(ErrorInternal, "instruction pointer out of range: ip=%d len=%d", ip, len(instructions))
	}

	instr := instructions[ip]
	frame.ip = ip + 1
	return instr
}

func (vm *VM) currentFrame() *Frame {
	if len(vm.frames) == 0 {
		vm.fatalError(ErrorInternal, "no current function frame")
	}

	return vm.frames[len(vm.frames)-1]
}

func (vm *VM) popArgs(count int) []TinyValue {
	if vm.top < count {
		vm.handleUnderflow()
	}

	args := make([]TinyValue, count)

	start := vm.top - count

	copy(args, vm.stack[start:vm.top])

	for i := start; i < vm.top; i++ {
		vm.stack[i] = TinyValue{}
	}

	vm.top = start

	return args
}

func (vm *VM) popCallArgs(count int, spreadArgs []bool) []TinyValue {
	values := vm.popArgs(count)
	if len(spreadArgs) == 0 {
		return values
	}
	if len(spreadArgs) != count {
		vm.fatalError(ErrorInternal, "spread argument metadata length mismatch: expected %d, got %d", count, len(spreadArgs))
	}

	args := make([]TinyValue, 0, len(values))
	for i, value := range values {
		if !spreadArgs[i] {
			args = append(args, value)
			continue
		}

		array, ok := vm.valueAsArrayForRead(value)
		if !ok {
			vm.fatalError(ErrorType, "spread operator expects array, got %s", TypeName(value))
		}

		args = append(args, array.Elements...)
	}

	return args
}

func (vm *VM) push(value TinyValue) {
	if vm.top == len(vm.stack) {
		newStack := make([]TinyValue, len(vm.stack)*2)
		copy(newStack, vm.stack)
		vm.stack = newStack
	}

	vm.stack[vm.top] = value
	vm.top++
}

func (vm *VM) pop() TinyValue {
	if vm.top == 0 {
		vm.handleUnderflow()
	}

	vm.top--
	val := vm.stack[vm.top]
	vm.stack[vm.top] = TinyValue{}

	return val
}

func (vm *VM) handleUnderflow() {
	lastFunctionName := "<main>"
	lastInstructionIndex := vm.ip - 1
	var lastInstruction Instruction

	if len(vm.frames) > 0 {
		frame := vm.frames[len(vm.frames)-1]
		lastFunctionName = frame.function.Name
		lastInstructionIndex = frame.ip - 1
		if lastInstructionIndex >= 0 && lastInstructionIndex < len(frame.instructions) {
			lastInstruction = frame.instructions[lastInstructionIndex]
		}
	} else if lastInstructionIndex >= 0 && lastInstructionIndex < len(vm.mainInstructions) {
		lastInstruction = vm.mainInstructions[lastInstructionIndex]
	}

	vm.fatalError(
		ErrorInternal,
		"stack underflow at function=%s ip=%d op=%v value=%#v",
		lastFunctionName,
		lastInstructionIndex,
		lastInstruction.Op.String(),
		lastInstruction.Value,
	)
}

func (vm *VM) popFast() TinyValue {
	if vm.top == 0 {
		vm.handleUnderflow()
	}

	vm.top--
	val := vm.stack[vm.top]
	return val
}

func (vm *VM) executeNativeWasmCall(fn api.Function, args []TinyValue, returnType string) {
	if vm.wasmMu != nil {
		vm.wasmMu.Lock()
		defer vm.wasmMu.Unlock()
	}

	expectedArgsParams := 0
	for _, arg := range args {
		var rawVal any
		if arg.IsInt {
			rawVal = arg.AsInt
		} else {
			rawVal = arg.Value
		}

		if _, ok := rawVal.(string); ok {
			expectedArgsParams += 2
		} else if _, ok := rawVal.(*ArrayValue); ok {
			expectedArgsParams += 3
		} else {
			expectedArgsParams += 1
		}
	}

	returnsString := returnType == "string"
	returnsArray := returnType == "array"
	returnsPointer := returnsString || returnsArray

	wasmParams := []uint64{}
	var allocatedPtrs []uint64

	for _, arg := range args {
		var rawVal any
		if arg.IsInt {
			rawVal = arg.AsInt
		} else {
			rawVal = arg.Value
		}

		switch v := rawVal.(type) {
		case float64:
			wasmParams = append(wasmParams, api.EncodeF64(v))
		case float32:
			wasmParams = append(wasmParams, api.EncodeF64(float64(v)))
		case int:
			wasmParams = append(wasmParams, api.EncodeF64(float64(v)))
		case int64:
			wasmParams = append(wasmParams, api.EncodeF64(float64(v)))
		case bool:
			val := uint32(0)
			if v {
				val = 1
			}
			wasmParams = append(wasmParams, api.EncodeU32(val))
		case *ArrayValue:
			floats := make([]float64, len(v.Elements))
			for idx, el := range v.Elements {
				var num float64
				if el.IsInt {
					num = float64(el.AsInt)
				} else if f, ok := el.Value.(float64); ok {
					num = f
				} else if b, ok := el.Value.(bool); ok {
					if b {
						num = 1
					} else {
						num = 0
					}
				} else {
					vm.runtimeError(ErrorType, "invalid element type in native array at index %d: only numbers or booleans are allowed", idx)
				}
				floats[idx] = num
			}

			malloc := vm.wasmModule.ExportedFunction("malloc")
			if malloc == nil {
				vm.fatalError(ErrorRuntime, "native arrays are not supported because 'malloc' is not exported")
			}

			size := uint64(len(floats) * 8)
			results, err := malloc.Call(vm.wazeroCtx, size)
			if err != nil || len(results) == 0 {
				vm.fatalError(ErrorRuntime, "failed to allocate Wasm memory for array: %v", err)
			}
			ptr := results[0]

			allocatedPtrs = append(allocatedPtrs, ptr)

			if len(floats) > 0 {
				buf := make([]byte, size)
				for idx, f := range floats {
					bits := math.Float64bits(f)
					binary.LittleEndian.PutUint64(buf[idx*8:(idx+1)*8], bits)
				}

				ok := vm.wasmModule.Memory().Write(uint32(ptr), buf)
				if !ok {
					vm.fatalError(ErrorRuntime, "failed to write array to Wasm memory")
				}
			}

			wasmParams = append(wasmParams, api.EncodeU32(uint32(ptr)))
			wasmParams = append(wasmParams, api.EncodeU32(uint32(len(floats))))
			wasmParams = append(wasmParams, api.EncodeU32(uint32(len(floats))))
		case string:
			malloc := vm.wasmModule.ExportedFunction("malloc")
			if malloc == nil {
				vm.fatalError(ErrorRuntime, "native strings are not supported because 'malloc' is not exported")
			}

			size := uint64(len(v))
			results, err := malloc.Call(vm.wazeroCtx, size)
			if err != nil || len(results) == 0 {
				vm.fatalError(ErrorRuntime, "failed to allocate Wasm memory for string: %v", err)
			}
			ptr := results[0]

			allocatedPtrs = append(allocatedPtrs, ptr)

			if len(v) > 0 {
				ok := vm.wasmModule.Memory().Write(uint32(ptr), []byte(v))
				if !ok {
					vm.fatalError(ErrorRuntime, "failed to write string to Wasm memory")
				}
			}

			wasmParams = append(wasmParams, api.EncodeU32(uint32(ptr)))
			wasmParams = append(wasmParams, api.EncodeU32(uint32(size)))

		default:
			vm.fatalError(ErrorType, "unsupported native parameter type: %T", v)
		}
	}

	var retPtr uint64
	if returnsPointer {
		malloc := vm.wasmModule.ExportedFunction("malloc")
		if malloc == nil {
			vm.fatalError(ErrorRuntime, "native string returns are not supported because 'malloc' is not exported")
		}

		allocSize := uint64(8)
		if returnsArray {
			allocSize = 12
		}

		results, err := malloc.Call(vm.wazeroCtx, allocSize)
		if err != nil || len(results) == 0 {
			vm.fatalError(ErrorRuntime, "failed to allocate return buffer in Wasm memory: %v", err)
		}
		retPtr = results[0]

		wasmParams = append([]uint64{api.EncodeU32(uint32(retPtr))}, wasmParams...)
	}

	results, err := fn.Call(vm.wazeroCtx, wasmParams...)
	if err != nil {
		vm.fatalError(ErrorRuntime, "native function crashed: %v", err)
	}

	free := vm.wasmModule.ExportedFunction("free")
	if free != nil {
		for _, ptr := range allocatedPtrs {
			free.Call(vm.wazeroCtx, ptr)
		}
	}

	if returnsString {
		descBytes, ok := vm.wasmModule.Memory().Read(uint32(retPtr), 8)
		if !ok {
			vm.fatalError(ErrorRuntime, "failed to read return descriptor from Wasm memory")
		}

		ptr := binary.LittleEndian.Uint32(descBytes[0:4])
		length := binary.LittleEndian.Uint32(descBytes[4:8])

		if free != nil {
			free.Call(vm.wazeroCtx, retPtr)
		}

		if length == 0 {
			vm.push(NewNative(""))
		} else {
			strBytes, ok := vm.wasmModule.Memory().Read(ptr, length)
			if !ok {
				vm.fatalError(ErrorRuntime, "failed to read return string from Wasm memory")
			}
			vm.push(NewNative(string(strBytes)))
		}
	} else if returnsArray {
		descBytes, ok := vm.wasmModule.Memory().Read(uint32(retPtr), 12)
		if !ok {
			vm.fatalError(ErrorRuntime, "failed to read return slice descriptor from Wasm memory")
		}

		ptr := binary.LittleEndian.Uint32(descBytes[0:4])
		length := binary.LittleEndian.Uint32(descBytes[4:8])

		if free != nil {
			free.Call(vm.wazeroCtx, retPtr)
		}

		if length == 0 {
			vm.push(NewNative(&ArrayValue{Elements: []TinyValue{}}))
		} else {
			floatBytes, ok := vm.wasmModule.Memory().Read(ptr, length*8)
			if !ok {
				vm.fatalError(ErrorRuntime, "failed to read return array from Wasm memory")
			}

			elements := make([]TinyValue, length)
			for i := uint32(0); i < length; i++ {
				bits := binary.LittleEndian.Uint64(floatBytes[i*8 : (i+1)*8])
				elements[i] = NewNative(math.Float64frombits(bits))
			}
			vm.push(NewNative(&ArrayValue{Elements: elements}))
		}
	} else {
		if len(results) == 0 {
			vm.push(NewNull())
		} else {
			resType := fn.Definition().ResultTypes()[0]

			if resType == api.ValueTypeI32 {
				vm.push(NewNative(results[0] != 0))
			} else {
				vm.push(NewNative(api.DecodeF64(results[0])))
			}
		}
	}
}

func asIntInternal(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	default:
		return 0, false
	}
}

func (vm *VM) getIndexValue(objectValue TinyValue, indexValue TinyValue) TinyValue {
	var rawObj any
	if objectValue.IsInt {
		rawObj = objectValue.AsInt
	} else {
		rawObj = objectValue.Value
	}

	switch obj := rawObj.(type) {
	case *ArrayValue:
		index, ok := fastIndexInt(indexValue)
		if !ok {
			index = vm.asInt(indexValue)
		}
		if index < 0 || index >= len(obj.Elements) {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return NewNull()
		}
		return obj.Elements[index]

	case ArrayValue:
		index, ok := fastIndexInt(indexValue)
		if !ok {
			index = vm.asInt(indexValue)
		}
		if index < 0 || index >= len(obj.Elements) {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return NewNull()
		}
		return obj.Elements[index]

	case WasmArrayValue:
		index, ok := fastIndexInt(indexValue)
		if !ok {
			index = vm.asInt(indexValue)
		}
		length := int(vm.ReadWasmFloat(uint32(obj.Address) + 8))
		if index < 0 || index >= length {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return NewNull()
		}
		elemPtr := uint32(vm.ReadWasmFloat(uint32(obj.Address) + 16))
		addr := elemPtr + uint32(index*16)
		tag := vm.ReadWasmFloat(addr)
		val := vm.ReadWasmFloat(addr + 8)
		switch tag {
		case 1.0:
			return NewNative(val)
		case 2.0:
			return NewNative(val != 0.0)
		case 4.0:
			return NewNative(WasmObjectValue{Address: val, VM: vm})
		case 5.0:
			return NewNative(WasmArrayValue{Address: val, VM: vm})
		case 6.0:
			if strVal, ok := vm.readWasmStringMaybe(uint32(val)); ok {
				return NewNative(strVal)
			}
			return NewNull()
		default:
			return NewNull()
		}

	case ObjectValue:
		var key string
		if indexValue.IsInt {
			key = intToString(indexValue.AsInt)
		} else {
			key = valueToString(indexValue)
		}
		value, exists := obj[key]
		if !exists {
			return NewNull()
		}

		return value

	case *InstanceValue:
		var key string
		if indexValue.IsInt {
			key = intToString(indexValue.AsInt)
		} else {
			key = valueToString(indexValue)
		}
		value, exists := obj.Fields[key]
		if !exists {
			return NewNull()
		}

		return value

	default:
		vm.fatalError(ErrorType, "cannot index %s", TypeName(objectValue))
		return NewNull()
	}
}

func (vm *VM) setIndexValue(objectValue TinyValue, indexValue TinyValue, value TinyValue) {
	var rawObj any
	if objectValue.IsInt {
		rawObj = objectValue.AsInt
	} else {
		rawObj = objectValue.Value
	}

	switch obj := rawObj.(type) {
	case *ArrayValue:
		index, ok := fastIndexInt(indexValue)
		if !ok {
			index = vm.asInt(indexValue)
		}
		if index < 0 || index >= len(obj.Elements) {
			vm.fatalError(ErrorRuntime, "array index out of range: %d", index)
		}
		obj.Elements[index] = value
		vm.invalidateJitArrayMirror(obj)

	case WasmArrayValue:
		index, ok := fastIndexInt(indexValue)
		if !ok {
			index = vm.asInt(indexValue)
		}
		length := int(vm.ReadWasmFloat(uint32(obj.Address) + 8))
		if index < 0 || index >= length {
			vm.fatalError(ErrorRuntime, "array index out of range: %d", index)
		}
		elemPtr := uint32(vm.ReadWasmFloat(uint32(obj.Address) + 16))
		addr := elemPtr + uint32(index*16)
		vm.WriteWasmTaggedValue(addr, value)

	case ObjectValue:
		var key string
		if indexValue.IsInt {
			key = intToString(indexValue.AsInt)
		} else {
			key = valueToString(indexValue)
		}
		obj[key] = value
		vm.invalidateJitObjectMirror(obj)

	case *InstanceValue:
		vm.runtimeError(ErrorRuntime, "cannot modify class '%s' by index operator.", obj.ClassName)
		return

	default:
		vm.fatalError(ErrorType, "cannot index assign %s", TypeName(objectValue))
	}
}

func (vm *VM) callWasmArrayMethod(arr WasmArrayValue, method string, args []TinyValue) {
	switch method {
	case "length":
		expectArgs(vm, "array.length", args, 0)
		lengthF := vm.ReadWasmFloat(uint32(arr.Address) + 8)
		vm.push(NewInt(int(lengthF)))
		return
	case "push":
		expectArgs(vm, "array.push", args, 1)
		tag, val := vm.tinyValueToJitValue(vm.jitModule, args[0])
		pushFn := vm.jitModule.ExportedFunction("array_push")
		if pushFn == nil {
			vm.fatalError(ErrorInternal, "JIT array_push function not found")
		}
		pushFn.Call(vm.wazeroCtx, api.EncodeF64(arr.Address), api.EncodeF64(tag), api.EncodeF64(val))
		vm.push(NewNative(arr))
		return
	case "get":
		expectArgs(vm, "array.get", args, 1)
		var index int
		idxVal := args[0]
		if idxVal.IsInt {
			index = idxVal.AsInt
		} else {
			index = argInt(vm, "array.get", args, 0)
		}
		length := int(vm.ReadWasmFloat(uint32(arr.Address) + 8))
		if index < 0 || index >= length {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return
		}
		elemPtr := uint32(vm.ReadWasmFloat(uint32(arr.Address) + 16))
		addr := elemPtr + uint32(index*16)
		tag := vm.ReadWasmFloat(addr)
		vVal := vm.ReadWasmFloat(addr + 8)
		switch tag {
		case 1.0:
			vm.push(NewNative(vVal))
		case 2.0:
			vm.push(NewNative(vVal != 0.0))
		case 4.0:
			vm.push(NewNative(WasmObjectValue{Address: vVal, VM: vm}))
		case 5.0:
			vm.push(NewNative(WasmArrayValue{Address: vVal, VM: vm}))
		case 6.0:
			strVal, ok := vm.readWasmStringMaybe(uint32(vVal))
			if ok {
				vm.push(NewNative(strVal))
			} else {
				vm.push(NewNull())
			}
		default:
			vm.push(NewNull())
		}
		return
	case "set":
		expectArgs(vm, "array.set", args, 2)
		var index int
		idxVal := args[0]
		if idxVal.IsInt {
			index = idxVal.AsInt
		} else {
			index = argInt(vm, "array.set", args, 0)
		}
		length := int(vm.ReadWasmFloat(uint32(arr.Address) + 8))
		if index < 0 || index >= length {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return
		}
		elemPtr := uint32(vm.ReadWasmFloat(uint32(arr.Address) + 16))
		addr := elemPtr + uint32(index*16)
		tag, val := vm.tinyValueToJitValue(vm.jitModule, args[1])
		vm.WriteWasmFloat(addr, tag)
		vm.WriteWasmFloat(addr+8, val)
		vm.push(NewNative(arr))
		return
	}
	vm.fatalError(ErrorName, "unknown array method: %s", method)
}
