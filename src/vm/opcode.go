package vm

type OpCode byte

const (
	OP_CONST OpCode = iota
	OP_INTERPOLATE
	OP_ARRAY
	OP_INDEX
	OP_SET_INDEX
	OP_OBJECT
	OP_GET_PROPERTY
	OP_SET_PROPERTY

	OP_ASSIGN_LOCAL
	OP_ASSIGN_GLOBAL

	OP_LOAD_LOCAL
	OP_STORE_LOCAL

	OP_LOAD_GLOBAL
	OP_STORE_GLOBAL

	OP_STRING_JOIN

	OP_ADD
	OP_SUB
	OP_MUL
	OP_DIV
	OP_NOT

	OP_INC_LOCAL
	OP_INC_GLOBAL

	OP_DEC_LOCAL
	OP_DEC_GLOBAL

	OP_BUILTIN_CALL
	OP_METHOD_CALL
	OP_CALL
	OP_CALL_VALUE
	OP_RETURN

	OP_CLOSURE

	OP_POP
	OP_HALT
	OP_THROW

	OP_SETUP_TRY
	OP_POP_TRY

	OP_MOD

	OP_LEN

	OP_INSTANCEOF

	OP_CALL_DIRECT

	OP_EQ
	OP_NEQ
	OP_LT
	OP_GT
	OP_LTE
	OP_GTE

	OP_NEGATE

	OP_TYPEOF
	OP_SPAWN

	OP_AND
	OP_OR

	OP_JUMP
	OP_JUMP_IF_FALSE
)

type Instruction struct {
	Op     OpCode
	Value  any
	File   string
	Line   int
	Column int
}

type Function struct {
	Name         string
	Params       []Param  `json:"params"`
	ReturnType   TypeHint `json:"returnType"`
	Instructions []Instruction
	LocalCount   int
	Captures     []CapturedVar
}

type CapturedVar struct {
	Name      string
	OuterSlot int
	InnerSlot int
}

type CallInfo struct {
	Name     string
	ArgCount int
}

type DirectCallInfo struct {
	Name     string
	ArgCount int
}

type TryInfo struct {
	CatchIP int
	Name    string
	Slot    int
	IsLocal bool
}

type ArrayInfo struct {
	Count int
}

type InterpolateInfo struct {
	Parts     []string
	ExprCount int
}

type ClosureInfo struct {
	Name     string
	Captures []CapturedVar
}

type BuiltinCallInfo struct {
	Object   string
	Method   string
	ArgCount int
}

type VariableInfo struct {
	Name     string
	Slot     int
	Constant bool
	TypeHint TypeHint `json:"typeHint"`
}

type ObjectInfo struct {
	Names []string
}

type MethodCallInfo struct {
	Method   string
	ArgCount int
}

type IncrementInfo struct {
	Slot        int
	Name        string
	IntAmount   int
	FloatAmount float64
	IsFloat     bool
}

type DecrementInfo struct {
	Slot        int
	Name        string
	IntAmount   int
	FloatAmount float64
	IsFloat     bool
}

type Class struct {
	Name    string
	Methods map[string]string
	Embeds  []string
}
