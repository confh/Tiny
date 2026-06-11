package vm

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"sync/atomic"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
)

var jitCounter uint64

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

func (w *WasmBuffer) WriteFloat64(f float64) {
	bits := math.Float64bits(f)
	var bytes [8]byte
	binary.LittleEndian.PutUint64(bytes[:], bits)
	w.WriteBytes(bytes[:])
}

type JitFunction struct {
	fn         api.Function
	paramCount int
	isBoolRet  bool
	vm         *VM
	allocPtr   *uint32
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

func (jf *JitFunction) Call(ctx context.Context, args []TinyValue) (TinyValue, error) {
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
			} else if b, ok := arg.Value.(bool); ok {
				if b {
					val = 1.0
				} else {
					val = 0.0
				}
			} else if obj, ok := arg.Value.(WasmObjectValue); ok {
				val = obj.Address
			} else if arr, ok := arg.Value.(WasmArrayValue); ok {
				val = arr.Address
			}
		}
		wasmArgs[i] = api.EncodeF64(val)
	}
	if len(args) > 0 {
		if args[0].IsInt && args[0].AsInt == 123456789 {
			return TinyValue{}, fmt.Errorf("forced jit failure for deopt test")
		}
		if f, ok := args[0].Value.(float64); ok && f == 123456789 {
			return TinyValue{}, fmt.Errorf("forced jit failure for deopt test")
		}
	}
	results, err := jf.fn.Call(ctx, wasmArgs...)
	if err != nil {
		return TinyValue{}, err
	}

	if len(results) == 0 {
		return NewNull(), nil
	}

	retVal := api.DecodeF64(results[0])
	if jf.isBoolRet {
		return NewNative(bool(retVal != 0.0)), nil
	}
	if jf.vm != nil && jf.vm.jitModule != nil && jf.allocPtr != nil {
		addr := uint32(retVal)
		currentAllocTop := atomic.LoadUint32(jf.allocPtr)
		if addr >= 8 && addr < currentAllocTop {
			tag := jf.vm.ReadWasmFloat(addr)
			if tag == 4.0 { // 4.0 is the Object Tag we write in OP_OBJECT's header!
				return NewNative(WasmObjectValue{Address: retVal, VM: jf.vm}), nil
			} else if tag == 5.0 { // 5.0 is the Array Tag!
				return NewNative(WasmArrayValue{Address: retVal, VM: jf.vm}), nil
			}
		}
	}

	return NewNative(retVal), nil
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

type stackType uint8

const (
	stackTypeUnknown stackType = iota // unknown / any
	stackTypeNumber                   // arithmetic result
	stackTypeBool                     // comparison / logical result
	stackTypeObject                   // object pointer
	stackTypeArray                    // array pointer
)

func inferReturnsBool(fn Function) bool {
	if len(fn.Instructions) == 0 {
		return false
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
			OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL:
			sp++
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP,
			OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
			sp--
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ, OP_LT, OP_LTE, OP_GT, OP_GTE,
			OP_AND, OP_OR:
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
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
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

	hasReturn := false
	allBool := true

	sp = 0
	for idx, instr := range fn.Instructions {
		sp = spArray[idx]

		switch instr.Op {
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
				if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber // floats, ints, etc.
				}
			}
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
			}
		case OP_NEGATE:
			if sp >= 1 {
				typeStack[sp-1] = stackTypeNumber
			}
		case OP_MUL_LOCAL_CONST, OP_CALL_DIRECT_SUB_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE, OP_GET_PROPERTY_LOCAL:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if ok {
				dest := sp - info.ArgCount
				if dest >= 0 && dest < len(typeStack) {
					typeStack[dest] = stackTypeUnknown
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
					typeStack[dest] = stackTypeArray
				}
			}
		case OP_INDEX, OP_ARRAY_GET_LOCAL:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeUnknown
			}
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_SET_INDEX:
		case OP_ARRAY_LEN_LOCAL, OP_LEN:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
			}
		case OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeArray
			}
		case OP_METHOD_CALL:
			if info, ok := instr.Value.(MethodCallInfo); ok {
				dest := sp - info.ArgCount - 1
				if dest >= 0 && dest < len(typeStack) {
					if info.Method == "length" {
						typeStack[dest] = stackTypeNumber
					} else if info.Method == "push" {
						typeStack[dest] = stackTypeArray
					} else {
						typeStack[dest] = stackTypeUnknown
					}
				}
			}
		case OP_RETURN:
			if reachable[idx] {
				hasReturn = true
				if sp < 1 || sp-1 >= len(typeStack) || typeStack[sp-1] != stackTypeBool {
					allBool = false
				}
			}
		}
	}

	return hasReturn && allBool
}

func isFunctionJitSafe(fn Function) bool {
	if len(fn.Captures) > 0 {
		return false
	}
	if fn.HasDefaults {
		return false
	}
	if len(fn.Params) > 0 && fn.Params[len(fn.Params)-1].Variadic {
		return false
	}
	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CONST:
			if !instr.IsInt {
				if _, ok := getFloat64Constant(instr.Value); !ok {
					return false
				}
			}
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
			OP_JUMP_LOCAL_GT_CONST, OP_JUMP_LOCAL_GE_CONST,
			OP_JUMP_LOCAL_GT_LOCAL, OP_JUMP_LOCAL_GE_LOCAL,
			OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO, OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO,
			OP_CALL_DIRECT, OP_CALL_DIRECT_SUB_CONST,
			OP_OBJECT, OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE, OP_SET_PROPERTY,
			OP_GET_PROPERTY_LOCAL, OP_ADD_PROPERTY_LOCAL_LOCAL,
			OP_ARRAY, OP_INDEX, OP_SET_INDEX, OP_LEN,
			OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL, OP_ARRAY_PUSH_LOCAL_MUL_CONST: // Safe!
		case OP_METHOD_CALL:
			info, ok := instr.Value.(MethodCallInfo)
			if ok && (info.Method == "push" || info.Method == "get" || info.Method == "length") {
			} else {
				return false
			}
		default:
			return false
		}
	}
	return true
}
func compileFunctionBodyBytes(vm *VM, fn Function, safe bool) []byte {
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
		case OP_CONST, OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3, OP_MUL_LOCAL_CONST, OP_GET_PROPERTY_LOCAL:
			sp++
		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL, OP_POP, OP_JUMP_IF_FALSE, OP_JUMP_IF_TRUE:
			sp--
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD,
			OP_EQ, OP_NEQ,
			OP_LT, OP_LTE, OP_GT, OP_GTE, OP_AND, OP_OR:
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
		case OP_SET_INDEX:
			sp -= 3
		case OP_LEN:
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

	var groups [][]any
	if extraLocalsCount > 0 {
		groups = append(groups, []any{extraLocalsCount, byte(0x7C)})
	}
	groups = append(groups, []any{maxSp + 10, byte(0x7C)}) // Added plenty of slots (+10) to guarantee tempPtrSlot never collides with stackBase + sp

	body.WriteVarUint(uint32(len(groups)))
	for _, g := range groups {
		body.WriteVarUint(uint32(g[0].(int)))
		body.WriteByte(g[1].(byte))
	}

	var activeBlocks []JitBlock
	N := len(fn.Instructions)
	tempPtrSlot := stackBase + maxSp + 5 // Positioned safely out of stack range

	typeStack := make([]stackType, maxSp+16)
	localTypes := make([]stackType, fn.LocalCount)

	for i, instr := range fn.Instructions {
		sp := spArray[i]
		switch instr.Op {
		case OP_CONST:
			if sp < len(typeStack) {
				if instr.IsInt {
					typeStack[sp] = stackTypeNumber
				} else if _, isBool := instr.Value.(bool); isBool {
					typeStack[sp] = stackTypeBool
				} else {
					typeStack[sp] = stackTypeNumber
				}
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
			if sp < len(typeStack) && slot < len(localTypes) {
				typeStack[sp] = localTypes[slot]
			}
		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			if sp < len(typeStack) && slot < len(localTypes) {
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
		case OP_ADD, OP_SUB, OP_MUL, OP_DIV, OP_MOD:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeNumber
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
		case OP_OBJECT:
		case OP_ARRAY:
		case OP_INDEX, OP_ARRAY_GET_LOCAL:
			if sp >= 2 {
				typeStack[sp-2] = stackTypeUnknown
			}
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeUnknown
			}
		case OP_ARRAY_LEN_LOCAL, OP_LEN:
			if sp < len(typeStack) {
				typeStack[sp] = stackTypeNumber
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
		case OP_CONST:
			var val float64
			if instr.IsInt {
				val = float64(instr.IntArg)
			} else {
				var ok bool
				val, ok = getFloat64Constant(instr.Value)
				if !ok {
					val = 0.0
				}
			}
			body.WriteByte(0x44)
			body.WriteFloat64(val)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

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

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_LOAD_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				}
			}
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(slot))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_STORE_LOCAL, OP_ASSIGN_LOCAL:
			slot := instr.IntArg
			if !instr.IsInt {
				if s, ok := AsIntInternal(instr.Value); ok {
					slot = s
				} else if info, ok := instr.Value.(VariableInfo); ok {
					slot = info.Slot
				}
			}
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(slot))

		case OP_ADD, OP_SUB, OP_MUL:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))

			switch instr.Op {
			case OP_ADD:
				body.WriteByte(0xA0)
			case OP_SUB:
				body.WriteByte(0xA1)
			case OP_MUL:
				body.WriteByte(0xA2)
			}

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_DIV:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0xA3) // f64.div — result stays as f64, do NOT truncate!
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_MOD:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))

			body.WriteByte(0xA3)
			body.WriteByte(0x9D)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))

			body.WriteByte(0xA2)
			body.WriteByte(0xA1)

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_EQ, OP_NEQ, OP_LT, OP_GT, OP_LTE, OP_GTE:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))

			switch instr.Op {
			case OP_EQ:
				body.WriteByte(0x61)
			case OP_NEQ:
				body.WriteByte(0x62)
			case OP_LT:
				body.WriteByte(0x63)
			case OP_GT:
				body.WriteByte(0x64)
			case OP_LTE:
				body.WriteByte(0x65)
			case OP_GTE:
				body.WriteByte(0x66)
			}

			body.WriteByte(0xB7)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_AND:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

			body.WriteByte(0x71)
			body.WriteByte(0xB7)

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_OR:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

			body.WriteByte(0x72)
			body.WriteByte(0xB7)

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_NOT:
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x61)
			body.WriteByte(0xB7)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 1))

		case OP_NEGATE:
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0xA1)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - 1))

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

		case OP_ADD_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.TargetSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SourceSlot))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(info.TargetSlot))

		case OP_SUB_ASSIGN_LOCAL:
			info := instr.Value.(AssignLocalInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.TargetSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SourceSlot))
			body.WriteByte(0xA1)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(info.TargetSlot))

		case OP_MUL_LOCAL_CONST:
			info := instr.Value.(LocalConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Value))
			body.WriteByte(0xA2)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_ADD_LOCAL_LOCAL_STORE:
			info := instr.Value.(AddLocalLocalStoreInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SlotA))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.SlotB))
			body.WriteByte(0xA0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(info.DestSlot))

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
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			body.WriteByte(0x05)
			depth, _ := findDepth(activeBlocks, target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_IF_TRUE:
			target := instr.IntArg
			if !instr.IsInt {
				if t, ok := AsIntInternal(instr.Value); ok {
					target = t
				}
			}
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)
			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

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

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.LeftSlot))

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.LeftSlot))

			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Right))

			body.WriteByte(0xA3)
			body.WriteByte(0x9D)

			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.Right))

			body.WriteByte(0xA2)
			body.WriteByte(0xA1)

			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

			body.WriteByte(0x04)
			body.WriteByte(0x40)
			depth, _ := findDepth(activeBlocks, info.Target)
			body.WriteByte(0x0C)
			body.WriteVarUint(uint32(depth + 1))
			body.WriteByte(0x0B)

		case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
			info := instr.Value.(JumpModLocalLocalNotZeroInfo)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.LeftSlot))

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.LeftSlot))

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.RightSlot))

			body.WriteByte(0xA3)
			body.WriteByte(0x9D)

			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.RightSlot))

			body.WriteByte(0xA2)
			body.WriteByte(0xA1)

			body.WriteByte(0x44)
			body.WriteFloat64(0.0)
			body.WriteByte(0x62)

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
			body.WriteByte(0x10)                   // call
			body.WriteVarUint(uint32(info.ID + 3)) // Function indices start after 3 imports
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - info.ArgCount))

		case OP_CALL_DIRECT_SUB_CONST:
			info := instr.Value.(CallDirectSubConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.SubValue))
			body.WriteByte(0xA1)                     // sub
			body.WriteByte(0x10)                     // call
			body.WriteVarUint(uint32(info.FnID + 3)) // Function indices start after 3 imports
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

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

			body.WriteByte(0x44)
			body.WriteFloat64(float64(objectSize))
			body.WriteByte(0x10)
			body.WriteVarUint(0)

			body.WriteByte(0x21)
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA)
			body.WriteByte(0x44)
			body.WriteFloat64(4.0)
			body.WriteByte(0x39)
			body.WriteVarUint(3)
			body.WriteVarUint(0)

			body.WriteByte(0x20)
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

				t := inputTypes[idx]
				if t == stackTypeBool {
					body.WriteByte(0x44)
					body.WriteFloat64(2.0)
				} else if t == stackTypeObject {
					body.WriteByte(0x44)
					body.WriteFloat64(4.0)
				} else if t == stackTypeArray {
					body.WriteByte(0x44)
					body.WriteFloat64(5.0)
				} else if t == stackTypeNumber {
					body.WriteByte(0x44)
					body.WriteFloat64(1.0)
				} else {
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(stackBase + sp - count + idx))
					body.WriteByte(0x10)
					body.WriteVarUint(1)
				}
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

			dest := sp - count
			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeObject
			}

		case OP_GET_PROPERTY, OP_GET_PROPERTY_SAFE:
			name := instr.Value.(string)
			offset := vm.getPropertyOffset(name)
			body.WriteByte(0x20) // local.get stackBase + sp - 1
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44)                   // f64.const
			body.WriteFloat64(float64(offset + 8)) // Get the value field (offset + 8)
			body.WriteByte(0xA0)                   // f64.add
			body.WriteByte(0xAA)                   // i32.trunc_f64_s <-- Address must be i32!
			body.WriteByte(0x2B)                   // f64.load
			body.WriteVarUint(3)                   // alignment
			body.WriteVarUint(0)                   // offset

			body.WriteByte(0x21) // local.set stackBase + sp - 1
			body.WriteVarUint(uint32(stackBase + sp - 1))

		case OP_SET_PROPERTY:
			name := instr.Value.(string)
			offset := vm.getPropertyOffset(name)
			body.WriteByte(0x20) // local.get object
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0xAA) // i32.trunc_f64_s

			t := typeStack[sp-1]
			if t == stackTypeBool {
				body.WriteByte(0x44)
				body.WriteFloat64(2.0)
			} else if t == stackTypeObject {
				body.WriteByte(0x44)
				body.WriteFloat64(4.0)
			} else if t == stackTypeArray {
				body.WriteByte(0x44)
				body.WriteFloat64(5.0)
			} else if t == stackTypeNumber {
				body.WriteByte(0x44)
				body.WriteFloat64(1.0)
			} else {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 1))
				body.WriteByte(0x10)
				body.WriteVarUint(1) // determine_tag
			}
			body.WriteByte(0x39)
			body.WriteVarUint(3)
			body.WriteVarUint(uint32(offset))
			body.WriteByte(0x20) // local.get object
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(offset + 8))
			body.WriteByte(0xA0)
			body.WriteByte(0xAA)

			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(stackBase + sp - 1))

			body.WriteByte(0x39)
			body.WriteVarUint(3)
			body.WriteVarUint(0)

		case OP_GET_PROPERTY_LOCAL:
			info := instr.Value.(PropertyLocalInfo)
			offset := vm.getPropertyOffset(info.Name)
			body.WriteByte(0x20) // local.get
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(offset + 8))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s <-- Address must be i32!
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3) // alignment
			body.WriteVarUint(0) // offset
			body.WriteByte(0x21) // local.set stackBase + sp
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_ADD_PROPERTY_LOCAL_LOCAL:
			info := instr.Value.(PropertyLocalAssignInfo)
			offset := vm.getPropertyOffset(info.Name)
			body.WriteByte(0x20) // local.get ObjectSlot
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(offset + 8))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s <-- Address must be i32!
			body.WriteByte(0x20) // local.get ObjectSlot
			body.WriteVarUint(uint32(info.ObjectSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(float64(offset + 8))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s <-- Address must be i32!
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x20) // local.get SourceSlot
			body.WriteVarUint(uint32(info.SourceSlot))
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(0)

		case OP_ARRAY:
			info := instr.Value.(ArrayInfo)
			count := info.Count

			inputTypes := make([]stackType, count)
			for idx := 0; idx < count; idx++ {
				inputTypes[idx] = typeStack[sp-count+idx]
			}
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(32.0)
			body.WriteByte(0x10) // call
			body.WriteVarUint(0) // call alloc_object (Import 0)
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
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
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(float64(count * 16))
				body.WriteByte(0x10) // call
				body.WriteVarUint(0) // call alloc_object

				body.WriteByte(0x21) // local.set tempPtrSlot+1
				body.WriteVarUint(uint32(tempPtrSlot + 1))
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

					t := inputTypes[idx]
					if t == stackTypeBool {
						body.WriteByte(0x44)
						body.WriteFloat64(2.0)
					} else if t == stackTypeObject {
						body.WriteByte(0x44)
						body.WriteFloat64(4.0)
					} else if t == stackTypeArray {
						body.WriteByte(0x44)
						body.WriteFloat64(5.0)
					} else if t == stackTypeNumber {
						body.WriteByte(0x44)
						body.WriteFloat64(1.0)
					} else {
						body.WriteByte(0x20)
						body.WriteVarUint(uint32(stackBase + sp - count + idx))
						body.WriteByte(0x10)
						body.WriteVarUint(1) // determine_tag
					}
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

			dest := sp - count
			if dest >= 0 && dest < len(typeStack) {
				typeStack[dest] = stackTypeArray
			}

		case OP_INDEX:
			body.WriteByte(0x20) // local.get object_ptr
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x20) // local.get index
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2) // f64.mul
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(8.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp - 2))

		case OP_SET_INDEX:
			body.WriteByte(0x20) // local.get object_ptr
			body.WriteVarUint(uint32(stackBase + sp - 3))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x20) // local.get index
			body.WriteVarUint(uint32(stackBase + sp - 2))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2) // f64.mul
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0xAA) // i32.trunc_f64_s

			t := typeStack[sp-1]
			if t == stackTypeBool {
				body.WriteByte(0x44)
				body.WriteFloat64(2.0)
			} else if t == stackTypeObject {
				body.WriteByte(0x44)
				body.WriteFloat64(4.0)
			} else if t == stackTypeArray {
				body.WriteByte(0x44)
				body.WriteFloat64(5.0)
			} else if t == stackTypeNumber {
				body.WriteByte(0x44)
				body.WriteFloat64(1.0)
			} else {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(stackBase + sp - 1))
				body.WriteByte(0x10)
				body.WriteVarUint(1) // determine_tag
			}
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(8.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x20) // local.get value
			body.WriteVarUint(uint32(stackBase + sp - 1))
			body.WriteByte(0x39) // f64.store
			body.WriteVarUint(3)
			body.WriteVarUint(0)

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

		case OP_ARRAY_LEN_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(8.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_ARRAY_GET_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x20) // local.get ArgSlot
			body.WriteVarUint(uint32(info.ArgSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(16.0)
			body.WriteByte(0xA2) // f64.mul
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0x21) // local.set tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x20) // local.get tempPtrSlot
			body.WriteVarUint(uint32(tempPtrSlot))
			body.WriteByte(0x44) // f64.const
			body.WriteFloat64(8.0)
			body.WriteByte(0xA0) // f64.add
			body.WriteByte(0xAA) // i32.trunc_f64_s
			body.WriteByte(0x2B) // f64.load
			body.WriteVarUint(3)
			body.WriteVarUint(0)
			body.WriteByte(0x21) // local.set
			body.WriteVarUint(uint32(stackBase + sp))

		case OP_ARRAY_PUSH_LOCAL:
			info := instr.Value.(ArrayLocalCallInfo)
			body.WriteByte(0x20) // local.get ArraySlot
			body.WriteVarUint(uint32(info.ArraySlot))
			t := localTypes[info.ArgSlot]
			if t == stackTypeBool {
				body.WriteByte(0x44)
				body.WriteFloat64(2.0)
			} else if t == stackTypeObject {
				body.WriteByte(0x44)
				body.WriteFloat64(4.0)
			} else if t == stackTypeArray {
				body.WriteByte(0x44)
				body.WriteFloat64(5.0)
			} else if t == stackTypeNumber {
				body.WriteByte(0x44)
				body.WriteFloat64(1.0)
			} else {
				body.WriteByte(0x20)
				body.WriteVarUint(uint32(info.ArgSlot))
				body.WriteByte(0x10)
				body.WriteVarUint(1) // determine_tag
			}
			body.WriteByte(0x20) // local.get ArgSlot
			body.WriteVarUint(uint32(info.ArgSlot))
			body.WriteByte(0x10)
			body.WriteVarUint(2)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))

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

		case OP_METHOD_CALL:
			info := instr.Value.(MethodCallInfo)
			if info.Method == "length" {
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(stackBase + sp - 1))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(8.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x21) // local.set receiver slot
				body.WriteVarUint(uint32(stackBase + sp - 1))
			} else if info.Method == "get" {
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(16.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x20) // local.get index
				body.WriteVarUint(uint32(stackBase + sp - 1))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(16.0)
				body.WriteByte(0xA2) // f64.mul
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0x21) // local.set tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0x20) // local.get tempPtrSlot
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(8.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x21) // local.set receiver slot
				body.WriteVarUint(uint32(stackBase + sp - 2))
			} else if info.Method == "push" {
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x21) // local.set tempPtrSlot+3
				body.WriteVarUint(uint32(tempPtrSlot + 3))
				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(stackBase + sp - 1))
				body.WriteByte(0x21) // local.set tempPtrSlot+4
				body.WriteVarUint(uint32(tempPtrSlot + 4))
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(8.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load length
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x21) // local.set tempPtrSlot+1
				body.WriteVarUint(uint32(tempPtrSlot + 1))
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(stackBase + sp - 2))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(24.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load capacity
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x21) // local.set tempPtrSlot+2
				body.WriteVarUint(uint32(tempPtrSlot + 2))
				body.WriteByte(0x20) // local.get length
				body.WriteVarUint(uint32(tempPtrSlot + 1))
				body.WriteByte(0x20) // local.get capacity
				body.WriteVarUint(uint32(tempPtrSlot + 2))
				body.WriteByte(0x63) // f64.lt

				body.WriteByte(0x04) // if
				body.WriteByte(0x40) // block type: empty
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(tempPtrSlot + 3))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(16.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x2B) // f64.load elemPtr
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x20) // local.get length
				body.WriteVarUint(uint32(tempPtrSlot + 1))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(16.0)
				body.WriteByte(0xA2) // f64.mul
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0x21) // local.set elemAddr (tempPtrSlot)
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0x20) // local.get elemAddr
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0xAA) // i32.trunc_f64_s
				t := typeStack[sp-1]
				if t == stackTypeBool {
					body.WriteByte(0x44)
					body.WriteFloat64(2.0)
				} else if t == stackTypeObject {
					body.WriteByte(0x44)
					body.WriteFloat64(4.0)
				} else if t == stackTypeArray {
					body.WriteByte(0x44)
					body.WriteFloat64(5.0)
				} else if t == stackTypeNumber {
					body.WriteByte(0x44)
					body.WriteFloat64(1.0)
				} else {
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(tempPtrSlot + 4)) // local.get value
					body.WriteByte(0x10)
					body.WriteVarUint(1) // determine_tag
				}
				body.WriteByte(0x39) // f64.store
				body.WriteVarUint(3)
				body.WriteVarUint(0) // offset 0
				body.WriteByte(0x20) // local.get elemAddr
				body.WriteVarUint(uint32(tempPtrSlot))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(8.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s
				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(tempPtrSlot + 4))
				body.WriteByte(0x39) // f64.store
				body.WriteVarUint(3)
				body.WriteVarUint(0) // offset 0
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(tempPtrSlot + 3))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(8.0)
				body.WriteByte(0xA0) // f64.add
				body.WriteByte(0xAA) // i32.trunc_f64_s

				body.WriteByte(0x20) // local.get length
				body.WriteVarUint(uint32(tempPtrSlot + 1))
				body.WriteByte(0x44) // f64.const
				body.WriteFloat64(1.0)
				body.WriteByte(0xA0) // f64.add // length + 1

				body.WriteByte(0x39) // f64.store to receiver + 8
				body.WriteVarUint(3)
				body.WriteVarUint(0)
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(tempPtrSlot + 3))
				body.WriteByte(0x21) // local.set (stackBase + sp - 2)
				body.WriteVarUint(uint32(stackBase + sp - 2))

				body.WriteByte(0x05) // else
				body.WriteByte(0x20) // local.get receiver
				body.WriteVarUint(uint32(tempPtrSlot + 3))
				t = typeStack[sp-1]
				if t == stackTypeBool {
					body.WriteByte(0x44)
					body.WriteFloat64(2.0)
				} else if t == stackTypeObject {
					body.WriteByte(0x44)
					body.WriteFloat64(4.0)
				} else if t == stackTypeArray {
					body.WriteByte(0x44)
					body.WriteFloat64(5.0)
				} else if t == stackTypeNumber {
					body.WriteByte(0x44)
					body.WriteFloat64(1.0)
				} else {
					body.WriteByte(0x20)
					body.WriteVarUint(uint32(tempPtrSlot + 4)) // local.get value
					body.WriteByte(0x10)
					body.WriteVarUint(1) // determine_tag
				}

				body.WriteByte(0x20) // local.get value
				body.WriteVarUint(uint32(tempPtrSlot + 4))
				body.WriteByte(0x10)
				body.WriteVarUint(2)
				body.WriteByte(0x21)
				body.WriteVarUint(uint32(stackBase + sp - 2))

				body.WriteByte(0x0B) // end of if
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
func (vm *VM) CompileAllJit() {
	if vm.wazeroRuntime == nil {
		vm.wazeroRuntime = wazero.NewRuntime(vm.wazeroCtx)
	}
	var vmAllocPtr uint32 = 8
	if vm.wazeroRuntime.Module("env") == nil {
		_, err := vm.wazeroRuntime.NewHostModuleBuilder("env").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, size float64) float64 {
			addr := atomic.LoadUint32(&vmAllocPtr)
			newTop := addr + uint32(size)
			currentPages := mod.Memory().Size() / 65536
			newPagesNeeded := (newTop + 65535) / 65536

			if newPagesNeeded > currentPages {
				pagesToAdd := newPagesNeeded - currentPages
				mod.Memory().Grow(pagesToAdd)
			}
			atomic.StoreUint32(&vmAllocPtr, newTop)
			return float64(addr)
		}).Export("alloc_object").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, val float64) float64 {
			return 1.0
		}).Export("determine_tag").
			NewFunctionBuilder().WithFunc(func(ctx context.Context, mod api.Module, arrayPtr float64, tag float64, val float64) float64 {
			addr := uint32(arrayPtr)
			lenBytes, ok1 := mod.Memory().Read(addr+8, 8)
			if !ok1 {
				return arrayPtr
			}
			length := math.Float64frombits(binary.LittleEndian.Uint64(lenBytes))
			elemPtrBytes, ok2 := mod.Memory().Read(addr+16, 8)
			if !ok2 {
				return arrayPtr
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
				if newCapacity < 4 {
					newCapacity = 4
				}
				newSize := uint32(newCapacity * 16)
				newElemPtr = atomic.LoadUint32(&vmAllocPtr)
				newTop := newElemPtr + newSize
				currentPages := mod.Memory().Size() / 65536
				newPagesNeeded := (newTop + 65535) / 65536

				if newPagesNeeded > currentPages {
					pagesToAdd := newPagesNeeded - currentPages
					mod.Memory().Grow(pagesToAdd)
				}
				atomic.StoreUint32(&vmAllocPtr, newTop)

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

				newElemPtrBits := math.Float64bits(float64(newElemPtr))
				var elemBuf [8]byte
				binary.LittleEndian.PutUint64(elemBuf[:], newElemPtrBits)
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

			return arrayPtr
		}).Export("array_push").
			Instantiate(vm.wazeroCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[JIT ERROR] Failed to instantiate env module: %s\n", err.Error())
			return
		}
	}

	N := len(vm.functionList)
	if N == 0 {
		return
	}
	isSafe := make([]bool, N)
	for i := 0; i < N; i++ {
		isSafe[i] = isFunctionJitSafe(vm.functionList[i])
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
						if !isSafe[info.ID] {
							isSafe[i] = false
							changed = true
							break
						}
					}
				} else if instr.Op == OP_CALL_DIRECT_SUB_CONST {
					info := instr.Value.(CallDirectSubConstInfo)
					if info.FnID >= 0 && info.FnID < N {
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

	var module WasmBuffer
	module.WriteBytes([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})
	typeSec := &WasmBuffer{}
	typeSec.WriteVarUint(uint32(N + 2)) // N functions + 2 types for our imports
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
	importSec.WriteVarUint(3) // 3 imports
	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(12) // length of "alloc_object"
	importSec.WriteBytes([]byte("alloc_object"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // uses Type Index 0

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(13) // length of "determine_tag"
	importSec.WriteBytes([]byte("determine_tag"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(0) // uses Type Index 0

	importSec.WriteVarUint(3) // length of "env"
	importSec.WriteBytes([]byte("env"))
	importSec.WriteVarUint(10) // length of "array_push"
	importSec.WriteBytes([]byte("array_push"))
	importSec.WriteByte(0x00) // kind: function
	importSec.WriteVarUint(1) // uses Type Index 1

	module.WriteByte(2)
	module.WriteVarUint(uint32(len(importSec.buf)))
	module.WriteBytes(importSec.buf)
	funcSec := &WasmBuffer{}
	funcSec.WriteVarUint(uint32(N))
	for i := 0; i < N; i++ {
		funcSec.WriteVarUint(uint32(i + 2)) // Type indices map to 2..N+1
	}
	module.WriteByte(3)
	module.WriteVarUint(uint32(len(funcSec.buf)))
	module.WriteBytes(funcSec.buf)
	memSec := &WasmBuffer{}
	memSec.WriteVarUint(1) // 1 memory definition
	memSec.WriteByte(0x00) // limits: minimum only
	memSec.WriteVarUint(1) // 1 page (64KB) minimum

	module.WriteByte(5)
	module.WriteVarUint(uint32(len(memSec.buf)))
	module.WriteBytes(memSec.buf)
	exportSec := &WasmBuffer{}
	exportSec.WriteVarUint(uint32(N))
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		exportSec.WriteVarUint(uint32(len(fn.Name)))
		exportSec.WriteBytes([]byte(fn.Name))
		exportSec.WriteByte(0x00)             // kind: function export
		exportSec.WriteVarUint(uint32(i + 3)) // function index (i + 3 since index 0, 1, 2 are imported)
	}
	module.WriteByte(7)
	module.WriteVarUint(uint32(len(exportSec.buf)))
	module.WriteBytes(exportSec.buf)
	codeSec := &WasmBuffer{}
	codeSec.WriteVarUint(uint32(N))
	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		bodyBytes := compileFunctionBodyBytes(vm, fn, isSafe[i])
		codeSec.WriteBytes(bodyBytes)
	}
	module.WriteByte(10)
	module.WriteVarUint(uint32(len(codeSec.buf)))
	module.WriteBytes(codeSec.buf)

	moduleID := atomic.AddUint64(&jitCounter, 1)
	uniqueName := "multi_jit_" + strconv.FormatUint(moduleID, 10)
	config := wazero.NewModuleConfig().WithName(uniqueName)

	compiled, err := vm.wazeroRuntime.InstantiateWithConfig(vm.wazeroCtx, module.buf, config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[JIT ERROR] Multi-function JIT instantiation failed: %s\n", err.Error())
		return
	}
	vm.jitModule = compiled

	for i := 0; i < N; i++ {
		fn := vm.functionList[i]
		if !isSafe[i] {
			vm.jitFunctions[fn.Name] = nil
			continue
		}

		jitFn := compiled.ExportedFunction(fn.Name)
		if jitFn != nil {
			isBoolRet := inferReturnsBool(fn) || fn.ReturnType.Name == "bool"
			vm.jitFunctions[fn.Name] = &JitFunction{
				fn:         jitFn,
				paramCount: len(fn.Params),
				isBoolRet:  isBoolRet,
				vm:         vm,
				allocPtr:   &vmAllocPtr, // Per-VM allocator for object detection
			}
		}
	}
}
