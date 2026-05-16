package vm

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
	Name  string
	Value Expr
}

func (s AssignStmt) stmtNode() {}

type NamespaceStmt struct {
	Name       string
	Statements []Stmt
}

func (s NamespaceStmt) stmtNode() {}

type EnumStmt struct {
	Name    string
	Members []string
}

func (s EnumStmt) stmtNode() {}

type BreakStmt struct{}

func (s BreakStmt) stmtNode() {}

type ExportStmt struct {
	Inner Stmt
}

func (s ExportStmt) stmtNode() {}

type ContinueStmt struct{}

func (s ContinueStmt) stmtNode() {}

type ForStmt struct {
	Init      Stmt
	Condition Expr
	Update    Stmt
	Body      []Stmt
}

func (s ForStmt) stmtNode() {}

type PropertyAssignStmt struct {
	Object Expr
	Name   string
	Value  Expr
}

func (s PropertyAssignStmt) stmtNode() {}

type ImportStmt struct {
	Path  string
	Std   bool
	Alias string
}

func (s ImportStmt) stmtNode() {}

type VariableStmt struct {
	Name     string
	Value    Expr
	Constant bool
	TypeHint TypeHint
}

func (s VariableStmt) stmtNode() {}

type ForInStmt struct {
	ItemName  string
	IndexName string
	Iterable  Expr
	Body      []Stmt
}

func (s ForInStmt) stmtNode() {}

type MatchCase struct {
	Value Expr
	Body  []Stmt
}

type MatchStmt struct {
	Value   Expr
	Cases   []MatchCase
	Default []Stmt
}

func (s MatchStmt) stmtNode() {}

type ClassStmt struct {
	Name    string
	Methods []FunctionStmt
}

func (s ClassStmt) stmtNode() {}

type WhileStmt struct {
	Condition Expr
	Body      []Stmt
}

func (s WhileStmt) stmtNode() {}

type IfStmt struct {
	Condition Expr
	ThenBody  []Stmt
	ElseBody  []Stmt
}

func (s IfStmt) stmtNode() {}

type StringExpr struct {
	Value string
}

func (e StringExpr) exprNode() {}

type ArrayExpr struct {
	Elements []Expr
}

func (e ArrayExpr) exprNode() {}

type ThisExpr struct{}

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
	Name  string
	Value Expr
}

type ObjectExpr struct {
	Fields []ObjectField
}

func (e ObjectExpr) exprNode() {}

type PropertyExpr struct {
	Object Expr
	Name   string
}

func (e PropertyExpr) exprNode() {}

type NullExpr struct{}

func (e NullExpr) exprNode() {}

type UndefinedExpr struct{}

func (e UndefinedExpr) exprNode() {}

type ExprStmt struct {
	Value Expr
}

type ThrowStmt struct {
	Value Expr
}

func (s ThrowStmt) stmtNode() {}

func (s ExprStmt) stmtNode() {}

type IndexAssignStmt struct {
	Object Expr
	Index  Expr
	Value  Expr
}

func (s IndexAssignStmt) stmtNode() {}

type Param struct {
	Name     string   `json:"name"`
	TypeHint TypeHint `json:"typeHint"`
}

type FunctionStmt struct {
	Name       string
	Params     []Param
	ReturnType TypeHint
	Body       []Stmt
}

func (s FunctionStmt) stmtNode() {}

type TryCatchStmt struct {
	TryBody   []Stmt
	ErrorName string
	CatchBody []Stmt
}

func (s TryCatchStmt) stmtNode() {}

type ReturnStmt struct {
	Value    Expr
	HasValue bool
}

func (s ReturnStmt) stmtNode() {}

type NumberExpr struct {
	Value int
}

func (e NumberExpr) exprNode() {}

type IncrementStmt struct {
	Name string
}

func (e IncrementStmt) stmtNode() {}

type FloatExpr struct {
	Value float64
}

func (e FloatExpr) exprNode() {}

type IdentExpr struct {
	Name string
}

func (e IdentExpr) exprNode() {}

type BinaryExpr struct {
	Left  Expr
	Op    TokenType
	Right Expr
}

func (e BinaryExpr) exprNode() {}

type CallExpr struct {
	Name string
	Args []Expr
}

func (e CallExpr) exprNode() {}

type CallValueExpr struct {
	Callee Expr
	Args   []Expr
}

func (e CallValueExpr) exprNode() {}

type MemberCallExpr struct {
	Object Expr
	Method string
	Args   []Expr
}

func (e MemberCallExpr) exprNode() {}
