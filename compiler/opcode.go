package main

type OpCode string

const (
	OP_CONST        OpCode = "CONST"
	OP_INTERPOLATE  OpCode = "INTERPOLATE"
	OP_ARRAY        OpCode = "ARRAY"
	OP_INDEX        OpCode = "INDEX"
	OP_OBJECT       OpCode = "OBJECT"
	OP_GET_PROPERTY OpCode = "GET_PROPERTY"
	OP_SET_PROPERTY OpCode = "SET_PROPERTY"

	OP_ASSIGN_LOCAL  OpCode = "ASSIGN_LOCAL"
	OP_ASSIGN_GLOBAL OpCode = "ASSIGN_GLOBAL"

	OP_LOAD_LOCAL  OpCode = "LOAD_LOCAL"
	OP_STORE_LOCAL OpCode = "STORE_LOCAL"

	OP_LOAD_GLOBAL  OpCode = "LOAD_GLOBAL"
	OP_STORE_GLOBAL OpCode = "STORE_GLOBAL"

	OP_ADD OpCode = "ADD"
	OP_SUB OpCode = "SUB"
	OP_MUL OpCode = "MUL"
	OP_DIV OpCode = "DIV"

	OP_BUILTIN_CALL OpCode = "BUILTIN_CALL"
	OP_METHOD_CALL  OpCode = "METHOD_CALL"
	OP_CALL         OpCode = "CALL"
	OP_RETURN       OpCode = "RETURN"

	OP_POP  OpCode = "POP"
	OP_HALT OpCode = "HALT"

	OP_EQ  OpCode = "EQ"
	OP_NEQ OpCode = "NEQ"
	OP_LT  OpCode = "LT"
	OP_GT  OpCode = "GT"
	OP_LTE OpCode = "LTE"
	OP_GTE OpCode = "GTE"

	OP_AND OpCode = "AND"
	OP_OR  OpCode = "OR"

	OP_JUMP          OpCode = "JUMP"
	OP_JUMP_IF_FALSE OpCode = "JUMP_IF_FALSE"
)

type Instruction struct {
	Op    OpCode
	Value any
}

type Function struct {
	Name         string
	Params       []string
	Instructions []Instruction
}

type CallInfo struct {
	Name     string
	ArgCount int
}

type ArrayInfo struct {
	Count int
}

type InterpolateInfo struct {
	Parts     []string
	ExprCount int
}

type BuiltinCallInfo struct {
	Object   string
	Method   string
	ArgCount int
}

type VariableInfo struct {
	Name     string
	Constant bool
}

type ObjectInfo struct {
	Names []string
}

type MethodCallInfo struct {
	Method   string
	ArgCount int
}

type Class struct {
	Name    string
	Methods map[string]string
}
