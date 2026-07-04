package vm

type EmbedType = byte

type SourcePosition struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type SourceRange struct {
	Start SourcePosition `json:"start"`
	End   SourcePosition `json:"end"`
}

const (
	EmbedText EmbedType = iota
	EmbedBytes
	EmbedFolder
)

type Stmt interface {
	stmtNode()
}

type Expr interface {
	exprNode()
}

type Program struct {
	Statements []Stmt
}

type AssignStmt struct {
	Name   string
	Value  Expr
	File   string
	Line   int
	Column int
}

func (s AssignStmt) stmtNode() {}

type NamespaceStmt struct {
	Name       string
	Statements []Stmt
	File       string
	Line       int
	Column     int
}

func (s NamespaceStmt) stmtNode() {}

type EnumStmt struct {
	Name    string
	Members []EnumField
	File    string
	Line    int
	Column  int
}

func (s EnumStmt) stmtNode() {}

type BreakStmt struct{}

func (s BreakStmt) stmtNode() {}

type ExportStmt struct {
	Inner  Stmt
	File   string
	Line   int
	Column int
}

func (s ExportStmt) stmtNode() {}

type ContinueStmt struct{}

func (s ContinueStmt) stmtNode() {}

type ForStmt struct {
	Init      Stmt
	Condition Expr
	Update    Stmt
	Body      []Stmt
	File      string
	Line      int
	Column    int
}

func (s ForStmt) stmtNode() {}

type PropertyAssignStmt struct {
	Object Expr
	Name   string
	Value  Expr
	File   string
	Line   int
	Column int
}

func (s PropertyAssignStmt) stmtNode() {}

type ImportStmt struct {
	Path          string
	Std           bool
	Plugin        bool
	Library       bool
	TypeOnly      bool
	TypeNamespace string
	Alias         string
	File          string
	Line          int
	Column        int
	Range         SourceRange
	PathRange     SourceRange
	AliasRange    SourceRange
}

func (s ImportStmt) stmtNode() {}

type VariableStmt struct {
	Name     string
	Value    Expr
	Constant bool
	TypeHint TypeHint
	File     string
	Line     int
	Column   int
}

func (s VariableStmt) stmtNode() {}

type ForInStmt struct {
	ItemName  string
	IndexName string
	Iterable  Expr
	Body      []Stmt
	File      string
	Line      int
	Column    int
}

func (s ForInStmt) stmtNode() {}

type MatchCase struct {
	Values   []Expr
	Value    Expr
	Guard    Expr
	BindName string
	Body     []Stmt
}

type MatchStmt struct {
	Value   Expr
	Cases   []MatchCase
	Default []Stmt
	File    string
	Line    int
	Column  int
}

func (s MatchStmt) stmtNode() {}

type ClassStmt struct {
	Name           string
	TypeParameters []string
	Implements     []string
	Methods        []FunctionStmt
	Embeds         []string
	Locals         []*Cell
	Fields         []FieldStmt
	File           string
	Line           int
	Column         int
}

func (s ClassStmt) stmtNode() {}

type FieldStmt struct {
	Name     string
	Value    Expr
	TypeHint TypeHint
	Constant bool
	Private  bool
	File     string
	Line     int
	Column   int
}

func (s FieldStmt) stmtNode() {}

type WhileStmt struct {
	Condition Expr
	Body      []Stmt
	File      string
	Line      int
	Column    int
}

func (s WhileStmt) stmtNode() {}

type IfStmt struct {
	Condition Expr
	ThenBody  []Stmt
	ElseBody  []Stmt
	File      string
	Line      int
	Column    int
}

func (s IfStmt) stmtNode() {}

type LockStmt struct {
	Mutex  Expr
	Block  []Stmt
	File   string
	Line   int
	Column int
}

func (s LockStmt) stmtNode() {}

type StringExpr struct {
	Value string
}

func (e StringExpr) exprNode() {}

type InstanceOfExpr struct {
	Object Expr
	Class  Expr
}

func (e InstanceOfExpr) exprNode() {}

type ObjectInExpr struct {
	Key    Expr
	Object Expr
	File   string
	Line   int
	Column int
}

func (e ObjectInExpr) exprNode() {}

type ArrayExpr struct {
	Elements []Expr
}

func (e ArrayExpr) exprNode() {}

type TypeOfExpr struct {
	Value Expr
}

func (e TypeOfExpr) exprNode() {}

type SpawnExpr struct {
	Args     []Expr
	Function Expr
}

func (e SpawnExpr) exprNode() {}

type AwaitExpr struct {
	Task   Expr
	File   string
	Line   int
	Column int
}

func (e AwaitExpr) exprNode() {}

type DeferExpr struct {
	Function Expr
	File     string
	Line     int
	Column   int
}

func (e DeferExpr) exprNode() {}

type ThisExpr struct {
	File   string
	Line   int
	Column int
}

func (e ThisExpr) exprNode() {}

type IndexExpr struct {
	Object Expr
	Index  Expr
}

func (e IndexExpr) exprNode() {}

type FunctionExpr struct {
	Params     []Param
	ReturnType TypeHint
	Body       []Stmt
	File       string
	Line       int
	Column     int
}

func (e FunctionExpr) exprNode() {}

type TernaryExpr struct {
	Condition Expr
	ThenExpr  Expr
	ElseExpr  Expr
}

func (e TernaryExpr) exprNode() {}

type InterpolatedStringPart struct {
	Text   string
	Expr   Expr
	IsExpr bool
}

type InterpolatedStringExpr struct {
	Parts []InterpolatedStringPart
}

func (e InterpolatedStringExpr) exprNode() {}

type BoolExpr struct {
	Value bool
}

func (e BoolExpr) exprNode() {}

type UnaryExpr struct {
	Op    TokenType
	Right Expr
}

func (e UnaryExpr) exprNode() {}

type ObjectField struct {
	Name      string
	Value     Expr
	Range     SourceRange
	NameRange SourceRange

	Copy    IdentExpr
	HasCopy bool
}

type ObjectExpr struct {
	Fields []ObjectField
	Range  SourceRange
}

func (e ObjectExpr) exprNode() {}

type PropertyExpr struct {
	Object Expr
	Name   string
	Safe   bool
	File   string
	Line   int
	Column int
	Range  SourceRange
}

func (e PropertyExpr) exprNode() {}

type NullishCoalescingExpr struct {
	Left   Expr
	Right  Expr
	File   string
	Line   int
	Column int
}

func (e NullishCoalescingExpr) exprNode() {}

type NullExpr struct{}

func (e NullExpr) exprNode() {}

type ExprStmt struct {
	Value Expr
}

type ThrowStmt struct {
	Value  Expr
	File   string
	Line   int
	Column int
}

func (s ThrowStmt) stmtNode() {}

func (s ExprStmt) stmtNode() {}

type InterfaceStmt struct {
	Name           string
	TypeParameters []string
	Extends        []string
	Fields         map[string]TypeHint
	File           string
	Line           int
	Column         int
	Range          SourceRange
	NameRange      SourceRange
	FieldRanges    map[string]SourceRange
	FieldNameRanges map[string]SourceRange
	FieldTypeRanges map[string]SourceRange
}

func (s InterfaceStmt) stmtNode() {}

type EmbedStmt struct {
	Kind         EmbedType
	Name         string
	EmbeddedPath string
	Constant     bool
	TypeHint     TypeHint
	File         string
	Line         int
	Column       int
}

func (s EmbedStmt) stmtNode() {}

type IndexAssignStmt struct {
	Object Expr
	Index  Expr
	Value  Expr
	File   string
	Line   int
	Column int
}

func (s IndexAssignStmt) stmtNode() {}

type Param struct {
	Name         string      `json:"name"`
	TypeHint     TypeHint    `json:"typeHint"`
	HasDefault   bool        `json:"hasDefault"`
	DefaultValue TinyValue   `json:"-"`
	Variadic     bool        `json:"variadic"`
	Range        SourceRange `json:"range,omitempty"`
	NameRange    SourceRange `json:"nameRange,omitempty"`
	TypeRange    SourceRange `json:"typeRange,omitempty"`
}

type FunctionStmt struct {
	Name           string
	TypeParameters []string
	Params         []Param
	ReturnType     TypeHint
	Body           []Stmt
	Async          bool
	Private        bool
	File           string
	Line           int
	Column         int
	Range          SourceRange
	NameRange      SourceRange
	ParamsRange    SourceRange
	ReturnTypeRange SourceRange
}

func (s FunctionStmt) stmtNode() {}

type NativeFnStmt struct {
	Name       string
	Params     []Param
	ReturnType TypeHint
	GoCode     string
	File       string
	Line       int
	Column     int
}

func (s NativeFnStmt) stmtNode() {}

type ExternalFnStmt struct {
	Name       string
	Params     []Param
	ReturnType TypeHint
	File       string
	Line       int
	Column     int
}

func (s ExternalFnStmt) stmtNode() {}

type ExternalGlobalStmt struct {
	Name   string
	Type   TypeHint
	File   string
	Line   int
	Column int
}

func (s ExternalGlobalStmt) stmtNode() {}

type TryCatchStmt struct {
	TryBody     []Stmt
	ErrorName   string
	CatchBody   []Stmt
	FinallyBody []Stmt
	File        string
	Line        int
	Column      int
}

func (s TryCatchStmt) stmtNode() {}

type ReturnStmt struct {
	Value    Expr
	HasValue bool
	File     string
	Line     int
	Column   int
}

func (s ReturnStmt) stmtNode() {}

type NumberExpr struct {
	Value  int
	File   string
	Line   int
	Column int
}

func (e NumberExpr) exprNode() {}

type IncrementStmt struct {
	Name   string
	File   string
	Line   int
	Column int
}

func (e IncrementStmt) stmtNode() {}

type DecrementStmt struct {
	Name   string
	File   string
	Line   int
	Column int
}

func (e DecrementStmt) stmtNode() {}

type FloatExpr struct {
	Value  float64
	File   string
	Line   int
	Column int
}

func (e FloatExpr) exprNode() {}

type IdentExpr struct {
	Name   string
	File   string
	Line   int
	Column int
	Range  SourceRange
}

func (e IdentExpr) exprNode() {}

type BinaryExpr struct {
	Left  Expr
	Op    TokenType
	Right Expr
}

func (e BinaryExpr) exprNode() {}

type CallExpr struct {
	Name   string
	Args   []Expr
	File   string
	Line   int
	Column int
}

func (e CallExpr) exprNode() {}

type InstantiatedExpr struct {
	Object   Expr
	TypeArgs []TypeHint
	File     string
	Line     int
	Column   int
}

func (e InstantiatedExpr) exprNode() {}

type CallValueExpr struct {
	Callee Expr
	Args   []Expr
	File   string
	Line   int
	Column int
}

func (e CallValueExpr) exprNode() {}

type MemberCallExpr struct {
	Object Expr
	Method string
	Args   []Expr
	Safe   bool
	File   string
	Line   int
	Column int
}

func (e MemberCallExpr) exprNode() {}

type SpreadExpr struct {
	Value Expr
}

func (e SpreadExpr) exprNode() {}

type DestructurePattern interface {
	destructureNode()
}

type ObjectDestructureField struct {
	Key            string
	Alias          string
	AliasIsRenamed bool
	Default        Expr
	HasDefault     bool
	Pattern        DestructurePattern
	HasNested      bool
}

type ObjectDestructurePattern struct {
	Fields    []ObjectDestructureField
	Spread    string
	HasSpread bool
}

func (o ObjectDestructurePattern) destructureNode() {}

type ArrayDestructureElement struct {
	Name      string
	Pattern   DestructurePattern
	HasNested bool
	IsSpread  bool
}

type ArrayDestructurePattern struct {
	Elements []ArrayDestructureElement
}

func (a ArrayDestructurePattern) destructureNode() {}

type DestructureStmt struct {
	Target   DestructurePattern
	Value    Expr
	Constant bool
	File     string
	Line     int
	Column   int
}

func (s DestructureStmt) stmtNode() {}

type EnumVariantExpr struct {
	EnumName string
	Variant  string
	Args     []Expr
	File     string
	Line     int
	Column   int
}

func (e EnumVariantExpr) exprNode() {}
