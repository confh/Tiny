package main

import (
	"unicode"
)

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
	l.skipIgnored()

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
		case "while":
			return Token{Type: TOKEN_WHILE, Literal: word}
		case "and":
			return Token{Type: TOKEN_AND, Literal: word}
		case "or":
			return Token{Type: TOKEN_OR, Literal: word}
		case "true":
			return Token{Type: TOKEN_TRUE, Literal: word}
		case "false":
			return Token{Type: TOKEN_FALSE, Literal: word}
		case "this":
			return Token{Type: TOKEN_THIS, Literal: word}
		case "null":
			return Token{Type: TOKEN_NULL, Literal: word}
		case "undefined":
			return Token{Type: TOKEN_UNDEFINED, Literal: word}
		case "class":
			return Token{Type: TOKEN_CLASS, Literal: word}
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

		langError(ErrorSyntax, "unexpected character: !")

		return Token{}

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
	case ':':
		l.pos++
		return Token{Type: TOKEN_COLON, Literal: ":"}
	case '[':
		l.pos++
		return Token{Type: TOKEN_LBRACKET, Literal: "["}
	case ']':
		l.pos++
		return Token{Type: TOKEN_RBRACKET, Literal: "]"}
	default:
		langError(ErrorSyntax, "unknown character: %q", ch)
		return Token{}
	}
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
		langError(ErrorSyntax, "unterminated string")
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
		langError(ErrorSyntax, "unterminated interpolated string")

	}

	value := string(l.input[start:l.pos])

	l.pos++ // skip closing `

	return value
}
