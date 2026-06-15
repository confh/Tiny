package vm

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

type tinyJSONParser struct {
	src string
	pos int
}

func parseTinyJSONDirect(src string) (TinyValue, error) {
	p := tinyJSONParser{src: src}

	p.skipWhitespace()

	value, err := p.parseValue()
	if err != nil {
		return NewNull(), err
	}

	p.skipWhitespace()

	if !p.done() {
		return NewNull(), p.errorf("unexpected trailing character %q", p.peek())
	}

	return value, nil
}

func (p *tinyJSONParser) done() bool {
	return p.pos >= len(p.src)
}

func (p *tinyJSONParser) peek() byte {
	if p.done() {
		return 0
	}
	return p.src[p.pos]
}

func (p *tinyJSONParser) next() byte {
	if p.done() {
		return 0
	}
	ch := p.src[p.pos]
	p.pos++
	return ch
}

func (p *tinyJSONParser) errorf(format string, args ...any) error {
	return fmt.Errorf("invalid JSON at byte %d: %s", p.pos, fmt.Sprintf(format, args...))
}

func (p *tinyJSONParser) skipWhitespace() {
	for !p.done() {
		switch p.src[p.pos] {
		case ' ', '\n', '\r', '\t':
			p.pos++
		default:
			return
		}
	}
}

func (p *tinyJSONParser) parseValue() (TinyValue, error) {
	p.skipWhitespace()

	if p.done() {
		return NewNull(), p.errorf("expected value")
	}

	switch p.peek() {
	case '{':
		return p.parseObject()

	case '[':
		return p.parseArray()

	case '"':
		s, err := p.parseString()
		if err != nil {
			return NewNull(), err
		}
		return NewNative(s), nil

	case 't':
		if p.consumeLiteral("true") {
			return NewNative(true), nil
		}
		return NewNull(), p.errorf("invalid literal")

	case 'f':
		if p.consumeLiteral("false") {
			return NewNative(false), nil
		}
		return NewNull(), p.errorf("invalid literal")

	case 'n':
		if p.consumeLiteral("null") {
			return NewNull(), nil
		}
		return NewNull(), p.errorf("invalid literal")

	default:
		ch := p.peek()
		if ch == '-' || (ch >= '0' && ch <= '9') {
			return p.parseNumber()
		}
		return NewNull(), p.errorf("unexpected character %q", ch)
	}
}

func (p *tinyJSONParser) consumeLiteral(lit string) bool {
	if len(p.src)-p.pos < len(lit) {
		return false
	}
	if p.src[p.pos:p.pos+len(lit)] != lit {
		return false
	}
	p.pos += len(lit)
	return true
}

func (p *tinyJSONParser) parseObject() (TinyValue, error) {
	p.next() // {

	obj := make(ObjectValue)

	p.skipWhitespace()

	if p.peek() == '}' {
		p.next()
		return NewNative(obj), nil
	}

	for {
		p.skipWhitespace()

		if p.peek() != '"' {
			return NewNull(), p.errorf("expected object key string")
		}

		key, err := p.parseString()
		if err != nil {
			return NewNull(), err
		}

		p.skipWhitespace()

		if p.next() != ':' {
			return NewNull(), p.errorf("expected ':' after object key")
		}

		value, err := p.parseValue()
		if err != nil {
			return NewNull(), err
		}

		obj[key] = value

		p.skipWhitespace()

		ch := p.next()
		switch ch {
		case '}':
			return NewNative(obj), nil

		case ',':
			continue

		default:
			return NewNull(), p.errorf("expected ',' or '}', got %q", ch)
		}
	}
}

func (p *tinyJSONParser) parseArray() (TinyValue, error) {
	p.next() // [

	elements := make([]TinyValue, 0, 8)

	p.skipWhitespace()

	if p.peek() == ']' {
		p.next()
		return NewNative(&ArrayValue{Elements: elements}), nil
	}

	for {
		value, err := p.parseValue()
		if err != nil {
			return NewNull(), err
		}

		elements = append(elements, value)

		p.skipWhitespace()

		ch := p.next()
		switch ch {
		case ']':
			return NewNative(&ArrayValue{Elements: elements}), nil

		case ',':
			continue

		default:
			return NewNull(), p.errorf("expected ',' or ']', got %q", ch)
		}
	}
}

func (p *tinyJSONParser) parseString() (string, error) {
	if p.next() != '"' {
		return "", p.errorf("expected string")
	}

	start := p.pos

	for !p.done() {
		ch := p.next()

		switch ch {
		case '"':
			// Fast path: no escapes.
			return p.src[start : p.pos-1], nil

		case '\\':
			// Slow path: escaped string.
			p.pos = start
			return p.parseEscapedString()

		default:
			if ch < 0x20 {
				return "", p.errorf("invalid control character in string")
			}
		}
	}

	return "", p.errorf("unterminated string")
}

func (p *tinyJSONParser) parseEscapedString() (string, error) {
	var b strings.Builder

	for !p.done() {
		ch := p.next()

		switch ch {
		case '"':
			return b.String(), nil

		case '\\':
			if p.done() {
				return "", p.errorf("unterminated escape")
			}

			esc := p.next()

			switch esc {
			case '"', '\\', '/':
				b.WriteByte(esc)

			case 'b':
				b.WriteByte('\b')

			case 'f':
				b.WriteByte('\f')

			case 'n':
				b.WriteByte('\n')

			case 'r':
				b.WriteByte('\r')

			case 't':
				b.WriteByte('\t')

			case 'u':
				r, err := p.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				b.WriteRune(r)

			default:
				return "", p.errorf("invalid escape \\%c", esc)
			}

		default:
			if ch < 0x20 {
				return "", p.errorf("invalid control character in string")
			}
			b.WriteByte(ch)
		}
	}

	return "", p.errorf("unterminated string")
}

func (p *tinyJSONParser) parseUnicodeEscape() (rune, error) {
	r1, err := p.readHex4()
	if err != nil {
		return 0, err
	}

	// Handle UTF-16 surrogate pairs.
	if utf16.IsSurrogate(r1) {
		if len(p.src)-p.pos < 6 || p.src[p.pos] != '\\' || p.src[p.pos+1] != 'u' {
			return utf8.RuneError, nil
		}

		old := p.pos
		p.pos += 2

		r2, err := p.readHex4()
		if err != nil {
			p.pos = old
			return utf8.RuneError, nil
		}

		decoded := utf16.DecodeRune(r1, r2)
		if decoded == utf8.RuneError {
			return utf8.RuneError, nil
		}

		return decoded, nil
	}

	return r1, nil
}

func (p *tinyJSONParser) readHex4() (rune, error) {
	if len(p.src)-p.pos < 4 {
		return 0, p.errorf("incomplete unicode escape")
	}

	var value rune

	for i := 0; i < 4; i++ {
		ch := p.next()

		var digit rune
		switch {
		case ch >= '0' && ch <= '9':
			digit = rune(ch - '0')
		case ch >= 'a' && ch <= 'f':
			digit = rune(ch-'a') + 10
		case ch >= 'A' && ch <= 'F':
			digit = rune(ch-'A') + 10
		default:
			return 0, p.errorf("invalid unicode escape")
		}

		value = value*16 + digit
	}

	return value, nil
}

func (p *tinyJSONParser) parseNumber() (TinyValue, error) {
	start := p.pos

	if p.peek() == '-' {
		p.pos++
		if p.done() {
			return NewNull(), p.errorf("invalid number")
		}
	}

	if p.peek() == '0' {
		p.pos++
	} else if p.peek() >= '1' && p.peek() <= '9' {
		for !p.done() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
	} else {
		return NewNull(), p.errorf("invalid number")
	}

	isFloat := false

	if !p.done() && p.peek() == '.' {
		isFloat = true
		p.pos++

		if p.done() || p.peek() < '0' || p.peek() > '9' {
			return NewNull(), p.errorf("invalid number fraction")
		}

		for !p.done() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
	}

	if !p.done() && (p.peek() == 'e' || p.peek() == 'E') {
		isFloat = true
		p.pos++

		if !p.done() && (p.peek() == '+' || p.peek() == '-') {
			p.pos++
		}

		if p.done() || p.peek() < '0' || p.peek() > '9' {
			return NewNull(), p.errorf("invalid number exponent")
		}

		for !p.done() && p.peek() >= '0' && p.peek() <= '9' {
			p.pos++
		}
	}

	raw := p.src[start:p.pos]

	if !isFloat {
		i, err := strconv.Atoi(raw)
		if err == nil {
			return TinyValue{IsInt: true, AsInt: i}, nil
		}
	}

	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return NewNull(), p.errorf("invalid number")
	}

	if math.IsNaN(f) || math.IsInf(f, 0) {
		return NewNull(), p.errorf("invalid number")
	}

	return NewNative(f), nil
}
