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
	Members []string
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
	Path   string
	Std    bool
	Alias  string
	File   string
	Line   int
	Column int
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
	Embeds  []string
}

func (s ClassStmt) stmtNode() {}

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

type StringExpr struct {
	Value string
}

func (e StringExpr) exprNode() {}

type InstanceOfExpr struct {
	Object Expr
	Class  Expr
}

func (e InstanceOfExpr) exprNode() {}

type ArrayExpr struct {
	Elements []Expr
}

func (e ArrayExpr) exprNode() {}

type TypeOfExpr struct {
	Value Expr
}

func (e TypeOfExpr) exprNode() {}

type SpawnExpr struct {
	Function Expr
}

func (e SpawnExpr) exprNode() {}

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
	Value  Expr
	File   string
	Line   int
	Column int
}

func (s ThrowStmt) stmtNode() {}

func (s ExprStmt) stmtNode() {}

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
	Name         string   `json:"name"`
	TypeHint     TypeHint `json:"typeHint"`
	HasDefault   bool     `json:"hasDefault"`
	DefaultValue Value    `json:"-"`
}

type FunctionStmt struct {
	Name       string
	Params     []Param
	ReturnType TypeHint
	Body       []Stmt
	File       string
	Line       int
	Column     int
}

func (s FunctionStmt) stmtNode() {}

type TryCatchStmt struct {
	TryBody   []Stmt
	ErrorName string
	CatchBody []Stmt
	File      string
	Line      int
	Column    int
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
	Value int
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
	Name   string
	Args   []Expr
	File   string
	Line   int
	Column int
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
	File   string
	Line   int
	Column int
}

func (e MemberCallExpr) exprNode() {}
