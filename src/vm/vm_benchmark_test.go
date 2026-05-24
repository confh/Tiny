package vm

import "testing"

func benchmarkProgram() ([]Instruction, map[string]Function, map[string]Class) {
	fn := Function{
		Name:       "sumLoop",
		Params:     []Param{{Name: "n"}},
		LocalCount: 3,
		Instructions: []Instruction{
			{Op: OP_CONST, Value: 0},
			{Op: OP_STORE_LOCAL, Value: VariableInfo{Name: "sum", Slot: 1}},
			{Op: OP_CONST, Value: 0},
			{Op: OP_STORE_LOCAL, Value: VariableInfo{Name: "i", Slot: 2}},
			{Op: OP_LOAD_LOCAL, Value: 2},
			{Op: OP_LOAD_LOCAL, Value: 0},
			{Op: OP_LT},
			{Op: OP_JUMP_IF_FALSE, Value: 17},
			{Op: OP_LOAD_LOCAL, Value: 1},
			{Op: OP_LOAD_LOCAL, Value: 2},
			{Op: OP_ADD},
			{Op: OP_ASSIGN_LOCAL, Value: 1},
			{Op: OP_LOAD_LOCAL, Value: 2},
			{Op: OP_CONST, Value: 1},
			{Op: OP_ADD},
			{Op: OP_ASSIGN_LOCAL, Value: 2},
			{Op: OP_JUMP, Value: 4},
			{Op: OP_LOAD_LOCAL, Value: 1},
			{Op: OP_RETURN},
		},
	}

	functions := map[string]Function{"sumLoop": fn}
	main := []Instruction{
		{Op: OP_CONST, Value: 10000},
		{Op: OP_CALL_DIRECT, Value: DirectCallInfo{Name: "sumLoop", ArgCount: 1}},
		{Op: OP_HALT},
	}

	main = OptimizeBytecode(main)
	fn.Instructions = OptimizeBytecode(fn.Instructions)
	functions["sumLoop"] = fn

	return main, functions, map[string]Class{}
}

func BenchmarkNumericLoopVM(b *testing.B) {
	main, functions, classes := benchmarkProgram()

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		machine := NewVM(main, functions, classes)
		machine.Run()
	}
}

func BenchmarkNativeArrayMethods(b *testing.B) {
	machine := NewVM(nil, nil, nil)
	array := &ArrayValue{Elements: []Value{1, 2, 3, 4, 5}}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		machine.callOneArgNativeMethod("get", array, 2)
		machine.popFast()
		machine.callZeroArgNativeMethod("length", array)
		machine.popFast()
	}
}
