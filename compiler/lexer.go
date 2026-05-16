package main

import (
	"unicode"
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

func (l *Lexer) peek() rune {
	if l.pos+1 >= len(l.input) {
		return 0
	}

	return l.input[l.pos+1]
}

func (l *Lexer) NextToken() Token {
	l.skipIgnored()

	start := l.pos

	if l.pos >= len(l.input) {
		return l.tokenAt(start, TOKEN_EOF, "")
	}

	ch := l.input[l.pos]

	if unicode.IsLetter(ch) || ch == '_' {
		word := l.readIdentifier()

		switch word {
		case "import":
			return l.tokenAt(start, TOKEN_IMPORT, word)
		case "let":
			return l.tokenAt(start, TOKEN_LET, word)
		case "const":
			return l.tokenAt(start, TOKEN_CONST, word)
		case "fn":
			return l.tokenAt(start, TOKEN_FN, word)
		case "return":
			return l.tokenAt(start, TOKEN_RETURN, word)
		case "throw":
			return l.tokenAt(start, TOKEN_THROW, word)
		case "try":
			return l.tokenAt(start, TOKEN_TRY, word)
		case "catch":
			return l.tokenAt(start, TOKEN_CATCH, word)
		case "if":
			return l.tokenAt(start, TOKEN_IF, word)
		case "else":
			return l.tokenAt(start, TOKEN_ELSE, word)
		case "while":
			return l.tokenAt(start, TOKEN_WHILE, word)
		case "for":
			return l.tokenAt(start, TOKEN_FOR, word)
		case "break":
			return l.tokenAt(start, TOKEN_BREAK, word)
		case "continue":
			return l.tokenAt(start, TOKEN_CONTINUE, word)
		case "and":
			return l.tokenAt(start, TOKEN_AND, word)
		case "or":
			return l.tokenAt(start, TOKEN_OR, word)
		case "true":
			return l.tokenAt(start, TOKEN_TRUE, word)
		case "false":
			return l.tokenAt(start, TOKEN_FALSE, word)
		case "this":
			return l.tokenAt(start, TOKEN_THIS, word)
		case "null":
			return l.tokenAt(start, TOKEN_NULL, word)
		case "undefined":
			return l.tokenAt(start, TOKEN_UNDEFINED, word)
		case "class":
			return l.tokenAt(start, TOKEN_CLASS, word)
		case "enum":
			return l.tokenAt(start, TOKEN_ENUM, word)
		case "export":
			return l.tokenAt(start, TOKEN_EXPORT, word)
		default:
			return l.tokenAt(start, TOKEN_IDENT, word)
		}
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
	case '%':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_PERCENT_ASSIGN, "%=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_PERCENT, "%")
	case '=':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_EQ, "==")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_ASSIGN, "=")

	case '!':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_NEQ, "!=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_LET, "!")

	case '<':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_LTE, "<=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_LT, "<")

	case '>':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_GTE, ">=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_GT, ">")

	case '+':
		if l.peek() == '+' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_INCREMENT, "++")
		} else if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_PLUS_ASSIGN, "+=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_PLUS, "+")
	case '-':
		if l.peek() == '-' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_DECREMENT, "--")
		} else if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_MINUS_ASSIGN, "-=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_MINUS, "-")
	case '*':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_STAR_ASSIGN, "*=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_STAR, "*")
	case '/':
		if l.peek() == '=' {
			l.pos += 2
			return l.tokenAt(start, TOKEN_SLASH_ASSIGN, "/=")
		}

		l.pos++
		return l.tokenAt(start, TOKEN_SLASH, "/")
	case '(':
		l.pos++
		return l.tokenAt(start, TOKEN_LPAREN, "(")
	case ')':
		l.pos++
		return l.tokenAt(start, TOKEN_RPAREN, ")")
	case '{':
		l.pos++
		return l.tokenAt(start, TOKEN_LBRACE, "{")
	case '}':
		l.pos++
		return l.tokenAt(start, TOKEN_RBRACE, "}")
	case ',':
		l.pos++
		return l.tokenAt(start, TOKEN_COMMA, ",")
	case ';':
		l.pos++
		return l.tokenAt(start, TOKEN_SEMI, ";")
	case '.':
		l.pos++
		return l.tokenAt(start, TOKEN_DOT, ".")
	case ':':
		l.pos++
		return l.tokenAt(start, TOKEN_COLON, ":")
	case '[':
		l.pos++
		return l.tokenAt(start, TOKEN_LBRACKET, "[")
	case ']':
		l.pos++
		return l.tokenAt(start, TOKEN_RBRACKET, "]")
	default:
		line, column := l.lineColumnAt(start)
		langErrorAt(ErrorSyntax, l.file, line, column, "unknown character: %q", ch)
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
			l.pos++
		}

		// Skip // comments
		if l.pos < len(l.input) && l.input[l.pos] == '/' && l.peek() == '/' {
			l.pos += 2

			for l.pos < len(l.input) && l.input[l.pos] != '\n' {
				l.pos++
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
		l.pos++
	}

	return string(l.input[start:l.pos])
}

func (l *Lexer) readNumber() string {
	start := l.pos
	hasDot := false

	for l.pos < len(l.input) {
		ch := l.input[l.pos]

		if unicode.IsDigit(ch) {
			l.pos++
			continue
		}

		if ch == '.' && !hasDot {
			// Only treat "." as part of a number if the next char is a digit.
			if l.pos+1 < len(l.input) && unicode.IsDigit(l.input[l.pos+1]) {
				hasDot = true
				l.pos++
				continue
			}
		}

		break
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
		line, column := l.lineColumnAt(start)
		langErrorAt(ErrorSyntax, l.file, line, column, "unterminated string")
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
		line, column := l.lineColumnAt(start)
		langErrorAt(ErrorSyntax, l.file, line, column, "unterminated interpolated string")
	}

	value := string(l.input[start:l.pos])

	l.pos++ // skip closing `

	return value
}
