package main

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

type ImportStmt struct {
	Path string
}

func (s ImportStmt) stmtNode() {}

type VariableStmt struct {
	Name     string
	Value    Expr
	Constant bool
}

func (s VariableStmt) stmtNode() {}

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

type IndexExpr struct {
	Array Expr
	Index Expr
}

func (e IndexExpr) exprNode() {}

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

func (s ExprStmt) stmtNode() {}

type FunctionStmt struct {
	Name   string
	Params []string
	Body   []Stmt
}

func (s FunctionStmt) stmtNode() {}

type ReturnStmt struct {
	Value Expr
}

func (s ReturnStmt) stmtNode() {}

type NumberExpr struct {
	Value int
}

func (e NumberExpr) exprNode() {}

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

type MemberCallExpr struct {
	Object string
	Method string
	Args   []Expr
}

func (e MemberCallExpr) exprNode() {}
