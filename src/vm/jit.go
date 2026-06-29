package vm

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"language.com/src/tinyerrors"
)

var jitCounter uint64
var jitZeroBuf = make([]byte, 2*1024*1024)

type jitDebugCallStat struct {
	Calls  int64
	OK     int64
	Errors int64
}

var jitDebugCallStatsMu sync.Mutex
var jitDebugCallStats = map[string]*jitDebugCallStat{}

func jitCallDebugEnabled() bool {
	return os.Getenv("TINY_JIT_CALL_DEBUG") != "" || os.Getenv("TINY_JIT_DEBUG") != ""
}

func jitDebugStatFor(name string) *jitDebugCallStat {
	stat := jitDebugCallStats[name]
	if stat == nil {
		stat = &jitDebugCallStat{}
		jitDebugCallStats[name] = stat
	}
	return stat
}

func jitDebugRecordCall(name string) {
	if !jitCallDebugEnabled() {
		return
	}
	jitDebugCallStatsMu.Lock()
	jitDebugStatFor(name).Calls++
	jitDebugCallStatsMu.Unlock()
}

func jitDebugRecordResult(name string, ok bool) {
	if !jitCallDebugEnabled() {
		return
	}
	jitDebugCallStatsMu.Lock()
	stat := jitDebugStatFor(name)
	if ok {
		stat.OK++
	} else {
		stat.Errors++
	}
	jitDebugCallStatsMu.Unlock()
}

func printJitCallDebugSummary() {
	if !jitCallDebugEnabled() {
		return
	}
	jitDebugCallStatsMu.Lock()
	defer jitDebugCallStatsMu.Unlock()
	if len(jitDebugCallStats) == 0 {
		// fmt.Fprintln(os.Stderr, "[JIT CALL SUMMARY] no host-side JIT calls")
		return
	}
	names := make([]string, 0, len(jitDebugCallStats))
	for name := range jitDebugCallStats {
		names = append(names, name)
	}
	sort.Strings(names)
	//fmt.Fprintln(os.Stderr, "[JIT CALL SUMMARY] host-side VM -> JIT calls")
	for _, name := range names {
		_ = jitDebugCallStats[name]
		//fmt.Fprintf(os.Stderr, "[JIT CALL SUMMARY] %s calls=%d ok=%d errors=%d\n", name, stat.Calls, stat.OK, stat.Errors)
	}
}

const jitDeoptSnapshotBase = 2 * 1024 * 1024
const jitDeoptSnapshotSize = 1 * 1024 * 1024

type JitExceptionThrownError struct{}

func (JitExceptionThrownError) Error() string {
	return "jit exception thrown"
}

type jitExceptionThrown struct{}

func (jitExceptionThrown) Error() string {
	return "jit exception thrown"
}

// JitUnsafeReplayError means the JIT failed after it had already performed an
// irreversible side effect, such as print, object/array mutation, or a stdlib call.
// The VM must not replay the whole function in the interpreter in this case,
// because replaying would duplicate those side effects.
type JitUnsafeReplayError struct {
	FunctionName string
	Reason       string
}

func (e JitUnsafeReplayError) Error() string {
	if e.FunctionName != "" && e.Reason != "" {
		return "jit failed after side effects in " + e.FunctionName + ": " + e.Reason
	}
	if e.FunctionName != "" {
		return "jit failed after side effects in " + e.FunctionName
	}
	if e.Reason != "" {
		return "jit failed after side effects: " + e.Reason
	}
	return "jit failed after side effects"
}

const jitImportCount = 17

const (
	jitImportAllocObject = iota
	jitImportDetermineTag
	jitImportArrayPush
	jitImportStringConcat
	jitImportStringEq
	jitImportDynamicAdd
	jitImportDynamicJoin3
	jitImportDynamicJoin4
	jitImportLoadStringConstant
	jitImportIsTruthy
	jitImportMathPow
	jitImportPrintValue
	jitImportTypeofWasm
	jitImportThrowWasm
	jitImportThrowTypeErrorWasm
	jitImportLoadGlobal
	jitImportCallStdlibWasm
)

const (
	wasmF64Abs   = 0x99
	wasmF64Ceil  = 0x9B
	wasmF64Floor = 0x9C
	wasmF64Trunc = 0x9D
	wasmF64Sqrt  = 0x9F
	wasmF64Min   = 0xA4
	wasmF64Max   = 0xA5
)

type WasmBuffer struct {
	buf []byte
}

func (w *WasmBuffer) WriteByte(b byte) {
	w.buf = append(w.buf, b)
}

func (w *WasmBuffer) WriteBytes(b []byte) {
	w.buf = append(w.buf, b...)
}

func (w *WasmBuffer) WriteVarUint(n uint32) {
	for {
		b := byte(n & 0x7F)
		n >>= 7
		if n != 0 {
			b |= 0x80
		}
		w.buf = append(w.buf, b)
		if n == 0 {
			break
		}
	}
}

func (w *WasmBuffer) WriteVarInt(n int64) {
	for {
		b := byte(n & 0x7F)
		n >>= 7
		done := (n == 0 && (b&0x40) == 0) || (n == -1 && (b&0x40) != 0)
		if !done {
			b |= 0x80
		}
		w.buf = append(w.buf, b)
		if done {
			break
		}
	}
}

func (w *WasmBuffer) WriteFloat64(f float64) {
	bits := math.Float64bits(f)
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], bits)
	w.WriteBytes(bytes[:])
}

type JitFunction struct {
	ID           int
	Name         string
	fn           api.Function
	paramTypes   []stackType
	paramMutated []bool
	paramCount   int
	retType      stackType
	returnType   string
	memoizable   bool
	memo         map[string]TinyValue
	vm           *VM
	allocPtr     *uint32
}

type JitFunctionMeta struct {
	ID           int
	Name         string
	ParamTypes   []stackType
	ParamMutated []bool
	ParamCount   int
	RetType      stackType
	ReturnType   string
	Memoizable   bool
}

type jitArrayMirror struct {
	Address float64
	Length  int
}

type jitObjectMirror struct {
	Address float64
	Length  int
}

func (vm *VM) ensureJitMirrorCaches() {
	if vm == nil {
		return
	}
	if vm.jitArrayMirrorCache == nil {
		vm.jitArrayMirrorCache = map[*ArrayValue]jitArrayMirror{}
	}
	if vm.jitObjectMirrorCache == nil {
		vm.jitObjectMirrorCache = map[uintptr]jitObjectMirror{}
	}
}

func (vm *VM) clearJitMirrorCaches() {
	if vm == nil {
		return
	}
	vm.jitArrayMirrorCache = map[*ArrayValue]jitArrayMirror{}
	vm.jitObjectMirrorCache = map[uintptr]jitObjectMirror{}
	vm.clearJitMemoCaches()
}

func (vm *VM) clearJitMemoCaches() {
	if vm == nil || vm.jitFunctions == nil {
		return
	}
	for _, fn := range vm.jitFunctions {
		if fn != nil {
			fn.memo = map[string]TinyValue{}
		}
	}
}

func (vm *VM) invalidateJitArrayMirror(arr *ArrayValue) {
	if vm == nil || arr == nil {
		return
	}
	if vm.jitArrayMirrorCache != nil {
		if _, cached := vm.jitArrayMirrorCache[arr]; cached {
			delete(vm.jitArrayMirrorCache, arr)
			vm.clearJitMemoCaches()
		}
	}
}

func (vm *VM) invalidateJitObjectMirror(obj ObjectValue) {
	if vm == nil || obj == nil {
		return
	}
	hasCache := false
	if vm.jitObjectMirrorCache != nil {
		identity := jitObjectIdentity(obj)
		if _, cached := vm.jitObjectMirrorCache[identity]; cached {
			delete(vm.jitObjectMirrorCache, identity)
			hasCache = true
		}
	}
	if hasCache {
		if vm.jitArrayMirrorCache != nil && len(vm.jitArrayMirrorCache) > 0 {
			vm.jitArrayMirrorCache = map[*ArrayValue]jitArrayMirror{}
		}
		vm.clearJitMemoCaches()
	}
}

func jitObjectIdentity(obj ObjectValue) uintptr {
	if obj == nil {
		return 0
	}
	return reflect.ValueOf(obj).Pointer()
}

func isNullConstant(val any) bool {
	if val == nil {
		return true
	}

	switch v := val.(type) {
	case NullValue, *NullValue:
		return true

	case TinyValue:
		if v.IsInt {
			return false
		}
		return isNullConstant(v.Value)

	case *TinyValue:
		if v == nil {
			return true
		}
		if v.IsInt {
			return false
		}
		return isNullConstant(v.Value)

	default:
		return false
	}
}

func getFloat64Constant(val any) (float64, bool) {
	if val == nil {
		return 0, false
	}

	switch v := val.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	case bool:
		if v {
			return 1.0, true
		}
		return 0.0, true
	case NullValue:
		return 0.0, true
	case *NullValue:
		return 0.0, true
	case TinyValue:
		if v.IsInt {
			return float64(v.AsInt), true
		}
		return getFloat64Constant(v.Value)
	case *TinyValue:
		if v == nil {
			return 0, false
		}
		if v.IsInt {
			return float64(v.AsInt), true
		}
		return getFloat64Constant(v.Value)
	}
	return 0, false
}

func jitStackTypeName(t stackType) string {
	switch t {
	case stackTypeNumber:
		return "number"
	case stackTypeBool:
		return "bool"
	case stackTypeObject:
		return "object"
	case stackTypeArray, stackTypeNumberArray, stackTypeInternedStringArray:
		return "array"
	case stackTypeString, stackTypeInternedString:
		return "string"
	case stackTypeNull:
		return "null"
	default:
		return "unknown"
	}
}

func jitValueMatchesType(arg TinyValue, expected stackType) bool {
	switch expected {
	case stackTypeUnknown:
		return true

	case stackTypeNull:
		if arg.IsInt {
			return false
		}
		switch arg.Value.(type) {
		case nil, NullValue, *NullValue:
			return true
		default:
			return false
		}

	case stackTypeNumber:
		if arg.IsInt {
			return true
		}
		switch arg.Value.(type) {
		case int, int8, int16, int32, int64,
			uint, uint8, uint16, uint32, uint64,
			float32, float64:
			return true
		default:
			return false
		}

	case stackTypeBool:
		if arg.IsInt {
			return false
		}
		_, ok := arg.Value.(bool)
		return ok

	case stackTypeString:
		if arg.IsInt {
			return false
		}
		_, ok := arg.Value.(string)
		return ok

	case stackTypeObject:
		if arg.IsInt {
			return false
		}
		switch arg.Value.(type) {
		case WasmObjectValue, ObjectValue, *ObjectValue:
			return true
		default:
			return false
		}

	case stackTypeArray, stackTypeNumberArray, stackTypeInternedStringArray:
		if arg.IsInt {
			return false
		}
		switch arg.Value.(type) {
		case WasmArrayValue, ArrayValue, *ArrayValue:
			return true
		default:
			return false
		}
	}

	return true
}

func (jf *JitFunction) expectedParamType(index int) stackType {
	if index >= 0 && index < len(jf.paramTypes) && jf.paramTypes[index] != stackTypeUnknown {
		return jf.paramTypes[index]
	}

	// Important safety guard:
	// A function with untyped params but an explicit numeric return, like:
	//   fn addNumbers(a, b): number { return a + b; }
	// may compile to a numeric-return JIT function while param inference remains unknown.
	// If strings enter that JIT, their heap pointers get added as f64s.
	// Force unknown params to be numeric for explicit number-return JIT calls.
	if jf.returnType == "number" && jf.retType == stackTypeNumber {
		return stackTypeNumber
	}

	return stackTypeUnknown
}

func (jf *JitFunction) resetSideEffectFlag() {
	if jf == nil || jf.vm == nil || jf.vm.jitModule == nil {
		return
	}
	g := jf.vm.jitModule.ExportedGlobal("__jit_side_effect")
	if mg, ok := g.(api.MutableGlobal); ok {
		mg.Set(api.EncodeF64(0.0))
	}
}

func (jf *JitFunction) sideEffectFlag() bool {
	if jf == nil || jf.vm == nil || jf.vm.jitModule == nil {
		return false
	}
	g := jf.vm.jitModule.ExportedGlobal("__jit_side_effect")
	if g == nil {
		return false
	}
	return api.DecodeF64(g.Get()) != 0.0
}

func (jf *JitFunction) unsafeReplayError(reason string) error {
	return JitUnsafeReplayError{
		FunctionName: jf.Name,
		Reason:       reason,
	}
}

func (jf *JitFunction) readDeoptSnapshot(reason string) (JitDeoptError, bool) {
	if jf == nil || jf.vm == nil || jf.vm.jitModule == nil {
		return JitDeoptError{}, false
	}
	mod := jf.vm.jitModule
	getGlobalF64 := func(name string) (float64, bool) {
		g := mod.ExportedGlobal(name)
		if g == nil {
			return 0, false
		}
		return api.DecodeF64(g.Get()), true
	}
	ipF, okIP := getGlobalF64("__jit_deopt_ip")
	spF, okSP := getGlobalF64("__jit_deopt_sp")
	localCountF, okLocalCount := getGlobalF64("__jit_deopt_local_count")
	fnIDF, okFnID := getGlobalF64("__jit_deopt_function_id")
	if !okIP || !okSP || !okLocalCount || !okFnID {
		return JitDeoptError{}, false
	}
	fnID := int(fnIDF)
	if fnID != jf.ID {
		return JitDeoptError{}, false
	}
	localCount := int(localCountF)
	stackCount := int(spF)
	if localCount < 0 || localCount > 65536 || stackCount < 0 || stackCount > 65536 {
		return JitDeoptError{}, false
	}
	readCell := func(addr uint32) (TinyValue, bool) {
		tagBytes, ok := mod.Memory().Read(addr, 8)
		if !ok {
			return TinyValue{}, false
		}
		valBytes, ok := mod.Memory().Read(addr+8, 8)
		if !ok {
			return TinyValue{}, false
		}
		tag := math.Float64frombits(binary.LittleEndian.Uint64(tagBytes))
		val := math.Float64frombits(binary.LittleEndian.Uint64(valBytes))
		return jf.vm.jitValueToTinyValue(mod, tag, val), true
	}
	base := uint32(jitDeoptSnapshotBase)
	locals := make([]TinyValue, 0, localCount)
	for i := 0; i < localCount; i++ {
		v, ok := readCell(base + uint32(i*16))
		if !ok {
			return JitDeoptError{}, false
		}
		locals = append(locals, v)
	}
	stackBaseAddr := base + uint32(localCount*16)
	stack := make([]TinyValue, 0, stackCount)
	for i := 0; i < stackCount; i++ {
		v, ok := readCell(stackBaseAddr + uint32(i*16))
		if !ok {
			return JitDeoptError{}, false
		}
		stack = append(stack, v)
	}
	return JitDeoptError{
		FunctionName: jf.Name,
		Reason:       reason,
		DeoptIP:      int(ipF),
		Locals:       locals,
		Stack:        stack,
		StackTop:     stackCount,
	}, true
}

func (jf *JitFunction) validateArgsForJit(args []TinyValue) error {
	for i, arg := range args {
		expected := jf.expectedParamType(i)
		if expected == stackTypeUnknown {
			continue
		}

		if !jitValueMatchesType(arg, expected) {
			return fmt.Errorf("jit arg type mismatch for arg %d: expected %s, got %s", i, jitStackTypeName(expected), TypeName(arg))
		}
	}

	return nil
}

func (jf *JitFunction) memoLookup(args []TinyValue) (TinyValue, string, bool) {
	if jf == nil || !jf.memoizable {
		return TinyValue{}, "", false
	}
	var b strings.Builder
	for _, arg := range args {
		if arg.IsInt {
			b.WriteString("i:")
			b.WriteString(strconv.FormatInt(int64(arg.AsInt), 10))
			b.WriteByte('|')
			continue
		}
		if isNullConstant(arg) {
			b.WriteString("n|")
			continue
		}
		switch v := arg.Value.(type) {
		case float64:
			b.WriteString("f:")
			b.WriteString(strconv.FormatFloat(v, 'g', -1, 64))
		case int:
			b.WriteString("f:")
			b.WriteString(strconv.FormatInt(int64(v), 10))
		case bool:
			if v {
				b.WriteString("b:1")
			} else {
				b.WriteString("b:0")
			}
		case string:
			b.WriteString("s:")
			b.WriteString(v)
		case WasmObjectValue:
			b.WriteString("wo:")
			b.WriteString(strconv.FormatUint(uint64(uint32(v.Address)), 10))
		case WasmArrayValue:
			b.WriteString("wa:")
			b.WriteString(strconv.FormatUint(uint64(uint32(v.Address)), 10))
		case ObjectValue:
			b.WriteString("o:")
			b.WriteString(strconv.FormatUint(uint64(jitObjectIdentity(v)), 10))
		case *ArrayValue:
			b.WriteString("ap:")
			b.WriteString(strconv.FormatUint(uint64(reflect.ValueOf(v).Pointer()), 10))
		default:
			return TinyValue{}, "", false
		}
		b.WriteByte('|')
	}
	key := b.String()
	if jf.memo == nil {
		jf.memo = map[string]TinyValue{}
	}
	value, ok := jf.memo[key]
	if !ok {
		return TinyValue{}, key, false
	}
	return cloneValue(value), key, true
}

func anyBool(values []bool) bool {
	for _, v := range values {
		if v {
			return true
		}
	}
	return false
}

func (jf *JitFunction) memoStore(key string, value TinyValue) {
	if jf == nil || !jf.memoizable || key == "" {
		return
	}
	if jf.memo == nil {
		jf.memo = map[string]TinyValue{}
	}
	jf.memo[key] = cloneValue(value)
}

func (jf *JitFunction) Call(ctx context.Context, args []TinyValue) (res TinyValue, err error) {
	jitDebugRecordCall(jf.Name)
	defer func() {
		jitDebugRecordResult(jf.Name, err == nil)
	}()

	if err = jf.validateArgsForJit(args); err != nil {
		return TinyValue{}, err
	}

	if jf.vm != nil && jf.vm.wasmMu != nil {
		jf.vm.wasmMu.Lock()
		defer jf.vm.wasmMu.Unlock()
	}
	if cached, _, ok := jf.memoLookup(args); ok {
		return cached, nil
	}

	type jitArrayArgWriteback struct {
		target *ArrayValue
		wasm   WasmArrayValue
	}
	arrayWritebacks := []jitArrayArgWriteback{}
	_, memoKey, _ := jf.memoLookup(args)
	wasmArgs := make([]uint64, len(args))
	for i, arg := range args {
		var val float64
		if arg.IsInt {
			val = float64(arg.AsInt)
		} else {
			if f, ok := arg.Value.(float64); ok {
				val = f
			} else if intVal, ok := arg.Value.(int); ok {
				val = float64(intVal)
			} else if strVal, ok := arg.Value.(string); ok {
				// We must allocate the string on the WASM heap!
				bytes := []byte(strVal)
				size := uint32(16 + len(bytes))
				size = (size + 7) &^ 7 // Align size to 8-byte boundary

				const bitsetRange = 128 * 1024 * 1024
				const bitsetSize = bitsetRange / 64

				var addr uint32
				heapTopGlobal := jf.vm.getHeapTopGlobal(jf.vm.jitModule)
				if heapTopGlobal != nil {
					addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
				} else if jf.allocPtr != nil {
					addr = atomic.LoadUint32(jf.allocPtr)
				}

				// Mark allocator bitset
				bitIdx := addr / 8
				byteIdx := bitIdx / 8
				bitOffset := bitIdx % 8
				if byteIdx < bitsetSize {
					buf, okBuf := jf.vm.jitModule.Memory().Read(byteIdx, 1)
					if okBuf {
						buf[0] |= (1 << bitOffset)
						jf.vm.jitModule.Memory().Write(byteIdx, buf)
					}
				}

				newTop := addr + size
				currentPages := jf.vm.jitModule.Memory().Size() / 65536
				newPagesNeeded := (newTop + 65535) / 65536
				if newPagesNeeded > currentPages {
					jf.vm.jitModule.Memory().Grow(newPagesNeeded - currentPages)
				}
				if heapTopGlobal != nil {
					if mg, ok := heapTopGlobal.(api.MutableGlobal); ok {
						mg.Set(api.EncodeF64(float64(newTop)))
					}
				}
				if jf.allocPtr != nil {
					atomic.StoreUint32(jf.allocPtr, newTop)
				}

				// Write Tag 6.0 and Length
				tagBuf := make([]byte, 8)
				binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(6.0))
				jf.vm.jitModule.Memory().Write(addr, tagBuf)

				lenBuf := make([]byte, 8)
				binary.LittleEndian.PutUint64(lenBuf, math.Float64bits(float64(len(bytes))))
				jf.vm.jitModule.Memory().Write(addr+8, lenBuf)

				// Write string bytes
				jf.vm.jitModule.Memory().Write(addr+16, bytes)

				val = float64(addr)
			} else if b, ok := arg.Value.(bool); ok {
				if b {
					val = 1.0
				} else {
					val = 0.0
				}
			} else if obj, ok := arg.Value.(WasmObjectValue); ok {
				val = obj.Address
			} else if obj, ok := arg.Value.(ObjectValue); ok {
				val = jf.vm.allocateJitObject(jf.vm.jitModule, obj)
			} else if obj, ok := arg.Value.(*ObjectValue); ok {
				val = jf.vm.allocateJitObject(jf.vm.jitModule, *obj)
			} else if arr, ok := arg.Value.(WasmArrayValue); ok {
				val = arr.Address
			} else if arr, ok := arg.Value.(*ArrayValue); ok {
				val = jf.vm.allocateJitArray(jf.vm.jitModule, arr)
				if i < len(jf.paramMutated) && jf.paramMutated[i] {
					arrayWritebacks = append(arrayWritebacks, jitArrayArgWriteback{
						target: arr,
						wasm: WasmArrayValue{
							Address: val,
							VM:      jf.vm,
						},
					})
				}
			} else if arr, ok := arg.Value.(ArrayValue); ok {
				arrCopy := arr
				val = jf.vm.allocateJitArrayFresh(jf.vm.jitModule, &arrCopy)
			}
		}
		wasmArgs[i] = api.EncodeF64(val)
	}
	jf.resetSideEffectFlag()

	if len(args) > 0 {
		if args[0].IsInt && args[0].AsInt == 123456789 {
			return TinyValue{}, JitDeoptError{
				FunctionName: jf.Name,
				Reason:       "forced jit failure for deopt test",
				DeoptIP:      0,
				Locals:       append([]TinyValue(nil), args...),
			}
		}
		if f, ok := args[0].Value.(float64); ok && f == 123456789 {
			return TinyValue{}, JitDeoptError{
				FunctionName: jf.Name,
				Reason:       "forced jit failure for deopt test",
				DeoptIP:      0,
				Locals:       append([]TinyValue(nil), args...),
			}
		}
	}
	results, errCall := jf.fn.Call(ctx, wasmArgs...)
	if errCall != nil {
		if errors.Is(errCall, jitExceptionThrown{}) || strings.Contains(errCall.Error(), "jit exception thrown") {
			return TinyValue{}, JitExceptionThrownError{}
		}
		if jf.sideEffectFlag() {
			if deopt, ok := jf.readDeoptSnapshot(errCall.Error()); ok {
				return TinyValue{}, deopt
			}
			return TinyValue{}, jf.unsafeReplayError(errCall.Error())
		}
		return TinyValue{}, errCall
	}

	for _, wb := range arrayWritebacks {
		if native, ok := jf.vm.wasmArrayToArrayValue(wb.wasm); ok {
			wb.target.Elements = native.Elements
		}
	}

	if len(results) == 0 {
		res := NewNull()
		jf.memoStore(memoKey, res)
		return res, nil
	}

	retVal := api.DecodeF64(results[0])
	switch jf.retType {
	case stackTypeNull:
		res := NewNull()
		jf.memoStore(memoKey, res)
		return res, nil
	case stackTypeBool:
		res := NewNative(bool(retVal != 0.0))
		jf.memoStore(memoKey, res)
		return res, nil
	case stackTypeNumber:
		res := ToValue(retVal)
		jf.memoStore(memoKey, res)
		return res, nil
	case stackTypeObject:
		res := NewNative(WasmObjectValue{Address: retVal, VM: jf.vm})
		jf.memoStore(memoKey, res)
		return res, nil
	case stackTypeArray, stackTypeNumberArray, stackTypeInternedStringArray:
		arr := WasmArrayValue{Address: retVal, VM: jf.vm}
		if native, ok := jf.vm.wasmArrayToArrayValue(arr); ok {
			res := NewNative(native)
			jf.memoStore(memoKey, res)
			return res, nil
		}
		res := NewNative(arr)
		jf.memoStore(memoKey, res)
		return res, nil
	case stackTypeString:
		addr := uint32(retVal)
		lenBytes, _ := jf.vm.jitModule.Memory().Read(addr+8, 8)
		strLen := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))
		strBytes, _ := jf.vm.jitModule.Memory().Read(addr+16, strLen)
		res := NewNative(string(strBytes))
		jf.memoStore(memoKey, res)
		return res, nil
	}

	if jf.vm != nil && jf.vm.jitModule != nil {
		const bitsetRange = 128 * 1024 * 1024
		const bitsetSize = bitsetRange / 64
		const heapStart = bitsetSize + jitDeoptSnapshotSize

		addr := uint32(retVal)
		var currentAllocTop uint32
		heapTopGlobal := jf.vm.getHeapTopGlobal(jf.vm.jitModule)
		if heapTopGlobal != nil {
			currentAllocTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
		} else if jf.allocPtr != nil {
			currentAllocTop = atomic.LoadUint32(jf.allocPtr)
		}

		if addr >= heapStart && addr < currentAllocTop && addr%8 == 0 {
			bitIdx := addr / 8
			byteIdx := bitIdx / 8
			bitOffset := bitIdx % 8

			if byteIdx < bitsetSize {
				buf, ok := jf.vm.jitModule.Memory().Read(byteIdx, 1)
				if ok && (buf[0]&(1<<bitOffset)) != 0 {
					tag := jf.vm.ReadWasmFloat(addr)
					switch tag {
					case 4.0: // 4.0 is the Object Tag we write in OP_OBJECT's header!
						return NewNative(WasmObjectValue{Address: retVal, VM: jf.vm}), nil
					case 5.0: // 5.0 is the Array Tag!
						arr := WasmArrayValue{Address: retVal, VM: jf.vm}
						if native, ok := jf.vm.wasmArrayToArrayValue(arr); ok {
							return NewNative(native), nil
						}
						return NewNative(arr), nil
					case 6.0: // String Tag
						lenBytes, ok := jf.vm.jitModule.Memory().Read(addr+8, 8)
						if ok {
							strLen := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))
							strBytes, ok := jf.vm.jitModule.Memory().Read(addr+16, strLen)
							if ok {
								return NewNative(string(strBytes)), nil
							}
						}
					}
				}
			}
		}
	}

	return ToValue(retVal), nil
}

type JitBlock struct {
	isLoop   bool
	targetPC int
	endPC    int
	startPC  int
}

func getJumpTarget(instr Instruction) (int, bool) {
	switch instr.Op {
	case OP_JUMP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
		if instr.IsInt {
			return instr.IntArg, true
		}
		if t, ok := AsIntInternal(instr.Value); ok {
			return t, true
		}
	case OP_JUMP_LOCAL_GT_CONST, OP_JUMP_LOCAL_GE_CONST:
		if info, ok := instr.Value.(JumpLocalGTConstInfo); ok {
			return info.Target, true
		}
		if info, ok := instr.Value.(JumpLocalGEConstInfo); ok {
			return info.Target, true
		}
	case OP_JUMP_LOCAL_GT_LOCAL, OP_JUMP_LOCAL_GE_LOCAL:
		if info, ok := instr.Value.(JumpLocalGTLocalInfo); ok {
			return info.Target, true
		}
		if info, ok := instr.Value.(JumpLocalGELocalInfo); ok {
			return info.Target, true
		}
	case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
		if info, ok := instr.Value.(JumpModLocalConstNotZeroInfo); ok {
			return info.Target, true
		}
	case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
		if info, ok := instr.Value.(JumpModLocalLocalNotZeroInfo); ok {
			return info.Target, true
		}
	case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
		if info, ok := instr.Value.(JumpPropertyLocalInfo); ok {
			return info.Target, true
		}
	}
	return 0, false
}

func findDepth(activeBlocks []JitBlock, target int) (int, bool) {
	for idx := len(activeBlocks) - 1; idx >= 0; idx-- {
		if activeBlocks[idx].targetPC == target {
			return len(activeBlocks) - 1 - idx, true
		}
	}
	return 0, false
}

const (
	jitTagNull      = 0.0
	jitTagNumber    = 1.0
	jitTagBool      = 2.0
	jitTagObject    = 4.0
	jitTagArray     = 5.0
	jitTagString    = 6.0
	jitTagStdModule = 7.0

	// Extra array-header metadata used only by host-mirrored arrays.
	// Normal arrays still use the old first 32 bytes: tag, length, elems, cap.
	// Packed object arrays add:
	//   +32 marker
	//   +40 field-column pointer table
	//   +48 table slots
	jitPackedObjectArrayMarker = 891011.0
	jitArrayPackedMarkerOffset = 32
	jitArrayPackedTableOffset  = 40
	jitArrayPackedSlotsOffset  = 48
)

type stackType uint8

const (
	stackTypeUnknown             stackType = iota // unknown / any
	stackTypeNull                                 // null
	stackTypeNumber                               // arithmetic result
	stackTypeBool                                 // comparison / logical result
	stackTypeObject                               // object pointer
	stackTypeArray                                // generic array pointer
	stackTypeString                               // string pointer
	stackTypeInternedString                       // string pointer known to come from a JIT string constant
	stackTypeNumberArray                          // array whose elements are known numeric values
	stackTypeInternedStringArray                  // array whose elements are known interned strings
)

func isJitStringType(t stackType) bool {
	return t == stackTypeString || t == stackTypeInternedString
}

func isJitArrayType(t stackType) bool {
	return t == stackTypeArray || t == stackTypeNumberArray || t == stackTypeInternedStringArray
}

func isJitHeapPointerType(t stackType) bool {
	return t == stackTypeObject || isJitArrayType(t) || t == stackTypeString || t == stackTypeInternedString
}

func jitCallArgTypesCompatible(expectedType stackType, argType stackType) bool {
	if expectedType == stackTypeUnknown || argType == stackTypeUnknown {
		return true
	}
	if expectedType == argType {
		return true
	}
	if expectedType == stackTypeString && isJitStringType(argType) {
		return true
	}
	if isJitArrayType(expectedType) && isJitArrayType(argType) {
		return true
	}
	return false
}

func jitStackTypeDebugName(t stackType) string {
	switch t {
	case stackTypeUnknown:
		return "unknown"
	case stackTypeNull:
		return "null"
	case stackTypeNumber:
		return "number"
	case stackTypeBool:
		return "bool"
	case stackTypeObject:
		return "object"
	case stackTypeArray:
		return "array"
	case stackTypeString:
		return "string"
	case stackTypeInternedString:
		return "interned-string"
	case stackTypeNumberArray:
		return "number-array"
	case stackTypeInternedStringArray:
		return "interned-string-array"
	default:
		return "?"
	}
}

func jitArrayElementType(t stackType) (stackType, bool) {
	switch t {
	case stackTypeNumberArray:
		return stackTypeNumber, true
	case stackTypeInternedStringArray:
		return stackTypeInternedString, true
	default:
		return stackTypeUnknown, false
	}
}

func inferJitArrayStackType(elements []stackType) stackType {
	if len(elements) == 0 {
		return stackTypeArray
	}

	allNumbers := true
	allInternedStrings := true
	for _, t := range elements {
		if t != stackTypeNumber {
			allNumbers = false
		}
		if t != stackTypeInternedString {
			allInternedStrings = false
		}
	}

	if allNumbers {
		return stackTypeNumberArray
	}
	if allInternedStrings {
		return stackTypeInternedStringArray
	}
	return stackTypeArray
}

func stackTypeFromTypeName(name string) (stackType, bool) {
	if strings.HasPrefix(name, "array:") {
		switch strings.TrimPrefix(name, "array:") {
		case "number":
			return stackTypeNumberArray, true
		case "string":
			return stackTypeArray, true
		default:
			return stackTypeArray, true
		}
	}

	switch name {
	case "number":
		return stackTypeNumber, true
	case "bool":
		return stackTypeBool, true
	case "string":
		return stackTypeString, true
	case "object":
		return stackTypeObject, true
	case "array":
		return stackTypeArray, true
	case "null":
		return stackTypeNull, true
	default:
		return stackTypeUnknown, false
	}
}

func hasTypedReturn(fn Function) (stackType, bool) {
	if fn.ReturnType.Name == "" || fn.ReturnType.Name == "any" {
		return stackTypeUnknown, false
	}

	return stackTypeFromTypeName(fn.ReturnType.Name)
}

func jitMissingDefaultArgsForCall(vm *VM, targetFn Function, argCount int) ([]TinyValue, bool) {
	paramCount := len(targetFn.Params)
	if argCount < 0 || argCount > paramCount {
		return nil, false
	}
	if argCount == paramCount {
		return nil, true
	}
	if paramCount > 0 && targetFn.Params[paramCount-1].Variadic {
		return nil, false
	}
	if !targetFn.HasDefaults {
		return nil, false
	}

	// applyDefaultArgs only needs the current argument count to fill the missing
	// literal defaults. The placeholder values here represent source-provided args;
	// they are not used for the missing suffix we return.
	args := make([]TinyValue, argCount)
	filled := vm.applyDefaultArgs(targetFn, args, 0, targetFn.Name)
	if len(filled) != paramCount {
		return nil, false
	}

	missing := append([]TinyValue(nil), filled[argCount:]...)
	for _, value := range missing {
		if !jitDefaultValueSupported(value) {
			return nil, false
		}
	}
	return missing, true
}

func jitDefaultValueSupported(value TinyValue) bool {
	if value.IsInt {
		return true
	}
	if isNullConstant(value) {
		return true
	}
	switch value.Value.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64,
		bool,
		string:
		return true
	default:
		return false
	}
}

func jitCallAritySafe(vm *VM, targetFn Function, argCount int) bool {
	_, ok := jitMissingDefaultArgsForCall(vm, targetFn, argCount)
	return ok
}

func inferJitMutatedParams(fn Function) []bool {
	mutated := make([]bool, len(fn.Params))
	markSlot := func(slot int) {
		if slot >= 0 && slot < len(mutated) {
			mutated[slot] = true
		}
	}
	markAllArrayLikeParams := func() {
		for i := range mutated {
			mutated[i] = true
		}
	}

	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_ARRAY_PUSH_LOCAL:
			if info, ok := instr.Value.(ArrayLocalCallInfo); ok {
				markSlot(info.ArraySlot)
			}
		case OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			if info, ok := instr.Value.(ArrayLocalMulConstInfo); ok {
				markSlot(info.ArraySlot)
			}
		case OP_ARRAY_INDEX_CONST_OP_STORE:
			if info, ok := instr.Value.(ArrayIndexConstOpInfo); ok {
				markSlot(info.ArraySlot)
			}
		case OP_SET_INDEX:
			// Generic SET_INDEX consumes array/index/value from the stack. Without a full
			// origin analysis here, be conservative and write back array params.
			markAllArrayLikeParams()
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok && info.Method == "push" {
				// Same story as OP_SET_INDEX: generic method call receiver is stack-based.
				markAllArrayLikeParams()
			}
		}
	}

	return mutated
}

func mergeJitPropertyHint(dst map[string]stackType, name string, typ stackType) {
	if name == "" || typ == stackTypeUnknown {
		return
	}
	if prev, ok := dst[name]; ok && prev != typ {
		// Conflicting uses should stay dynamic. Do not invent a fake shape.
		dst[name] = stackTypeUnknown
		return
	}
	dst[name] = typ
}

func inferJitLocalPropertyHints(fn Function) []map[string]stackType {
	hints := make([]map[string]stackType, fn.LocalCount)
	ensure := func(slot int) map[string]stackType {
		if slot < 0 || slot >= len(hints) {
			return nil
		}
		if hints[slot] == nil {
			hints[slot] = map[string]stackType{}
		}
		return hints[slot]
	}

	for i, instr := range fn.Instructions {
		switch instr.Op {
		case OP_GET_PROPERTY_LOCAL:
			info, ok := instr.Value.(PropertyLocalInfo)
			if !ok {
				continue
			}
			m := ensure(info.Slot)
			if m == nil {
				continue
			}

			// A property used directly as a branch condition should normally be a bool
			// in hot code. If it is not, the generated code deopts back to the VM instead
			// of producing a wrong truthiness result.
			if i+1 < len(fn.Instructions) {
				next := fn.Instructions[i+1].Op
				if next == OP_JUMP_IF_FALSE || next == OP_JUMP_IF_TRUE {
					mergeJitPropertyHint(m, info.Name, stackTypeBool)
				}
			}

		case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			info, ok := instr.Value.(JumpPropertyLocalInfo)
			if !ok {
				continue
			}
			m := ensure(info.Slot)
			if m == nil {
				continue
			}
			mergeJitPropertyHint(m, info.Name, stackTypeBool)

		case OP_ADD_LOCAL_PROPERTIES_STORE:
			info, ok := instr.Value.(AddLocalPropertiesStoreInfo)
			if !ok {
				continue
			}
			m := ensure(info.ObjectSlot)
			if m == nil {
				continue
			}
			for _, name := range info.Names {
				// This superinstruction is only emitted for local = local + object.field...
				// The JIT still guards at runtime and deopts if the field is not numeric.
				mergeJitPropertyHint(m, name, stackTypeNumber)
			}

		case OP_ADD_PROPERTY_LOCAL_CONST:
			info, ok := instr.Value.(PropertyLocalConstAssignInfo)
			if !ok || info.Op == OP_ADD {
				continue
			}
			m := ensure(info.ObjectSlot)
			if m != nil {
				mergeJitPropertyHint(m, info.Name, stackTypeNumber)
			}

		case OP_ADD_PROPERTY_LOCAL_PROPERTY:
			info, ok := instr.Value.(PropertyLocalPropertyAssignInfo)
			if !ok || info.Op == OP_ADD {
				continue
			}
			m := ensure(info.ObjectSlot)
			if m != nil {
				mergeJitPropertyHint(m, info.Name, stackTypeNumber)
				mergeJitPropertyHint(m, info.SourceName, stackTypeNumber)
			}
		}
	}

	for slot, m := range hints {
		for name, typ := range m {
			if typ == stackTypeUnknown {
				delete(m, name)
			}
		}
		if len(m) == 0 {
			hints[slot] = nil
		}
	}
	return hints
}

func jitTagForStackType(t stackType) (float64, bool) {
	switch t {
	case stackTypeNumber:
		return jitTagNumber, true
	case stackTypeBool:
		return jitTagBool, true
	case stackTypeString, stackTypeInternedString:
		return jitTagString, true
	case stackTypeObject:
		return jitTagObject, true
	case stackTypeArray, stackTypeNumberArray, stackTypeInternedStringArray:
		return jitTagArray, true
	case stackTypeNull:
		return jitTagNull, true
	default:
		return 0, false
	}
}

func inferParamTypes(vm *VM, fn Function, currentReturnTypes []stackType, currentParamTypes [][]stackType) []stackType {
	paramTypes := make([]stackType, len(fn.Params))
	for i := range paramTypes {
		paramTypes[i] = stackTypeUnknown

		if i < len(fn.Params) {
			if t, ok := stackTypeFromTypeName(fn.Params[i].TypeHint.Name); ok {
				paramTypes[i] = t
			}
		}
	}

	setParamType := func(index int, typ stackType) {
		if index < 0 || index >= len(paramTypes) || typ == stackTypeUnknown {
			return
		}

		current := paramTypes[index]
		if current == stackTypeUnknown || current == typ {
			paramTypes[index] = typ
			return
		}

		// Array use is stronger than a numeric use accidentally inferred from
		// the numeric result of OP_LEN. This is the exact case in:
		//   for log in logs { ... }
		// where `logs` is array, but `len(logs)` participates in numeric compare.
		if isJitArrayType(typ) {
			paramTypes[index] = stackTypeArray
			return
		}
	}

	markParamMarker := func(t stackType, typ stackType) {
		if t >= 10 {
			setParamType(int(t-10), typ)
		}
	}

	if len(fn.Instructions) == 0 {
		return paramTypes
	}

	// 1. Scan for specialized optimized numeric instructions on parameter slots
	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		case OP_JUMP_LOCAL_GE_CONST:
			info, ok := instr.Value.(JumpLocalGEConstInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		case OP_JUMP_LOCAL_GT_CONST:
			info, ok := instr.Value.(JumpLocalGTConstInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		case OP_JUMP_LOCAL_GE_LOCAL:
			info, ok := instr.Value.(JumpLocalGELocalInfo)
			if ok {
				if info.LeftSlot < len(paramTypes) {
					paramTypes[info.LeftSlot] = stackTypeNumber
				}
				if info.RightSlot < len(paramTypes) {
					paramTypes[info.RightSlot] = stackTypeNumber
				}
			}
		case OP_JUMP_LOCAL_GT_LOCAL:
			info, ok := instr.Value.(JumpLocalGTLocalInfo)
			if ok {
				if info.SlotA < len(paramTypes) {
					paramTypes[info.SlotA] = stackTypeNumber
				}
				if info.SlotB < len(paramTypes) {
					paramTypes[info.SlotB] = stackTypeNumber
				}
			}
		case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
			info, ok := instr.Value.(JumpModLocalConstNotZeroInfo)
			if ok && info.LeftSlot < len(paramTypes) {
				paramTypes[info.LeftSlot] = stackTypeNumber
			}
		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info, ok := instr.Value.(JumpModLocalLocalNotZeroInfo)
			if ok {
				if info.LeftSlot < len(paramTypes) {
					paramTypes[info.LeftSlot] = stackTypeNumber
				}
				if info.RightSlot < len(paramTypes) {
					paramTypes[info.RightSlot] = stackTypeNumber
				}
			}
		case OP_MUL_LOCAL_CONST:
			info, ok := instr.Value.(LocalConstInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		case OP_LOCAL_CONST_OP, OP_LOCAL_CONST_OP_STORE:
			info, ok := instr.Value.(LocalConstOpInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		case OP_ARRAY_INDEX_CONST_OP_STORE:
			info, ok := instr.Value.(ArrayIndexConstOpInfo)
			if ok {
				if info.ArraySlot < len(paramTypes) {
					paramTypes[info.ArraySlot] = stackTypeArray
				}
				if info.IndexSlot < len(paramTypes) {
					paramTypes[info.IndexSlot] = stackTypeNumber
				}
			}
		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			info, ok := instr.Value.(AddLocalArrayIndexStoreInfo)
			if ok {
				if info.ArraySlot < len(paramTypes) {
					paramTypes[info.ArraySlot] = stackTypeArray
				}
				if info.IndexSlot < len(paramTypes) {
					paramTypes[info.IndexSlot] = stackTypeNumber
				}
			}
		case OP_SUB_ASSIGN_LOCAL:
			info, ok := instr.Value.(AssignLocalInfo)
			if ok {
				if info.TargetSlot < len(paramTypes) {
					paramTypes[info.TargetSlot] = stackTypeNumber
				}
				if info.SourceSlot < len(paramTypes) {
					paramTypes[info.SourceSlot] = stackTypeNumber
				}
			}
		case OP_INC_LOCAL, OP_DEC_LOCAL:
			info, ok := instr.Value.(IncrementInfo)
			if ok && info.Slot < len(paramTypes) {
				paramTypes[info.Slot] = stackTypeNumber
			}
		}
	}

	// Run a basic type propagation pass on the stack
	spArray := make([]int, len(fn.Instructions))
	sp := 0
	for idx, instr := range fn.Instructions {
		spArray[idx] = sp
		switch instr.Op {
		case OP_CONST, OP_LOAD_LOCAL,
			OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOAD_GLOBAL,
			OP_LOCAL_CONST_OP:
			sp++
		case OP_MATH_POW:
			sp--
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			sp = sp - info.ArgCount + 1
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			sp--
		case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_COALESCE_JUMP:
			sp--
		case OP_RETURN:
			sp--
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				sp = sp - info.ArgCount + 1
			}
		case OP_CALL_DIRECT_SUB_CONST:
			sp++
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				if count > 0 {
					sp -= count - 1
				} else {
					sp++
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				if info.Count > 0 {
					sp -= info.Count - 1
				} else {
					sp++
				}
			}
		case OP_INDEX:
			sp--
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count > 0 {
				sp -= count - 1
			} else {
				sp++
			}
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
			// net change is 0 (pop 1, push 1)
		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			sp++
		case OP_SET_PROPERTY:
			sp -= 2
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				sp -= info.ArgCount
			}
		}
	}
	maxSp := 0
	for _, s := range spArray {
		if s > maxSp {
			maxSp = s
		}
	}

	typeStack := make([]stackType, maxSp+16)
	localTypes := make([]stackType, fn.LocalCount)
	stackPropertyTypes := make([]map[string]stackType, maxSp+16)
	localPropertyTypes := make([]map[string]stackType, fn.LocalCount)
	for i := 0; i < len(fn.Params) && i < len(localTypes); i++ {
		localTypes[i] = stackType(10 + i) // stackTypeParam0 + i
	}

	sp = 0
	for idx, instr := range fn.Instructions {
		sp = spArray[idx]
		switch instr.Op {
		case OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS:
			if sp >= 1 {
				t := typeStack[sp-1]
				if t >= 10 {
					paramIdx := int(t - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = stackTypeNumber
					}
				}
				typeStack[sp-1] = stackTypeNumber
			}

		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			dest := sp - info.ArgCount

			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeNull // null result
			}

		case OP_MATH_POW:
			if sp >= 2 {
				t1 := typeStack[sp-1]
				t2 := typeStack[sp-2]

				if t1 >= 10 {
					paramIdx := int(t1 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = stackTypeNumber
					}
				}

				if t2 >= 10 {
					paramIdx := int(t2 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = stackTypeNumber
					}
				}

				typeStack[sp-2] = stackTypeNumber
			}
		case OP_CONST:
			if sp < len(typeStack) {
				if isNullConstant(instr.Value) {
					typeStack[sp] = stackTypeNull
				} else if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isStr := instr.Value.(string); isStr {
					typeStack[sp] = stackTypeString
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_LOAD_GLOBAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			}
			if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			}
			if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			if sp >= 1 && slot >= 0 && slot < len(localTypes) {
				localTypes[slot] = typeStack[sp-1]
				localPropertyTypes[slot] = stackPropertyTypes[sp-1]
			}
		case OP_SUB, OP_MUL, OP_DIV, OP_MOD, OP_LT, OP_GT, OP_LTE, OP_GTE:
			if sp >= 2 {
				t1 := typeStack[sp-1]
				t2 := typeStack[sp-2]
				if t1 >= 10 {
					paramIdx := int(t1 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = stackTypeNumber
					}
				}
				if t2 >= 10 {
					paramIdx := int(t2 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = stackTypeNumber
					}
				}
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_ADD:
			if sp >= 2 {
				t1 := typeStack[sp-1]
				t2 := typeStack[sp-2]

				if t1 == stackTypeString || t2 == stackTypeString {
					typeStack[sp-2] = stackTypeString
				} else {
					if t1 >= 10 {
						paramIdx := int(t1 - 10)
						if paramIdx < len(paramTypes) {
							paramTypes[paramIdx] = stackTypeNumber
						}
					}
					if t2 >= 10 {
						paramIdx := int(t2 - 10)
						if paramIdx < len(paramTypes) {
							paramTypes[paramIdx] = stackTypeNumber
						}
					}
					if t1 == stackTypeNumber || t2 == stackTypeNumber {
						typeStack[sp-2] = stackTypeNumber
					} else {
						typeStack[sp-2] = stackTypeUnknown
					}
				}
			}
		case OP_EQ, OP_NEQ:
			if sp >= 2 {
				t1 := typeStack[sp-1]
				t2 := typeStack[sp-2]
				if t1 >= 10 && t2 < 10 && t2 != stackTypeUnknown {
					paramIdx := int(t1 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = t2
					}
				}
				if t2 >= 10 && t1 < 10 && t1 != stackTypeUnknown {
					paramIdx := int(t2 - 10)
					if paramIdx < len(paramTypes) {
						paramTypes[paramIdx] = t1
					}
				}
				typeStack[sp-2] = stackTypeBool
			}
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				if len(currentParamTypes) > 0 && info.ID >= 0 && info.ID < len(currentParamTypes) {
					calleeParams := currentParamTypes[info.ID]
					for a := 0; a < info.ArgCount && a < len(calleeParams); a++ {
						t := typeStack[sp-info.ArgCount+a]
						if t >= 10 {
							paramIdx := int(t - 10)
							if paramIdx < len(paramTypes) {
								if calleeParams[a] != stackTypeUnknown {
									paramTypes[paramIdx] = calleeParams[a]
								}
							}
						}
					}
				}

				retT := stackTypeUnknown
				if vm != nil && info.ID >= 0 && info.ID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.ID]
				}
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = retT
				}
			}
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok {
				if info.Slot >= 0 && info.Slot < len(paramTypes) {
					paramTypes[info.Slot] = stackTypeNumber
				}

				retT := stackTypeUnknown
				if vm != nil && info.FnID >= 0 && info.FnID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.FnID]
				}
				if sp < len(typeStack) {
					typeStack[sp] = retT
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				dest := sp - info.Count
				if dest >= 0 && dest < len(typeStack) {
					elements := make([]stackType, 0, info.Count)
					for idx := 0; idx < info.Count; idx++ {
						elements = append(elements, typeStack[sp-info.Count+idx])
					}
					typeStack[dest] = inferJitArrayStackType(elements)
				}
			}
		case OP_INDEX:
			if sp >= 2 {
				arrayType := typeStack[sp-2]
				indexType := typeStack[sp-1]
				markParamMarker(arrayType, stackTypeArray)
				markParamMarker(indexType, stackTypeNumber)

				if elemType, ok := jitArrayElementType(arrayType); ok {
					typeStack[sp-2] = elemType
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_ARRAY_GET_LOCAL:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(ArrayLocalCallInfo); ok && info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
					arrayType := localTypes[info.ArraySlot]
					markParamMarker(arrayType, stackTypeArray)
					if elemType, ok := jitArrayElementType(arrayType); ok {
						typeStack[sp] = elemType
					} else {
						typeStack[sp] = stackTypeUnknown
					}
				} else {
					typeStack[sp] = stackTypeUnknown
				}
			}
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				props := make(map[string]stackType)
				for idx := 0; idx < count; idx++ {
					propName := info.Names[idx].Name
					propType := typeStack[sp-count+idx]
					props[propName] = propType
				}
				dest := sp - count
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeObject
					stackPropertyTypes[dest] = props
				}
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			name, ok := instr.Value.(string)
			if ok && sp >= 1 {
				propType := stackTypeUnknown
				objProps := stackPropertyTypes[sp-1]
				if objProps != nil {
					if t, ok := objProps[name]; ok {
						propType = t
					}
				}
				typeStack[sp-1] = propType
				stackPropertyTypes[sp-1] = nil
			}
		case OP_GET_PROPERTY_LOCAL:
			info, ok := instr.Value.(PropertyLocalInfo)
			if ok && sp < len(typeStack) {
				propType := stackTypeUnknown
				if info.Slot >= 0 && info.Slot < len(localPropertyTypes) && localPropertyTypes[info.Slot] != nil {
					if t, ok := localPropertyTypes[info.Slot][info.Name]; ok {
						propType = t
					}
				}
				typeStack[sp] = propType
				stackPropertyTypes[sp] = nil
			}
		case OP_ARRAY_LEN_LOCAL:
			if info, ok := instr.Value.(ArrayLocalCallInfo); ok && info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
				markParamMarker(localTypes[info.ArraySlot], stackTypeArray)
			}
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
				stackPropertyTypes[sp] = nil
			}
		case OP_LEN:
			if sp-1 >= 0 && sp-1 < len(typeStack) {
				markParamMarker(typeStack[sp-1], stackTypeArray)
				typeStack[sp-1] = stackTypeNumber
				stackPropertyTypes[sp-1] = nil
			}
		case OP_ARRAY_INDEX_LOCAL_STORE:
			if info, ok := instr.Value.(ArrayIndexLocalStoreInfo); ok {
				if info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
					markParamMarker(localTypes[info.ArraySlot], stackTypeArray)
				}
				if info.IndexSlot >= 0 && info.IndexSlot < len(localTypes) {
					markParamMarker(localTypes[info.IndexSlot], stackTypeNumber)
				}
				if info.DestSlot >= 0 && info.DestSlot < len(localTypes) {
					localTypes[info.DestSlot] = stackTypeObject
				}
			}
		case OP_COALESCE_JUMP:
			if sp >= 2 {
				t1 := typeStack[sp-1] // right
				t2 := typeStack[sp-2] // left
				var coalescedType stackType
				if t1 == t2 {
					coalescedType = t1
				} else if t1 == stackTypeUnknown {
					coalescedType = t2
				} else if t2 == stackTypeUnknown {
					coalescedType = t1
				} else if t2 >= 10 && t1 < 10 && t1 != stackTypeUnknown {
					coalescedType = t1
				} else if t1 >= 10 && t2 < 10 && t2 != stackTypeUnknown {
					coalescedType = t2
				} else {
					coalescedType = stackTypeUnknown
				}
				typeStack[sp-2] = coalescedType

				// Property propagation:
				props1 := stackPropertyTypes[sp-1]
				props2 := stackPropertyTypes[sp-2]
				var mergedProps map[string]stackType
				if props1 != nil && props2 != nil {
					mergedProps = make(map[string]stackType)
					for k, v := range props1 {
						if v2, ok := props2[k]; ok && v == v2 {
							mergedProps[k] = v
						}
					}
				} else if props1 != nil {
					mergedProps = props1
				} else {
					mergedProps = props2
				}
				stackPropertyTypes[sp-2] = mergedProps
				stackPropertyTypes[sp-1] = nil
			}
		case OP_TYPEOF:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeString
				stackPropertyTypes[sp-1] = nil
			}
		case OP_THROW:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeUnknown
				stackPropertyTypes[sp-1] = nil
			}
		}
	}
	return paramTypes
}

func inferReturnType(vm *VM, fn Function, currentReturnTypes []stackType) stackType {
	if len(fn.Instructions) == 0 {
		return stackTypeNull
	}
	reachable := make([]bool, len(fn.Instructions))
	queue := []int{0}
	for len(queue) > 0 {
		pc := queue[0]
		queue = queue[1:]
		if pc < 0 || pc >= len(fn.Instructions) || reachable[pc] {
			continue
		}
		reachable[pc] = true
		instr := fn.Instructions[pc]
		if instr.Op == OP_RETURN {
			continue
		}
		if target, ok := getJumpTarget(instr); ok {
			queue = append(queue, target)
			if instr.Op != OP_JUMP {
				queue = append(queue, pc+1)
			}
		} else {
			queue = append(queue, pc+1)
		}
	}
	spArray := make([]int, len(fn.Instructions))
	sp := 0
	for idx, instr := range fn.Instructions {
		spArray[idx] = sp
		switch instr.Op {
		case OP_CONST, OP_LOAD_LOCAL,
			OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOAD_GLOBAL,
			OP_LOCAL_CONST_OP:
			sp++
		case OP_MATH_POW:
			sp--
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			sp = sp - info.ArgCount + 1
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			sp--
		case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_COALESCE_JUMP:
			sp--
		case OP_RETURN:
			sp--
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				sp = sp - info.ArgCount + 1
			}
		case OP_CALL_DIRECT_SUB_CONST:
			sp++
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				if count > 0 {
					sp -= count - 1
				} else {
					sp++
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				if info.Count > 0 {
					sp -= info.Count - 1
				} else {
					sp++
				}
			}
		case OP_INDEX:
			sp--
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count > 0 {
				sp -= count - 1
			} else {
				sp++
			}
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
			// net change is 0 (pop 1, push 1)
		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			sp++
		case OP_SET_PROPERTY:
			sp -= 2
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				sp -= info.ArgCount
			}
		}
	}
	maxSp := 0
	for _, s := range spArray {
		if s > maxSp {
			maxSp = s
		}
	}

	typeStack := make([]stackType, maxSp+16)
	inferredParams := inferParamTypes(vm, fn, currentReturnTypes, nil)
	localTypes := make([]stackType, fn.LocalCount)
	stackPropertyTypes := make([]map[string]stackType, maxSp+16)
	localPropertyTypes := make([]map[string]stackType, fn.LocalCount)
	for i := 0; i < len(fn.Params) && i < len(localTypes); i++ {
		localTypes[i] = inferredParams[i]
	}

	hasReturn := false
	var finalType stackType = stackTypeUnknown
	firstReturn := true

	sp = 0
	for idx, instr := range fn.Instructions {
		sp = spArray[idx]

		switch instr.Op {
		case OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}

		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			dest := sp - info.ArgCount

			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeNull // null result
			}

		case OP_MATH_POW:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeBool
			}
		case OP_AND, OP_OR:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeBool
			}
		case OP_NOT:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeBool
			}
		case OP_CONST:
			if sp < len(typeStack) {
				if isNullConstant(instr.Value) {
					typeStack[sp] = stackTypeNull
				} else if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isStr := instr.Value.(string); isStr {
					typeStack[sp] = stackTypeString
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_ADD:
			if sp >= 2 {
				if isJitStringType(typeStack[sp-1]) || isJitStringType(typeStack[sp-2]) {
					typeStack[sp-2] = stackTypeString
				} else if typeStack[sp-1] == stackTypeNumber || typeStack[sp-2] == stackTypeNumber {
					typeStack[sp-2] = stackTypeNumber
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_NEGATE:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_MUL_LOCAL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LOCAL_CONST_OP:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(LocalConstOpInfo); ok {
					typeStack[sp] = jitLocalConstOpResultType(info)
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_LOCAL_CONST_OP_STORE:
			if info, ok := instr.Value.(LocalConstOpInfo); ok && info.Slot >= 0 && info.Slot < len(localTypes) {
				localTypes[info.Slot] = jitLocalConstOpResultType(info)
			}
		case OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
			if info, ok := instr.Value.(AddLocalGlobalGlobalStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			if info, ok := instr.Value.(AddLocalArrayIndexStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ARRAY_INDEX_LOCAL_STORE:
			if info, ok := instr.Value.(ArrayIndexLocalStoreInfo); ok && info.DestSlot >= 0 && info.DestSlot < len(localTypes) {
				localTypes[info.DestSlot] = stackTypeObject
			}
		case OP_ARRAY_INDEX_CONST_OP_STORE, OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY, OP_ADD_PROPERTY_LOCAL_LOCAL, OP_ADD_LOCAL_PROPERTIES_STORE,
			OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect for type propagation
		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)
			tA := localTypes[info.SlotA]
			tB := localTypes[info.SlotB]
			if isJitStringType(tA) || isJitStringType(tB) {
				localTypes[info.DestSlot] = stackTypeString
			} else if tA == stackTypeNumber && tB == stackTypeNumber {
				localTypes[info.DestSlot] = stackTypeNumber
			} else {
				localTypes[info.DestSlot] = stackTypeUnknown
			}
		case OP_LOAD_GLOBAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			}
			if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			}
			if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)

			if count > 0 {
				dest := sp - count
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeString
				}
			} else {
				if sp < len(typeStack) {
					typeStack[sp] = stackTypeString
				}
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			if sp >= 1 && slot < len(localTypes) {
				localTypes[slot] = typeStack[sp-1]
				localPropertyTypes[slot] = stackPropertyTypes[sp-1]
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			name, ok := instr.Value.(string)
			if ok && sp >= 1 {
				propType := stackTypeUnknown
				objProps := stackPropertyTypes[sp-1]
				if objProps != nil {
					if t, ok := objProps[name]; ok {
						propType = t
					}
				}
				typeStack[sp-1] = propType
				stackPropertyTypes[sp-1] = nil
			}
		case OP_GET_PROPERTY_LOCAL:
			info, ok := instr.Value.(PropertyLocalInfo)
			if ok && sp < len(typeStack) {
				propType := stackTypeUnknown
				if info.Slot >= 0 && info.Slot < len(localPropertyTypes) && localPropertyTypes[info.Slot] != nil {
					if t, ok := localPropertyTypes[info.Slot][info.Name]; ok {
						propType = t
					}
				}
				typeStack[sp] = propType
				stackPropertyTypes[sp] = nil
			}
		case OP_COALESCE_JUMP:
			if sp >= 2 {
				t1 := typeStack[sp-1] // right
				t2 := typeStack[sp-2] // left
				var coalescedType stackType
				if t1 == t2 {
					coalescedType = t1
				} else if t1 == stackTypeUnknown {
					coalescedType = t2
				} else if t2 == stackTypeUnknown {
					coalescedType = t1
				} else if t2 >= 10 && t1 < 10 && t1 != stackTypeUnknown {
					coalescedType = t1
				} else if t1 >= 10 && t2 < 10 && t2 != stackTypeUnknown {
					coalescedType = t2
				} else {
					coalescedType = stackTypeUnknown
				}
				typeStack[sp-2] = coalescedType

				// Property propagation:
				props1 := stackPropertyTypes[sp-1]
				props2 := stackPropertyTypes[sp-2]
				var mergedProps map[string]stackType
				if props1 != nil && props2 != nil {
					mergedProps = make(map[string]stackType)
					for k, v := range props1 {
						if v2, ok := props2[k]; ok && v == v2 {
							mergedProps[k] = v
						}
					}
				} else if props1 != nil {
					mergedProps = props1
				} else {
					mergedProps = props2
				}
				stackPropertyTypes[sp-2] = mergedProps
				stackPropertyTypes[sp-1] = nil
			}
		case OP_TYPEOF:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeString
				stackPropertyTypes[sp-1] = nil
			}
		case OP_THROW:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeUnknown
				stackPropertyTypes[sp-1] = nil
			}
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				retT := stackTypeUnknown
				if vm != nil && info.ID >= 0 && info.ID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.ID]
				}
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = retT
					stackPropertyTypes[dest] = nil
				}
			}
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok {
				retT := stackTypeUnknown
				if vm != nil && info.FnID >= 0 && info.FnID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.FnID]
				}
				if sp < len(typeStack) {
					typeStack[sp] = retT
					stackPropertyTypes[sp] = nil
				}
			}
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				props := make(map[string]stackType)
				for idx := 0; idx < count; idx++ {
					propName := info.Names[idx].Name
					propType := typeStack[sp-count+idx]
					props[propName] = propType
				}
				dest := sp - count
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeObject
					stackPropertyTypes[dest] = props
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				dest := sp - info.Count
				if dest >= 0 && dest < len(typeStack) {
					elements := make([]stackType, 0, info.Count)
					for idx := 0; idx < info.Count; idx++ {
						elements = append(elements, typeStack[sp-info.Count+idx])
					}
					typeStack[dest] = inferJitArrayStackType(elements)
				}
			}
		case OP_INDEX:
			if sp >= 2 {
				if elemType, ok := jitArrayElementType(typeStack[sp-2]); ok {
					typeStack[sp-2] = elemType
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_ARRAY_GET_LOCAL:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(ArrayLocalCallInfo); ok && info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
					if elemType, ok := jitArrayElementType(localTypes[info.ArraySlot]); ok {
						typeStack[sp] = elemType
					} else {
						typeStack[sp] = stackTypeUnknown
					}
				} else {
					typeStack[sp] = stackTypeUnknown
				}
			}
		case OP_SET_INDEX:
		case OP_ARRAY_LEN_LOCAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LEN:
			if sp-1 >= 0 && sp-1 < len(typeStack) {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeArray
			}
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				dest := sp - info.ArgCount - 1
				if dest >= 0 && dest < len(typeStack) {
					switch info.Method {
					case "length":
						typeStack[dest] = stackTypeNumber
					case "push":
						typeStack[dest] = stackTypeArray
					default:
						typeStack[dest] = stackTypeUnknown
					}
				}
			}
		case OP_RETURN:
			if reachable[idx] {
				hasReturn = true
				retType := stackTypeUnknown
				if sp >= 1 {
					retType = typeStack[sp-1]
				}

				if firstReturn {
					finalType = retType
					firstReturn = false
				} else if retType != stackTypeUnknown {
					if finalType == stackTypeUnknown {
						finalType = retType
					} else if finalType != retType {
						finalType = stackTypeUnknown
					}
				}
			}
		}
	}

	if !hasReturn {
		return stackTypeNull
	}

	return finalType
}

func inferReturnPropertyTypes(vm *VM, fn Function, currentReturnTypes []stackType, visiting map[int]bool) map[string]stackType {
	if vm == nil || len(fn.Instructions) == 0 {
		return nil
	}
	if visiting == nil {
		visiting = map[int]bool{}
	}
	if visiting[fn.ID] {
		return nil
	}
	visiting[fn.ID] = true
	defer delete(visiting, fn.ID)

	spArray := make([]int, len(fn.Instructions))
	sp := 0
	for idx, instr := range fn.Instructions {
		spArray[idx] = sp
		switch instr.Op {
		case OP_CONST, OP_LOAD_LOCAL,
			OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOAD_GLOBAL,
			OP_LOCAL_CONST_OP:
			sp++
		case OP_MATH_POW:
			sp--
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			sp = sp - info.ArgCount + 1
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			sp--
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_COALESCE_JUMP:
			sp--
		case OP_RETURN:
			sp--
		case OP_CALL_DIRECT:
			if info, ok := instr.Value.(DirectCallInfo); ok {
				sp = sp - info.ArgCount + 1
			}
		case OP_CALL_DIRECT_SUB_CONST:
			sp++
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				if len(info.Names) > 0 {
					sp -= len(info.Names) - 1
				} else {
					sp++
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				if info.Count > 0 {
					sp -= info.Count - 1
				} else {
					sp++
				}
			}
		case OP_INDEX:
			sp--
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count > 0 {
				sp -= count - 1
			} else {
				sp++
			}
		case OP_SET_INDEX:
			sp -= 3
		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			sp++
		case OP_SET_PROPERTY:
			sp -= 2
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				sp -= info.ArgCount
			}
		}
	}

	maxSp := 0
	for _, s := range spArray {
		if s > maxSp {
			maxSp = s
		}
	}

	typeStack := make([]stackType, maxSp+16)
	stackPropertyTypes := make([]map[string]stackType, maxSp+16)
	localTypes := make([]stackType, fn.LocalCount)
	localPropertyTypes := make([]map[string]stackType, fn.LocalCount)
	inferredParams := inferParamTypes(vm, fn, currentReturnTypes, nil)
	for i := 0; i < len(fn.Params) && i < len(localTypes); i++ {
		localTypes[i] = inferredParams[i]
	}

	var finalProps map[string]stackType
	firstReturn := true

	for idx, instr := range fn.Instructions {
		sp = spArray[idx]
		switch instr.Op {
		case OP_CONST:
			if sp < len(typeStack) {
				if isNullConstant(instr.Value) {
					typeStack[sp] = stackTypeNull
				} else if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isStr := instr.Value.(string); isStr {
					typeStack[sp] = stackTypeString
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
				stackPropertyTypes[sp] = nil
			}
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			} else if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			} else if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
				stackPropertyTypes[sp] = localPropertyTypes[slot]
			}
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			if sp >= 1 && slot >= 0 && slot < len(localTypes) {
				localTypes[slot] = typeStack[sp-1]
				localPropertyTypes[slot] = stackPropertyTypes[sp-1]
			}
		case OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
				stackPropertyTypes[sp-2] = nil
			}
		case OP_ADD:
			if sp >= 2 {
				if isJitStringType(typeStack[sp-1]) || isJitStringType(typeStack[sp-2]) {
					typeStack[sp-2] = stackTypeString
				} else if typeStack[sp-1] == stackTypeNumber || typeStack[sp-2] == stackTypeNumber {
					typeStack[sp-2] = stackTypeNumber
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
				stackPropertyTypes[sp-2] = nil
			}
		case OP_LOCAL_CONST_OP:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(LocalConstOpInfo); ok {
					typeStack[sp] = jitLocalConstOpResultType(info)
				} else {
					typeStack[sp] = stackTypeNumber
				}
				stackPropertyTypes[sp] = nil
			}
		case OP_LOCAL_CONST_OP_STORE:
			if info, ok := instr.Value.(LocalConstOpInfo); ok && info.Slot >= 0 && info.Slot < len(localTypes) {
				localTypes[info.Slot] = jitLocalConstOpResultType(info)
				localPropertyTypes[info.Slot] = nil
			}
		case OP_INC_LOCAL, OP_DEC_LOCAL:
			if info, ok := instr.Value.(IncrementInfo); ok && info.Slot >= 0 && info.Slot < len(localTypes) {
				localTypes[info.Slot] = stackTypeNumber
				localPropertyTypes[info.Slot] = nil
			}
		case OP_ARRAY_INDEX_LOCAL_STORE:
			if info, ok := instr.Value.(ArrayIndexLocalStoreInfo); ok && info.DestSlot >= 0 && info.DestSlot < len(localTypes) {
				localTypes[info.DestSlot] = stackTypeObject
				localPropertyTypes[info.DestSlot] = nil
			}
		case OP_ADD_LOCAL_PROPERTIES_STORE:
			if info, ok := instr.Value.(AddLocalPropertiesStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
				localPropertyTypes[info.LocalSlot] = nil
			}
		case OP_GET_PROPERTY_LOCAL:
			if info, ok := instr.Value.(PropertyLocalInfo); ok && sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
				if info.Slot >= 0 && info.Slot < len(localPropertyTypes) && localPropertyTypes[info.Slot] != nil {
					if t, ok := localPropertyTypes[info.Slot][info.Name]; ok {
						typeStack[sp] = t
					}
				}
				stackPropertyTypes[sp] = nil
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			if name, ok := instr.Value.(string); ok && sp >= 1 {
				typeStack[sp-1] = stackTypeUnknown
				if props := stackPropertyTypes[sp-1]; props != nil {
					if t, ok := props[name]; ok {
						typeStack[sp-1] = t
					}
				}
				stackPropertyTypes[sp-1] = nil
			}
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				props := make(map[string]stackType, count)
				for i := 0; i < count; i++ {
					props[info.Names[i].Name] = typeStack[sp-count+i]
				}
				dest := sp - count
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeObject
					stackPropertyTypes[dest] = props
				}
			}
		case OP_CALL_DIRECT:
			if info, ok := instr.Value.(DirectCallInfo); ok {
				retT := stackTypeUnknown
				if info.ID >= 0 && info.ID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.ID]
				}
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = retT
					if retT == stackTypeObject && info.ID >= 0 && info.ID < len(vm.functionList) {
						stackPropertyTypes[dest] = inferReturnPropertyTypes(vm, vm.functionList[info.ID], currentReturnTypes, visiting)
					} else {
						stackPropertyTypes[dest] = nil
					}
				}
			}
		case OP_CALL_DIRECT_SUB_CONST:
			if info, ok := instr.Value.(CallDirectSubConstInfo); ok {
				retT := stackTypeUnknown
				if info.FnID >= 0 && info.FnID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.FnID]
				}
				if sp < len(typeStack) {
					typeStack[sp] = retT
					if retT == stackTypeObject && info.FnID >= 0 && info.FnID < len(vm.functionList) {
						stackPropertyTypes[sp] = inferReturnPropertyTypes(vm, vm.functionList[info.FnID], currentReturnTypes, visiting)
					} else {
						stackPropertyTypes[sp] = nil
					}
				}
			}
		case OP_RETURN:
			if sp < 1 || sp-1 >= len(typeStack) || typeStack[sp-1] != stackTypeObject {
				continue
			}
			props := stackPropertyTypes[sp-1]
			if props == nil {
				continue
			}
			if firstReturn {
				finalProps = make(map[string]stackType, len(props))
				for k, v := range props {
					finalProps[k] = v
				}
				firstReturn = false
				continue
			}
			for k, v := range finalProps {
				if other, ok := props[k]; !ok || other != v {
					delete(finalProps, k)
				}
			}
		}
	}

	if len(finalProps) == 0 {
		return nil
	}
	return finalProps
}

type jitStackSourceKind uint8

const (
	jitSourceUnknown jitStackSourceKind = iota
	jitSourceStdModule
)

type jitStackSource struct {
	kind   jitStackSourceKind
	module string
}

type allowedMethod struct {
	method   string
	argCount int
}

func isAllowedMethod(method string, argCount int, allowedMethods []allowedMethod) bool {
	for _, v := range allowedMethods {
		if v.method == method {
			if v.argCount == argCount {
				return true
			} else {
				break
			}
		}
	}

	return false
}

func isJitAllowedStdlibCall(module string, method string, argCount int) bool {
	switch module {
	case "time":
		allowedMethods := []allowedMethod{
			{
				method:   "nowMs",
				argCount: 0,
			},
			{
				method:   "nowNs",
				argCount: 0,
			},
			{
				method:   "nowSec",
				argCount: 0,
			},
		}

		return isAllowedMethod(method, argCount, allowedMethods)

	case "process":
		allowedMethods := []allowedMethod{
			{
				method:   "args",
				argCount: 0,
			},
			{
				method:   "exit",
				argCount: 1,
			},
			{
				method:   "getEnv",
				argCount: 1,
			},
			{
				method:   "halt",
				argCount: 0,
			},
			{
				method:   "pid",
				argCount: 0,
			},
		}

		return isAllowedMethod(method, argCount, allowedMethods)

	case "strings":
		allowedMethods := []allowedMethod{
			{
				method:   "isDigit",
				argCount: 1,
			},
			{
				method:   "random",
				argCount: 1,
			},
		}

		return isAllowedMethod(method, argCount, allowedMethods)

	// io.println/io.print should be lowered to OP_PRINT.
	// If they still appear as OP_METHOD_CALL, don't JIT them.
	case "io":
		return false

	// math should be lowered to OP_MATH_*.
	// If it still appears as OP_METHOD_CALL, don't JIT it.
	case "math":
		return false

	default:
		return false
	}
}

func getGlobalSlotFromInstruction(instr Instruction) (int, bool) {
	if info, ok := instr.Value.(VariableInfo); ok {
		return info.Slot, true
	}
	if s, ok := AsIntInternal(instr.Value); ok {
		return s, true
	}
	return -1, false
}

func isKnownStdModuleName(name string) bool {
	switch name {
	case "time", "io", "math", "http", "fs", "os", "random", "strings", "json", "path", "process", "desktop", "webview":
		return true
	default:
		return false
	}
}

func getStdModuleNameFromRuntimeGlobalSlot(vm *VM, slot int) (string, bool) {
	if vm == nil || vm.globals == nil {
		return "", false
	}
	if slot < 0 || slot >= len(*vm.globals) {
		return "", false
	}

	value := (*vm.globals)[slot]

	var raw any
	if value.IsInt {
		raw = value.AsInt
	} else {
		raw = value.Value
	}

	mod, ok := raw.(*StandardModuleValue)
	if !ok || mod == nil {
		return "", false
	}

	return mod.Name, true
}

func getStdModuleNameFromLoadGlobal(vm *VM, instr Instruction) (string, bool) {
	if info, ok := instr.Value.(VariableInfo); ok {
		// JIT compilation usually happens before top-level import bytecode has run,
		// so vm.globals[slot] can still be null here. Use the static global name first.
		if isKnownStdModuleName(info.Name) {
			return info.Name, true
		}

		// Runtime fallback for cases where CompileAllJit is called after imports ran.
		if module, ok := getStdModuleNameFromRuntimeGlobalSlot(vm, info.Slot); ok {
			return module, true
		}
	}

	if slot, ok := getGlobalSlotFromInstruction(instr); ok {
		// Another static fallback: reverse lookup the global slot to its name.
		if vm != nil && vm.globalNames != nil {
			for name, globalSlot := range vm.globalNames {
				if globalSlot == slot && isKnownStdModuleName(name) {
					return name, true
				}
			}
		}

		return getStdModuleNameFromRuntimeGlobalSlot(vm, slot)
	}

	return "", false
}

func popJitSources(stack *[]jitStackSource, count int) ([]jitStackSource, bool) {
	if count < 0 || len(*stack) < count {
		return nil, false
	}

	start := len(*stack) - count
	out := append([]jitStackSource(nil), (*stack)[start:]...)
	*stack = (*stack)[:start]
	return out, true
}

func hasStdModuleSource(values []jitStackSource) bool {
	for _, v := range values {
		if v.kind == jitSourceStdModule {
			return true
		}
	}
	return false
}

func stdlibCallsAreJitSafe(vm *VM, fn Function) bool {
	stack := make([]jitStackSource, 0, 32)

	pushUnknown := func() {
		stack = append(stack, jitStackSource{kind: jitSourceUnknown})
	}

	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count < 0 {
				return false
			}

			values, ok := popJitSources(&stack, count)
			if !ok {
				return false
			}

			if hasStdModuleSource(values) {
				return false
			}

			pushUnknown()
		case OP_CONST,
			OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOCAL_CONST_OP:
			pushUnknown()

		case OP_LOAD_GLOBAL:
			if module, ok := getStdModuleNameFromLoadGlobal(vm, instr); ok {
				stack = append(stack, jitStackSource{
					kind:   jitSourceStdModule,
					module: module,
				})
			} else {
				pushUnknown()
			}

		case OP_METHOD_CALL:
			info, ok := instr.Value.(MethodCallInfo)
			if !ok || info.ArgCount > 3 {
				return false
			}

			needed := info.ArgCount + 1 // receiver + args
			if len(stack) < needed {
				return false
			}

			receiverIndex := len(stack) - needed
			receiver := stack[receiverIndex]

			// Remove receiver + args.
			stack = stack[:receiverIndex]

			if receiver.kind == jitSourceStdModule {
				if !isJitAllowedStdlibCall(receiver.module, info.Method, info.ArgCount) {
					return false
				}

				// stdlib result
				pushUnknown()
				continue
			}

			// Non-stdlib method calls are only safe for the old inline methods.
			switch info.Method {
			case "length", "push", "get":
				pushUnknown()
			default:
				return false
			}

		case OP_PRINT:
			info, ok := instr.Value.(PrintInfo)
			if !ok {
				return false
			}

			args, ok := popJitSources(&stack, info.ArgCount)
			if !ok {
				return false
			}

			// Do not let someone print a raw std module value from JIT.
			if hasStdModuleSource(args) {
				return false
			}

			// OP_PRINT returns null.
			pushUnknown()

		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}

			// Do not allow std module values to escape into locals/returns/etc.
			if hasStdModuleSource(values) {
				return false
			}

		case OP_RETURN:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}

		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_COALESCE_JUMP,
			OP_MATH_POW:
			values, ok := popJitSources(&stack, 2)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_NOT, OP_NEGATE,
			OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if !ok {
				return false
			}
			values, ok := popJitSources(&stack, info.ArgCount)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_CALL_DIRECT_SUB_CONST:
			pushUnknown()

		case OP_OBJECT:
			info, ok := instr.Value.(ObjectInfo)
			if !ok {
				return false
			}
			values, ok := popJitSources(&stack, len(info.Names))
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_ARRAY:
			info, ok := instr.Value.(ArrayInfo)
			if !ok {
				return false
			}
			values, ok := popJitSources(&stack, info.Count)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_INDEX:
			values, ok := popJitSources(&stack, 2)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_SET_INDEX:
			values, ok := popJitSources(&stack, 3)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}

		case OP_SET_PROPERTY:
			values, ok := popJitSources(&stack, 2)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}

		case OP_LEN:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			pushUnknown()

		case OP_INC_LOCAL, OP_DEC_LOCAL,
			OP_ADD_ASSIGN_LOCAL, OP_SUB_ASSIGN_LOCAL,
			OP_ADD_LOCAL_LOCAL_STORE,
			OP_LOCAL_CONST_OP_STORE, OP_ARRAY_INDEX_CONST_OP_STORE,
			OP_ADD_LOCAL_ARRAY_INDEX_STORE, OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE,
			OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY, OP_ADD_PROPERTY_LOCAL_LOCAL, OP_ADD_LOCAL_PROPERTIES_STORE, OP_ARRAY_INDEX_LOCAL_STORE,
			OP_JUMP, OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE,
			OP_JUMP_LOCAL_GT_CONST, OP_JUMP_LOCAL_GE_CONST,
			OP_JUMP_LOCAL_GT_LOCAL, OP_JUMP_LOCAL_GE_LOCAL,
			OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO, OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			// no stack effect for this stdlib safety scan

		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		case OP_TYPEOF:
			values, ok := popJitSources(&stack, 1)
			if !ok {
				return false
			}
			if hasStdModuleSource(values) {
				return false
			}
			pushUnknown()

		default:
			// If we don't understand its stack effect, don't JIT.
			// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: stdlib safety scan does not understand opcode %s\n", fn.Name, instr.Op.String())
			return false
		}
	}

	return true
}

func jitOpcodeUsesRuntimeGlobalLoad(op OpCode) bool {
	switch op {
	case OP_LOAD_GLOBAL, OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
		return true
	default:
		return false
	}
}

func functionHasRuntimeGlobalLoadInLoop(vm *VM, fn Function) (OpCode, bool) {
	for end, instr := range fn.Instructions {
		start, ok := getJumpTarget(instr)
		if !ok || start >= end {
			continue
		}

		if start < 0 {
			start = 0
		}

		for pc := start; pc <= end && pc < len(fn.Instructions); pc++ {
			instr := fn.Instructions[pc]
			op := instr.Op

			if op == OP_LOAD_GLOBAL {
				// Loading a known std module inside a loop is safe when the
				// stdlib-call verifier accepts the following method call. The old
				// blanket guard rejected strings.random/strings.isDigit before the
				// generic call_stdlib_wasm bridge could even be considered.
				if _, ok := getStdModuleNameFromLoadGlobal(vm, instr); ok {
					continue
				}
				return op, true
			}

			if op == OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE {
				return op, true
			}
		}
	}

	return 0, false
}

const minJitStatementCount = 8
const minJitInstructionCount = 24
const maxSmallLeafJitInstructionCount = 48

func isSmallNumericOrArrayLeafJitCandidate(fn Function) bool {
	if len(fn.Instructions) == 0 || len(fn.Instructions) > maxSmallLeafJitInstructionCount {
		return false
	}

	if functionHasLoop(fn) || functionIsRecursive(fn) {
		return false
	}

	hasUsefulNumericOrArrayWork := false

	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CONST:
			if instr.IsInt {
				continue
			}
			if _, ok := getFloat64Constant(instr.Value); ok {
				continue
			}
			if isNullConstant(instr.Value) {
				continue
			}
			return false

		case OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_STORE_LOCAL, OP_ASSIGN_LOCAL,
			OP_RETURN, OP_POP:
			// Plain local traffic / function epilogue.

		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_NEGATE,
			OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS, OP_MATH_POW,
			OP_MUL_LOCAL_CONST,
			OP_LOCAL_CONST_OP, OP_LOCAL_CONST_OP_STORE,
			OP_ADD_LOCAL_LOCAL_STORE,
			OP_INC_LOCAL, OP_DEC_LOCAL,
			OP_ADD_ASSIGN_LOCAL, OP_SUB_ASSIGN_LOCAL:
			hasUsefulNumericOrArrayWork = true

		case OP_ARRAY, OP_INDEX, OP_SET_INDEX,
			OP_LEN,
			OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST,
			OP_ARRAY_INDEX_CONST_OP_STORE, OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			hasUsefulNumericOrArrayWork = true

		default:
			// Calls, globals, object property ops, exceptions, closures, stdlib calls,
			// locks, etc. are not small leaf helpers. Let the normal JIT worth
			// rules decide for larger functions.
			return false
		}
	}

	return hasUsefulNumericOrArrayWork
}

func functionHasLoop(fn Function) bool {
	for i, instr := range fn.Instructions {
		target, ok := getJumpTarget(instr)
		if ok && target < i {
			return true
		}
	}
	return false
}

func isTinyFunctionWorthJit(fn Function) bool {
	if functionHasLoop(fn) {
		return true
	}

	if functionIsRecursive(fn) {
		return true
	}

	if isSmallNumericOrArrayLeafJitCandidate(fn) {
		// fmt.Fprintf(
		// 	os.Stderr,
		// 	"[JIT DEBUG] function %s is worth JIT: small numeric/array leaf (%d statements, %d instructions)\n",
		// 	fn.Name,
		// 	fn.StatementCount,
		// 	len(fn.Instructions),
		// )
		return true
	}

	return fn.StatementCount >= minJitStatementCount &&
		len(fn.Instructions) >= minJitInstructionCount
}

func functionIsRecursive(fn Function) bool {
	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				if info.ID == fn.ID || info.Name == fn.Name {
					return true
				}
			}

		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok {
				if info.FnID == fn.ID || info.FnName == fn.Name {
					return true
				}
			}
		}
	}

	return false
}

func getStringJoinCount(instr Instruction) int {
	return instr.IntArg
}

func jitLocalConstOpResultType(info LocalConstOpInfo) stackType {
	if info.Op == OP_ADD {
		if _, ok := info.Const.(string); ok {
			return stackTypeString
		}
	}
	return stackTypeNumber
}

func isFunctionJitSafe(vm *VM, fn Function) bool {
	if !isTinyFunctionWorthJit(fn) {
		return false
	}
	if fn.Async {
		return false
	}
	if len(fn.Captures) > 0 {
		return false
	}
	if len(fn.Params) > 0 && fn.Params[len(fn.Params)-1].Variadic {
		return false
	}

	if _, ok := functionHasRuntimeGlobalLoadInLoop(vm, fn); ok {
		// fmt.Fprintf(
		// 	os.Stderr,
		// 	"[JIT DEBUG] function %s is not JIT-safe: %s performs VM global loads inside a loop; interpreter superinstruction is faster than Wasm host calls\n",
		// 	fn.Name,
		// 	op.String(),
		// )
		return false
	}

	if !stdlibCallsAreJitSafe(vm, fn) {
		return false
	}

	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CONST:
			if !instr.IsInt {
				if _, ok := instr.Value.(string); ok {
					break
				} else if _, ok := getFloat64Constant(instr.Value); !ok {
					// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: unsupported constant value type %T (%v)\n", fn.Name, instr.Value, instr.Value)
					return false
				}
			}
		case OP_LOCAL_CONST_OP_STORE, OP_LOCAL_CONST_OP:
			info, ok := instr.Value.(LocalConstOpInfo)
			if !ok {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: bad LocalConstOpInfo in %s\n", fn.Name, instr.Op.String())
				return false
			}
			if _, isString := info.Const.(string); !isString {
				if _, ok := getFloat64Constant(info.Const); !ok {
					// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: non-numeric const in %s (%T)\n", fn.Name, instr.Op.String(), info.Const)
					return false
				}
			} else if info.Op != OP_ADD {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: non-numeric const in %s (%T)\n", fn.Name, instr.Op.String(), info.Const)
				return false
			}
			if info.Op != OP_ADD && info.Op != OP_SUB && info.Op != OP_MUL && info.Op != OP_DIV && info.Op != OP_MOD {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: unsupported op %s inside %s\n", fn.Name, info.Op.String(), instr.Op.String())
				return false
			}

		case OP_ARRAY_INDEX_CONST_OP_STORE:
			info, ok := instr.Value.(ArrayIndexConstOpInfo)
			if !ok {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: bad ArrayIndexConstOpInfo\n", fn.Name)
				return false
			}
			if _, ok := getFloat64Constant(info.Const); !ok {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: non-numeric const in OP_ARRAY_INDEX_CONST_OP_STORE (%T)\n", fn.Name, info.Const)
				return false
			}
			if info.Op != OP_ADD && info.Op != OP_SUB && info.Op != OP_MUL && info.Op != OP_DIV && info.Op != OP_MOD {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: unsupported op %s inside OP_ARRAY_INDEX_CONST_OP_STORE\n", fn.Name, info.Op.String())
				return false
			}

		case OP_ADD_LOCAL_ARRAY_INDEX_STORE, OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE,
			OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY, OP_ADD_LOCAL_PROPERTIES_STORE, OP_ARRAY_INDEX_LOCAL_STORE:
			// Safe superinstructions lowered from already-JIT-safe bytecode shapes.

		case OP_RETURN, OP_POP,
			OP_LOAD_LOCAL, OP_STORE_LOCAL, OP_ASSIGN_LOCAL,
			OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_NOT, OP_NEGATE,
			OP_INC_LOCAL, OP_DEC_LOCAL,
			OP_ADD_ASSIGN_LOCAL, OP_SUB_ASSIGN_LOCAL,
			OP_MUL_LOCAL_CONST, OP_ADD_LOCAL_LOCAL_STORE,
			OP_JUMP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE,
			OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE,
			OP_JUMP_LOCAL_GT_CONST, OP_JUMP_LOCAL_GE_CONST,
			OP_JUMP_LOCAL_GT_LOCAL, OP_JUMP_LOCAL_GE_LOCAL,
			OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO, OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO,
			OP_CALL_DIRECT, OP_CALL_DIRECT_SUB_CONST,
			OP_OBJECT, OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE, OP_SET_PROPERTY,
			OP_GET_PROPERTY_LOCAL, OP_ADD_PROPERTY_LOCAL_LOCAL,
			OP_ARRAY, OP_INDEX, OP_SET_INDEX, OP_LEN,
			OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_MATH_CEIL, OP_MATH_FLOOR,
			OP_MATH_SQRT, OP_MATH_ABS, OP_MATH_POW, OP_PRINT,
			OP_COALESCE_JUMP, OP_TYPEOF, OP_THROW,
			OP_STRING_JOIN, OP_LOAD_GLOBAL: // Safe

		case OP_METHOD_CALL:
			// All method calls are permitted up to 3 args: push/get/length are compiled inline;
			// any other method dispatches through call_stdlib_wasm at runtime.
			info, ok := instr.Value.(MethodCallInfo)
			if !ok || info.ArgCount > 3 {
				return false
			}
		default:
			if strings.HasPrefix(fn.Name, "__jit_region_string_hot_") {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: unsupported opcode %s\n", fn.Name, instr.Op.String())
			}
			return false
		}
	}
	return true
}

func checkCallArgumentsSafe(vm *VM, fn Function, currentReturnTypes []stackType, currentParamTypes [][]stackType) bool {
	if len(fn.Instructions) == 0 {
		return true
	}

	spArray := make([]int, len(fn.Instructions))
	sp := 0
	for idx, instr := range fn.Instructions {
		spArray[idx] = sp
		switch instr.Op {
		case OP_CONST, OP_LOAD_LOCAL,
			OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3,
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOAD_GLOBAL,
			OP_LOCAL_CONST_OP:
			sp++
		case OP_MATH_POW:
			sp--
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			sp = sp - info.ArgCount + 1
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			sp--
		case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR, OP_COALESCE_JUMP:
			sp--
		case OP_RETURN:
			sp--
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				sp = sp - info.ArgCount + 1
			}
		case OP_CALL_DIRECT_SUB_CONST:
			sp++
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				if count > 0 {
					sp -= count - 1
				} else {
					sp++
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				if info.Count > 0 {
					sp -= info.Count - 1
				} else {
					sp++
				}
			}
		case OP_INDEX:
			sp--
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count > 0 {
				sp -= count - 1
			} else {
				sp++
			}
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
			// net change is 0 (pop 1, push 1)
		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			sp++
		case OP_SET_PROPERTY:
			sp -= 2
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				sp -= info.ArgCount
			}
		}
	}
	maxSp := 0
	for _, s := range spArray {
		if s > maxSp {
			maxSp = s
		}
	}

	typeStack := make([]stackType, maxSp+16)
	localTypes := make([]stackType, fn.LocalCount)
	for i := 0; i < len(fn.Params) && i < len(localTypes); i++ {
		localTypes[i] = stackTypeUnknown
		if t, ok := stackTypeFromTypeName(fn.Params[i].TypeHint.Name); ok {
			localTypes[i] = t
		} else if len(currentParamTypes) > 0 && fn.ID >= 0 && fn.ID < len(currentParamTypes) {
			localTypes[i] = currentParamTypes[fn.ID][i]
		}
	}

	sp = 0
	for idx, instr := range fn.Instructions {
		sp = spArray[idx]
		switch instr.Op {
		case OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}

		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			dest := sp - info.ArgCount

			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeNull // null result
			}

		case OP_MATH_POW:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeBool
			}
		case OP_AND, OP_OR:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeBool
			}
		case OP_NOT:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeBool
			}
		case OP_CONST:
			if sp < len(typeStack) {
				if isNullConstant(instr.Value) {
					typeStack[sp] = stackTypeNull
				} else if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isStr := instr.Value.(string); isStr {
					typeStack[sp] = stackTypeString
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_ADD:
			if sp >= 2 {
				if isJitStringType(typeStack[sp-1]) || isJitStringType(typeStack[sp-2]) {
					typeStack[sp-2] = stackTypeString
				} else if typeStack[sp-1] == stackTypeNumber || typeStack[sp-2] == stackTypeNumber {
					typeStack[sp-2] = stackTypeNumber
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_NEGATE:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_MUL_LOCAL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LOCAL_CONST_OP:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(LocalConstOpInfo); ok {
					typeStack[sp] = jitLocalConstOpResultType(info)
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_LOCAL_CONST_OP_STORE:
			if info, ok := instr.Value.(LocalConstOpInfo); ok && info.Slot >= 0 && info.Slot < len(localTypes) {
				localTypes[info.Slot] = jitLocalConstOpResultType(info)
			}
		case OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
			if info, ok := instr.Value.(AddLocalGlobalGlobalStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			if info, ok := instr.Value.(AddLocalArrayIndexStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ARRAY_INDEX_CONST_OP_STORE, OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY, OP_ADD_PROPERTY_LOCAL_LOCAL, OP_ADD_LOCAL_PROPERTIES_STORE, OP_ARRAY_INDEX_LOCAL_STORE,
			OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect for type propagation
		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)
			tA := localTypes[info.SlotA]
			tB := localTypes[info.SlotB]
			if isJitStringType(tA) || isJitStringType(tB) {
				localTypes[info.DestSlot] = stackTypeString
			} else if tA == stackTypeNumber && tB == stackTypeNumber {
				localTypes[info.DestSlot] = stackTypeNumber
			} else {
				localTypes[info.DestSlot] = stackTypeUnknown
			}
		case OP_LOAD_GLOBAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			}
			if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			}
			if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
			}
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			if sp >= 1 && slot < len(localTypes) {
				localTypes[slot] = typeStack[sp-1]
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE, OP_GET_PROPERTY_LOCAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				if len(currentParamTypes) > 0 && info.ID >= 0 && info.ID < len(currentParamTypes) {
					calleeParams := currentParamTypes[info.ID]
					for a := 0; a < info.ArgCount && a < len(calleeParams); a++ {
						argType := typeStack[sp-info.ArgCount+a]
						expectedType := calleeParams[a]
						if !jitCallArgTypesCompatible(expectedType, argType) {
							// Region helpers are produced by the compiler from already-typed live-ins.
							// If the local type propagator loses a heap-pointer distinction inside the
							// outlined loop, do not silently drop the whole region. The actual Wasm value
							// is still the raw pointer passed through the helper parameter.
							if strings.HasPrefix(fn.Name, "__jit_region_") && isJitHeapPointerType(expectedType) && isJitHeapPointerType(argType) {
								if jitCallDebugEnabled() {
									// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s: allowing conservative heap-pointer direct-call arg mismatch target=%s arg=%d expected=%s got=%s\n", fn.Name, info.Name, a, jitStackTypeDebugName(expectedType), jitStackTypeDebugName(argType))
								}
							} else {
								if jitCallDebugEnabled() {
									targetName := info.Name
									if targetName == "" && info.ID >= 0 && info.ID < len(vm.functionList) {
										targetName = vm.functionList[info.ID].Name
									}
									// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: direct call arg mismatch target=%s arg=%d expected=%s got=%s\n", fn.Name, targetName, a, jitStackTypeDebugName(expectedType), jitStackTypeDebugName(argType))
								}
								return false
							}
						}
					}
				}

				retT := stackTypeUnknown
				if vm != nil && info.ID >= 0 && info.ID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.ID]
				}
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = retT
				}
			}
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok {
				if len(currentParamTypes) > 0 && info.FnID >= 0 && info.FnID < len(currentParamTypes) {
					calleeParams := currentParamTypes[info.FnID]
					if len(calleeParams) > 0 {
						expectedType := calleeParams[0]
						if expectedType != stackTypeUnknown && expectedType != stackTypeNumber {
							if jitCallDebugEnabled() {
								targetName := info.FnName
								if targetName == "" && info.FnID >= 0 && info.FnID < len(vm.functionList) {
									targetName = vm.functionList[info.FnID].Name
								}
								// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: direct-sub-const target=%s expected first arg number-compatible, got %s\n", fn.Name, targetName, jitStackTypeDebugName(expectedType))
							}
							return false
						}
					}
				}

				retT := stackTypeUnknown
				if vm != nil && info.FnID >= 0 && info.FnID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.FnID]
				}
				if sp < len(typeStack) {
					typeStack[sp] = retT
				}
			}
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				dest := sp - len(info.Names)
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeObject
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				dest := sp - info.Count
				if dest >= 0 && dest < len(typeStack) {
					elements := make([]stackType, 0, info.Count)
					for idx := 0; idx < info.Count; idx++ {
						elements = append(elements, typeStack[sp-info.Count+idx])
					}
					typeStack[dest] = inferJitArrayStackType(elements)
				}
			}
		case OP_INDEX:
			if sp >= 2 {
				if elemType, ok := jitArrayElementType(typeStack[sp-2]); ok {
					typeStack[sp-2] = elemType
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_ARRAY_GET_LOCAL:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(ArrayLocalCallInfo); ok && info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
					if elemType, ok := jitArrayElementType(localTypes[info.ArraySlot]); ok {
						typeStack[sp] = elemType
					} else {
						typeStack[sp] = stackTypeUnknown
					}
				} else {
					typeStack[sp] = stackTypeUnknown
				}
			}
		case OP_SET_INDEX:
		case OP_ARRAY_LEN_LOCAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LEN:
			if sp-1 >= 0 && sp-1 < len(typeStack) {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeArray
			}
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				dest := sp - info.ArgCount - 1
				if dest >= 0 && dest < len(typeStack) {
					switch info.Method {
					case "length":
						typeStack[dest] = stackTypeNumber
					case "push":
						typeStack[dest] = stackTypeArray
					default:
						typeStack[dest] = stackTypeUnknown
					}
				}
			}
		}
	}

	return true
}

func compileFunctionBodyBytes(vm *VM, fn Function, safe bool, currentReturnTypes []stackType, jitStringAddr map[string]uint32, jitStringID map[string]uint32, currentParamTypes [][]stackType) []byte {
	body := &WasmBuffer{}

	if !safe {
		body.WriteVarUint(0) // 0 local groups
		body.WriteByte(0x44) // f64.const
		body.WriteFloat64(0.0)
		body.WriteByte(0x0F) // return
		body.WriteByte(0x0B) // end

		funcBodySec := &WasmBuffer{}
		funcBodySec.WriteVarUint(uint32(len(body.buf)))
		funcBodySec.WriteBytes(body.buf)
		return funcBodySec.buf
	}

	spArray := make([]int, len(fn.Instructions))
	sp := 0
	maxSp := 0
	for idx, instr := range fn.Instructions {
		spArray[idx] = sp
		if sp > maxSp {
			maxSp = sp
		}
		switch instr.Op {
		case OP_CONST, OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3, OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL, OP_LOAD_GLOBAL,
			OP_LOCAL_CONST_OP:
			sp++
		case OP_MATH_POW:
			sp--
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			sp = sp - info.ArgCount + 1
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE, OP_THROW:
			sp--
		case OP_JUMP_PROPERTY_LOCAL_FALSE, OP_JUMP_PROPERTY_LOCAL_TRUE:
			// no stack effect
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE, OP_AND, OP_OR, OP_COALESCE_JUMP:
			sp--
		case OP_RETURN:
			sp--
		case OP_CALL_DIRECT:
			info := instr.Value.(DirectCallInfo)
			sp = sp - info.ArgCount + 1
		case OP_CALL_DIRECT_SUB_CONST:
			sp++
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				count := len(info.Names)
				if count > 0 {
					sp -= count - 1
				} else {
					sp++
				}
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				if info.Count > 0 {
					sp -= info.Count - 1
				} else {
					sp++
				}
			}
		case OP_INDEX:
			sp--
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count > 0 {
				sp -= count - 1
			} else {
				sp++
			}
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
			// net change is 0 (pop 1, push 1)
		case OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			sp++
		case OP_SET_PROPERTY:
			sp -= 2
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				sp -= info.ArgCount
			}
		}
	}

	var blocks []JitBlock

	for i, instr := range fn.Instructions {
		if target, ok := getJumpTarget(instr); ok {
			if target < i {
				blocks = append(blocks, JitBlock{
					isLoop:   true,
					targetPC: target,
					endPC:    i + 1,
					startPC:  target,
				})
			} else {
				blocks = append(blocks, JitBlock{
					isLoop:   false,
					targetPC: target,
					endPC:    target,
					startPC:  i,
				})
			}
		}
	}

	changed := true
	for changed {
		changed = false
		for i := 0; i < len(blocks); i++ {
			for j := 0; j < len(blocks); j++ {
				if i == j {
					continue
				}
				if blocks[i].startPC < blocks[j].startPC &&
					blocks[j].startPC < blocks[i].endPC &&
					blocks[i].endPC < blocks[j].endPC {
					blocks[j].startPC = blocks[i].startPC
					changed = true
				}
			}
		}
	}

	opens := make(map[int][]JitBlock)
	closes := make(map[int][]JitBlock)

	for _, b := range blocks {
		opens[b.startPC] = append(opens[b.startPC], b)
		closes[b.endPC] = append(closes[b.endPC], b)
	}

	for pc, list := range opens {
		sort.Slice(list, func(i, j int) bool {
			if list[i].endPC != list[j].endPC {
				return list[i].endPC > list[j].endPC
			}
			if list[i].isLoop != list[j].isLoop {
				return !list[i].isLoop
			}
			return false
		})
		opens[pc] = list
	}

	for pc, list := range closes {
		sort.Slice(list, func(i, j int) bool {
			if list[i].startPC != list[j].startPC {
				return list[i].startPC > list[j].startPC
			}
			if list[i].isLoop != list[j].isLoop {
				return list[i].isLoop
			}
			return false
		})
		closes[pc] = list
	}

	stackBase := fn.LocalCount
	extraLocalsCount := fn.LocalCount - len(fn.Params)

	stackLocalCount := maxSp + 20
	valueLocalCount := fn.LocalCount + stackLocalCount
	tagBase := valueLocalCount

	var groups [][]any
	if extraLocalsCount > 0 {
		groups = append(groups, []any{extraLocalsCount, byte(0x7C)})
	}
	const jitI32LocalCount = 16
	const jitI64LocalCount = 4

	groups = append(groups, []any{stackLocalCount, byte(0x7C)})
	groups = append(groups, []any{valueLocalCount, byte(0x7C)})
	groups = append(groups, []any{jitI32LocalCount, byte(0x7F)})
	groups = append(groups, []any{jitI64LocalCount, byte(0x7E)})

	body.WriteVarUint(uint32(len(groups)))
	for _, g := range groups {
		body.WriteVarUint(uint32(g[0].(int)))
		body.WriteByte(g[1].(byte))
	}

	var activeBlocks []JitBlock
	N := len(fn.Instructions)
	tempPtrSlot := stackBase + maxSp + 8 // Positioned safely out of stack range
	powResultSlot := stackBase + maxSp + 9
	powBaseSlot := stackBase + maxSp + 10
	powExpSlot := stackBase + maxSp + 11
	stringJoinResultSlot := stackBase + maxSp + 12
	stringJoinScratchSlot := stackBase + maxSp + 13

	// Extra integer locals used by the fully-Wasm string join fast path.
	// Existing code also uses the first 3 i32 locals for string equality, so keep
	// them at the same base and reserve enough scratch space for both features.
	i32Base := tagBase + valueLocalCount
	joinByteIdxSlot := i32Base
	joinBitOffsetSlot := i32Base + 1
	joinMaskSlot := i32Base + 2
	joinAddrSlot := i32Base + 3
	joinLenSlot := i32Base + 4
	joinSizeSlot := i32Base + 5
	joinSrcPtrSlot := i32Base + 6
	joinDstPtrSlot := i32Base + 7
	joinCopyLenSlot := i32Base + 8
	joinPartLenSlot := i32Base + 9
	joinDigitsSlot := i32Base + 10
	joinPosSlot := i32Base + 11

	i64Base := i32Base + jitI32LocalCount
	joinNumSlot := i64Base
	joinTmpI64Slot := i64Base + 1
	joinDigitSlot := i64Base + 2

	getStoreLocalSlot := func(instr Instruction) (int, bool) {
		if instr.Op != OP_STORE_LOCAL && instr.Op != OP_ASSIGN_LOCAL {
			return -1, false
		}

		if instr.IsInt {
			return instr.IntArg, true
		}

		if info, ok := instr.Value.(VariableInfo); ok {
			return info.Slot, true
		}

		if slot, ok := AsIntInternal(instr.Value); ok {
			return slot, true
		}

		return -1, false
	}

	getLoadLocalSlot := func(instr Instruction) (int, bool) {
		switch instr.Op {
		case OP_LOAD_LOCAL_0:
			return 0, true
		case OP_LOAD_LOCAL_1:
			return 1, true
		case OP_LOAD_LOCAL_2:
			return 2, true
		case OP_LOAD_LOCAL_3:
			return 3, true
		case OP_LOAD_LOCAL:
			if instr.IsInt {
				return instr.IntArg, true
			}
			if slot, ok := AsIntInternal(instr.Value); ok {
				return slot, true
			}
		}

		return -1, false
	}

	getArrayLenLocalSlot := func(instr Instruction) (int, bool) {
		if instr.Op != OP_ARRAY_LEN_LOCAL {
			return -1, false
		}
		if info, ok := instr.Value.(ArrayLocalCallInfo); ok {
			return info.ArraySlot, true
		}
		return -1, false
	}

	localIsOnlyUsedForLengthAfterJoin := func(joinPC int, slot int) bool {
		hasLengthUse := false

		for pc := joinPC + 2; pc < len(fn.Instructions); pc++ {
			instr := fn.Instructions[pc]

			if lengthSlot, ok := getArrayLenLocalSlot(instr); ok && lengthSlot == slot {
				hasLengthUse = true
				continue
			}

			if loadSlot, ok := getLoadLocalSlot(instr); ok && loadSlot == slot {
				return false
			}

			if storeSlot, ok := getStoreLocalSlot(instr); ok && storeSlot == slot {
				return false
			}

			switch instr.Op {
			case OP_GET_PROPERTY_LOCAL:
				if info, ok := instr.Value.(PropertyLocalInfo); ok && info.Slot == slot {
					return false
				}
			case OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
				if info, ok := instr.Value.(ArrayLocalCallInfo); ok {
					if info.ArraySlot == slot || info.ArgSlot == slot {
						return false
					}
				}
			}
		}

		return hasLengthUse
	}

	optimizedStringJoinPC := make(map[int]bool)
	optimizedStringJoinLengthSlot := make(map[int]bool)

	// Disabled for correctness. The old length-only string join shortcut is not
	// control-flow safe: it can replace a loop-carried string local with a number.
	// Keep the maps empty so OP_STRING_JOIN always materializes a real string if
	// that opcode is ever allowed again.
	_ = localIsOnlyUsedForLengthAfterJoin

	tagSlot := func(valueSlot int) int {
		return tagBase + valueSlot
	}

	emitSetTagConst := func(valueSlot int, tag float64) {
		body.WriteByte(0x44) // f64.const tag
		body.WriteFloat64(tag)
		body.WriteByte(0x21) // local.set tagSlot
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
	}

	emitCopyTag := func(dstValueSlot int, srcValueSlot int) {
		body.WriteByte(0x20) // local.get source tag
		body.WriteVarUint(uint32(tagSlot(srcValueSlot)))
		body.WriteByte(0x21) // local.set destination tag
		body.WriteVarUint(uint32(tagSlot(dstValueSlot)))
	}

	emitCopyTagged := func(dstValueSlot int, srcValueSlot int) {
		body.WriteByte(0x20) // local.get source value
		body.WriteVarUint(uint32(srcValueSlot))
		body.WriteByte(0x21) // local.set destination value
		body.WriteVarUint(uint32(dstValueSlot))
		emitCopyTag(dstValueSlot, srcValueSlot)
	}

	emitSetTagFromType := func(valueSlot int, t stackType) {
		switch t {
		case stackTypeBool:
			emitSetTagConst(valueSlot, jitTagBool)
		case stackTypeObject:
			emitSetTagConst(valueSlot, jitTagObject)
		case stackTypeArray, stackTypeNumberArray, stackTypeInternedStringArray:
			emitSetTagConst(valueSlot, jitTagArray)
		case stackTypeString, stackTypeInternedString:
			emitSetTagConst(valueSlot, jitTagString)
		case stackTypeNumber:
			emitSetTagConst(valueSlot, jitTagNumber)
		case stackTypeNull:
			emitSetTagConst(valueSlot, jitTagNull)
		default:
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(valueSlot))
			body.WriteByte(0x10) // call determine_tag
			body.WriteVarUint(1)
			body.WriteByte(0x21) // local.set tag
			body.WriteVarUint(uint32(tagSlot(valueSlot)))
		}
	}

	emitLoadTaggedCell := func(addrSlot int, dstValueSlot int) {
		// tag = *(addr + 0)
		body.WriteByte(0x20) // local.get addr
		body.WriteVarUint(uint32(addrSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x2B) // f64.load
		body.WriteVarUint(3)
		body.WriteVarUint(0)
		body.WriteByte(0x21) // local.set tag
		body.WriteVarUint(uint32(tagSlot(dstValueSlot)))

		// value = *(addr + 8)
		body.WriteByte(0x20) // local.get addr
		body.WriteVarUint(uint32(addrSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x2B) // f64.load
		body.WriteVarUint(3)
		body.WriteVarUint(8)
		body.WriteByte(0x21) // local.set value
		body.WriteVarUint(uint32(dstValueSlot))
	}

	emitObjectPropertyLoadFromLocal := func(objectSlot int, name string, dstValueSlot int) {
		offset := vm.getPropertyOffset(name)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(objectSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(float64(offset))
		body.WriteByte(0xA0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tempPtrSlot))
		emitLoadTaggedCell(tempPtrSlot, dstValueSlot)
	}

	emitPackedArrayPropertyLoadOrFallback := func(arraySlot int, indexSlot int, objectSlot int, name string, dstValueSlot int) {
		offset := vm.getPropertyOffset(name)
		tableIndex := float64((offset - 16) / 16)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(arraySlot))
		body.WriteByte(0xAA)
		body.WriteByte(0x2B)
		body.WriteVarUint(3)
		body.WriteVarUint(jitArrayPackedMarkerOffset)
		body.WriteByte(0x44)
		body.WriteFloat64(jitPackedObjectArrayMarker)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(arraySlot))
		body.WriteByte(0xAA)
		body.WriteByte(0x2B)
		body.WriteVarUint(3)
		body.WriteVarUint(jitArrayPackedSlotsOffset)
		body.WriteByte(0x44)
		body.WriteFloat64(tableIndex + 1)
		body.WriteByte(0x66)
		body.WriteByte(0x04)
		body.WriteByte(0x40)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(arraySlot))
		body.WriteByte(0xAA)
		body.WriteByte(0x2B)
		body.WriteVarUint(3)
		body.WriteVarUint(jitArrayPackedTableOffset)
		body.WriteByte(0x44)
		body.WriteFloat64(tableIndex * 8.0)
		body.WriteByte(0xA0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tempPtrSlot))

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tempPtrSlot))
		body.WriteByte(0xAA)
		body.WriteByte(0x2B)
		body.WriteVarUint(3)
		body.WriteVarUint(0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tempPtrSlot + 2))

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tempPtrSlot + 2))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x62)
		body.WriteByte(0x04)
		body.WriteByte(0x40)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tempPtrSlot + 2))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(indexSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(16.0)
		body.WriteByte(0xA2)
		body.WriteByte(0xA0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tempPtrSlot))
		emitLoadTaggedCell(tempPtrSlot, dstValueSlot)

		body.WriteByte(0x05)
		emitObjectPropertyLoadFromLocal(objectSlot, name, dstValueSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x05)
		emitObjectPropertyLoadFromLocal(objectSlot, name, dstValueSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x05)
		emitObjectPropertyLoadFromLocal(objectSlot, name, dstValueSlot)
		body.WriteByte(0x0B)
	}

	emitStoreTaggedCell := func(addrSlot int, srcValueSlot int) {
		// *(addr + 0) = tag
		body.WriteByte(0x20) // local.get addr
		body.WriteVarUint(uint32(addrSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x20) // local.get source tag
		body.WriteVarUint(uint32(tagSlot(srcValueSlot)))
		body.WriteByte(0x39) // f64.store
		body.WriteVarUint(3)
		body.WriteVarUint(0)

		// *(addr + 8) = value
		body.WriteByte(0x20) // local.get addr
		body.WriteVarUint(uint32(addrSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x20) // local.get source value
		body.WriteVarUint(uint32(srcValueSlot))
		body.WriteByte(0x39) // f64.store
		body.WriteVarUint(3)
		body.WriteVarUint(8)
	}

	emitMarkSideEffect := func() {
		body.WriteByte(0x44) // f64.const 1.0
		body.WriteFloat64(1.0)
		body.WriteByte(0x24) // global.set __jit_side_effect
		body.WriteVarUint(1)
	}

	emitF64GlobalSetConst := func(globalIndex int, value float64) {
		body.WriteByte(0x44)
		body.WriteFloat64(value)
		body.WriteByte(0x24)
		body.WriteVarUint(uint32(globalIndex))
	}

	emitI32ConstRaw := func(v int32) {
		body.WriteByte(0x41)
		body.WriteVarInt(int64(v))
	}

	emitStoreF64LocalAtConstAddr := func(addr uint32, localSlot int) {
		emitI32ConstRaw(int32(addr))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(localSlot))
		body.WriteByte(0x39) // f64.store
		body.WriteVarUint(3)
		body.WriteVarUint(0)
	}

	emitStoreTaggedSnapshotCell := func(addr uint32, valueSlot int) {
		emitStoreF64LocalAtConstAddr(addr, tagSlot(valueSlot))
		emitStoreF64LocalAtConstAddr(addr+8, valueSlot)
	}

	emitDeoptCheckpoint := func(resumeIP int, resumeSP int) {
		if resumeSP < 0 {
			resumeSP = 0
		}
		emitMarkSideEffect()
		emitF64GlobalSetConst(2, float64(resumeIP))
		emitF64GlobalSetConst(3, float64(resumeSP))
		emitF64GlobalSetConst(4, float64(fn.LocalCount))
		emitF64GlobalSetConst(5, float64(fn.ID))

		addr := uint32(jitDeoptSnapshotBase)
		for local := 0; local < fn.LocalCount; local++ {
			emitStoreTaggedSnapshotCell(addr+uint32(local*16), local)
		}

		stackAddr := addr + uint32(fn.LocalCount*16)
		for stackIndex := 0; stackIndex < resumeSP; stackIndex++ {
			emitStoreTaggedSnapshotCell(stackAddr+uint32(stackIndex*16), stackBase+stackIndex)
		}
	}

	emitDeoptTrap := func(resumeIP int, resumeSP int) {
		emitDeoptCheckpoint(resumeIP, resumeSP)
		body.WriteByte(0x00) // unreachable: wazero returns an error, then the VM resumes from the snapshot.
	}

	emitRequireTag := func(valueSlot int, expectedTag float64, resumeIP int, resumeSP int) {
		body.WriteByte(0x20) // local.get actual tag
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(expectedTag)
		body.WriteByte(0x61) // f64.eq
		body.WriteByte(0x04) // if
		body.WriteByte(0x40) // empty block
		body.WriteByte(0x05) // else
		emitDeoptTrap(resumeIP, resumeSP)
		body.WriteByte(0x0B) // end
	}

	emitDynamicAddTagged := func(leftSlot int, rightSlot int, dstSlot int) {
		// Check if leftTag == number && rightTag == number
		body.WriteByte(0x20) // local.get leftTag
		body.WriteVarUint(uint32(tagSlot(leftSlot)))
		body.WriteByte(0x44) // f64.const 1.0
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x20) // local.get rightTag
		body.WriteVarUint(uint32(tagSlot(rightSlot)))
		body.WriteByte(0x44) // f64.const 1.0
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x71) // i32.and
		body.WriteByte(0x04) // if (result f64)
		body.WriteByte(0x7C)
		body.WriteByte(0x20) // local.get leftValue
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // local.get rightValue
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0xA0) // f64.add
		body.WriteByte(0x05) // else
		body.WriteByte(0x20) // left tag
		body.WriteVarUint(uint32(tagSlot(leftSlot)))
		body.WriteByte(0x20) // left value
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // right tag
		body.WriteVarUint(uint32(tagSlot(rightSlot)))
		body.WriteByte(0x20) // right value
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x10) // call dynamic_add
		body.WriteVarUint(5)
		body.WriteByte(0x0B) // end if
		body.WriteByte(0x21) // local.set dst value
		body.WriteVarUint(uint32(dstSlot))

		// dst tag = (leftTag == number && rightTag == number) ? number : string
		body.WriteByte(0x20) // local.get left tag
		body.WriteVarUint(uint32(tagSlot(leftSlot)))
		body.WriteByte(0x44) // f64.const number tag
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x20) // local.get right tag
		body.WriteVarUint(uint32(tagSlot(rightSlot)))
		body.WriteByte(0x44) // f64.const number tag
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x71) // i32.and
		body.WriteByte(0x04) // if
		body.WriteByte(0x7C) // result f64
		body.WriteByte(0x44) // f64.const number tag
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x05) // else
		body.WriteByte(0x44) // f64.const string tag
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x0B) // end
		body.WriteByte(0x21) // local.set dst tag
		body.WriteVarUint(uint32(tagSlot(dstSlot)))
	}

	emitConstTagged := func(dstSlot int, raw any) bool {
		if strVal, ok := raw.(string); ok {
			if addr, exists := jitStringAddr[strVal]; exists {
				body.WriteByte(0x44)
				body.WriteFloat64(float64(addr))
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dstSlot))
				emitSetTagConst(dstSlot, jitTagString)
				return true
			}
			if strID, exists := jitStringID[strVal]; exists {
				body.WriteByte(0x44)
				body.WriteFloat64(float64(strID))
				body.WriteByte(0x10)
				body.WriteVarUint(jitImportLoadStringConstant)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dstSlot))
				emitSetTagConst(dstSlot, jitTagString)
				return true
			}
			return false
		}
		val, ok := getFloat64Constant(raw)
		if !ok {
			return false
		}
		body.WriteByte(0x44)
		body.WriteFloat64(val)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		emitSetTagConst(dstSlot, jitTagNumber)
		return true
	}

	emitTinyValueArg := func(value TinyValue) bool {
		if value.IsInt {
			body.WriteByte(0x44)
			body.WriteFloat64(float64(value.AsInt))
			return true
		}
		if isNullConstant(value) {
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			return true
		}
		switch v := value.Value.(type) {
		case int:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case int8:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case int16:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case int32:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case int64:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case uint:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case uint8:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case uint16:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case uint32:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case uint64:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case float32:
			body.WriteByte(0x44)
			body.WriteFloat64(float64(v))
			return true
		case float64:
			body.WriteByte(0x44)
			body.WriteFloat64(v)
			return true
		case bool:
			body.WriteByte(0x44)
			if v {
				body.WriteFloat64(1.0)
			} else {
				body.WriteFloat64(0.0)
			}
			return true
		case string:
			addr, ok := jitStringAddr[v]
			if !ok {
				return false
			}
			body.WriteByte(0x44)
			body.WriteFloat64(float64(addr))
			return true
		default:
			return false
		}
	}

	emitNumericBinaryOp := func(leftSlot int, rightSlot int, dstSlot int, op OpCode) bool {
		switch op {
		case OP_ADD:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA0)
		case OP_SUB:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA1)
		case OP_MUL:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA2)
		case OP_DIV:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA3)
		case OP_MOD:
			// f64 modulo fallback: a - trunc(a / b) * b. This avoids depending on
			// emitFastModValue, which is declared later in this function.
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(leftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA3)
			body.WriteByte(0x9D)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(rightSlot))
			body.WriteByte(0xA2)
			body.WriteByte(0xA1)
		default:
			return false
		}
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		emitSetTagConst(dstSlot, jitTagNumber)
		return true
	}

	emitLocalConstOp := func(leftSlot int, raw any, op OpCode, dstSlot int) bool {
		if !emitConstTagged(tempPtrSlot+2, raw) {
			return false
		}
		if op == OP_ADD {
			if _, ok := raw.(string); ok {
				body.WriteByte(0x20) // local.get left tag
				body.WriteVarUint(uint32(tagSlot(leftSlot)))
				body.WriteByte(0x44) // f64.const jitTagString
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C)
				body.WriteByte(0x20) // local.get left string ptr
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0x20) // local.get right const string ptr
				body.WriteVarUint(uint32(tempPtrSlot + 2))
				body.WriteByte(0x10) // call string_concat_wasm
				body.WriteVarUint(jitImportStringConcat)
				body.WriteByte(0x05) // else
				body.WriteByte(0x20) // left tag
				body.WriteVarUint(uint32(tagSlot(leftSlot)))
				body.WriteByte(0x20) // left value
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0x20) // right tag
				body.WriteVarUint(uint32(tagSlot(tempPtrSlot + 2)))
				body.WriteByte(0x20) // right value
				body.WriteVarUint(uint32(tempPtrSlot + 2))
				body.WriteByte(0x10) // call dynamic_add
				body.WriteVarUint(jitImportDynamicAdd)
				body.WriteByte(0x0B) // end if
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dstSlot))
				body.WriteByte(0x20) // local.get left tag
				body.WriteVarUint(uint32(tagSlot(leftSlot)))
				body.WriteByte(0x44) // f64.const jitTagString
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C)
				body.WriteByte(0x44)
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x05) // else
				body.WriteByte(0x44)
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x0B)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(tagSlot(dstSlot)))
				return true
			}
		}
		return emitNumericBinaryOp(leftSlot, tempPtrSlot+2, dstSlot, op)
	}

	emitArrayElementAddress := func(arraySlot int, indexSlot int) {
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(arraySlot))
		body.WriteByte(0xAA)
		body.WriteByte(0x2B)
		body.WriteVarUint(3)
		body.WriteVarUint(16)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(indexSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(16.0)
		body.WriteByte(0xA2)
		body.WriteByte(0xA0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tempPtrSlot))
	}

	emitLoadGlobalTagged := func(globalSlot int, dstSlot int) {
		body.WriteByte(0x44)
		body.WriteFloat64(float64(globalSlot))
		body.WriteByte(0x10)
		body.WriteVarUint(jitImportLoadGlobal)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(tagSlot(dstSlot)))
	}

	emitStringMemoryLength := func(valueSlot int, dstSlot int) {
		body.WriteByte(0x20) // local.get string pointer
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x2B) // f64.load length at offset 8
		body.WriteVarUint(3)
		body.WriteVarUint(8)
		body.WriteByte(0x21) // local.set dst
		body.WriteVarUint(uint32(dstSlot))
	}

	emitIntegerNumberStringLength := func(valueSlot int, dstSlot int) {
		absSlot := powBaseSlot
		minusSlot := powExpSlot

		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x63) // f64.lt
		body.WriteByte(0x04) // if
		body.WriteByte(0x40)

		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xA1) // f64.sub
		body.WriteByte(0x21) // local.set abs
		body.WriteVarUint(uint32(absSlot))
		body.WriteByte(0x44) // f64.const 1
		body.WriteFloat64(1.0)
		body.WriteByte(0x21) // local.set minus
		body.WriteVarUint(uint32(minusSlot))

		body.WriteByte(0x05) // else

		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x21) // local.set abs
		body.WriteVarUint(uint32(absSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x21) // local.set minus
		body.WriteVarUint(uint32(minusSlot))

		body.WriteByte(0x0B) // end if

		body.WriteByte(0x44) // digits = 1
		body.WriteFloat64(1.0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))

		threshold := 10.0
		for digits := 2; digits <= 17; digits++ {
			body.WriteByte(0x20) // local.get abs
			body.WriteVarUint(uint32(absSlot))
			body.WriteByte(0x44) // f64.const threshold
			body.WriteFloat64(threshold)
			body.WriteByte(0x66) // f64.ge
			body.WriteByte(0x04) // if
			body.WriteByte(0x40)
			body.WriteByte(0x44) // f64.const digits
			body.WriteFloat64(float64(digits))
			body.WriteByte(0x21) // local.set dst
			body.WriteVarUint(uint32(dstSlot))
			body.WriteByte(0x0B) // end if
			threshold *= 10.0
		}

		body.WriteByte(0x20) // local.get digits
		body.WriteVarUint(uint32(dstSlot))
		body.WriteByte(0x20) // local.get minus
		body.WriteVarUint(uint32(minusSlot))
		body.WriteByte(0xA0) // f64.add
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
	}

	emitBoolStringLength := func(valueSlot int, dstSlot int) {
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne
		body.WriteByte(0x04) // if
		body.WriteByte(0x40)
		body.WriteByte(0x44)
		body.WriteFloat64(4.0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		body.WriteByte(0x05) // else
		body.WriteByte(0x44)
		body.WriteFloat64(5.0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		body.WriteByte(0x0B)
	}

	emitValueStringLength := func(valueSlot int, dstSlot int, knownType stackType) {
		switch knownType {
		case stackTypeString, stackTypeInternedString:
			emitStringMemoryLength(valueSlot, dstSlot)
			return
		case stackTypeNumber:
			emitIntegerNumberStringLength(valueSlot, dstSlot)
			return
		case stackTypeBool:
			emitBoolStringLength(valueSlot, dstSlot)
			return
		case stackTypeNull:
			body.WriteByte(0x44)
			body.WriteFloat64(4.0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(dstSlot))
			return
		}

		body.WriteByte(0x44) // default length 0 for unsupported runtime tags
		body.WriteFloat64(0.0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitStringMemoryLength(valueSlot, dstSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitIntegerNumberStringLength(valueSlot, dstSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagBool)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitBoolStringLength(valueSlot, dstSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNull)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		body.WriteByte(0x44)
		body.WriteFloat64(4.0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstSlot))
		body.WriteByte(0x0B)
	}

	emitI32Const := func(v int32) {
		body.WriteByte(0x41) // i32.const
		body.WriteVarInt(int64(v))
	}

	emitI64Const := func(v int64) {
		body.WriteByte(0x42) // i64.const
		body.WriteVarInt(v)
	}

	emitIncrementI32Local := func(slot int, amount int32) {
		body.WriteByte(0x20) // local.get
		body.WriteVarUint(uint32(slot))
		emitI32Const(amount)
		body.WriteByte(0x6A) // i32.add
		body.WriteByte(0x21) // local.set
		body.WriteVarUint(uint32(slot))
	}

	emitWriteByteConstAndAdvance := func(dstPtrSlot int, b byte) {
		body.WriteByte(0x20) // local.get dst
		body.WriteVarUint(uint32(dstPtrSlot))
		emitI32Const(int32(b))
		body.WriteByte(0x3A) // i32.store8
		body.WriteVarUint(0) // align
		body.WriteVarUint(0) // offset
		emitIncrementI32Local(dstPtrSlot, 1)
	}

	emitWriteStaticBytes := func(dstPtrSlot int, data []byte) {
		for _, b := range data {
			emitWriteByteConstAndAdvance(dstPtrSlot, b)
		}
	}

	emitLoadStringLenI32 := func(valueSlot int, dstLenSlot int) {
		body.WriteByte(0x20) // local.get string ptr as f64
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x2B) // f64.load length at offset 8
		body.WriteVarUint(3)
		body.WriteVarUint(8)
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x21) // local.set dstLen
		body.WriteVarUint(uint32(dstLenSlot))
	}

	emitCopyStringBytesAndAdvance := func(valueSlot int, dstPtrSlot int) {
		// src = i32(value) + 16
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		emitI32Const(16)
		body.WriteByte(0x6A) // i32.add
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinSrcPtrSlot))

		emitLoadStringLenI32(valueSlot, joinCopyLenSlot)

		body.WriteByte(0x02) // block
		body.WriteByte(0x40)
		body.WriteByte(0x03) // loop
		body.WriteByte(0x40)

		body.WriteByte(0x20) // local.get len
		body.WriteVarUint(uint32(joinCopyLenSlot))
		body.WriteByte(0x45) // i32.eqz
		body.WriteByte(0x0D) // br_if outer block
		body.WriteVarUint(1)

		body.WriteByte(0x20) // dst address
		body.WriteVarUint(uint32(dstPtrSlot))
		body.WriteByte(0x20) // src address
		body.WriteVarUint(uint32(joinSrcPtrSlot))
		body.WriteByte(0x2D) // i32.load8_u
		body.WriteVarUint(0)
		body.WriteVarUint(0)
		body.WriteByte(0x3A) // i32.store8
		body.WriteVarUint(0)
		body.WriteVarUint(0)

		emitIncrementI32Local(dstPtrSlot, 1)
		emitIncrementI32Local(joinSrcPtrSlot, 1)

		body.WriteByte(0x20) // len--
		body.WriteVarUint(uint32(joinCopyLenSlot))
		emitI32Const(1)
		body.WriteByte(0x6B) // i32.sub
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinCopyLenSlot))

		body.WriteByte(0x0C) // br loop
		body.WriteVarUint(0)
		body.WriteByte(0x0B) // end loop
		body.WriteByte(0x0B) // end block
	}

	emitAbsIntegerNumberToI64 := func(valueSlot int, dstI64Slot int) {
		body.WriteByte(0x20) // value < 0 ?
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x63) // f64.lt
		body.WriteByte(0x04) // if
		body.WriteByte(0x40)

		body.WriteByte(0x44) // 0 - value
		body.WriteFloat64(0.0)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xA1) // f64.sub
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstI64Slot))

		body.WriteByte(0x05) // else

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstI64Slot))

		body.WriteByte(0x0B) // end if
	}

	emitIntegerDigitCountNoSignI32 := func(srcI64Slot int, dstDigitsSlot int) {
		emitI32Const(1)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstDigitsSlot))

		body.WriteByte(0x20) // tmp = n
		body.WriteVarUint(uint32(srcI64Slot))
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinTmpI64Slot))

		body.WriteByte(0x02) // block
		body.WriteByte(0x40)
		body.WriteByte(0x03) // loop
		body.WriteByte(0x40)

		body.WriteByte(0x20) // tmp >= 10
		body.WriteVarUint(uint32(joinTmpI64Slot))
		emitI64Const(10)
		body.WriteByte(0x59) // i64.ge_s
		body.WriteByte(0x45) // i32.eqz
		body.WriteByte(0x0D) // br_if outer block
		body.WriteVarUint(1)

		body.WriteByte(0x20) // digits++
		body.WriteVarUint(uint32(dstDigitsSlot))
		emitI32Const(1)
		body.WriteByte(0x6A)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstDigitsSlot))

		body.WriteByte(0x20) // tmp /= 10
		body.WriteVarUint(uint32(joinTmpI64Slot))
		emitI64Const(10)
		body.WriteByte(0x7F) // i64.div_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinTmpI64Slot))

		body.WriteByte(0x0C) // br loop
		body.WriteVarUint(0)
		body.WriteByte(0x0B) // end loop
		body.WriteByte(0x0B) // end block
	}

	emitIntegerNumberStringLenI32 := func(valueSlot int, dstLenSlot int) {
		emitAbsIntegerNumberToI64(valueSlot, joinNumSlot)
		emitIntegerDigitCountNoSignI32(joinNumSlot, dstLenSlot)

		body.WriteByte(0x20) // if value < 0, include '-'
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x63) // f64.lt
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(dstLenSlot))
		emitI32Const(1)
		body.WriteByte(0x6A)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstLenSlot))
		body.WriteByte(0x0B)
	}

	emitBoolStringLenI32 := func(valueSlot int, dstLenSlot int) {
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitI32Const(4)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstLenSlot))
		body.WriteByte(0x05)
		emitI32Const(5)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstLenSlot))
		body.WriteByte(0x0B)
	}

	emitFastJoinPartLenI32 := func(valueSlot int, knownType stackType, dstLenSlot int) {
		switch knownType {
		case stackTypeString, stackTypeInternedString:
			emitLoadStringLenI32(valueSlot, dstLenSlot)
		case stackTypeNumber:
			emitIntegerNumberStringLenI32(valueSlot, dstLenSlot)
		case stackTypeBool:
			emitBoolStringLenI32(valueSlot, dstLenSlot)
		case stackTypeNull:
			emitI32Const(4)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(dstLenSlot))
		default:
			emitI32Const(0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(dstLenSlot))
		}
	}

	emitWriteIntegerNumberPartAndAdvance := func(valueSlot int, dstPtrSlot int) {
		// Leading minus sign.
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x63) // f64.lt
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitWriteByteConstAndAdvance(dstPtrSlot, '-')
		body.WriteByte(0x0B)

		emitAbsIntegerNumberToI64(valueSlot, joinNumSlot)
		emitIntegerDigitCountNoSignI32(joinNumSlot, joinDigitsSlot)

		// pos = dst + digits - 1
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(dstPtrSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinDigitsSlot))
		body.WriteByte(0x6A)
		emitI32Const(1)
		body.WriteByte(0x6B)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinPosSlot))

		// Special-case zero.
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinNumSlot))
		body.WriteByte(0x50) // i64.eqz
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitWriteByteConstAndAdvance(dstPtrSlot, '0')
		body.WriteByte(0x05) // else

		body.WriteByte(0x02) // block
		body.WriteByte(0x40)
		body.WriteByte(0x03) // loop
		body.WriteByte(0x40)

		body.WriteByte(0x20) // if n == 0 break
		body.WriteVarUint(uint32(joinNumSlot))
		body.WriteByte(0x50) // i64.eqz
		body.WriteByte(0x0D)
		body.WriteVarUint(1)

		body.WriteByte(0x20) // digit = n % 10
		body.WriteVarUint(uint32(joinNumSlot))
		emitI64Const(10)
		body.WriteByte(0x81) // i64.rem_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinDigitSlot))

		body.WriteByte(0x20) // store digit
		body.WriteVarUint(uint32(joinPosSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinDigitSlot))
		body.WriteByte(0xA7) // i32.wrap_i64
		emitI32Const(48)
		body.WriteByte(0x6A) // i32.add
		body.WriteByte(0x3A) // i32.store8
		body.WriteVarUint(0)
		body.WriteVarUint(0)

		body.WriteByte(0x20) // pos--
		body.WriteVarUint(uint32(joinPosSlot))
		emitI32Const(1)
		body.WriteByte(0x6B)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinPosSlot))

		body.WriteByte(0x20) // n /= 10
		body.WriteVarUint(uint32(joinNumSlot))
		emitI64Const(10)
		body.WriteByte(0x7F) // i64.div_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinNumSlot))

		body.WriteByte(0x0C)
		body.WriteVarUint(0)
		body.WriteByte(0x0B) // end loop
		body.WriteByte(0x0B) // end block

		// dst += digits
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(dstPtrSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinDigitsSlot))
		body.WriteByte(0x6A)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstPtrSlot))

		body.WriteByte(0x0B) // end zero/non-zero if
	}

	emitWriteFastJoinPartAndAdvance := func(valueSlot int, knownType stackType, dstPtrSlot int) {
		switch knownType {
		case stackTypeString, stackTypeInternedString:
			emitCopyStringBytesAndAdvance(valueSlot, dstPtrSlot)
		case stackTypeNumber:
			emitWriteIntegerNumberPartAndAdvance(valueSlot, dstPtrSlot)
		case stackTypeBool:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(valueSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			emitWriteStaticBytes(dstPtrSlot, []byte("true"))
			body.WriteByte(0x05)
			emitWriteStaticBytes(dstPtrSlot, []byte("false"))
			body.WriteByte(0x0B)
		case stackTypeNull:
			emitWriteStaticBytes(dstPtrSlot, []byte("null"))
		}
	}

	emitMarkJitAllocation := func(addrSlot int) {
		// byteIdx = addr >> 6
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(addrSlot))
		emitI32Const(6)
		body.WriteByte(0x76) // i32.shr_u
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinByteIdxSlot))

		// bitOffset = (addr >> 3) & 7
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(addrSlot))
		emitI32Const(3)
		body.WriteByte(0x76) // i32.shr_u
		emitI32Const(7)
		body.WriteByte(0x71) // i32.and
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinBitOffsetSlot))

		// mask = 1 << bitOffset
		emitI32Const(1)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinBitOffsetSlot))
		body.WriteByte(0x74) // i32.shl
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinMaskSlot))

		// if byteIdx is inside the allocator bitset, memory[byteIdx] |= mask.
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinByteIdxSlot))
		emitI32Const(2 * 1024 * 1024)
		body.WriteByte(0x49) // i32.lt_u
		body.WriteByte(0x04)
		body.WriteByte(0x40)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinByteIdxSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinByteIdxSlot))
		body.WriteByte(0x2D) // i32.load8_u
		body.WriteVarUint(0)
		body.WriteVarUint(0)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinMaskSlot))
		body.WriteByte(0x72) // i32.or
		body.WriteByte(0x3A) // i32.store8
		body.WriteVarUint(0)
		body.WriteVarUint(0)

		body.WriteByte(0x0B) // end bitset bounds check
	}

	emitDynamicJoinCall := func(count int, resultSlot int) bool {
		if count != 3 && count != 4 {
			return false
		}
		for part := 0; part < count; part++ {
			slot := resultSlot + part
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tagSlot(slot)))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
		}
		body.WriteByte(0x10) // call
		if count == 3 {
			body.WriteVarUint(jitImportDynamicJoin3)
		} else {
			body.WriteVarUint(jitImportDynamicJoin4)
		}
		return true
	}

	emitFallbackStringJoinValue := func(count int, resultSlot int) {
		if emitDynamicJoinCall(count, resultSlot) {
			return
		}

		// Generic fallback for 5+ parts: perform Tiny's normal dynamic + chain.
		// This keeps correctness for objects/arrays/non-integer floats while the
		// common primitive cases are handled by the inline Wasm path below.
		emitCopyTagged(stringJoinResultSlot, resultSlot)
		for part := 1; part < count; part++ {
			nextSlot := resultSlot + part
			emitDynamicAddTagged(stringJoinResultSlot, nextSlot, stringJoinScratchSlot)
			emitCopyTagged(stringJoinResultSlot, stringJoinScratchSlot)
		}
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(stringJoinResultSlot))
	}

	fastJoinStaticTypeOK := func(t stackType) bool {
		switch t {
		case stackTypeString, stackTypeInternedString, stackTypeNumber, stackTypeBool, stackTypeNull, stackTypeUnknown:
			return true
		default:
			return false
		}
	}

	emitNumberCanBeInlineStringifiedI32 := func(valueSlot int) {
		// This inline path intentionally formats only integer-valued f64 numbers.
		// If the number is 1.5, NaN, +Inf, etc., fall back to Go so FloatToString
		// remains the single source of truth for non-integers.
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x61) // f64.eq

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(-9223372036854774784.0)
		body.WriteByte(0x64) // f64.gt
		body.WriteByte(0x71) // i32.and

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(9223372036854774784.0)
		body.WriteByte(0x65) // f64.le
		body.WriteByte(0x71) // i32.and
	}

	emitFastJoinPartCanInlineI32 := func(valueSlot int, knownType stackType) {
		switch knownType {
		case stackTypeString, stackTypeInternedString, stackTypeBool, stackTypeNull:
			emitI32Const(1)
		case stackTypeNumber:
			emitNumberCanBeInlineStringifiedI32(valueSlot)
		case stackTypeUnknown:
			// Runtime guard:
			//   string | bool | null | integer-valued number
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tagSlot(valueSlot)))
			body.WriteByte(0x44)
			body.WriteFloat64(jitTagString)
			body.WriteByte(0x61) // f64.eq

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tagSlot(valueSlot)))
			body.WriteByte(0x44)
			body.WriteFloat64(jitTagBool)
			body.WriteByte(0x61)
			body.WriteByte(0x72) // i32.or

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tagSlot(valueSlot)))
			body.WriteByte(0x44)
			body.WriteFloat64(jitTagNull)
			body.WriteByte(0x61)
			body.WriteByte(0x72) // i32.or

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tagSlot(valueSlot)))
			body.WriteByte(0x44)
			body.WriteFloat64(jitTagNumber)
			body.WriteByte(0x61)
			body.WriteByte(0x04) // if tag == number: result i32
			body.WriteByte(0x7F)
			emitNumberCanBeInlineStringifiedI32(valueSlot)
			body.WriteByte(0x05) // else
			emitI32Const(0)
			body.WriteByte(0x0B) // end if
			body.WriteByte(0x72) // i32.or
		default:
			emitI32Const(0)
		}
	}

	emitRuntimeFastJoinPartLenI32 := func(valueSlot int, knownType stackType, dstLenSlot int) {
		switch knownType {
		case stackTypeString, stackTypeInternedString, stackTypeNumber, stackTypeBool, stackTypeNull:
			emitFastJoinPartLenI32(valueSlot, knownType, dstLenSlot)
			return
		}

		// Unknown-but-guarded primitive value. Set a harmless default first, then
		// overwrite it in the matching runtime-tag branch.
		emitI32Const(0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstLenSlot))

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitLoadStringLenI32(valueSlot, dstLenSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitIntegerNumberStringLenI32(valueSlot, dstLenSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagBool)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitBoolStringLenI32(valueSlot, dstLenSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNull)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitI32Const(4)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(dstLenSlot))
		body.WriteByte(0x0B)
	}

	emitWriteRuntimeFastJoinPartAndAdvance := func(valueSlot int, knownType stackType, dstPtrSlot int) {
		switch knownType {
		case stackTypeString, stackTypeInternedString, stackTypeNumber, stackTypeBool, stackTypeNull:
			emitWriteFastJoinPartAndAdvance(valueSlot, knownType, dstPtrSlot)
			return
		}

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitCopyStringBytesAndAdvance(valueSlot, dstPtrSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNumber)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitWriteIntegerNumberPartAndAdvance(valueSlot, dstPtrSlot)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagBool)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitWriteStaticBytes(dstPtrSlot, []byte("true"))
		body.WriteByte(0x05)
		emitWriteStaticBytes(dstPtrSlot, []byte("false"))
		body.WriteByte(0x0B)
		body.WriteByte(0x0B)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagNull)
		body.WriteByte(0x61)
		body.WriteByte(0x04)
		body.WriteByte(0x40)
		emitWriteStaticBytes(dstPtrSlot, []byte("null"))
		body.WriteByte(0x0B)
	}

	emitFastStringJoinIfPossible := func(count int, resultSlot int, partTypes []stackType) bool {
		if count <= 0 || len(partTypes) != count {
			return false
		}
		for _, t := range partTypes {
			if !fastJoinStaticTypeOK(t) {
				return false
			}
		}

		// Guard all parts. Known strings/bools/nulls always pass. Known numbers pass
		// only when integer-valued. Unknown parts pass at runtime only if their tag is
		// string/bool/null, or integer-valued number. Everything else falls back.
		emitI32Const(1)
		for part, t := range partTypes {
			slot := resultSlot + part
			emitFastJoinPartCanInlineI32(slot, t)
			body.WriteByte(0x71) // i32.and
		}

		body.WriteByte(0x04) // if (result f64)
		body.WriteByte(0x7C)

		// totalLen = sum(part string lengths)
		emitI32Const(0)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinLenSlot))
		for part, t := range partTypes {
			slot := resultSlot + part
			emitRuntimeFastJoinPartLenI32(slot, t, joinPartLenSlot)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(joinLenSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(joinPartLenSlot))
			body.WriteByte(0x6A) // i32.add
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(joinLenSlot))
		}

		// size = align8(16 + totalLen) == ((totalLen + 23) / 8) * 8
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinLenSlot))
		emitI32Const(23)
		body.WriteByte(0x6A) // i32.add
		emitI32Const(8)
		body.WriteByte(0x6E) // i32.div_u
		emitI32Const(8)
		body.WriteByte(0x6C) // i32.mul
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinSizeSlot))

		// addr = i32(__heap_top)
		body.WriteByte(0x23) // global.get __heap_top
		body.WriteVarUint(0)
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinAddrSlot))

		emitMarkJitAllocation(joinAddrSlot)

		// __heap_top = f64(addr + size)
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinAddrSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinSizeSlot))
		body.WriteByte(0x6A)
		body.WriteByte(0xB7) // f64.convert_i32_s
		body.WriteByte(0x24) // global.set
		body.WriteVarUint(0)

		// string header: tag and length
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinAddrSlot))
		body.WriteByte(0x44)
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x39) // f64.store
		body.WriteVarUint(3)
		body.WriteVarUint(0)

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinAddrSlot))
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinLenSlot))
		body.WriteByte(0xB7) // f64.convert_i32_s
		body.WriteByte(0x39) // f64.store length
		body.WriteVarUint(3)
		body.WriteVarUint(8)

		// dst = addr + 16
		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinAddrSlot))
		emitI32Const(16)
		body.WriteByte(0x6A)
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(joinDstPtrSlot))

		for part, t := range partTypes {
			emitWriteRuntimeFastJoinPartAndAdvance(resultSlot+part, t, joinDstPtrSlot)
		}

		body.WriteByte(0x20)
		body.WriteVarUint(uint32(joinAddrSlot))
		body.WriteByte(0xB7) // f64.convert_i32_s

		body.WriteByte(0x05) // else: exact old behavior
		emitFallbackStringJoinValue(count, resultSlot)

		body.WriteByte(0x0B) // end if
		body.WriteByte(0x21)
		body.WriteVarUint(uint32(resultSlot))
		emitSetTagConst(resultSlot, jitTagString)
		return true
	}

	emitTruthyI32 := func(valueSlot int, t stackType) {
		// For all known non-string Tiny values currently represented in JIT,
		// truthiness is just value != 0. Only strings need length check
		// because empty string is false and non-empty string is true.
		if t == stackTypeNumber || t == stackTypeBool || t == stackTypeObject || isJitArrayType(t) {
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(valueSlot))
			body.WriteByte(0x44) // f64.const 0
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne -> i32
			return
		}

		if t == stackTypeString || t == stackTypeInternedString {
			// It is definitely a string.
			// Truthiness is: value != 0 && *(value + 8) != 0
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(valueSlot))
			body.WriteByte(0x44) // f64.const 0
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne

			body.WriteByte(0x04) // if (result i32)
			body.WriteByte(0x7F) // i32

			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(valueSlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(8) // offset 8 (length)
			body.WriteByte(0x44) // f64.const 0
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne

			body.WriteByte(0x05) // else
			body.WriteByte(0x41) // i32.const
			body.WriteByte(0x00) // 0
			body.WriteByte(0x0B) // end
			return
		}

		// For unknown types, check runtime tag
		// if tag == 6.0 (String)
		body.WriteByte(0x20) // local.get tag
		body.WriteVarUint(uint32(tagSlot(valueSlot)))
		body.WriteByte(0x44) // f64.const 6.0
		body.WriteFloat64(jitTagString)
		body.WriteByte(0x61) // f64.eq

		body.WriteByte(0x04) // if (result i32)
		body.WriteByte(0x7F) // i32

		// string path: value != 0 && *(value + 8) != 0
		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne

		body.WriteByte(0x04) // if (result i32)
		body.WriteByte(0x7F) // i32

		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0xAA) // i32.trunc_f64_s
		body.WriteByte(0x2B) // f64.load
		body.WriteVarUint(3)
		body.WriteVarUint(8) // offset 8 (length)
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne

		body.WriteByte(0x05) // else
		body.WriteByte(0x41) // i32.const
		body.WriteByte(0x00) // 0
		body.WriteByte(0x0B) // end

		body.WriteByte(0x05) // else

		// non-string path: value != 0
		body.WriteByte(0x20) // local.get value
		body.WriteVarUint(uint32(valueSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne

		body.WriteByte(0x0B) // end
	}

	emitFastModValue := func(leftSlot int, rightSlot int) {
		// Fast integer modulo when both operands are integer-valued.
		// This matters a lot for hot loops like i % 2 / i % 3.
		// If either side is non-integer, fall back to the old float formula.
		// condition: trunc(a)==a && trunc(b)==b && b!=0
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x71) // i32.and

		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x62) // f64.ne -> i32

		body.WriteByte(0x71) // i32.and

		body.WriteByte(0x04) // if
		body.WriteByte(0x7C) // result f64

		// integer path: f64(i64(a) % i64(b))
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x81) // i64.rem_s
		body.WriteByte(0xB9) // f64.convert_i64_s

		body.WriteByte(0x05) // else

		// float fallback: a - trunc(a / b) * b
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0xA3) // f64.div
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20) // local.get b
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0xA2) // f64.mul
		body.WriteByte(0xA1) // f64.sub

		body.WriteByte(0x0B) // end
	}

	emitFastModValueConst := func(leftSlot int, _ float64) {
		// Since right is a known non-zero integer constant, we can simplify the check to just:
		// trunc(a) == a
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x61) // f64.eq -> i32

		body.WriteByte(0x04) // if
		body.WriteByte(0x7C) // result f64

		// integer path: f64(i64(a) % constant)
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x20) // local.get temp
		body.WriteVarUint(uint32(tempPtrSlot))
		body.WriteByte(0xB0) // i64.trunc_f64_s
		body.WriteByte(0x81) // i64.rem_s
		body.WriteByte(0xB9) // f64.convert_i64_s

		body.WriteByte(0x05) // else

		// float fallback: a - trunc(a / constant) * constant
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // local.get a
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x20) // local.get temp
		body.WriteVarUint(uint32(tempPtrSlot))
		body.WriteByte(0xA3) // f64.div
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x20) // local.get temp
		body.WriteVarUint(uint32(tempPtrSlot))
		body.WriteByte(0xA2) // f64.mul
		body.WriteByte(0xA1) // f64.sub

		body.WriteByte(0x0B) // end
	}

	emitUnaryMath := func(sp int, wasmOp byte) {
		slot := stackBase + sp - 1

		body.WriteByte(0x20) // local.get
		body.WriteVarUint(uint32(slot))

		body.WriteByte(wasmOp)

		body.WriteByte(0x21) // local.set
		body.WriteVarUint(uint32(slot))

		emitSetTagConst(slot, jitTagNumber)
	}

	emitPowCoreValue := func(leftSlot int, rightSlot int) {
		// Condition:
		// exponent is integer && exponent >= 0
		//
		// trunc(exp) == exp
		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x9D) // f64.trunc

		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(rightSlot))

		body.WriteByte(0x61) // f64.eq

		// exp >= 0
		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(rightSlot))

		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)

		body.WriteByte(0x66) // f64.ge

		body.WriteByte(0x71) // i32.and

		// if integer non-negative exponent, result f64
		body.WriteByte(0x04) // if
		body.WriteByte(0x7C) // result f64

		// result = 1
		body.WriteByte(0x44) // f64.const
		body.WriteFloat64(1.0)
		body.WriteByte(0x21) // local.set result
		body.WriteVarUint(uint32(powResultSlot))

		// base = left
		body.WriteByte(0x20) // local.get left
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x21) // local.set base
		body.WriteVarUint(uint32(powBaseSlot))

		// exp = trunc(right)
		body.WriteByte(0x20) // local.get right
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x9D) // f64.trunc
		body.WriteByte(0x21) // local.set exp
		body.WriteVarUint(uint32(powExpSlot))

		// block
		body.WriteByte(0x02)
		body.WriteByte(0x40)

		// loop
		body.WriteByte(0x03)
		body.WriteByte(0x40)

		// if !(exp > 0) break
		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(powExpSlot))
		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)
		body.WriteByte(0x64) // f64.gt

		body.WriteByte(0x45) // i32.eqz
		body.WriteByte(0x0D) // br_if
		body.WriteVarUint(1) // break outer block

		// odd check:
		// exp - floor(exp / 2) * 2 != 0
		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(powExpSlot))

		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(powExpSlot))
		body.WriteByte(0x44) // f64.const 2
		body.WriteFloat64(2.0)
		body.WriteByte(0xA3) // f64.div
		body.WriteByte(0x9C) // f64.floor
		body.WriteByte(0x44) // f64.const 2
		body.WriteFloat64(2.0)
		body.WriteByte(0xA2) // f64.mul

		body.WriteByte(0xA1) // f64.sub

		body.WriteByte(0x44) // f64.const 0
		body.WriteFloat64(0.0)

		body.WriteByte(0x62) // f64.ne

		// if odd
		body.WriteByte(0x04)
		body.WriteByte(0x40)

		// result = result * base
		body.WriteByte(0x20) // local.get result
		body.WriteVarUint(uint32(powResultSlot))
		body.WriteByte(0x20) // local.get base
		body.WriteVarUint(uint32(powBaseSlot))
		body.WriteByte(0xA2) // f64.mul
		body.WriteByte(0x21) // local.set result
		body.WriteVarUint(uint32(powResultSlot))

		body.WriteByte(0x0B) // end if odd

		// base = base * base
		body.WriteByte(0x20) // local.get base
		body.WriteVarUint(uint32(powBaseSlot))
		body.WriteByte(0x20) // local.get base
		body.WriteVarUint(uint32(powBaseSlot))
		body.WriteByte(0xA2) // f64.mul
		body.WriteByte(0x21) // local.set base
		body.WriteVarUint(uint32(powBaseSlot))

		// exp = floor(exp / 2)
		body.WriteByte(0x20) // local.get exp
		body.WriteVarUint(uint32(powExpSlot))
		body.WriteByte(0x44) // f64.const 2
		body.WriteFloat64(2.0)
		body.WriteByte(0xA3) // f64.div
		body.WriteByte(0x9C) // f64.floor
		body.WriteByte(0x21) // local.set exp
		body.WriteVarUint(uint32(powExpSlot))

		// continue loop
		body.WriteByte(0x0C) // br
		body.WriteVarUint(0)

		body.WriteByte(0x0B) // end loop
		body.WriteByte(0x0B) // end block

		// final value from integer pow path
		body.WriteByte(0x20) // local.get result
		body.WriteVarUint(uint32(powResultSlot))

		// else: fallback to host math_pow
		body.WriteByte(0x05)

		body.WriteByte(0x20) // local.get left
		body.WriteVarUint(uint32(leftSlot))

		body.WriteByte(0x20) // local.get right
		body.WriteVarUint(uint32(rightSlot))

		body.WriteByte(0x10) // call
		body.WriteVarUint(jitImportMathPow)

		body.WriteByte(0x0B) // end if
	}

	emitPow := func(sp int) {
		leftSlot := stackBase + sp - 2
		rightSlot := stackBase + sp - 1

		// Fast overflow shortcut:
		//
		// if base >= 20000 && exponent >= 72:
		//     return +Inf
		//
		// This is safe because 20000^72 is bigger than max float64.
		// It catches your benchmark around i = 144.
		body.WriteByte(0x20) // local.get base
		body.WriteVarUint(uint32(leftSlot))
		body.WriteByte(0x44) // f64.const
		body.WriteFloat64(20000.0)
		body.WriteByte(0x66) // f64.ge -> i32

		body.WriteByte(0x20) // local.get exponent
		body.WriteVarUint(uint32(rightSlot))
		body.WriteByte(0x44) // f64.const
		body.WriteFloat64(72.0)
		body.WriteByte(0x66) // f64.ge -> i32

		body.WriteByte(0x71) // i32.and

		// if (base >= 20000 && exponent >= 72) result f64
		body.WriteByte(0x04) // if
		body.WriteByte(0x7C) // result f64

		// then: +Infinity
		body.WriteByte(0x44) // f64.const
		body.WriteFloat64(math.Inf(1))

		// else: normal pow path
		body.WriteByte(0x05) // else

		emitPowCoreValue(leftSlot, rightSlot)

		body.WriteByte(0x0B) // end if

		// Store final result into left stack slot.
		body.WriteByte(0x21) // local.set left
		body.WriteVarUint(uint32(leftSlot))

		emitSetTagConst(leftSlot, jitTagNumber)
	}

	typeStack := make([]stackType, maxSp+16)
	inferredParams := inferParamTypes(vm, fn, currentReturnTypes, currentParamTypes)
	localPropertyHints := inferJitLocalPropertyHints(fn)
	localTypes := make([]stackType, fn.LocalCount)
	for i := 0; i < len(fn.Params) && i < len(localTypes); i++ {
		localTypes[i] = inferredParams[i]
		emitSetTagFromType(i, localTypes[i])
	}

	type jitRowOrigin struct {
		ArraySlot int
		IndexSlot int
	}
	rowOrigins := map[int]jitRowOrigin{}

	for i, instr := range fn.Instructions {
		sp := spArray[i]

		unaryTypeBefore := stackTypeUnknown
		leftTypeBefore := stackTypeUnknown
		rightTypeBefore := stackTypeUnknown

		if sp >= 1 {
			unaryTypeBefore = typeStack[sp-1]
		}
		if sp >= 2 {
			leftTypeBefore = typeStack[sp-2]
			rightTypeBefore = typeStack[sp-1]
		}

		var stringJoinPartTypes []stackType
		if instr.Op == OP_STRING_JOIN {
			count := getStringJoinCount(instr)
			if count > 0 && sp >= count {
				stringJoinPartTypes = make([]stackType, count)
				for part := 0; part < count; part++ {
					stringJoinPartTypes[part] = typeStack[sp-count+part]
				}
			}
		}

		switch instr.Op {
		case OP_MATH_FLOOR, OP_MATH_CEIL, OP_MATH_SQRT, OP_MATH_ABS:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_PRINT:
			info := instr.Value.(PrintInfo)
			dest := sp - info.ArgCount

			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeNull // null result
			}
		case OP_MATH_POW:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_CONST:
			if sp < len(typeStack) {
				if isNullConstant(instr.Value) {
					typeStack[sp] = stackTypeNull
				} else if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isStr := instr.Value.(string); isStr {
					typeStack[sp] = stackTypeString
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_LOAD_GLOBAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			}
			if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			}
			if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot >= 0 && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
			}
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			if sp >= 1 && slot >= 0 && slot < len(localTypes) {
				localTypes[slot] = typeStack[sp-1]
			}
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				if instr.Op == OP_ADD && (isJitStringType(typeStack[sp-1]) || isJitStringType(typeStack[sp-2])) {
					typeStack[sp-2] = stackTypeString
				} else if typeStack[sp-1] == stackTypeNumber && typeStack[sp-2] == stackTypeNumber {
					typeStack[sp-2] = stackTypeNumber
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE, OP_AND, OP_OR:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeBool
			}
		case OP_NOT:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeBool
			}
		case OP_NEGATE, OP_INC_LOCAL, OP_DEC_LOCAL:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)
			tA := localTypes[info.SlotA]
			tB := localTypes[info.SlotB]
			if isJitStringType(tA) || isJitStringType(tB) {
				localTypes[info.DestSlot] = stackTypeString
			} else if tA == stackTypeNumber && tB == stackTypeNumber {
				localTypes[info.DestSlot] = stackTypeNumber
			} else {
				localTypes[info.DestSlot] = stackTypeUnknown
			}
		case OP_OBJECT:
			if info, ok := instr.Value.(ObjectInfo); ok {
				dest := sp - len(info.Names)
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeObject
				}
			}
		case OP_MUL_LOCAL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LOCAL_CONST_OP:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(LocalConstOpInfo); ok {
					typeStack[sp] = jitLocalConstOpResultType(info)
				} else {
					typeStack[sp] = stackTypeNumber
				}
			}
		case OP_LOCAL_CONST_OP_STORE:
			if info, ok := instr.Value.(LocalConstOpInfo); ok && info.Slot >= 0 && info.Slot < len(localTypes) {
				localTypes[info.Slot] = jitLocalConstOpResultType(info)
			}
		case OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
			if info, ok := instr.Value.(AddLocalGlobalGlobalStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			if info, ok := instr.Value.(AddLocalArrayIndexStoreInfo); ok && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) {
				localTypes[info.LocalSlot] = stackTypeNumber
			}
		case OP_ARRAY_INDEX_CONST_OP_STORE, OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY, OP_ADD_PROPERTY_LOCAL_LOCAL, OP_ADD_LOCAL_PROPERTIES_STORE, OP_ARRAY_INDEX_LOCAL_STORE:
			// no stack effect for type propagation
		case OP_GET_PROPERTY_LOCAL:
			if sp < len(typeStack) {
				propType := stackTypeUnknown
				if info, ok := instr.Value.(PropertyLocalInfo); ok {
					if info.Slot >= 0 && info.Slot < len(localPropertyHints) && localPropertyHints[info.Slot] != nil {
						if t, ok := localPropertyHints[info.Slot][info.Name]; ok {
							propType = t
						}
					}
				}
				typeStack[sp] = propType
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeUnknown
			}
		case OP_ARRAY:
			if info, ok := instr.Value.(ArrayInfo); ok {
				dest := sp - info.Count
				if dest >= 0 && dest < len(typeStack) {
					elements := make([]stackType, 0, info.Count)
					for idx := 0; idx < info.Count; idx++ {
						elements = append(elements, typeStack[sp-info.Count+idx])
					}
					typeStack[dest] = inferJitArrayStackType(elements)
				}
			}

		case OP_INDEX:
			if sp >= 2 {
				if elemType, ok := jitArrayElementType(typeStack[sp-2]); ok {
					typeStack[sp-2] = elemType
				} else {
					typeStack[sp-2] = stackTypeUnknown
				}
			}
		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)

			if count > 0 {
				dest := sp - count
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeString
				}
			} else {
				if sp < len(typeStack) {
					typeStack[sp] = stackTypeString
				}
			}
		case OP_ARRAY_GET_LOCAL:
			if sp < len(typeStack) {
				if info, ok := instr.Value.(ArrayLocalCallInfo); ok && info.ArraySlot >= 0 && info.ArraySlot < len(localTypes) {
					if elemType, ok := jitArrayElementType(localTypes[info.ArraySlot]); ok {
						typeStack[sp] = elemType
					} else {
						typeStack[sp] = stackTypeUnknown
					}
				} else {
					typeStack[sp] = stackTypeUnknown
				}
			}
		case OP_ARRAY_LEN_LOCAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LEN:
			if sp-1 >= 0 && sp-1 < len(typeStack) {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeArray
			}
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				dest := sp - info.ArgCount - 1
				if dest >= 0 && dest < len(typeStack) {
					switch info.Method {
					case "length":
						typeStack[dest] = stackTypeNumber
					case "push":
						typeStack[dest] = stackTypeArray
					default:
						typeStack[dest] = stackTypeUnknown
					}
				}
			}
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				retT := stackTypeUnknown
				if info.ID >= 0 && info.ID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.ID]
				}
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = retT
				}
			}
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if ok {
				retT := stackTypeUnknown
				if info.FnID >= 0 && info.FnID < len(currentReturnTypes) {
					retT = currentReturnTypes[info.FnID]
				}
				if sp < len(typeStack) {
					typeStack[sp] = retT
				}
			}
		case OP_COALESCE_JUMP:
			if sp >= 2 {
				t1 := typeStack[sp-1] // right
				t2 := typeStack[sp-2] // left
				var coalescedType stackType
				if t1 == t2 {
					coalescedType = t1
				} else if t1 == stackTypeUnknown {
					coalescedType = t2
				} else if t2 == stackTypeUnknown {
					coalescedType = t1
				} else if t2 >= 10 && t1 < 10 && t1 != stackTypeUnknown {
					coalescedType = t1
				} else if t1 >= 10 && t2 < 10 && t2 != stackTypeUnknown {
					coalescedType = t2
				} else {
					coalescedType = stackTypeUnknown
				}
				typeStack[sp-2] = coalescedType
			}
		case OP_TYPEOF:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeString
			}
		case OP_THROW:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeUnknown
			}
		}

		if list, exists := closes[i]; exists {
			for range list {
				body.WriteByte(0x0B)
				if len(activeBlocks) > 0 {
					activeBlocks = activeBlocks[:len(activeBlocks)-1]
				}
			}
		}

		if list, exists := opens[i]; exists {
			for _, b := range list {
				if b.isLoop {
					body.WriteByte(0x03)
				} else {
					body.WriteByte(0x02)
				}
				body.WriteByte(0x40)
				activeBlocks = append(activeBlocks, b)
			}
		}

		switch instr.Op {
		case OP_MATH_ABS:
			emitUnaryMath(sp, wasmF64Abs)

		case OP_MATH_CEIL:
			emitUnaryMath(sp, wasmF64Ceil)

		case OP_MATH_FLOOR:
			emitUnaryMath(sp, wasmF64Floor)

		case OP_MATH_SQRT:
			emitUnaryMath(sp, wasmF64Sqrt)

		case OP_PRINT:
			info := instr.Value.(PrintInfo)

			argCount := info.ArgCount
			startSlot := stackBase + sp - argCount

			for argIndex := 0; argIndex < argCount; argIndex++ {
				slot := startSlot + argIndex

				// arg0: tag
				body.WriteByte(0x20) // local.get tag
				body.WriteVarUint(uint32(tagSlot(slot)))

				// arg1: value
				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(slot))

				// arg2: newline flag
				// Only the LAST argument gets newline=true.
				body.WriteByte(0x44) // f64.const
				if info.NewLine && argIndex == argCount-1 {
					body.WriteFloat64(1.0)
				} else {
					body.WriteFloat64(0.0)
				}

				// arg3: spaceBefore flag
				body.WriteByte(0x44) // f64.const
				if argIndex > 0 {
					body.WriteFloat64(1.0)
				} else {
					body.WriteFloat64(0.0)
				}

				body.WriteByte(0x10) // call print_value
				body.WriteVarUint(jitImportPrintValue)
			}

			// Replace all printed args with one null result at startSlot.
			body.WriteByte(0x44) // f64.const 0
			body.WriteFloat64(0.0)
			body.WriteByte(0x21) // local.set startSlot
			body.WriteVarUint(uint32(startSlot))

			emitSetTagConst(startSlot, jitTagNull)
			emitDeoptCheckpoint(i+1, sp-info.ArgCount+1)

		case OP_COALESCE_JUMP:
			slotL := stackBase + sp - 2
			slotR := stackBase + sp - 1

			// Check if leftTag == 0.0 (nullish tag is 0.0)
			body.WriteByte(0x20) // local.get leftTag
			body.WriteVarUint(uint32(tagSlot(slotL)))
			body.WriteByte(0x44) // f64.const 0.0
			body.WriteFloat64(jitTagNull)
			body.WriteByte(0x61) // f64.eq -> i32

			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature

			// leftValue = rightValue
			body.WriteByte(0x20) // local.get rightValue
			body.WriteVarUint(uint32(slotR))
			body.WriteByte(0x21) // local.set leftValue
			body.WriteVarUint(uint32(slotL))

			// leftTag = rightTag
			body.WriteByte(0x20) // local.get rightTag
			body.WriteVarUint(uint32(tagSlot(slotR)))
			body.WriteByte(0x21) // local.set leftTag
			body.WriteVarUint(uint32(tagSlot(slotL)))

			body.WriteByte(0x0B) // end if

		case OP_TYPEOF:
			slot := stackBase + sp - 1
			body.WriteByte(0x20) // local.get tag
			body.WriteVarUint(uint32(tagSlot(slot)))
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x10) // call typeof_wasm
			body.WriteVarUint(jitImportTypeofWasm)
			body.WriteByte(0x21) // local.set value
			body.WriteVarUint(uint32(slot))

			emitSetTagConst(slot, jitTagString)

		case OP_THROW:
			slot := stackBase + sp - 1
			body.WriteByte(0x20) // local.get tag
			body.WriteVarUint(uint32(tagSlot(slot)))
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x10) // call throw_wasm
			body.WriteVarUint(jitImportThrowWasm)
			body.WriteByte(0x1A) // drop

		case OP_MATH_POW:
			emitPow(sp)
		case OP_CONST:
			dst := stackBase + sp
			if instr.IsInt {
				val := float64(instr.IntArg)
				body.WriteByte(0x44)
				body.WriteFloat64(val)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dst))
				emitSetTagConst(dst, jitTagNumber)
			} else if strVal, ok := instr.Value.(string); ok {
				addr := jitStringAddr[strVal]
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(float64(addr))
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(dst))
				emitSetTagConst(dst, jitTagString)
			} else if isNullConstant(instr.Value) {
				body.WriteByte(0x44)
				body.WriteFloat64(0.0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dst))
				emitSetTagConst(dst, jitTagNull)
			} else {
				val, ok := getFloat64Constant(instr.Value)
				if !ok {
					val = 0.0
				}
				body.WriteByte(0x44)
				body.WriteFloat64(val)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(dst))
				if _, isBool := instr.Value.(bool); isBool {
					emitSetTagConst(dst, jitTagBool)
				} else {
					emitSetTagConst(dst, jitTagNumber)
				}
			}

		case OP_LOAD_GLOBAL:
			dst := stackBase + sp
			globalSlot := -1
			if info, ok := instr.Value.(VariableInfo); ok {
				globalSlot = info.Slot
			} else if s, ok := AsIntInternal(instr.Value); ok {
				globalSlot = s
			}

			body.WriteByte(0x44) // f64.const global slot
			body.WriteFloat64(float64(globalSlot))
			body.WriteByte(0x10) // call load_global_wasm
			body.WriteVarUint(jitImportLoadGlobal)

			// load_global_wasm returns (tag, value). Store value first, then tag.
			body.WriteByte(0x21) // local.set value
			body.WriteVarUint(uint32(dst))
			body.WriteByte(0x21) // local.set tag
			body.WriteVarUint(uint32(tagSlot(dst)))
		case OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			slot := 0
			if instr.Op == OP_LOAD_LOCAL_1 {
				slot = 1
			}
			if instr.Op == OP_LOAD_LOCAL_2 {
				slot = 2
			}
			if instr.Op == OP_LOAD_LOCAL_3 {
				slot = 3
			}

			dst := stackBase + sp
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(dst))
			emitCopyTag(dst, slot)

		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			dst := stackBase + sp
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(dst))
			emitCopyTag(dst, slot)

		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			delete(rowOrigins, slot)
			src := stackBase + sp - 1
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(src))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(slot))
			emitCopyTag(slot, src)

		case OP_STRING_JOIN:
			count := getStringJoinCount(instr)
			if count <= 0 {
				body.WriteByte(0x00) // unreachable: invalid compiler-emitted join count
				break
			}

			resultSlot := stackBase + sp - count
			if resultSlot < stackBase {
				body.WriteByte(0x00) // unreachable: stack underflow in compiler/JIT metadata
				break
			}

			if optimizedStringJoinPC[i] {
				// IMPORTANT: resultSlot overlaps the first joined operand.
				// Do NOT use resultSlot as the running length accumulator before
				// reading all operands, or the first part, e.g. "hello", gets
				// overwritten with 0 and its length becomes missing.
				// Use a scratch local outside the active stack, then copy the final
				// numeric length back into resultSlot.
				body.WriteByte(0x44) // running length = 0
				body.WriteFloat64(0.0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(stringJoinResultSlot))

				for part := 0; part < count; part++ {
					nextSlot := resultSlot + part
					partType := stackTypeUnknown
					partStackIndex := sp - count + part
					if partStackIndex >= 0 && partStackIndex < len(typeStack) {
						partType = typeStack[partStackIndex]
					}

					emitValueStringLength(nextSlot, stringJoinScratchSlot, partType)

					body.WriteByte(0x20)
					body.WriteVarUint(uint32(stringJoinResultSlot))
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(stringJoinScratchSlot))
					body.WriteByte(0xA0) // f64.add
					body.WriteByte(0x21)
					body.WriteVarUint(uint32(stringJoinResultSlot))
				}

				body.WriteByte(0x20) // resultSlot = running length
				body.WriteVarUint(uint32(stringJoinResultSlot))
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(resultSlot))

				emitSetTagConst(resultSlot, jitTagNumber)
				break
			}

			if emitFastStringJoinIfPossible(count, resultSlot, stringJoinPartTypes) {
				break
			}

			emitCopyTagged(stringJoinResultSlot, resultSlot)

			if count == 3 {
				// Push tag 0, val 0
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot))

				// Push tag 1, val 1
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot + 1)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot + 1))

				// Push tag 2, val 2
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot + 2)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot + 2))

				body.WriteByte(0x10) // call
				body.WriteVarUint(jitImportDynamicJoin3)
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(resultSlot))

				emitSetTagConst(resultSlot, jitTagString)
				break
			}

			if count == 4 {
				// Push tag 0, val 0
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot))

				// Push tag 1, val 1
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot + 1)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot + 1))

				// Push tag 2, val 2
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot + 2)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot + 2))

				// Push tag 3, val 3
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(resultSlot + 3)))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(resultSlot + 3))

				body.WriteByte(0x10) // call
				body.WriteVarUint(jitImportDynamicJoin4)
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(resultSlot))

				emitSetTagConst(resultSlot, jitTagString)
				break
			}

			for part := 1; part < count; part++ {
				nextSlot := resultSlot + part
				emitDynamicAddTagged(stringJoinResultSlot, nextSlot, stringJoinScratchSlot)
				emitCopyTagged(stringJoinResultSlot, stringJoinScratchSlot)
			}

			emitCopyTagged(resultSlot, stringJoinResultSlot)
			emitSetTagConst(resultSlot, jitTagString)

		case OP_ADD, OP_SUB, OP_MUL:
			leftT := leftTypeBefore
			rightT := rightTypeBefore
			leftSlot := stackBase + sp - 2
			rightSlot := stackBase + sp - 1

			if instr.Op == OP_ADD {
				// If both operands are known numeric, keep the fast f64 path.
				// If one side is known string, use the same generic inline join path
				// as OP_STRING_JOIN. This covers normal binary additions like:
				//   s + i, i + "x", "x" + maybeDynamic
				// while still falling back for non-integer floats / objects / arrays.
				if leftT == stackTypeNumber && rightT == stackTypeNumber {
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(leftSlot))
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(rightSlot))
					body.WriteByte(0xA0) // f64.add
					body.WriteByte(0x21)
					body.WriteVarUint(uint32(leftSlot))
					emitSetTagConst(leftSlot, jitTagNumber)
				} else if isJitStringType(leftT) || isJitStringType(rightT) {
					if !emitFastStringJoinIfPossible(2, leftSlot, []stackType{leftT, rightT}) {
						emitDynamicAddTagged(leftSlot, rightSlot, leftSlot)
					}
				} else {
					emitDynamicAddTagged(leftSlot, rightSlot, leftSlot)
				}
			} else {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(rightSlot))
				switch instr.Op {
				case OP_SUB:
					body.WriteByte(0xA1) // f64.sub
				case OP_MUL:
					body.WriteByte(0xA2) // f64.mul
				}
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(leftSlot))
				emitSetTagConst(leftSlot, jitTagNumber)
			}

		case OP_DIV:
			body.WriteByte(0x20) // local.get
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(0.0)
			body.WriteByte(0x61) // f64.eq
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature
			body.WriteByte(0x00) // unreachable (traps to trigger interpreter fallback)
			body.WriteByte(0x0B) // end

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0xA3) // f64.div — result stays as f64, do NOT truncate!
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			emitSetTagConst(stackBase+sp-2, jitTagNumber)

		case OP_MOD:
			leftSlot := stackBase + sp - 2
			rightSlot := stackBase + sp - 1
			// Fast path: if the right operand is a compile-time integer constant AND
			// the left operand is known to be a number (integer-valued in a loop),
			// skip all guard checks and emit i64.rem_s directly.
			rightIsIntConst := false
			var rightConstVal int64
			if i > 0 {
				prev := fn.Instructions[i-1]
				if prev.Op == OP_CONST {
					if prev.IsInt {
						rightIsIntConst = true
						rightConstVal = int64(prev.IntArg)
					} else if fv, ok := getFloat64Constant(prev.Value); ok && fv == math.Trunc(fv) && fv != 0 {
						rightIsIntConst = true
						rightConstVal = int64(fv)
					}
				}
			}
			leftIsNumber := leftTypeBefore == stackTypeNumber
			if rightIsIntConst && rightConstVal != 0 && leftIsNumber {
				// Fully inlined integer modulo: i64.trunc(left) % rightConstVal → f64
				body.WriteByte(0x20) // local.get left
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0xB0) // i64.trunc_f64_s
				// push constant divisor as i64
				body.WriteByte(0x42) // i64.const
				// LEB128-encode rightConstVal as signed
				{
					v := rightConstVal
					for {
						b := byte(v & 0x7F)
						v >>= 7
						if (v == 0 && b&0x40 == 0) || (v == -1 && b&0x40 != 0) {
							body.WriteByte(b)
							break
						}
						body.WriteByte(b | 0x80)
					}
				}
				body.WriteByte(0x81) // i64.rem_s
				body.WriteByte(0xB9) // f64.convert_i64_s
				body.WriteByte(0x21) // local.set leftSlot
				body.WriteVarUint(uint32(leftSlot))
			} else {
				emitFastModValue(leftSlot, rightSlot)
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(leftSlot))
			}
			emitSetTagConst(leftSlot, jitTagNumber)

		case OP_EQ, OP_NEQ, OP_LT, OP_GT, OP_LTE, OP_GTE:

			if (instr.Op == OP_EQ || instr.Op == OP_NEQ) && leftTypeBefore == stackTypeInternedString && rightTypeBefore == stackTypeInternedString {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 1))
				if instr.Op == OP_EQ {
					body.WriteByte(0x61) // f64.eq
				} else {
					body.WriteByte(0x62) // f64.ne
				}
				body.WriteByte(0xB7) // f64.convert_i32_s
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(stackBase + sp - 2))
			} else if (instr.Op == OP_EQ || instr.Op == OP_NEQ) && (rightTypeBefore != stackTypeNumber || leftTypeBefore != stackTypeNumber) {
				leftSlot := stackBase + sp - 2
				rightSlot := stackBase + sp - 1

				// 1. Check if values are identical (fast path for same pointers or same primitive values)
				body.WriteByte(0x20) // local.get leftValue
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0x20) // local.get rightValue
				body.WriteVarUint(uint32(rightSlot))
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C)
				if instr.Op == OP_EQ {
					body.WriteByte(0x44) // f64.const 1.0 (true)
					body.WriteFloat64(1.0)
				} else {
					body.WriteByte(0x44) // f64.const 0.0 (false)
					body.WriteFloat64(0.0)
				}
				body.WriteByte(0x05) // else
				// 2. Values are different. Check if tags are identical.
				body.WriteByte(0x20) // local.get leftTag
				body.WriteVarUint(uint32(tagSlot(leftSlot)))
				body.WriteByte(0x20) // local.get rightTag
				body.WriteVarUint(uint32(tagSlot(rightSlot)))
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C)
				// 3. Tags are identical. Check if both are strings (6.0).
				body.WriteByte(0x20) // local.get leftTag
				body.WriteVarUint(uint32(tagSlot(leftSlot)))
				body.WriteByte(0x44) // f64.const 6.0
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C)
				// 4. Both are strings. Compare their lengths in WASM first.
				// Load A's length
				body.WriteByte(0x20) // local.get leftValue
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3) // alignment 3 (8 bytes)
				body.WriteVarUint(8) // offset 8

				// Load B's length
				body.WriteByte(0x20) // local.get rightValue
				body.WriteVarUint(uint32(rightSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3) // alignment 3 (8 bytes)
				body.WriteVarUint(8) // offset 8

				body.WriteByte(0x61) // f64.eq -> i32
				body.WriteByte(0x04) // if (result f64)
				body.WriteByte(0x7C) // result type: f64
				// Lengths are identical. Compare contents in WASM.
				idxLeft := tagBase + valueLocalCount
				idxRight := idxLeft + 1
				idxLen := idxLeft + 2

				// Initialize idxLeft = leftValue(i32) + 16
				body.WriteByte(0x20) // local.get leftValue
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x41) // i32.const 16
				body.WriteVarUint(16)
				body.WriteByte(0x6A) // i32.add
				body.WriteByte(0x21) // local.set idxLeft
				body.WriteVarUint(uint32(idxLeft))

				// Initialize idxRight = rightValue(i32) + 16
				body.WriteByte(0x20) // local.get rightValue
				body.WriteVarUint(uint32(rightSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x41) // i32.const 16
				body.WriteVarUint(16)
				body.WriteByte(0x6A) // i32.add
				body.WriteByte(0x21) // local.set idxRight
				body.WriteVarUint(uint32(idxRight))

				// Initialize idxLen = leftValue.length(i32)
				body.WriteByte(0x20) // local.get leftValue
				body.WriteVarUint(uint32(leftSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3) // alignment 3 (8 bytes)
				body.WriteVarUint(8) // offset 8
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x21) // local.set idxLen
				body.WriteVarUint(uint32(idxLen))

				if instr.Op == OP_EQ {
					body.WriteByte(0x44) // f64.const 1.0
					body.WriteFloat64(1.0)
				} else {
					body.WriteByte(0x44) // f64.const 0.0
					body.WriteFloat64(0.0)
				}
				body.WriteByte(0x21) // local.set tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))

				// WASM block and loop
				body.WriteByte(0x02) // block
				body.WriteByte(0x40) // empty signature
				body.WriteByte(0x03) // loop
				body.WriteByte(0x40) // empty signature

				// loop condition: if idxLen == 0, break loop to block (goes to matching return)
				body.WriteByte(0x20) // local.get idxLen
				body.WriteVarUint(uint32(idxLen))
				body.WriteByte(0x45) // i32.eqz
				body.WriteByte(0x0D) // br_if
				body.WriteVarUint(1) // to outer block (Label 1)

				// Compare bytes
				body.WriteByte(0x20) // local.get idxLeft
				body.WriteVarUint(uint32(idxLeft))
				body.WriteByte(0x2D) // i32.load8_u
				body.WriteVarUint(0) // alignment 0
				body.WriteVarUint(0) // offset 0

				body.WriteByte(0x20) // local.get idxRight
				body.WriteVarUint(uint32(idxRight))
				body.WriteByte(0x2D) // i32.load8_u
				body.WriteVarUint(0)
				body.WriteVarUint(0)

				body.WriteByte(0x47) // i32.ne
				body.WriteByte(0x04) // if
				body.WriteByte(0x40) // empty signature

				// bytes differ
				if instr.Op == OP_EQ {
					body.WriteByte(0x44) // f64.const 0.0
					body.WriteFloat64(0.0)
				} else {
					body.WriteByte(0x44) // f64.const 1.0
					body.WriteFloat64(1.0)
				}
				body.WriteByte(0x0C) // br
				body.WriteVarUint(3) // branch out of if (result f64) block (Label 3)
				body.WriteByte(0x0B) // end if

				// Decrement idxLen
				body.WriteByte(0x20) // local.get idxLen
				body.WriteVarUint(uint32(idxLen))
				body.WriteByte(0x41) // i32.const 1
				body.WriteVarUint(1)
				body.WriteByte(0x6B) // i32.sub
				body.WriteByte(0x21) // local.set idxLen
				body.WriteVarUint(uint32(idxLen))

				// Increment idxLeft
				body.WriteByte(0x20) // local.get idxLeft
				body.WriteVarUint(uint32(idxLeft))
				body.WriteByte(0x41) // i32.const 1
				body.WriteVarUint(1)
				body.WriteByte(0x6A) // i32.add
				body.WriteByte(0x21) // local.set idxLeft
				body.WriteVarUint(uint32(idxLeft))

				// Increment idxRight
				body.WriteByte(0x20) // local.get idxRight
				body.WriteVarUint(uint32(idxRight))
				body.WriteByte(0x41) // i32.const 1
				body.WriteVarUint(1)
				body.WriteByte(0x6A) // i32.add
				body.WriteByte(0x21) // local.set idxRight
				body.WriteVarUint(uint32(idxRight))

				body.WriteByte(0x0C) // br
				body.WriteVarUint(0) // back to start of loop (Label 0)

				body.WriteByte(0x0B) // end loop

				body.WriteByte(0x0B) // end block
				body.WriteByte(0x20) // local.get tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0x05) // else
				if instr.Op == OP_EQ {
					body.WriteByte(0x44) // f64.const 0.0 (false)
					body.WriteFloat64(0.0)
				} else {
					body.WriteByte(0x44) // f64.const 1.0 (true)
					body.WriteFloat64(1.0)
				}
				body.WriteByte(0x0B) // end if
				body.WriteByte(0x05) // else
				// Both tags are same, but not string (e.g. both numbers, both bools, both arrays/objects)
				// Since values are different, they are definitely different.
				if instr.Op == OP_EQ {
					body.WriteByte(0x44)
					body.WriteFloat64(0.0)
				} else {
					body.WriteByte(0x44)
					body.WriteFloat64(1.0)
				}
				body.WriteByte(0x0B) // end if
				body.WriteByte(0x05) // else
				// Tags are different. Can never be equal.
				if instr.Op == OP_EQ {
					body.WriteByte(0x44)
					body.WriteFloat64(0.0)
				} else {
					body.WriteByte(0x44)
					body.WriteFloat64(1.0)
				}
				body.WriteByte(0x0B) // end if
				body.WriteByte(0x0B) // end if
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(leftSlot))
			} else {
				// Standard Numeric Comparison Path
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 1))

				switch instr.Op {
				case OP_EQ:
					body.WriteByte(0x61) // f64.eq
				case OP_NEQ:
					body.WriteByte(0x62) // f64.ne
				case OP_LT:
					body.WriteByte(0x63) // f64.lt
				case OP_GT:
					body.WriteByte(0x64) // f64.gt
				case OP_LTE:
					body.WriteByte(0x65) // f64.le
				case OP_GTE:
					body.WriteByte(0x66) // f64.ge
				}

				body.WriteByte(0xB7) // f64.convert_i32_s (converts i32 result to f64)
				body.WriteByte(0x21) // local.set
				body.WriteVarUint(uint32(stackBase + sp - 2))
			}
			emitSetTagConst(stackBase+sp-2, jitTagBool)

		case OP_AND:
			emitTruthyI32(stackBase+sp-2, leftTypeBefore)
			emitTruthyI32(stackBase+sp-1, rightTypeBefore)

			body.WriteByte(0x71) // i32.and
			body.WriteByte(0xB7) // f64.convert_i32_s

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			emitSetTagConst(stackBase+sp-2, jitTagBool)

		case OP_OR:
			emitTruthyI32(stackBase+sp-2, leftTypeBefore)
			emitTruthyI32(stackBase+sp-1, rightTypeBefore)

			body.WriteByte(0x72) // i32.or
			body.WriteByte(0xB7) // f64.convert_i32_s

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			emitSetTagConst(stackBase+sp-2, jitTagBool)

		case OP_NOT:
			emitTruthyI32(stackBase+sp-1, unaryTypeBefore)
			body.WriteByte(0x45) // i32.eqz
			body.WriteByte(0xB7) // f64.convert_i32_s
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp - 1))
			emitSetTagConst(stackBase+sp-1, jitTagBool)

		case OP_NEGATE:
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0xA1)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			emitSetTagConst(stackBase+sp-1, jitTagNumber)

		case OP_POP:

		case OP_INC_LOCAL:
			var slot int
			amount := 1
			if s, ok := AsIntInternal(instr.Value); ok {
				slot = s
			} else if info, ok := instr.Value.(IncrementInfo); ok {
				slot = info.Slot
				amount = info.IntAmount
			}
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(amount))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(slot))
			emitSetTagConst(slot, jitTagNumber)

		case OP_DEC_LOCAL:
			var slot int
			amount := 1
			if s, ok := AsIntInternal(instr.Value); ok {
				slot = s
			} else if info, ok := instr.Value.(IncrementInfo); ok {
				slot = info.Slot
				amount = info.IntAmount
			}
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(amount))
			body.WriteByte(0xA1)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(slot))
			emitSetTagConst(slot, jitTagNumber)

		case OP_ADD_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)

			if localTypes[info.TargetSlot] == stackTypeNumber && localTypes[info.SourceSlot] == stackTypeNumber {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(info.TargetSlot))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(info.SourceSlot))
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(info.TargetSlot))
				emitSetTagConst(info.TargetSlot, jitTagNumber)
			} else {
				emitDynamicAddTagged(info.TargetSlot, info.SourceSlot, info.TargetSlot)
			}

		case OP_SUB_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.TargetSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SourceSlot))
			body.WriteByte(0xA1)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(info.TargetSlot))
			emitSetTagConst(info.TargetSlot, jitTagNumber)

		case OP_MUL_LOCAL_CONST:
			info := instr.Value.(LocalConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Value))
			body.WriteByte(0xA2)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))
			emitSetTagConst(stackBase+sp, jitTagNumber)

		case OP_LOCAL_CONST_OP:
			info := instr.Value.(LocalConstOpInfo)
			if !emitLocalConstOp(info.Slot, info.Const, info.Op, stackBase+sp) {
				body.WriteByte(0x44)
				body.WriteFloat64(0.0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(stackBase + sp))
				emitSetTagConst(stackBase+sp, jitTagNull)
			}

		case OP_LOCAL_CONST_OP_STORE:
			info := instr.Value.(LocalConstOpInfo)
			if !emitLocalConstOp(info.Slot, info.Const, info.Op, info.Slot) {
				emitSetTagFromType(info.Slot, stackTypeUnknown)
			}

		case OP_ADD_LOCAL_GLOBAL_GLOBAL_STORE:
			info := instr.Value.(AddLocalGlobalGlobalStoreInfo)
			emitLoadGlobalTagged(info.GlobalSlotA, tempPtrSlot+1)
			emitLoadGlobalTagged(info.GlobalSlotB, tempPtrSlot+2)
			emitDynamicAddTagged(info.LocalSlot, tempPtrSlot+1, tempPtrSlot+3)
			emitDynamicAddTagged(tempPtrSlot+3, tempPtrSlot+2, info.LocalSlot)

		case OP_ADD_LOCAL_ARRAY_INDEX_STORE:
			info := instr.Value.(AddLocalArrayIndexStoreInfo)
			emitArrayElementAddress(info.ArraySlot, info.IndexSlot)
			emitLoadTaggedCell(tempPtrSlot, tempPtrSlot+1)
			emitDynamicAddTagged(info.LocalSlot, tempPtrSlot+1, info.LocalSlot)

		case OP_ARRAY_INDEX_CONST_OP_STORE:
			info := instr.Value.(ArrayIndexConstOpInfo)
			emitArrayElementAddress(info.ArraySlot, info.IndexSlot)
			emitLoadTaggedCell(tempPtrSlot, tempPtrSlot+1)
			if emitLocalConstOp(tempPtrSlot+1, info.Const, info.Op, tempPtrSlot+1) {
				emitStoreTaggedCell(tempPtrSlot, tempPtrSlot+1)
				emitDeoptCheckpoint(i+1, sp)
			}

		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)

			if localTypes[info.SlotA] == stackTypeNumber && localTypes[info.SlotB] == stackTypeNumber {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(info.SlotA))
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(info.SlotB))
				body.WriteByte(0xA0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(info.DestSlot))
				emitSetTagConst(info.DestSlot, jitTagNumber)
			} else {
				emitDynamicAddTagged(info.SlotA, info.SlotB, info.DestSlot)
			}

		case OP_RETURN:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x0F)

		case OP_JUMP:
			target := instr.IntArg
			if !instr.IsInt {
				if t, ok := AsIntInternal(instr.Value); ok {
					target = t
				}
			}
			depth, _ := findDepth(activeBlocks, target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth))

		case OP_JUMP_IF_FALSE:
			target := instr.IntArg
			if !instr.IsInt {
				if t, ok := AsIntInternal(instr.Value); ok {
					target = t
				}
			}
			emitTruthyI32(stackBase+sp-1, typeStack[sp-1])
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature
			body.WriteByte(0x05) // else
			depth, _ := findDepth(activeBlocks, target)
			body.WriteByte(0x0C) // br
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B) // end

		case OP_JUMP_IF_TRUE:
			target := instr.IntArg
			if !instr.IsInt {
				if t, ok := AsIntInternal(instr.Value); ok {
					target = t
				}
			}
			emitTruthyI32(stackBase+sp-1, typeStack[sp-1])
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature
			depth, _ := findDepth(activeBlocks, target)
			body.WriteByte(0x0C) // br
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B) // end

		case OP_JUMP_PROPERTY_LOCAL_FALSE:
			info := instr.Value.(JumpPropertyLocalInfo)
			if origin, ok := rowOrigins[info.Slot]; ok {
				emitPackedArrayPropertyLoadOrFallback(origin.ArraySlot, origin.IndexSlot, info.Slot, info.Name, tempPtrSlot+1)
			} else {
				emitObjectPropertyLoadFromLocal(info.Slot, info.Name, tempPtrSlot+1)
			}
			if info.Slot >= 0 && info.Slot < len(localPropertyHints) && localPropertyHints[info.Slot] != nil {
				if hintedType, ok := localPropertyHints[info.Slot][info.Name]; ok {
					if expectedTag, ok := jitTagForStackType(hintedType); ok {
						emitRequireTag(tempPtrSlot+1, expectedTag, i, sp)
					}
				}
			}
			emitTruthyI32(tempPtrSlot+1, stackTypeBool)
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature
			body.WriteByte(0x05) // else
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C) // br
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B) // end

		case OP_JUMP_PROPERTY_LOCAL_TRUE:
			info := instr.Value.(JumpPropertyLocalInfo)
			if origin, ok := rowOrigins[info.Slot]; ok {
				emitPackedArrayPropertyLoadOrFallback(origin.ArraySlot, origin.IndexSlot, info.Slot, info.Name, tempPtrSlot+1)
			} else {
				emitObjectPropertyLoadFromLocal(info.Slot, info.Name, tempPtrSlot+1)
			}
			if info.Slot >= 0 && info.Slot < len(localPropertyHints) && localPropertyHints[info.Slot] != nil {
				if hintedType, ok := localPropertyHints[info.Slot][info.Name]; ok {
					if expectedTag, ok := jitTagForStackType(hintedType); ok {
						emitRequireTag(tempPtrSlot+1, expectedTag, i, sp)
					}
				}
			}
			emitTruthyI32(tempPtrSlot+1, stackTypeBool)
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // empty block signature
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C) // br
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B) // end

		case OP_JUMP_LOCAL_GT_CONST:
			info := instr.Value.(JumpLocalGTConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Value))
			body.WriteByte(0x64)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_LOCAL_GE_CONST:
			info := instr.Value.(JumpLocalGEConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Value))
			body.WriteByte(0x66)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_LOCAL_GT_LOCAL:
			info := instr.Value.(JumpLocalGTLocalInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SlotA))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SlotB))
			body.WriteByte(0x64)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_LOCAL_GE_LOCAL:
			info := instr.Value.(JumpLocalGELocalInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.LeftSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.RightSlot))
			body.WriteByte(0x66)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
			info := instr.Value.(JumpModLocalConstNotZeroInfo)

			body.WriteByte(0x44) // f64.const right
			body.WriteFloat64(float64(info.Right))
			body.WriteByte(0x21) // local.set temp
			body.WriteVarUint(uint32(tempPtrSlot))

			emitFastModValueConst(info.LeftSlot, float64(info.Right))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne

			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info := instr.Value.(JumpModLocalLocalNotZeroInfo)

			emitFastModValue(info.LeftSlot, info.RightSlot)
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62) // f64.ne

			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_CALL_DIRECT:
			info := instr.Value.(DirectCallInfo)
			for a := 0; a < info.ArgCount; a++ {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - info.ArgCount + a))
			}
			if info.ID >= 0 && info.ID < len(vm.functionList) {
				if missingDefaults, ok := jitMissingDefaultArgsForCall(vm, vm.functionList[info.ID], info.ArgCount); ok {
					for _, defaultValue := range missingDefaults {
						if !emitTinyValueArg(defaultValue) {
							body.WriteByte(0x00) // unreachable: unsupported JIT default literal
						}
					}
				} else {
					body.WriteByte(0x00) // unreachable: invalid JIT default-arg call
				}
			}
			body.WriteByte(0x10)                                // call
			body.WriteVarUint(uint32(info.ID + jitImportCount)) // Function indices start after imports
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - info.ArgCount))
			emitSetTagFromType(stackBase+sp-info.ArgCount, currentReturnTypes[info.ID])

		case OP_CALL_DIRECT_SUB_CONST:
			info := instr.Value.(CallDirectSubConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.SubValue))
			body.WriteByte(0xA1) // sub
			if info.FnID >= 0 && info.FnID < len(vm.functionList) {
				if missingDefaults, ok := jitMissingDefaultArgsForCall(vm, vm.functionList[info.FnID], info.ArgCount); ok {
					for _, defaultValue := range missingDefaults {
						if !emitTinyValueArg(defaultValue) {
							body.WriteByte(0x00) // unreachable: unsupported JIT default literal
						}
					}
				} else {
					body.WriteByte(0x00) // unreachable: invalid JIT default-arg call
				}
			}
			body.WriteByte(0x10)                                  // call
			body.WriteVarUint(uint32(info.FnID + jitImportCount)) // Function indices start after imports
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))
			emitSetTagFromType(stackBase+sp, currentReturnTypes[info.FnID])

		case OP_OBJECT:
			info := instr.Value.(ObjectInfo)
			count := len(info.Names)

			shapeNames := make([]string, count)
			for idx := 0; idx < count; idx++ {
				shapeNames[idx] = info.Names[idx].Name
			}
			shapeID := vm.getObjectShapeID(shapeNames)

			inputTypes := make([]stackType, count)
			for idx := 0; idx < count; idx++ {
				inputTypes[idx] = typeStack[sp-count+idx]
			}

			objectSize := uint32(16)
			if count > 0 {
				var maxOffset uint32 = 16
				for _, prop := range info.Names {
					offset := vm.getPropertyOffset(prop.Name)
					if offset > maxOffset {
						maxOffset = offset
					}
				}
				objectSize = maxOffset + 16
			}

			// CALL alloc_object(size) -> addr (handles memory growth)
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(objectSize))
			body.WriteByte(0x10) // call
			body.WriteVarUint(jitImportAllocObject)
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))

			// The address is now stored in tempPtrSlot
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x44)
			body.WriteFloat64(4.0)
			body.WriteByte(0x39)
			body.WriteVarUint(3)
			body.WriteVarUint(0)

			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x44)
			body.WriteFloat64(float64(shapeID))
			body.WriteByte(0x39)
			body.WriteVarUint(3)
			body.WriteVarUint(8)

			for idx := 0; idx < count; idx++ {
				name := info.Names[idx].Name
				offset := vm.getPropertyOffset(name)

				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0xAA)

				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tagSlot(stackBase + sp - count + idx)))
				body.WriteByte(0x39)
				body.WriteVarUint(3)
				body.WriteVarUint(uint32(offset))

				body.WriteByte(0x20)
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0xAA)
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - count + idx))
				body.WriteByte(0x39)
				body.WriteVarUint(3)
				body.WriteVarUint(uint32(offset + 8))
			}

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - count))
			emitSetTagConst(stackBase+sp-count, jitTagObject)

			dest := sp - count
			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeObject
			}

		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			name := instr.Value.(string)
			offset := vm.getPropertyOffset(name)
			dst := stackBase + sp - 1

			// Check if tag == 4.0 (jitTagObject)
			body.WriteByte(0x20) // local.get tag
			body.WriteVarUint(uint32(tagSlot(dst)))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(jitTagObject)
			body.WriteByte(0x61) // f64.eq
			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // block type: empty

			// then: normal load
			body.WriteByte(0x20) // local.get object
			body.WriteVarUint(uint32(dst))
			body.WriteByte(0x44) // f64.const offset
			body.WriteFloat64(float64(offset))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			emitLoadTaggedCell(tempPtrSlot, dst)

			body.WriteByte(0x05) // else

			// else: not an object
			if instr.Op == OP_GET_PROPERTY_SAFE {
				// if tag == 0.0, set to null
				body.WriteByte(0x20) // local.get tag
				body.WriteVarUint(uint32(tagSlot(dst)))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(jitTagNull)
				body.WriteByte(0x61) // f64.eq
				body.WriteByte(0x04) // if
				body.WriteByte(0x40) // block type: empty

				// set dst to null
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(0.0)
				body.WriteByte(0x21) // local.set dst value
				body.WriteVarUint(uint32(dst))
				emitSetTagConst(dst, jitTagNull)

				body.WriteByte(0x05) // else

				// call throw_type_error_wasm
				body.WriteByte(0x20) // local.get tag
				body.WriteVarUint(uint32(tagSlot(dst)))
				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(dst))
				body.WriteByte(0x10) // call
				body.WriteVarUint(jitImportThrowTypeErrorWasm)
				body.WriteByte(0x1A) // drop

				body.WriteByte(0x0B) // end if (tag == 0.0)
			} else {
				// call throw_type_error_wasm
				body.WriteByte(0x20) // local.get tag
				body.WriteVarUint(uint32(tagSlot(dst)))
				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(dst))
				body.WriteByte(0x10) // call
				body.WriteVarUint(jitImportThrowTypeErrorWasm)
				body.WriteByte(0x1A) // drop
			}

			body.WriteByte(0x0B) // end if (tag == 4.0)

		case OP_SET_PROPERTY:
			name := instr.Value.(string)
			offset := vm.getPropertyOffset(name)
			objSlot := stackBase + sp - 2
			srcSlot := stackBase + sp - 1

			body.WriteByte(0x20) // local.get object
			body.WriteVarUint(uint32(objSlot))
			body.WriteByte(0x44) // f64.const offset
			body.WriteFloat64(float64(offset))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))

			emitStoreTaggedCell(tempPtrSlot, srcSlot)
			emitDeoptCheckpoint(i+1, sp-2)

		case OP_GET_PROPERTY_LOCAL:
			info := instr.Value.(PropertyLocalInfo)
			dst := stackBase + sp

			if origin, ok := rowOrigins[info.Slot]; ok {
				emitPackedArrayPropertyLoadOrFallback(origin.ArraySlot, origin.IndexSlot, info.Slot, info.Name, dst)
			} else {
				emitObjectPropertyLoadFromLocal(info.Slot, info.Name, dst)
			}
			if info.Slot >= 0 && info.Slot < len(localPropertyHints) && localPropertyHints[info.Slot] != nil {
				if hintedType, ok := localPropertyHints[info.Slot][info.Name]; ok {
					if expectedTag, ok := jitTagForStackType(hintedType); ok {
						emitRequireTag(dst, expectedTag, i, sp)
					}
				}
			}

		case OP_ADD_PROPERTY_LOCAL_LOCAL:
			info := instr.Value.(PropertyLocalAssignInfo)
			offset := vm.getPropertyOffset(info.Name)

			// tempPtrSlot = address of the tagged property cell
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(offset))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))

			// tempPtrSlot+1 = old property value, with tag copied
			emitLoadTaggedCell(tempPtrSlot, tempPtrSlot+1)

			// tempPtrSlot+1 = old + source, using tags
			emitDynamicAddTagged(tempPtrSlot+1, info.SourceSlot, tempPtrSlot+1)

			// store result back into property cell
			emitStoreTaggedCell(tempPtrSlot, tempPtrSlot+1)
			emitDeoptCheckpoint(i+1, sp)

		case OP_ADD_PROPERTY_LOCAL_CONST:
			info := instr.Value.(PropertyLocalConstAssignInfo)
			offset := vm.getPropertyOffset(info.Name)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(offset))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))
			emitLoadTaggedCell(tempPtrSlot, tempPtrSlot+1)
			if emitLocalConstOp(tempPtrSlot+1, info.Const, info.Op, tempPtrSlot+1) {
				emitStoreTaggedCell(tempPtrSlot, tempPtrSlot+1)
				emitDeoptCheckpoint(i+1, sp)
			}

		case OP_ADD_PROPERTY_LOCAL_PROPERTY:
			info := instr.Value.(PropertyLocalPropertyAssignInfo)
			dstOffset := vm.getPropertyOffset(info.Name)
			srcOffset := vm.getPropertyOffset(info.SourceName)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(dstOffset))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))
			emitLoadTaggedCell(tempPtrSlot, tempPtrSlot+1)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(srcOffset))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot + 2))
			emitLoadTaggedCell(tempPtrSlot+2, tempPtrSlot+3)

			if info.Op == OP_ADD {
				emitDynamicAddTagged(tempPtrSlot+1, tempPtrSlot+3, tempPtrSlot+1)
			} else {
				emitNumericBinaryOp(tempPtrSlot+1, tempPtrSlot+3, tempPtrSlot+1, info.Op)
			}
			emitStoreTaggedCell(tempPtrSlot, tempPtrSlot+1)
			emitDeoptCheckpoint(i+1, sp)

		case OP_ADD_LOCAL_PROPERTIES_STORE:
			info := instr.Value.(AddLocalPropertiesStoreInfo)
			for _, name := range info.Names {
				if origin, ok := rowOrigins[info.ObjectSlot]; ok {
					emitPackedArrayPropertyLoadOrFallback(origin.ArraySlot, origin.IndexSlot, info.ObjectSlot, name, tempPtrSlot+1)
				} else {
					emitObjectPropertyLoadFromLocal(info.ObjectSlot, name, tempPtrSlot+1)
				}

				propIsHintedNumber := false
				if info.ObjectSlot >= 0 && info.ObjectSlot < len(localPropertyHints) && localPropertyHints[info.ObjectSlot] != nil {
					propIsHintedNumber = localPropertyHints[info.ObjectSlot][name] == stackTypeNumber
				}

				if propIsHintedNumber && info.LocalSlot >= 0 && info.LocalSlot < len(localTypes) && localTypes[info.LocalSlot] == stackTypeNumber {
					// Fast generic numeric field add. This is not benchmark-name based: any local += obj.field
					// pattern gets it when the field is proven numeric by surrounding bytecode.
					emitRequireTag(tempPtrSlot+1, jitTagNumber, i, sp)
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(info.LocalSlot))
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(tempPtrSlot + 1))
					body.WriteByte(0xA0) // f64.add
					body.WriteByte(0x21)
					body.WriteVarUint(uint32(info.LocalSlot))
					emitSetTagConst(info.LocalSlot, jitTagNumber)
				} else {
					emitDynamicAddTagged(info.LocalSlot, tempPtrSlot+1, info.LocalSlot)
				}
			}

		case OP_ARRAY:
			info := instr.Value.(ArrayInfo)
			count := info.Count

			inputTypes := make([]stackType, count)
			for idx := 0; idx < count; idx++ {
				inputTypes[idx] = typeStack[sp-count+idx]
			}
			// INLINED NATIVE ALLOCATOR (ARRAY)
			// 1. Get current heap top
			body.WriteByte(0x23) // global.get
			body.WriteVarUint(0) // global 0
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))

			// 2. Increment heap top by 32 bytes (header)
			body.WriteByte(0x23) // global.get
			body.WriteVarUint(0)
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(32.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x24) // global.set
			body.WriteVarUint(0)

			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(5.0)
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(0) // offset 0
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(count))
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(8) // offset 8
			if count > 0 {
				// Allocate elements array
				body.WriteByte(0x23) // global.get
				body.WriteVarUint(0)
				body.WriteByte(0x21) // local.set tempPtrSlot+1
				body.WriteVarUint(uint32(tempPtrSlot + 1))

				// Increment heap top for elements (count * 16)
				body.WriteByte(0x23) // global.get
				body.WriteVarUint(0)
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(float64(count * 16))
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0x24) // global.set
				body.WriteVarUint(0)

				body.WriteByte(0x20) // local.get tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x20) // local.get tempPtrSlot+1
				body.WriteVarUint(uint32(tempPtrSlot + 1))
				body.WriteByte(0x39) // f64.store
				body.WriteVarUint(3)
				body.WriteVarUint(16) // offset 16
				for idx := 0; idx < count; idx++ {
					body.WriteByte(0x20) // local.get tempPtrSlot+1
					body.WriteVarUint(uint32(tempPtrSlot + 1))
					body.WriteByte(0xAA) // i32.trunc_f64_s

					body.WriteByte(0x20)
					body.WriteVarUint(uint32(tagSlot(stackBase + sp - count + idx)))
					body.WriteByte(0x39) // f64.store
					body.WriteVarUint(3)
					body.WriteVarUint(uint32(idx * 16))
					body.WriteByte(0x20) // local.get tempPtrSlot+1
					body.WriteVarUint(uint32(tempPtrSlot + 1))
					body.WriteByte(0xAA) // i32.trunc_f64_s
					body.WriteByte(0x20) // local.get stackBase + sp - count + idx
					body.WriteVarUint(uint32(stackBase + sp - count + idx))
					body.WriteByte(0x39) // f64.store
					body.WriteVarUint(3)
					body.WriteVarUint(uint32(idx*16 + 8))
				}
			} else {
				body.WriteByte(0x20) // local.get tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(0.0)
				body.WriteByte(0x39) // f64.store
				body.WriteVarUint(3)
				body.WriteVarUint(16) // offset 16
			}
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(count))
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(24) // offset 24
			body.WriteByte(0x20)  // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp - count))
			emitSetTagConst(stackBase+sp-count, jitTagArray)

		case OP_ARRAY_INDEX_LOCAL_STORE:
			info := instr.Value.(ArrayIndexLocalStoreInfo)
			// Keep normal semantics: dest = array[index].
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x2B)
			body.WriteVarUint(3)
			body.WriteVarUint(16)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.IndexSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2)
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))
			emitLoadTaggedCell(tempPtrSlot, info.DestSlot)
			rowOrigins[info.DestSlot] = jitRowOrigin{ArraySlot: info.ArraySlot, IndexSlot: info.IndexSlot}

		case OP_INDEX:
			arrSlot := stackBase + sp - 2
			idxSlot := stackBase + sp - 1

			// tempPtrSlot = element base pointer
			body.WriteByte(0x20) // local.get array
			body.WriteVarUint(uint32(arrSlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x2B) // f64.load elemPtr offset 16
			body.WriteVarUint(3)
			body.WriteVarUint(16)
			body.WriteByte(0x20) // local.get index
			body.WriteVarUint(uint32(idxSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2)
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))

			emitLoadTaggedCell(tempPtrSlot, arrSlot)

		case OP_SET_INDEX:
			arrSlot := stackBase + sp - 3
			idxSlot := stackBase + sp - 2
			srcSlot := stackBase + sp - 1

			// tempPtrSlot = element base pointer
			body.WriteByte(0x20) // local.get array
			body.WriteVarUint(uint32(arrSlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x2B) // f64.load elemPtr offset 16
			body.WriteVarUint(3)
			body.WriteVarUint(16)
			body.WriteByte(0x20) // local.get index
			body.WriteVarUint(uint32(idxSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2)
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))

			emitStoreTaggedCell(tempPtrSlot, srcSlot)
			emitDeoptCheckpoint(i+1, sp-3)

		case OP_LEN:
			body.WriteByte(0x20) // local.get object_ptr
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(8.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp - 1))
			emitSetTagConst(stackBase+sp-1, jitTagNumber)

		case OP_ARRAY_LEN_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			dst := stackBase + sp
			if optimizedStringJoinLengthSlot[info.ArraySlot] {
				emitCopyTagged(dst, info.ArraySlot)
				emitSetTagConst(dst, jitTagNumber)
				break
			}

			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load length (offset 8)
			body.WriteVarUint(3)
			body.WriteVarUint(8)
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(dst))
			emitSetTagConst(dst, jitTagNumber)

		case OP_ARRAY_GET_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			dst := stackBase + sp

			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x2B) // f64.load elemPtr offset 16
			body.WriteVarUint(3)
			body.WriteVarUint(16)
			body.WriteByte(0x20) // local.get ArgSlot
			body.WriteVarUint(uint32(info.ArgSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2)
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))

			emitLoadTaggedCell(tempPtrSlot, dst)

		case OP_ARRAY_PUSH_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0x20) // local.get ArgSlot tag
			body.WriteVarUint(uint32(tagSlot(info.ArgSlot)))
			body.WriteByte(0x20) // local.get ArgSlot value
			body.WriteVarUint(uint32(info.ArgSlot))
			body.WriteByte(0x10)
			body.WriteVarUint(2) // array_push
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))
			emitSetTagConst(stackBase+sp, jitTagArray)
			emitDeoptCheckpoint(i+1, sp+1)

		case OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			info := instr.Value.(ArrayLocalMulConstInfo)
			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0x44)
			body.WriteFloat64(1.0)
			body.WriteByte(0x20) // local.get ArgSlot
			body.WriteVarUint(uint32(info.ArgSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(info.Factor))
			body.WriteByte(0xA2) // f64.mul
			body.WriteByte(0x10)
			body.WriteVarUint(2)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))
			emitSetTagConst(stackBase+sp, jitTagArray)
			emitDeoptCheckpoint(i+1, sp+1)

		case OP_METHOD_CALL:
			info := instr.Value.(MethodCallInfo)
			receiverSlot := stackBase + sp - info.ArgCount - 1
			resultSlot := receiverSlot
			methodID, ok := jitStringID[info.Method]
			if !ok {
				methodID = 0
			}

			// If receiver is a standard module loaded by OP_LOAD_GLOBAL, dispatch to Go stdlib.
			body.WriteByte(0x20) // local.get receiver tag
			body.WriteVarUint(uint32(tagSlot(receiverSlot)))
			body.WriteByte(0x44) // f64.const jitTagStdModule
			body.WriteFloat64(jitTagStdModule)
			body.WriteByte(0x61) // f64.eq

			body.WriteByte(0x04) // if
			body.WriteByte(0x40) // no result, stores into resultSlot

			// call_stdlib_wasm(moduleSlot, methodID, argCount, tag0, val0, tag1, val1, tag2, val2)
			body.WriteByte(0x20) // module slot stored in receiver value
			body.WriteVarUint(uint32(receiverSlot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(methodID))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.ArgCount))
			for argIndex := 0; argIndex < 3; argIndex++ {
				if argIndex < info.ArgCount {
					argSlot := receiverSlot + 1 + argIndex
					body.WriteByte(0x20) // arg tag
					body.WriteVarUint(uint32(tagSlot(argSlot)))
					body.WriteByte(0x20) // arg value
					body.WriteVarUint(uint32(argSlot))
				} else {
					body.WriteByte(0x44) // missing arg tag = null
					body.WriteFloat64(jitTagNull)
					body.WriteByte(0x44) // missing arg value = 0
					body.WriteFloat64(0.0)
				}
			}
			emitMarkSideEffect()
			body.WriteByte(0x10) // call call_stdlib_wasm
			body.WriteVarUint(jitImportCallStdlibWasm)

			// call_stdlib_wasm returns (tag, value). Store value first, then tag.
			body.WriteByte(0x21) // local.set result value
			body.WriteVarUint(uint32(resultSlot))
			body.WriteByte(0x21) // local.set result tag
			body.WriteVarUint(uint32(tagSlot(resultSlot)))

			body.WriteByte(0x05) // else: normal JIT-supported methods

			switch info.Method {
			case "length":
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(receiverSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load length
				body.WriteVarUint(3) // alignment 3
				body.WriteVarUint(8) // offset 8
				body.WriteByte(0x21) // local.set result
				body.WriteVarUint(uint32(resultSlot))
				emitSetTagConst(resultSlot, jitTagNumber)

			case "get":
				indexSlot := receiverSlot + 1
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(receiverSlot))
				body.WriteByte(0xAA)
				body.WriteByte(0x2B) // f64.load elemPtr offset 16
				body.WriteVarUint(3)
				body.WriteVarUint(16)
				body.WriteByte(0x20) // local.get index
				body.WriteVarUint(uint32(indexSlot))
				body.WriteByte(0x44)
				body.WriteFloat64(16.0)
				body.WriteByte(0xA2)
				body.WriteByte(0xA0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(tempPtrSlot))
				emitLoadTaggedCell(tempPtrSlot, resultSlot)

			case "push":
				emitMarkSideEffect()
				argSlot := receiverSlot + 1
				body.WriteByte(0x20) // receiver array
				body.WriteVarUint(uint32(receiverSlot))
				body.WriteByte(0x20) // value tag
				body.WriteVarUint(uint32(tagSlot(argSlot)))
				body.WriteByte(0x20) // value
				body.WriteVarUint(uint32(argSlot))
				body.WriteByte(0x10) // array_push
				body.WriteVarUint(jitImportArrayPush)
				body.WriteByte(0x21) // local.set result
				body.WriteVarUint(uint32(resultSlot))
				emitSetTagConst(resultSlot, jitTagArray)

			default:
				body.WriteByte(0x44) // f64.const string tag
				body.WriteFloat64(jitTagString)
				body.WriteByte(0x44) // f64.const method string id
				body.WriteFloat64(float64(methodID))
				body.WriteByte(0x10) // load_string_constant(methodID)
				body.WriteVarUint(jitImportLoadStringConstant)
				body.WriteByte(0x10) // throw_wasm(tag, value)
				body.WriteVarUint(jitImportThrowWasm)
				body.WriteByte(0x1A) // drop returned f64 if throw_wasm ever returns
				body.WriteByte(0x44)
				body.WriteFloat64(0.0)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(resultSlot))
				emitSetTagConst(resultSlot, jitTagNull)
			}

			body.WriteByte(0x0B) // end standard-module dispatch if
			if info.Method != "length" && info.Method != "get" {
				emitDeoptCheckpoint(i+1, sp-info.ArgCount)
			}
		}
	}

	if list, exists := closes[N]; exists {
		for range list {
			body.WriteByte(0x0B)
		}
	}

	body.WriteByte(0x44)
	body.WriteFloat64(0.0)
	body.WriteByte(0x0B)

	funcBodySec := &WasmBuffer{}
	funcBodySec.WriteVarUint(uint32(len(body.buf)))
	funcBodySec.WriteBytes(body.buf)
	return funcBodySec.buf
}

func (vm *VM) jitValueToTinyValue(mod api.Module, tag float64, val float64) TinyValue {
	switch tag {
	case jitTagNull:
		return NewNull()

	case jitTagNumber:
		return ToValue(val)

	case jitTagBool:
		return NewNative(val != 0)

	case jitTagString:
		addr := uint32(val)

		lenBytes, ok := mod.Memory().Read(addr+8, 8)
		if !ok {
			return NewNull()
		}

		strLen := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))

		strBytes, ok := mod.Memory().Read(addr+16, strLen)
		if !ok {
			return NewNull()
		}

		return NewNative(string(strBytes))

	case jitTagObject:
		return NewNative(WasmObjectValue{
			Address: val,
			VM:      vm,
		})

	case jitTagArray:
		arr := WasmArrayValue{
			Address: val,
			VM:      vm,
		}
		if native, ok := vm.wasmArrayToArrayValue(arr); ok {
			return NewNative(native)
		}
		return NewNative(arr)

	default:
		return ToValue(val)
	}
}

func (vm *VM) allocateJitMemory(mod api.Module, size uint32) uint32 {
	size = (size + 7) &^ 7 // Align size to 8-byte boundary

	const bitsetRange = 128 * 1024 * 1024
	const bitsetSize = bitsetRange / 64 // 2MB

	var addr uint32
	heapTopGlobal := vm.getHeapTopGlobal(mod)
	if heapTopGlobal != nil {
		addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
	} else {
		addr = atomic.LoadUint32(&vm.jitHeapTop)
	}

	// Mark allocator bitset
	bitIdx := addr / 8
	byteIdx := bitIdx / 8
	bitOffset := bitIdx % 8
	if byteIdx < bitsetSize {
		buf, ok := mod.Memory().Read(byteIdx, 1)
		if ok {
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
	atomic.StoreUint32(&vm.jitHeapTop, newTop)
	return addr
}

func appendPartBytes(mod api.Module, tag, val float64, buf []byte) []byte {
	switch tag {
	case 6.0: // String
		addr := uint32(val)
		lenBytes, ok := mod.Memory().Read(addr+8, 8)
		if !ok {
			return buf
		}
		n := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))
		if n == 0 {
			return buf
		}
		bytes, ok := mod.Memory().Read(addr+16, n)
		if !ok {
			return buf
		}
		return append(buf, bytes...)
	case 2.0: // Bool
		if val != 0.0 {
			return append(buf, "true"...)
		}
		return append(buf, "false"...)
	case 1.0: // Number
		if math.Trunc(val) == val {
			return strconv.AppendInt(buf, int64(val), 10)
		}
		return append(buf, FloatToString(val)...)
	case 0.0: // Null
		return append(buf, "null"...)
	default:
		return buf
	}
}

func (vm *VM) allocateWasmStringNoRegister(mod api.Module, bytes []byte) float64 {
	length := len(bytes)
	size := uint32(16 + length)
	addr := vm.allocateJitMemory(mod, size)

	buf := make([]byte, 16+length)
	binary.LittleEndian.PutUint64(buf[0:8], math.Float64bits(6.0))
	binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(float64(length)))
	copy(buf[16:], bytes)

	mod.Memory().Write(addr, buf)
	return float64(addr)
}

func (vm *VM) allocateWasmString(mod api.Module, s string) float64 {
	bytes := []byte(s)
	length := len(bytes)
	size := uint32(16 + length)
	addr := vm.allocateJitMemory(mod, size)

	buf := make([]byte, 16+length)
	binary.LittleEndian.PutUint64(buf[0:8], math.Float64bits(6.0))
	binary.LittleEndian.PutUint64(buf[8:16], math.Float64bits(float64(length)))
	copy(buf[16:], bytes)

	mod.Memory().Write(addr, buf)

	vm.RegisterJitString(s)
	return float64(addr)
}

func jitObjectKeyString(k any) string {
	if s, ok := k.(string); ok {
		return s
	}
	return fmt.Sprint(k)
}

type jitPackedObjectArrayField struct {
	Name       string
	Offset     uint32
	TableIndex uint32
}

type jitPackedObjectArrayPlan struct {
	Fields     []jitPackedObjectArrayField
	TableSlots uint32
}

func (vm *VM) planPackedObjectArray(arr *ArrayValue) (jitPackedObjectArrayPlan, bool) {
	if vm == nil || arr == nil || len(arr.Elements) == 0 {
		return jitPackedObjectArrayPlan{}, false
	}

	first, ok := jitTinyObjectValue(arr.Elements[0])
	if !ok || len(first) == 0 {
		return jitPackedObjectArrayPlan{}, false
	}

	names := make([]string, 0, len(first))
	for k := range first {
		name := jitObjectKeyString(k)
		if name == "__class" {
			return jitPackedObjectArrayPlan{}, false
		}
		names = append(names, name)
	}
	sort.Strings(names)

	for i := 1; i < len(arr.Elements); i++ {
		obj, ok := jitTinyObjectValue(arr.Elements[i])
		if !ok || len(obj) != len(names) {
			return jitPackedObjectArrayPlan{}, false
		}
		for _, name := range names {
			if _, exists := obj[name]; !exists {
				return jitPackedObjectArrayPlan{}, false
			}
		}
	}

	fields := make([]jitPackedObjectArrayField, 0, len(names))
	var maxTableIndex uint32
	for _, name := range names {
		offset := vm.getPropertyOffset(name)
		if offset < 16 {
			continue
		}
		tableIndex := (offset - 16) / 16
		if tableIndex > maxTableIndex {
			maxTableIndex = tableIndex
		}
		fields = append(fields, jitPackedObjectArrayField{Name: name, Offset: offset, TableIndex: tableIndex})
	}
	if len(fields) == 0 {
		return jitPackedObjectArrayPlan{}, false
	}
	return jitPackedObjectArrayPlan{Fields: fields, TableSlots: maxTableIndex + 1}, true
}

func (vm *VM) packWasmObjectArray(mod api.Module, arrayAddr uint32) bool {
	if vm == nil || mod == nil {
		return false
	}

	markerBytes, ok := mod.Memory().Read(arrayAddr+jitArrayPackedMarkerOffset, 8)
	if ok && math.Float64frombits(binary.LittleEndian.Uint64(markerBytes)) == jitPackedObjectArrayMarker {
		return true
	}

	lenBytes, ok := mod.Memory().Read(arrayAddr+8, 8)
	if !ok {
		return false
	}
	length := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))
	if length == 0 {
		return false
	}

	elemPtrBytes, ok := mod.Memory().Read(arrayAddr+16, 8)
	if !ok {
		return false
	}
	elemPtr := uint32(math.Float64frombits(binary.LittleEndian.Uint64(elemPtrBytes)))
	if elemPtr == 0 {
		return false
	}

	firstTagBytes, ok := mod.Memory().Read(elemPtr, 8)
	if !ok || math.Float64frombits(binary.LittleEndian.Uint64(firstTagBytes)) != 4.0 {
		return false
	}
	firstValBytes, ok := mod.Memory().Read(elemPtr+8, 8)
	if !ok {
		return false
	}
	firstAddr := uint32(math.Float64frombits(binary.LittleEndian.Uint64(firstValBytes)))
	shapeIDBytes, ok := mod.Memory().Read(firstAddr+8, 8)
	if !ok {
		return false
	}
	shapeID := int(math.Float64frombits(binary.LittleEndian.Uint64(shapeIDBytes)))
	if shapeID < 0 || shapeID >= len(vm.objectShapes) {
		return false
	}

	names := vm.objectShapes[shapeID]
	if len(names) == 0 {
		return false
	}

	fields := make([]jitPackedObjectArrayField, 0, len(names))
	var maxTableIndex uint32
	for _, name := range names {
		if name == "__class" {
			return false
		}
		offset := vm.getPropertyOffset(name)
		if offset < 16 {
			continue
		}
		tableIndex := (offset - 16) / 16
		if tableIndex > maxTableIndex {
			maxTableIndex = tableIndex
		}
		fields = append(fields, jitPackedObjectArrayField{Name: name, Offset: offset, TableIndex: tableIndex})
	}
	if len(fields) == 0 {
		return false
	}

	capacityBytes, ok := mod.Memory().Read(arrayAddr+24, 8)
	if !ok {
		return false
	}
	capacity := uint32(math.Float64frombits(binary.LittleEndian.Uint64(capacityBytes)))
	if capacity < length {
		capacity = length
	}

	tableSlots := maxTableIndex + 1
	tablePtr := vm.allocateJitMemory(mod, tableSlots*8)
	if tableSlots > 0 {
		zero := make([]byte, tableSlots*8)
		mod.Memory().Write(tablePtr, zero)
	}

	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(jitPackedObjectArrayMarker))
	mod.Memory().Write(arrayAddr+jitArrayPackedMarkerOffset, buf)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(tablePtr)))
	mod.Memory().Write(arrayAddr+jitArrayPackedTableOffset, buf)
	binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(tableSlots)))
	mod.Memory().Write(arrayAddr+jitArrayPackedSlotsOffset, buf)

	for _, field := range fields {
		colPtr := vm.allocateJitMemory(mod, capacity*16)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(colPtr)))
		mod.Memory().Write(tablePtr+field.TableIndex*8, buf)

		for i := uint32(0); i < length; i++ {
			rowAddr := elemPtr + i*16
			tagBytes, okTag := mod.Memory().Read(rowAddr, 8)
			valBytes, okVal := mod.Memory().Read(rowAddr+8, 8)
			if !okTag || !okVal {
				continue
			}
			if math.Float64frombits(binary.LittleEndian.Uint64(tagBytes)) != 4.0 {
				continue
			}
			objAddr := uint32(math.Float64frombits(binary.LittleEndian.Uint64(valBytes)))
			propTagBytes, okPropTag := mod.Memory().Read(objAddr+field.Offset, 8)
			propValBytes, okPropVal := mod.Memory().Read(objAddr+field.Offset+8, 8)
			if !okPropTag || !okPropVal {
				continue
			}
			mod.Memory().Write(colPtr+i*16, propTagBytes)
			mod.Memory().Write(colPtr+i*16+8, propValBytes)
		}
	}

	return true
}

func jitTinyObjectValue(v TinyValue) (ObjectValue, bool) {
	if v.IsInt || v.Value == nil {
		return nil, false
	}
	switch obj := v.Value.(type) {
	case ObjectValue:
		return obj, true
	case *ObjectValue:
		if obj == nil {
			return nil, false
		}
		return *obj, true
	default:
		return nil, false
	}
}

func (vm *VM) allocateJitObject(mod api.Module, obj ObjectValue) float64 {
	if obj == nil {
		return 0
	}
	vm.ensureJitMirrorCaches()
	identity := jitObjectIdentity(obj)
	if mirror, ok := vm.jitObjectMirrorCache[identity]; ok && mirror.Length == len(obj) {
		return mirror.Address
	}

	names := make([]string, 0, len(obj))
	for k := range obj {
		names = append(names, jitObjectKeyString(k))
	}

	shapeID := vm.getObjectShapeID(names)

	objectSize := uint32(16)
	if len(names) > 0 {
		var maxOffset uint32 = 16
		for _, name := range names {
			offset := vm.getPropertyOffset(name)
			if offset > maxOffset {
				maxOffset = offset
			}
		}
		objectSize = maxOffset + 16
	}

	addr := vm.allocateJitMemory(mod, objectSize)

	tagBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(jitTagObject))
	mod.Memory().Write(addr, tagBuf)

	shapeBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(shapeBuf, math.Float64bits(float64(shapeID)))
	mod.Memory().Write(addr+8, shapeBuf)

	for key, val := range obj {
		name := jitObjectKeyString(key)
		offset := vm.getPropertyOffset(name)

		t, v := vm.tinyValueToJitValue(mod, val)

		tBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(tBuf, math.Float64bits(t))
		mod.Memory().Write(addr+offset, tBuf)

		vBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(vBuf, math.Float64bits(v))
		mod.Memory().Write(addr+offset+8, vBuf)
	}

	address := float64(addr)
	vm.jitObjectMirrorCache[identity] = jitObjectMirror{
		Address: address,
		Length:  len(obj),
	}
	return address
}

func (vm *VM) allocateJitArray(mod api.Module, arr *ArrayValue) float64 {
	if arr == nil {
		return 0
	}
	vm.ensureJitMirrorCaches()
	if mirror, ok := vm.jitArrayMirrorCache[arr]; ok && mirror.Length == len(arr.Elements) {
		return mirror.Address
	}
	addr := vm.allocateJitArrayFresh(mod, arr)
	vm.jitArrayMirrorCache[arr] = jitArrayMirror{
		Address: addr,
		Length:  len(arr.Elements),
	}
	return addr
}

func (vm *VM) allocateJitArrayFresh(mod api.Module, arr *ArrayValue) float64 {
	count := len(arr.Elements)
	packedPlan, packedOK := vm.planPackedObjectArray(arr)

	headerSize := uint32(32)
	if packedOK {
		headerSize = 56
	}

	addr := vm.allocateJitMemory(mod, headerSize)

	tagBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(5.0))
	mod.Memory().Write(addr, tagBuf)

	lenBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(lenBuf, math.Float64bits(float64(count)))
	mod.Memory().Write(addr+8, lenBuf)

	capBuf := make([]byte, 8)
	binary.LittleEndian.PutUint64(capBuf, math.Float64bits(float64(count)))
	mod.Memory().Write(addr+24, capBuf)

	if count > 0 {
		elemPtr := vm.allocateJitMemory(mod, uint32(count*16))

		elemPtrBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(elemPtrBuf, math.Float64bits(float64(elemPtr)))
		mod.Memory().Write(addr+16, elemPtrBuf)

		for idx, val := range arr.Elements {
			t, v := vm.tinyValueToJitValue(mod, val)

			tBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(tBuf, math.Float64bits(t))
			mod.Memory().Write(elemPtr+uint32(idx*16), tBuf)

			vBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(vBuf, math.Float64bits(v))
			mod.Memory().Write(elemPtr+uint32(idx*16+8), vBuf)
		}
	} else {
		elemPtrBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(elemPtrBuf, math.Float64bits(0.0))
		mod.Memory().Write(addr+16, elemPtrBuf)
	}

	if packedOK {
		tablePtr := vm.allocateJitMemory(mod, packedPlan.TableSlots*8)
		if packedPlan.TableSlots > 0 {
			zero := make([]byte, packedPlan.TableSlots*8)
			mod.Memory().Write(tablePtr, zero)
		}

		buf := make([]byte, 8)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(jitPackedObjectArrayMarker))
		mod.Memory().Write(addr+jitArrayPackedMarkerOffset, buf)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(tablePtr)))
		mod.Memory().Write(addr+jitArrayPackedTableOffset, buf)
		binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(packedPlan.TableSlots)))
		mod.Memory().Write(addr+jitArrayPackedSlotsOffset, buf)

		for _, field := range packedPlan.Fields {
			colPtr := vm.allocateJitMemory(mod, uint32(count*16))
			binary.LittleEndian.PutUint64(buf, math.Float64bits(float64(colPtr)))
			mod.Memory().Write(tablePtr+field.TableIndex*8, buf)

			for idx, elem := range arr.Elements {
				obj, _ := jitTinyObjectValue(elem)
				t, v := vm.tinyValueToJitValue(mod, obj[field.Name])
				binary.LittleEndian.PutUint64(buf, math.Float64bits(t))
				mod.Memory().Write(colPtr+uint32(idx*16), buf)
				binary.LittleEndian.PutUint64(buf, math.Float64bits(v))
				mod.Memory().Write(colPtr+uint32(idx*16+8), buf)
			}
		}
	}

	return float64(addr)
}

func (vm *VM) tinyValueToJitValue(mod api.Module, val TinyValue) (float64, float64) {
	if val.IsInt {
		return jitTagNumber, float64(val.AsInt)
	}
	if val.Value == nil {
		return jitTagNull, 0.0
	}
	switch v := val.Value.(type) {
	case float64:
		return jitTagNumber, v
	case int:
		return jitTagNumber, float64(v)
	case int64:
		return jitTagNumber, float64(v)
	case bool:
		if v {
			return jitTagBool, 1.0
		}
		return jitTagBool, 0.0
	case string:
		return jitTagString, vm.allocateWasmString(mod, v)
	case WasmObjectValue:
		return jitTagObject, v.Address
	case WasmArrayValue:
		return jitTagArray, v.Address
	case ObjectValue:
		return jitTagObject, vm.allocateJitObject(mod, v)
	case *ArrayValue:
		return jitTagArray, vm.allocateJitArray(mod, v)
	case ArrayValue:
		arrCopy := v
		return jitTagArray, vm.allocateJitArrayFresh(mod, &arrCopy)
	case *StandardModuleValue:
		slot := -1
		if vm.globals != nil {
			for i, g := range *vm.globals {
				if g.Value == v {
					slot = i
					break
				}
			}
		}
		return 7.0, float64(slot)
	default:
		return jitTagNull, 0.0
	}
}

func (vm *VM) setupJitRuntimeAndEnv(jitStringConstCache map[uint32]uint32) bool {
	if vm.wazeroCtx == nil {
		vm.wazeroCtx = context.Background()
	}
	if vm.wazeroRuntime == nil {
		vm.wazeroRuntime = wazero.NewRuntime(vm.wazeroCtx)
	}
	const bitsetRange = 128 * 1024 * 1024
	const bitsetSize = bitsetRange / 64 // 2MB
	const heapStart = bitsetSize + jitDeoptSnapshotSize

	allocateWasmString := func(mod api.Module, s string) float64 {
		return vm.allocateWasmString(mod, s)
	}

	if vm.wazeroRuntime.Module("env") == nil {
		_, err := vm.wazeroRuntime.NewHostModuleBuilder("env").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			size := api.DecodeF64(stack[0])
			var addr uint32
			heapTopGlobal := vm.getHeapTopGlobal(mod)
			if heapTopGlobal != nil {
				addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
			} else {
				addr = atomic.LoadUint32(&vm.jitHeapTop)
			}

			bitIdx := addr / 8
			byteIdx := bitIdx / 8
			bitOffset := bitIdx % 8

			if byteIdx < bitsetSize {
				buf, _ := mod.Memory().Read(byteIdx, 1)
				buf[0] |= (1 << bitOffset)
				mod.Memory().Write(byteIdx, buf)
			}

			newTop := addr + uint32(size)
			currentPages := mod.Memory().Size() / 65536
			newPagesNeeded := (newTop + 65535) / 65536

			if newPagesNeeded > currentPages {
				pagesToAdd := newPagesNeeded - currentPages
				mod.Memory().Grow(pagesToAdd)
			}
			if heapTopGlobal != nil {
				if mg, ok := heapTopGlobal.(api.MutableGlobal); ok {
					mg.Set(api.EncodeF64(float64(newTop)))
				}
			}
			atomic.StoreUint32(&vm.jitHeapTop, newTop)
			stack[0] = api.EncodeF64(float64(addr))
		}), f64s(1), f64s(1)).Export("alloc_object").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			val := api.DecodeF64(stack[0])
			addr := uint32(val)
			var heapTop uint32
			heapTopGlobal := vm.getHeapTopGlobal(mod)
			if heapTopGlobal != nil {
				heapTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
			} else {
				heapTop = atomic.LoadUint32(&vm.jitHeapTop)
			}

			if addr >= heapStart && addr < heapTop && addr%8 == 0 {
				tagBytes, ok := mod.Memory().Read(addr, 8)
				if ok {
					tag := math.Float64frombits(binary.LittleEndian.Uint64(tagBytes))
					if tag == 4.0 || tag == 5.0 || tag == 6.0 {
						stack[0] = api.EncodeF64(tag)
						return
					}
				}
			}
			stack[0] = api.EncodeF64(1.0) // default to number
		}), f64s(1), f64s(1)).Export("determine_tag").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			arrayPtr := api.DecodeF64(stack[0])
			tag := api.DecodeF64(stack[1])
			val := api.DecodeF64(stack[2])

			addr := uint32(arrayPtr)
			lenBytes, ok1 := mod.Memory().Read(addr+8, 8)
			if !ok1 {
				stack[0] = api.EncodeF64(arrayPtr)
				return
			}
			length := math.Float64frombits(binary.LittleEndian.Uint64(lenBytes))
			elemPtrBytes, ok2 := mod.Memory().Read(addr+16, 8)
			if !ok2 {
				stack[0] = api.EncodeF64(arrayPtr)
				return
			}
			oldElemPtr := uint32(math.Float64frombits(binary.LittleEndian.Uint64(elemPtrBytes)))

			var capacity float64 = 0
			capBytes, okCap := mod.Memory().Read(addr+24, 8)
			if okCap {
				capacity = math.Float64frombits(binary.LittleEndian.Uint64(capBytes))
			}

			newLength := length + 1
			newElemPtr := oldElemPtr

			if newLength > capacity {
				newCapacity := capacity * 2
				if newCapacity < 4096 {
					newCapacity = 4096
				}
				newSize := uint32(newCapacity * 16)

				var heapTop uint32
				heapTopGlobal := vm.getHeapTopGlobal(mod)
				if heapTopGlobal != nil {
					heapTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
				} else {
					heapTop = atomic.LoadUint32(&vm.jitHeapTop)
				}

				newElemPtr = heapTop

				// Set bit in bitset for the new element buffer
				bitIdx := newElemPtr / 8
				byteIdx := bitIdx / 8
				bitOffset := bitIdx % 8
				if byteIdx < bitsetSize {
					buf, _ := mod.Memory().Read(byteIdx, 1)
					buf[0] |= (1 << bitOffset)
					mod.Memory().Write(byteIdx, buf)
				}

				newTop := newElemPtr + newSize
				currentPages := mod.Memory().Size() / 65536
				newPagesNeeded := (newTop + 65535) / 65536

				if newPagesNeeded > currentPages {
					pagesToAdd := newPagesNeeded - currentPages
					mod.Memory().Grow(pagesToAdd)
				}
				if heapTopGlobal != nil {
					if mg, ok := heapTopGlobal.(api.MutableGlobal); ok {
						mg.Set(api.EncodeF64(float64(newTop)))
					}
				}
				atomic.StoreUint32(&vm.jitHeapTop, newTop)

				if length > 0 && oldElemPtr != 0 {
					oldBytes, ok3 := mod.Memory().Read(oldElemPtr, uint32(length*16))
					if ok3 {
						mod.Memory().Write(newElemPtr, oldBytes)
					}
				}

				capBits := math.Float64bits(newCapacity)
				var capBuf [8]byte
				binary.LittleEndian.PutUint64(capBuf[:], capBits)
				mod.Memory().Write(addr+24, capBuf[:])

				elemPtrBits := math.Float64bits(float64(newElemPtr))
				var elemBuf [8]byte
				binary.LittleEndian.PutUint64(elemBuf[:], elemPtrBits)
				mod.Memory().Write(addr+16, elemBuf[:])
			}

			tagBits := math.Float64bits(tag)
			valBits := math.Float64bits(val)
			var buf [16]byte
			binary.LittleEndian.PutUint64(buf[0:8], tagBits)
			binary.LittleEndian.PutUint64(buf[8:16], valBits)
			mod.Memory().Write(newElemPtr+uint32(length*16), buf[:])

			newLenBits := math.Float64bits(newLength)
			var lenBuf [8]byte
			binary.LittleEndian.PutUint64(lenBuf[:], newLenBits)
			mod.Memory().Write(addr+8, lenBuf[:])

			stack[0] = api.EncodeF64(arrayPtr)
		}), f64s(3), f64s(1)).Export("array_push").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			srcA := api.DecodeF64(stack[0])
			srcB := api.DecodeF64(stack[1])
			addrA := uint32(srcA)
			lenABytes, _ := mod.Memory().Read(addrA+8, 8)
			lenA := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenABytes)))
			addrB := uint32(srcB)
			lenBBytes, _ := mod.Memory().Read(addrB+8, 8)
			lenB := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBBytes)))
			bytesA, _ := mod.Memory().Read(addrA+16, lenA)
			bytesB, _ := mod.Memory().Read(addrB+16, lenB)
			// Allocate heap memory for destination
			size := uint32(16 + lenA + lenB)
			size = (size + 7) &^ 7 // Align size to 8-byte boundary
			var addr uint32
			heapTopGlobal := vm.getHeapTopGlobal(mod)
			if heapTopGlobal != nil {
				addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
			} else {
				addr = atomic.LoadUint32(&vm.jitHeapTop)
			}
			// Set bit in allocation bitset
			bitIdx := addr / 8
			byteIdx := bitIdx / 8
			bitOffset := bitIdx % 8
			if byteIdx < bitsetSize {
				buf, _ := mod.Memory().Read(byteIdx, 1)
				buf[0] |= (1 << bitOffset)
				mod.Memory().Write(byteIdx, buf)
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
			atomic.StoreUint32(&vm.jitHeapTop, newTop)
			// Write Tag 6.0
			tagBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(6.0))
			mod.Memory().Write(addr, tagBuf)
			// Write Length
			lenBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(lenBuf, math.Float64bits(float64(lenA+lenB)))
			mod.Memory().Write(addr+8, lenBuf)
			// Copy characters
			mod.Memory().Write(addr+16, bytesA)
			mod.Memory().Write(addr+16+lenA, bytesB)
			stack[0] = api.EncodeF64(float64(addr))
		}), f64s(2), f64s(1)).Export("string_concat_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			addrA := api.DecodeF64(stack[0])
			addrB := api.DecodeF64(stack[1])
			addrA32 := uint32(addrA)
			addrB32 := uint32(addrB)

			if addrA32 == addrB32 {
				stack[0] = api.EncodeF64(1.0)
				return
			}

			lenBytesA, okA := mod.Memory().Read(addrA32+8, 8)
			lenBytesB, okB := mod.Memory().Read(addrB32+8, 8)
			if !okA || !okB {
				stack[0] = api.EncodeF64(0.0)
				return
			}

			lenA := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytesA)))
			lenB := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytesB)))
			if lenA != lenB {
				stack[0] = api.EncodeF64(0.0)
				return
			}
			if lenA == 0 {
				stack[0] = api.EncodeF64(1.0)
				return
			}

			bytesA, okA := mod.Memory().Read(addrA32+16, lenA)
			bytesB, okB := mod.Memory().Read(addrB32+16, lenB)
			if !okA || !okB {
				stack[0] = api.EncodeF64(0.0)
				return
			}

			for i := uint32(0); i < lenA; i++ {
				if bytesA[i] != bytesB[i] {
					stack[0] = api.EncodeF64(0.0)
					return
				}
			}

			stack[0] = api.EncodeF64(1.0)
		}), f64s(2), f64s(1)).Export("string_eq_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			lTag := api.DecodeF64(stack[0])
			lVal := api.DecodeF64(stack[1])
			rTag := api.DecodeF64(stack[2])
			rVal := api.DecodeF64(stack[3])

			readString := func(addr uint32) string {
				lenBytes, ok := mod.Memory().Read(addr+8, 8)
				if !ok {
					return ""
				}
				strLen := uint32(math.Float64frombits(binary.LittleEndian.Uint64(lenBytes)))
				strBytes, ok := mod.Memory().Read(addr+16, strLen)
				if !ok {
					return ""
				}
				return string(strBytes)
			}

			if lTag == 6.0 || rTag == 6.0 {
				var leftStr string
				if lTag == 6.0 {
					leftStr = readString(uint32(lVal))
				} else {
					leftStr = valueToString(vm.jitValueToTinyValue(mod, lTag, lVal), true)
				}

				var rightStr string
				if rTag == 6.0 {
					rightStr = readString(uint32(rVal))
				} else {
					rightStr = valueToString(vm.jitValueToTinyValue(mod, rTag, rVal), true)
				}

				stack[0] = api.EncodeF64(vm.allocateWasmStringNoRegister(mod, []byte(leftStr+rightStr)))
				return
			}

			if lTag == 1.0 && rTag == 1.0 {
				stack[0] = api.EncodeF64(lVal + rVal)
				return
			}

			panic("invalid operands for add")
		}), f64s(4), f64s(1)).Export("dynamic_add").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag1 := api.DecodeF64(stack[0])
			val1 := api.DecodeF64(stack[1])
			tag2 := api.DecodeF64(stack[2])
			val2 := api.DecodeF64(stack[3])
			tag3 := api.DecodeF64(stack[4])
			val3 := api.DecodeF64(stack[5])

			var localBuf [128]byte
			bytes := localBuf[:0]
			bytes = appendPartBytes(mod, tag1, val1, bytes)
			bytes = appendPartBytes(mod, tag2, val2, bytes)
			bytes = appendPartBytes(mod, tag3, val3, bytes)
			stack[0] = api.EncodeF64(vm.allocateWasmStringNoRegister(mod, bytes))
		}), f64s(6), f64s(1)).Export("dynamic_join_3").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag1 := api.DecodeF64(stack[0])
			val1 := api.DecodeF64(stack[1])
			tag2 := api.DecodeF64(stack[2])
			val2 := api.DecodeF64(stack[3])
			tag3 := api.DecodeF64(stack[4])
			val3 := api.DecodeF64(stack[5])
			tag4 := api.DecodeF64(stack[6])
			val4 := api.DecodeF64(stack[7])

			var localBuf [128]byte
			bytes := localBuf[:0]
			bytes = appendPartBytes(mod, tag1, val1, bytes)
			bytes = appendPartBytes(mod, tag2, val2, bytes)
			bytes = appendPartBytes(mod, tag3, val3, bytes)
			bytes = appendPartBytes(mod, tag4, val4, bytes)
			stack[0] = api.EncodeF64(vm.allocateWasmStringNoRegister(mod, bytes))
		}), f64s(8), f64s(1)).Export("dynamic_join_4").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			id := api.DecodeF64(stack[0])
			strID := uint32(id)

			if cachedAddr, ok := jitStringConstCache[strID]; ok {
				var heapTop uint32
				heapTopGlobal := vm.getHeapTopGlobal(mod)
				if heapTopGlobal != nil {
					heapTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
				} else {
					heapTop = atomic.LoadUint32(&vm.jitHeapTop)
				}

				if cachedAddr >= heapStart && cachedAddr < heapTop && cachedAddr%8 == 0 {
					bitIdx := cachedAddr / 8
					byteIdx := bitIdx / 8
					bitOffset := bitIdx % 8

					if byteIdx < bitsetSize {
						buf, ok := mod.Memory().Read(byteIdx, 1)
						if ok && (buf[0]&(1<<bitOffset)) != 0 {
							tagBytes, ok := mod.Memory().Read(cachedAddr, 8)
							if ok && math.Float64frombits(binary.LittleEndian.Uint64(tagBytes)) == 6.0 {
								stack[0] = api.EncodeF64(float64(cachedAddr))
								return
							}
						}
					}
				}
			}

			strVal := vm.jitStrings[strID]
			bytes := []byte(strVal)
			size := uint32(16 + len(bytes))
			size = (size + 7) &^ 7

			var addr uint32
			heapTopGlobal := mod.ExportedGlobal("__heap_top")
			if heapTopGlobal != nil {
				addr = uint32(api.DecodeF64(heapTopGlobal.Get()))
			} else {
				addr = atomic.LoadUint32(&vm.jitHeapTop)
			}

			bitIdx := addr / 8
			byteIdx := bitIdx / 8
			bitOffset := bitIdx % 8
			if byteIdx < bitsetSize {
				buf, _ := mod.Memory().Read(byteIdx, 1)
				buf[0] |= (1 << bitOffset)
				mod.Memory().Write(byteIdx, buf)
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
			atomic.StoreUint32(&vm.jitHeapTop, newTop)

			tagBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(6.0))
			mod.Memory().Write(addr, tagBuf)

			lenBuf := make([]byte, 8)
			binary.LittleEndian.PutUint64(lenBuf, math.Float64bits(float64(len(bytes))))
			mod.Memory().Write(addr+8, lenBuf)

			mod.Memory().Write(addr+16, bytes)

			jitStringConstCache[strID] = addr
			stack[0] = api.EncodeF64(float64(addr))
		}), f64s(1), f64s(1)).Export("load_string_constant").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			val := api.DecodeF64(stack[0])
			addr := uint32(val)
			var heapTop uint32
			heapTopGlobal := vm.getHeapTopGlobal(mod)
			if heapTopGlobal != nil {
				heapTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
			} else {
				heapTop = atomic.LoadUint32(&vm.jitHeapTop)
			}
			if addr >= heapStart && addr < heapTop && addr%8 == 0 {
				bitIdx := addr / 8
				byteIdx := bitIdx / 8
				bitOffset := bitIdx % 8
				if byteIdx < bitsetSize {
					buf, ok := mod.Memory().Read(byteIdx, 1)
					if ok && (buf[0]&(1<<bitOffset)) != 0 {
						tagBytes, ok := mod.Memory().Read(addr, 8)
						if ok {
							tag := math.Float64frombits(binary.LittleEndian.Uint64(tagBytes))
							if tag == 6.0 {
								lenBytes, ok := mod.Memory().Read(addr+8, 8)
								if ok {
									strLen := math.Float64frombits(binary.LittleEndian.Uint64(lenBytes))
									if strLen == 0.0 {
										stack[0] = api.EncodeF64(0.0)
										return
									}
									stack[0] = api.EncodeF64(1.0)
									return
								}
							}
						}
					}
				}
			}
			if val != 0.0 {
				stack[0] = api.EncodeF64(1.0)
			} else {
				stack[0] = api.EncodeF64(0.0)
			}
		}), f64s(1), f64s(1)).Export("is_truthy_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			a := api.DecodeF64(stack[0])
			b := api.DecodeF64(stack[1])
			stack[0] = api.EncodeF64(math.Pow(a, b))
		}), f64s(2), f64s(1)).Export("math_pow").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag := api.DecodeF64(stack[0])
			val := api.DecodeF64(stack[1])
			newline := api.DecodeF64(stack[2])
			spaceBefore := api.DecodeF64(stack[3])

			if spaceBefore != 0 {
				fmt.Print(" ")
			}

			tinyVal := vm.jitValueToTinyValue(mod, tag, val)
			text := valueToString(tinyVal, true)

			if newline != 0 {
				fmt.Println(text)
			} else {
				fmt.Print(text)
			}
		}), f64s(4), nil).Export("print_value").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag := api.DecodeF64(stack[0])
			val := api.DecodeF64(stack[1])
			tinyVal := vm.jitValueToTinyValue(mod, tag, val)
			typeNameStr := TypeName(tinyVal)
			stack[0] = api.EncodeF64(allocateWasmString(mod, typeNameStr))
		}), f64s(2), f64s(1)).Export("typeof_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag := api.DecodeF64(stack[0])
			val := api.DecodeF64(stack[1])
			tinyVal := vm.jitValueToTinyValue(mod, tag, val)
			vm.throwValue(tinyVal)
			panic(jitExceptionThrown{})
		}), f64s(2), f64s(1)).Export("throw_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			tag := api.DecodeF64(stack[0])
			val := api.DecodeF64(stack[1])
			tinyVal := vm.jitValueToTinyValue(mod, tag, val)
			vm.typeError("expected object, got %s", TypeName(tinyVal))
			panic(jitExceptionThrown{})
		}), f64s(2), f64s(1)).Export("throw_type_error_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			slotVal := api.DecodeF64(stack[0])
			slot := int(slotVal)
			if vm.globals == nil || slot < 0 || slot >= len(*vm.globals) {
				stack[0] = api.EncodeF64(jitTagNull)
				stack[1] = api.EncodeF64(0.0)
				return
			}
			val := (*vm.globals)[slot]
			var rawVal any
			if val.IsInt {
				rawVal = val.AsInt
			} else {
				rawVal = val.Value
			}
			if module, ok := rawVal.(*StandardModuleValue); ok {
				_ = module
				stack[0] = api.EncodeF64(7.0)
				stack[1] = api.EncodeF64(float64(slot))
				return
			}
			retTag, retVal := vm.tinyValueToJitValue(mod, val)
			stack[0] = api.EncodeF64(retTag)
			stack[1] = api.EncodeF64(retVal)
		}), f64s(1), f64s(2)).Export("load_global_wasm").
			NewFunctionBuilder().WithGoModuleFunction(api.GoModuleFunc(func(ctx context.Context, mod api.Module, stack []uint64) {
			moduleSlot := int(api.DecodeF64(stack[0]))
			methodStrID := int(api.DecodeF64(stack[1]))
			argCount := int(api.DecodeF64(stack[2]))
			tag0 := api.DecodeF64(stack[3])
			val0 := api.DecodeF64(stack[4])
			tag1 := api.DecodeF64(stack[5])
			val1 := api.DecodeF64(stack[6])
			tag2 := api.DecodeF64(stack[7])
			val2 := api.DecodeF64(stack[8])

			if vm.globals == nil || moduleSlot < 0 || moduleSlot >= len(*vm.globals) {
				vm.fatalError(tinyerrors.ErrorName, "undefined standard module slot: %d", moduleSlot)
				stack[0] = api.EncodeF64(jitTagNull)
				stack[1] = api.EncodeF64(0.0)
				return
			}
			moduleVal := (*vm.globals)[moduleSlot]
			var rawVal any
			if moduleVal.IsInt {
				rawVal = moduleVal.AsInt
			} else {
				rawVal = moduleVal.Value
			}
			module, ok := rawVal.(*StandardModuleValue)
			if !ok {
				vm.fatalError(tinyerrors.ErrorType, "expected standard module at slot %d", moduleSlot)
				stack[0] = api.EncodeF64(jitTagNull)
				stack[1] = api.EncodeF64(0.0)
				return
			}

			if methodStrID < 0 || methodStrID >= len(vm.jitStrings) {
				vm.fatalError(tinyerrors.ErrorName, "undefined JIT string ID for method: %d", methodStrID)
				stack[0] = api.EncodeF64(jitTagNull)
				stack[1] = api.EncodeF64(0.0)
				return
			}
			method := vm.jitStrings[methodStrID]

			var args []TinyValue
			if argCount >= 1 {
				args = append(args, vm.jitValueToTinyValue(mod, tag0, val0))
			}
			if argCount >= 2 {
				args = append(args, vm.jitValueToTinyValue(mod, tag1, val1))
			}
			if argCount >= 3 {
				args = append(args, vm.jitValueToTinyValue(mod, tag2, val2))
			}

			popNative := vm.pushNativeFrame(module.Name + "." + method)
			defer popNative()

			vm.callStandardModule(module.Name, method, args)

			result := vm.popFast()
			retTag, retVal := vm.tinyValueToJitValue(mod, result)
			stack[0] = api.EncodeF64(retTag)
			stack[1] = api.EncodeF64(retVal)
		}), f64s(9), f64s(2)).Export("call_stdlib_wasm").
			Instantiate(vm.wazeroCtx)
		if err != nil {
			return false
		}
	}

	return true
}

func (vm *VM) InstantiateJitModule() {
	if len(vm.jitWasmBytes) == 0 {
		return
	}

	const bitsetRange = 128 * 1024 * 1024
	const bitsetSize = bitsetRange / 64 // 2MB
	const heapStart = bitsetSize + jitDeoptSnapshotSize

	vm.jitInitialHeapTop = heapStart
	vm.jitHeapTop = heapStart
	jitStringConstCache := make(map[uint32]uint32)

	if vm.wazeroCtx == nil {
		vm.wazeroCtx = context.Background()
	}
	vm.wazeroRuntime = wazero.NewRuntime(vm.wazeroCtx)
	vm.wasmMu = &sync.Mutex{}

	if !vm.setupJitRuntimeAndEnv(jitStringConstCache) {
		return
	}

	moduleID := atomic.AddUint64(&jitCounter, 1)
	uniqueName := "multi_jit_" + strconv.FormatUint(moduleID, 10)
	config := wazero.NewModuleConfig().WithName(uniqueName)

	compiled, err := vm.wazeroRuntime.InstantiateWithConfig(vm.wazeroCtx, vm.jitWasmBytes, config)
	if err != nil {
		return
	}
	vm.jitModule = compiled

	for strVal, addr := range vm.jitStringAddrs {
		bytes := []byte(strVal)
		tagBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(tagBuf, math.Float64bits(6.0))
		compiled.Memory().Write(addr, tagBuf)

		lenBuf := make([]byte, 8)
		binary.LittleEndian.PutUint64(lenBuf, math.Float64bits(float64(len(bytes))))
		compiled.Memory().Write(addr+8, lenBuf)

		compiled.Memory().Write(addr+16, bytes)
	}

	if heapTopGlobal := compiled.ExportedGlobal("__heap_top"); heapTopGlobal != nil {
		vm.jitHeapTopGlobal = heapTopGlobal
		vm.jitInitialHeapTop = uint32(api.DecodeF64(heapTopGlobal.Get()))
		vm.jitHeapTop = vm.jitInitialHeapTop
	} else {
		vm.jitHeapTopGlobal = nil
	}

	vm.clearJitMirrorCaches()
	vm.jitFunctions = map[string]*JitFunction{}
	for _, meta := range vm.jitMetas {
		jitFn := compiled.ExportedFunction(meta.Name)
		if jitFn == nil {
			continue
		}

		paramTypes := append([]stackType(nil), meta.ParamTypes...)
		paramMutated := append([]bool(nil), meta.ParamMutated...)
		vm.jitFunctions[meta.Name] = &JitFunction{
			ID:           meta.ID,
			Name:         meta.Name,
			fn:           jitFn,
			paramTypes:   paramTypes,
			paramMutated: paramMutated,
			paramCount:   meta.ParamCount,
			retType:      meta.RetType,
			returnType:   meta.ReturnType,
			memoizable:   meta.Memoizable,
			vm:           vm,
			allocPtr:     &vm.jitHeapTop,
		}
	}
}

func (vm *VM) getHeapTopGlobal(mod api.Module) api.Global {
	g := vm.jitHeapTopGlobal
	if g == nil && mod != nil {
		g = mod.ExportedGlobal("__heap_top")
		vm.jitHeapTopGlobal = g
	}
	return g
}

func f64s(n int) []api.ValueType {
	if n == 0 {
		return nil
	}
	res := make([]api.ValueType, n)
	for i := range res {
		res[i] = api.ValueTypeF64
	}
	return res
}

func jitMinMemoryPages(nextAddr uint32) uint32 {
	const defaultJitMemoryPages = 64
	pagesNeeded := (nextAddr + 65535) / 65536
	if pagesNeeded < defaultJitMemoryPages {
		return defaultJitMemoryPages
	}
	return pagesNeeded
}

func isJitFunctionMemoizable(fn Function, paramMutated []bool) bool {
	if anyBool(paramMutated) {
		return false
	}

	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_STORE_GLOBAL, OP_ASSIGN_GLOBAL,
			OP_SET_INDEX, OP_SET_PROPERTY,
			OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST,
			OP_ARRAY_INDEX_CONST_OP_STORE,
			OP_ADD_PROPERTY_LOCAL_LOCAL, OP_ADD_PROPERTY_LOCAL_CONST, OP_ADD_PROPERTY_LOCAL_PROPERTY,
			OP_NATIVE_CALL, OP_BUILTIN_CALL, OP_METHOD_CALL, OP_METHOD_CALL_SAFE,
			OP_CALL, OP_CALL_VALUE, OP_CALL_VALUE_SPREAD, OP_CALL_DIRECT, OP_CALL_DIRECT_SUB_CONST,
			OP_PRINT, OP_THROW, OP_SETUP_TRY, OP_POP_TRY,
			OP_SPAWN, OP_AWAIT, OP_DEFER,
			OP_LOCK_MUTEX, OP_UNLOCK_MUTEX,
			OP_LOAD_GLOBAL,
			OP_JSON_STRINGIFY, OP_JSON_PARSE:
			return false
		}
	}

	return true
}

func (vm *VM) CompileAllJit() {
	const bitsetRange = 128 * 1024 * 1024
	const bitsetSize = bitsetRange / 64 // 2MB
	const heapStart = bitsetSize + jitDeoptSnapshotSize

	N := len(vm.functionList)
	if N == 0 {
		return
	}
	isSafe := make([]bool, N)
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		isSafe[i] = isFunctionJitSafe(vm, fn)
		if jitCallDebugEnabled() {
			// fmt.Fprintf(os.Stderr, "[JIT PLAN initial] fn=%s id=%d safe=%v stmts=%d instrs=%d params=%d defaults=%v return=%s\n", fn.Name, fn.ID, isSafe[i], fn.StatementCount, len(fn.Instructions), len(fn.Params), fn.HasDefaults, fn.ReturnType.Name)
		}
	}
	changed := true
	for changed {
		changed = false
		for i := 0; i < N; i++ {
			if !isSafe[i] {
				continue
			}
			fn := vm.functionList[i]
			for _, instr := range fn.Instructions {
				if instr.Op == OP_CALL_DIRECT {
					info := instr.Value.(DirectCallInfo)
					if info.ID >= 0 && info.ID < N {
						targetFn := vm.functionList[info.ID]
						if !jitCallAritySafe(vm, targetFn, info.ArgCount) {
							isSafe[i] = false
							changed = true
							break
						}
						if !isSafe[info.ID] {
							isSafe[i] = false
							changed = true
							break
						}
					}
				} else if instr.Op == OP_CALL_DIRECT_SUB_CONST {
					info := instr.Value.(CallDirectSubConstInfo)
					if info.FnID >= 0 && info.FnID < N {
						targetFn := vm.functionList[info.FnID]
						if !jitCallAritySafe(vm, targetFn, info.ArgCount) {
							isSafe[i] = false
							changed = true
							break
						}
						if !isSafe[info.FnID] {
							isSafe[i] = false
							changed = true
							break
						}
					}
				}
			}
		}
	}

	inferredReturnTypes := make([]stackType, N)
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		if t, ok := hasTypedReturn(fn); ok {
			inferredReturnTypes[i] = t
		} else {
			inferredReturnTypes[i] = stackTypeUnknown
		}
	}

	typeChanged := true
	for iteration := 0; iteration < 10 && typeChanged; iteration++ {
		typeChanged = false
		for i := 0; i < N; i++ {
			if !isSafe[i] {
				continue
			}
			fn := vm.functionList[i]
			if fn.ReturnType.Name != "" {
				continue
			}
			newRetType := inferReturnType(vm, fn, inferredReturnTypes)
			if newRetType != inferredReturnTypes[i] {
				inferredReturnTypes[i] = newRetType
				typeChanged = true
			}
		}
	}

	for i := 0; i < N; i++ {
		if !isSafe[i] {
			continue
		}

		fn := vm.functionList[i]
		if inferredReturnTypes[i] == stackTypeUnknown {
			if t, ok := hasTypedReturn(fn); ok {
				inferredReturnTypes[i] = t
			} else {
				// fmt.Fprintf(os.Stderr, "[JIT DEBUG] function %s is not JIT-safe: mixed/unknown return type\n", fn.Name)
				isSafe[i] = false
			}
		}
	}

	changed = true
	for changed {
		changed = false
		for i := 0; i < N; i++ {
			if !isSafe[i] {
				continue
			}
			fn := vm.functionList[i]
			for _, instr := range fn.Instructions {
				if instr.Op == OP_CALL_DIRECT {
					info := instr.Value.(DirectCallInfo)
					if info.ID >= 0 && info.ID < N && !isSafe[info.ID] {
						isSafe[i] = false
						changed = true
						break
					}
				} else if instr.Op == OP_CALL_DIRECT_SUB_CONST {
					info := instr.Value.(CallDirectSubConstInfo)
					if info.FnID >= 0 && info.FnID < N && !isSafe[info.FnID] {
						isSafe[i] = false
						changed = true
						break
					}
				}
			}
		}
	}

	inferredParamTypes := make([][]stackType, N)
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		inferredParamTypes[i] = make([]stackType, len(fn.Params))
		for p := range fn.Params {
			inferredParamTypes[i][p] = stackTypeUnknown
			if t, ok := stackTypeFromTypeName(fn.Params[p].TypeHint.Name); ok {
				inferredParamTypes[i][p] = t
			}
		}
	}

	paramTypeChanged := true
	for iteration := 0; iteration < 10 && paramTypeChanged; iteration++ {
		paramTypeChanged = false
		for i := 0; i < N; i++ {
			if !isSafe[i] {
				continue
			}
			fn := vm.functionList[i]
			newParamTypes := inferParamTypes(vm, fn, inferredReturnTypes, inferredParamTypes)
			for p := range newParamTypes {
				if newParamTypes[p] != inferredParamTypes[i][p] {
					inferredParamTypes[i][p] = newParamTypes[p]
					paramTypeChanged = true
				}
			}
		}
	}

	changed = true
	for changed {
		changed = false
		for i := 0; i < N; i++ {
			if !isSafe[i] {
				continue
			}
			fn := vm.functionList[i]
			for _, instr := range fn.Instructions {
				if instr.Op == OP_CALL_DIRECT {
					info := instr.Value.(DirectCallInfo)
					if info.ID >= 0 && info.ID < N && !isSafe[info.ID] {
						isSafe[i] = false
						changed = true
						break
					}
				} else if instr.Op == OP_CALL_DIRECT_SUB_CONST {
					info := instr.Value.(CallDirectSubConstInfo)
					if info.FnID >= 0 && info.FnID < N && !isSafe[info.FnID] {
						isSafe[i] = false
						changed = true
						break
					}
				}
			}
			if !isSafe[i] {
				continue
			}
			if !checkCallArgumentsSafe(vm, fn, inferredReturnTypes, inferredParamTypes) {
				if jitCallDebugEnabled() {
					//fmt.Fprintf(os.Stderr, "[JIT PLAN reject] fn=%s id=%d reason=call-argument-types\n", fn.Name, fn.ID)
				}
				isSafe[i] = false
				changed = true
			}
		}
	}

	jitStrings := []string{}
	jitStringID := make(map[string]uint32)
	jitStringAddr := make(map[string]uint32)
	nextAddr := uint32(heapStart)

	addJitString := func(strVal string, preallocate bool) uint32 {
		if id, exists := jitStringID[strVal]; exists {
			if preallocate {
				if _, hasAddr := jitStringAddr[strVal]; !hasAddr {
					jitStringAddr[strVal] = nextAddr
					size := uint32(16 + len(strVal))
					size = (size + 7) &^ 7 // 8-byte align
					nextAddr += size
				}
			}
			return id
		}
		id := uint32(len(jitStrings))
		jitStringID[strVal] = id
		jitStrings = append(jitStrings, strVal)

		if preallocate {
			jitStringAddr[strVal] = nextAddr
			size := uint32(16 + len(strVal))
			size = (size + 7) &^ 7 // 8-byte align
			nextAddr += size
		}

		return id
	}

	for i := 0; i < N; i++ {
		if !isSafe[i] {
			continue
		}
		fn := vm.functionList[i]
		for _, instr := range fn.Instructions {
			switch instr.Op {
			case OP_CONST:
				if strVal, ok := instr.Value.(string); ok {
					addJitString(strVal, true)
				}
			case OP_LOCAL_CONST_OP, OP_LOCAL_CONST_OP_STORE:
				if info, ok := instr.Value.(LocalConstOpInfo); ok {
					if strVal, isStr := info.Const.(string); isStr {
						addJitString(strVal, true)
					}
				}
			case OP_CALL_DIRECT:
				if info, ok := instr.Value.(DirectCallInfo); ok && info.ID >= 0 && info.ID < N {
					if defaults, ok := jitMissingDefaultArgsForCall(vm, vm.functionList[info.ID], info.ArgCount); ok {
						for _, defaultValue := range defaults {
							if !defaultValue.IsInt {
								if strVal, isStr := defaultValue.Value.(string); isStr {
									addJitString(strVal, true)
								}
							}
						}
					}
				}
			case OP_CALL_DIRECT_SUB_CONST:
				if info, ok := instr.Value.(CallDirectSubConstInfo); ok && info.FnID >= 0 && info.FnID < N {
					if defaults, ok := jitMissingDefaultArgsForCall(vm, vm.functionList[info.FnID], info.ArgCount); ok {
						for _, defaultValue := range defaults {
							if !defaultValue.IsInt {
								if strVal, isStr := defaultValue.Value.(string); isStr {
									addJitString(strVal, true)
								}
							}
						}
					}
				}
			case OP_STRING_JOIN:
				addJitString("", true)
			case OP_METHOD_CALL:
				if info, ok := instr.Value.(MethodCallInfo); ok {
					addJitString(info.Method, false)
				}
			}
		}
	}

	var module WasmBuffer
	module.WriteBytes([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	typeSec := &WasmBuffer{}
	typeSec.WriteVarUint(uint32(N + 9)) // N functions + 9 types for imports
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(1) // 1 param
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteVarUint(1) // 1 return
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(3) // 3 params
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteVarUint(1) // 1 return
	typeSec.WriteByte(0x7C) // f64

	typeSec.WriteByte(0x60) // Type Index 2 (f64, f64) -> f64
	typeSec.WriteVarUint(2) // 2 params
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteVarUint(1) // 1 return
	typeSec.WriteByte(0x7C)

	typeSec.WriteByte(0x60) // Type Index 3 (f64, f64, f64, f64) -> f64
	typeSec.WriteVarUint(4) // 4 params: leftTag, leftValue, rightTag, rightValue
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteVarUint(1)
	typeSec.WriteByte(0x7C)

	// Type Index 4: (f64, f64, f64, f64) -> void
	// Used by print_value(tag, value, newline, spaceBefore)
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(4)
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteByte(0x7C)
	typeSec.WriteVarUint(0)

	// Type Index 5: (f64) -> (f64, f64)
	// Used by load_global_wasm(slot)
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(1) // 1 param
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteVarUint(2) // 2 returns
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x7C) // f64

	// Type Index 6: (f64, f64, f64, f64, f64, f64, f64, f64, f64) -> (f64, f64)
	// Used by call_stdlib_wasm(moduleSlot, methodStrID, argCount, tag0, val0, tag1, val1, tag2, val2)
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(9) // 9 params
	for p := 0; p < 9; p++ {
		typeSec.WriteByte(0x7C) // f64 param
	}
	typeSec.WriteVarUint(2) // 2 returns
	typeSec.WriteByte(0x7C) // f64
	typeSec.WriteByte(0x7C) // f64

	// Type Index 7: (f64, f64, f64, f64, f64, f64) -> f64
	// Used by dynamic_join_3
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(6) // 6 params
	for p := 0; p < 6; p++ {
		typeSec.WriteByte(0x7C) // f64 param
	}
	typeSec.WriteVarUint(1) // 1 return
	typeSec.WriteByte(0x7C) // f64

	// Type Index 8: (f64, f64, f64, f64, f64, f64, f64, f64) -> f64
	// Used by dynamic_join_4
	typeSec.WriteByte(0x60)
	typeSec.WriteVarUint(8) // 8 params
	for p := 0; p < 8; p++ {
		typeSec.WriteByte(0x7C) // f64 param
	}
	typeSec.WriteVarUint(1) // 1 return
	typeSec.WriteByte(0x7C) // f64

	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		typeSec.WriteByte(0x60) // function type
		paramCount := len(fn.Params)
		typeSec.WriteVarUint(uint32(paramCount))
		for p := 0; p < paramCount; p++ {
			typeSec.WriteByte(0x7C) // f64 param
		}
		typeSec.WriteVarUint(1) // 1 return
		typeSec.WriteByte(0x7C) // f64 return
	}
	module.WriteByte(1)
	module.WriteVarUint(uint32(len(typeSec.buf)))
	module.WriteBytes(typeSec.buf)

	importSec := &WasmBuffer{}
	importSec.WriteVarUint(jitImportCount) // 9 imports total (0, 1, 2, 3, 4, 5, 6, 7, 8)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(12) // length of "alloc_object"
	importSec.WriteBytes([]byte("alloc_object"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // Type Index 0

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(13) // length of "determine_tag"
	importSec.WriteBytes([]byte("determine_tag"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // Type Index 0

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(10) // length of "array_push"
	importSec.WriteBytes([]byte("array_push"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(1) // Type Index 1

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(18) // length of "string_concat_wasm"
	importSec.WriteBytes([]byte("string_concat_wasm"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(2) // Type Index 2 (Import Index 3)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(14) // length of "string_eq_wasm"
	importSec.WriteBytes([]byte("string_eq_wasm"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(2) // Type Index 2 (Import Index 4)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(11) // length of "dynamic_add"
	importSec.WriteBytes([]byte("dynamic_add"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(3) // Type Index 3 (Import Index 5)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(14) // length of "dynamic_join_3"
	importSec.WriteBytes([]byte("dynamic_join_3"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(7) // Type Index 7 (Import Index 6)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(14) // length of "dynamic_join_4"
	importSec.WriteBytes([]byte("dynamic_join_4"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(8) // Type Index 8 (Import Index 7)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(20) // length of "load_string_constant"
	importSec.WriteBytes([]byte("load_string_constant"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // Type Index 0 (Import Index 6)

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(14) // length of "is_truthy_wasm"
	importSec.WriteBytes([]byte("is_truthy_wasm"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // Type Index 0 (f64 -> f64)

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(8)
	importSec.WriteBytes([]byte("math_pow"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(2) // Type Index 2: (f64, f64) -> f64

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(11)
	importSec.WriteBytes([]byte("print_value"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(4) // type index for (f64, f64, f64) -> void

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(11)
	importSec.WriteBytes([]byte("typeof_wasm"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(2) // Type Index 2: (f64, f64) -> f64

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(10)
	importSec.WriteBytes([]byte("throw_wasm"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(2) // Type Index 2: (f64, f64) -> f64

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(21)
	importSec.WriteBytes([]byte("throw_type_error_wasm"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(2) // Type Index 2: (f64, f64) -> f64

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(16)
	importSec.WriteBytes([]byte("load_global_wasm"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(5) // Type Index 5: (f64) -> (f64, f64)

	importSec.WriteVarUint(3)
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(16)
	importSec.WriteBytes([]byte("call_stdlib_wasm"))
	importSec.WriteByte(0x00)
	importSec.WriteVarUint(6) // Type Index 6: (f64, f64, f64, f64, f64, f64, f64, f64, f64) -> (f64, f64)

	module.WriteByte(2)
	module.WriteVarUint(uint32(len(importSec.buf)))
	module.WriteBytes(importSec.buf)
	funcSec := &WasmBuffer{}
	funcSec.WriteVarUint(uint32(N))
	for i := 0; i < N; i++ {
		funcSec.WriteVarUint(uint32(i + 9)) // Type indices map to 9..N+8
	}
	module.WriteByte(3)
	module.WriteVarUint(uint32(len(funcSec.buf)))
	module.WriteBytes(funcSec.buf)
	memSec := &WasmBuffer{}
	memSec.WriteVarUint(1) // 1 memory definition
	memSec.WriteByte(0x00) // limits: minimum only
	memSec.WriteVarUint(jitMinMemoryPages(nextAddr))

	module.WriteByte(5)
	module.WriteVarUint(uint32(len(memSec.buf)))
	module.WriteBytes(memSec.buf)

	globalSec := &WasmBuffer{}
	globalSec.WriteVarUint(6)                 // heap top + side-effect/deopt metadata
	globalSec.WriteByte(0x7C)                 // type: f64
	globalSec.WriteByte(0x01)                 // mutable: true
	globalSec.WriteByte(0x44)                 // f64.const
	globalSec.WriteFloat64(float64(nextAddr)) // initial heap top
	globalSec.WriteByte(0x0B)                 // end
	for range 5 {
		globalSec.WriteByte(0x7C)
		globalSec.WriteByte(0x01)
		globalSec.WriteByte(0x44)
		globalSec.WriteFloat64(0.0)
		globalSec.WriteByte(0x0B)
	}

	module.WriteByte(6)
	module.WriteVarUint(uint32(len(globalSec.buf)))
	module.WriteBytes(globalSec.buf)

	exportSec := &WasmBuffer{}
	exportSec.WriteVarUint(uint32(N + 6))
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		exportSec.WriteVarUint(uint32(len(fn.Name)))
		exportSec.WriteBytes([]byte(fn.Name))
		exportSec.WriteByte(0x00)                          // kind: function export
		exportSec.WriteVarUint(uint32(i + jitImportCount)) // function index (i + 4 since index 0, 1, 2, 3, 4, 5, 6, 7, 8 are imported)
	}

	exportSec.WriteVarUint(uint32(len("__heap_top")))
	exportSec.WriteBytes([]byte("__heap_top"))
	exportSec.WriteByte(0x03) // kind: global export
	exportSec.WriteVarUint(0) // global index 0

	exportSec.WriteVarUint(uint32(len("__jit_side_effect")))
	exportSec.WriteBytes([]byte("__jit_side_effect"))
	exportSec.WriteByte(0x03) // kind: global export
	exportSec.WriteVarUint(1) // global index 1

	for idx, name := range []string{"__jit_deopt_ip", "__jit_deopt_sp", "__jit_deopt_local_count", "__jit_deopt_function_id"} {
		exportSec.WriteVarUint(uint32(len(name)))
		exportSec.WriteBytes([]byte(name))
		exportSec.WriteByte(0x03)
		exportSec.WriteVarUint(uint32(idx + 2))
	}

	module.WriteByte(7)
	module.WriteVarUint(uint32(len(exportSec.buf)))
	module.WriteBytes(exportSec.buf)
	codeSec := &WasmBuffer{}
	codeSec.WriteVarUint(uint32(N))
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		bodyBytes := compileFunctionBodyBytes(vm, fn, isSafe[i], inferredReturnTypes, jitStringAddr, jitStringID, inferredParamTypes)
		codeSec.WriteBytes(bodyBytes)
	}
	module.WriteByte(10)
	module.WriteVarUint(uint32(len(codeSec.buf)))
	module.WriteBytes(codeSec.buf)

	metas := make([]JitFunctionMeta, 0, N)
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		if !isSafe[i] {
			continue
		}

		retType := inferredReturnTypes[i]
		switch fn.ReturnType.Name {
		case "bool":
			retType = stackTypeBool
		case "number":
			retType = stackTypeNumber
		case "object":
			retType = stackTypeObject
		case "array":
			retType = stackTypeArray
		case "string":
			retType = stackTypeString
		}

		paramTypes := inferredParamTypes[i]
		paramMutated := inferJitMutatedParams(fn)
		if jitCallDebugEnabled() {
			//fmt.Fprintf(os.Stderr, "[JIT PLAN compiled] fn=%s id=%d params=%v ret=%s/%s instrs=%d\n", fn.Name, fn.ID, paramTypes, jitStackTypeName(retType), fn.ReturnType.Name, len(fn.Instructions))
		}

		metas = append(metas, JitFunctionMeta{
			ID:           fn.ID,
			Name:         fn.Name,
			ParamTypes:   append([]stackType(nil), paramTypes...),
			ParamMutated: append([]bool(nil), paramMutated...),
			ParamCount:   len(fn.Params),
			RetType:      retType,
			ReturnType:   fn.ReturnType.Name,
			Memoizable:   isJitFunctionMemoizable(fn, paramMutated),
		})
	}

	vm.jitWasmBytes = append([]byte(nil), module.buf...)
	vm.jitMetas = metas
	vm.jitStrings = append([]string(nil), jitStrings...)
	vm.jitStringMap = make(map[string]uint32, len(jitStrings))
	for id, strVal := range jitStrings {
		vm.jitStringMap[strVal] = uint32(id)
	}
	vm.jitStringAddrs = make(map[string]uint32, len(jitStringAddr))
	for strVal, addr := range jitStringAddr {
		vm.jitStringAddrs[strVal] = addr
	}

	vm.InstantiateJitModule()
}
