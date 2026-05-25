package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	. "language.com/src/vm"
)

type LSPMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}
type TextDocumentItem struct {
	URI  string `json:"uri"`
	Text string `json:"text"`
}

type TextEdit struct {
	Range   LSPRange `json:"range"`
	NewText string   `json:"newText"`
}

type WorkspaceEdit struct {
	Changes map[string][]TextEdit `json:"changes"`
}

type FormattingParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type Location struct {
	URI   string   `json:"uri"`
	Range LSPRange `json:"range"`
}

type LSPRange struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type DidOpenParams struct {
	TextDocument TextDocumentItem `json:"textDocument"`
}

type TextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type VersionedTextDocumentIdentifier struct {
	URI string `json:"uri"`
}

type DocumentSymbol struct {
	Name           string           `json:"name"`
	Detail         string           `json:"detail,omitempty"`
	Kind           int              `json:"kind"`
	Range          LSPRange         `json:"range"`
	SelectionRange LSPRange         `json:"selectionRange"`
	Children       []DocumentSymbol `json:"children,omitempty"`
}

type TextDocumentContentChangeEvent struct {
	Text string `json:"text"`
}

type DidChangeParams struct {
	TextDocument   VersionedTextDocumentIdentifier  `json:"textDocument"`
	ContentChanges []TextDocumentContentChangeEvent `json:"contentChanges"`
}

type DidCloseParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
}

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type ReferenceParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	Context      struct {
		IncludeDeclaration bool `json:"includeDeclaration"`
	} `json:"context"`
}

type RenameParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
	NewName      string                 `json:"newName"`
}

type CompletionItem struct {
	Label               string     `json:"label"`
	Kind                int        `json:"kind,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	InsertText          string     `json:"insertText,omitempty"`
	InsertTextFormat    int        `json:"insertTextFormat,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
}

type MarkupContent struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type HoverParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type HoverResult struct {
	Contents MarkupContent `json:"contents"`
}

type SignatureHelp struct {
	Signatures      []SignatureInformation `json:"signatures"`
	ActiveSignature int                    `json:"activeSignature"`
	ActiveParameter int                    `json:"activeParameter"`
}
type SignatureInformation struct {
	Label         string                 `json:"label"`
	Documentation string                 `json:"documentation,omitempty"`
	Parameters    []ParameterInformation `json:"parameters,omitempty"`
}

type ParameterInformation struct {
	Label string `json:"label"`
}

var tinyKeywords = map[string]bool{
	"import": true,
	"std":    true,
	"as":     true,
	"export": true,

	"fn":      true,
	"let":     true,
	"const":   true,
	"class":   true,
	"embed":   true,
	"field":   true,
	"private": true,
	"public":  true,
	"enum":    true,

	"if":    true,
	"else":  true,
	"while": true,
	"for":   true,
	"in":    true,
	"match": true,

	"return":   true,
	"break":    true,
	"continue": true,
	"try":      true,
	"catch":    true,
	"finally":  true,
	"throw":    true,

	"true":      true,
	"false":     true,
	"null":      true,
	"undefined": true,

	"spawn":      true,
	"typeof":     true,
	"and":        true,
	"or":         true,
	"not":        true,
	"instanceof": true,
}

var lspDocs = map[string]string{}

type CallContext struct {
	Receiver string
	Method   string
	Name     string
	ArgIndex int
	IsMember bool
}

func formatFunctionSignature(name string, params []StdArg, returns string) string {
	parts := []string{}

	for _, arg := range params {
		label := arg.Name + ": " + arg.Type
		if arg.Optional {
			label = arg.Name + "?: " + arg.Type
		}

		parts = append(parts, label)
	}

	if returns == "" {
		returns = "any"
	}

	return name + "(" + strings.Join(parts, ", ") + "): " + returns
}

func signatureHelpFromMethod(fullName string, method StdMethodInfo, activeParam int) SignatureHelp {
	label := formatSignatureName(fullName, method)

	params := []ParameterInformation{}
	for _, arg := range method.Args {
		argLabel := arg.Name + ": " + arg.Type
		if arg.Optional {
			argLabel = arg.Name + "?: " + arg.Type
		}

		params = append(params, ParameterInformation{
			Label: argLabel,
		})
	}

	if activeParam >= len(params) {
		activeParam = len(params) - 1
	}

	if activeParam < 0 {
		activeParam = 0
	}

	return SignatureHelp{
		Signatures: []SignatureInformation{
			{
				Label:         label,
				Documentation: method.Description,
				Parameters:    params,
			},
		},
		ActiveSignature: 0,
		ActiveParameter: activeParam,
	}
}

func formatSignatureName(fullName string, method StdMethodInfo) string {
	parts := []string{}

	for _, arg := range method.Args {
		name := arg.Name
		if arg.Optional {
			name += "?"
		}

		parts = append(parts, name+": "+arg.Type)
	}

	return fullName + "(" + strings.Join(parts, ", ") + "): " + method.Returns
}

func callContextAtPosition(text string, pos Position) (CallContext, bool) {
	cursor := offsetAtLine(text, pos.Line+1) + pos.Character
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}

	open := findUnclosedCallParen(text[:cursor])
	if open == -1 {
		return CallContext{}, false
	}

	callee := extractCalleeBefore(text, open)
	if callee == "" {
		return CallContext{}, false
	}

	argIndex := countTopLevelCommas(text[open+1 : cursor])

	// member call: io.println(
	if dot := strings.LastIndex(callee, "."); dot != -1 {
		receiver := strings.TrimSpace(callee[:dot])
		receiver = strings.TrimSuffix(receiver, "?")
		method := strings.TrimSpace(callee[dot+1:])

		return CallContext{
			Receiver: receiver,
			Method:   method,
			ArgIndex: argIndex,
			IsMember: true,
		}, true
	}

	// normal call: fib(
	name := callee

	if name == "" {
		return CallContext{}, false
	}

	return CallContext{
		Name:     name,
		ArgIndex: argIndex,
		IsMember: false,
	}, true
}

func findUnclosedCallParen(text string) int {
	stack := []int{}
	inString := byte(0)
	escaped := false
	inLineComment := false

	for i := 0; i < len(text); i++ {
		ch := text[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}

		if i+1 < len(text) && ch == '/' && text[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}

		switch ch {
		case '(':
			stack = append(stack, i)
		case ')':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}

	if len(stack) == 0 {
		return -1
	}

	return stack[len(stack)-1]
}

func extractCalleeBefore(text string, open int) string {
	i := open - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}

	end := i + 1
	for i >= 0 {
		ch := text[i]
		if isIdentChar(ch) || ch == '.' || ch == '?' {
			i--
			continue
		}
		break
	}

	return strings.TrimSpace(text[i+1 : end])
}

func countTopLevelCommas(text string) int {
	count := 0
	depth := 0
	inString := byte(0)
	escaped := false
	inLineComment := false

	for i := 0; i < len(text); i++ {
		ch := text[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}

		if i+1 < len(text) && ch == '/' && text[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}

		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ',':
			if depth == 0 {
				count++
			}
		}
	}

	return count
}

func patchTextForCompletion(text string, pos Position) string {
	lines := strings.Split(text, "\n")

	for i, line := range lines {
		// Remove Windows \r so parser doesn't get weird text.
		line = strings.TrimSuffix(line, "\r")

		// Current line: patch at exact cursor position.
		if i == pos.Line {
			if pos.Character > len(line) {
				pos.Character = len(line)
			}

			before := line[:pos.Character]
			after := line[pos.Character:]

			trimmedBefore := strings.TrimRight(before, " \t")

			if strings.HasSuffix(trimmedBefore, ".") {
				// Important: add semicolon because Tiny requires it.
				lines[i] = before + "__complete__;" + after
				continue
			}

			lines[i] = line
			continue
		}

		// Other lines: if they end with a dangling dot, fix them too.
		trimmed := strings.TrimRight(line, " \t")

		if strings.HasSuffix(trimmed, ".") {
			spaces := line[len(trimmed):]
			lines[i] = trimmed + "__complete__;" + spaces
			continue
		}

		lines[i] = line
	}

	return strings.Join(lines, "\n")
}

func getStdCompletions(module string) []CompletionItem {
	info, ok := GetStdModuleInfo(module)
	if !ok {
		return []CompletionItem{}
	}

	items := []CompletionItem{}
	names := make([]string, 0, len(info.Methods))

	for name := range info.Methods {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		method := info.Methods[name]
		items = append(items, CompletionItem{
			Label:            method.Name,
			Kind:             2,
			Detail:           formatStdSignature(module, method),
			InsertText:       callableInsertText(method.Name),
			InsertTextFormat: 2,
		})
	}

	return items
}

func formatStdSignature(module string, method StdMethodInfo) string {
	parts := []string{}

	for _, arg := range method.Args {
		name := arg.Name

		if arg.Optional {
			name += "?"
		}

		parts = append(parts, name+": "+arg.Type)
	}

	return module + "." + method.Name + "(" + strings.Join(parts, ", ") + "): " + method.Returns
}

func runLSP() {
	reader := bufio.NewReader(os.Stdin)

	for {
		msg, err := readLSPMessage(reader)
		if err != nil {
			return
		}

		handleLSPMessage(msg)
	}
}

func readLSPMessage(reader *bufio.Reader) (LSPMessage, error) {
	contentLength := 0

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return LSPMessage{}, err
		}

		line = strings.TrimSpace(line)

		if line == "" {
			break
		}

		if strings.HasPrefix(strings.ToLower(line), "content-length:") {
			raw := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "content-length:"))

			n, err := strconv.Atoi(raw)
			if err != nil {
				return LSPMessage{}, err
			}

			contentLength = n
		}
	}

	if contentLength <= 0 {
		return LSPMessage{}, fmt.Errorf("missing Content-Length")
	}

	body := make([]byte, contentLength)

	_, err := io.ReadFull(reader, body)
	if err != nil {
		return LSPMessage{}, err
	}

	var msg LSPMessage
	err = json.Unmarshal(body, &msg)
	if err != nil {
		return LSPMessage{}, err
	}

	return msg, nil
}

func writeLSPMessage(msg LSPMessage) {
	msg.JSONRPC = "2.0"

	bytes, _ := json.Marshal(msg)

	fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(bytes), bytes)
	os.Stdout.Sync()
}

func nullLSPResult(value any) any {
	if value == nil {
		return json.RawMessage("null")
	}
	return value
}

func lspPositionToBytePosition(text string, pos Position) Position {
	line := getLine(text, pos.Line)
	return Position{
		Line:      pos.Line,
		Character: utf16ColumnToByteColumn(line, pos.Character),
	}
}

func utf16ColumnToByteColumn(line string, column int) int {
	if column <= 0 {
		return 0
	}

	units := 0
	for byteIndex, r := range line {
		if units >= column {
			return byteIndex
		}

		units += utf16.RuneLen(r)
		if units > column {
			return byteIndex
		}
	}

	return len(line)
}

func byteColumnToUTF16Column(line string, column int) int {
	if column <= 0 {
		return 0
	}
	if column > len(line) {
		column = len(line)
	}

	units := 0
	for byteIndex, r := range line {
		if byteIndex >= column {
			break
		}

		units += utf16.RuneLen(r)
	}

	return units
}

func lspRangeFromByteColumns(text string, line int, start int, end int) LSPRange {
	rawLine := getLine(text, line)

	return LSPRange{
		Start: Position{
			Line:      line,
			Character: byteColumnToUTF16Column(rawLine, start),
		},
		End: Position{
			Line:      line,
			Character: byteColumnToUTF16Column(rawLine, end),
		},
	}
}

func normalizeDiagnosticRangesForLSP(text string, diagnostics []map[string]any) []map[string]any {
	for _, diagnostic := range diagnostics {
		rangeValue, ok := diagnostic["range"].(map[string]any)
		if !ok {
			continue
		}

		startValue, ok := rangeValue["start"].(map[string]any)
		if !ok {
			continue
		}

		endValue, ok := rangeValue["end"].(map[string]any)
		if !ok {
			continue
		}

		line := intFromAny(startValue["line"])
		start := intFromAny(startValue["character"])
		end := intFromAny(endValue["character"])
		converted := lspRangeFromByteColumns(text, line, start, end)

		rangeValue["start"] = map[string]any{
			"line":      converted.Start.Line,
			"character": converted.Start.Character,
		}
		rangeValue["end"] = map[string]any{
			"line":      converted.End.Line,
			"character": converted.End.Character,
		}
	}

	return diagnostics
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func getSignatureHelp(uri string, text string, pos Position) any {
	ctx, ok := callContextAtPosition(text, pos)
	if !ok {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	if ctx.IsMember {
		sym, receiverType, exists := resolveReceiverPath(scope, text, pos, ctx.Receiver)
		if !exists {
			return nil
		}

		if sym.Kind == SymbolNamespace {
			member, ok := sym.Members[ctx.Method]
			if !ok {
				return nil
			}

			if member.Kind == SymbolFunction {
				return signatureHelpFromFunction(member, ctx.ArgIndex)
			}

			return nil
		}

		if strings.HasPrefix(receiverType, "class:") {
			className := strings.TrimPrefix(receiverType, "class:")

			classSym, ok := resolveClassSymbol(scope, className)
			if !ok || classSym.Kind != SymbolClass {
				return nil
			}

			methodSym, ok := classSym.Methods[ctx.Method]
			if !ok {
				return nil
			}

			return signatureHelpFromFunction(methodSym, ctx.ArgIndex)
		}

		if strings.HasPrefix(receiverType, "std:") {
			module := strings.TrimPrefix(receiverType, "std:")

			info, ok := GetStdModuleInfo(module)
			if !ok {
				return nil
			}

			method, ok := info.Methods[ctx.Method]
			if !ok {
				return nil
			}

			return signatureHelpFromMethod(module+"."+ctx.Method, method, ctx.ArgIndex)
		}

		method, ok := GetNativeMethodInfo(receiverType, ctx.Method)
		if ok {
			return signatureHelpFromMethod(receiverType+"."+ctx.Method, method, ctx.ArgIndex)
		}

		return nil
	}

	// basic normal function support later
	sym, exists := scope.Resolve(ctx.Name)
	if !exists {
		return nil
	}

	if sym.Kind == SymbolFunction {
		return signatureHelpFromFunction(sym, ctx.ArgIndex)
	}

	return nil
}

func signatureHelpFromFunction(sym SymbolInfo, activeParam int) SignatureHelp {
	parts := []string{}
	params := []ParameterInformation{}

	for _, arg := range sym.Params {
		label := arg.Name + ": " + arg.Type
		if arg.Optional {
			label = arg.Name + "?: " + arg.Type
		}

		parts = append(parts, label)
		params = append(params, ParameterInformation{
			Label: label,
		})
	}

	returns := sym.Returns
	if returns == "" {
		returns = "any"
	}

	label := sym.Name + "(" + strings.Join(parts, ", ") + "): " + returns

	if activeParam >= len(params) {
		activeParam = len(params) - 1
	}

	if activeParam < 0 {
		activeParam = 0
	}

	return SignatureHelp{
		Signatures: []SignatureInformation{
			{
				Label:         label,
				Documentation: sym.Detail,
				Parameters:    params,
			},
		},
		ActiveSignature: 0,
		ActiveParameter: activeParam,
	}
}

func getDefinition(uri string, text string, pos Position) any {
	word := wordAtPosition(text, pos)
	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	if receiver, member, ok := memberExprAtPosition(text, pos); ok {
		receiverSym, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
		if !exists {
			return nil
		}

		if receiverSym.Kind == SymbolNamespace || receiverSym.Kind == SymbolEnum {
			if memberSym, ok := receiverSym.Members[member]; ok {
				return locationFromSymbol(uri, text, memberSym)
			}
			return nil
		}

		if strings.HasPrefix(receiverType, "class:") {
			className := strings.TrimPrefix(receiverType, "class:")
			classSym, ok := resolveClassSymbol(scope, className)
			if !ok {
				return nil
			}

			if methodSym, ok := classSym.Methods[member]; ok {
				return locationFromSymbol(uri, text, methodSym)
			}
			return nil
		}

		if receiverType == "object" && receiverSym.Fields != nil {
			if fieldSym, ok := receiverSym.Fields[member]; ok {
				return locationFromSymbol(uri, text, fieldSym)
			}
		}

		return nil
	}

	sym, ok := scope.Resolve(word)
	if !ok {
		return nil
	}

	return locationFromSymbol(uri, text, sym)
}

func locationFromSymbol(defaultURI string, text string, sym SymbolInfo) any {
	if sym.Line <= 0 {
		return nil
	}

	targetURI := sym.SourceURI
	if targetURI == "" {
		targetURI = defaultURI
	}

	line := sym.Line - 1
	column := sym.Column - 1

	if column < 0 {
		column = 0
	}

	targetText := text
	if targetURI != defaultURI {
		if openText, ok := lspDocs[targetURI]; ok {
			targetText = openText
		}
	}

	lineText := getLine(targetText, line)
	startColumn := byteColumnToUTF16Column(lineText, column)
	endColumn := byteColumnToUTF16Column(lineText, column+len(sym.Name))

	return Location{
		URI: targetURI,
		Range: LSPRange{
			Start: Position{
				Line:      line,
				Character: startColumn,
			},
			End: Position{
				Line:      line,
				Character: endColumn,
			},
		},
	}
}

func getReferences(uri string, text string, pos Position, includeDeclaration bool) []Location {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] {
		return []Location{}
	}

	docs := collectReferenceDocuments(uri, text)
	locations := []Location{}

	for docURI, docText := range docs {
		for _, rng := range identifierRangesInText(docText, name) {
			if !includeDeclaration && docURI == uri && positionInByteRange(pos, rng) {
				continue
			}
			locations = append(locations, Location{
				URI:   docURI,
				Range: lspRangeFromByteColumns(docText, rng.Line, rng.Start, rng.End),
			})
		}
	}

	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}
		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line < locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character < locations[j].Range.Start.Character
	})

	return locations
}

func getRenameEdit(uri string, text string, pos Position, newName string) WorkspaceEdit {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] || !validTinyIdentifier(newName) {
		return WorkspaceEdit{Changes: map[string][]TextEdit{}}
	}

	changes := map[string][]TextEdit{}
	for _, loc := range getReferences(uri, text, pos, true) {
		changes[loc.URI] = append(changes[loc.URI], TextEdit{
			Range:   loc.Range,
			NewText: newName,
		})
	}

	return WorkspaceEdit{Changes: changes}
}

type byteIdentifierRange struct {
	Line  int
	Start int
	End   int
}

func identifierRangesInText(text string, name string) []byteIdentifierRange {
	if name == "" {
		return nil
	}

	ranges := []byteIdentifierRange{}
	lines := strings.Split(text, "\n")

	for lineIndex, line := range lines {
		code := stripLineComment(line)
		for start := 0; start < len(code); {
			index := strings.Index(code[start:], name)
			if index < 0 {
				break
			}
			index += start
			end := index + len(name)
			if isIdentifierBoundary(code, index-1) && isIdentifierBoundary(code, end) {
				ranges = append(ranges, byteIdentifierRange{Line: lineIndex, Start: index, End: end})
			}
			start = end
		}
	}

	return ranges
}

func stripLineComment(line string) string {
	inString := byte(0)
	escaped := false
	templateDepth := 0
	templateString := byte(0)
	templateEscaped := false
	out := []byte(line)
	for i := 0; i < len(line); i++ {
		ch := line[i]
		if templateDepth > 0 {
			if templateEscaped {
				out[i] = ' '
				templateEscaped = false
				continue
			}
			if templateString != 0 {
				if ch == '\\' {
					out[i] = ' '
					templateEscaped = true
					continue
				}
				if ch == templateString {
					templateString = 0
				}
				out[i] = ' '
				continue
			}
			if ch == '"' || ch == '\'' || ch == '`' {
				templateString = ch
				out[i] = ' '
				continue
			}
			if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
				for j := i; j < len(out); j++ {
					out[j] = ' '
				}
				return string(out)
			}
			if ch == '{' {
				templateDepth++
				continue
			}
			if ch == '}' {
				templateDepth--
				if templateDepth == 0 {
					out[i] = ' '
					inString = '`'
				}
				continue
			}
			continue
		}
		if escaped {
			out[i] = ' '
			escaped = false
			continue
		}
		if inString != 0 {
			if inString == '`' && ch == '$' && i+1 < len(line) && line[i+1] == '{' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				inString = 0
				templateDepth = 1
				continue
			}
			if ch == '\\' {
				out[i] = ' '
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			out[i] = ' '
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			out[i] = ' '
			continue
		}
		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return string(out[:i])
		}
	}
	if len(line) > 0 && inString != 0 {
		out[len(line)-1] = ' '
	}
	return string(out)
}

func isIdentifierBoundary(text string, index int) bool {
	if index < 0 || index >= len(text) {
		return true
	}
	return !isIdentChar(text[index])
}

func positionInByteRange(pos Position, rng byteIdentifierRange) bool {
	return pos.Line == rng.Line && pos.Character >= rng.Start && pos.Character <= rng.End
}

func validTinyIdentifier(name string) bool {
	if name == "" {
		return false
	}
	for i := 0; i < len(name); i++ {
		ch := name[i]
		if i == 0 {
			if !((ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || ch == '_') {
				return false
			}
			continue
		}
		if !isIdentChar(ch) {
			return false
		}
	}
	return !tinyKeywords[name]
}

func collectReferenceDocuments(uri string, text string) map[string]string {
	docs := map[string]string{uri: text}
	for openURI, openText := range lspDocs {
		docs[openURI] = openText
	}
	collectImportedReferenceDocuments(uri, text, docs, map[string]bool{})
	return docs
}

func collectImportedReferenceDocuments(uri string, text string, docs map[string]string, visited map[string]bool) {
	if visited[uri] {
		return
	}
	visited[uri] = true

	matches := fileImportRegex.FindAllStringSubmatch(text, -1)
	for _, match := range matches {
		resolved := resolveImportPath(uri, match[1])
		importURI := pathToFileURI(resolved)
		if _, exists := docs[importURI]; exists {
			continue
		}

		importText, ok := tinyFileTextForLSP(resolved, importURI)
		if !ok {
			continue
		}

		docs[importURI] = importText
		collectImportedReferenceDocuments(importURI, importText, docs, visited)
	}
}

func getDocumentSymbols(uri string, text string) []DocumentSymbol {
	lines := strings.Split(text, "\n")
	if len(lines) > 0 {
		lastLine := len(lines) - 1
		lastText := strings.TrimSuffix(lines[lastLine], "\r")
		scope := scopeAtPosition(uri, text, Position{
			Line:      lastLine,
			Character: len(lastText),
		})

		symbols := documentSymbolsFromScope(uri, text, scope)
		if len(symbols) > 0 {
			return symbols
		}
	}

	symbols := []DocumentSymbol{}
	for i, rawLine := range lines {
		line := strings.TrimSpace(strings.TrimSuffix(rawLine, "\r"))
		if line == "" {
			continue
		}

		if sym, ok := documentSymbolFromLine(rawLine, line, i); ok {
			symbols = append(symbols, sym)
		}
	}

	return symbols
}

func documentSymbolsFromScope(uri string, text string, scope *Scope) []DocumentSymbol {
	if scope == nil {
		return []DocumentSymbol{}
	}

	candidates := make([]SymbolInfo, 0, len(scope.Symbols))
	for _, sym := range scope.Symbols {
		candidates = append(candidates, sym)
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].Line == candidates[j].Line {
			return candidates[i].Column < candidates[j].Column
		}
		return candidates[i].Line < candidates[j].Line
	})

	symbols := []DocumentSymbol{}
	for _, sym := range candidates {
		if sym.SourceURI != "" && sym.SourceURI != uri {
			continue
		}
		if sym.Line <= 0 || sym.Kind == SymbolStd || strings.TrimSpace(sym.Name) == "" {
			continue
		}

		symbols = append(symbols, documentSymbolFromSymbol(sym, text))
	}

	return symbols
}

func documentSymbolFromLine(rawLine string, line string, lineIndex int) (DocumentSymbol, bool) {
	// fn name(...)
	if match := regexp.MustCompile(`^fn\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "function", 12), true
	}

	// export fn name(...)
	if match := regexp.MustCompile(`^export\s+fn\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "export function", 12), true
	}

	// class Name
	if match := regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "class", 5), true
	}

	// export class Name
	if match := regexp.MustCompile(`^export\s+class\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "export class", 5), true
	}

	// const/let name =
	if match := regexp.MustCompile(`^(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "variable", 13), true
	}

	// export const/let name =
	if match := regexp.MustCompile(`^export\s+(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "export variable", 13), true
	}

	return DocumentSymbol{}, false
}

func makeDocumentSymbol(rawLine string, lineIndex int, name string, detail string, kind int) DocumentSymbol {
	column := strings.Index(rawLine, name)
	if column < 0 {
		column = 0
	}

	rng := LSPRange{
		Start: Position{
			Line:      lineIndex,
			Character: byteColumnToUTF16Column(rawLine, column),
		},
		End: Position{
			Line:      lineIndex,
			Character: byteColumnToUTF16Column(rawLine, column+len(name)),
		},
	}

	return DocumentSymbol{
		Name:           name,
		Detail:         detail,
		Kind:           kind,
		Range:          rng,
		SelectionRange: rng,
	}
}

func documentSymbolFromSymbol(sym SymbolInfo, text string) DocumentSymbol {
	line := sym.Line - 1
	column := sym.Column - 1
	if column < 0 {
		column = 0
	}

	lineText := getLine(text, line)

	rng := LSPRange{
		Start: Position{
			Line:      line,
			Character: byteColumnToUTF16Column(lineText, column),
		},
		End: Position{
			Line:      line,
			Character: byteColumnToUTF16Column(lineText, column+len(sym.Name)),
		},
	}

	children := []DocumentSymbol{}

	if sym.Kind == SymbolClass && len(sym.Fields) > 0 {
		fieldNames := make([]string, 0, len(sym.Fields))
		for fieldName := range sym.Fields {
			fieldNames = append(fieldNames, fieldName)
		}
		sort.Strings(fieldNames)

		for _, fieldName := range fieldNames {
			field := sym.Fields[fieldName]
			if strings.TrimSpace(field.Name) == "" {
				continue
			}
			children = append(children, documentSymbolFromSymbol(field, text))
		}
	}

	if sym.Kind == SymbolClass && len(sym.Methods) > 0 {
		methodNames := make([]string, 0, len(sym.Methods))
		for methodName := range sym.Methods {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)

		for _, methodName := range methodNames {
			method := sym.Methods[methodName]
			if strings.TrimSpace(method.Name) == "" {
				continue
			}
			children = append(children, documentSymbolFromSymbol(method, text))
		}
	}

	if sym.Kind == SymbolNamespace && len(sym.Members) > 0 {
		memberNames := make([]string, 0, len(sym.Members))
		for memberName := range sym.Members {
			memberNames = append(memberNames, memberName)
		}
		sort.Strings(memberNames)

		for _, memberName := range memberNames {
			member := sym.Members[memberName]
			if strings.TrimSpace(member.Name) == "" {
				continue
			}
			children = append(children, documentSymbolFromSymbol(member, text))
		}
	}

	return DocumentSymbol{
		Name:           sym.Name,
		Detail:         symbolDetail(sym),
		Kind:           symbolKindToDocumentKind(sym.Kind),
		Range:          rng,
		SelectionRange: rng,
		Children:       children,
	}
}

func symbolKindToDocumentKind(kind SymbolKind) int {
	switch kind {
	case SymbolFunction:
		return 12
	case SymbolClass:
		return 5
	case SymbolVariable:
		return 13
	case SymbolStd, SymbolNamespace:
		return 2
	case SymbolField:
		return 8
	case SymbolEnum:
		return 13
	default:
		return 13
	}
}

func handleLSPMessage(msg LSPMessage) {
	switch msg.Method {
	case "initialize":
		writeLSPMessage(LSPMessage{
			ID: msg.ID,
			Result: map[string]any{
				"capabilities": map[string]any{
					"textDocumentSync": 1,
					"completionProvider": map[string]any{
						"triggerCharacters":   []string{".", `"`},
						"resolveProvider":     false,
						"completionItem":      map[string]any{"snippetSupport": true},
						"allCommitCharacters": []string{},
					},
					"signatureHelpProvider": map[string]any{
						"triggerCharacters": []string{"(", ","},
					},
					"documentFormattingProvider": true,
					"definitionProvider":         true,
					"referencesProvider":         true,
					"renameProvider":             true,
					"documentSymbolProvider":     true,
					"hoverProvider":              true,
				},
			},
		})

	case "initialized":
		// nothing

	case "shutdown":
		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(nil),
		})

	case "exit":
		os.Exit(0)

	case "textDocument/didOpen":
		var params DidOpenParams
		json.Unmarshal(msg.Params, &params)

		lspDocs[params.TextDocument.URI] = params.TextDocument.Text
		invalidateLSPImportCacheForURI(params.TextDocument.URI)
		publishDiagnostics(params.TextDocument.URI, params.TextDocument.Text)
		publishDiagnosticsForImportDependents(params.TextDocument.URI)

	case "textDocument/didChange":
		var params DidChangeParams
		json.Unmarshal(msg.Params, &params)

		if len(params.ContentChanges) > 0 {
			text := params.ContentChanges[0].Text
			lspDocs[params.TextDocument.URI] = text
			invalidateLSPImportCacheForURI(params.TextDocument.URI)
			publishDiagnostics(params.TextDocument.URI, text)
			publishDiagnosticsForImportDependents(params.TextDocument.URI)
		}

	case "textDocument/didClose":
		var params DidCloseParams
		json.Unmarshal(msg.Params, &params)

		delete(lspDocs, params.TextDocument.URI)
		invalidateLSPImportCacheForURI(params.TextDocument.URI)
		publishDiagnostics(params.TextDocument.URI, "")
		publishDiagnosticsForImportDependents(params.TextDocument.URI)

	case "textDocument/completion":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)
		items := getCompletions(params.TextDocument.URI, text, params.Position)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: items,
		})

	case "textDocument/signatureHelp":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any

		func() {
			defer func() {
				if r := recover(); r != nil {
					result = nil
				}
			}()

			result = getSignatureHelp(params.TextDocument.URI, text, params.Position)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(result),
		})

	case "textDocument/definition":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any

		func() {
			defer func() {
				if r := recover(); r != nil {
					result = nil
				}
			}()

			result = getDefinition(params.TextDocument.URI, text, params.Position)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(result),
		})

	case "textDocument/references":
		var params ReferenceParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []Location{}
				}
			}()

			result = getReferences(params.TextDocument.URI, text, params.Position, params.Context.IncludeDeclaration)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/rename":
		var params RenameParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = WorkspaceEdit{Changes: map[string][]TextEdit{}}
				}
			}()

			result = getRenameEdit(params.TextDocument.URI, text, params.Position, params.NewName)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/documentSymbol":
		var params struct {
			TextDocument TextDocumentIdentifier `json:"textDocument"`
		}

		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]

		var result any

		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []DocumentSymbol{}
				}
			}()

			result = getDocumentSymbols(params.TextDocument.URI, text)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/formatting":
		var params FormattingParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]

		var result any = []TextEdit{}

		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []TextEdit{}
				}
			}()

			formatted := formatTinyDocument(text)

			result = []TextEdit{
				{
					Range:   fullDocumentRange(text),
					NewText: formatted,
				},
			}
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/hover":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any

		func() {
			defer func() {
				if r := recover(); r != nil {
					result = nil
				}
			}()

			result = getHover(params.TextDocument.URI, text, params.Position)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(result),
		})

	default:
		if msg.ID != nil {
			writeLSPMessage(LSPMessage{
				ID:     msg.ID,
				Result: nullLSPResult(nil),
			})
		}
	}
}

type TinySymbols struct {
	Functions []string
	Classes   []string
	Variables []string
	Imports   map[string]string
}

func collectSymbolsFromText(uri string, text string) TinySymbols {
	statements, diagnostics := parseTinyForLSP(uri, text)

	symbols := TinySymbols{
		Imports: parseStdImports(text), // fallback always
	}

	if len(diagnostics) > 0 {
		return symbols
	}

	for _, stmt := range statements {
		collectSymbolFromStmt(&symbols, stmt)
	}

	return symbols
}

func collectSymbolFromStmt(symbols *TinySymbols, stmt Stmt) {
	switch s := stmt.(type) {
	case ImportStmt:
		if s.Std {
			alias := s.Path

			if s.Alias != "" {
				alias = s.Alias
			}

			symbols.Imports[alias] = s.Path
		}

	case FunctionStmt:
		symbols.Functions = append(symbols.Functions, s.Name)

	case ClassStmt:
		symbols.Classes = append(symbols.Classes, s.Name)

	case VariableStmt:
		symbols.Variables = append(symbols.Variables, s.Name)
	}
}

var lspLogFile *os.File

// func initLSPLogger() {
// 	file, err := os.OpenFile("tiny-lsp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
// 	if err == nil {
// 		lspLogFile = file
// 	}
// }

// func lspDebug(format string, args ...any) {
// 	if lspLogFile == nil {
// 		return
// 	}

// 	fmt.Fprintf(lspLogFile, "[tiny-lsp] "+format+"\n", args...)
// 	lspLogFile.Sync()
// }

func makeLSPDiagnostic(line int, column int, severity int, message string) map[string]any {
	if line < 0 {
		line = 0
	}

	if column < 0 {
		column = 0
	}

	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{
				"line":      line,
				"character": column,
			},
			"end": map[string]any{
				"line":      line,
				"character": column + 1,
			},
		},
		"severity": severity,
		"message":  message,
		"source":   "tiny",
	}
}

func classBlockAtLine(text string, lineIndex int) *blockInfo {
	offset := offsetAtLine(text, lineIndex+1)

	for _, block := range findBlocks(text, "class") {
		if offset >= block.Start && offset < block.End {
			return &block
		}
	}

	return nil
}

func functionBlockAtLine(text string, lineIndex int) *blockInfo {
	offset := offsetAtLine(text, lineIndex+1)

	var best *blockInfo

	for _, block := range findBlocks(text, "fn") {
		if offset >= block.Start && offset < block.End {
			copy := block

			if best == nil || copy.Start > best.Start {
				best = &copy
			}
		}
	}

	return best
}

func semanticDiagnostics(uri string, text string) []map[string]any {
	return semanticDiagnosticsFromAST(uri, text)
}

func publishDiagnostics(uri string, text string) {
	if text == "" {
		params, _ := json.Marshal(map[string]any{
			"uri":         uri,
			"diagnostics": []map[string]any{},
		})

		writeLSPMessage(LSPMessage{
			Method: "textDocument/publishDiagnostics",
			Params: params,
		})
		return
	}

	_, parseDiagnostics := parseTinyForLSP(uri, text)

	diagnostics := []map[string]any{}

	for _, diagnostic := range parseDiagnostics {
		line := diagnostic.Line
		column := diagnostic.Column

		diagnostics = append(diagnostics, map[string]any{
			"range": map[string]any{
				"start": map[string]any{
					"line":      line,
					"character": column,
				},
				"end": map[string]any{
					"line":      line,
					"character": column + 1,
				},
			},
			"severity": 1,
			"message":  diagnostic.Message,
			"source":   "tiny",
		})
	}

	if len(parseDiagnostics) == 0 {
		diagnostics = append(diagnostics, semanticDiagnostics(uri, text)...)
	}

	diagnostics = normalizeDiagnosticRangesForLSP(text, diagnostics)

	params, _ := json.Marshal(map[string]any{
		"uri":         uri,
		"diagnostics": diagnostics,
	})

	writeLSPMessage(LSPMessage{
		Method: "textDocument/publishDiagnostics",
		Params: params,
	})
}

func publishDiagnosticsForImportDependents(changedURI string) {
	for _, uri := range dependentDocumentURIs(changedURI) {
		if text, ok := lspDocs[uri]; ok {
			publishDiagnostics(uri, text)
		}
	}
}

func dependentDocumentURIs(changedURI string) []string {
	changedPath := filepath.Clean(uriToPath(changedURI))
	if changedPath == "." || changedPath == "" {
		return nil
	}

	dependents := []string{}
	for uri, text := range lspDocs {
		if uri == changedURI {
			continue
		}
		if documentImportsPath(uri, text, changedPath, map[string]bool{}) {
			dependents = append(dependents, uri)
		}
	}

	sort.Strings(dependents)
	return dependents
}

func documentImportsPath(uri string, text string, targetPath string, visited map[string]bool) bool {
	if visited[uri] {
		return false
	}
	visited[uri] = true

	for _, match := range fileImportRegex.FindAllStringSubmatch(text, -1) {
		importPath := filepath.Clean(resolveImportPath(uri, match[1]))
		if importPath == targetPath {
			return true
		}

		importURI := pathToFileURI(importPath)
		importText, ok := tinyFileTextForLSP(importPath, importURI)
		if ok && documentImportsPath(importURI, importText, targetPath, visited) {
			return true
		}
	}

	return false
}

func isInsideStdImportString(line string, character int) bool {
	if character > len(line) {
		character = len(line)
	}

	before := line[:character]

	return strings.Contains(before, `import std "`)
}

func stdModuleNameCompletions() []CompletionItem {
	items := []CompletionItem{}
	names := make([]string, 0, len(StdMetadata))

	for name := range StdMetadata {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		items = append(items, CompletionItem{
			Label:  name,
			Kind:   9,
			Detail: "std module",
		})
	}

	return items
}

func getNativeTypeCompletions(typeName string) []CompletionItem {
	if strings.HasPrefix(typeName, "task:") {
		return []CompletionItem{
			{
				Label:            "await",
				Kind:             2,
				Detail:           "task.await(): " + strings.TrimPrefix(typeName, "task:"),
				InsertText:       callableInsertText("await"),
				InsertTextFormat: 2,
			},
		}
	}

	info, ok := GetNativeTypeInfo(typeName)
	if !ok {
		return []CompletionItem{}
	}

	items := []CompletionItem{}
	names := make([]string, 0, len(info.Methods))

	for name := range info.Methods {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		method := info.Methods[name]
		items = append(items, CompletionItem{
			Label:            method.Name,
			Kind:             2,
			Detail:           formatNativeSignature(typeName, method),
			InsertText:       callableInsertText(method.Name),
			InsertTextFormat: 2,
		})
	}

	items = append(items, CompletionItem{
		Label:            "toString",
		Kind:             2,
		InsertText:       callableInsertText("toString"),
		InsertTextFormat: 2,
		Detail: formatNativeSignature(typeName, StdMethodInfo{
			Name:        "toString",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns a stringified version of the value.",
		}),
	})

	return items
}

func formatNativeSignature(typeName string, method StdMethodInfo) string {
	parts := []string{}

	for _, arg := range method.Args {
		name := arg.Name
		if arg.Optional {
			name += "?"
		}

		parts = append(parts, name+": "+arg.Type)
	}

	return typeName + "." + method.Name + "(" + strings.Join(parts, ", ") + "): " + method.Returns
}

func scopeCompletions(scope *Scope, uri string, text string) []CompletionItem {
	items := []CompletionItem{
		snippetCompletion("import", "import statement", "import \"$1\";$0"),
		snippetCompletion("import std", "standard library import", "import std \"$1\";$0"),
		{Label: "export", Kind: 14, Detail: "export statement", InsertText: "export $0", InsertTextFormat: 2},
		{Label: "std", Kind: 14, Detail: "standard library import"},
		snippetCompletion("fn", "function", "fn ${1:name}(${2}) {\n    $0\n}"),
		snippetCompletion("let", "variable", "let ${1:name} = ${2:value};$0"),
		snippetCompletion("const", "constant", "const ${1:name} = ${2:value};$0"),
		{Label: "class", Kind: 7, Detail: "class", InsertText: "class ${1:Name} {\n    $0\n}", InsertTextFormat: 2},
		{Label: "embed", Kind: 14, Detail: "embed class methods"},
		snippetCompletion("field", "class field", "field ${1:name} = ${2:value};$0"),
		{Label: "private", Kind: 14, Detail: "private field"},
		{Label: "public", Kind: 14, Detail: "public field"},
		snippetCompletion("if", "if statement", "if ${1:condition} {\n    $0\n}"),
		{Label: "else", Kind: 14, Detail: "else"},
		snippetCompletion("while", "while loop", "while ${1:condition} {\n    $0\n}"),
		snippetCompletion("for", "for loop", "for let ${1:i} = 0; ${1:i} < ${2:count}; ${1:i}++ {\n    $0\n}"),
		snippetCompletion("for in", "for-in loop", "for ${1:item} in ${2:items} {\n    $0\n}"),
		snippetCompletion("match", "match expression", "match ${1:value} {\n    ${2:case} {\n        $0\n    }\n    _ {\n    }\n}"),
		snippetCompletion("return", "return", "return ${1:value};$0"),
		{Label: "break", Kind: 14, Detail: "break"},
		{Label: "continue", Kind: 14, Detail: "continue"},
		snippetCompletion("try", "try statement", "try {\n    $0\n} catch ${1:err} {\n    \n}"),
		{Label: "catch", Kind: 14, Detail: "catch block"},
		{Label: "finally", Kind: 14, Detail: "finally block"},
		snippetCompletion("throw", "throw error", "throw ${1:error};$0"),
		snippetCompletion("spawn", "spawn task", "spawn fn() {\n    $0\n}"),
		{Label: "typeof", Kind: 14, Detail: "type operator"},
		{Label: "instanceof", Kind: 14, Detail: "instance check"},
		{Label: "true", Kind: 14, Detail: "boolean literal"},
		{Label: "false", Kind: 14, Detail: "boolean literal"},
		{Label: "null", Kind: 14, Detail: "null literal"},
		{Label: "undefined", Kind: 14, Detail: "undefined literal"},
	}

	seen := map[string]bool{}

	for s := scope; s != nil; s = s.Parent {
		names := make([]string, 0, len(s.Symbols))
		for name := range s.Symbols {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			sym := s.Symbols[name]

			if seen[sym.Name] {
				continue
			}

			seen[sym.Name] = true

			items = append(items, CompletionItem{
				Label:            sym.Name,
				Kind:             symbolKindToCompletionKind(sym.Kind),
				Detail:           symbolDetail(sym),
				InsertText:       completionInsertText(sym),
				InsertTextFormat: completionInsertTextFormat(sym),
			})
		}
	}

	items = append(items, stdAutoImportCompletions(scope, text)...)
	items = append(items, fileAutoImportCompletions(scope, uri, text)...)

	return dedupeCompletionItems(items)
}

func snippetCompletion(label string, detail string, insertText string) CompletionItem {
	return CompletionItem{
		Label:            label,
		Kind:             14,
		Detail:           detail,
		InsertText:       insertText,
		InsertTextFormat: 2,
	}
}

func stdAutoImportCompletions(scope *Scope, text string) []CompletionItem {
	items := []CompletionItem{}
	imports := parseStdImports(text)
	names := make([]string, 0, len(StdMetadata))
	for name := range StdMetadata {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		if _, ok := imports[name]; ok {
			continue
		}
		if _, ok := scope.Resolve(name); ok {
			continue
		}

		items = append(items, CompletionItem{
			Label:  name,
			Kind:   9,
			Detail: "auto import std module " + name,
			AdditionalTextEdits: []TextEdit{
				importTextEdit(text, `import std "`+name+`";`),
			},
		})
	}

	return items
}

func fileAutoImportCompletions(scope *Scope, uri string, text string) []CompletionItem {
	currentPath := uriToPath(uri)
	if currentPath == "" {
		return nil
	}

	root := filepath.Dir(currentPath)
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}

	items := []CompletionItem{}
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".tiny" {
			continue
		}

		path := filepath.Join(root, entry.Name())
		if filepath.Clean(path) == filepath.Clean(currentPath) {
			continue
		}

		relImport := filepath.ToSlash(entry.Name())
		if fileImportAlreadyPresent(text, relImport) {
			continue
		}

		exports := loadTinyFileExports(path, map[string]bool{})
		if len(exports) == 0 {
			continue
		}

		alias := importAliasForPath(path)
		importEdit := importTextEdit(text, `import "`+relImport+`" as `+alias+`;`)
		names := make([]string, 0, len(exports))
		for name := range exports {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			sym := exports[name]
			if _, exists := scope.Resolve(name); exists {
				continue
			}

			insert := alias + "." + sym.Name
			format := 0
			if sym.Kind == SymbolClass || sym.Kind == SymbolFunction {
				insert = alias + "." + callableInsertText(sym.Name)
				format = 2
			}

			items = append(items, CompletionItem{
				Label:               sym.Name,
				Kind:                symbolKindToCompletionKind(sym.Kind),
				Detail:              "auto import from " + relImport,
				InsertText:          insert,
				InsertTextFormat:    format,
				AdditionalTextEdits: []TextEdit{importEdit},
			})
		}
	}

	return items
}

func importTextEdit(text string, importLine string) TextEdit {
	line := importInsertLine(text)
	return TextEdit{
		Range: LSPRange{
			Start: Position{Line: line, Character: 0},
			End:   Position{Line: line, Character: 0},
		},
		NewText: importLine + "\n",
	}
}

func importInsertLine(text string) int {
	lines := strings.Split(text, "\n")
	lastImport := -1
	for i, raw := range lines {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "import ") {
			lastImport = i
			continue
		}
		if line == "" {
			continue
		}
		if lastImport >= 0 {
			break
		}
	}
	return lastImport + 1
}

func fileImportAlreadyPresent(text string, importPath string) bool {
	for _, match := range fileImportRegex.FindAllStringSubmatch(text, -1) {
		if filepath.ToSlash(match[1]) == filepath.ToSlash(importPath) {
			return true
		}
	}
	return false
}

func importAliasForPath(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	parts := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_' || r == ' ' || r == '.'
	})
	alias := ""
	for _, part := range parts {
		if part == "" {
			continue
		}
		alias += strings.ToUpper(part[:1]) + part[1:]
	}
	if alias == "" {
		return "Module"
	}
	return alias
}

func dedupeCompletionItems(items []CompletionItem) []CompletionItem {
	seen := map[string]bool{}
	result := []CompletionItem{}

	for _, item := range items {
		if seen[item.Label] {
			continue
		}

		seen[item.Label] = true
		result = append(result, item)
	}

	return result
}

func classNameAtPosition(text string, pos Position) string {
	block := classBlockAtLine(text, pos.Line)
	if block != nil && block.Name != "" {
		return block.Name
	}

	lines := strings.Split(text, "\n")
	currentLine := pos.Line
	if currentLine >= len(lines) {
		currentLine = len(lines) - 1
	}

	for i := currentLine; i >= 0; i-- {
		line := cleanLine(lines[i])
		match := classLineRegex.FindStringSubmatch(line)
		if match != nil {
			return match[1]
		}
	}

	return ""
}

func completionItemsForClass(scope *Scope, classSym SymbolInfo, receiver string) []CompletionItem {
	items := []CompletionItem{}

	for _, field := range classSym.Fields {
		if isPrivateSymbol(field) && receiver != "this" && !strings.HasPrefix(receiver, "this.") {
			continue
		}

		items = append(items, CompletionItem{
			Label:  field.Name,
			Kind:   symbolKindToCompletionKind(field.Kind),
			Detail: field.Detail + " : " + field.Type,
		})
	}

	for _, method := range classSym.Methods {
		if isPrivateSymbol(method) && receiver != "this" && !strings.HasPrefix(receiver, "this.") {
			continue
		}

		items = append(items, CompletionItem{
			Label:            method.Name,
			Kind:             2,
			Detail:           formatFunctionSignature(method.Name, method.Params, method.Returns),
			InsertText:       callableInsertText(method.Name),
			InsertTextFormat: 2,
		})
	}

	return dedupeCompletionItems(items)
}

func staticTypeOfSymbol(receiver string, sym SymbolInfo) string {
	switch sym.Kind {
	case SymbolClass:
		return "class:" + sym.Name
	case SymbolEnum:
		if strings.Contains(receiver, ".") {
			return "enum:" + receiver
		}
		return "enum:" + sym.Name
	case SymbolFunction:
		return "function"
	case SymbolNamespace:
		return "namespace:" + sym.Name
	default:
		return sym.Type
	}
}

func splitReceiverPath(receiver string) []string {
	receiver = strings.TrimSpace(receiver)
	receiver = strings.ReplaceAll(receiver, "?.", ".")
	receiver = strings.TrimSuffix(receiver, "?")
	parts := strings.Split(receiver, ".")
	out := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(strings.TrimSuffix(part, "?"))
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func resolveMemberFromStaticType(scope *Scope, typ string, member string) (SymbolInfo, string, bool) {
	typ = strings.TrimSpace(typ)

	if strings.Contains(typ, "|") {
		for _, part := range splitUnionType(typ) {
			if isNullishLSPType(part) {
				continue
			}
			if sym, memberType, ok := resolveMemberFromStaticType(scope, part, member); ok {
				return sym, memberType, true
			}
		}
		return SymbolInfo{}, "unknown", false
	}

	if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")
		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			return SymbolInfo{}, "unknown", false
		}

		if fieldSym, ok := classSym.Fields[member]; ok {
			return fieldSym, fieldSym.Type, true
		}

		if methodSym, ok := classSym.Methods[member]; ok {
			return methodSym, firstNonEmpty(methodSym.Returns, "function"), true
		}

		return SymbolInfo{}, "unknown", false
	}

	if strings.HasPrefix(typ, "enum:") {
		enumName := strings.TrimPrefix(typ, "enum:")
		enumSym, ok := resolveEnumSymbol(scope, enumName)
		if !ok || enumSym.Kind != SymbolEnum {
			return SymbolInfo{}, "unknown", false
		}
		if memberSym, ok := enumSym.Members[member]; ok {
			return memberSym, "number", true
		}
		return SymbolInfo{}, "unknown", false
	}

	if strings.HasPrefix(typ, "std:") {
		module := strings.TrimPrefix(typ, "std:")
		info, ok := GetStdModuleInfo(module)
		if !ok {
			return SymbolInfo{}, "unknown", false
		}
		if methodInfo, ok := info.Methods[member]; ok {
			return SymbolInfo{Name: methodInfo.Name, Kind: SymbolFunction, Type: "function", Detail: methodInfo.Description, Params: methodInfo.Args, Returns: methodInfo.Returns}, methodInfo.Returns, true
		}
		return SymbolInfo{}, "unknown", false
	}

	if methodInfo, ok := GetNativeMethodInfo(typ, member); ok {
		return SymbolInfo{Name: methodInfo.Name, Kind: SymbolFunction, Type: "function", Detail: methodInfo.Description, Params: methodInfo.Args, Returns: methodInfo.Returns}, methodInfo.Returns, true
	}

	return SymbolInfo{}, "unknown", false
}

func resolveReceiverPath(scope *Scope, text string, pos Position, receiver string) (SymbolInfo, string, bool) {
	parts := splitReceiverPath(receiver)
	if len(parts) == 0 {
		return SymbolInfo{}, "", false
	}

	var sym SymbolInfo
	var typ string
	ok := false
	qualified := parts[0]

	if parts[0] == "this" {
		className := classNameAtPosition(text, pos)
		if className == "" {
			return SymbolInfo{}, "", false
		}
		classSym, exists := currentClassSymbolAtPosition(scope, text, pos, className)
		if !exists {
			return SymbolInfo{}, "", false
		}
		sym = SymbolInfo{Name: "this", Kind: SymbolVariable, Type: "class:" + className, Detail: "current class instance", Fields: classSym.Fields, Methods: classSym.Methods}
		typ = "class:" + className
		ok = true
	} else {
		sym, ok = scope.Resolve(parts[0])
		if !ok {
			return SymbolInfo{}, "", false
		}
		typ = staticTypeOfSymbol(parts[0], sym)
	}

	if len(parts) == 1 {
		return sym, typ, true
	}

	for _, member := range parts[1:] {
		qualified += "." + member

		if sym.Kind == SymbolNamespace {
			memberSym, exists := sym.Members[member]
			if !exists {
				return SymbolInfo{}, "unknown", false
			}
			sym = memberSym
			typ = staticTypeOfSymbol(qualified, memberSym)
			continue
		}

		if fieldSym, exists := sym.Fields[member]; exists {
			sym = fieldSym
			typ = fieldSym.Type
			continue
		}

		if methodSym, exists := sym.Methods[member]; exists {
			sym = methodSym
			typ = firstNonEmpty(methodSym.Returns, "function")
			continue
		}

		if nextSym, nextType, exists := resolveMemberFromStaticType(scope, typ, member); exists {
			sym = nextSym
			typ = nextType
			continue
		}

		if sym.Type == "object" && sym.Fields != nil {
			if fieldSym, exists := sym.Fields[member]; exists {
				sym = fieldSym
				typ = fieldSym.Type
				continue
			}
		}

		return SymbolInfo{}, "unknown", false
	}

	return sym, typ, true
}

func currentClassSymbolAtPosition(scope *Scope, text string, pos Position, className string) (SymbolInfo, bool) {
	lines := strings.Split(text, "\n")
	classLine := -1
	body := ""
	bodyBaseLine := 1

	if block := classBlockAtLine(text, pos.Line); block != nil && block.Name == className {
		classLine = block.Line - 1
		body = block.Body
		bodyBaseLine = block.Line
	} else {
		for i := pos.Line; i >= 0 && i < len(lines); i-- {
			if classLineRegex.FindStringSubmatch(cleanLine(lines[i])) != nil {
				classLine = i
				break
			}
		}

		if classLine < 0 {
			return resolveClassSymbol(scope, className)
		}

		endLine := pos.Line
		if endLine >= len(lines) {
			endLine = len(lines) - 1
		}
		if endLine < classLine {
			endLine = classLine
		}

		bodyStartLine := classLine + 1
		if bodyStartLine > endLine {
			bodyStartLine = endLine
		}

		body = strings.Join(lines[bodyStartLine:endLine+1], "\n")
		bodyBaseLine = bodyStartLine + 1
	}

	fields := scanClassFields(scope, body, "", bodyBaseLine)
	methods := map[string]SymbolInfo{}
	collectEmbeddedSymbolsFromBody(scope, body, fields, methods, "", bodyBaseLine)
	for name, method := range scanClassMethodHeaders(scope, className, body, bodyBaseLine) {
		methods[name] = method
	}

	for _, methodBlock := range findBlocks(body, "fn") {
		params := normalizeStdArgs(scope, parseFunctionParams(methodBlock.ParamsText))
		returnType := inferReturnTypeFromBody(scope, methodBlock.Body, methodBlock.ReturnType)
		detail := "method " + className + "." + methodBlock.Name
		if isPrivateFunctionAt(body, methodBlock.Start) {
			detail = "private " + detail
		}
		methods[methodBlock.Name] = SymbolInfo{
			Name:    methodBlock.Name,
			Kind:    SymbolFunction,
			Type:    "function",
			Detail:  detail,
			Line:    bodyBaseLine + methodBlock.Line,
			Column:  methodBlock.Column,
			Params:  params,
			Returns: returnType,
		}
	}

	classSym, ok := resolveClassSymbol(scope, className)
	if !ok {
		classSym = SymbolInfo{
			Name:    className,
			Kind:    SymbolClass,
			Type:    "class:" + className,
			Detail:  "class " + className,
			Line:    classLine + 1,
			Column:  1,
			Fields:  map[string]SymbolInfo{},
			Methods: map[string]SymbolInfo{},
		}
	}

	if classSym.Fields == nil {
		classSym.Fields = map[string]SymbolInfo{}
	}
	for name, field := range fields {
		classSym.Fields[name] = field
	}

	if classSym.Methods == nil {
		classSym.Methods = map[string]SymbolInfo{}
	}
	for name, method := range methods {
		classSym.Methods[name] = method
	}

	return classSym, true
}

func scanClassMethodHeaders(scope *Scope, className string, body string, bodyBaseLine int) map[string]SymbolInfo {
	methods := map[string]SymbolInfo{}
	lines := strings.Split(body, "\n")

	for i, raw := range lines {
		line := cleanLine(raw)
		match := functionLineRegex.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		name := match[1]
		if name == "" {
			continue
		}

		detail := "method " + className + "." + name
		fnIndex := strings.Index(line, "fn")
		if fnIndex > 0 && strings.Contains(line[:fnIndex], "private") {
			detail = "private " + detail
		}

		returnType := "undefined"
		if len(match) > 3 && strings.TrimSpace(match[3]) != "" {
			returnType = normalizeLSPType(scope, match[3])
		}

		methods[name] = SymbolInfo{
			Name:    name,
			Kind:    SymbolFunction,
			Type:    "function",
			Detail:  detail,
			Line:    bodyBaseLine + i,
			Column:  indexColumn(raw, name),
			Params:  normalizeStdArgs(scope, parseFunctionParams(match[2])),
			Returns: returnType,
		}
	}

	return methods
}

func completionItemsForReceiver(scope *Scope, text string, pos Position, receiver string) []CompletionItem {
	sym, typ, ok := resolveReceiverPath(scope, text, pos, receiver)
	if !ok {
		return []CompletionItem{}
	}

	if strings.Contains(typ, "|") {
		return getUnionTypeCompletions(scope, typ, receiver)
	}

	if sym.Kind == SymbolNamespace || sym.Kind == SymbolEnum {
		return completionItemsFromMembers(sym.Members)
	}

	if strings.HasPrefix(typ, "std:") {
		module := strings.TrimPrefix(typ, "std:")
		return getStdCompletions(module)
	}

	if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")
		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			classSym = SymbolInfo{Kind: SymbolClass}
		}
		if len(sym.Fields) > 0 || len(sym.Methods) > 0 {
			if classSym.Fields == nil {
				classSym.Fields = map[string]SymbolInfo{}
			}
			for name, field := range sym.Fields {
				classSym.Fields[name] = field
			}
			if classSym.Methods == nil {
				classSym.Methods = map[string]SymbolInfo{}
			}
			for name, method := range sym.Methods {
				classSym.Methods[name] = method
			}
		}
		return completionItemsForClass(scope, classSym, receiver)
	}

	if typ == "object" {
		items := []CompletionItem{}
		for _, field := range sym.Fields {
			items = append(items, CompletionItem{Label: field.Name, Kind: symbolKindToCompletionKind(field.Kind), Detail: field.Detail + " : " + field.Type})
		}
		items = append(items, getNativeTypeCompletions("object")...)
		return dedupeCompletionItems(items)
	}

	return getNativeTypeCompletions(typ)
}

func getUnionTypeCompletions(scope *Scope, typ string, receiver string) []CompletionItem {
	items := []CompletionItem{}

	for _, part := range splitUnionType(typ) {
		if isNullishLSPType(part) {
			continue
		}

		if strings.HasPrefix(part, "class:") {
			className := strings.TrimPrefix(part, "class:")

			classSym, ok := resolveClassSymbol(scope, className)
			if !ok || classSym.Kind != SymbolClass {
				continue
			}

			for _, field := range classSym.Fields {
				if isPrivateSymbol(field) && receiver != "this" {
					continue
				}

				items = append(items, CompletionItem{
					Label:  field.Name,
					Kind:   symbolKindToCompletionKind(field.Kind),
					Detail: field.Detail + " : " + field.Type,
				})
			}

			for _, method := range classSym.Methods {
				if isPrivateSymbol(method) && receiver != "this" {
					continue
				}

				items = append(items, CompletionItem{
					Label:  method.Name,
					Kind:   2,
					Detail: formatFunctionSignature(method.Name, method.Params, method.Returns),
				})
			}

			continue
		}

		if strings.HasPrefix(part, "std:") {
			module := strings.TrimPrefix(part, "std:")
			items = append(items, getStdCompletions(module)...)
			continue
		}

		if part == "object" {
			items = append(items, getNativeTypeCompletions("object")...)
			continue
		}

		items = append(items, getNativeTypeCompletions(part)...)
	}

	return dedupeCompletionItems(items)
}

func getCompletions(uri string, text string, pos Position) []CompletionItem {
	line := getLine(text, pos.Line)

	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	if isInsideStdImportString(line, pos.Character) {
		return stdModuleNameCompletions()
	}

	before := line[:pos.Character]
	receiver := receiverBeforeDot(before)

	scope := scopeAtPosition(uri, text, pos)

	if receiver == "" {
		return scopeCompletions(scope, uri, text)
	}

	return completionItemsForReceiver(scope, text, pos, receiver)
}

func completionItemsFromMembers(members map[string]SymbolInfo) []CompletionItem {
	items := []CompletionItem{}
	names := make([]string, 0, len(members))

	for name := range members {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		member := members[name]
		detail := symbolDetail(member)
		if member.Kind == SymbolFunction {
			detail = formatFunctionSignature(member.Name, member.Params, member.Returns)
		}

		items = append(items, CompletionItem{
			Label:            member.Name,
			Kind:             symbolKindToCompletionKind(member.Kind),
			Detail:           detail,
			InsertText:       completionInsertText(member),
			InsertTextFormat: completionInsertTextFormat(member),
		})
	}

	return items
}

func callableInsertText(name string) string {
	return name + "($0);"
}

func completionInsertText(sym SymbolInfo) string {
	switch sym.Kind {
	case SymbolFunction, SymbolClass:
		return callableInsertText(sym.Name)
	default:
		return ""
	}
}

func completionInsertTextFormat(sym SymbolInfo) int {
	if completionInsertText(sym) == "" {
		return 0
	}
	return 2
}

func symbolDetail(sym SymbolInfo) string {
	switch sym.Kind {
	case SymbolFunction:
		return formatFunctionSignature(sym.Name, sym.Params, sym.Returns)
	case SymbolClass:
		return sym.Detail
	case SymbolNamespace:
		return sym.Detail
	case SymbolEnum:
		return sym.Detail
	case SymbolField:
		return "field " + sym.Name + " : " + sym.Type
	default:
		if sym.Type != "" {
			return sym.Detail + " : " + sym.Type
		}
		return sym.Detail
	}
}

func getLine(text string, lineNumber int) string {
	lines := strings.Split(text, "\n")

	if lineNumber < 0 || lineNumber >= len(lines) {
		return ""
	}

	return lines[lineNumber]
}

func receiverBeforeDot(text string) string {
	text = strings.TrimRight(text, " \t")

	if !strings.HasSuffix(text, ".") && !strings.HasSuffix(text, "?.") {
		return ""
	}

	if strings.HasSuffix(text, "?.") {
		text = strings.TrimSuffix(text, "?.")
	} else {
		text = strings.TrimSuffix(text, ".")
	}

	text = strings.TrimRight(text, " \t")

	i := len(text) - 1
	for i >= 0 {
		ch := text[i]
		if isIdentChar(ch) || ch == '.' || ch == '?' {
			i--
			continue
		}
		break
	}

	receiver := strings.TrimSpace(text[i+1:])
	receiver = strings.TrimSuffix(receiver, "?")
	return receiver
}

func parseStdImports(text string) map[string]string {
	result := map[string]string{}

	// import std "io";
	// import std "json" as j;
	re := regexp.MustCompile(`import\s+std\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?`)

	matches := re.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		module := match[1]
		alias := module

		if len(match) > 2 && match[2] != "" {
			alias = match[2]
		}

		result[alias] = module
	}

	return result
}

func symbolKindToCompletionKind(kind SymbolKind) int {
	switch kind {
	case SymbolFunction:
		return 3
	case SymbolClass:
		return 7
	case SymbolVariable:
		return 6
	case SymbolStd:
		return 9
	case SymbolNamespace:
		return 9
	case SymbolField:
		return 5
	case SymbolEnum:
		return 13
	default:
		return 6
	}
}
