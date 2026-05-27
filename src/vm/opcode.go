package vm

//go:generate stringer -type=OpCode

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

	OP_ADD_ASSIGN_LOCAL
	OP_SUB_ASSIGN_LOCAL

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
	OP_OBJECT_IN

	OP_CALL_DIRECT
	OP_CALL_DIRECT_SUB_CONST

	OP_GET_PROPERTY_SAFE
	OP_METHOD_CALL_SAFE

	OP_COALESCE_JUMP

	OP_EQ
	OP_NEQ
	OP_LT
	OP_GT
	OP_LTE
	OP_GTE

	OP_NEGATE

	OP_TYPEOF
	OP_SPAWN
	OP_DEFER
	OP_AWAIT
	OP_LOCK_MUTEX
	OP_UNLOCK_MUTEX

	OP_AND
	OP_OR

	OP_JUMP
	OP_JUMP_IF_FALSE
	OP_JUMP_IF_TRUE
	OP_JUMP_LOCAL_GE_CONST
	OP_JUMP_LOCAL_GE_LOCAL
	OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO
	OP_JUMP_LOCAL_GT_CONST
	OP_JUMP_LOCAL_GT_LOCAL

	OP_LOAD_LOCAL_0
	OP_LOAD_LOCAL_1
	OP_LOAD_LOCAL_2
	OP_LOAD_LOCAL_3
	OP_METHOD_CALL_LOCAL_0
	OP_METHOD_CALL_LOCAL_1
	OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO
	OP_ARRAY_LEN_LOCAL
	OP_ARRAY_GET_LOCAL
	OP_ARRAY_PUSH_LOCAL
	OP_ARRAY_PUSH_LOCAL_MUL_CONST
	OP_GET_PROPERTY_LOCAL
	OP_MUL_LOCAL_CONST

	OP_ADD_PROPERTY_LOCAL_LOCAL
	OP_ADD_LOCAL_LOCAL_STORE
	OP_ADD_LOCAL_CONST
	OP_ADD_LOCAL_LOCAL

	OP_SUB_LOCAL_LOCAL
	OP_MUL_LOCAL_LOCAL
	OP_DIV_LOCAL_LOCAL
)

type Instruction struct {
	Op     OpCode
	Value  any
	IntArg int
	IsInt  bool
	File   string
	Line   int
	Column int
}

type Function struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	Params       []Param       `json:"params"`
	ReturnType   TypeHint      `json:"returnType"`
	Instructions []Instruction `json:"instructions"`
	LocalCount   int           `json:"localCount"`
	Captures     []CapturedVar
	Async        bool `json:"async"`
	HasDefaults  bool `json:"hasDefaults"`
	HasTypeHints bool `json:"hasTypeHints"`
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
	ID       int    `json:"id"`
	Name     string `json:"name"`
	ArgCount int    `json:"argCount"`
}

type JumpLocalGELocalInfo struct {
	LeftSlot  int
	RightSlot int
	Target    int
}

type TryInfo struct {
	CatchIP int
	Name    string
	Slot    int
	IsLocal bool
}

type JumpLocalGEConstInfo struct {
	Slot   int
	Value  int
	Target int
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

type ObjectFieldsInfo struct {
	Name string
	Copy bool
}

type ObjectInfo struct {
	Names []ObjectFieldsInfo
}

type MethodCallInfo struct {
	Method   string
	ArgCount int
}

type MethodLocalCallInfo struct {
	Method       string
	ReceiverSlot int
	ArgSlot      int
}

type ArrayLocalCallInfo struct {
	ArraySlot int
	ArgSlot   int
}

type ArrayLocalMulConstInfo struct {
	ArraySlot int
	ArgSlot   int
	Factor    int
}

type PropertyLocalInfo struct {
	Slot int
	Name string
}

type PropertyLocalAssignInfo struct {
	ObjectSlot int
	SourceSlot int
	Name       string
}

type LocalConstInfo struct {
	Slot  int
	Value int
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

type AssignLocalInfo struct {
	TargetSlot int
	SourceSlot int
}

type JumpModLocalLocalNotZeroInfo struct {
	LeftSlot  int
	RightSlot int
	Target    int
}

type JumpModLocalConstNotZeroInfo struct {
	LeftSlot int
	Right    int
	Target   int
}

type JumpLocalGTLocalInfo struct {
	SlotA  int
	SlotB  int
	Target int
}

type AddLocalLocalStoreInfo struct {
	SlotA    int
	SlotB    int
	DestSlot int
}

type JumpLocalGTConstInfo struct {
	Slot   int
	Value  int
	Target int
}

type CallDirectSubConstInfo struct {
	Slot     int
	SubValue int
	FnID     int
	FnName   string
	ArgCount int
}

type Class struct {
	Name           string
	Fields         []ClassField
	Methods        map[string]string
	Embeds         []string
	PrivateMethods map[string]bool
}

type ClassField struct {
	Constant bool
	Name     string
	Value    Value
	TypeHint TypeHint
	Private  bool
}

type EnumField struct {
	Name  string
	Value Expr
}
