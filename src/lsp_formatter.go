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
	stringState := formatterStringState{}

	for _, raw := range lines {
		if stringState.inString {
			formatted = append(formatted, raw)
			stringState = updateFormatterStringState(raw, stringState)
			continue
		}

		line := strings.TrimSpace(raw)

		if line == "" {
			formatted = append(formatted, "")
			continue
		}

		line = formatTinyLine(line)
		stringState = updateFormatterStringState(line, stringState)

		leadingClosings := countLeadingClosingBraces(line)

		indent -= leadingClosings
		if indent < 0 {
			indent = 0
		}

		formatted = append(formatted, strings.Repeat("    ", indent)+line)

		opens, closes := countBracesOutsideStrings(line)

		closes -= leadingClosings
		if closes < 0 {
			closes = 0
		}

		indent += opens - closes
		if indent < 0 {
			indent = 0
		}
	}

	formatted = cuddleElseBraces(formatted)
	formatted = collapseBlankLines(formatted)
	formatted = collapseMultilineCallAndArrayLiterals(formatted)

	result := strings.Join(formatted, "\n")

	if strings.HasSuffix(text, "\n") && !strings.HasSuffix(result, "\n") {
		result += "\n"
	}

	return result
}

type formatterStringState struct {
	inString bool
	quote    rune
	escaped  bool
}

func updateFormatterStringState(line string, state formatterStringState) formatterStringState {
	runes := []rune(line)

	for i := 0; i < len(runes); i++ {
		ch := runes[i]

		if state.inString {
			if state.escaped {
				state.escaped = false
				continue
			}

			if ch == '\\' {
				state.escaped = true
				continue
			}

			if ch == state.quote {
				state.inString = false
				state.quote = 0
			}

			continue
		}

		if ch == '/' && i+1 < len(runes) && runes[i+1] == '/' {
			break
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			state.inString = true
			state.quote = ch
			state.escaped = false
		}
	}

	return state
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
				if isPrefixUnaryOperator(runes, i) {
					i = skipSpacesAfter(runes, i)
				}
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
		"&^=",
		"<<=",
		">>=",
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
		":=",
		"<-",
		"&^",
		"<<",
		">>",
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
	case '=', '+', '-', '*', '/', '%', '<', '>', '!', '.', '?', '|', '&', '^':
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
		if index+1 < len(runes) && runes[index+1] == ':' {
			return true
		}
		if index > 0 && isIdentifierPart(runes[index-1]) && isFieldDeclarationLineRunes(runes) && !hasEqualBefore(runes, index) {
			return true
		}
	}

	return false
}

func isPrefixUnaryOperator(runes []rune, index int) bool {
	switch runes[index] {
	case '!':
		return isUnaryBang(runes, index)
	default:
		return false
	}
}

func isFieldDeclarationLine(code string) bool {
	trimmed := strings.TrimSpace(code)
	if strings.HasPrefix(trimmed, "field ") || trimmed == "field" {
		return true
	}
	parts := strings.Fields(trimmed)
	for _, p := range parts {
		if p == "field" {
			return true
		}
		if p == "private" || p == "public" || p == "const" {
			continue
		}
		break
	}
	return false
}

func isFieldDeclarationLineRunes(runes []rune) bool {
	return isFieldDeclarationLine(string(runes))
}

func hasEqualBefore(runes []rune, index int) bool {
	inString := false
	stringQuote := rune(0)
	escaped := false

	for i := 0; i < index; i++ {
		ch := runes[i]
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
		if ch == '=' {
			return true
		}
	}
	return false
}

func isUnaryBang(runes []rune, index int) bool {
	nextIndex := nextNonSpaceIndex(runes, index+1)
	return nextIndex < len(runes) && isExpressionStartRune(runes[nextIndex])
}

func isUnarySign(runes []rune, index int) bool {
	nextIndex := nextNonSpaceIndex(runes, index+1)
	if nextIndex >= len(runes) {
		return false
	}

	next := runes[nextIndex]
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

func nextNonSpaceIndex(runes []rune, index int) int {
	for index < len(runes) && unicode.IsSpace(runes[index]) {
		index++
	}
	return index
}

func isExpressionStartRune(ch rune) bool {
	return isIdentifierStart(ch) || unicode.IsDigit(ch) || ch == '(' || ch == '[' || ch == '{' || ch == '!' || ch == '"' || ch == '\'' || ch == '`'
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
	if s == "" {
		return 0
	}
	runes := []rune(s)
	return runes[len(runes)-1]
}

func cleanupTinySpaces(code string) string {
	code = collapseSpacesOutsideStrings(code)
	code = normalizePunctuationOutsideStrings(code)

	return strings.TrimSpace(code)
}

func isIdentifierPart(ch rune) bool {
	return ch == '_' || unicode.IsLetter(ch) || unicode.IsDigit(ch)
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
			s := out.String()
			trimmed := strings.TrimRightFunc(s, unicode.IsSpace)
			needsSpace := false
			keywords := []string{"if", "while", "for", "match", "catch", "lock"}
			for _, kw := range keywords {
				if strings.HasSuffix(trimmed, kw) {
					wordStart := len(trimmed) - len(kw)
					if wordStart == 0 || !isIdentifierPart(rune(trimmed[wordStart-1])) {
						needsSpace = true
						break
					}
				}
			}

			trimTrailingSpaces(&out)
			if needsSpace {
				out.WriteRune(' ')
			}
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

			if out.Len() > 0 {
				last := lastRune(out.String())
				if last != '{' && last != '(' && last != '[' && last != ' ' {
					out.WriteRune(' ')
				}
			}

			out.WriteRune(ch)

			if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) && runes[i+1] != '}' {
				out.WriteRune(' ')
			}

			i = skipExtraSpacesAfterOne(runes, i)
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

		case ':':
			nextIndex := nextNonSpaceIndex(runes, i+1)
			if nextIndex < len(runes) && runes[nextIndex] == '=' {
				writeSpaceBefore(&out)
				out.WriteString(":=")
				writeSpaceAfter(&out, runes, nextIndex+1)
				i = nextIndex
				continue
			}

			trimTrailingSpaces(&out)
			out.WriteRune(ch)

			if !isGenericColon(runes, i) && shouldWriteSpaceAfterColon(runes, i+1) {
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

func isGenericColon(runes []rune, index int) bool {
	i := index - 1
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}

	if i < 0 {
		return true
	}

	if runes[i] == ')' {
		return false
	}

	if !isIdentifierPart(runes[i]) {
		return false
	}

	end := i
	for i > 0 && isIdentifierPart(runes[i-1]) {
		i--
	}
	idStart := i
	id := string(runes[idStart : end+1])
	isCap := len(id) > 0 && unicode.IsUpper(runes[idStart])

	i = idStart - 1
	for i >= 0 && unicode.IsSpace(runes[i]) {
		i--
	}

	if i < 0 {
		return isCap
	}

	charBefore := runes[i]
	if charBefore == ':' {
		return true
	}

	if !isIdentifierPart(charBefore) {
		if charBefore == '(' || charBefore == '{' || charBefore == ',' {
			return false
		}
		return isCap
	}

	wordEnd := i
	for i > 0 && isIdentifierPart(runes[i-1]) {
		i--
	}
	wordBefore := string(runes[i : wordEnd+1])

	switch wordBefore {
	case "class", "interface", "fn":
		return true
	case "let", "const", "field", "private", "public":
		return false
	}

	return isCap
}

func skipSpacesAfter(runes []rune, index int) int {
	for index+1 < len(runes) && unicode.IsSpace(runes[index+1]) {
		index++
	}

	return index
}

func skipExtraSpacesAfterOne(runes []rune, index int) int {
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

	nextIndex = nextNonSpaceIndex(runes, nextIndex)

	if nextIndex >= len(runes) {
		return false
	}

	next := runes[nextIndex]

	return next != ')' && next != ']' && next != '}' && next != ';' && next != ','
}

func shouldWriteSpaceAfterColon(runes []rune, nextIndex int) bool {
	if nextIndex >= len(runes) {
		return false
	}

	nextIndex = nextNonSpaceIndex(runes, nextIndex)
	if nextIndex >= len(runes) {
		return false
	}

	next := runes[nextIndex]
	return next != ')' && next != ']' && next != '}' && next != ';' && next != ',' && next != '='
}

func trimTrailingSpaces(out *strings.Builder) {
	s := out.String()
	if len(s) == 0 {
		return
	}

	runes := []rune(s)
	n := len(runes)
	for n > 0 && unicode.IsSpace(runes[n-1]) {
		n--
	}

	if n == len(runes) {
		return
	}

	out.Reset()
	out.WriteString(string(runes[:n]))
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

func collapseBlankLines(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	result := []string{}
	lastWasBlank := false

	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if lastWasBlank {
				continue
			}
			lastWasBlank = true
			result = append(result, line)
			continue
		}
		lastWasBlank = false
		result = append(result, line)
	}

	return result
}

func cuddleElseBraces(lines []string) []string {
	if len(lines) == 0 {
		return lines
	}

	result := []string{}
	i := 0

	for i < len(lines) {
		line := lines[i]
		trimmed := strings.TrimSpace(line)

		if trimmed == "}" {
			j := i + 1
			for j < len(lines) && strings.TrimSpace(lines[j]) == "" {
				j++
			}

			if j < len(lines) {
				nextTrimmed := strings.TrimSpace(lines[j])
				if strings.HasPrefix(nextTrimmed, "else") ||
					strings.HasPrefix(nextTrimmed, "catch") ||
					strings.HasPrefix(nextTrimmed, "finally") {
					indent := line[:len(line)-len(trimmed)]
					result = append(result, indent+"} "+nextTrimmed)
					i = j + 1
					continue
				}
			}
		}

		result = append(result, line)
		i++
	}

	return result
}

const maxCollapseLineLen = 100

func collapseMultilineCallAndArrayLiterals(lines []string) []string {
	result := []string{}
	i := 0

	for i < len(lines) {
		trimmed := strings.TrimSpace(lines[i])

		if len(trimmed) == 0 {
			result = append(result, lines[i])
			i++
			continue
		}

		lastChar := trimmed[len(trimmed)-1]

		if lastChar == '(' || lastChar == '[' {
			openChar := lastChar
			closeChar := byte(')')
			if openChar == '[' {
				closeChar = ']'
			}

			indent := lines[i][:len(lines[i])-len(trimmed)]
			prefix := trimmed[:len(trimmed)-1]

			inner, closingLine, suffix, ok := collectToClosingBrace(lines, i+1, openChar, closeChar)
			if ok {
				candidate := buildCollapsedLine(prefix, openChar, inner, closeChar, suffix)
				if len(indent)+len(candidate) <= maxCollapseLineLen && inner != "" {
					result = append(result, indent+candidate)
					i = closingLine + 1
					continue
				}
			}
		}

		result = append(result, lines[i])
		i++
	}

	return result
}

func buildCollapsedLine(prefix string, openChar byte, inner string, closeChar byte, suffix string) string {
	between := ""
	if len(prefix) > 0 {
		last := prefix[len(prefix)-1]
		if openChar == '[' && (last == '=' || last == ',' || last == ':' || last == ' ') {
			between = " "
		}
	}

	afterClose := ""
	if suffix != "" {
		afterClose = " " + suffix
	}

	return prefix + between + string(openChar) + inner + string(closeChar) + afterClose
}

func collectToClosingBrace(lines []string, start int, openChar, closeChar byte) (string, int, string, bool) {
	inner := ""
	depth := 1
	j := start
	inString := false
	strCh := byte(0)
	escaped := false

	for j < len(lines) && depth > 0 {
		code := stripLineCommentAware(lines[j])

		for k := 0; k < len(code); k++ {
			ch := code[k]

			if inString {
				if escaped {
					escaped = false
					continue
				}
				if ch == '\\' {
					escaped = true
					continue
				}
				if ch == strCh {
					inString = false
				}
				continue
			}

			if ch == '"' || ch == '\'' || ch == '`' {
				inString = true
				strCh = ch
				continue
			}

			if ch == openChar {
				depth++
			} else if ch == closeChar {
				depth--
				if depth == 0 {
					suffix := strings.TrimSpace(code[k+1:])
					return inner, j, suffix, true
				}
			}
		}

		if depth > 0 {
			trimmed := strings.TrimSpace(code)
			if trimmed != "" {
				if inner != "" {
					inner += " " + trimmed
				} else {
					inner = trimmed
				}
			}
		}
		j++
	}

	return "", 0, "", false
}
