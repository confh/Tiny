package main

import (
	"strconv"
)

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

func containsDot(s string) bool {
	for _, ch := range s {
		if ch == '.' {
			return true
		}
	}

	return false
}

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
			langError(ErrorSyntax, "unterminated interpolation")
		}

		exprSource := input[start+2 : end]

		lexer := NewLexer(exprSource, "")
		parser := NewParser(lexer)
		expr := parser.parseExpression()

		if parser.current.Type != TOKEN_EOF {
			langError(ErrorSyntax, "unexpected tokens inside interpolation")
		}

		parts = append(parts, InterpolatedStringPart{
			Expr:   expr,
			IsExpr: true,
		})

		input = input[end+1:]
	}

	return InterpolatedStringExpr{Parts: parts}
}

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

func (p *Parser) parseOptionalTypeHint() TypeHint {
	if p.current.Type != TOKEN_COLON {
		return TypeHint{}
	}

	p.advance()

	if p.current.Type != TOKEN_IDENT {
		langErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected type name after :",
		)
	}

	name := p.current.Literal
	p.advance()

	return TypeHint{Name: name}
}

func (p *Parser) parsePossibleAssignmentStatement() Stmt {
	left := p.parseUnary()

	switch p.current.Type {
	case TOKEN_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.expect(TOKEN_SEMI)

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name:  target.Name,
				Value: value,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value:  value,
			}

		case IndexExpr:
			return IndexAssignStmt{
				Object: target.Object,
				Index:  target.Index,
				Value:  value,
			}

		default:
			langError(ErrorSyntax, "invalid assignment target")
		}
	case TOKEN_INCREMENT:
		p.advance()

		p.expect(TOKEN_SEMI)
		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1},
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1},
				},
			}

		default:
			langError(ErrorSyntax, "invalid assignment target")
		}

	case TOKEN_DECREMENT:
		p.advance()

		p.expect(TOKEN_SEMI)
		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_MINUS,
					Right: NumberExpr{Value: 1},
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_MINUS,
					Right: NumberExpr{Value: 1},
				},
			}

		default:
			langError(ErrorSyntax, "invalid assignment target")
		}

	case TOKEN_PLUS_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.expect(TOKEN_SEMI)

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_PLUS,
					Right: value,
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_PLUS,
					Right: value,
				},
			}

		default:
			langError(ErrorSyntax, "invalid += target")
		}

	case TOKEN_MINUS_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.expect(TOKEN_SEMI)

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_MINUS,
					Right: value,
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_MINUS,
					Right: value,
				},
			}

		default:
			langError(ErrorSyntax, "invalid -= target")
		}

	case TOKEN_STAR_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.expect(TOKEN_SEMI)

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_STAR,
					Right: value,
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_STAR,
					Right: value,
				},
			}

		default:
			langError(ErrorSyntax, "invalid *= target")
		}

	case TOKEN_SLASH_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.expect(TOKEN_SEMI)

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_SLASH,
					Right: value,
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_SLASH,
					Right: value,
				},
			}

		default:
			langError(ErrorSyntax, "invalid /= target")
		}
	}

	p.expect(TOKEN_SEMI)

	return ExprStmt{
		Value: left,
	}
}

func (p *Parser) parseStatement() Stmt {
	switch p.current.Type {
	case TOKEN_IDENT, TOKEN_THIS:
		return p.parsePossibleAssignmentStatement()
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
	case TOKEN_WHILE:
		return p.parseWhileStatement()
	case TOKEN_FOR:
		return p.parseForStatement()
	case TOKEN_CLASS:
		return p.parseClassStatement()
	case TOKEN_BREAK:
		return p.parseBreakStatement()
	case TOKEN_CONTINUE:
		return p.parseContinueStatement()
	case TOKEN_THROW:
		return p.parseThrowStatement()
	case TOKEN_TRY:
		return p.parseTryCatchStatement()
	case TOKEN_ENUM:
		return p.parseEnumStatement()
	case TOKEN_EXPORT:
		return p.parseExportStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseExportStatement() Stmt {
	p.expect(TOKEN_EXPORT)

	switch p.current.Type {
	case TOKEN_CONST:
		return ExportStmt{Inner: p.parseConstStatement()}

	case TOKEN_LET:
		return ExportStmt{Inner: p.parseLetStatement()}

	case TOKEN_FN:
		return ExportStmt{Inner: p.parseFunctionStatement()}

	case TOKEN_CLASS:
		return ExportStmt{Inner: p.parseClassStatement()}

	case TOKEN_ENUM:
		return ExportStmt{Inner: p.parseEnumStatement()}

	default:
		langErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected const, let, fn, class, or enum after export",
		)
	}

	return nil
}

func (p *Parser) parseTryCatchStatement() Stmt {
	p.expect(TOKEN_TRY)
	p.expect(TOKEN_LBRACE)

	tryBody := p.parseBlock()

	p.expect(TOKEN_CATCH)

	if p.current.Type != TOKEN_IDENT {
		langError(ErrorSyntax, "expected error variable name after catch")
	}

	errorName := p.current.Literal
	p.advance()

	p.expect(TOKEN_LBRACE)

	catchBody := p.parseBlock()

	return TryCatchStmt{
		TryBody:   tryBody,
		ErrorName: errorName,
		CatchBody: catchBody,
	}
}

func (p *Parser) parseThrowStatement() Stmt {
	p.expect(TOKEN_THROW)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ThrowStmt{
		Value: value,
	}
}

func (p *Parser) parseForStatement() Stmt {
	p.expect(TOKEN_FOR)

	var init Stmt

	switch p.current.Type {
	case TOKEN_LET:
		init = p.parseLetStatement()
	case TOKEN_CONST:
		init = p.parseConstStatement()
	case TOKEN_SEMI:
		p.expect(TOKEN_SEMI)
	default:
		init = p.parsePossibleAssignmentStatement()
	}

	var condition Expr

	if p.current.Type != TOKEN_SEMI {
		condition = p.parseExpression()
	} else {
		condition = BoolExpr{Value: true}
	}

	p.expect(TOKEN_SEMI)

	var update Stmt

	if p.current.Type != TOKEN_LBRACE {
		update = p.parseForUpdateStatement()
	}

	p.expect(TOKEN_LBRACE)

	body := p.parseBlock()

	return ForStmt{
		Init:      init,
		Condition: condition,
		Update:    update,
		Body:      body,
	}
}

func (p *Parser) parseForUpdateStatement() Stmt {
	left := p.parsePostfix()

	if p.current.Type == TOKEN_ASSIGN {
		p.advance()

		value := p.parseExpression()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name:  target.Name,
				Value: value,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value:  value,
			}

		default:
			langError(ErrorSyntax, "invalid assignment target")
		}
	}

	if p.current.Type == TOKEN_PLUS_ASSIGN {
		p.advance()

		value := p.parseExpression()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_PLUS,
					Right: value,
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_PLUS,
					Right: value,
				},
			}

		default:
			langError(ErrorSyntax, "invalid += target")
		}
	}

	if p.current.Type == TOKEN_INCREMENT {
		p.advance()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1},
				},
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1},
				},
			}

		default:
			langError(ErrorSyntax, "invalid increment target")
		}
	}

	return ExprStmt{
		Value: left,
	}
}

func (p *Parser) parseBreakStatement() Stmt {
	p.expect(TOKEN_BREAK)
	p.expect(TOKEN_SEMI)

	return BreakStmt{}
}

func (p *Parser) parseContinueStatement() Stmt {
	p.expect(TOKEN_CONTINUE)
	p.expect(TOKEN_SEMI)

	return ContinueStmt{}
}

func (p *Parser) parseWhileStatement() Stmt {
	p.expect(TOKEN_WHILE)

	condition := p.parseExpression()

	p.expect(TOKEN_LBRACE)

	body := p.parseBlock()

	return WhileStmt{
		Condition: condition,
		Body:      body,
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
	p.expect(TOKEN_LBRACE)

	statements := []Stmt{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		statements = append(statements, p.parseStatement())
	}

	p.expect(TOKEN_RBRACE)

	return statements
}

func (p *Parser) parseImportStatement() Stmt {
	p.expect(TOKEN_IMPORT)

	if p.current.Type == TOKEN_IDENT && p.current.Literal == "std" {
		p.advance()

		if p.current.Type != TOKEN_STRING {
			langErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected standard module name after import std",
			)
		}

		moduleName := p.current.Literal
		p.advance()

		alias := moduleName

		if p.current.Type == TOKEN_IDENT && p.current.Literal == "as" {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				langErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected alias name after as",
				)
			}

			alias = p.current.Literal
			p.advance()
		}

		p.expect(TOKEN_SEMI)

		return ImportStmt{
			Path:  moduleName,
			Std:   true,
			Alias: alias,
		}
	}

	if p.current.Type != TOKEN_STRING {
		langErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected import path",
		)
	}

	path := p.current.Literal
	p.advance()

	alias := ""

	if p.current.Type == TOKEN_IDENT && p.current.Literal == "as" {
		p.advance()

		if p.current.Type != TOKEN_IDENT {
			langErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected alias name after as",
			)
		}

		alias = p.current.Literal
		p.advance()
	}

	p.expect(TOKEN_SEMI)

	return ImportStmt{
		Path:  path,
		Std:   false,
		Alias: alias,
	}
}

func (p *Parser) parseLetStatement() Stmt {
	p.expect(TOKEN_LET)

	if p.current.Type != TOKEN_IDENT {
		langError(ErrorSyntax, "expected variable name after let")
	}

	name := p.current.Literal
	p.advance()

	typeHint := p.parseOptionalTypeHint()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: false,
		TypeHint: typeHint,
	}
}

func (p *Parser) parseConstStatement() Stmt {
	p.expect(TOKEN_CONST)

	if p.current.Type != TOKEN_IDENT {
		langError(ErrorSyntax, "expected variable name after const")
	}

	name := p.current.Literal
	p.advance()

	typeHint := p.parseOptionalTypeHint()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: true,
		TypeHint: typeHint,
	}
}

func (p *Parser) parseFunctionStatement() Stmt {
	p.expect(TOKEN_FN)

	if p.current.Type != TOKEN_IDENT {
		langErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected function name")
	}

	name := p.current.Literal
	p.advance()

	params, returnType, body := p.parseFunctionSignatureAndBody()

	return FunctionStmt{
		Name:       name,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
	}
}

func (p *Parser) parseParameterList() []Param {
	params := []Param{}

	if p.current.Type == TOKEN_RPAREN {
		return params
	}

	for {
		if p.current.Type != TOKEN_IDENT {
			langErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected parameter name",
			)
		}

		name := p.current.Literal
		p.advance()

		typeHint := TypeHint{}

		if p.current.Type == TOKEN_COLON {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				langErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected type name after :",
				)
			}

			typeHint = TypeHint{Name: p.current.Literal}
			p.advance()
		}

		params = append(params, Param{
			Name:     name,
			TypeHint: typeHint,
		})

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	return params
}

func (p *Parser) parseReturnStatement() Stmt {
	p.expect(TOKEN_RETURN)

	if p.current.Type == TOKEN_SEMI {
		p.expect(TOKEN_SEMI)

		return ReturnStmt{
			HasValue: false,
		}
	}

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ReturnStmt{
		Value:    value,
		HasValue: true,
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
	left := p.parseUnary()

	for p.current.Type == TOKEN_STAR || p.current.Type == TOKEN_SLASH || p.current.Type == TOKEN_PERCENT {
		op := p.current.Type
		p.advance()

		right := p.parseUnary()

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

	for {
		switch p.current.Type {
		case TOKEN_LPAREN:
			p.advance()

			args := p.parseArgumentList()

			p.expect(TOKEN_RPAREN)

			expr = CallValueExpr{
				Callee: expr,
				Args:   args,
			}

		case TOKEN_DOT:
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				langError(ErrorSyntax, "expected property name after dot")
			}

			name := p.current.Literal
			p.advance()

			if p.current.Type == TOKEN_LPAREN {
				p.advance()

				args := p.parseArgumentList()

				p.expect(TOKEN_RPAREN)

				expr = MemberCallExpr{
					Object: expr,
					Method: name,
					Args:   args,
				}

				continue
			}

			expr = PropertyExpr{
				Object: expr,
				Name:   name,
			}

		case TOKEN_LBRACKET:
			p.advance()

			index := p.parseExpression()

			p.expect(TOKEN_RBRACKET)

			expr = IndexExpr{
				Object: expr,
				Index:  index,
			}

		default:
			return expr
		}
	}
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

func (p *Parser) parseObjectLiteral() Expr {
	p.expect(TOKEN_LBRACE)

	var fields []ObjectField

	if p.current.Type == TOKEN_RBRACE {
		p.expect(TOKEN_RBRACE)
		return ObjectExpr{Fields: fields}
	}

	for {
		if p.current.Type != TOKEN_IDENT && p.current.Type != TOKEN_STRING {
			langError(ErrorSyntax, "expected object field name")
		}

		name := p.current.Literal
		p.advance()

		p.expect(TOKEN_COLON)

		value := p.parseExpression()

		fields = append(fields, ObjectField{
			Name:  name,
			Value: value,
		})

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	p.expect(TOKEN_RBRACE)

	return ObjectExpr{Fields: fields}
}

func (p *Parser) parseFunctionSignatureAndBody() ([]Param, TypeHint, []Stmt) {
	p.expect(TOKEN_LPAREN)

	params := p.parseParameterList()

	p.expect(TOKEN_RPAREN)

	returnType := TypeHint{}

	if p.current.Type == TOKEN_COLON {
		p.advance()

		if p.current.Type != TOKEN_IDENT {
			langErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected return type after :")
		}

		returnType = TypeHint{Name: p.current.Literal}
		p.advance()
	}

	body := p.parseBlock()

	return params, returnType, body
}

func (p *Parser) parsePrimary() Expr {
	switch p.current.Type {
	case TOKEN_NUMBER:
		literal := p.current.Literal

		if containsDot(literal) {
			value, err := strconv.ParseFloat(literal, 64)
			if err != nil {
				langError(ErrorSyntax, "invalid float: %s", literal)
			}

			p.advance()

			return FloatExpr{Value: value}
		}

		value, err := strconv.Atoi(literal)
		if err != nil {
			langError(ErrorSyntax, "invalid number: %s", literal)
		}

		p.advance()

		return NumberExpr{Value: value}

	case TOKEN_FN:
		return p.parseFunctionExpr()

	case TOKEN_LBRACKET:
		return p.parseArrayLiteral()

	case TOKEN_IDENT:
		name := p.current.Literal
		p.advance()

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

	case TOKEN_LBRACE:
		return p.parseObjectLiteral()

	case TOKEN_TRUE:
		p.advance()
		return BoolExpr{Value: true}

	case TOKEN_FALSE:
		p.advance()
		return BoolExpr{Value: false}

	case TOKEN_THIS:
		p.advance()
		return ThisExpr{}

	case TOKEN_NULL:
		p.advance()
		return NullExpr{}

	case TOKEN_UNDEFINED:
		p.advance()
		return UndefinedExpr{}

	case TOKEN_BANG:
		return p.parseUnary()

	default:
		langError(ErrorSyntax, "expected expression, got %s", p.current.Type)
		return UndefinedExpr{}
	}
}

func (p *Parser) parseEnumStatement() Stmt {
	p.expect(TOKEN_ENUM)

	if p.current.Type != TOKEN_IDENT {
		langErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected enum name",
		)
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LBRACE)

	members := []string{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		if p.current.Type != TOKEN_IDENT {
			langErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected enum member name",
			)
		}

		members = append(members, p.current.Literal)
		p.advance()

		if p.current.Type == TOKEN_COMMA {
			p.advance()
			continue
		}

		if p.current.Type != TOKEN_RBRACE {
			langErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected , or } after enum member",
			)
		}
	}

	p.expect(TOKEN_RBRACE)

	// Optional semicolon support:
	if p.current.Type == TOKEN_SEMI {
		p.advance()
	}

	return EnumStmt{
		Name:    name,
		Members: members,
	}
}

func (p *Parser) parseFunctionExpr() Expr {
	p.expect(TOKEN_FN)

	params, returnType, body := p.parseFunctionSignatureAndBody()

	return FunctionExpr{
		Params:     params,
		ReturnType: returnType,
		Body:       body,
	}
}

func (p *Parser) parseClassStatement() Stmt {
	p.expect(TOKEN_CLASS)

	if p.current.Type != TOKEN_IDENT {
		langError(ErrorSyntax, "expected class name after class")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LBRACE)

	var methods []FunctionStmt

	for p.current.Type != TOKEN_RBRACE {
		if p.current.Type == TOKEN_EOF {
			langError(ErrorSyntax, "unexpected EOF inside class body")
		}

		if p.current.Type != TOKEN_FN {
			langError(ErrorSyntax, "expected method declaration inside class")
		}

		method := p.parseFunctionStatement()

		fn, ok := method.(FunctionStmt)
		if !ok {
			langError(ErrorInternal, "expected function method")
		}

		methods = append(methods, fn)
	}

	p.expect(TOKEN_RBRACE)

	return ClassStmt{
		Name:    name,
		Methods: methods,
	}
}

func (p *Parser) parseUnary() Expr {
	if p.current.Type == TOKEN_BANG {
		op := p.current.Type
		p.advance()

		right := p.parseUnary()

		return UnaryExpr{
			Op:    op,
			Right: right,
		}
	}

	return p.parsePostfix()
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
		langErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected %s, got %s",
			tokenType,
			p.current.Type,
		)
	}

	p.advance()
}
