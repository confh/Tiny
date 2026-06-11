package vm

import (
	"os"
	"path/filepath"
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
		lexer.EnableASI = false
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

func (p *Parser) posOfToken(tok Token) int {
	line := 1
	column := 1

	for i := 0; i < len(p.lexer.input); i++ {
		if line == tok.Line && column == tok.Column {
			return i
		}

		if p.lexer.input[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return len(p.lexer.input)
}

type Parser struct {
	lexer *Lexer

	current Token
	next    Token

	deferCountStack []int
	inTernaryThen   bool
}

func NewParser(lexer *Lexer) *Parser {
	p := &Parser{lexer: lexer}

	p.advance()
	p.advance()

	return p
}

func (p *Parser) compareTwoConst(expr1 Expr, expr2 Expr) bool {
	switch expr1.(type) {
	case StringExpr:
		_, ok := expr2.(StringExpr)
		return ok

	case NumberExpr:
		_, ok := expr2.(NumberExpr)
		return ok

	case BoolExpr:
		_, ok := expr2.(BoolExpr)
		return ok

	case NullExpr:
		_, ok := expr2.(NullExpr)
		return ok

	case ArrayExpr:
		_, ok := expr2.(ArrayExpr)
		return ok

	case ObjectExpr:
		_, ok := expr2.(ObjectExpr)
		return ok

	default:
		return false
	}
}

func (p *Parser) advance() {
	p.current = p.next
	p.next = p.lexer.NextToken()
}

func (p *Parser) ParseProgram() Program {
	var statements []Stmt

	for p.current.Type != TOKEN_EOF {
		if p.current.Type == TOKEN_RBRACE {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"unexpected }",
			)
		}

		stmt := p.parseStatement()
		if stmt != nil {
			statements = append(statements, stmt)
		}
	}

	return Program{Statements: statements}
}

func (p *Parser) parseTypeName() string {
	if !p.isValidType(p.current.Type) {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected type name, got %s",
			p.current.Type,
		)
	}

	name := p.current.Literal
	p.advance()

	for p.current.Type == TOKEN_DOT {
		p.advance()

		if !p.isValidType(p.current.Type) {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected type name after .",
			)
		}

		name += "." + p.current.Literal
		p.advance()
	}

	for p.current.Type == TOKEN_COLON {
		p.advance()
		name += ":" + p.parseTypeName()
	}

	return name
}

func (p *Parser) parseTypeHint(nullable bool) TypeHint {
	p.expect(TOKEN_COLON)

	types := []string{}

	for {
		types = append(types, p.parseTypeName())

		if p.current.Type != TOKEN_PIPE {
			break
		}

		p.advance()
	}

	if len(types) == 1 {
		if nullable {
			types = append(types, "null")

			return TypeHint{
				Name:  strings.Join(types, " | "),
				Types: types,
			}
		} else {
			return TypeHint{Name: types[0]}
		}

	}

	if nullable {
		types = append(types, "null")
	}

	return TypeHint{
		Name:  strings.Join(types, " | "),
		Types: types,
	}
}

func (p *Parser) parseOptionalTypeHint() TypeHint {
	if p.current.Type != TOKEN_COLON {
		return TypeHint{}
	}

	p.advance()

	types := []string{}

	for {
		types = append(types, p.parseTypeName())

		if p.current.Type != TOKEN_PIPE {
			break
		}

		p.advance()
	}

	if len(types) == 1 {
		return TypeHint{Name: types[0]}
	}

	return TypeHint{
		Name:  strings.Join(types, " | "),
		Types: types,
	}
}

func (p *Parser) isValidType(token TokenType) bool {
	return token == TOKEN_IDENT ||
		token == TOKEN_NULL
}

func (p *Parser) parsePossibleAssignmentStatement() Stmt {
	left := p.parseUnary()

	switch p.current.Type {
	case TOKEN_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

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

		p.consumeTerminator()
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

		case IndexExpr:
			return IndexAssignStmt{
				Object: target.Object,
				Index:  target.Index,
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

		p.consumeTerminator()
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

		p.consumeTerminator()

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

		p.consumeTerminator()

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

		p.consumeTerminator()

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

		p.consumeTerminator()

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

	p.consumeTerminator()

	return ExprStmt{
		Value: left,
	}
}

func (p *Parser) parseStatement() Stmt {
	for p.current.Type == TOKEN_SEMI {
		p.advance()
	}

	if p.current.Type == TOKEN_EOF || p.current.Type == TOKEN_RBRACE {
		return nil
	}

	if p.current.Type == TOKEN_IDENT && p.current.Literal == "lock" &&
		(p.next.Type == TOKEN_IDENT || p.next.Type == TOKEN_THIS || p.next.Type == TOKEN_LPAREN) {
		return p.parseLockStatement()
	}

	switch p.current.Type {
	case TOKEN_IDENT, TOKEN_THIS:
		return p.parsePossibleAssignmentStatement()
	case TOKEN_IMPORT:
		return p.parseImportStatement()
	case TOKEN_LET:
		return p.parseLetStatement()
	case TOKEN_CONST:
		return p.parseConstStatement()
	case TOKEN_NATIVE:
		return p.parseNativeFunctionStatement()
	case TOKEN_FN:
		return p.parseFunctionStatement(false)
	case TOKEN_ASYNC:
		return p.parseAsyncStmt()
	case TOKEN_RETURN:
		return p.parseReturnStatement()
	case TOKEN_INTERFACE:
		return p.parseInterfaceStatement()
	case TOKEN_EMBED_STR:
		return p.parseEmbedStrStatement()
	case TOKEN_EMBED_BIN:
		return p.parseEmbedBinStatement()
	case TOKEN_EMBED_DIR:
		return p.parseEmbedDirStatement()
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
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_MATCH)

	value := p.parseExpression()

	p.expect(TOKEN_LBRACE)

	cases := []MatchCase{}
	var defaultBody []Stmt
	hasDefault := false

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RBRACE || p.current.Type == TOKEN_EOF {
			break
		}

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

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

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
		File:    file,
		Line:    line,
		Column:  column,
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
		return ExportStmt{Inner: p.parseFunctionStatement(false)}

	case TOKEN_CLASS:
		return ExportStmt{Inner: p.parseClassStatement()}

	case TOKEN_ENUM:
		return ExportStmt{Inner: p.parseEnumStatement()}

	case TOKEN_INTERFACE:
		return ExportStmt{Inner: p.parseInterfaceStatement()}

	case TOKEN_EMBED_STR:
		return ExportStmt{Inner: p.parseEmbedStrStatement()}

	case TOKEN_EMBED_BIN:
		return ExportStmt{Inner: p.parseEmbedBinStatement()}

	case TOKEN_EMBED_DIR:
		return ExportStmt{Inner: p.parseEmbedDirStatement()}

	case TOKEN_NATIVE:
		return ExportStmt{Inner: p.parseNativeFunctionStatement()}

	default:
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected const, let, fn, class, embedbin, embedstr, embeddir, native fn, interface, or enum after export",
		)
	}

	return nil
}

func (p *Parser) parseTryCatchStatement() Stmt {
	line := p.current.Line
	column := p.current.Column
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
		Line:      line,
		Column:    column,
		File:      p.current.File,
	}

	if p.current.Type == TOKEN_FINALLY {
		p.expect(TOKEN_FINALLY)
		finallyBody := p.parseBlock()
		statement.FinallyBody = finallyBody
	}

	return statement
}

func (p *Parser) parseThrowStatement() Stmt {
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_THROW)

	value := p.parseExpression()

	p.consumeTerminator()

	return ThrowStmt{
		Value:  value,
		Line:   line,
		Column: column,
		File:   p.current.File,
	}
}

func (p *Parser) parseForStatement() Stmt {
	p.expect(TOKEN_FOR)
	line := p.current.Line
	column := p.current.Column

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
				Line:      line,
				Column:    column,
				File:      p.current.File,
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
		Line:      line,
		Column:    column,
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
	p.consumeTerminator()

	return BreakStmt{}
}

func (p *Parser) parseContinueStatement() Stmt {
	p.expect(TOKEN_CONTINUE)
	p.consumeTerminator()

	return ContinueStmt{}
}

func (p *Parser) parseWhileStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_WHILE)

	condition := p.parseExpression()

	body := p.parseBlock()

	return WhileStmt{
		Condition: condition,
		Body:      body,
		Line:      line,
		Column:    column,
		File:      file,
	}
}

func (p *Parser) parseLockStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.advance()

	value := p.parseExpression()

	block := p.parseBlock()

	return LockStmt{
		Mutex:  value,
		Block:  block,
		File:   file,
		Line:   line,
		Column: column,
	}
}

func (p *Parser) parseEmbedStrStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_STR)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embedstr expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embedstr expected const or let after path, got %s", p.current.Type)
	}

	name := p.current.Literal
	p.expect(TOKEN_IDENT)

	combinedPath := filepath.Join(filepath.Dir(file), path.Value)

	absPath, err := filepath.Abs(combinedPath)

	_, err = os.Stat(absPath)
	if err != nil {
		LangErrorAt(ErrorSyntax, file, line, column, "could not embed file '%s': %s", filepath.Base(absPath), err)
	}

	return EmbedStmt{
		Kind:         EmbedStr,
		Name:         name,
		EmbeddedPath: absPath,
		Constant:     constant,
		TypeHint:     TypeHint{Name: "string"},
		File:         file,
		Line:         line,
		Column:       column,
	}
}

func (p *Parser) parseEmbedDirStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_DIR)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embeddir expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embeddir expected const or let after path, got %s", p.current.Type)
	}

	name := p.current.Literal
	p.expect(TOKEN_IDENT)

	combinedPath := filepath.Join(filepath.Dir(file), path.Value)

	absPath, err := filepath.Abs(combinedPath)

	info, err := os.Stat(absPath)
	if err != nil {
		LangErrorAt(ErrorSyntax, file, line, column, "could not embed directory '%s': %s", filepath.Base(absPath), err)
	}

	if !info.IsDir() {
		LangErrorAt(ErrorSyntax, file, line, column, "embeddir expected a directory path, but '%s' is a file", filepath.Base(absPath))
	}

	return EmbedStmt{
		Kind:         EmbedDir,
		Name:         name,
		EmbeddedPath: absPath,
		Constant:     constant,
		TypeHint:     TypeHint{Name: "object"},
		File:         file,
		Line:         line,
		Column:       column,
	}
}

func (p *Parser) parseEmbedBinStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_BIN)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embedbin expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embedbin expected const or let after path, got %s", p.current.Type)
	}

	name := p.current.Literal
	p.expect(TOKEN_IDENT)

	combinedPath := filepath.Join(filepath.Dir(file), path.Value)

	absPath, err := filepath.Abs(combinedPath)

	_, err = os.Stat(absPath)
	if err != nil {
		LangErrorAt(ErrorSyntax, file, line, column, "could not embed file '%s': %s", filepath.Base(absPath), err)
	}

	return EmbedStmt{
		Kind:         EmbedBin,
		Name:         name,
		EmbeddedPath: absPath,
		Constant:     constant,
		TypeHint:     TypeHint{Name: "buffer"},
		File:         file,
		Line:         line,
		Column:       column,
	}
}

func (p *Parser) parseInterfaceStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_INTERFACE)

	name := p.current.Literal
	p.expect(TOKEN_IDENT)

	typeParams := []string{}
	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	p.expect(TOKEN_LBRACE)

	fields := map[string]TypeHint{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		fieldName := p.current.Literal
		p.expect(TOKEN_IDENT)

		nullable := false
		if p.current.Type == TOKEN_QUESTION {
			p.advance()
			nullable = true
		}

		typeHint := p.parseTypeHint(nullable)
		fields[fieldName] = typeHint

		if p.current.Type == TOKEN_COMMA || p.current.Type == TOKEN_SEMI {
			p.advance()
		}
	}

	p.expect(TOKEN_RBRACE)

	return InterfaceStmt{
		Name:           name,
		TypeParameters: typeParams,
		Fields:         fields,
		File:           file,
		Line:           line,
		Column:         column,
	}
}

func (p *Parser) parseIfStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_IF)

	condition := p.parseExpression()

	thenBody := p.parseBlock()

	var elseBody []Stmt

	if p.current.Type == TOKEN_ELSE {
		p.advance()

		if p.current.Type == TOKEN_IF {
			elseIfStmt := p.parseIfStatement()
			elseBody = []Stmt{elseIfStmt}
		} else {
			elseBody = p.parseBlock()
		}
	}

	return IfStmt{
		Condition: condition,
		ThenBody:  thenBody,
		ElseBody:  elseBody,
		Line:      line,
		Column:    column,
		File:      file,
	}
}

func (p *Parser) parseBlock() []Stmt {
	p.expect(TOKEN_LBRACE)

	statements := []Stmt{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		stmt := p.parseStatement()
		if stmt != nil {
			statements = append(statements, stmt)
		}
	}

	p.expect(TOKEN_RBRACE)

	return statements
}

func (p *Parser) parseImportStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
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

		p.consumeTerminator()

		return ImportStmt{
			Path:   moduleName,
			Std:    true,
			Alias:  alias,
			File:   file,
			Line:   line,
			Column: column,
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

		p.consumeTerminator()

		return ImportStmt{
			Path:   pluginPath,
			Plugin: true,
			Std:    false,
			Alias:  alias,
			File:   file,
			Line:   line,
			Column: column,
		}
	} else if p.current.Type == TOKEN_IDENT && (p.current.Literal == "library" || p.current.Literal == "lib") {
		p.advance()

		if p.current.Type != TOKEN_STRING {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected library path after import library",
			)
		}

		libraryPath := p.current.Literal
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

		p.consumeTerminator()

		return ImportStmt{
			Path:    libraryPath,
			Library: true,
			Alias:   alias,
			File:    file,
			Line:    line,
			Column:  column,
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

	p.consumeTerminator()

	return ImportStmt{
		Path:   path,
		Plugin: false,
		Std:    false,
		Alias:  alias,
		File:   file,
		Line:   line,
		Column: column,
	}
}

func (p *Parser) parseFieldStatement() Stmt {
	p.expect(TOKEN_FIELD)

	constant := false
	private := false
	nullable := false

	switch p.current.Type {
	case TOKEN_PRIVATE:
		p.expect(TOKEN_PRIVATE)
		private = true
	case TOKEN_PUBLIC:
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
	line := p.current.Line
	column := p.current.Column
	p.advance()

	if p.current.Type == TOKEN_QUESTION {
		p.advance()
		nullable = true
	}

	typeHint := p.parseOptionalTypeHint()

	if nullable {
		typeHint.Name += "|null"
		typeHint.Types = append(typeHint.Types, "null")
	}

	p.expect(TOKEN_ASSIGN)

	value := p.parseExpression()

	p.consumeTerminator()

	return FieldStmt{
		Name:     name,
		Value:    value,
		TypeHint: typeHint,
		Constant: constant,
		Private:  private,
		File:     p.current.File,
		Line:     line,
		Column:   column,
	}
}

func (p *Parser) parseLetStatement() Stmt {
	line := p.current.Line
	column := p.current.Column
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

	p.consumeTerminator()

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: false,
		TypeHint: typeHint,
		Line:     line,
		Column:   column,
		File:     p.current.File,
	}
}

func (p *Parser) parseDefaultParamValue() TinyValue {
	switch p.current.Type {
	case TOKEN_STRING:
		value := p.current.Literal
		p.advance()
		return NewNative(value)

	case TOKEN_NUMBER:
		text := p.current.Literal
		p.advance()

		if strings.Contains(text, ".") {
			number, err := strconv.ParseFloat(text, 64)
			if err != nil {
				LangError(ErrorSyntax, "invalid number default: %s", text)
			}
			return NewNative(number)
		}

		number, err := strconv.Atoi(text)
		if err != nil {
			LangError(ErrorSyntax, "invalid number default: %s", text)
		}

		return NewInt(number)

	case TOKEN_TRUE:
		p.advance()
		return NewNative(true)

	case TOKEN_FALSE:
		p.advance()
		return NewNative(false)

	case TOKEN_NULL:
		p.advance()
		return NewNull()

	case TOKEN_LBRACKET:
		return p.parseDefaultParamArrayValue()

	case TOKEN_LBRACE:
		return p.parseDefaultParamObjectValue()

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
			return NewNative(-number)
		}

		number, err := strconv.Atoi(text)
		if err != nil {
			LangError(ErrorSyntax, "invalid number default: -%s", text)
		}

		return NewInt(-number)

	default:
		LangError(
			ErrorSyntax,
			"default arguments currently only support constant values",
		)
		return NewNull()
	}
}

func (p *Parser) parseDefaultParamArrayValue() TinyValue {
	p.expect(TOKEN_LBRACKET)

	elements := []TinyValue{}
	if p.current.Type == TOKEN_RBRACKET {
		p.advance()
		return NewNative(&ArrayValue{Elements: elements})
	}

	for {
		elements = append(elements, p.parseDefaultParamValue())

		if p.current.Type != TOKEN_COMMA {
			break
		}
		p.advance()

		if p.current.Type == TOKEN_RBRACKET {
			break
		}
	}

	p.expect(TOKEN_RBRACKET)
	return NewNative(&ArrayValue{Elements: elements})
}

func (p *Parser) parseDefaultParamObjectValue() TinyValue {
	p.expect(TOKEN_LBRACE)

	object := ObjectValue{}
	if p.current.Type == TOKEN_RBRACE {
		p.advance()
		return NewNative(object)
	}

	for {
		if p.current.Type != TOKEN_IDENT && p.current.Type != TOKEN_STRING {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected object field name in default argument",
			)
		}

		name := p.current.Literal
		p.advance()
		p.expect(TOKEN_COLON)

		object[name] = p.parseDefaultParamValue()

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type != TOKEN_COMMA {
			break
		}
		p.advance()

		if p.current.Type == TOKEN_RBRACE {
			break
		}
	}

	p.expect(TOKEN_RBRACE)
	return NewNative(object)
}

func (p *Parser) parseConstStatement() Stmt {
	line := p.current.Line
	column := p.current.Column
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

	p.consumeTerminator()

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: true,
		TypeHint: typeHint,
		Line:     line,
		Column:   column,
		File:     p.current.File,
	}
}

func (p *Parser) parseNativeFunctionStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_NATIVE)
	p.expect(TOKEN_FN)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected native function name")
	}

	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LPAREN)
	params := p.parseParameterList(true)
	p.expect(TOKEN_RPAREN)

	if p.current.Type != TOKEN_COLON {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "native function declarations require an explicit return type. use \"null\" if there is none.")
	}

	returnType := p.parseTypeHint(false)

	p.expect(TOKEN_LBRACE)

	if p.current.Type == TOKEN_IDENT && p.current.Literal == "go" {
		p.advance()
	} else {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected 'go' block inside native function body")
	}

	startPos := p.posOfToken(p.current)
	var builder strings.Builder
	depth := 1
	pos := startPos + 1

	for {
		if pos >= len(p.lexer.input) {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "unexpected EOF while parsing native block")
		}

		ch := p.lexer.input[pos]
		pos++

		if ch == '{' {
			depth++
		} else if ch == '}' {
			depth--
			if depth == 0 {
				break
			}
		}

		builder.WriteRune(ch)
	}

	p.lexer.pos = pos
	p.lexer.line, p.lexer.column = p.lexer.lineColumnAt(pos)

	p.advance()
	p.advance()

	for p.current.Type == TOKEN_SEMI {
		p.advance()
	}

	p.expect(TOKEN_RBRACE)

	return NativeFnStmt{
		Name:       name,
		Params:     params,
		ReturnType: returnType,
		GoCode:     builder.String(),
		File:       file,
		Line:       line,
		Column:     column,
	}
}

func (p *Parser) parseFunctionStatement(async bool) Stmt {
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_FN)

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected function name")
	}

	name := p.current.Literal
	p.advance()

	typeParams := []string{}
	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	params, returnType, body := p.parseFunctionSignatureAndBody()

	return FunctionStmt{
		Name:           name,
		TypeParameters: typeParams,
		Params:         params,
		ReturnType:     returnType,
		Body:           body,
		Async:          async,
		Line:           line,
		Column:         column,
		File:           p.current.File,
	}
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}

	return false
}

func (p *Parser) parseParameterList(enforceTypes ...bool) []Param {
	params := []Param{}
	var enforceTypeChecks bool
	if len(enforceTypes) > 0 {
		enforceTypeChecks = enforceTypes[0]
	} else {
		enforceTypeChecks = false
	}

	if p.current.Type == TOKEN_RPAREN {
		return params
	}

	for {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RPAREN {
			break
		}

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

		nullable := false

		if p.current.Type == TOKEN_QUESTION {
			p.advance()
			nullable = true
		}

		typeHint := TypeHint{}

		if enforceTypeChecks && p.current.Type != TOKEN_COLON {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "function parameter types are required")
		}

		if p.current.Type == TOKEN_COLON {
			p.advance()

			types := []string{}

			for {
				types = append(types, p.parseTypeName())

				if p.current.Type != TOKEN_PIPE {
					break
				}

				p.advance()
			}

			typeHint = TypeHint{
				Name:  strings.Join(types, "|"),
				Types: types,
			}
		}

		if nullable {
			if typeHint.IsEmpty() {
				typeHint = TypeHint{
					Name:  "any|null",
					Types: []string{"any", "null"},
				}
			} else {
				types := typeHint.AllTypes()

				if !containsString(types, "null") {
					types = append(types, "null")
				}

				typeHint = TypeHint{
					Name:  strings.Join(types, "|"),
					Types: types,
				}
			}
		}

		param := Param{
			Name:     name,
			TypeHint: typeHint,
			Variadic: variadic,
		}

		if nullable {
			param.HasDefault = true
			param.DefaultValue = NewNull()
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

func (vm *VM) applyDefaultArgs(fn Function, args []TinyValue, paramOffset int, callableName string) []TinyValue {
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

	finalArgs := make([]TinyValue, maxArgs)

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

func cloneDefaultValue(value TinyValue) TinyValue {
	switch v := value.Value.(type) {
	case *ArrayValue:
		copied := make([]TinyValue, len(v.Elements))
		for i, item := range v.Elements {
			copied[i] = cloneDefaultValue(item)
		}
		return NewNative(&ArrayValue{Elements: copied})

	case ObjectValue:
		copied := ObjectValue{}
		for key, item := range v {
			copied[key] = cloneDefaultValue(item)
		}
		return NewNative(copied)

	default:
		return value
	}
}

func (p *Parser) parseReturnStatement() Stmt {
	p.expect(TOKEN_RETURN)
	line := p.current.Line
	column := p.current.Column

	if p.current.Type == TOKEN_SEMI {
		p.consumeTerminator()

		return ReturnStmt{
			HasValue: false,
			Line:     line,
			Column:   column,
			File:     p.current.File,
		}
	}

	value := p.parseExpression()

	p.consumeTerminator()

	return ReturnStmt{
		Value:    value,
		HasValue: true,
		Line:     line,
		Column:   column,
		File:     p.current.File,
	}
}

func (p *Parser) consumeTerminator() {
	if p.current.Type == TOKEN_SEMI {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}
		return
	}

	if p.current.Type == TOKEN_RBRACE || p.current.Type == TOKEN_EOF {
		return
	}

	p.expect(TOKEN_SEMI)
}

func (p *Parser) parseExpressionStatement() Stmt {
	value := p.parseExpression()

	p.consumeTerminator()

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

	oldInTernary := p.inTernaryThen
	p.inTernaryThen = true
	thenExpr := p.parseExpression()
	p.inTernaryThen = oldInTernary

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
				File:   p.current.File,
				Line:   p.current.Line,
				Column: p.current.Column,
			}

		case TOKEN_QUESTION_QUESTION:
			p.advance()

			right := p.parseExpression()

			file := p.current.File
			line := p.current.Line
			column := p.current.Column

			return NullishCoalescingExpr{
				Left:   expr,
				Right:  right,
				File:   file,
				Line:   line,
				Column: column,
			}

		case TOKEN_DOT, TOKEN_QUESTION_DOT:
			safe := p.current.Type == TOKEN_QUESTION_DOT
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
					Safe:   safe,
				}

				continue
			}

			expr = PropertyExpr{
				Object: expr,
				Name:   name,
				File:   file,
				Line:   line,
				Column: column,
				Safe:   safe,
			}

		case TOKEN_LBRACKET:
			p.advance()

			index := p.parseExpression()

			p.expect(TOKEN_RBRACKET)

			expr = IndexExpr{
				Object: expr,
				Index:  index,
			}

		case TOKEN_COLON:
			if !p.isGenericColon() {
				return expr
			}

			typeArgs := []TypeHint{}
			for p.current.Type == TOKEN_COLON {
				p.advance()
				typeArgs = append(typeArgs, TypeHint{Name: p.parseTypeName()})
			}

			expr = InstantiatedExpr{
				Object:   expr,
				TypeArgs: typeArgs,
				File:     p.current.File,
				Line:     p.current.Line,
				Column:   p.current.Column,
			}

		default:
			return expr
		}
	}
}

func (p *Parser) isGenericColon() bool {
	if p.current.Type != TOKEN_COLON {
		return false
	}

	if !p.inTernaryThen {
		return true
	}

	pos := p.posOfToken(p.current)
	if pos < 0 || pos >= len(p.lexer.input) {
		return false
	}

	pos++

	skipWhitespace := func(i int) int {
		for i < len(p.lexer.input) {
			ch := p.lexer.input[i]
			if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
				i++
				continue
			}
			if ch == '/' && i+1 < len(p.lexer.input) && p.lexer.input[i+1] == '/' {
				i += 2
				for i < len(p.lexer.input) && p.lexer.input[i] != '\n' {
					i++
				}
				continue
			}
			if ch == '/' && i+1 < len(p.lexer.input) && p.lexer.input[i+1] == '*' {
				i += 2
				for i+1 < len(p.lexer.input) {
					if p.lexer.input[i] == '*' && p.lexer.input[i+1] == '/' {
						i += 2
						break
					}
					i++
				}
				continue
			}
			break
		}
		return i
	}

	pos = skipWhitespace(pos)
	if pos >= len(p.lexer.input) {
		return false
	}

	isIdentStart := func(ch rune) bool {
		return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_'
	}
	isIdentPart := func(ch rune) bool {
		return isIdentStart(ch) || (ch >= '0' && ch <= '9')
	}

	if !isIdentStart(p.lexer.input[pos]) {
		return false
	}

	for pos < len(p.lexer.input) && isIdentPart(p.lexer.input[pos]) {
		pos++
	}

	pos = skipWhitespace(pos)
	if pos >= len(p.lexer.input) {
		return false
	}

	nextChar := p.lexer.input[pos]
	return nextChar == '(' || nextChar == ':' || nextChar == '|'
}

func (p *Parser) parseArrayLiteral() Expr {
	p.expect(TOKEN_LBRACKET)

	var elements []Expr

	if p.current.Type == TOKEN_RBRACKET {
		p.expect(TOKEN_RBRACKET)
		return ArrayExpr{Elements: elements}
	}

	for {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RBRACKET {
			break
		}

		element := p.parseExpression()
		elements = append(elements, element)

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

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
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			p.advance()
			name := p.current.Literal
			tokenType := p.current.Type
			p.advance()

			if tokenType != TOKEN_IDENT {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected object field name, got %s",
					tokenType,
				)
			}

			value := IdentExpr{
				Name:   name,
				File:   p.current.File,
				Line:   p.current.Line,
				Column: p.current.Column,
			}

			fields = append(fields, ObjectField{
				Name:    name,
				Value:   nil,
				Copy:    value,
				HasCopy: true,
			})

			if p.current.Type == TOKEN_SEMI {
				p.advance()
			}

			if p.current.Type != TOKEN_COMMA {
				break
			}

			p.advance()

			continue
		}
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
		tokenType := p.current.Type
		p.advance()

		if p.current.Type == TOKEN_COLON {
			p.expect(TOKEN_COLON)

			value := p.parseExpression()

			fields = append(fields, ObjectField{
				Name:  name,
				Value: value,
			})
		} else {
			if tokenType != TOKEN_IDENT {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"expected object field name, got %s",
					tokenType,
				)
			}
			value := IdentExpr{
				Name:   name,
				File:   p.current.File,
				Line:   p.current.Line,
				Column: p.current.Column,
			}

			fields = append(fields, ObjectField{
				Name:  name,
				Value: value,
			})
		}

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	p.expect(TOKEN_RBRACE)

	return ObjectExpr{Fields: fields}
}

func (p *Parser) parseFunctionSignatureAndBody() ([]Param, TypeHint, []Stmt) {
	p.deferCountStack = append(p.deferCountStack, 0)
	defer func() {
		p.deferCountStack = p.deferCountStack[:len(p.deferCountStack)-1]
	}()

	p.expect(TOKEN_LPAREN)

	params := p.parseParameterList()

	p.expect(TOKEN_RPAREN)

	returnType := TypeHint{}

	if p.current.Type == TOKEN_COLON {
		returnType = p.parseTypeHint(false)
		// p.advance()
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

		return IdentExpr{
			Name:   name,
			File:   p.current.File,
			Line:   p.current.Line,
			Column: p.current.Column}

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
		return ThisExpr{
			File:   p.current.File,
			Line:   p.current.Line,
			Column: p.current.Column,
		}

	case TOKEN_NULL:
		p.advance()
		return NullExpr{}

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
		return NullExpr{}
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

	enumFile := p.current.File
	enumLine := p.current.Line
	enumColumn := p.current.Column
	name := p.current.Literal
	p.advance()

	p.expect(TOKEN_LBRACE)

	members := []EnumField{}
	iotaEnum := false

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RBRACE || p.current.Type == TOKEN_EOF {
			break
		}

		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected enum member name",
			)
		}

		name := p.current.Literal
		p.advance()

		if p.current.Type == TOKEN_ASSIGN {
			p.advance()

			if p.current.Type == TOKEN_IOTA {
				if len(members) > 0 {
					LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "the 'iota' keyword can only be used for the first member of an enum.")
				}
				iotaEnum = true
				members = append(members, EnumField{
					Name:  name,
					Value: NumberExpr{Value: 0},
				})
				p.advance()
			} else {
				if iotaEnum {
					LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "enums using 'iota' may not contain members with explicit values of other types.")
				}

				value := p.parseExpression()

				// enforce strings and numbers only
				switch value.(type) {
				case StringExpr, NumberExpr:
					break
				default:
					LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "enum members must be either a numeric or string constant.")
				}

				// enforce the same type
				if !iotaEnum && len(members) > 0 {
					for _, v := range members {
						if !p.compareTwoConst(v.Value, value) {
							LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "all enum members must have the same type.")

						}
						break
					}
				}

				members = append(members, EnumField{
					Name:  name,
					Value: value,
				})
			}
		} else {
			// enforce the same type
			if !iotaEnum && len(members) > 0 {
				for _, v := range members {
					if !p.compareTwoConst(v.Value, StringExpr{Value: name}) {
						LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "all enum members must have the same type.")
					}
					break
				}
			}

			members = append(members, EnumField{
				Name:  name,
				Value: StringExpr{Value: name},
			})
		}

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

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

	if iotaEnum {
		for i := range members {
			members[i].Value = NumberExpr{Value: i}
		}
	}

	p.expect(TOKEN_RBRACE)

	if p.current.Type == TOKEN_SEMI {
		p.advance()
	}

	return EnumStmt{
		Name:    name,
		Members: members,
		File:    enumFile,
		Line:    enumLine,
		Column:  enumColumn,
	}
}

func (p *Parser) parseAsyncStmt() Stmt {
	p.expect(TOKEN_ASYNC)

	return p.parseFunctionStatement(true)
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

	typeParams := []string{}
	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	p.expect(TOKEN_LBRACE)

	var methods []FunctionStmt
	embeds := []string{}
	fields := []FieldStmt{}

	for p.current.Type != TOKEN_RBRACE {
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RBRACE {
			break
		}

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

			p.consumeTerminator()
			continue
		}

		functionPrivate := false

		if p.current.Type == TOKEN_PRIVATE {
			p.advance()
			functionPrivate = true
		} else if p.current.Type == TOKEN_PUBLIC {
			p.advance()
		}

		async := false

		if p.current.Type == TOKEN_ASYNC {
			p.advance()
			async = true
		}

		if p.current.Type != TOKEN_FN {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected declared variable, method or embed in class")
		}

		method := p.parseFunctionStatement(async)

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
		Name:           name,
		TypeParameters: typeParams,
		Methods:        methods,
		Embeds:         embeds,
		Fields:         fields,
		File:           p.current.File,
		Line:           p.current.Line,
		Column:         p.current.Column,
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

	case TOKEN_AWAIT:
		p.advance()
		expr := p.parsePostfix()
		return AwaitExpr{
			Task:   expr,
			File:   p.current.File,
			Line:   p.current.Line,
			Column: p.current.Column,
		}

	case TOKEN_DEFER:
		p.advance()

		if len(p.deferCountStack) > 0 {
			p.deferCountStack[len(p.deferCountStack)-1]++
			if p.deferCountStack[len(p.deferCountStack)-1] > 1 {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"multiple defer statements are not permitted within the same function scope",
				)
			}
		}

		fn := p.parseUnary()

		_, ok := fn.(FunctionExpr)
		if !ok {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected function after defer, got %s",
				p.current.Type,
			)
		}

		return DeferExpr{
			Function: fn,
			File:     p.current.File,
			Line:     p.current.Line,
			Column:   p.current.Column,
		}

	case TOKEN_SPAWN:
		p.advance()

		args := []Expr{}

		p.expect(TOKEN_LPAREN)

		for {
			for p.current.Type == TOKEN_SEMI {
				p.advance()
			}

			if p.current.Type == TOKEN_RPAREN {
				break
			}

			args = append(args, p.parseExpression())

			for p.current.Type == TOKEN_SEMI {
				p.advance()
			}

			if p.current.Type != TOKEN_COMMA {
				break
			}

			p.advance()
		}

		p.expect(TOKEN_RPAREN)

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
			Args:     args,
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
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RPAREN {
			break
		}

		var arg Expr
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			p.expect(TOKEN_DOT_DOT_DOT)
			arg = SpreadExpr{Value: p.parseExpression()}
		} else {
			arg = p.parseExpression()
		}

		args = append(args, arg)

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

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
