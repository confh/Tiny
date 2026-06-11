package vm

import (
	"context"
	"encoding/binary"
	"math"
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
			}
		}
		wasmArgs[i] = api.EncodeF64(val)
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
		println("returned")
		return NewNative(bool(retVal != 0.0)), nil
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

func TryCompileJit(runtime wazero.Runtime, ctx context.Context, fn Function) (*JitFunction, bool) {
	if len(fn.Captures) > 0 {
		return nil, false
	}
	if !fn.ReturnType.IsEmpty() {
		if fn.ReturnType.Name != "number" && fn.ReturnType.Name != "bool" {
			return nil, false
		}
	}
	isBoolRet := fn.ReturnType.Name == "bool"
	for _, instr := range fn.Instructions {
		switch instr.Op {
		case OP_CONST:
			if !instr.IsInt {
				ok := false
				switch v := instr.Value.(type) {
				case int, int64, float64, float32, bool:
					ok = true
				case TinyValue:
					if v.IsInt {
						ok = true
					} else {
						switch v.Value.(type) {
						case int, int64, float64, float32, bool:
							ok = true
						}
					}
				}
				if !ok {
					return nil, false
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
			OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO, OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
		case OP_CALL_DIRECT:
			info, ok := instr.Value.(DirectCallInfo)
			if !ok || info.Name != fn.Name {
				return nil, false
			}
		case OP_CALL_DIRECT_SUB_CONST:
			info, ok := instr.Value.(CallDirectSubConstInfo)
			if !ok || info.FnName != fn.Name {
				return nil, false
			}
		default:
			return nil, false
		}
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
		case OP_CONST, OP_LOAD_LOCAL, OP_LOAD_LOCAL_0, OP_LOAD_LOCAL_1, OP_LOAD_LOCAL_2, OP_LOAD_LOCAL_3, OP_MUL_LOCAL_CONST:
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

	var module WasmBuffer
	module.WriteBytes([]byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00})

	typeSec := &WasmBuffer{}
	typeSec.WriteVarUint(1)
	typeSec.WriteByte(0x60)

	paramCount := len(fn.Params)
	typeSec.WriteVarUint(uint32(paramCount))
	for i := 0; i < paramCount; i++ {
		typeSec.WriteByte(0x7C)
	}
	typeSec.WriteVarUint(1)
	typeSec.WriteByte(0x7C)

	module.WriteByte(1)
	module.WriteVarUint(uint32(len(typeSec.buf)))
	module.WriteBytes(typeSec.buf)

	funcSec := &WasmBuffer{}
	funcSec.WriteVarUint(1)
	funcSec.WriteVarUint(0)

	module.WriteByte(3)
	module.WriteVarUint(uint32(len(funcSec.buf)))
	module.WriteBytes(funcSec.buf)

	exportSec := &WasmBuffer{}
	exportSec.WriteVarUint(1)
	exportSec.WriteVarUint(3)
	exportSec.WriteBytes([]byte("jit"))
	exportSec.WriteByte(0x00)
	exportSec.WriteVarUint(0)

	module.WriteByte(7)
	module.WriteVarUint(uint32(len(exportSec.buf)))
	module.WriteBytes(exportSec.buf)

	codeSec := &WasmBuffer{}
	codeSec.WriteVarUint(1)

	body := &WasmBuffer{}

	stackBase := fn.LocalCount
	extraLocalsCount := fn.LocalCount - paramCount

	var groups [][]any
	if extraLocalsCount > 0 {
		groups = append(groups, []any{extraLocalsCount, byte(0x7C)})
	}
	if maxSp > 0 {
		groups = append(groups, []any{maxSp, byte(0x7C)})
	}

	body.WriteVarUint(uint32(len(groups)))
	for _, g := range groups {
		body.WriteVarUint(uint32(g[0].(int)))
		body.WriteByte(g[1].(byte))
	}

	var activeBlocks []JitBlock
	N := len(fn.Instructions)

	for i, instr := range fn.Instructions {
		sp := spArray[i]

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
			} else if s, ok := AsIntInternal(instr.Value); ok {
				val = float64(s)
			} else if f, ok := instr.Value.(float64); ok {
				val = f
			} else if b, ok := instr.Value.(bool); ok {
				if b {
					val = 1.0
				} else {
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
			body.WriteByte(0xA3)
			body.WriteByte(0x9D)
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
			body.WriteByte(0x9F)

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
			body.WriteByte(0x10)
			body.WriteVarUint(0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp - info.ArgCount))

		case OP_CALL_DIRECT_SUB_CONST:
			info := instr.Value.(CallDirectSubConstInfo)
			body.WriteByte(0x20)
			body.WriteVarUint(uint32(info.Slot))
			body.WriteByte(0x44)
			body.WriteFloat64(float64(info.SubValue))
			body.WriteByte(0xA1)
			body.WriteByte(0x10)
			body.WriteVarUint(0)
			body.WriteByte(0x21)
			body.WriteVarUint(uint32(stackBase + sp))
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

	codeSec.WriteBytes(funcBodySec.buf)

	module.WriteByte(10)
	module.WriteVarUint(uint32(len(codeSec.buf)))
	module.WriteBytes(codeSec.buf)

	moduleID := atomic.AddUint64(&jitCounter, 1)
	uniqueName := fn.Name + "_jit_" + strconv.FormatUint(moduleID, 10)
	config := wazero.NewModuleConfig().WithName(uniqueName)

	compiled, err := runtime.InstantiateWithConfig(ctx, module.buf, config)
	if err != nil {
		println("[JIT ERROR] Wazero instantiation failed:", err.Error())
		return nil, false
	}

	jitFn := compiled.ExportedFunction("jit")
	if jitFn == nil {
		return nil, false
	}

	return &JitFunction{
		fn:         jitFn,
		paramCount: paramCount,
		isBoolRet:  isBoolRet,
	}, true
}
