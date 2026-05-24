package vm

import "testing"

func TestOptimizeIncLocalRemapsJumpTargets(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_CONST, Value: 1},
		{Op: OP_ADD},
		{Op: OP_ASSIGN_LOCAL, Value: 0},
		{Op: OP_JUMP, Value: 6},
		{Op: OP_CONST, Value: "skipped"},
		{Op: OP_HALT},
	}

	optimized := OptimizeBytecode(input)

	if len(optimized) != 4 {
		t.Fatalf("expected 4 instructions after optimization, got %d: %#v", len(optimized), optimized)
	}

	if optimized[0].Op != OP_INC_LOCAL {
		t.Fatalf("expected first instruction to be OP_INC_LOCAL, got %v", optimized[0].Op)
	}

	info, ok := optimized[0].Value.(IncrementInfo)
	if !ok {
		t.Fatalf("expected IncrementInfo, got %T", optimized[0].Value)
	}

	if info.Slot != 0 || info.IntAmount != 1 {
		t.Fatalf("unexpected increment info: %#v", info)
	}

	if optimized[1].Value != 3 {
		t.Fatalf("expected jump target to be remapped from 6 to 3, got %#v", optimized[1].Value)
	}
}

func TestOptimizeJumpLocalGEConstRemapsTryHandlers(t *testing.T) {
	input := []Instruction{
		{Op: OP_SETUP_TRY, Value: TryInfo{CatchIP: 6, Name: "err"}},
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_CONST, Value: 3},
		{Op: OP_LT},
		{Op: OP_JUMP_IF_FALSE, Value: 6},
		{Op: OP_CONST, Value: "body"},
		{Op: OP_HALT},
	}

	optimized := OptimizeBytecode(input)

	tryInfo, ok := optimized[0].Value.(TryInfo)
	if !ok {
		t.Fatalf("expected TryInfo, got %T", optimized[0].Value)
	}

	if tryInfo.CatchIP != len(optimized)-1 {
		t.Fatalf("expected try catch target %d, got %d", len(optimized)-1, tryInfo.CatchIP)
	}
}

func TestOptimizeLoadLocalSlots(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_LOAD_LOCAL, Value: 3},
		{Op: OP_LOAD_LOCAL, Value: 4},
	}

	optimized := OptimizeBytecode(input)

	want := []OpCode{
		OP_LOAD_LOCAL_0,
		OP_LOAD_LOCAL_1,
		OP_LOAD_LOCAL_2,
		OP_LOAD_LOCAL_3,
		OP_LOAD_LOCAL,
	}

	for i, op := range want {
		if optimized[i].Op != op {
			t.Fatalf("instruction %d: expected %v, got %v", i, op, optimized[i].Op)
		}
	}

	if optimized[4].Value != 4 {
		t.Fatalf("slot 4 load should keep original operand, got %#v", optimized[4].Value)
	}
}

func TestOptimizeJumpModLocalConstNotZero(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_CONST, Value: 3},
		{Op: OP_MOD},
		{Op: OP_CONST, Value: 0},
		{Op: OP_EQ},
		{Op: OP_JUMP_IF_FALSE, Value: 7},
		{Op: OP_CONST, Value: "body"},
		{Op: OP_HALT},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO {
		t.Fatalf("expected modulo/const branch to be fused, got %v", optimized[0].Op)
	}

	info, ok := optimized[0].Value.(JumpModLocalConstNotZeroInfo)
	if !ok {
		t.Fatalf("expected JumpModLocalConstNotZeroInfo, got %T", optimized[0].Value)
	}

	if info.LeftSlot != 2 || info.Right != 3 || info.Target != 2 {
		t.Fatalf("unexpected modulo const info: %#v", info)
	}
}

func TestOptimizeLocalMethodCalls(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "customZero", ArgCount: 0}},
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "customOne", ArgCount: 1}},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_METHOD_CALL_LOCAL_0 {
		t.Fatalf("expected zero-arg local method call to be fused, got %v", optimized[0].Op)
	}
	if optimized[1].Op != OP_METHOD_CALL_LOCAL_1 {
		t.Fatalf("expected one-arg local method call to be fused, got %v", optimized[1].Op)
	}

	zeroArg := optimized[0].Value.(MethodLocalCallInfo)
	if zeroArg.Method != "customZero" || zeroArg.ReceiverSlot != 1 {
		t.Fatalf("unexpected zero-arg method info: %#v", zeroArg)
	}

	oneArg := optimized[1].Value.(MethodLocalCallInfo)
	if oneArg.Method != "customOne" || oneArg.ReceiverSlot != 1 || oneArg.ArgSlot != 2 {
		t.Fatalf("unexpected one-arg method info: %#v", oneArg)
	}
}

func TestOptimizeArrayLocalMethodCalls(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "length", ArgCount: 0}},
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "get", ArgCount: 1}},
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_LOAD_LOCAL, Value: 3},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "push", ArgCount: 1}},
	}

	optimized := OptimizeBytecode(input)

	want := []OpCode{OP_ARRAY_LEN_LOCAL, OP_ARRAY_GET_LOCAL, OP_ARRAY_PUSH_LOCAL}
	for i, op := range want {
		if optimized[i].Op != op {
			t.Fatalf("instruction %d: expected %v, got %v", i, op, optimized[i].Op)
		}
	}

	lengthInfo := optimized[0].Value.(ArrayLocalCallInfo)
	if lengthInfo.ArraySlot != 1 {
		t.Fatalf("unexpected length info: %#v", lengthInfo)
	}

	getInfo := optimized[1].Value.(ArrayLocalCallInfo)
	if getInfo.ArraySlot != 1 || getInfo.ArgSlot != 2 {
		t.Fatalf("unexpected get info: %#v", getInfo)
	}

	pushInfo := optimized[2].Value.(ArrayLocalCallInfo)
	if pushInfo.ArraySlot != 1 || pushInfo.ArgSlot != 3 {
		t.Fatalf("unexpected push info: %#v", pushInfo)
	}
}

func TestOptimizeArrayPushLocalMulConst(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_CONST, Value: 3},
		{Op: OP_MUL},
		{Op: OP_METHOD_CALL, Value: MethodCallInfo{Method: "push", ArgCount: 1}},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_ARRAY_PUSH_LOCAL_MUL_CONST {
		t.Fatalf("expected array push local*const to be fused, got %v", optimized[0].Op)
	}

	info := optimized[0].Value.(ArrayLocalMulConstInfo)
	if info.ArraySlot != 1 || info.ArgSlot != 2 || info.Factor != 3 {
		t.Fatalf("unexpected array push mul const info: %#v", info)
	}
}

func TestOptimizeGetPropertyLocal(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 4},
		{Op: OP_GET_PROPERTY, Value: "score"},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_GET_PROPERTY_LOCAL {
		t.Fatalf("expected local property get to be fused, got %v", optimized[0].Op)
	}

	info := optimized[0].Value.(PropertyLocalInfo)
	if info.Slot != 4 || info.Name != "score" {
		t.Fatalf("unexpected property local info: %#v", info)
	}
}

func TestOptimizeAddPropertyLocalLocal(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_GET_PROPERTY, Value: "value"},
		{Op: OP_LOAD_LOCAL, Value: 1},
		{Op: OP_ADD},
		{Op: OP_SET_PROPERTY, Value: "value"},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_ADD_PROPERTY_LOCAL_LOCAL {
		t.Fatalf("expected property add assignment to be fused, got %v", optimized[0].Op)
	}

	info := optimized[0].Value.(PropertyLocalAssignInfo)
	if info.ObjectSlot != 0 || info.SourceSlot != 1 || info.Name != "value" {
		t.Fatalf("unexpected property assign info: %#v", info)
	}
}

func TestOptimizeMulLocalConst(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 2},
		{Op: OP_CONST, Value: 8},
		{Op: OP_MUL},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_MUL_LOCAL_CONST {
		t.Fatalf("expected local*const to be fused, got %v", optimized[0].Op)
	}

	info := optimized[0].Value.(LocalConstInfo)
	if info.Slot != 2 || info.Value != 8 {
		t.Fatalf("unexpected local const info: %#v", info)
	}
}

func TestOptimizeSubConstToIncLocal(t *testing.T) {
	input := []Instruction{
		{Op: OP_LOAD_LOCAL, Value: 0},
		{Op: OP_CONST, Value: 3},
		{Op: OP_SUB},
		{Op: OP_ASSIGN_LOCAL, Value: 0},
		{Op: OP_HALT},
	}

	optimized := OptimizeBytecode(input)

	if optimized[0].Op != OP_INC_LOCAL {
		t.Fatalf("expected subtract const to become OP_INC_LOCAL, got %v", optimized[0].Op)
	}

	info := optimized[0].Value.(IncrementInfo)
	if info.Slot != 0 || info.IntAmount != -3 {
		t.Fatalf("unexpected increment info: %#v", info)
	}
}
