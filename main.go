package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// =====================
// TOKENS
// =====================

type TokenType string

const (
	TOKEN_EOF             TokenType = "EOF"
	TOKEN_IDENT           TokenType = "IDENT"
	TOKEN_NUMBER          TokenType = "NUMBER"
	TOKEN_STRING          TokenType = "STRING"
	TOKEN_BACKTICK_STRING TokenType = "BACKTICK_STRING"
	TOKEN_TRUE            TokenType = "TRUE"
	TOKEN_FALSE           TokenType = "FALSE"
	TOKEN_NULL            TokenType = "NULL"
	TOKEN_UNDEFINED       TokenType = "UNDEFINED"

	TOKEN_IMPORT TokenType = "IMPORT"
	TOKEN_LET    TokenType = "LET"
	TOKEN_CONST  TokenType = "CONST"
	TOKEN_FN     TokenType = "FN"
	TOKEN_RETURN TokenType = "RETURN"

	TOKEN_IF   TokenType = "IF"
	TOKEN_ELSE TokenType = "ELSE"

	TOKEN_AND TokenType = "AND"
	TOKEN_OR  TokenType = "OR"

	TOKEN_ASSIGN TokenType = "="
	TOKEN_PLUS   TokenType = "+"
	TOKEN_MINUS  TokenType = "-"
	TOKEN_STAR   TokenType = "*"
	TOKEN_SLASH  TokenType = "/"
	TOKEN_EQ     TokenType = "=="
	TOKEN_NEQ    TokenType = "!="
	TOKEN_LT     TokenType = "<"
	TOKEN_GT     TokenType = ">"
	TOKEN_LTE    TokenType = "<="
	TOKEN_GTE    TokenType = ">="

	TOKEN_LPAREN   TokenType = "("
	TOKEN_RPAREN   TokenType = ")"
	TOKEN_LBRACKET TokenType = "["
	TOKEN_RBRACKET TokenType = "]"
	TOKEN_LBRACE   TokenType = "{"
	TOKEN_RBRACE   TokenType = "}"
	TOKEN_COMMA    TokenType = ","
	TOKEN_SEMI     TokenType = ";"
	TOKEN_DOT      TokenType = "."
)

type Token struct {
	Type    TokenType
	Literal string
}

// =====================
// LEXER
// =====================

type Lexer struct {
	input []rune
	pos   int
}

func NewLexer(input string) *Lexer {
	return &Lexer{input: []rune(input)}
}

func (l *Lexer) peek() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}

	return l.input[l.pos+1]
}

func (l *Lexer) NextToken() Token {
	l.skipWhitespace()

	if l.pos >= len(l.input) {
		return Token{Type: TOKEN_EOF}
	}

	ch := l.input[l.pos]

	if unicode.IsLetter(ch) || ch == '_' {
		word := l.readIdentifier()

		switch word {
		case "import":
			return Token{Type: TOKEN_IMPORT, Literal: word}
		case "let":
			return Token{Type: TOKEN_LET, Literal: word}
		case "const":
			return Token{Type: TOKEN_CONST, Literal: word}
		case "fn":
			return Token{Type: TOKEN_FN, Literal: word}
		case "return":
			return Token{Type: TOKEN_RETURN, Literal: word}
		case "if":
			return Token{Type: TOKEN_IF, Literal: word}
		case "else":
			return Token{Type: TOKEN_ELSE, Literal: word}
		case "and":
			return Token{Type: TOKEN_AND, Literal: word}
		case "or":
			return Token{Type: TOKEN_OR, Literal: word}
		case "true":
			return Token{Type: TOKEN_TRUE, Literal: word}
		case "false":
			return Token{Type: TOKEN_FALSE, Literal: word}
		case "null":
			return Token{Type: TOKEN_NULL, Literal: word}
		case "undefined":
			return Token{Type: TOKEN_UNDEFINED, Literal: word}
		default:
			return Token{Type: TOKEN_IDENT, Literal: word}
		}
	}

	if unicode.IsDigit(ch) {
		num := l.readNumber()
		return Token{Type: TOKEN_NUMBER, Literal: num}
	}

	if ch == '"' {
		str := l.readString()
		return Token{Type: TOKEN_STRING, Literal: str}
	}

	if ch == '`' {
		str := l.readBacktickString()
		return Token{Type: TOKEN_BACKTICK_STRING, Literal: str}
	}

	switch ch {
	case '=':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TOKEN_EQ, Literal: "=="}
		}

		l.pos++
		return Token{Type: TOKEN_ASSIGN, Literal: "="}

	case '!':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TOKEN_NEQ, Literal: "!="}
		}

		panic("unexpected character: !")

	case '<':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TOKEN_LTE, Literal: "<="}
		}

		l.pos++
		return Token{Type: TOKEN_LT, Literal: "<"}

	case '>':
		if l.peek() == '=' {
			l.pos += 2
			return Token{Type: TOKEN_GTE, Literal: ">="}
		}

		l.pos++
		return Token{Type: TOKEN_GT, Literal: ">"}

	case '+':
		l.pos++
		return Token{Type: TOKEN_PLUS, Literal: "+"}
	case '-':
		l.pos++
		return Token{Type: TOKEN_MINUS, Literal: "-"}
	case '*':
		l.pos++
		return Token{Type: TOKEN_STAR, Literal: "*"}
	case '/':
		l.pos++
		return Token{Type: TOKEN_SLASH, Literal: "/"}
	case '(':
		l.pos++
		return Token{Type: TOKEN_LPAREN, Literal: "("}
	case ')':
		l.pos++
		return Token{Type: TOKEN_RPAREN, Literal: ")"}
	case '{':
		l.pos++
		return Token{Type: TOKEN_LBRACE, Literal: "{"}
	case '}':
		l.pos++
		return Token{Type: TOKEN_RBRACE, Literal: "}"}
	case ',':
		l.pos++
		return Token{Type: TOKEN_COMMA, Literal: ","}
	case ';':
		l.pos++
		return Token{Type: TOKEN_SEMI, Literal: ";"}
	case '.':
		l.pos++
		return Token{Type: TOKEN_DOT, Literal: "."}
	case '[':
		l.pos++
		return Token{Type: TOKEN_LBRACKET, Literal: "["}
	case ']':
		l.pos++
		return Token{Type: TOKEN_RBRACKET, Literal: "]"}
	default:
		panic(fmt.Sprintf("unknown character: %q", ch))
	}
}

func (l *Lexer) skipWhitespace() {
	for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
		l.pos++
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' {
			break
		}
		l.pos++
	}

	return string(l.input[start:l.pos])
}

func (l *Lexer) readNumber() string {
	start := l.pos

	for l.pos < len(l.input) && unicode.IsDigit(l.input[l.pos]) {
		l.pos++
	}

	return string(l.input[start:l.pos])
}

func (l *Lexer) readString() string {
	l.pos++ // skip opening "

	start := l.pos

	for l.pos < len(l.input) && l.input[l.pos] != '"' {
		l.pos++
	}

	if l.pos >= len(l.input) {
		panic("unterminated string")
	}

	value := string(l.input[start:l.pos])

	l.pos++ // skip closing "

	return value
}

func (l *Lexer) readBacktickString() string {
	l.pos++ // skip opening `

	start := l.pos

	for l.pos < len(l.input) && l.input[l.pos] != '`' {
		l.pos++
	}

	if l.pos >= len(l.input) {
		panic("unterminated interpolated string")
	}

	value := string(l.input[start:l.pos])

	l.pos++ // skip closing `

	return value
}

// =====================
// AST
// =====================

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

// =====================
// PARSER
// =====================

type Parser struct {
	lexer *Lexer

	current Token
	next    Token
}

func NewParser(lexer *Lexer) *Parser {
	p := &Parser{lexer: lexer}

	p.advance()
	p.advance()

	return p
}

func (p *Parser) advance() {
	p.current = p.next
	p.next = p.lexer.NextToken()
}

func (p *Parser) ParseProgram() Program {
	var statements []Stmt

	for p.current.Type != TOKEN_EOF {
		stmt := p.parseStatement()
		statements = append(statements, stmt)
	}

	return Program{Statements: statements}
}

func (p *Parser) parseAssignStatement() Stmt {
	if p.current.Type != TOKEN_IDENT {
		panic("expected variable name")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return AssignStmt{
		Name:  name,
		Value: value,
	}
}

func (p *Parser) parseStatement() Stmt {
	switch p.current.Type {
	case TOKEN_IDENT:
		if p.next.Type == TOKEN_ASSIGN {
			return p.parseAssignStatement()
		}
		return p.parseExpressionStatement()
	case TOKEN_IMPORT:
		return p.parseImportStatement()
	case TOKEN_LET:
		return p.parseLetStatement()
	case TOKEN_CONST:
		return p.parseConstStatement()
	case TOKEN_FN:
		return p.parseFunctionStatement()
	case TOKEN_RETURN:
		return p.parseReturnStatement()
	case TOKEN_IF:
		return p.parseIfStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseIfStatement() Stmt {
	p.expect(TOKEN_IF)

	condition := p.parseExpression()

	p.expect(TOKEN_LBRACE)

	thenBody := p.parseBlock()

	var elseBody []Stmt

	if p.current.Type == TOKEN_ELSE {
		p.expect(TOKEN_ELSE)
		p.expect(TOKEN_LBRACE)

		elseBody = p.parseBlock()
	}

	return IfStmt{
		Condition: condition,
		ThenBody:  thenBody,
		ElseBody:  elseBody,
	}
}

func (p *Parser) parseBlock() []Stmt {
	var statements []Stmt

	for p.current.Type != TOKEN_RBRACE {
		if p.current.Type == TOKEN_EOF {
			panic("unexpected EOF inside block")
		}

		statements = append(statements, p.parseStatement())
	}

	p.expect(TOKEN_RBRACE)

	return statements
}

func (p *Parser) parseImportStatement() Stmt {
	p.expect(TOKEN_IMPORT)

	if p.current.Type != TOKEN_STRING {
		panic("expected string path after import")
	}

	path := p.current.Literal
	p.advance()

	p.expect(TOKEN_SEMI)

	return ImportStmt{Path: path}
}

func (p *Parser) parseLetStatement() Stmt {
	p.expect(TOKEN_LET)

	if p.current.Type != TOKEN_IDENT {
		panic("expected variable name after let")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: false,
	}
}

func (p *Parser) parseConstStatement() Stmt {
	p.expect(TOKEN_CONST)

	if p.current.Type != TOKEN_IDENT {
		panic("expected variable name after const")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: true,
	}
}

func (p *Parser) parseFunctionStatement() Stmt {
	p.expect(TOKEN_FN)

	if p.current.Type != TOKEN_IDENT {
		panic("expected function name after fn")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LPAREN)

	params := p.parseParameterList()

	p.expect(TOKEN_RPAREN)
	p.expect(TOKEN_LBRACE)

	var body []Stmt

	for p.current.Type != TOKEN_RBRACE {
		if p.current.Type == TOKEN_EOF {
			panic("unexpected EOF inside function body")
		}

		body = append(body, p.parseStatement())
	}

	p.expect(TOKEN_RBRACE)

	return FunctionStmt{
		Name:   name,
		Params: params,
		Body:   body,
	}
}

func (p *Parser) parseParameterList() []string {
	var params []string

	if p.current.Type == TOKEN_RPAREN {
		return params
	}

	for {
		if p.current.Type != TOKEN_IDENT {
			panic("expected parameter name")
		}

		params = append(params, p.current.Literal)
		p.advance()

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	return params
}

func (p *Parser) parseReturnStatement() Stmt {
	p.expect(TOKEN_RETURN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ReturnStmt{
		Value: value,
	}
}

func (p *Parser) parseExpressionStatement() Stmt {
	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ExprStmt{
		Value: value,
	}
}

func (p *Parser) parseExpression() Expr {
	return p.parseOr()
}

func (p *Parser) parseOr() Expr {
	left := p.parseAnd()

	for p.current.Type == TOKEN_OR {
		op := p.current.Type
		p.advance()

		right := p.parseAnd()

		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left
}

func (p *Parser) parseAnd() Expr {
	left := p.parseComparison()

	for p.current.Type == TOKEN_AND {
		op := p.current.Type
		p.advance()

		right := p.parseComparison()

		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left
}

func (p *Parser) parseComparison() Expr {
	left := p.parseAddSub()

	for p.current.Type == TOKEN_EQ ||
		p.current.Type == TOKEN_NEQ ||
		p.current.Type == TOKEN_LT ||
		p.current.Type == TOKEN_GT ||
		p.current.Type == TOKEN_LTE ||
		p.current.Type == TOKEN_GTE {

		op := p.current.Type
		p.advance()

		right := p.parseAddSub()

		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left
}

func (p *Parser) parseAddSub() Expr {
	left := p.parseMulDiv()

	for p.current.Type == TOKEN_PLUS || p.current.Type == TOKEN_MINUS {
		op := p.current.Type
		p.advance()

		right := p.parseMulDiv()

		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left
}

func (p *Parser) parseMulDiv() Expr {
	left := p.parsePostfix()

	for p.current.Type == TOKEN_STAR || p.current.Type == TOKEN_SLASH {
		op := p.current.Type
		p.advance()

		right := p.parsePostfix()

		left = BinaryExpr{
			Left:  left,
			Op:    op,
			Right: right,
		}
	}

	return left
}

func (p *Parser) parsePostfix() Expr {
	expr := p.parsePrimary()

	for p.current.Type == TOKEN_LBRACKET {
		p.advance()

		index := p.parseExpression()

		p.expect(TOKEN_RBRACKET)

		expr = IndexExpr{
			Array: expr,
			Index: index,
		}
	}

	return expr
}

func (p *Parser) parseArrayLiteral() Expr {
	p.expect(TOKEN_LBRACKET)

	var elements []Expr

	if p.current.Type == TOKEN_RBRACKET {
		p.expect(TOKEN_RBRACKET)
		return ArrayExpr{Elements: elements}
	}

	for {
		element := p.parseExpression()
		elements = append(elements, element)

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	p.expect(TOKEN_RBRACKET)

	return ArrayExpr{Elements: elements}
}

func (p *Parser) parsePrimary() Expr {
	switch p.current.Type {
	case TOKEN_NUMBER:
		value, err := strconv.Atoi(p.current.Literal)
		if err != nil {
			panic(err)
		}

		p.advance()

		return NumberExpr{Value: value}

	case TOKEN_LBRACKET:
		return p.parseArrayLiteral()

	case TOKEN_IDENT:
		name := p.current.Literal
		p.advance()

		// Normal function call: add(1, 2)
		if p.current.Type == TOKEN_LPAREN {
			p.advance()

			args := p.parseArgumentList()

			p.expect(TOKEN_RPAREN)

			return CallExpr{
				Name: name,
				Args: args,
			}
		}

		// Member call: core.halt()
		if p.current.Type == TOKEN_DOT {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				panic("expected method name after dot")
			}

			method := p.current.Literal
			p.advance()

			p.expect(TOKEN_LPAREN)

			args := p.parseArgumentList()

			p.expect(TOKEN_RPAREN)

			return MemberCallExpr{
				Object: name,
				Method: method,
				Args:   args,
			}
		}

		return IdentExpr{Name: name}

	case TOKEN_LPAREN:
		p.advance()

		expr := p.parseExpression()

		p.expect(TOKEN_RPAREN)

		return expr

	case TOKEN_STRING:
		value := p.current.Literal
		p.advance()

		return StringExpr{Value: value}

	case TOKEN_BACKTICK_STRING:
		value := p.current.Literal
		p.advance()

		return parseInterpolatedString(value)

	case TOKEN_TRUE:
		p.advance()
		return BoolExpr{Value: true}

	case TOKEN_FALSE:
		p.advance()
		return BoolExpr{Value: false}

	case TOKEN_NULL:
		p.advance()
		return NullExpr{}

	case TOKEN_UNDEFINED:
		p.advance()
		return UndefinedExpr{}

	default:
		panic(fmt.Sprintf("expected expression, got %s", p.current.Type))
	}
}

func (p *Parser) parseArgumentList() []Expr {
	var args []Expr

	if p.current.Type == TOKEN_RPAREN {
		return args
	}

	for {
		arg := p.parseExpression()
		args = append(args, arg)

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	return args
}

func (p *Parser) expect(tokenType TokenType) {
	if p.current.Type != tokenType {
		panic(fmt.Sprintf("expected %s, got %s", tokenType, p.current.Type))
	}

	p.advance()
}

// =====================
// IMPORT LOADER
// =====================

func LoadProgram(path string) Program {
	visited := map[string]bool{}
	statements := loadFile(path, visited)

	return Program{Statements: statements}
}

func loadFile(path string, visited map[string]bool) []Stmt {
	absPath, err := filepath.Abs(path)
	if err != nil {
		panic(err)
	}

	if visited[absPath] {
		return nil
	}

	visited[absPath] = true

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		panic(fmt.Sprintf("failed to read file %s: %v", path, err))
	}

	lexer := NewLexer(string(bytes))
	parser := NewParser(lexer)
	program := parser.ParseProgram()

	var result []Stmt
	dir := filepath.Dir(absPath)

	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case ImportStmt:
			importPath := filepath.Join(dir, s.Path)
			importedStatements := loadFile(importPath, visited)
			result = append(result, importedStatements...)

		default:
			result = append(result, stmt)
		}
	}

	return result
}

// =====================
// BYTECODE
// =====================

type OpCode string

const (
	OP_CONST       OpCode = "CONST"
	OP_INTERPOLATE OpCode = "INTERPOLATE"
	OP_ARRAY       OpCode = "ARRAY"
	OP_INDEX       OpCode = "INDEX"

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

// =====================
// COMPILER
// =====================

type Compiler struct {
	mainInstructions []Instruction
	functions        map[string]Function

	currentInstructions *[]Instruction

	locals          map[string]bool
	localConstants  map[string]bool
	globalConstants map[string]bool
}

func NewCompiler() *Compiler {
	c := &Compiler{
		mainInstructions: []Instruction{},
		functions:        map[string]Function{},
		locals:           map[string]bool{},
		localConstants:   map[string]bool{},
		globalConstants:  map[string]bool{},
	}

	c.currentInstructions = &c.mainInstructions

	return c
}

func (c *Compiler) CompileProgram(program Program) ([]Instruction, map[string]Function) {
	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	return c.mainInstructions, c.functions
}

func (c *Compiler) compileStatement(stmt Stmt) {
	switch s := stmt.(type) {
	case VariableStmt:
		c.compileExpr(s.Value)

		if c.isInsideFunction() {
			c.locals[s.Name] = true
			c.localConstants[s.Name] = s.Constant
			c.emit(OP_STORE_LOCAL, VariableInfo{
				Name:     s.Name,
				Constant: s.Constant,
			})
		} else {
			c.globalConstants[s.Name] = s.Constant
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:     s.Name,
				Constant: s.Constant,
			})
		}

	case AssignStmt:
		c.compileExpr(s.Value)

		if c.isInsideFunction() && c.locals[s.Name] {
			c.emit(OP_ASSIGN_LOCAL, s.Name)
		} else {
			c.emit(OP_ASSIGN_GLOBAL, s.Name)
		}

	case ExprStmt:
		c.compileExpr(s.Value)
		c.emit(OP_POP, nil)

	case FunctionStmt:
		c.compileFunction(s)

	case ReturnStmt:
		c.compileExpr(s.Value)
		c.emit(OP_RETURN, nil)

	case ImportStmt:
		panic("imports should be resolved before compiling")

	case IfStmt:
		c.compileIfStatement(s)

	default:
		panic("unknown statement")
	}
}

func (c *Compiler) compileIfStatement(stmt IfStmt) {
	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	for _, bodyStmt := range stmt.ThenBody {
		c.compileStatement(bodyStmt)
	}

	if len(stmt.ElseBody) > 0 {
		jumpOverElseIndex := c.emitJump(OP_JUMP)

		c.patchJump(jumpIfFalseIndex)

		for _, bodyStmt := range stmt.ElseBody {
			c.compileStatement(bodyStmt)
		}

		c.patchJump(jumpOverElseIndex)
	} else {
		c.patchJump(jumpIfFalseIndex)
	}
}

func (c *Compiler) compileFunction(stmt FunctionStmt) {
	if _, exists := c.functions[stmt.Name]; exists {
		panic(fmt.Sprintf("function already defined: %s", stmt.Name))
	}

	oldInstructions := c.currentInstructions
	oldLocals := c.locals
	oldLocalConstants := c.localConstants

	functionInstructions := []Instruction{}
	c.currentInstructions = &functionInstructions
	c.locals = map[string]bool{}
	c.localConstants = map[string]bool{}

	for _, param := range stmt.Params {
		c.locals[param] = true
		c.localConstants[param] = false
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, 0)
	c.emit(OP_RETURN, nil)

	c.functions[stmt.Name] = Function{
		Name:         stmt.Name,
		Params:       stmt.Params,
		Instructions: functionInstructions,
	}

	c.currentInstructions = oldInstructions
	c.locals = oldLocals
	c.localConstants = oldLocalConstants
}

func (c *Compiler) compileExpr(expr Expr) {
	switch e := expr.(type) {
	case StringExpr:
		c.emit(OP_CONST, e.Value)

	case InterpolatedStringExpr:
		textParts := []string{}
		exprCount := 0

		textParts = append(textParts, "")

		for _, part := range e.Parts {
			if part.IsExpr {
				c.compileExpr(part.Expr)
				exprCount++
				textParts = append(textParts, "")
			} else {
				textParts[len(textParts)-1] += part.Text
			}
		}

		c.emit(OP_INTERPOLATE, InterpolateInfo{
			Parts:     textParts,
			ExprCount: exprCount,
		})

	case BoolExpr:
		c.emit(OP_CONST, e.Value)

	case NullExpr:
		c.emit(OP_CONST, NullValue{})

	case UndefinedExpr:
		c.emit(OP_CONST, UndefinedValue{})

	case ArrayExpr:
		for _, element := range e.Elements {
			c.compileExpr(element)
		}

		c.emit(OP_ARRAY, ArrayInfo{
			Count: len(e.Elements),
		})

	case IndexExpr:
		c.compileExpr(e.Array)
		c.compileExpr(e.Index)
		c.emit(OP_INDEX, nil)

	case NumberExpr:
		c.emit(OP_CONST, e.Value)

	case IdentExpr:
		if c.locals[e.Name] {
			c.emit(OP_LOAD_LOCAL, e.Name)
		} else {
			c.emit(OP_LOAD_GLOBAL, e.Name)
		}

	case BinaryExpr:
		c.compileExpr(e.Left)
		c.compileExpr(e.Right)

		switch e.Op {
		case TOKEN_PLUS:
			c.emit(OP_ADD, nil)
		case TOKEN_MINUS:
			c.emit(OP_SUB, nil)
		case TOKEN_STAR:
			c.emit(OP_MUL, nil)
		case TOKEN_SLASH:
			c.emit(OP_DIV, nil)

		case TOKEN_EQ:
			c.emit(OP_EQ, nil)
		case TOKEN_NEQ:
			c.emit(OP_NEQ, nil)
		case TOKEN_LT:
			c.emit(OP_LT, nil)
		case TOKEN_GT:
			c.emit(OP_GT, nil)
		case TOKEN_LTE:
			c.emit(OP_LTE, nil)
		case TOKEN_GTE:
			c.emit(OP_GTE, nil)
		case TOKEN_AND:
			c.emit(OP_AND, nil)
		case TOKEN_OR:
			c.emit(OP_OR, nil)

		default:
			panic("unknown binary operator")
		}

	case CallExpr:
		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.emit(OP_CALL, CallInfo{
			Name:     e.Name,
			ArgCount: len(e.Args),
		})

	case MemberCallExpr:
		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
			Object:   e.Object,
			Method:   e.Method,
			ArgCount: len(e.Args),
		})

	default:
		panic("unknown expression")
	}
}

func (c *Compiler) emit(op OpCode, value any) {
	*c.currentInstructions = append(*c.currentInstructions, Instruction{
		Op:    op,
		Value: value,
	})
}

func (c *Compiler) isInsideFunction() bool {
	return c.currentInstructions != &c.mainInstructions
}

func (c *Compiler) emitJump(op OpCode) int {
	c.emit(op, -1)
	return len(*c.currentInstructions) - 1
}

func (c *Compiler) patchJump(index int) {
	(*c.currentInstructions)[index].Value = len(*c.currentInstructions)
}

func asInt(value Value) int {
	switch n := value.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	default:
		panic(fmt.Sprintf("expected number, got %T", value))
	}
}

func valueToString(value Value) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case bool:
		if v {
			return "true"
		}
		return "false"

	case ArrayValue:
		parts := make([]string, len(v))

		for i, item := range v {
			parts[i] = valueToString(item)
		}

		return "[" + strings.Join(parts, ", ") + "]"
	case NullValue:
		return "null"
	case UndefinedValue:
		return "undefined"
	case nil:
		return "nil"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asString(value Value) string {
	stringValue, ok := value.(string)
	if !ok {
		panic(fmt.Sprintf("expected string, got %T", value))
	}

	return stringValue
}

func asBool(value Value) bool {
	boolean, ok := value.(bool)
	if !ok {
		panic(fmt.Sprintf("expected bool, got %T", value))
	}

	return boolean
}

func isTruthy(value Value) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case string:
		return v != ""
	case NullValue:
		return false
	case UndefinedValue:
		return false
	default:
		return value != nil
	}
}

func valuesEqual(a Value, b Value) bool {
	switch left := a.(type) {
	case int:
		right, ok := b.(int)
		return ok && left == right

	case string:
		right, ok := b.(string)
		return ok && left == right

	case bool:
		right, ok := b.(bool)
		return ok && left == right

	case NullValue:
		_, ok := b.(NullValue)
		return ok

	case UndefinedValue:
		_, ok := b.(UndefinedValue)
		return ok

	default:
		return a == b
	}
}

// =====================
// VM
// =====================

type NullValue struct{}

type UndefinedValue struct{}

type ArrayValue []Value

type Value any

type Frame struct {
	function     Function
	ip           int
	locals       map[string]Value
	constants    map[string]bool
	instructions []Instruction
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	functions        map[string]Function

	ip int

	stack           []Value
	globals         map[string]Value
	globalConstants map[string]bool

	frames []Frame
}

func NewVM(mainInstructions []Instruction, functions map[string]Function) *VM {
	return &VM{
		start:            time.Now().UnixMilli(),
		mainInstructions: mainInstructions,
		functions:        functions,
		globals:          map[string]Value{},
		globalConstants:  map[string]bool{},
	}
}

func (vm *VM) callBuiltin(object string, method string, argCount int) {
	if object != "core" {
		panic(fmt.Sprintf("unknown builtin module: %s", object))
	}

	switch method {
	case "len":
		if argCount != 1 {
			panic("core.len expects 1 argument")
		}

		value := vm.pop()

		switch n := value.(type) {
		case string:
			vm.push(len(n))
		case ArrayValue:
			vm.push(len(n))
		default:
			panic("argument does not have a length.")
		}
	case "clock":
		if argCount != 0 {
			panic("core.clock expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli() - vm.start)
	case "time":
		if argCount != 0 {
			panic("core.time expects 0 arguments")
		}

		vm.push(time.Now().UnixMilli())
	case "sleep":
		if argCount != 1 {
			panic("core.sleep expects 1 argument")
		}

		time.Sleep(time.Duration(asInt(vm.pop())) * time.Millisecond)

		vm.push(0)
	case "print":
		args := vm.popArgs(argCount)

		for _, arg := range args {
			fmt.Print(valueToString(arg))
		}

		vm.push(0)

	case "println":
		args := vm.popArgs(argCount)

		for i, arg := range args {
			if i > 0 {
				fmt.Print(" ")
			}

			fmt.Print(valueToString(arg))
		}

		fmt.Println()

		vm.push(0)
	case "input":
		if argCount != 1 {
			panic("core.input expects 1 argument")
		}

		reader := bufio.NewReader(os.Stdin)
		fmt.Print(vm.pop())

		input, _ := reader.ReadString('\n')

		vm.push(input)
	case "close":
		if argCount != 0 {
			panic("core.close expects 0 arguments")
		}

		os.Exit(0)

	case "exit":
		if argCount != 1 {
			panic("core.exit expects 1 argument")
		}

		os.Exit(asInt(vm.pop()))
	case "halt":
		if argCount != 0 {
			panic("core.halt expects 0 arguments")
		}

		fmt.Println("Press Enter to exit...")
		reader := bufio.NewReader(os.Stdin)
		_, _ = reader.ReadString('\n')

		// Builtin calls are expressions, so push a dummy return value.
		vm.push(0)

	default:
		panic(fmt.Sprintf("unknown core function: %s", method))
	}
}

func (vm *VM) Run() {
	for {
		instructions := vm.currentInstructions()
		ip := vm.currentIP()

		if ip < 0 || ip >= len(instructions) {
			panic("instruction pointer out of range")
		}

		instr := instructions[ip]
		vm.incrementIP()

		switch instr.Op {
		case OP_CONST:
			vm.push(instr.Value)

		case OP_LOAD_GLOBAL:
			name := instr.Value.(string)

			value, ok := vm.globals[name]
			if !ok {
				panic(fmt.Sprintf("undefined global variable: %s", name))
			}

			vm.push(value)

		case OP_STORE_GLOBAL:
			info := instr.Value.(VariableInfo)
			value := vm.pop()

			vm.globals[info.Name] = value
			vm.globalConstants[info.Name] = info.Constant

		case OP_LOAD_LOCAL:
			name := instr.Value.(string)

			frame := vm.currentFrame()

			value, ok := frame.locals[name]
			if !ok {
				panic(fmt.Sprintf("undefined local variable: %s", name))
			}

			vm.push(value)

		case OP_STORE_LOCAL:
			info := instr.Value.(VariableInfo)
			value := vm.pop()

			frame := vm.currentFrame()
			frame.locals[info.Name] = value
			frame.constants[info.Name] = info.Constant

		case OP_ASSIGN_GLOBAL:
			name := instr.Value.(string)
			value := vm.pop()

			_, exists := vm.globals[name]
			if !exists {
				panic(fmt.Sprintf("cannot assign to undefined variable: %s", name))
			}

			if vm.globalConstants[name] {
				panic(fmt.Sprintf("cannot assign to constant: %s", name))
			}

			vm.globals[name] = value

		case OP_ASSIGN_LOCAL:
			name := instr.Value.(string)
			value := vm.pop()

			frame := vm.currentFrame()

			_, exists := frame.locals[name]
			if !exists {
				panic(fmt.Sprintf("cannot assign to undefined local variable: %s", name))
			}

			if frame.constants[name] {
				panic(fmt.Sprintf("cannot assign to constant: %s", name))
			}

			frame.locals[name] = value

		case OP_ADD:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left + right)

		case OP_SUB:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left - right)

		case OP_MUL:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left * right)

		case OP_DIV:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left / right)

		case OP_EQ:
			right := vm.pop()
			left := vm.pop()
			vm.push(valuesEqual(left, right))

		case OP_NEQ:
			right := vm.pop()
			left := vm.pop()
			vm.push(!valuesEqual(left, right))

		case OP_LT:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left < right)

		case OP_GT:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left > right)

		case OP_LTE:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left <= right)

		case OP_GTE:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left >= right)

		case OP_AND:
			right := vm.pop()
			left := vm.pop()
			vm.push(isTruthy(left) && isTruthy(right))

		case OP_OR:
			right := vm.pop()
			left := vm.pop()
			vm.push(isTruthy(left) || isTruthy(right))

		case OP_JUMP:
			target := instr.Value.(int)
			vm.setIP(target)

		case OP_JUMP_IF_FALSE:
			target := instr.Value.(int)
			condition := vm.pop()

			if !isTruthy(condition) {
				vm.setIP(target)
			}

		case OP_CALL:
			info := instr.Value.(CallInfo)
			vm.callFunction(info.Name, info.ArgCount)

		case OP_BUILTIN_CALL:
			info := instr.Value.(BuiltinCallInfo)
			vm.callBuiltin(info.Object, info.Method, info.ArgCount)

		case OP_ARRAY:
			info := instr.Value.(ArrayInfo)

			elements := make([]Value, info.Count)

			for i := info.Count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}

			vm.push(ArrayValue(elements))

		case OP_INDEX:
			index := asInt(vm.pop())
			arrayValue := vm.pop()

			array, ok := arrayValue.(ArrayValue)
			if !ok {
				panic(fmt.Sprintf("expected array, got %T", arrayValue))
			}

			if index < 0 || index >= len(array) {
				panic(fmt.Sprintf("array index out of range: %d", index))
			}

			vm.push(array[index])

		case OP_RETURN:
			returnValue := vm.pop()

			if len(vm.frames) == 0 {
				panic("return used outside of function")
			}

			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.push(returnValue)

		case OP_POP:
			vm.pop()

		case OP_INTERPOLATE:
			info := instr.Value.(InterpolateInfo)

			values := make([]Value, info.ExprCount)

			for i := info.ExprCount - 1; i >= 0; i-- {
				values[i] = vm.pop()
			}

			result := ""

			for i := 0; i < info.ExprCount; i++ {
				result += info.Parts[i]
				result += valueToString(values[i])
			}

			result += info.Parts[len(info.Parts)-1]

			vm.push(result)

		case OP_HALT:
			return

		default:
			panic(fmt.Sprintf("unknown opcode: %s", instr.Op))
		}
	}
}

func (vm *VM) setIP(value int) {
	if len(vm.frames) == 0 {
		vm.ip = value
		return
	}

	vm.frames[len(vm.frames)-1].ip = value
}

func (vm *VM) callFunction(name string, argCount int) {
	fn, ok := vm.functions[name]
	if !ok {
		panic(fmt.Sprintf("undefined function: %s", name))
	}

	if len(fn.Params) != argCount {
		panic(fmt.Sprintf(
			"function %s expects %d arguments, got %d",
			name,
			len(fn.Params),
			argCount,
		))
	}

	locals := map[string]Value{}

	for i := argCount - 1; i >= 0; i-- {
		paramName := fn.Params[i]
		locals[paramName] = vm.pop()
	}

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    map[string]bool{},
		instructions: fn.Instructions,
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) currentInstructions() []Instruction {
	if len(vm.frames) == 0 {
		return vm.mainInstructions
	}

	return vm.frames[len(vm.frames)-1].instructions
}

func (vm *VM) currentIP() int {
	if len(vm.frames) == 0 {
		return vm.ip
	}

	return vm.frames[len(vm.frames)-1].ip
}

func (vm *VM) incrementIP() {
	if len(vm.frames) == 0 {
		vm.ip++
		return
	}

	vm.frames[len(vm.frames)-1].ip++
}

func (vm *VM) currentFrame() *Frame {
	if len(vm.frames) == 0 {
		panic("no current function frame")
	}

	return &vm.frames[len(vm.frames)-1]
}

func (vm *VM) popArgs(argCount int) []Value {
	args := make([]Value, argCount)

	for i := argCount - 1; i >= 0; i-- {
		args[i] = vm.pop()
	}

	return args
}

func (vm *VM) push(value Value) {
	vm.stack = append(vm.stack, value)
}

func (vm *VM) pop() Value {
	if len(vm.stack) == 0 {
		panic("stack underflow")
	}

	value := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]

	return value
}

// =====================
// MAIN
// =====================

func parseInterpolatedString(input string) Expr {
	var parts []InterpolatedStringPart

	for len(input) > 0 {
		start := findInterpolationStart(input)

		if start == -1 {
			if input != "" {
				parts = append(parts, InterpolatedStringPart{
					Text: input,
				})
			}
			break
		}

		if start > 0 {
			parts = append(parts, InterpolatedStringPart{
				Text: input[:start],
			})
		}

		end := findClosingBrace(input, start+2)
		if end == -1 {
			panic("unterminated interpolation")
		}

		exprSource := input[start+2 : end]

		lexer := NewLexer(exprSource)
		parser := NewParser(lexer)
		expr := parser.parseExpression()

		if parser.current.Type != TOKEN_EOF {
			panic("unexpected tokens inside interpolation")
		}

		parts = append(parts, InterpolatedStringPart{
			Expr:   expr,
			IsExpr: true,
		})

		input = input[end+1:]
	}

	return InterpolatedStringExpr{Parts: parts}
}

func findInterpolationStart(input string) int {
	for i := 0; i < len(input)-1; i++ {
		if input[i] == '$' && input[i+1] == '{' {
			return i
		}
	}

	return -1
}

func findClosingBrace(input string, start int) int {
	depth := 0

	for i := start; i < len(input); i++ {
		switch input[i] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return i
			}
			depth--
		}
	}

	return -1
}

func main() {
	debug := flag.Bool("debug", false, "show bytecode debug output")

	flag.Parse()

	entryFile := "main.tiny"

	if flag.NArg() >= 1 {
		entryFile = flag.Arg(0)
	}

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions := compiler.CompileProgram(program)

	if *debug {
		fmt.Println("Main bytecode:")
		for i, instr := range mainBytecode {
			fmt.Printf("%03d: %-13s %v\n", i, instr.Op, instr.Value)
		}

		fmt.Println("\nFunctions:")
		for name, fn := range functions {
			fmt.Println("fn", name, fn.Params)
			for i, instr := range fn.Instructions {
				fmt.Printf("  %03d: %-13s %v\n", i, instr.Op, instr.Value)
			}
		}

		fmt.Println("\nOutput:")
	}

	vm := NewVM(mainBytecode, functions)
	vm.Run()
}
