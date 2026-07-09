package vm

import (
	"os"
	"path/filepath"
	"sort"
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

func advanceSourcePosition(text string, line int, column int) (int, int) {
	for _, ch := range text {
		if ch == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return line, column
}

func parseInterpolatedString(input string, file string, line int, column int) Expr {
	var parts []InterpolatedStringPart
	currentLine := line
	currentColumn := column

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
		exprLine, exprColumn := advanceSourcePosition(input[:start+2], currentLine, currentColumn)

		lexer := NewLexer(exprSource, file)
		lexer.EnableASI = false
		lexer.line = exprLine
		lexer.column = exprColumn
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

		currentLine, currentColumn = advanceSourcePosition(input[:end+1], currentLine, currentColumn)
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

func tokenEndPosition(tok Token) SourcePosition {
	literal := tok.Literal
	if literal == "" {
		literal = string(tok.Type)
	}
	line, column := advanceSourcePosition(literal, tok.Line, tok.Column)
	return SourcePosition{File: tok.File, Line: line, Column: column}
}

func tokenRange(tok Token) SourceRange {
	return SourceRange{
		Start: SourcePosition{File: tok.File, Line: tok.Line, Column: tok.Column},
		End:   tokenEndPosition(tok),
	}
}

func rangeFromTokens(start Token, end Token) SourceRange {
	return SourceRange{
		Start: SourcePosition{File: start.File, Line: start.Line, Column: start.Column},
		End:   tokenEndPosition(end),
	}
}

type ParsedBlockInfo struct {
	Kind           string
	Name           string
	StartLine      int
	StartCol       int
	EndLine        int
	EndCol         int
	LparenLine     int
	LparenCol      int
	RparenLine     int
	RparenCol      int
	LbraceLine     int
	LbraceCol      int
	RbraceLine     int
	RbraceCol      int
	IsAsync        bool
	TypeParameters []string
}

type Parser struct {
	lexer *Lexer

	current Token
	next    Token

	deferCountStack []int
	inTernaryThen   bool

	ErrorTolerant   bool
	Diagnostics     []LangErrorType
	Blocks          []ParsedBlockInfo
	lastRbraceToken Token
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

func (p *Parser) peek(n int) Token {
	if n <= 0 {
		return p.current
	}
	if n == 1 {
		return p.next
	}

	pos := p.lexer.pos
	line := p.lexer.line
	column := p.lexer.column
	insertSemi := p.lexer.insertSemi
	defer func() {
		p.lexer.pos = pos
		p.lexer.line = line
		p.lexer.column = column
		p.lexer.insertSemi = insertSemi
	}()

	tok := p.next
	for i := 1; i < n; i++ {
		tok = p.lexer.NextToken()
	}
	return tok
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

func (p *Parser) ParseProgramTolerant() Program {
	p.ErrorTolerant = true
	var statements []Stmt

	for p.current.Type != TOKEN_EOF {
		stmt := func() Stmt {
			defer func() {
				if r := recover(); r != nil {
					if err, ok := r.(LangErrorType); ok {
						p.Diagnostics = append(p.Diagnostics, err)
						p.synchronize()
					} else {
						panic(r)
					}
				}
			}()

			if p.current.Type == TOKEN_RBRACE {
				LangErrorAt(
					ErrorSyntax,
					p.current.File,
					p.current.Line,
					p.current.Column,
					"unexpected }",
				)
			}

			return p.parseStatement()
		}()

		if stmt != nil {
			statements = append(statements, stmt)
		}
	}

	return Program{Statements: statements}
}

func (p *Parser) synchronize() {
	p.advance()

	for p.current.Type != TOKEN_EOF {
		if p.current.Type == TOKEN_SEMI {
			p.advance()
			return
		}

		switch p.current.Type {
		case TOKEN_IMPORT, TOKEN_LET, TOKEN_CONST, TOKEN_NATIVE, TOKEN_EXTERNAL,
			TOKEN_FN, TOKEN_ASYNC, TOKEN_RETURN, TOKEN_INTERFACE,
			TOKEN_IF, TOKEN_WHILE, TOKEN_FOR, TOKEN_CLASS, TOKEN_BREAK,
			TOKEN_CONTINUE, TOKEN_THROW, TOKEN_TRY, TOKEN_ENUM, TOKEN_EXPORT, TOKEN_MATCH:
			return
		}

		p.advance()
	}
}

func (p *Parser) synchronizeBlock() {
	p.advance()

	for p.current.Type != TOKEN_EOF && p.current.Type != TOKEN_RBRACE {
		if p.current.Type == TOKEN_SEMI {
			p.advance()
			return
		}

		switch p.current.Type {
		case TOKEN_IMPORT, TOKEN_LET, TOKEN_CONST, TOKEN_NATIVE, TOKEN_EXTERNAL,
			TOKEN_FN, TOKEN_ASYNC, TOKEN_RETURN, TOKEN_INTERFACE,
			TOKEN_IF, TOKEN_WHILE, TOKEN_FOR, TOKEN_CLASS, TOKEN_BREAK,
			TOKEN_CONTINUE, TOKEN_THROW, TOKEN_TRY, TOKEN_ENUM, TOKEN_EXPORT, TOKEN_MATCH:
			return
		}

		p.advance()
	}
}

func (p *Parser) recordBlock(kind string, name string, startTok Token, endTok Token, isAsync bool, typeParams []string, lparenTok Token, rparenTok Token, lbraceTok Token, rbraceTok Token) {
	if name == "" || startTok.Line <= 0 || lbraceTok.Line <= 0 {
		return
	}
	if rbraceTok.Line <= 0 {
		rbraceTok = endTok
	}
	p.Blocks = append(p.Blocks, ParsedBlockInfo{
		Kind:           kind,
		Name:           name,
		StartLine:      startTok.Line,
		StartCol:       startTok.Column,
		EndLine:        endTok.Line,
		EndCol:         endTok.Column + len(endTok.Literal),
		LparenLine:     lparenTok.Line,
		LparenCol:      lparenTok.Column,
		RparenLine:     rparenTok.Line,
		RparenCol:      rparenTok.Column,
		LbraceLine:     lbraceTok.Line,
		LbraceCol:      lbraceTok.Column,
		RbraceLine:     rbraceTok.Line,
		RbraceCol:      rbraceTok.Column,
		IsAsync:        isAsync,
		TypeParameters: typeParams,
	})
}

func (p *Parser) parseTypeName() string {
	if p.current.Type == TOKEN_LBRACE {
		fields := p.parseStructuralTypeFields()
		return "{" + strings.Join(structuralTypeFieldStrings(fields), ", ") + "}"
	}

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

	if name == "function" && p.current.Type == TOKEN_LPAREN {
		p.advance()

		params := []string{}
		for p.current.Type != TOKEN_RPAREN {
			paramType := p.parseTypeName()
			for p.current.Type == TOKEN_PIPE {
				p.advance()
				paramType += " | " + p.parseTypeName()
			}
			params = append(params, paramType)

			if p.current.Type != TOKEN_COMMA {
				break
			}

			p.advance()
		}

		p.expect(TOKEN_RPAREN)
		name = "function(" + strings.Join(params, ", ") + ")"
	} else {
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
	}

	if p.current.Type == TOKEN_QUESTION {
		p.advance()
		name += " | null"
	}

	return name
}

func (p *Parser) parseStructuralTypeFields() map[string]TypeHint {
	p.expect(TOKEN_LBRACE)
	fields := make(map[string]TypeHint)

	for p.current.Type != TOKEN_RBRACE {
		if !isSoftIdentifierToken(p.current.Type) {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected field name in structural type")
		}
		fieldName := p.current.Literal
		fieldLine := p.current.Line
		fieldCol := p.current.Column
		p.advance()

		fieldType := p.parseOptionalTypeHint()
		fieldType.Range = SourceRange{
			Start: SourcePosition{Line: fieldLine, Column: fieldCol},
			End:   SourcePosition{Line: fieldLine, Column: fieldCol + len(fieldName)},
		}
		fields[fieldName] = fieldType

		if p.current.Type == TOKEN_COMMA {
			p.advance()
		}
	}

	p.expect(TOKEN_RBRACE)
	return fields
}

func (p *Parser) parseTypeHint(nullable bool) TypeHint {
	p.expect(TOKEN_COLON)

	if p.current.Type == TOKEN_LBRACE {
		fields := p.parseStructuralTypeFields()
		hint := TypeHint{
			Name:   "{" + strings.Join(structuralTypeFieldStrings(fields), ", ") + "}",
			Fields: fields,
		}
		if nullable {
			hint.Name += " | null"
			hint.Types = []string{hint.Name}
		}
		return hint
	}

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

	if p.current.Type == TOKEN_LBRACE {
		fields := p.parseStructuralTypeFields()
		return TypeHint{
			Name:   "{" + strings.Join(structuralTypeFieldStrings(fields), ", ") + "}",
			Fields: fields,
		}
	}

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

func structuralTypeFieldStrings(fields map[string]TypeHint) []string {
	parts := make([]string, 0, len(fields))
	for name, field := range fields {
		parts = append(parts, name+": "+field.String())
	}
	sort.Strings(parts)
	return parts
}

func (p *Parser) isValidType(token TokenType) bool {
	return token == TOKEN_IDENT ||
		token == TOKEN_NULL
}

func isIdentifierLikeToken(tokenType TokenType) bool {
	switch tokenType {
	case TOKEN_IDENT,
		TOKEN_TRUE,
		TOKEN_FALSE,
		TOKEN_THIS,
		TOKEN_NULL,
		TOKEN_IMPORT,
		TOKEN_EXPORT,
		TOKEN_LET,
		TOKEN_CONST,
		TOKEN_FIELD,
		TOKEN_NATIVE,
		TOKEN_EXTERNAL,
		TOKEN_FN,
		TOKEN_RETURN,
		TOKEN_THROW,
		TOKEN_CLASS,
		TOKEN_PRIVATE,
		TOKEN_PUBLIC,
		TOKEN_INTERFACE,
		TOKEN_ENUM,
		TOKEN_IOTA,
		TOKEN_DEFER,
		TOKEN_IF,
		TOKEN_ELSE,
		TOKEN_WHILE,
		TOKEN_FOR,
		TOKEN_IN,
		TOKEN_TRY,
		TOKEN_CATCH,
		TOKEN_FINALLY,
		TOKEN_BREAK,
		TOKEN_CONTINUE,
		TOKEN_MATCH,
		TOKEN_AND,
		TOKEN_OR,
		TOKEN_TYPEOF,
		TOKEN_INSTANCEOF,
		TOKEN_SPAWN,
		TOKEN_ASYNC,
		TOKEN_AWAIT,
		TOKEN_EMBED,
		TOKEN_EMBED_TEXT,
		TOKEN_EMBED_BYTES,
		TOKEN_EMBED_FOLDER:
		return true
	default:
		return false
	}
}

func isStatementStartKeyword(tokenType TokenType) bool {
	switch tokenType {
	case TOKEN_IF, TOKEN_WHILE, TOKEN_FOR, TOKEN_TRY, TOKEN_MATCH,
		TOKEN_BREAK, TOKEN_CONTINUE, TOKEN_RETURN, TOKEN_THROW:
		return true
	}
	return false
}

func isSoftIdentifierToken(tokenType TokenType) bool {
	switch tokenType {
	case TOKEN_IDENT,
		TOKEN_EMBED, TOKEN_MATCH,
		TOKEN_FIELD, TOKEN_NATIVE, TOKEN_EXTERNAL,
		TOKEN_PRIVATE, TOKEN_PUBLIC,
		TOKEN_IMPLEMENTS, TOKEN_EXTENDS,
		TOKEN_IOTA,
		TOKEN_EMBED_TEXT, TOKEN_EMBED_BYTES, TOKEN_EMBED_FOLDER:
		return true
	default:
		return false
	}
}

func (p *Parser) parseIdentifierLikeName(context string) string {
	if !isIdentifierLikeToken(p.current.Type) {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected %s",
			context,
		)
	}

	name := p.current.Literal
	p.advance()
	return name
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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

	case TOKEN_AMP_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
					Op:    TOKEN_AMP,
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
					Op:    TOKEN_AMP,
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
				"invalid &= target",
			)
		}

	case TOKEN_PIPE_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
					Op:    TOKEN_PIPE,
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
					Op:    TOKEN_PIPE,
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
				"invalid |= target",
			)
		}

	case TOKEN_CARET_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
					Op:    TOKEN_CARET,
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
					Op:    TOKEN_CARET,
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
				"invalid ^= target",
			)
		}

	case TOKEN_LSHIFT_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
					Op:    TOKEN_LSHIFT,
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
					Op:    TOKEN_LSHIFT,
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
				"invalid <<= target",
			)
		}

	case TOKEN_RSHIFT_ASSIGN:
		p.advance()

		value := p.parseExpression()

		p.consumeTerminator()

		switch target := left.(type) {
		case IdentExpr:
			return AssignStmt{
				Name: target.Name,
				Value: BinaryExpr{
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
					Op:    TOKEN_RSHIFT,
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
					Op:    TOKEN_RSHIFT,
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
				"invalid >>= target",
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
		if p.peek(1).Type != TOKEN_FN {
			return p.parseExpressionStatement()
		}
		return p.parseNativeFunctionStatement()
	case TOKEN_EXTERNAL:
		nextToken := p.peek(1).Type
		switch nextToken {
		case TOKEN_FN:
			return p.parseExternalFunctionStatement()
		case TOKEN_CONST:
			return p.parseExternalGlobalStatement()
		default:
			return p.parseExpressionStatement()
		}

	case TOKEN_FN:
		return p.parseFunctionStatement(false)
	case TOKEN_ASYNC:
		return p.parseAsyncStmt()
	case TOKEN_RETURN:
		return p.parseReturnStatement()
	case TOKEN_INTERFACE:
		return p.parseInterfaceStatement()
	case TOKEN_EMBED_TEXT:
		return p.parseEmbedTextStatement()
	case TOKEN_EMBED_BYTES:
		return p.parseEmbedBytesStatement()
	case TOKEN_EMBED_FOLDER:
		return p.parseEmbedFolderStatement()
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
		if p.peek(1).Type == TOKEN_LPAREN {
			return p.parseExpressionStatement()
		}
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

		caseValues, guard, bindName := p.parseMatchPattern()

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		body := p.parseBlock()

		cases = append(cases, MatchCase{
			Values:   caseValues,
			Value:    caseValues[0],
			Guard:    guard,
			BindName: bindName,
			Body:     body,
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

func (p *Parser) parseMatchPattern() ([]Expr, Expr, string) {
	bindName := ""

	// Check for binding pattern: `name if ...` where name is an identifier
	// that isn't a keyword or literal expression start
	if p.current.Type == TOKEN_IDENT {
		// Peek ahead: if the next token is `if`, this is a binding pattern
		if p.next.Type == TOKEN_IF {
			bindName = p.current.Literal
			p.advance()
			p.expect(TOKEN_IF)
			guard := p.parseExpression()
			return []Expr{nil}, guard, bindName
		}
	}

	// Check for parenthesized union pattern: (expr | expr | expr)
	if p.current.Type == TOKEN_LPAREN {
		p.advance() // consume '('

		values := []Expr{p.parseExpression()}

		for p.current.Type == TOKEN_PIPE {
			p.advance() // consume '|'
			values = append(values, p.parseExpression())
		}

		p.expect(TOKEN_RPAREN) // expect ')'

		// Check for guard after union: (a | b | c) if guard
		var guard Expr
		if p.current.Type == TOKEN_IF {
			p.advance()
			guard = p.parseExpression()
		}

		return values, guard, ""
	}

	// Parse a single expression (could be a guard pattern)
	firstExpr := p.parseExpression()

	// Check for guard after single expression: expr if guard
	var guard Expr
	if p.current.Type == TOKEN_IF {
		p.advance()
		guard = p.parseExpression()
	}

	return []Expr{firstExpr}, guard, ""
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

	case TOKEN_EMBED_TEXT:
		return ExportStmt{Inner: p.parseEmbedTextStatement()}

	case TOKEN_EMBED_BYTES:
		return ExportStmt{Inner: p.parseEmbedBytesStatement()}

	case TOKEN_EMBED_FOLDER:
		return ExportStmt{Inner: p.parseEmbedFolderStatement()}

	case TOKEN_NATIVE:
		return ExportStmt{Inner: p.parseNativeFunctionStatement()}

	case TOKEN_EXTERNAL:
		nextToken := p.peek(1).Type
		switch nextToken {
		case TOKEN_FN:
			return ExportStmt{Inner: p.parseExternalFunctionStatement()}
		case TOKEN_CONST:
			return ExportStmt{Inner: p.parseExternalGlobalStatement()}
		default:
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "external keyword expects 'fn' or 'const' after it.")
		}

	default:
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected const, let, fn, class, embedbytes, embedtext, embedfolder, native fn, interface, enum, or external after export",
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

	if !isSoftIdentifierToken(p.current.Type) {
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

	hasParens := p.current.Type == TOKEN_LPAREN
	if hasParens {
		p.advance()
	}

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

	if p.current.Type != TOKEN_LBRACE && p.current.Type != TOKEN_RPAREN {
		update = p.parseForUpdateStatement()
	}

	if hasParens {
		if p.current.Type == TOKEN_RPAREN {
			p.advance()
		}
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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
					Left: IdentExpr{
						Name:   target.Name,
						Line:   p.current.Line,
						Column: p.current.Column,
						File:   p.current.File,
					},
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

func (p *Parser) parseEmbedTextStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_TEXT)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embedtext expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embedtext expected const or let after path, got %s", p.current.Type)
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
		Kind:         EmbedText,
		Name:         name,
		EmbeddedPath: absPath,
		Constant:     constant,
		TypeHint:     TypeHint{Name: "string"},
		File:         file,
		Line:         line,
		Column:       column,
	}
}

func (p *Parser) parseEmbedFolderStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_FOLDER)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embedfolder expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embedfolder expected const or let after path, got %s", p.current.Type)
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
		LangErrorAt(ErrorSyntax, file, line, column, "embedfolder expected a directory path, but '%s' is a file", filepath.Base(absPath))
	}

	return EmbedStmt{
		Kind:         EmbedFolder,
		Name:         name,
		EmbeddedPath: absPath,
		Constant:     constant,
		TypeHint:     TypeHint{Name: "object"},
		File:         file,
		Line:         line,
		Column:       column,
	}
}

func (p *Parser) parseEmbedBytesStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EMBED_BYTES)

	pathExpr := p.parseExpression()

	path, ok := pathExpr.(StringExpr)
	if !ok {
		LangErrorAt(ErrorSyntax, file, line, column, "embedbytes expected string, got %T", pathExpr)
	}

	constant := false

	switch p.current.Type {
	case TOKEN_CONST:
		p.advance()
		constant = true
	case TOKEN_LET:
		p.advance()
	default:
		LangErrorAt(ErrorSyntax, file, line, column, "embedbytes expected const or let after path, got %s", p.current.Type)
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
		Kind:         EmbedBytes,
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
	startTok := p.current
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_INTERFACE)

	var name string
	var typeParams []string
	var extends []string
	var lbraceTok Token
	var rbraceTok Token

	defer func() {
		if name == "" {
			return
		}
		endTok := rbraceTok
		if endTok.Line == 0 {
			endTok = p.current
		}
		rbrace := rbraceTok
		if rbrace.Line == 0 {
			rbrace = p.current
		}
		p.recordBlock("interface", name, startTok, endTok, false, typeParams, Token{}, Token{}, lbraceTok, rbrace)
	}()

	nameTok := p.current
	name = p.current.Literal
	nameRange := tokenRange(nameTok)
	p.expect(TOKEN_IDENT)

	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	if p.current.Type == TOKEN_EXTENDS {
		p.advance()
		for {
			extends = append(extends, p.parseTypeName())
			if p.current.Type != TOKEN_COMMA {
				break
			}
			p.advance()
		}
	}

	lbraceTok = p.current
	p.expect(TOKEN_LBRACE)

	fields := map[string]TypeHint{}
	fieldRanges := map[string]SourceRange{}
	fieldNameRanges := map[string]SourceRange{}
	fieldTypeRanges := map[string]SourceRange{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		fieldStartTok := p.current
		fieldName := p.parseIdentifierLikeName("interface field name")

		nullable := false
		if p.current.Type == TOKEN_QUESTION {
			p.advance()
			nullable = true
		}

		typeStartTok := p.current
		typeHint := p.parseTypeHint(nullable)
		typeEndTok := p.current
		fields[fieldName] = typeHint
		fieldRanges[fieldName] = rangeFromTokens(fieldStartTok, typeEndTok)
		fieldNameRanges[fieldName] = tokenRange(fieldStartTok)
		if typeStartTok.Line > 0 && typeEndTok.Line > 0 {
			fieldTypeRanges[fieldName] = rangeFromTokens(typeStartTok, typeEndTok)
		}

		if p.current.Type == TOKEN_COMMA || p.current.Type == TOKEN_SEMI {
			p.advance()
		}
	}

	rbraceTok = p.current
	p.expect(TOKEN_RBRACE)

	stmt := InterfaceStmt{
		Name:           name,
		TypeParameters: typeParams,
		Extends:        extends,
		Fields:         fields,
		File:           file,
		Line:           line,
		Column:         column,
	}
	stmt.Range = rangeFromTokens(startTok, rbraceTok)
	stmt.NameRange = nameRange
	stmt.FieldRanges = fieldRanges
	stmt.FieldNameRanges = fieldNameRanges
	stmt.FieldTypeRanges = fieldTypeRanges
	return stmt
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
		if p.ErrorTolerant {
			stmt := func() Stmt {
				defer func() {
					if r := recover(); r != nil {
						if err, ok := r.(LangErrorType); ok {
							p.Diagnostics = append(p.Diagnostics, err)
						} else {
							panic(r)
						}
						p.synchronizeBlock()
					}
				}()
				return p.parseStatement()
			}()
			if stmt != nil {
				statements = append(statements, stmt)
			}
		} else {
			stmt := p.parseStatement()
			if stmt != nil {
				statements = append(statements, stmt)
			}
		}
	}

	p.lastRbraceToken = p.current
	p.expect(TOKEN_RBRACE)

	return statements
}

func (p *Parser) parseImportStatement() Stmt {
	importStartTok := p.current
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_IMPORT)

	typeOnly := false
	if p.current.Type == TOKEN_IDENT && p.current.Literal == "type" {
		typeOnly = true
		p.advance()
	}

	if p.current.Type == TOKEN_IDENT && p.current.Literal == "std" {
		if typeOnly {
			LangErrorAt(ErrorSyntax, file, line, column, "import type only supports source files")
		}
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

		moduleNameTok := p.current
		moduleName := p.current.Literal
		p.advance()

		alias := moduleName
		aliasTok := Token{}

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

			aliasTok = p.current
			alias = p.current.Literal
			p.advance()
		}

		p.consumeTerminator()

		stmt := ImportStmt{
			Path:   moduleName,
			Std:    true,
			Alias:  alias,
			File:   file,
			Line:   line,
			Column: column,
		}
		stmt.Range = rangeFromTokens(importStartTok, p.current)
		stmt.PathRange = tokenRange(moduleNameTok)
		if aliasTok.Line > 0 {
			stmt.AliasRange = tokenRange(aliasTok)
		}
		return stmt
	} else if p.current.Type == TOKEN_IDENT && p.current.Literal == "plugin" {
		if typeOnly {
			LangErrorAt(ErrorSyntax, file, line, column, "import type only supports source files")
		}
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

		pluginPathTok := p.current
		pluginPath := p.current.Literal
		p.advance()

		alias := ""
		aliasTok := Token{}

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

			aliasTok = p.current
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

		stmt := ImportStmt{
			Path:   pluginPath,
			Plugin: true,
			Std:    false,
			Alias:  alias,
			File:   file,
			Line:   line,
			Column: column,
		}
		stmt.Range = rangeFromTokens(importStartTok, p.current)
		stmt.PathRange = tokenRange(pluginPathTok)
		if aliasTok.Line > 0 {
			stmt.AliasRange = tokenRange(aliasTok)
		}
		return stmt
	} else if p.current.Type == TOKEN_IDENT && (p.current.Literal == "lib" || p.current.Literal == "library") {
		if typeOnly {
			LangErrorAt(ErrorSyntax, file, line, column, "import type only supports source files")
		}
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

		libraryPathTok := p.current
		libraryPath := p.current.Literal
		p.advance()

		alias := ""
		aliasTok := Token{}

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

			aliasTok = p.current
			alias = p.current.Literal
			p.advance()
		}

		p.consumeTerminator()

		stmt := ImportStmt{
			Path:    libraryPath,
			Library: true,
			Alias:   alias,
			File:    file,
			Line:    line,
			Column:  column,
		}
		stmt.Range = rangeFromTokens(importStartTok, p.current)
		stmt.PathRange = tokenRange(libraryPathTok)
		if aliasTok.Line > 0 {
			stmt.AliasRange = tokenRange(aliasTok)
		}
		return stmt
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

	pathTok := p.current
	path := p.current.Literal
	p.advance()

	alias := ""
	aliasTok := Token{}

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

		aliasTok = p.current
		alias = p.current.Literal
		p.advance()
	}

	p.consumeTerminator()

	stmt := ImportStmt{
		Path:     path,
		Plugin:   false,
		Std:      false,
		TypeOnly: typeOnly,
		Alias:    alias,
		File:     file,
		Line:     line,
		Column:   column,
	}
	stmt.Range = rangeFromTokens(importStartTok, p.current)
	stmt.PathRange = tokenRange(pathTok)
	if aliasTok.Line > 0 {
		stmt.AliasRange = tokenRange(aliasTok)
	}
	return stmt
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

	if !isSoftIdentifierToken(p.current.Type) {
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

	value := Expr(NullExpr{})
	if p.current.Type == TOKEN_ASSIGN {
		p.advance()
		value = p.parseExpression()
	} else if constant {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected initializer for const field",
		)
	}

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
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_LET)

	if p.current.Type == TOKEN_LBRACE || p.current.Type == TOKEN_LBRACKET {
		pattern := p.parseDestructurePattern()
		p.expect(TOKEN_ASSIGN)
		value := p.parseExpression()
		p.consumeTerminator()
		return DestructureStmt{
			Target:   pattern,
			Value:    value,
			Constant: false,
			File:     file,
			Line:     line,
			Column:   column,
		}
	}

	if !isSoftIdentifierToken(p.current.Type) {
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

	value := Expr(NullExpr{})
	if p.current.Type == TOKEN_ASSIGN {
		p.advance()
		value = p.parseExpression()
	}

	p.consumeTerminator()

	return VariableStmt{
		Name:     name,
		Value:    value,
		Constant: false,
		TypeHint: typeHint,
		Line:     line,
		Column:   column,
		File:     file,
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
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_CONST)

	if p.current.Type == TOKEN_LBRACE || p.current.Type == TOKEN_LBRACKET {
		pattern := p.parseDestructurePattern()
		p.expect(TOKEN_ASSIGN)
		value := p.parseExpression()
		p.consumeTerminator()
		return DestructureStmt{
			Target:   pattern,
			Value:    value,
			Constant: true,
			File:     file,
			Line:     line,
			Column:   column,
		}
	}

	if !isSoftIdentifierToken(p.current.Type) {
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
		File:     file,
	}
}

func (p *Parser) parseDestructurePattern() DestructurePattern {
	if p.current.Type == TOKEN_LBRACE {
		return p.parseObjectDestructurePattern()
	}
	return p.parseArrayDestructurePattern()
}

func (p *Parser) parseObjectDestructurePattern() ObjectDestructurePattern {
	p.expect(TOKEN_LBRACE)

	pattern := ObjectDestructurePattern{}

	for p.current.Type != TOKEN_RBRACE && p.current.Type != TOKEN_EOF {
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			p.advance()
			if p.current.Type != TOKEN_IDENT {
				LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected variable name after ... in object destructuring")
			}
			pattern.Spread = p.current.Literal
			pattern.HasSpread = true
			p.advance()
			if p.current.Type == TOKEN_COMMA {
				p.advance()
			}
			break
		}

		if p.current.Type != TOKEN_IDENT && p.current.Type != TOKEN_STRING {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected property name in object destructuring")
		}

		key := p.current.Literal
		p.advance()

		field := ObjectDestructureField{Key: key}

		if p.current.Type == TOKEN_COLON {
			p.advance()
			if p.current.Type == TOKEN_LBRACE || p.current.Type == TOKEN_LBRACKET {
				field.Pattern = p.parseDestructurePattern()
				field.HasNested = true
				field.Alias = ""
				field.AliasIsRenamed = false
			} else if p.current.Type == TOKEN_IDENT {
				field.Alias = p.current.Literal
				field.AliasIsRenamed = true
				p.advance()
			} else {
				LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected variable name or nested pattern after ':' in object destructuring")
			}
		} else {
			field.Alias = key
			field.AliasIsRenamed = false
		}

		if p.current.Type == TOKEN_ASSIGN {
			p.advance()
			field.Default = p.parseExpression()
			field.HasDefault = true
		}

		pattern.Fields = append(pattern.Fields, field)

		if p.current.Type == TOKEN_COMMA {
			p.advance()
		}
	}

	p.expect(TOKEN_RBRACE)
	return pattern
}

func (p *Parser) parseArrayDestructurePattern() ArrayDestructurePattern {
	p.expect(TOKEN_LBRACKET)

	pattern := ArrayDestructurePattern{}

	for p.current.Type != TOKEN_RBRACKET && p.current.Type != TOKEN_EOF {
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			p.advance()
			if p.current.Type != TOKEN_IDENT {
				LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected variable name after ... in array destructuring")
			}
			pattern.Elements = append(pattern.Elements, ArrayDestructureElement{
				Name:     p.current.Literal,
				IsSpread: true,
			})
			p.advance()
			if p.current.Type == TOKEN_COMMA {
				p.advance()
			}
			break
		}

		if p.current.Type == TOKEN_LBRACE || p.current.Type == TOKEN_LBRACKET {
			elem := ArrayDestructureElement{HasNested: true}
			elem.Pattern = p.parseDestructurePattern()
			pattern.Elements = append(pattern.Elements, elem)
		} else if p.current.Type == TOKEN_IDENT {
			pattern.Elements = append(pattern.Elements, ArrayDestructureElement{
				Name: p.current.Literal,
			})
			p.advance()
		} else {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected variable name, nested pattern, or '...' in array destructuring")
		}

		if p.current.Type == TOKEN_COMMA {
			p.advance()
		}
	}

	p.expect(TOKEN_RBRACKET)
	return pattern
}

func (p *Parser) parseNativeFunctionStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_NATIVE)
	p.expect(TOKEN_FN)

	name := p.parseIdentifierLikeName("native function name")

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

func (p *Parser) parseExternalGlobalStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EXTERNAL)
	p.expect(TOKEN_CONST)

	name := p.parseIdentifierLikeName("external global name")

	var typ TypeHint

	if p.current.Type != TOKEN_COLON {
		typ = stdTypeHint("any")
		// LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "external global declarations require an explicit type. use \"any\" if it is unknown.")
	} else {
		typ = p.parseTypeHint(false)
	}

	p.consumeTerminator()

	return ExternalGlobalStmt{
		Name:   name,
		Type:   typ,
		File:   file,
		Line:   line,
		Column: column,
	}
}

func (p *Parser) parseExternalFunctionStatement() Stmt {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	p.expect(TOKEN_EXTERNAL)
	p.expect(TOKEN_FN)

	name := p.parseIdentifierLikeName("external function name")

	p.expect(TOKEN_LPAREN)
	params := p.parseParameterList(true)
	p.expect(TOKEN_RPAREN)

	if p.current.Type != TOKEN_COLON {
		LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "external function declarations require an explicit return type. use \"any\" if it is unknown.")
	}

	returnType := p.parseTypeHint(false)
	p.consumeTerminator()

	return ExternalFnStmt{
		Name:       name,
		Params:     params,
		ReturnType: returnType,
		File:       file,
		Line:       line,
		Column:     column,
	}
}

func (p *Parser) parseFunctionStatement(async bool, asyncTok ...Token) Stmt {
	startTok := p.current
	if async && len(asyncTok) > 0 {
		startTok = asyncTok[0]
	}

	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_FN)

	var name string
	var typeParams []string
	var lparenTok Token
	var rparenTok Token
	var lbraceTok Token
	var rbraceTok Token

	defer func() {
		if name == "" {
			return
		}
		p.recordBlock("fn", name, startTok, rbraceTok, async, typeParams, lparenTok, rparenTok, lbraceTok, rbraceTok)
	}()

	nameTok := p.current
	name = p.parseIdentifierLikeName("function name")

	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	lparenTok = p.current
	p.expect(TOKEN_LPAREN)
	params := p.parseParameterList()
	rparenTok = p.current
	p.expect(TOKEN_RPAREN)

	returnType := TypeHint{}
	returnTypeRange := SourceRange{}
	if p.current.Type == TOKEN_COLON {
		returnTypeStartTok := p.current
		returnType = p.parseTypeHint(false)
		returnTypeRange = rangeFromTokens(returnTypeStartTok, p.current)
	}

	var body []Stmt

	if p.current.Type != TOKEN_SEMI {
		lbraceTok = p.current
		body = p.parseBlock()
		rbraceTok = p.lastRbraceToken
	}

	endTok := rbraceTok
	if endTok.Line == 0 {
		endTok = p.current
	}

	stmt := FunctionStmt{
		Name:           name,
		TypeParameters: typeParams,
		Params:         params,
		ReturnType:     returnType,
		Body:           body,
		Async:          async,
		Line:           line,
		Column:         column,
		File:           file,
	}
	stmt.Range = rangeFromTokens(startTok, endTok)
	stmt.NameRange = tokenRange(nameTok)
	if lparenTok.Line > 0 && rparenTok.Line > 0 {
		stmt.ParamsRange = rangeFromTokens(lparenTok, rparenTok)
	}
	if returnTypeRange.Start.Line > 0 {
		stmt.ReturnTypeRange = returnTypeRange
	}
	return stmt
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
		paramStartTok := p.current
		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type == TOKEN_RPAREN {
			break
		}

		variadic := false
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			paramStartTok = p.current
			p.expect(TOKEN_DOT_DOT_DOT)
			variadic = true
		}

		if !isSoftIdentifierToken(p.current.Type) {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected parameter name",
			)
		}

		nameTok := p.current
		name := p.current.Literal
		p.advance()

		nullable := false

		if p.current.Type == TOKEN_QUESTION {
			p.advance()
			nullable = true
		}

		typeHint := TypeHint{}
		typeStartTok := Token{}
		typeEndTok := Token{}

		if enforceTypeChecks && p.current.Type != TOKEN_COLON {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "function parameter types are required")
		}

		if p.current.Type == TOKEN_COLON {
			p.advance()
			typeStartTok = p.current

			if p.current.Type == TOKEN_LBRACE {
				fields := p.parseStructuralTypeFields()
				typeHint = TypeHint{
					Name:   "{" + strings.Join(structuralTypeFieldStrings(fields), ", ") + "}",
					Fields: fields,
				}
				typeEndTok = p.current
			} else {
				types := []string{}

				for {
					typeEndTok = p.current
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
			if typeStartTok.Line > 0 && typeEndTok.Line > 0 {
				typeHint.Range = rangeFromTokens(typeStartTok, typeEndTok)
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
		paramEndTok := nameTok
		if typeEndTok.Line > 0 {
			paramEndTok = typeEndTok
		}
		param.Range = rangeFromTokens(paramStartTok, paramEndTok)
		param.NameRange = tokenRange(nameTok)
		if typeHint.Range.Start.Line > 0 {
			param.TypeRange = typeHint.Range
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

	if p.ErrorTolerant && isStatementStartKeyword(p.current.Type) {
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
	left := p.parseBitwise()

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

		right := p.parseBitwise()

		switch op {
		case TOKEN_INSTANCEOF:
			left = InstanceOfExpr{
				Object: left,
				Class:  right,
			}

		case TOKEN_IN:
			left = ObjectInExpr{
				Key:    left,
				Object: right,
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

func (p *Parser) parseBitwise() Expr {
	left := p.parseAddSub()

	for p.current.Type == TOKEN_AMP ||
		p.current.Type == TOKEN_PIPE ||
		p.current.Type == TOKEN_CARET ||
		p.current.Type == TOKEN_LSHIFT ||
		p.current.Type == TOKEN_RSHIFT {

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
	safeChain := false

	for {
		switch p.current.Type {
		case TOKEN_LPAREN:
			file, line, column := exprSourcePosition(expr)
			if line <= 0 || column <= 0 {
				file = p.current.File
				line = p.current.Line
				column = p.current.Column
			}

			p.advance()

			args := p.parseArgumentList()

			p.expect(TOKEN_RPAREN)

			expr = CallValueExpr{
				Callee: expr,
				Args:   args,
				File:   file,
				Line:   line,
				Column: column,
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
			if safe {
				safeChain = true
			}
			dotTok := p.current
			p.advance()

			if !isIdentifierLikeToken(p.current.Type) || (p.ErrorTolerant && isStatementStartKeyword(p.current.Type) && p.current.Line > dotTok.Line) {
				if p.ErrorTolerant {
					objFile, objLine, objCol := exprSourcePosition(expr)
					if objLine <= 0 || objCol <= 0 {
						objFile = p.current.File
						objLine = p.current.Line
						objCol = p.current.Column
					}
					expr = PropertyExpr{
						Object: expr,
						Name:   "",
						File:   objFile,
						Line:   objLine,
						Column: objCol,
						Range:  rangeFromTokens(dotTok, p.current),
					}
					break
				}
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
					Safe:   safe || safeChain,
				}

				continue
			}

			expr = PropertyExpr{
				Object: expr,
				Name:   name,
				File:   file,
				Line:   line,
				Column: column,
				Safe:   safe || safeChain,
				Range:  rangeFromTokens(dotTok, p.current),
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

func exprSourcePosition(expr Expr) (string, int, int) {
	switch e := expr.(type) {
	case NumberExpr:
		return e.File, e.Line, e.Column
	case FloatExpr:
		return e.File, e.Line, e.Column
	case IdentExpr:
		return e.File, e.Line, e.Column
	case CallExpr:
		return e.File, e.Line, e.Column
	case InstantiatedExpr:
		if file, line, column := exprSourcePosition(e.Object); line > 0 && column > 0 {
			return file, line, column
		}
		return e.File, e.Line, e.Column
	case CallValueExpr:
		if file, line, column := exprSourcePosition(e.Callee); line > 0 && column > 0 {
			return file, line, column
		}
		return e.File, e.Line, e.Column
	case MemberCallExpr:
		return e.File, e.Line, e.Column
	case PropertyExpr:
		return e.File, e.Line, e.Column
	case ObjectInExpr:
		return e.File, e.Line, e.Column
	case NullishCoalescingExpr:
		return e.File, e.Line, e.Column
	case AwaitExpr:
		return e.File, e.Line, e.Column
	case DeferExpr:
		return e.File, e.Line, e.Column
	case ThisExpr:
		return e.File, e.Line, e.Column
	case FunctionExpr:
		return e.File, e.Line, e.Column
	case EnumVariantExpr:
		return e.File, e.Line, e.Column
	}
	return "", 0, 0
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
	lbraceTok := p.current
	p.expect(TOKEN_LBRACE)

	var fields []ObjectField

	if p.current.Type == TOKEN_RBRACE {
		rbraceTok := p.current
		p.expect(TOKEN_RBRACE)
		expr := ObjectExpr{Fields: fields}
		expr.Range = rangeFromTokens(lbraceTok, rbraceTok)
		return expr
	}

	for {
		if p.current.Type == TOKEN_DOT_DOT_DOT {
			fieldStartTok := p.current
			p.advance()
			nameTok := p.current
			name := p.current.Literal
			tokenType := p.current.Type
			p.advance()

			if !isIdentifierLikeToken(tokenType) {
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

			field := ObjectField{
				Name:    name,
				Value:   nil,
				Copy:    value,
				HasCopy: true,
			}
			field.Range = rangeFromTokens(fieldStartTok, p.current)
			field.NameRange = tokenRange(nameTok)
			fields = append(fields, field)

			if p.current.Type == TOKEN_SEMI {
				p.advance()
			}

			if p.current.Type != TOKEN_COMMA {
				break
			}

			p.advance()

			continue
		}
		if !isIdentifierLikeToken(p.current.Type) && p.current.Type != TOKEN_STRING {
			LangErrorAt(
				ErrorSyntax,
				p.current.File,
				p.current.Line,
				p.current.Column,
				"expected object field name",
			)
		}

		fieldStartTok := p.current
		nameTok := p.current
		name := p.current.Literal
		tokenType := p.current.Type
		p.advance()

		if p.current.Type == TOKEN_COLON {
			p.expect(TOKEN_COLON)

			value := p.parseExpression()

			field := ObjectField{
				Name:  name,
				Value: value,
			}
			field.Range = rangeFromTokens(fieldStartTok, p.current)
			field.NameRange = tokenRange(nameTok)
			fields = append(fields, field)
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

			field := ObjectField{
				Name:  name,
				Value: value,
			}
			field.Range = rangeFromTokens(fieldStartTok, p.current)
			field.NameRange = tokenRange(nameTok)
			fields = append(fields, field)
		}

		for p.current.Type == TOKEN_SEMI {
			p.advance()
		}

		if p.current.Type != TOKEN_COMMA {
			break
		}

		p.advance()
	}

	rbraceTok := p.current
	p.expect(TOKEN_RBRACE)

	expr := ObjectExpr{Fields: fields}
	expr.Range = rangeFromTokens(lbraceTok, rbraceTok)
	return expr
}

func (p *Parser) parseFunctionSignatureAndBody() ([]Param, TypeHint, []Stmt, Token, Token) {
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
	}

	if p.current.Type == TOKEN_SEMI {
		return params, returnType, []Stmt{}, Token{}, Token{}
	}

	lbraceTok := p.current
	body := p.parseBlock()
	rbraceTok := p.lastRbraceToken

	return params, returnType, body, lbraceTok, rbraceTok
}

func (p *Parser) parsePrimary() Expr {
	if p.current.Type == TOKEN_IDENT && p.peek(1).Type == TOKEN_ARROW {
		return p.parseArrowFunctionExpr()
	}
	if p.current.Type == TOKEN_LPAREN && p.isArrowFunctionAhead() {
		return p.parseArrowFunctionExpr()
	}

	switch p.current.Type {
	case TOKEN_NUMBER:
		literal := p.current.Literal
		file := p.current.File
		line := p.current.Line
		column := p.current.Column

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

			return FloatExpr{Value: value, File: file, Line: line, Column: column}
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

		return NumberExpr{Value: value, File: file, Line: line, Column: column}

	case TOKEN_FN:
		return p.parseFunctionExpr()

	case TOKEN_LBRACKET:
		return p.parseArrayLiteral()

	case TOKEN_IDENT, TOKEN_EMBED, TOKEN_MATCH,
		TOKEN_FIELD, TOKEN_NATIVE, TOKEN_EXTERNAL,
		TOKEN_PRIVATE, TOKEN_PUBLIC,
		TOKEN_IMPLEMENTS, TOKEN_EXTENDS,
		TOKEN_IOTA,
		TOKEN_EMBED_TEXT, TOKEN_EMBED_BYTES, TOKEN_EMBED_FOLDER:
		tok := p.current
		file := p.current.File
		line := p.current.Line
		column := p.current.Column
		name := p.current.Literal
		p.advance()

		return IdentExpr{
			Name:   name,
			File:   file,
			Line:   line,
			Column: column,
			Range:  tokenRange(tok),
		}

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
		file := p.current.File
		line := p.current.Line
		column := p.current.Column + 1
		value := p.current.Literal
		p.advance()

		return parseInterpolatedString(value, file, line, column)

	case TOKEN_LBRACE:
		return p.parseObjectLiteral()

	case TOKEN_TRUE:
		p.advance()
		return BoolExpr{Value: true}

	case TOKEN_FALSE:
		p.advance()
		return BoolExpr{Value: false}

	case TOKEN_THIS:
		file := p.current.File
		line := p.current.Line
		column := p.current.Column
		p.advance()
		return ThisExpr{
			File:   file,
			Line:   line,
			Column: column,
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
	startTok := p.current
	p.expect(TOKEN_ENUM)

	var name string
	var lbraceTok Token
	var rbraceTok Token

	defer func() {
		if name == "" {
			return
		}
		p.recordBlock("enum", name, startTok, rbraceTok, false, nil, Token{}, Token{}, lbraceTok, rbraceTok)
	}()

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
	name = p.current.Literal
	p.advance()

	lbraceTok = p.current
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

		memberName := p.current.Literal
		p.advance()

		// Check for variant data: EnumVariant(args)
		if p.current.Type == TOKEN_LPAREN {
			p.advance()
			variantParams := []Param{}
			for p.current.Type != TOKEN_RPAREN && p.current.Type != TOKEN_EOF {
				if p.current.Type == TOKEN_COMMA {
					p.advance()
					continue
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
				paramName := p.current.Literal
				p.advance()
				paramType := TypeHint{Name: "any"}
				if p.current.Type == TOKEN_COLON {
					p.advance()
					paramType = TypeHint{Name: p.parseTypeName()}
				}
				variantParams = append(variantParams, Param{
					Name:     paramName,
					TypeHint: paramType,
				})
			}
			p.expect(TOKEN_RPAREN)
			members = append(members, EnumField{
				Name:          memberName,
				VariantParams: variantParams,
				Value:         StringExpr{Value: memberName},
			})
			for p.current.Type == TOKEN_SEMI {
				p.advance()
			}
			if p.current.Type == TOKEN_COMMA {
				p.advance()
			}
			continue
		}

		if p.current.Type == TOKEN_ASSIGN {
			p.advance()

			if p.current.Type == TOKEN_IOTA {
				if len(members) > 0 {
					LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "the 'iota' keyword can only be used for the first member of an enum.")
				}
				iotaEnum = true
				members = append(members, EnumField{
					Name:  memberName,
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
					Name:  memberName,
					Value: value,
				})
			}
		} else {
			// enforce the same type
			if !iotaEnum && len(members) > 0 {
				for _, v := range members {
					if !p.compareTwoConst(v.Value, StringExpr{Value: memberName}) {
						LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "all enum members must have the same type.")
					}
					break
				}
			}

			members = append(members, EnumField{
				Name:  memberName,
				Value: StringExpr{Value: memberName},
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

	rbraceTok = p.current
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

func (p *Parser) isArrowFunctionAhead() bool {
	if p.current.Type != TOKEN_LPAREN {
		return false
	}

	parenDepth := 0
	braceDepth := 0
	for i := 1; ; i++ {
		t := p.peek(i)
		switch t.Type {
		case TOKEN_LPAREN:
			parenDepth++
		case TOKEN_RPAREN:
			if parenDepth == 0 {
				next := p.peek(i + 1)
				if next.Type == TOKEN_ARROW {
					return true
				}
				if next.Type == TOKEN_COLON {
					for j := i + 2; ; j++ {
						t2 := p.peek(j)
						if t2.Type == TOKEN_ARROW {
							return true
						}
						if t2.Type == TOKEN_EOF || t2.Type == TOKEN_SEMI {
							return false
						}
					}
				}
				return false
			}
			parenDepth--
		case TOKEN_LBRACE:
			braceDepth++
		case TOKEN_RBRACE:
			if braceDepth > 0 {
				braceDepth--
			}
		case TOKEN_EOF, TOKEN_SEMI:
			return false
		}
	}
}

func (p *Parser) parseArrowFunctionExpr() Expr {
	file := p.current.File
	line := p.current.Line
	column := p.current.Column

	var params []Param

	if p.current.Type == TOKEN_LPAREN {
		p.expect(TOKEN_LPAREN)
		if p.current.Type != TOKEN_RPAREN {
			for {
				if !isSoftIdentifierToken(p.current.Type) {
					LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected parameter name")
				}
				paramName := p.current.Literal
				p.advance()

				var typeHint TypeHint
				if p.current.Type == TOKEN_COLON {
					p.expect(TOKEN_COLON)
					typeHint = p.parseTypeNameAsHint()
				}

				params = append(params, Param{
					Name:     paramName,
					TypeHint: typeHint,
				})

				if p.current.Type != TOKEN_COMMA {
					break
				}
				p.advance()
			}
		}
		p.expect(TOKEN_RPAREN)
	} else {
		paramName := p.current.Literal
		p.advance()

		var typeHint TypeHint
		if p.current.Type == TOKEN_COLON {
			p.expect(TOKEN_COLON)
			typeHint = p.parseTypeNameAsHint()
		}

		params = append(params, Param{
			Name:     paramName,
			TypeHint: typeHint,
		})
	}

	var returnType TypeHint
	if p.current.Type == TOKEN_COLON {
		p.expect(TOKEN_COLON)
		returnType = p.parseTypeNameAsHint()
	}

	p.expect(TOKEN_ARROW)

	bodyExpr := p.parseExpression()

	body := []Stmt{
		ReturnStmt{
			Value:    bodyExpr,
			HasValue: true,
			File:     file,
			Line:     line,
			Column:   column,
		},
	}

	return FunctionExpr{
		Params:     params,
		ReturnType: returnType,
		Body:       body,
		File:       file,
		Line:       line,
		Column:     column,
	}
}

func (p *Parser) parseTypeNameAsHint() TypeHint {
	types := []string{}
	types = append(types, p.parseTypeName())

	for p.current.Type == TOKEN_PIPE {
		p.advance()
		types = append(types, p.parseTypeName())
	}

	if len(types) == 1 {
		return TypeHint{Name: types[0]}
	}
	return TypeHint{
		Name:  strings.Join(types, " | "),
		Types: types,
	}
}

func (p *Parser) parseAsyncStmt() Stmt {
	asyncTok := p.current
	p.expect(TOKEN_ASYNC)

	return p.parseFunctionStatement(true, asyncTok)
}

func (p *Parser) parseFunctionExpr() Expr {
	startTok := p.current
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_FN)

	if p.current.Type == TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"anonymous functions must not have a name",
		)
	}

	var lbraceTok Token
	var rbraceTok Token

	defer func() {
		p.recordBlock("fn", "", startTok, rbraceTok, false, nil, Token{}, Token{}, lbraceTok, rbraceTok)
	}()

	params, returnType, body, lbr, rbr := p.parseFunctionSignatureAndBody()
	lbraceTok = lbr
	rbraceTok = rbr

	return FunctionExpr{
		Params:     params,
		ReturnType: returnType,
		Body:       body,
		File:       file,
		Line:       line,
		Column:     column,
	}
}

func (p *Parser) parseClassStatement() Stmt {
	startTok := p.current
	file := p.current.File
	line := p.current.Line
	column := p.current.Column
	p.expect(TOKEN_CLASS)

	var name string
	var typeParams []string
	var lbraceTok Token
	var rbraceTok Token

	defer func() {
		if name == "" {
			return
		}
		p.recordBlock("class", name, startTok, rbraceTok, false, typeParams, Token{}, Token{}, lbraceTok, rbraceTok)
	}()

	if p.current.Type != TOKEN_IDENT {
		LangErrorAt(
			ErrorSyntax,
			p.current.File,
			p.current.Line,
			p.current.Column,
			"expected class name after class",
		)
	}

	name = p.current.Literal
	p.advance()

	for p.current.Type == TOKEN_COLON {
		p.advance()
		if p.current.Type != TOKEN_IDENT {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected type parameter name")
		}
		typeParams = append(typeParams, p.current.Literal)
		p.advance()
	}

	implements := []string{}
	if p.current.Type == TOKEN_IMPLEMENTS {
		p.advance()
		for {
			implements = append(implements, p.parseTypeName())
			if p.current.Type != TOKEN_COMMA {
				break
			}
			p.advance()
		}
	}

	lbraceTok = p.current
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
		var asyncTok Token

		if p.current.Type == TOKEN_ASYNC {
			asyncTok = p.current
			p.advance()
			async = true
		}

		if p.current.Type != TOKEN_FN {
			LangErrorAt(ErrorSyntax, p.current.File, p.current.Line, p.current.Column, "expected declared variable, method or embed in class")
		}

		method := p.parseFunctionStatement(async, asyncTok)

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

	rbraceTok = p.current
	p.expect(TOKEN_RBRACE)

	return ClassStmt{
		Name:           name,
		TypeParameters: typeParams,
		Implements:     implements,
		Methods:        methods,
		Embeds:         embeds,
		Fields:         fields,
		File:           file,
		Line:           line,
		Column:         column,
	}
}

func (p *Parser) parseUnary() Expr {
	switch p.current.Type {
	case TOKEN_MINUS, TOKEN_BANG, TOKEN_TILDE:
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
