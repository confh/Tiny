package vm

import (
	"unicode"

	. "language.com/src/tinyerrors"
)

type Lexer struct {
	input []rune
	pos   int

	file   string
	line   int
	column int
}

func NewLexer(input string, file string) *Lexer {
	l := &Lexer{
		input:  []rune(input),
		file:   file,
		line:   1,
		column: 1,
	}

	return l
}

func (l *Lexer) advance() rune {
	if l.pos >= len(l.input) {
		return 0
	}

	ch := rune(l.input[l.pos])
	l.pos++

	if ch == '\n' {
		l.line++
		l.column = 1
	} else {
		l.column++
	}

	return ch
}

func (l *Lexer) peek() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}

	return l.input[l.pos+1]
}

func (l *Lexer) makeToken(tokenType TokenType, literal string, start int) Token {
	return Token{
		Type:    tokenType,
		Literal: literal,
		File:    l.file,
		Line:    l.line,
		Column:  start + 1,
	}
}

func (l *Lexer) NextToken() Token {
	l.skipIgnored()

	start := l.pos

	if l.pos >= len(l.input) {
		return l.tokenAt(start, TOKEN_EOF, "")
	}

	ch := l.input[l.pos]

	if unicode.IsLetter(ch) || ch == '_' {
		line := l.line
		column := l.column

		word := l.readIdentifier()

		tok := Token{
			Literal: word,
			File:    l.file,
			Line:    line,
			Column:  column,
		}

		switch word {
		case "import":
			tok.Type = TOKEN_IMPORT
		case "let":
			tok.Type = TOKEN_LET
		case "const":
			tok.Type = TOKEN_CONST
		case "fn":
			tok.Type = TOKEN_FN
		case "return":
			tok.Type = TOKEN_RETURN
		case "throw":
			tok.Type = TOKEN_THROW
		case "try":
			tok.Type = TOKEN_TRY
		case "catch":
			tok.Type = TOKEN_CATCH
		case "if":
			tok.Type = TOKEN_IF
		case "else":
			tok.Type = TOKEN_ELSE
		case "while":
			tok.Type = TOKEN_WHILE
		case "for":
			tok.Type = TOKEN_FOR
		case "break":
			tok.Type = TOKEN_BREAK
		case "continue":
			tok.Type = TOKEN_CONTINUE
		case "and":
			tok.Type = TOKEN_AND
		case "or":
			tok.Type = TOKEN_OR
		case "true":
			tok.Type = TOKEN_TRUE
		case "false":
			tok.Type = TOKEN_FALSE
		case "this":
			tok.Type = TOKEN_THIS
		case "null":
			tok.Type = TOKEN_NULL
		case "undefined":
			tok.Type = TOKEN_UNDEFINED
		case "class":
			tok.Type = TOKEN_CLASS
		case "enum":
			tok.Type = TOKEN_ENUM
		case "export":
			tok.Type = TOKEN_EXPORT
		case "match":
			tok.Type = TOKEN_MATCH
		case "in":
			tok.Type = TOKEN_IN
		case "typeof":
			tok.Type = TOKEN_TYPEOF
		case "spawn":
			tok.Type = TOKEN_SPAWN
		case "embed":
			tok.Type = TOKEN_EMBED
		case "instanceof":
			tok.Type = TOKEN_INSTANCEOF
		default:
			tok.Type = TOKEN_IDENT
		}

		return tok
	}

	if unicode.IsDigit(ch) {
		num := l.readNumber()
		return l.tokenAt(start, TOKEN_NUMBER, num)
	}

	if ch == '"' {
		str := l.readString()
		return l.tokenAt(start, TOKEN_STRING, str)
	}

	if ch == '`' {
		str := l.readBacktickString()
		return l.tokenAt(start, TOKEN_BACKTICK_STRING, str)
	}

	switch ch {
	case '?':
		tok := Token{
			Type:    TOKEN_QUESTION,
			Literal: "?",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '%':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_PERCENT_ASSIGN, "%=")
		}
		tok := Token{
			Type:    TOKEN_PERCENT,
			Literal: "%",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '=':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_EQ, "==")
		}
		tok := Token{
			Type:    TOKEN_ASSIGN,
			Literal: "=",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '!':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_NEQ, "!=")
		}
		tok := Token{
			Type:    TOKEN_BANG,
			Literal: "!",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '<':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_LTE, "<=")
		}
		tok := Token{
			Type:    TOKEN_LT,
			Literal: "<",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '>':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_GTE, ">=")
		}
		tok := Token{
			Type:    TOKEN_GT,
			Literal: ">",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '+':
		if l.peek() == '+' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_INCREMENT, "++")
		} else if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_PLUS_ASSIGN, "+=")
		}
		tok := Token{
			Type:    TOKEN_PLUS,
			Literal: "+",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '-':
		if l.peek() == '-' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_DECREMENT, "--")
		} else if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_MINUS_ASSIGN, "-=")
		}
		tok := Token{
			Type:    TOKEN_MINUS,
			Literal: "-",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '*':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_STAR_ASSIGN, "*=")
		}
		tok := Token{
			Type:    TOKEN_STAR,
			Literal: "*",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '/':
		if l.peek() == '=' {
			l.pos += 2
			l.column += 2
			return l.tokenAt(start, TOKEN_SLASH_ASSIGN, "/=")
		}
		tok := Token{
			Type:    TOKEN_SLASH,
			Literal: "/",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok

	case '(':
		tok := Token{
			Type:    TOKEN_LPAREN,
			Literal: "(",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case ')':
		tok := Token{
			Type:    TOKEN_RPAREN,
			Literal: ")",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case '{':
		tok := Token{
			Type:    TOKEN_LBRACE,
			Literal: "{",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case '}':
		tok := Token{
			Type:    TOKEN_RBRACE,
			Literal: "}",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case ',':
		tok := Token{
			Type:    TOKEN_COMMA,
			Literal: ",",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case ';':
		tok := Token{
			Type:    TOKEN_SEMI,
			Literal: ";",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case '.':
		tok := Token{
			Type:    TOKEN_DOT,
			Literal: ".",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case ':':
		tok := Token{
			Type:    TOKEN_COLON,
			Literal: ":",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case '[':
		tok := Token{
			Type:    TOKEN_LBRACKET,
			Literal: "[",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	case ']':
		tok := Token{
			Type:    TOKEN_RBRACKET,
			Literal: "]",
			File:    l.file,
			Line:    l.line,
			Column:  l.column,
		}
		l.advance()
		return tok
	default:
		line, column := l.lineColumnAt(start)
		LangErrorAt(ErrorSyntax, l.file, line, column, "unknown character: %q", ch)
		return Token{}
	}
}

func (l *Lexer) tokenAt(pos int, tokenType TokenType, literal string) Token {
	line, column := l.lineColumnAt(pos)

	return Token{
		Type:    tokenType,
		Literal: literal,
		File:    l.file,
		Line:    line,
		Column:  column,
	}
}

func (l *Lexer) lineColumnAt(pos int) (int, int) {
	line := 1
	column := 1

	for i := 0; i < pos && i < len(l.input); i++ {
		if l.input[i] == '\n' {
			line++
			column = 1
		} else {
			column++
		}
	}

	return line, column
}

func (l *Lexer) skipIgnored() {
	for {
		// Skip whitespace
		for l.pos < len(l.input) && unicode.IsSpace(l.input[l.pos]) {
			l.advance()
		}

		// Skip // comments
		if l.pos < len(l.input) && l.input[l.pos] == '/' && l.peek() == '/' {
			l.pos += 2

			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.advance()
			}

			continue
		}

		break
	}
}

func (l *Lexer) readIdentifier() string {
	start := l.pos

	for l.pos < len(l.input) {
		ch := l.input[l.pos]
		if !unicode.IsLetter(ch) && !unicode.IsDigit(ch) && ch != '_' {
			break
		}
		l.advance()
	}

	return string(l.input[start:l.pos])
}

func (l *Lexer) readNumber() string {
	start := l.pos
	hasDot := false

	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		if unicode.IsDigit(ch) {
			l.advance()
			continue
		}

		if ch == '.' && !hasDot {
			// Only treat "." as part of a number if the next char is a digit.
			if l.pos+1 < len(l.input) && unicode.IsDigit(l.input[l.pos+1]) {
				hasDot = true
				l.advance()
				continue
			}
		}

		break
	}

	return string(l.input[start:l.pos])
}

func (l *Lexer) readString() string {
	// skip opening "
	l.advance()

	var result []rune

	for l.pos < len(l.input) {
		ch := rune(l.input[l.pos])

		if ch == '"' {
			l.advance()
			return string(result)
		}

		if ch == '\\' {
			l.advance()

			if l.pos >= len(l.input) {
				LangError(ErrorSyntax, "unterminated escape sequence in string")
			}

			esc := rune(l.input[l.pos])

			switch esc {
			case 'n':
				result = append(result, '\n')
			case 'r':
				result = append(result, '\r')
			case 't':
				result = append(result, '\t')
			case '\\':
				result = append(result, '\\')
			case '"':
				result = append(result, '"')
			case '0':
				result = append(result, '\x00')
			default:
				LangError(ErrorSyntax, "unknown escape sequence: \\%c", esc)
			}

			l.advance()
			continue
		}

		result = append(result, ch)
		l.advance()
	}

	LangError(ErrorSyntax, "unterminated string")
	return ""
}

func (l *Lexer) readBacktickString() string {
	l.advance() // skip opening `

	start := l.pos

	for l.pos < len(l.input) && l.input[l.pos] != '`' {
		l.advance()
	}

	if l.pos >= len(l.input) {
		line, column := l.lineColumnAt(start)
		LangErrorAt(ErrorSyntax, l.file, line, column, "unterminated interpolated string")
	}

	value := string(l.input[start:l.pos])

	l.advance() // skip closing `

	return value
}
