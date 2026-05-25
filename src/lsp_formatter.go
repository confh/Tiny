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
		lastLineText := strings.TrimSuffix(lines[lastLine], "\r")
		lastChar = byteColumnToUTF16Column(lastLineText, len(lastLineText))
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
	text = strings.ReplaceAll(text, "\r", "\n")

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

		// Leading closing braces were already handled before writing the line.
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

		if matched, size := writeMultiCharOperator(&out, runes, i); matched {
			i += size - 1
			continue
		}

		if isTinyOperator(ch) {
			if shouldKeepOperatorTight(runes, i) {
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

func writeMultiCharOperator(out *strings.Builder, runes []rune, index int) (bool, int) {
	remaining := string(runes[index:])

	tightOperators := []string{
		"...",
		"?.",
		"++",
		"--",
	}

	for _, op := range tightOperators {
		if strings.HasPrefix(remaining, op) {
			out.WriteString(op)
			return true, len([]rune(op))
		}
	}

	spacedOperators := []string{
		"==",
		"!=",
		"<=",
		">=",
		"+=",
		"-=",
		"*=",
		"/=",
		"%=",
		"&&",
		"||",
		"=>",
	}

	for _, op := range spacedOperators {
		if strings.HasPrefix(remaining, op) {
			writeSpaceBefore(out)
			out.WriteString(op)
			writeSpaceAfter(out, runes, index+len([]rune(op)))
			return true, len([]rune(op))
		}
	}

	return false, 0
}

func isTinyOperator(ch rune) bool {
	switch ch {
	case '=', '+', '-', '*', '/', '%', '<', '>', '!', '.', '?', '|':
		return true
	default:
		return false
	}
}

func shouldKeepOperatorTight(runes []rune, index int) bool {
	ch := runes[index]

	switch ch {
	case '.':
		return true

	case '!':
		return isUnaryBang(runes, index)

	case '-':
		return isUnarySign(runes, index)

	case '+':
		return isUnarySign(runes, index)

	case '?':
		// Optional parameter: name?: string
		if index+1 < len(runes) && runes[index+1] == ':' {
			return true
		}
	}

	return false
}

func isUnaryBang(runes []rune, index int) bool {
	j := index - 1
	for j >= 0 && unicode.IsSpace(runes[j]) {
		j--
	}

	if j < 0 {
		return true
	}

	prev := runes[j]

	switch prev {
	case '(', '[', '{', '=', '+', '-', '*', '/', '%', ',', ':', '?':
		return true
	default:
		return false
	}
}

func isUnarySign(runes []rune, index int) bool {
	if index+1 >= len(runes) {
		return false
	}

	next := runes[index+1]
	if !isIdentifierStart(next) && (next < '0' || next > '9') {
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
	case '(', '[', '{', '=', '+', '-', '*', '/', '%', ',', ':', '?':
		return true
	default:
		return false
	}
}

func isIdentifierStart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch)
}

func writeSpaceBefore(out *strings.Builder) {
	s := out.String()
	if s == "" {
		return
	}

	last := lastRune(s)
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

func lastRune(s string) rune {
	var last rune
	for _, ch := range s {
		last = ch
	}
	return last
}

func cleanupTinySpaces(code string) string {
	code = collapseSpacesOutsideStrings(code)
	code = normalizePunctuationOutsideStrings(code)

	return strings.TrimSpace(code)
}

func normalizePunctuationOutsideStrings(code string) string {
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

		switch ch {
		case '(':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)
			i = skipSpacesAfter(runes, i)
			continue

		case '[':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)
			i = skipSpacesAfter(runes, i)
			continue

		case ')', ']':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)
			continue

		case '{':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)

			if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) && runes[i+1] != '}' {
				out.WriteRune(' ')
			}

			i = skipExtraSpacesAfterOne(&out, runes, i)
			continue

		case '}':
			trimTrailingSpaces(&out)

			if out.Len() > 0 {
				last := lastRune(out.String())
				if last != '{' && !unicode.IsSpace(last) {
					out.WriteRune(' ')
				}
			}

			out.WriteRune(ch)
			continue

		case ',':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)

			if shouldWriteSpaceAfterPunctuation(runes, i+1) {
				out.WriteRune(' ')
			}

			i = skipSpacesAfter(runes, i)
			continue

		case ';':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)

			if shouldWriteSpaceAfterPunctuation(runes, i+1) {
				out.WriteRune(' ')
			}

			i = skipSpacesAfter(runes, i)
			continue

		case '.':
			trimTrailingSpaces(&out)
			out.WriteRune(ch)
			i = skipSpacesAfter(runes, i)
			continue
		}

		out.WriteRune(ch)
	}

	return out.String()
}

func skipSpacesAfter(runes []rune, index int) int {
	for index+1 < len(runes) && unicode.IsSpace(runes[index+1]) {
		index++
	}

	return index
}

func skipExtraSpacesAfterOne(out *strings.Builder, runes []rune, index int) int {
	if index+1 >= len(runes) || !unicode.IsSpace(runes[index+1]) {
		return index
	}

	index++

	for index+1 < len(runes) && unicode.IsSpace(runes[index+1]) {
		index++
	}

	return index
}

func shouldWriteSpaceAfterPunctuation(runes []rune, nextIndex int) bool {
	if nextIndex >= len(runes) {
		return false
	}

	nextIndex = skipSpacesAfter(runes, nextIndex-1)

	if nextIndex >= len(runes) {
		return false
	}

	next := runes[nextIndex]

	return next != ')' && next != ']' && next != '}' && next != ';' && next != ','
}

func trimTrailingSpaces(out *strings.Builder) {
	s := out.String()
	trimmed := strings.TrimRightFunc(s, unicode.IsSpace)

	if len(trimmed) == len(s) {
		return
	}

	out.Reset()
	out.WriteString(trimmed)
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
