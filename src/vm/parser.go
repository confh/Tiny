package vm

import (
	"strconv"
	"strings"

	. "language.com/src/tinyerrors"
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
			LangError(ErrorSyntax, "unterminated interpolation")
		}

		exprSource := input[start+2 : end]

		lexer := NewLexer(exprSource, "")
		parser := NewParser(lexer)
		expr := parser.parseExpression()

		if parser.current.Type != TOKEN_EOF {
			LangErrorAt(
				ErrorSyntax,
				parser.current.File,
				parser.current.Line,
				parser.current.Column,
				"unexpected tokens inside interpolation",
			)
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
		LangErrorAt(
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
				Name:   target.Name,
				Value:  value,
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value:  value,
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		case IndexExpr:
			return IndexAssignStmt{
				Object: target.Object,
				Index:  target.Index,
				Value:  value,
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid assignment target",
			)
		}
	case TOKEN_INCREMENT:
		p.advance()

		p.expect(TOKEN_SEMI)
		switch target := left.(type) {
		case IdentExpr:
			return IncrementStmt{
				Name: target.Name,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left: target,
					Op:   TOKEN_PLUS,
					Right: NumberExpr{
						Value:  1,
						File:   p.current.File,
						Line:   p.current.Line,
						Column: p.current.Column,
					},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid assignment target",
			)
		}

	case TOKEN_DECREMENT:
		p.advance()

		p.expect(TOKEN_SEMI)
		switch target := left.(type) {
		case IdentExpr:
			return DecrementStmt{
				Name: target.Name,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_MINUS,
					Right: NumberExpr{Value: 1, File: p.current.File, Line: p.current.Line, Column: p.current.Column},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid assignment target",
			)
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid += target",
			)
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid -= target",
			)
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid *= target",
			)
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid /= target",
			)
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
	case TOKEN_MATCH:
		return p.parseMatchStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseMatchStatement() Stmt {
	p.expect(TOKEN_MATCH)

	value := p.parseExpression()

	p.expect(TOKEN_LBRACE)

	cases := []MatchCase{}
	var defaultBody []Stmt
	hasDefault := false

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		// default case: _ { ... }
		if p.current.Type == TOKEN_IDENT && p.current.Literal == "_" {
			if hasDefault {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"duplicate default case in match",
				)
			}

			p.advance()

			defaultBody = p.parseBlock()
			hasDefault = true
			continue
		}

		caseValue := p.parseExpression()

		for _, c := range cases {
			if c.Value == caseValue {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"duplicate case value in match",
				)
			}
		}

		body := p.parseBlock()

		cases = append(cases, MatchCase{
			Value: caseValue,
			Body:  body,
		})
	}

	p.expect(TOKEN_RBRACE)

	return MatchStmt{
		Value:   value,
		Cases:   cases,
		Default: defaultBody,
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
		LangErrorAt(
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

	tryBody := p.parseBlock()

	p.expect(TOKEN_CATCH)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected error variable name after catch",
		)
	}

	errorName := p.current.Literal
	p.advance()

	catchBody := p.parseBlock()

	statement := TryCatchStmt{
		TryBody:   tryBody,
		ErrorName: errorName,
		CatchBody: catchBody,
	}

	if p.current.Type == TOKEN_FINALLY {
		p.expect(TOKEN_FINALLY)
		finallyBody := p.parseBlock()
		statement.FinallyBody = finallyBody
	}

	return statement
}

func (p *Parser) parseThrowStatement() Stmt {
	p.expect(TOKEN_THROW)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ThrowStmt{
		Value:  value,
		Line:   p.current.Line,
		Column: p.current.Column,
		File:   p.current.File,
	}
}

func (p *Parser) parseForStatement() Stmt {
	p.expect(TOKEN_FOR)

	if p.current.Type == TOKEN_IDENT {
		itemName := p.current.Literal
		p.advance()

		indexName := ""

		if p.current.Type == TOKEN_COMMA {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected index variable name after ,",
				)
			}

			indexName = p.current.Literal
			p.advance()
		}

		if p.current.Type == TOKEN_IN {
			p.advance()

			iterable := p.parseExpression()
			body := p.parseBlock()

			return ForInStmt{
				ItemName:  itemName,
				IndexName: indexName,
				Iterable:  iterable,
				Body:      body,
			}
		}

		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected in after for variable",
		)
	}

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

	body := p.parseBlock()

	return ForStmt{
		Init:      init,
		Condition: condition,
		Update:    update,
		Body:      body,
		Line:      p.current.Line,
		Column:    p.current.Column,
		File:      p.current.File,
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
				Name:   target.Name,
				Value:  value,
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value:  value,
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid assignment target",
			)
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
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
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid += target",
			)
		}
	}

	switch p.current.Type {
	case TOKEN_INCREMENT:
		p.advance()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1, File: p.current.File, Line: p.current.Line, Column: p.current.Column},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_PLUS,
					Right: NumberExpr{Value: 1, File: p.current.File, Line: p.current.Line, Column: p.current.Column},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid increment target",
			)
		}
	case TOKEN_DECREMENT:
		p.advance()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left:  IdentExpr{Name: target.Name},
					Op:    TOKEN_MINUS,
					Right: NumberExpr{Value: 1, File: p.current.File, Line: p.current.Line, Column: p.current.Column},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		case PropertyExpr:
			return PropertyAssignStmt{
				Object: target.Object,
				Name:   target.Name,
				Value: BinaryExpr{
					Left:  target,
					Op:    TOKEN_MINUS,
					Right: NumberExpr{Value: 1, File: p.current.File, Line: p.current.Line, Column: p.current.Column},
				},
				Line:   p.current.Line,
				Column: p.current.Column,
				File:   p.current.File,
			}

		default:
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid increment target",
			)
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

	body := p.parseBlock()

	return WhileStmt{
		Condition: condition,
		Body:      body,
		Line:      p.current.Line,
		Column:    p.current.Column,
		File:      p.current.File,
	}
}

func (p *Parser) parseIfStatement() Stmt {
	p.expect(TOKEN_IF)

	condition := p.parseExpression()

	thenBody := p.parseBlock()

	var elseBody []Stmt

	if p.current.Type == TOKEN_ELSE {
		p.expect(TOKEN_ELSE)

		elseBody = p.parseBlock()
	}

	return IfStmt{
		Condition: condition,
		ThenBody:  thenBody,
		ElseBody:  elseBody,
		Line:      p.current.Line,
		Column:    p.current.Column,
		File:      p.current.File,
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
			LangErrorAt(
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
				LangErrorAt(
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
	} else if p.current.Type == TOKEN_IDENT && p.current.Literal == "plugin" {
		p.advance()

		if p.current.Type != TOKEN_STRING {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected plugin path after import plugin",
			)
		}

		pluginPath := p.current.Literal
		p.advance()

		alias := ""

		if p.current.Type == TOKEN_IDENT && p.current.Literal == "as" {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected alias name after as",
				)
			}

			alias = p.current.Literal
			p.advance()
		} else {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected alias name",
			)
		}

		p.expect(TOKEN_SEMI)

		return ImportStmt{
			Path:   pluginPath,
			Plugin: true,
			Std:    false,
			Alias:  alias,
		}
	}

	if p.current.Type != TOKEN_STRING {
		LangErrorAt(
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
			LangErrorAt(
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
		Path:   path,
		Plugin: false,
		Std:    false,
		Alias:  alias,
	}
}

func (p *Parser) parseFieldStatement() Stmt {
	p.expect(TOKEN_FIELD)

	constant := false
	private := false

	if p.current.Type == TOKEN_PRIVATE {
		p.expect(TOKEN_PRIVATE)
		private = true
	} else if p.current.Type == TOKEN_PUBLIC {
		p.expect(TOKEN_PUBLIC)
	}

	if p.current.Type == TOKEN_CONST {
		p.expect(TOKEN_CONST)
		constant = true
	}

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected variable name after field",
		)
	}

	name := p.current.Literal
	p.advance()

	typeHint := p.parseOptionalTypeHint()

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return FieldStmt{
		Name:     name,
		Value:    value,
		TypeHint: typeHint,
		Constant: constant,
		Private:  private,
		File:     p.current.File,
		Line:     p.current.Line,
		Column:   p.current.Column,
	}
}

func (p *Parser) parseLetStatement() Stmt {
	p.expect(TOKEN_LET)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected variable name after let",
		)
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
		Line:     p.current.Line,
		Column:   p.current.Column,
		File:     p.current.File,
	}
}

func (p *Parser) parseDefaultParamValue() Value {
	switch p.current.Type {
	case TOKEN_STRING:
		value := p.current.Literal
		p.advance()
		return value

	case TOKEN_NUMBER:
		text := p.current.Literal
		p.advance()

		if strings.Contains(text, ".") {
			number, err := strconv.ParseFloat(text, 64)
			if err != nil {
				LangError(ErrorSyntax, "invalid number default: %s", text)
			}
			return number
		}

		number, err := strconv.Atoi(text)
		if err != nil {
			LangError(ErrorSyntax, "invalid number default: %s", text)
		}

		return number

	case TOKEN_TRUE:
		p.advance()
		return true

	case TOKEN_FALSE:
		p.advance()
		return false

	case TOKEN_NULL:
		p.advance()
		return NullValue{}

	case TOKEN_UNDEFINED:
		p.advance()
		return UndefinedValue{}

	case TOKEN_MINUS:
		p.advance()

		if p.current.Type != TOKEN_NUMBER {
			LangError(ErrorSyntax, "expected number after - in default argument")
		}

		text := p.current.Literal
		p.advance()

		if strings.Contains(text, ".") {
			number, err := strconv.ParseFloat(text, 64)
			if err != nil {
				LangError(ErrorSyntax, "invalid number default: -%s", text)
			}
			return -number
		}

		number, err := strconv.Atoi(text)
		if err != nil {
			LangError(ErrorSyntax, "invalid number default: -%s", text)
		}

		return -number

	default:
		LangError(
			ErrorSyntax,
			"default arguments currently only support constant values",
		)
		return UndefinedValue{}
	}
}

func (p *Parser) parseConstStatement() Stmt {
	p.expect(TOKEN_CONST)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected variable name after const",
		)
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
		Line:     p.current.Line,
		Column:   p.current.Column,
		File:     p.current.File,
	}
}

func (p *Parser) parseFunctionStatement() Stmt {
	p.expect(TOKEN_FN)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected function name")
	}

	name := p.current.Literal
	p.advance()

	params, returnType, body := p.parseFunctionSignatureAndBody()

	return FunctionStmt{
		Name:       name,
		Params:     params,
		ReturnType: returnType,
		Body:       body,
		Line:       p.current.Line,
		Column:     p.current.Column,
		File:       p.current.File,
	}
}

func (p *Parser) parseParameterList() []Param {
	params := []Param{}

	if p.current.Type == TOKEN_RPAREN {
		return params
	}

	for {
		variadic := false
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			p.expect(TOKEN_DOT_DOT_DOT)
			variadic = true
		}

		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(
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
			if variadic {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"variadic params cannot have types",
				)
			} else {
				p.advance()

				if p.current.Type != TOKEN_IDENT {
					LangErrorAt(
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
		}

		param := Param{
			Name:     name,
			TypeHint: typeHint,
			Variadic: variadic,
		}

		if p.current.Type == TOKEN_ASSIGN {
			if variadic {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"variadic params cannot default values",
				)
			} else {
				p.advance()

				defaultValue := p.parseDefaultParamValue()

				param.HasDefault = true
				param.DefaultValue = defaultValue
			}
		}

		params = append(params, param)

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	variadicArgsNumber := 0

	for i, param := range params {
		if param.Variadic {
			variadicArgsNumber++
			if i != len(params)-1 {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"variadic parameter must be the last parameter",
				)
			}
			if variadicArgsNumber > 1 {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"variadic parameter must be declared once at max",
				)
			}
		}
	}

	seenDefault := false

	for _, param := range params {
		if param.HasDefault {
			seenDefault = true
			continue
		}

		if seenDefault {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"required parameter cannot come after default parameter",
			)
		}
	}

	return params
}

func (vm *VM) applyDefaultArgs(fn Function, args []Value, paramOffset int, callableName string) []Value {
	params := fn.Params[paramOffset:]

	minArgs := 0

	for _, param := range params {
		if param.Variadic {
			continue
		}
		if !param.HasDefault {
			minArgs++
		}
	}

	maxArgs := len(params)

	if len(args) < minArgs || len(args) > maxArgs {
		LangError(
			ErrorRuntime,
			"%s expects %d to %d arguments, got %d",
			callableName,
			minArgs,
			maxArgs,
			len(args),
		)
	}

	finalArgs := make([]Value, maxArgs)

	copy(finalArgs, args)

	for i := len(args); i < maxArgs; i++ {
		param := params[i]

		if !param.HasDefault {
			LangError(ErrorRuntime, "%s missing argument: %s", callableName, param.Name)
		}

		finalArgs[i] = cloneDefaultValue(param.DefaultValue)
	}

	return finalArgs
}

func cloneDefaultValue(value Value) Value {
	switch v := value.(type) {
	case *ArrayValue:
		copied := make([]Value, len(v.Elements))
		copy(copied, v.Elements)
		return &ArrayValue{Elements: copied}

	case ObjectValue:
		copied := ObjectValue{}
		for key, item := range v {
			copied[key] = item
		}
		return copied

	default:
		return value
	}
}

func (p *Parser) parseReturnStatement() Stmt {
	p.expect(TOKEN_RETURN)

	if p.current.Type == TOKEN_SEMI {
		p.expect(TOKEN_SEMI)

		return ReturnStmt{
			HasValue: false,
			Line:     p.current.Line,
			Column:   p.current.Column,
			File:     p.current.File,
		}
	}

	value := p.parseExpression()

	p.expect(TOKEN_SEMI)

	return ReturnStmt{
		Value:    value,
		HasValue: true,
		Line:     p.current.Line,
		Column:   p.current.Column,
		File:     p.current.File,
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
	return p.parseTernary()
}

func (p *Parser) parseTernary() Expr {
	condition := p.parseOr()

	if p.current.Type != TOKEN_QUESTION {
		return condition
	}

	p.advance()

	thenExpr := p.parseExpression()

	p.expect(TOKEN_COLON)

	elseExpr := p.parseExpression()

	return TernaryExpr{
		Condition: condition,
		ThenExpr:  thenExpr,
		ElseExpr:  elseExpr,
	}
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
		p.current.Type == TOKEN_GTE ||
		p.current.Type == TOKEN_INSTANCEOF ||
		p.current.Type == TOKEN_IN {

		op := p.current.Type
		p.advance()

		right := p.parseAddSub()

		switch op {
		case TOKEN_INSTANCEOF:
			left = InstanceOfExpr{
				Object: left,
				Class:  right,
			}

		case TOKEN_IN:
			left = ObjectInExpr{
				Key:    right,
				Object: left,
				File:   p.current.File,
				Line:   p.current.Line,
				Column: p.current.Column,
			}

		default:
			left = BinaryExpr{
				Left:  left,
				Op:    op,
				Right: right,
			}
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
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected property name after dot",
				)
			}

			name := p.current.Literal
			file := p.current.File
			line := p.current.Line
			column := p.current.Column

			p.advance()

			if p.current.Type == TOKEN_LPAREN {
				p.advance()

				args := p.parseArgumentList()

				p.expect(TOKEN_RPAREN)

				expr = MemberCallExpr{
					Object: expr,
					Method: name,
					Args:   args,
					Line:   line,
					Column: column,
					File:   file,
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
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected object field name",
			)
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
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected return type after :")
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
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"invalid float: %s", literal,
				)
			}

			p.advance()

			return FloatExpr{Value: value}
		}

		value, err := strconv.Atoi(literal)
		if err != nil {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"invalid number: %s", literal,
			)
		}

		p.advance()

		return NumberExpr{Value: value, File: p.current.File, Line: p.current.Line, Column: p.current.Column}

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
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected expression, got %s", p.current.Type,
		)
		return UndefinedExpr{}
	}
}

func (p *Parser) parseEnumStatement() Stmt {
	p.expect(TOKEN_ENUM)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
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
			LangErrorAt(
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
			LangErrorAt(
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
		File:       p.current.File,
		Line:       p.current.Line,
		Column:     p.current.Column,
	}
}

func (p *Parser) parseClassStatement() Stmt {
	p.expect(TOKEN_CLASS)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected class name after class",
		)
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LBRACE)

	var methods []FunctionStmt
	embeds := []string{}
	fields := []FieldStmt{}

	for p.current.Type != TOKEN_RBRACE {
		if p.current.Type == TOKEN_EOF {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"unexpected EOF inside class body",
			)
		}

		if p.current.Type == TOKEN_FIELD {
			field, ok := p.parseFieldStatement().(FieldStmt)
			if !ok {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected field",
				)
			}

			fields = append(fields, field)
			continue
		}

		if p.current.Type == TOKEN_EMBED {
			p.advance()

			if p.current.Type != TOKEN_IDENT {
				LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected embedded field name")
			}

			embeds = append(embeds, p.current.Literal)
			p.advance()

			p.expect(TOKEN_SEMI)
			continue
		}

		functionPrivate := false

		if p.current.Type == TOKEN_PRIVATE {
			p.expect(TOKEN_PRIVATE)
			functionPrivate = true
		} else if p.current.Type == TOKEN_PUBLIC {
			p.expect(TOKEN_PUBLIC)
		}

		if p.current.Type != TOKEN_FN {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected declared variable, method or embed in class")
		}

		method := p.parseFunctionStatement()

		fn, ok := method.(FunctionStmt)
		if !ok {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected function method",
			)
		}

		fn.Private = functionPrivate

		methods = append(methods, fn)
	}

	p.expect(TOKEN_RBRACE)

	return ClassStmt{
		Name:    name,
		Methods: methods,
		Embeds:  embeds,
		Fields:  fields,
		File:    p.current.File,
		Line:    p.current.Line,
		Column:  p.current.Column,
	}
}

func (p *Parser) parseUnary() Expr {
	switch p.current.Type {
	case TOKEN_MINUS, TOKEN_BANG:
		op := p.current.Type
		p.advance()

		right := p.parseUnary()

		return UnaryExpr{
			Op:    op,
			Right: right,
		}

	case TOKEN_TYPEOF:
		p.advance()

		value := p.parseUnary()

		return TypeOfExpr{
			Value: value,
		}
	case TOKEN_SPAWN:
		p.advance()

		fn := p.parseUnary()

		_, ok := fn.(FunctionExpr)
		if !ok {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected function after spawn, got %s",
				p.current.Type,
			)
		}

		return SpawnExpr{
			Function: fn,
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
		LangErrorAt(
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
