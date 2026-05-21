package main

import (
	"strings"
	"unicode"
)

func fullDocumentRange(text string) LSPRange {
	lines := strings.Split(text, "\n")

	lastLine := len(lines) - 1
	lastChar := 0

	if lastLine >= 0 {
		lastChar = len(strings.TrimSuffix(lines[lastLine], "\r"))
	}

	return LSPRange{
		Start: Position{
			Line:      0,
			Character: 0,
		},
		End: Position{
			Line:      lastLine,
			Character: lastChar,
		},
	}
}

func formatTinyDocument(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")

	lines := strings.Split(text, "\n")

	formatted := []string{}
	indent := 0

	for _, raw := range lines {
		line := strings.TrimSpace(raw)

		if line == "" {
			formatted = append(formatted, "")
			continue
		}

		line = formatTinyLine(line)

		leadingClosings := countLeadingClosingBraces(line)

		indent -= leadingClosings
		if indent < 0 {
			indent = 0
		}

		formatted = append(formatted, strings.Repeat("    ", indent)+line)

		opens, closes := countBracesOutsideStrings(line)

		// We already handled leading closing braces before printing,
		// so do NOT subtract them again.
		closes -= leadingClosings
		if closes < 0 {
			closes = 0
		}

		indent += opens - closes
		if indent < 0 {
			indent = 0
		}
	}

	result := strings.Join(formatted, "\n")

	if strings.HasSuffix(text, "\n") && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	return result
}

func countLeadingClosingBraces(line string) int {
	line = strings.TrimSpace(line)

	count := 0

	for _, ch := range line {
		if ch == '}' {
			count++
			continue
		}

		break
	}

	return count
}

func countBracesOutsideStrings(line string) (int, int) {
	code := stripLineCommentAware(line)

	opens := 0
	closes := 0

	inString := false
	stringQuote := rune(0)
	escaped := false

	for _, ch := range code {
		if inString {
			if escaped {
				escaped = false
				continue
			}

			if ch == '\\' {
				escaped = true
				continue
			}

			if ch == stringQuote {
				inString = false
			}

			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringQuote = ch
			continue
		}

		switch ch {
		case '{':
			opens++
		case '}':
			closes++
		}
	}

	return opens, closes
}

func formatTinyLine(line string) string {
	code, comment := splitCodeAndComment(line)

	code = strings.TrimSpace(code)
	code = spaceOperatorsOutsideStrings(code)
	code = cleanupTinySpaces(code)

	if comment != "" {
		if code == "" {
			return comment
		}

		return code + " " + comment
	}

	return code
}

func splitCodeAndComment(line string) (string, string) {
	inString := false
	stringQuote := rune(0)
	escaped := false

	runes := []rune(line)

	for i := 0; i < len(runes)-1; i++ {
		ch := runes[i]
		next := runes[i+1]

		if inString {
			if escaped {
				escaped = false
				continue
			}

			if ch == '\\' {
				escaped = true
				continue
			}

			if ch == stringQuote {
				inString = false
			}

			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringQuote = ch
			continue
		}

		if ch == '/' && next == '/' {
			code := strings.TrimRightFunc(string(runes[:i]), unicode.IsSpace)
			comment := strings.TrimSpace(string(runes[i:]))
			return code, comment
		}
	}

	return line, ""
}

func stripLineCommentAware(line string) string {
	code, _ := splitCodeAndComment(line)
	return code
}

func spaceOperatorsOutsideStrings(code string) string {
	var out strings.Builder

	runes := []rune(code)

	inString := false
	stringQuote := rune(0)
	escaped := false

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if inString {
			out.WriteRune(ch)

			if escaped {
				escaped = false
				continue
			}

			if ch == '\\' {
				escaped = true
				continue
			}

			if ch == stringQuote {
				inString = false
			}

			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringQuote = ch
			out.WriteRune(ch)
			continue
		}

		// Multi-char operators.
		if i+1 < len(runes) {
			two := string([]rune{ch, runes[i+1]})

			switch two {
			case "==", "!=", "<=", ">=", "+=", "-=", "*=", "/=", "%=", "&&", "||":
				writeSpaceBefore(&out)
				out.WriteString(two)
				writeSpaceAfter(&out, runes, i+2)
				i++
				continue
			}
		}

		// Single-char operators.
		if isTinyOperator(ch) {
			// Don't space dot access.
			if ch == '.' {
				out.WriteRune(ch)
				continue
			}

			// Don't space unary !.
			if ch == '!' {
				out.WriteRune(ch)
				continue
			}

			// Don't space negative number: -1
			if ch == '-' && isUnaryMinus(runes, i) {
				out.WriteRune(ch)
				continue
			}

			writeSpaceBefore(&out)
			out.WriteRune(ch)
			writeSpaceAfter(&out, runes, i+1)
			continue
		}

		out.WriteRune(ch)
	}

	return out.String()
}

func isTinyOperator(ch rune) bool {
	switch ch {
	case '=', '+', '-', '*', '/', '%', '<', '>', '!', '.':
		return true
	default:
		return false
	}
}

func isUnaryMinus(runes []rune, index int) bool {
	if index+1 >= len(runes) {
		return false
	}

	next := runes[index+1]
	if next < '0' || next > '9' {
		return false
	}

	j := index - 1
	for j >= 0 && unicode.IsSpace(runes[j]) {
		j--
	}

	if j < 0 {
		return true
	}

	prev := runes[j]

	switch prev {
	case '(', '[', '{', '=', '+', '-', '*', '/', '%', ',', ':':
		return true
	default:
		return false
	}
}

func writeSpaceBefore(out *strings.Builder) {
	s := out.String()
	if s == "" {
		return
	}

	last := rune(s[len(s)-1])
	if unicode.IsSpace(last) {
		return
	}

	out.WriteRune(' ')
}

func writeSpaceAfter(out *strings.Builder, runes []rune, nextIndex int) {
	if nextIndex >= len(runes) {
		return
	}

	next := runes[nextIndex]
	if unicode.IsSpace(next) {
		return
	}

	out.WriteRune(' ')
}

func cleanupTinySpaces(code string) string {
	code = collapseSpacesOutsideStrings(code)

	replacements := []struct {
		old string
		new string
	}{
		{"( ", "("},
		{" )", ")"},
		{"[ ", "["},
		{" ]", "]"},
		{"{ ", "{ "},
		{" }", " }"},
		{",", ", "},
		{"; ", ";"},
		{". ", "."},
		{" .", "."},
	}

	for _, r := range replacements {
		code = strings.ReplaceAll(code, r.old, r.new)
	}

	// Fix comma double spaces.
	for strings.Contains(code, ",  ") {
		code = strings.ReplaceAll(code, ",  ", ", ")
	}

	return strings.TrimSpace(code)
}

func collapseSpacesOutsideStrings(code string) string {
	var out strings.Builder

	inString := false
	stringQuote := rune(0)
	escaped := false
	lastWasSpace := false

	for _, ch := range code {
		if inString {
			out.WriteRune(ch)

			if escaped {
				escaped = false
				continue
			}

			if ch == '\\' {
				escaped = true
				continue
			}

			if ch == stringQuote {
				inString = false
			}

			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = true
			stringQuote = ch
			out.WriteRune(ch)
			lastWasSpace = false
			continue
		}

		if unicode.IsSpace(ch) {
			if !lastWasSpace {
				out.WriteRune(' ')
				lastWasSpace = true
			}

			continue
		}

		out.WriteRune(ch)
		lastWasSpace = false
	}

	return out.String()
}
