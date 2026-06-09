package main

import (
	"bufio"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
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

type InlayHintParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        LSPRange               `json:"range"`
}

type InlayHint struct {
	Position     Position `json:"position"`
	Label        string   `json:"label"`
	Kind         int      `json:"kind,omitempty"`
	PaddingLeft  bool     `json:"paddingLeft,omitempty"`
	PaddingRight bool     `json:"paddingRight,omitempty"`
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

type DidSaveParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Text         string                 `json:"text,omitempty"`
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

type CodeActionContext struct {
	Diagnostics []map[string]any `json:"diagnostics"`
}

type CodeActionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Range        LSPRange               `json:"range"`
	Context      CodeActionContext      `json:"context"`
}

type Command struct {
	Title     string `json:"title"`
	Command   string `json:"command"`
	Arguments []any  `json:"arguments,omitempty"`
}

type CodeAction struct {
	Title       string           `json:"title"`
	Kind        string           `json:"kind,omitempty"`
	Diagnostics []map[string]any `json:"diagnostics,omitempty"`
	Edit        WorkspaceEdit    `json:"edit,omitempty"`
	Command     *Command         `json:"command,omitempty"`
}

type CompletionItem struct {
	Label               string     `json:"label"`
	Kind                int        `json:"kind,omitempty"`
	Detail              string     `json:"detail,omitempty"`
	InsertText          string     `json:"insertText,omitempty"`
	InsertTextFormat    int        `json:"insertTextFormat,omitempty"`
	SortText            string     `json:"sortText,omitempty"`
	AdditionalTextEdits []TextEdit `json:"additionalTextEdits,omitempty"`
	TextEdit            *TextEdit  `json:"textEdit,omitempty"`
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

type CallHierarchyPrepareParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CallHierarchyItem struct {
	Name           string   `json:"name"`
	Kind           int      `json:"kind"`
	Detail         string   `json:"detail,omitempty"`
	URI            string   `json:"uri"`
	Range          LSPRange `json:"range"`
	SelectionRange LSPRange `json:"selectionRange"`
	Data           any      `json:"data,omitempty"`
}

type CallHierarchyIncomingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

type CallHierarchyIncomingCall struct {
	From       CallHierarchyItem `json:"from"`
	FromRanges []LSPRange        `json:"fromRanges"`
}

type CallHierarchyOutgoingCallsParams struct {
	Item CallHierarchyItem `json:"item"`
}

type CallHierarchyOutgoingCall struct {
	To         CallHierarchyItem `json:"to"`
	FromRanges []LSPRange        `json:"fromRanges"`
}


//go:embed lsp_stubs/*
var lspStubs embed.FS

var tinyKeywords = map[string]bool{
	"import": true,
	"std":    true,
	"as":     true,
	"export": true,
	"await":  true,
	"async":  true,

	"fn":        true,
	"let":       true,
	"const":     true,
	"class":     true,
	"embed":     true,
	"native":    true,
	"field":     true,
	"private":   true,
	"public":    true,
	"enum":      true,
	"iota":      true,
	"interface": true,
	"embedstr":  true,
	"embedbin":  true,

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
	"lock":     true,
	"finally":  true,
	"throw":    true,
	"defer":    true,

	"true":  true,
	"false": true,
	"null":  true,

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

func URIToPath(uriStr string) string {
	if !strings.HasPrefix(uriStr, "file:") {
		return uriStr
	}
	u, err := url.Parse(uriStr)
	if err != nil {
		path := strings.TrimPrefix(uriStr, "file://")
		path = strings.ReplaceAll(path, "%3A", ":")
		return filepath.FromSlash(path)
	}

	path := u.Path

	if runtime.GOOS == "windows" {
		if len(path) > 0 && path[0] == '/' {
			path = path[1:]
		}
		path = strings.ReplaceAll(path, "/", "\\")
	}

	return filepath.Clean(path)
}

func formatFunctionSignature(name string, params []StdArg, returns string) string {
	parts := []string{}

	for _, arg := range params {
		label := arg.Name + ": " + arg.Type

		if arg.Variadic {
			label = "..." + arg.Name + ": " + arg.Type
		} else if arg.Optional {
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

		if arg.Variadic {
			argLabel = "..." + arg.Name + ": " + arg.Type
		} else if arg.Optional {
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

		if arg.Variadic {
			name = "..." + name
		} else if arg.Optional {
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
func getStdCompletions(module string, hasParens bool) []CompletionItem {
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
			InsertText:       callableInsertText(method.Name, hasParens),
			InsertTextFormat: 2,
		})
	}

	return items
}

func formatStdSignature(module string, method StdMethodInfo) string {
	parts := []string{}

	for _, arg := range method.Args {
		name := arg.Name

		if arg.Variadic {
			name = "..." + name
		} else if arg.Optional {
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

		if strings.Contains(receiverType, "|") {
			for _, part := range splitUnionType(receiverType) {
				if !isNullishLSPType(part) {
					receiverType = part
					break
				}
			}
		}


		if sym.Kind == SymbolNamespace {
			member, ok := sym.Members[ctx.Method]
			if !ok {
				return nil
			}

			if member.Kind == SymbolFunction {
				return signatureHelpFromFunction(member, ctx.ArgIndex)
			}
			if member.Kind == SymbolClass {
				return signatureHelpFromFunction(constructorSymbolFromClass(member, ctx.Method), ctx.ArgIndex)
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

	sym, exists := scope.Resolve(ctx.Name)
	if !exists {
		return nil
	}

	if sym.Kind == SymbolFunction {
		return signatureHelpFromFunction(sym, ctx.ArgIndex)
	}
	if sym.Kind == SymbolClass {
		return signatureHelpFromFunction(constructorSymbolFromClass(sym, sym.Name), ctx.ArgIndex)
	}

	return nil
}

func constructorSymbolFromClass(classSym SymbolInfo, displayName string) SymbolInfo {
	returns := classSym.Type
	if returns == "" {
		returns = "class:" + displayName
	}

	if initSym, ok := classSym.Methods["init"]; ok {
		initSym.Name = displayName
		initSym.Kind = SymbolFunction
		initSym.Type = "function"
		initSym.Returns = returns
		if strings.TrimSpace(initSym.Detail) == "" {
			initSym.Detail = "constructor " + displayName
		}
		return initSym
	}

	return SymbolInfo{
		Name:    displayName,
		Kind:    SymbolFunction,
		Type:    "function",
		Detail:  "constructor " + displayName,
		Returns: returns,
	}
}

func signatureHelpFromFunction(sym SymbolInfo, activeParam int) SignatureHelp {
	parts := []string{}
	params := []ParameterInformation{}

	for _, arg := range sym.Params {
		label := arg.Name + ": " + arg.Type

		if arg.Variadic {
			label = "..." + arg.Name + ": " + arg.Type
		} else if arg.Optional {
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
	line := getLine(text, pos.Line)
	if loc, ok := importLocationAtPosition(uri, line, pos); ok {
		return loc
	}
	if isPositionInStringOrComment(line, pos.Character) {
		return nil
	}

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

func importLocationAtPosition(uri string, line string, pos Position) (Location, bool) {
	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	quoteStart := strings.LastIndex(line[:pos.Character], `"`)
	if quoteStart < 0 {
		return Location{}, false
	}
	quoteEnd := strings.Index(line[quoteStart+1:], `"`)
	if quoteEnd < 0 || quoteStart+1+quoteEnd < pos.Character {
		return Location{}, false
	}

	prefix := strings.TrimSpace(line[:quoteStart])
	pathText := line[quoteStart+1 : quoteStart+1+quoteEnd]
	targetPath := ""

	switch {
	case strings.Contains(prefix, "import lib"):
		targetPath = resolveLibraryImportPath(pathText)
	case strings.Contains(prefix, "import std"):
		targetPath = "std:" + pathText
	case strings.Contains(prefix, "import"):
		targetPath = resolveImportPath(uri, pathText)
	default:
		return Location{}, false
	}

	targetURI := pathToFileURI(targetPath)
	return Location{
		URI: targetURI,
		Range: LSPRange{
			Start: Position{Line: 0, Character: 0},
			End:   Position{Line: 0, Character: 0},
		},
	}, true
}

func getReferences(uri string, text string, pos Position, includeDeclaration bool) []Location {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] {
		return []Location{}
	}

	scope := scopeAtPosition(uri, text, pos)

	targetSym, exists := symbolAtPositionForReferences(text, pos, scope)
	if !exists {
		return []Location{}
	}

	docs := collectReferenceDocuments(uri, text)
	locations := []Location{}

	for docURI, docText := range docs {
		for _, rng := range identifierRangesInText(docText, name) {
			if !includeDeclaration && docURI == uri && positionInByteRange(pos, rng) {
				continue
			}

			matchPos := Position{Line: rng.Line, Character: rng.Start}
			matchScope := scopeAtPosition(docURI, docText, matchPos)

			resolvedSym, ok := symbolAtPositionForReferences(docText, matchPos, matchScope)

			if ok && sameReferencedSymbol(resolvedSym, targetSym) {
				locations = append(locations, Location{
					URI:   docURI,
					Range: lspRangeFromByteColumns(docText, rng.Line, rng.Start, rng.End),
				})
			}
		}
	}

	sort.SliceStable(locations, func(i, j int) bool {
		if locations[i].URI != locations[j].URI {
			return locations[i].URI < locations[j].URI
		}

		if locations[i].Range.Start.Line != locations[j].Range.Start.Line {
			return locations[i].Range.Start.Line > locations[j].Range.Start.Line
		}
		return locations[i].Range.Start.Character > locations[j].Range.Start.Character
	})

	return locations
}

func symbolAtPositionForReferences(text string, pos Position, scope *Scope) (SymbolInfo, bool) {
	if receiver, member, ok := memberExprAtPosition(text, pos); ok {
		receiverSym, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
		if !exists {
			return SymbolInfo{}, false
		}
		if receiverSym.Kind == SymbolNamespace || receiverSym.Kind == SymbolEnum {
			memberSym, ok := receiverSym.Members[member]
			return memberSym, ok
		}
		if strings.HasPrefix(receiverType, "class:") {
			classSym, ok := resolveClassSymbol(scope, strings.TrimPrefix(receiverType, "class:"))
			if !ok {
				return SymbolInfo{}, false
			}
			if methodSym, ok := classSym.Methods[member]; ok {
				return methodSym, true
			}
			if fieldSym, ok := classSym.Fields[member]; ok {
				return fieldSym, true
			}
		}
	}
	return scope.Resolve(wordAtPosition(text, pos))
}

func sameReferencedSymbol(left SymbolInfo, right SymbolInfo) bool {
	if left.SourceURI != "" && right.SourceURI != "" && left.SourceURI != right.SourceURI {
		return false
	}
	if left.Line > 0 && right.Line > 0 {
		return left.Line == right.Line && left.Column == right.Column && left.Name == right.Name
	}
	return left.Name == right.Name && left.Kind == right.Kind && left.Type == right.Type
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

func getInlayHints(uri string, text string, rng LSPRange) []InlayHint {
	lines := strings.Split(text, "\n")
	startLine := rng.Start.Line
	endLine := rng.End.Line
	if startLine < 0 {
		startLine = 0
	}
	if endLine >= len(lines) || endLine < startLine {
		endLine = len(lines) - 1
	}

	hints := variableTypeInlayHintsForText(uri, text, rng)
	hints = append(hints, parameterInlayHintsForText(uri, text, rng)...)

	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].Position.Line != hints[j].Position.Line {
			return hints[i].Position.Line < hints[j].Position.Line
		}
		return hints[i].Position.Character < hints[j].Position.Character
	})
	return hints
}

func variableTypeInlayHintsForText(uri string, text string, rng LSPRange) []InlayHint {
	lines := strings.Split(text, "\n")
	startRegex := regexp.MustCompile(`^\s*(?:export\s+)?(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*=\s*`)
	hints := []InlayHint{}

	for lineIndex, line := range lines {
		if !positionInRange(Position{Line: lineIndex, Character: 0}, LSPRange{
			Start: Position{Line: rng.Start.Line, Character: 0},
			End:   Position{Line: rng.End.Line, Character: len(line)},
		}) {
			continue
		}

		code := stripLineComment(line)
		match := startRegex.FindStringSubmatchIndex(code)
		if match == nil || match[4] >= 0 {
			continue
		}

		name := code[match[2]:match[3]]
		exprStart := offsetAtLine(text, lineIndex+1) + match[1]
		exprEnd := variableInitializerEnd(text, exprStart)
		if exprEnd < exprStart {
			continue
		}

		scope := scopeAtPosition(uri, text, Position{Line: lineIndex, Character: len(line)})
		expr := strings.TrimSpace(text[exprStart:exprEnd])
		typ := inferExprTypeFromText(scope, expr)
		if (typ == "" || typ == "any" || typ == "unknown") && expr != "" {
			if fallback, ok := inferNamespaceFunctionReturnFromText(scope, text, expr); ok {
				typ = fallback
			}
		}
		typ = normalizeLSPType(scope, typ)
		if typ == "" || typ == "any" || typ == "unknown" {
			continue
		}

		typ = strings.TrimPrefix(strings.TrimPrefix(typ, "class:"), "interface:")

		hints = append(hints, InlayHint{
			Position:    Position{Line: lineIndex, Character: match[3]},
			Label:       ": " + typ,
			Kind:        1,
			PaddingLeft: false,
		})
		_ = name
	}

	return hints
}

func parameterInlayHintsForText(uri string, text string, rng LSPRange) []InlayHint {
	hints := []InlayHint{}
	callRegex := regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)?)\s*\(`)
	matches := callRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		name := text[match[2]:match[3]]
		if tinyKeywords[name] {
			continue
		}

		open := match[1] - 1
		close := findMatching(text, open, '(', ')')
		if close < 0 {
			continue
		}

		callPos := bytePositionAtOffset(text, match[2])
		line := getLine(text, callPos.Line)
		if isPositionInStringOrComment(line, callPos.Character) {
			continue
		}
		if isFunctionDeclarationNameAt(line, callPos.Character) {
			continue
		}

		scope := scopeAtPosition(uri, text, callPos)
		params := paramsForCallName(scope, text, callPos, name)
		if len(params) == 0 {
			continue
		}

		argsText := text[open+1 : close]
		argStarts := topLevelArgumentStarts(argsText)
		for i, relStart := range argStarts {
			if i >= len(params) {
				break
			}
			param := params[i]
			if param.Name == "" || param.Name == "_" {
				continue
			}
			hintPos := bytePositionAtOffset(text, open+1+relStart)
			if !positionInRange(hintPos, rng) {
				continue
			}
			hints = append(hints, InlayHint{
				Position:     hintPos,
				Label:        param.Name + ":",
				Kind:         2,
				PaddingRight: true,
			})
		}
	}
	return hints
}

func isFunctionDeclarationNameAt(line string, nameStart int) bool {
	if nameStart < 0 || nameStart > len(line) {
		return false
	}
	before := strings.TrimSpace(line[:nameStart])
	return strings.HasSuffix(before, "fn") ||
		strings.HasSuffix(before, "export fn") ||
		strings.HasSuffix(before, "native fn") ||
		strings.HasSuffix(before, "export native fn")
}

func bytePositionAtOffset(text string, offset int) Position {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}

	line := 0
	lineStart := 0
	for i := 0; i < offset; i++ {
		if text[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}
	return Position{Line: line, Character: offset - lineStart}
}

func positionInRange(pos Position, rng LSPRange) bool {
	if pos.Line < rng.Start.Line || pos.Line > rng.End.Line {
		return false
	}
	if pos.Line == rng.Start.Line && pos.Character < rng.Start.Character {
		return false
	}
	if pos.Line == rng.End.Line && pos.Character > rng.End.Character {
		return false
	}
	return true
}

func paramsForCallName(scope *Scope, text string, pos Position, name string) []StdArg {
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		member := parts[len(parts)-1]
		receiver := strings.Join(parts[:len(parts)-1], ".")
		if params, ok := namespaceFunctionParamsFromText(scope, text, receiver, member); ok {
			return params
		}
		sym, receiverType, ok := resolveReceiverPath(scope, text, pos, receiver)
		if !ok {
			return nil
		}
		if sym.Kind == SymbolNamespace {
			if memberSym, ok := sym.Members[member]; ok {
				if memberSym.Kind == SymbolClass {
					return constructorSymbolFromClass(memberSym, memberSym.Name).Params
				}
				return memberSym.Params
			}
		}
		if strings.HasPrefix(receiverType, "class:") {
			if classSym, ok := resolveClassSymbol(scope, strings.TrimPrefix(receiverType, "class:")); ok {
				if methodSym, ok := classSym.Methods[member]; ok {
					return methodSym.Params
				}
			}
		}
		if methodInfo, ok := GetNativeMethodInfo(receiverType, member); ok {
			return methodInfo.Args
		}
		return nil
	}

	if sym, ok := scope.Resolve(name); ok {
		if sym.Kind == SymbolClass {
			return constructorSymbolFromClass(sym, sym.Name).Params
		}
		return sym.Params
	}
	return nil
}

func namespaceFunctionParamsFromText(scope *Scope, text string, namespace string, member string) ([]StdArg, bool) {
	for _, nsBlock := range findBlocks(text, "namespace") {
		if nsBlock.Name != namespace {
			continue
		}
		for _, fnBlock := range findBlocks(nsBlock.Body, "fn") {
			if fnBlock.Name == member {
				return normalizeStdArgs(scope, parseFunctionParams(fnBlock.ParamsText)), true
			}
		}
	}
	return nil, false
}

func topLevelArgumentStarts(argsText string) []int {
	args := splitTopLevel(argsText, ',')
	starts := []int{}
	offset := 0
	for _, arg := range args {
		trimmed := strings.TrimSpace(arg)
		if trimmed == "" {
			offset += len(arg) + 1
			continue
		}
		leading := len(arg) - len(strings.TrimLeft(arg, " \t\r\n"))
		starts = append(starts, offset+leading)
		offset += len(arg) + 1
	}
	return starts
}

func getCodeActions(uri string, text string, params CodeActionParams) []CodeAction {
	lineIndex := params.Range.Start.Line
	line := getLine(text, lineIndex)
	actions := []CodeAction{}

	if action, ok := inferredTypeHintAction(uri, text, line, lineIndex); ok {
		actions = append(actions, action)
	}
	if action, ok := removeImportAction(uri, line, lineIndex); ok {
		actions = append(actions, action)
	}
	if action, ok := createMissingFunctionAction(uri, text, params.Range.Start); ok {
		actions = append(actions, action)
	}
	if action, ok := addImportForSymbolAction(uri, text, params.Range.Start); ok {
		actions = append(actions, action)
	}
	if action, ok := installMissingLibraryAction(uri, line); ok {
		actions = append(actions, action)
	}

	return actions
}

func inferredTypeHintAction(uri string, text string, line string, lineIndex int) (CodeAction, bool) {
	match := variableLineRegex.FindStringSubmatchIndex(line)
	if match == nil || match[4] >= 0 {
		return CodeAction{}, false
	}
	scope := scopeAtPosition(uri, text, Position{Line: lineIndex, Character: len(line)})
	typ := inferExprTypeFromText(scope, strings.TrimSpace(line[match[6]:match[7]]))
	typ = normalizeLSPType(scope, typ)
	if typ == "" || typ == "any" || typ == "unknown" {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Add inferred type hint",
		Kind:  "quickfix",
		Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
			Range:   lspRangeFromByteColumns(text, lineIndex, match[3], match[3]),
			NewText: ": " + typ,
		}}}},
	}, true
}

func removeImportAction(uri string, line string, lineIndex int) (CodeAction, bool) {
	if !strings.Contains(strings.TrimSpace(line), "import") {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Remove import",
		Kind:  "quickfix",
		Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
			Range: LSPRange{
				Start: Position{Line: lineIndex, Character: 0},
				End:   Position{Line: lineIndex + 1, Character: 0},
			},
			NewText: "",
		}}}},
	}, true
}

func createMissingFunctionAction(uri string, text string, pos Position) (CodeAction, bool) {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] {
		return CodeAction{}, false
	}
	scope := scopeAtPosition(uri, text, pos)
	if _, ok := scope.Resolve(name); ok {
		return CodeAction{}, false
	}
	line := getLine(text, pos.Line)
	if !strings.Contains(line, name+"(") {
		return CodeAction{}, false
	}
	endLine := len(strings.Split(text, "\n"))
	return CodeAction{
		Title: "Create function '" + name + "'",
		Kind:  "quickfix",
		Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
			Range:   LSPRange{Start: Position{Line: endLine, Character: 0}, End: Position{Line: endLine, Character: 0}},
			NewText: "\nfn " + name + "() {\n}\n",
		}}}},
	}, true
}

func addImportForSymbolAction(uri string, text string, pos Position) (CodeAction, bool) {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] {
		return CodeAction{}, false
	}
	scope := scopeAtPosition(uri, text, pos)
	if _, ok := scope.Resolve(name); ok {
		return CodeAction{}, false
	}
	base := filepath.Dir(URIToPath(uri))

	projectFiles := scanProjectTinyFiles(URIToPath(uri))
	fileSet := map[string]bool{}
	for _, f := range projectFiles {
		fileSet[filepath.Clean(f)] = true
	}
	for openPath := range lspDocs {
		fileSet[filepath.Clean(openPath)] = true
	}

	for path := range fileSet {
		if filepath.Clean(path) == filepath.Clean(URIToPath(uri)) {
			continue
		}
		exports := loadTinyFileExports(path, map[string]bool{})
		if _, ok := exports[name]; !ok {
			continue
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			rel = path
		}
		rel = filepath.ToSlash(rel)
		if filepath.Dir(rel) != "." && !strings.HasPrefix(rel, ".") && !strings.HasPrefix(rel, "/") {
			rel = "./" + rel
		}
		alias := importAliasForPath(path)
		return CodeAction{
			Title: "Import '" + name + "'",
			Kind:  "quickfix",
			Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
				Range:   LSPRange{Start: Position{Line: importInsertLine(text), Character: 0}, End: Position{Line: importInsertLine(text), Character: 0}},
				NewText: "import \"" + rel + "\" as " + alias + ";\n",
			}}}},
		}, true
	}
	return CodeAction{}, false
}

func installMissingLibraryAction(uri string, line string) (CodeAction, bool) {
	match := libraryImportRegex.FindStringSubmatch(line)
	if match == nil {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Install library '" + match[1] + "'",
		Kind:  "quickfix",
		Command: &Command{
			Title:     "Install library '" + match[1] + "'",
			Command:   "tiny.installLibrary",
			Arguments: []any{uri, match[1]},
		},
	}, true
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

	libraryMatches := libraryImportRegex.FindAllStringSubmatch(text, -1)
	for _, match := range libraryMatches {
		resolved := resolveLibraryImportPath(match[1])
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

	// interface Name
	if match := regexp.MustCompile(`^interface\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "interface", 11), true
	}

	// export interface Name
	if match := regexp.MustCompile(`^export\s+interface\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		return makeDocumentSymbol(rawLine, lineIndex, match[1], "export interface", 11), true
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

	// embedStr / embedBin "path" const/let name
	if match := regexp.MustCompile(`^(embedStr|embedBin)\s+"[^"]+"\s+(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		kind := match[1] // "embedStr" or "embedBin"
		name := match[2]
		return makeDocumentSymbol(rawLine, lineIndex, name, kind, 13), true
	}

	// export embedStr / export embedBin "path" const/let name
	if match := regexp.MustCompile(`^export\s+(embedStr|embedBin)\s+"[^"]+"\s+(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)`).FindStringSubmatch(line); match != nil {
		kind := match[1] // "embedStr" or "embedBin"
		name := match[2]
		return makeDocumentSymbol(rawLine, lineIndex, name, "export "+kind, 13), true
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

func refreshLSPDocument(uri string, text string) {
	path := URIToPath(uri)
	lspDocs[path] = text
	invalidateLSPImportCacheForURI(path)
	publishDiagnostics(uri, text)
	publishDiagnosticsForImportDependents(uri)
}

func handleLSPMessage(msg LSPMessage) {
	switch msg.Method {
	case "initialize":
		writeLSPMessage(LSPMessage{
			ID: msg.ID,
			Result: map[string]any{
				"capabilities": map[string]any{
					"textDocumentSync": map[string]any{
						"openClose": true,
						"change":    1,
						"save": map[string]any{
							"includeText": true,
						},
					},
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
					"codeActionProvider":         true,
					"inlayHintProvider":          true,
					"documentSymbolProvider":     true,
					"hoverProvider":              true,
					"implementationProvider":     true,
					"callHierarchyProvider":      true,
				},
			},
		})

	case "initialized":
		publishProjectDiagnostics()

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

		refreshLSPDocument(params.TextDocument.URI, params.TextDocument.Text)
		publishProjectDiagnostics()

	case "textDocument/didChange":
		var params DidChangeParams
		json.Unmarshal(msg.Params, &params)

		if len(params.ContentChanges) > 0 {
			text := params.ContentChanges[0].Text
			refreshLSPDocument(params.TextDocument.URI, text)
		}

	case "textDocument/didSave":
		var params DidSaveParams
		json.Unmarshal(msg.Params, &params)

		text := params.Text
		if text == "" {
			if bytes, err := os.ReadFile(URIToPath(params.TextDocument.URI)); err == nil {
				text = string(bytes)
			} else {
				text = lspDocs[URIToPath(params.TextDocument.URI)]
			}
		}

		refreshLSPDocument(params.TextDocument.URI, text)
		publishProjectDiagnostics()

	case "textDocument/didClose":
		var params DidCloseParams
		json.Unmarshal(msg.Params, &params)

		delete(lspDocs, URIToPath(params.TextDocument.URI))
		invalidateLSPImportCacheForURI(URIToPath(params.TextDocument.URI))
		publishDiagnostics(params.TextDocument.URI, "")
		publishDiagnosticsForImportDependents(params.TextDocument.URI)
		publishProjectDiagnostics()

	case "textDocument/completion":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Position = lspPositionToBytePosition(text, params.Position)
		items := getCompletions(params.TextDocument.URI, text, params.Position)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: items,
		})

	case "textDocument/signatureHelp":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
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

	case "textDocument/inlayHint":
		var params InlayHintParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Range.Start = lspPositionToBytePosition(text, params.Range.Start)
		params.Range.End = lspPositionToBytePosition(text, params.Range.End)
		result := getInlayHints(params.TextDocument.URI, text, params.Range)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/codeAction":
		var params CodeActionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Range.Start = lspPositionToBytePosition(text, params.Range.Start)
		params.Range.End = lspPositionToBytePosition(text, params.Range.End)
		result := getCodeActions(params.TextDocument.URI, text, params)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/definition":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
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

		text := lspDocs[URIToPath(params.TextDocument.URI)]
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

	case "textDocument/implementation":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []Location{}
				}
			}()
			result = getImplementations(params.TextDocument.URI, text, params.Position)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(result),
		})

	case "textDocument/prepareCallHierarchy":
		var params CallHierarchyPrepareParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []CallHierarchyItem{}
				}
			}()
			result = prepareCallHierarchy(params.TextDocument.URI, text, params.Position)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: nullLSPResult(result),
		})

	case "callHierarchy/incomingCalls":
		var params CallHierarchyIncomingCallsParams
		json.Unmarshal(msg.Params, &params)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []CallHierarchyIncomingCall{}
				}
			}()
			result = getIncomingCalls(params.Item)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "callHierarchy/outgoingCalls":
		var params CallHierarchyOutgoingCallsParams
		json.Unmarshal(msg.Params, &params)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []CallHierarchyOutgoingCall{}
				}
			}()
			result = getOutgoingCalls(params.Item)
		}()

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
		})

	case "textDocument/rename":
		var params RenameParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
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

		text := lspDocs[URIToPath(params.TextDocument.URI)]

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

		text := lspDocs[URIToPath(params.TextDocument.URI)]

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

		text := lspDocs[URIToPath(params.TextDocument.URI)]
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

	_, parseDiagnostics := parseTinyForLSP(URIToPath(uri), text)

	diagnostics := []map[string]any{}

	for _, diagnostic := range parseDiagnostics {
		line := diagnostic.Line
		column := diagnostic.Column - 1

		if line < 0 || column < 0 {
			name := extractNameFromMessage(diagnostic.Message)
			if name != "" {
				l, c := findWordFirstOccurrence(text, name)
				line = l - 1
				column = c - 1
			}
		}

		if line < 0 {
			line = 0
		}
		if column < 0 {
			column = 0
		}

		lineText := getLine(text, line)
		wordLen := wordLengthAtColumn(lineText, column)

		diagnostics = append(diagnostics, map[string]any{
			"range": map[string]any{
				"start": map[string]any{
					"line":      line,
					"character": column,
				},
				"end": map[string]any{
					"line":      line,
					"character": column + wordLen,
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
	diagnostics = append(diagnostics, importDiagnostics(uri, text)...)

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

func publishProjectDiagnostics() {
	var startPath string
	for path := range lspDocs {
		startPath = path
		break
	}
	projectFiles := scanProjectTinyFiles(startPath)
	for _, path := range projectFiles {
		uri := pathToFileURI(path)
		text := lspDocs[path]
		if text == "" {
			bytes, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			text = string(bytes)
		}
		publishDiagnostics(uri, text)
	}
}

func importDiagnostics(uri string, text string) []map[string]any {
	diagnostics := []map[string]any{}
	lines := strings.Split(text, "\n")
	for lineIndex, line := range lines {
		code := stripLineComment(line)
		for _, imp := range importPathsInLine(code) {
			resolved := ""
			message := ""
			switch {
			case imp.Kind == "library":
				resolved = resolveLibraryImportPath(imp.Path)
				if resolved == imp.Path {
					message = "library is not installed: " + imp.Path
				} else if _, err := os.Stat(resolved); err != nil {
					message = "library entry file not found: " + imp.Path
				}
			case imp.Kind == "file":
				resolved = resolveImportPath(uri, imp.Path)
				if _, err := os.Stat(resolved); err != nil {
					message = "import file not found: " + imp.Path
				}
			case imp.Kind == "plugin":
				resolved = resolveImportPath(uri, imp.Path)
				if _, err := os.Stat(resolved); err != nil {
					message = "plugin file not found: " + imp.Path
				}
			}
			if message == "" {
				continue
			}
			diagnostics = append(diagnostics, map[string]any{
				"range":    lspRangeFromByteColumns(text, lineIndex, imp.Start, imp.End),
				"severity": 1,
				"message":  message,
				"source":   "tiny",
			})
		}
	}
	return diagnostics
}

type importPathInLine struct {
	Kind  string
	Path  string
	Start int
	End   int
}

func importPathsInLine(line string) []importPathInLine {
	result := []importPathInLine{}
	re := regexp.MustCompile(`\bimport\s+(?:(std|plugin|library|lib)\s+)?"([^"]+)"`)
	for _, match := range re.FindAllStringSubmatchIndex(line, -1) {
		kind := "file"
		if match[2] >= 0 {
			kind = line[match[2]:match[3]]
			if kind == "lib" {
				kind = "library"
			}
		}
		result = append(result, importPathInLine{
			Kind:  kind,
			Path:  line[match[4]:match[5]],
			Start: match[4],
			End:   match[5],
		})
	}
	return result
}

func publishDiagnosticsForImportDependents(changedURI string) {
	for _, uri := range dependentDocumentURIs(changedURI) {
		if text, ok := lspDocs[uri]; ok {
			publishDiagnostics(uri, text)
		}
	}
}

func dependentDocumentURIs(changedURI string) []string {
	changedPath := filepath.Clean(URIToPath(changedURI))
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

	for _, match := range libraryImportRegex.FindAllStringSubmatch(text, -1) {
		importPath := filepath.Clean(resolveLibraryImportPath(match[1]))
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

func isInsideLibraryImportString(line string, character int) bool {
	if character > len(line) {
		character = len(line)
	}

	before := line[:character]

	return strings.Contains(before, `import lib "`)
}

func isInsidePluginImportString(line string, character int) bool {
	if character > len(line) {
		character = len(line)
	}
	return strings.Contains(line[:character], `import plugin "`)
}

func isInsideFileImportString(line string, character int) bool {
	if character > len(line) {
		character = len(line)
	}
	before := line[:character]
	return strings.Contains(before, `import "`) && !strings.Contains(before, `import std "`) && !strings.Contains(before, `import lib "`) && !strings.Contains(before, `import plugin "`)
}

func pluginImportPathCompletions() []CompletionItem {
	items := []CompletionItem{}
	for _, path := range configuredPluginPaths(defaultProjectTarget()) {
		items = append(items, CompletionItem{
			Label:  filepath.ToSlash(path),
			Kind:   17,
			Detail: "configured plugin",
		})
	}
	return dedupeCompletionItems(items)
}

func fileImportPathCompletions(uri string, line string, character int) []CompletionItem {
	prefix := fileImportPrefixAt(line, character)
	baseDir := filepath.Dir(URIToPath(uri))
	dir := baseDir
	relDir := filepath.Dir(prefix)
	if relDir != "." && relDir != "" {
		dir = filepath.Join(baseDir, filepath.FromSlash(relDir))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}

	basePrefix := ""
	if relDir != "." && relDir != "" {
		basePrefix = filepath.ToSlash(relDir) + "/"
	}
	items := []CompletionItem{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			items = append(items, CompletionItem{Label: basePrefix + name + "/", Kind: 19, Detail: "folder"})
			continue
		}
		if filepath.Ext(name) == ".tiny" {
			items = append(items, CompletionItem{Label: strings.TrimSuffix(basePrefix+name, ".tiny"), Kind: 17, Detail: "tiny file"})
		}
	}
	return dedupeCompletionItems(items)
}

func fileImportPrefixAt(line string, character int) string {
	if character > len(line) {
		character = len(line)
	}
	before := line[:character]
	idx := strings.LastIndex(before, `import "`)
	if idx < 0 {
		return ""
	}
	return before[idx+len(`import "`):]
}

func libraryImportPathCompletions(line string, character int) []CompletionItem {
	prefix := libraryImportPrefixAt(line, character)
	if prefix == "" || strings.Count(prefix, "/") < 2 {
		return libraryPackageCompletions()
	}

	lib, ok := parseLibraryImportPath(prefix)
	if !ok {
		return libraryPackageCompletions()
	}

	root := ""
	version := dependencyVersionForLibrary(lib.Owner, lib.Repo)
	if version != "" {
		root = libraryGlobalRoot(lib.Owner, lib.Repo, version)
	} else {
		root = firstInstalledLibraryRoot(lib.Owner, lib.Repo)
	}
	if root == "" {
		return libraryPackageCompletions()
	}

	dir := root
	restDir := filepath.Dir(lib.Rest)
	if restDir != "." && restDir != "" {
		dir = filepath.Join(root, filepath.FromSlash(restDir))
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return libraryPackageCompletions()
	}

	basePrefix := lib.Owner + "/" + lib.Repo
	if restDir != "." && restDir != "" {
		basePrefix += "/" + filepath.ToSlash(restDir)
	}

	items := []CompletionItem{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() {
			items = append(items, CompletionItem{
				Label:  basePrefix + "/" + name + "/",
				Kind:   19,
				Detail: "library folder",
			})
			continue
		}

		if filepath.Ext(name) == ".tiny" {
			items = append(items, CompletionItem{
				Label:  basePrefix + "/" + name,
				Kind:   17,
				Detail: "library file",
			})
		}
	}

	return rankedCompletionItems(dedupeCompletionItems(items))
}

func libraryImportPrefixAt(line string, character int) string {
	if character > len(line) {
		character = len(line)
	}

	before := line[:character]
	markers := `import lib "`
	idx := -1
	marker := ""
	candidateIdx := strings.LastIndex(before, markers)

	if candidateIdx > idx {
		idx = candidateIdx
		marker = markers
	}

	if idx < 0 || marker == "" {
		return ""
	}

	return before[idx+len(marker):]
}

func parseGitHubPackageSourceSafe(source string) (githubPackageSpec, error) {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "https://")
	source = strings.TrimPrefix(source, "http://")
	source = strings.TrimPrefix(source, "github.com/")
	source = strings.TrimPrefix(source, "github:")
	source = strings.TrimSuffix(source, ".git")

	ref := ""
	if at := strings.LastIndex(source, "@"); at >= 0 {
		ref = source[at+1:]
		source = source[:at]
	}
	source = strings.TrimSuffix(source, ".git")

	parts := strings.Split(source, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return githubPackageSpec{}, fmt.Errorf("expected GitHub source in the form github:owner/repo[@ref]")
	}

	return githubPackageSpec{
		Owner: parts[0],
		Repo:  parts[1],
		Ref:   ref,
	}, nil
}

func libraryPackageCompletions() []CompletionItem {
	items := []CompletionItem{}
	config, ok := loadTinyConfig()
	if !ok {
		return items
	}

	for _, dep := range config.Dependencies {
		if dep.Source == "" {
			continue
		}
		spec, err := parseGitHubPackageSourceSafe(dep.Source)
		if err == nil {
			items = append(items, CompletionItem{
				Label:  spec.Owner + "/" + spec.Repo,
				Kind:   9,
				Detail: "project dependency",
			})
		}
	}
	return rankedCompletionItems(dedupeCompletionItems(items))
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

func getNativeTypeCompletions(typeName string, hasParens bool) []CompletionItem {
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
			InsertText:       callableInsertText(method.Name, hasParens),
			InsertTextFormat: 2,
		})
	}

	items = append(items, CompletionItem{
		Label:            "toString",
		Kind:             2,
		InsertText:       callableInsertText("toString", hasParens),
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

		if arg.Variadic {
			name = "..." + name
		} else if arg.Optional {
			name += "?"
		}

		parts = append(parts, name+": "+arg.Type)
	}

	return typeName + "." + method.Name + "(" + strings.Join(parts, ", ") + "): " + method.Returns
}

func scopeCompletions(scope *Scope, uri string, text string, hasParens bool) []CompletionItem {
	items := []CompletionItem{
		snippetCompletion("import", "import statement", "import \"$1\"$0"),
		snippetCompletion("import std", "standard library import", "import std \"$1\"$0"),
		snippetCompletion("import plugin", "plugin import", "import plugin \"$1\" as ${2:Plugin}$0"),
		snippetCompletion("import library", "library import", "import lib \"$1\" as ${2:Library}$0"),
		{Label: "export", Kind: 14, Detail: "export statement", InsertText: "export $0", InsertTextFormat: 2},
		{Label: "std", Kind: 14, Detail: "standard library import"},
		snippetCompletion("fn", "function", "fn ${1:name}(${2}) {\n    $0\n}"),
		snippetCompletion("let", "variable", "let ${1:name} = ${2:value}$0"),
		snippetCompletion("const", "constant", "const ${1:name} = ${2:value}$0"),
		{Label: "class", Kind: 7, Detail: "class", InsertText: "class ${1:Name} {\n    $0\n}", InsertTextFormat: 2},
		{Label: "enum", Kind: 7, Detail: "enum", InsertText: "enum ${1:Name} {\n    $0\n}", InsertTextFormat: 2},
		{Label: "interface", Kind: 7, Detail: "interface", InsertText: "interface ${1:Name} {\n    $0\n}", InsertTextFormat: 2},
		{Label: "embed", Kind: 14, Detail: "embed class methods"},
		snippetCompletion("field", "class field", "field ${1:name} = ${2:value}$0"),
		{Label: "private", Kind: 14, Detail: "private field"},
		{Label: "public", Kind: 14, Detail: "public field"},
		snippetCompletion("if", "if statement", "if ${1:condition} {\n    $0\n}"),
		{Label: "else", Kind: 14, Detail: "else"},
		snippetCompletion("while", "while loop", "while ${1:condition} {\n    $0\n}"),
		snippetCompletion("for", "for loop", "for let ${1:i} = 0; ${1:i} < ${2:count}; ${1:i}++ {\n    $0\n}"),
		snippetCompletion("for in", "for-in loop", "for ${1:item} in ${2:items} {\n    $0\n}"),
		snippetCompletion("match", "match expression", "match ${1:value} {\n    ${2:case} {\n        $0\n    }\n    _ {\n    }\n}"),
		snippetCompletion("return", "return", "return ${1:value}$0"),
		{Label: "break", Kind: 14, Detail: "break"},
		{Label: "continue", Kind: 14, Detail: "continue"},
		snippetCompletion("try", "try statement", "try {\n    $0\n} catch ${1:err} {\n    \n}"),
		{Label: "catch", Kind: 14, Detail: "catch block"},
		{Label: "finally", Kind: 14, Detail: "finally block"},
		snippetCompletion("throw", "throw error", "throw ${1:error}$0"),
		snippetCompletion("spawn", "spawn task", "spawn () fn() {\n    $0\n}"),
		snippetCompletion("defer", "defer statement", "defer fn() {\n    $0\n}"),
		snippetCompletion("embedstr", "embedstr statement", "embedstr \"$0\" const $1"),
		snippetCompletion("embedbin", "embedbin statement", "embedbin \"$0\" const $1"),
		snippetCompletion("embeddir", "embeddir statement", "embeddir \"$0\" const $1"),
		snippetCompletion("native fn", "native fn statement", "native fn ${0:Name}(): null {\n\tgo {\n$1\n\t}\n}"),
		{Label: "await ", Kind: 14, Detail: "await statement"},
		{Label: "lock ", Kind: 14, Detail: "lock statement"},
		{Label: "typeof", Kind: 14, Detail: "type operator"},
		{Label: "instanceof", Kind: 14, Detail: "instance check"},
		{Label: "true", Kind: 14, Detail: "boolean literal"},
		{Label: "false", Kind: 14, Detail: "boolean literal"},
		{Label: "null", Kind: 14, Detail: "null literal"},
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
				InsertText:       completionInsertText(sym, hasParens),
				InsertTextFormat: completionInsertTextFormat(sym, hasParens),
			})

			if sym.Kind == SymbolNamespace {
				for _, memberSym := range sym.Members {
					mName := memberSym.Name
					if seen[mName] {
						continue
					}
					items = append(items, CompletionItem{
						Label:            mName,
						Kind:             symbolKindToCompletionKind(memberSym.Kind),
						Detail:           sym.Name + "." + mName + " (from " + sym.Name + ")",
						InsertText:       sym.Name + "." + completionInsertText(memberSym, hasParens),
						InsertTextFormat: completionInsertTextFormat(memberSym, hasParens),
					})
				}
			}
		}
	}

	items = append(items, stdAutoImportCompletions(scope, text)...)
	items = append(items, fileAutoImportCompletions(scope, uri, text, hasParens)...)

	return rankedCompletionItems(dedupeCompletionItems(items))
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

func fileAutoImportCompletions(scope *Scope, uri string, text string, hasParens bool) []CompletionItem {
	currentPath := URIToPath(uri)
	if currentPath == "" {
		return nil
	}

	projectFiles := scanProjectTinyFiles(currentPath)
	if len(projectFiles) == 0 {
		root := filepath.Dir(currentPath)
		entries, err := os.ReadDir(root)
		if err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && filepath.Ext(entry.Name()) == ".tiny" {
					projectFiles = append(projectFiles, filepath.Join(root, entry.Name()))
				}
			}
		}
	}

	items := []CompletionItem{}
	for _, path := range projectFiles {
		if filepath.Clean(path) == filepath.Clean(currentPath) {
			continue
		}

		rel, err := filepath.Rel(filepath.Dir(currentPath), path)
		if err != nil {
			continue
		}
		relImport := filepath.ToSlash(rel)
		if filepath.Dir(rel) != "." && !strings.HasPrefix(relImport, ".") && !strings.HasPrefix(relImport, "/") {
			relImport = "./" + relImport
		}

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
			if sym.Kind == SymbolClass || sym.Kind == SymbolFunction || sym.Kind == SymbolVariable {
				insert = alias + "." + callableInsertText(sym.Name, hasParens)
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

func rankedCompletionItems(items []CompletionItem) []CompletionItem {
	for i := range items {
		if items[i].SortText != "" {
			continue
		}
		items[i].SortText = completionRank(items[i]) + items[i].Label
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SortText < items[j].SortText
	})
	return items
}

func completionRank(item CompletionItem) string {
	if strings.Contains(item.Detail, "parameter") || strings.HasPrefix(item.Detail, "variable") {
		return "01_"
	}
	if item.Kind == 5 {
		return "02_"
	}
	if item.Kind == 3 || item.Kind == 2 {
		return "03_"
	}
	if item.Kind == 7 || item.Kind == 13 {
		return "04_"
	}
	if item.Kind == 9 {
		return "05_"
	}
	if item.Kind == 14 {
		return "08_"
	}
	return "06_"
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

func completionItemsForClass(classSym SymbolInfo, receiver string, hasParens bool) []CompletionItem {
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
			InsertText:       callableInsertText(method.Name, hasParens),
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

	// --- NEW: RESOLVE FROM NAMESPACE STATIC TYPE ---
	if strings.HasPrefix(typ, "namespace:") {
		nsName := strings.TrimPrefix(typ, "namespace:")
		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			if memberSym, ok := ns.Members[member]; ok {
				return memberSym, staticTypeOfSymbol(member, memberSym), true
			}
		}
		return SymbolInfo{}, "unknown", false
	}

	if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName)
		if !ok {
			return SymbolInfo{}, "unknown", false
		}
		if fieldSym, ok := ifaceSym.Fields[member]; ok {
			return fieldSym, fieldSym.Type, true
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
		cleanMember := cleanMemberName(member)

		if sym.Kind == SymbolNamespace {
			memberSym, exists := sym.Members[cleanMember]
			if !exists {
				return SymbolInfo{}, "unknown", false
			}
			sym = memberSym
			typ = staticTypeOfSymbol(qualified, memberSym)
			continue
		}

		if fieldSym, exists := sym.Fields[cleanMember]; exists {
			sym = fieldSym
			typ = fieldSym.Type
			continue
		}

		if methodSym, exists := sym.Methods[cleanMember]; exists {
			sym = methodSym
			typ = firstNonEmpty(methodSym.Returns, "function")
			continue
		}

		if nextSym, nextType, exists := resolveMemberFromStaticType(scope, typ, cleanMember); exists {
			sym = nextSym
			typ = nextType
			continue
		}

		if sym.Type == "object" && sym.Fields != nil {
			if fieldSym, exists := sym.Fields[cleanMember]; exists {
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

		returnType := "null"
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

func isNullableLSPType(typ string) bool {
	if isNullishLSPType(typ) {
		return true
	}
	for _, part := range splitUnionType(typ) {
		if isNullishLSPType(part) {
			return true
		}
	}
	return false
}

func findLastDotIndex(before string) (int, bool) {
	trimmed := strings.TrimRight(before, " \t")
	if strings.HasSuffix(trimmed, "?.") {
		return -1, false
	}
	if strings.HasSuffix(trimmed, ".") {
		return len(trimmed) - 1, true
	}
	return -1, false
}

func completionItemsForReceiver(scope *Scope, text string, pos Position, receiver string, hasParens bool) []CompletionItem {
	sym, typ, ok := resolveReceiverPath(scope, text, pos, receiver)
	if !ok {
		return []CompletionItem{}
	}

	var items []CompletionItem

	if strings.Contains(typ, "|") {
		items = getUnionTypeCompletions(scope, typ, receiver, hasParens)
	} else if sym.Kind == SymbolNamespace || sym.Kind == SymbolEnum {
		items = completionItemsFromMembers(sym.Members, hasParens)
	} else if strings.HasPrefix(typ, "std:") {
		module := strings.TrimPrefix(typ, "std:")
		items = getStdCompletions(module, hasParens)
	} else if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName)
		if !ok {
			ifaceSym = SymbolInfo{Kind: SymbolInterface}
		}

		items = []CompletionItem{}
		for _, field := range ifaceSym.Fields {
			items = append(items, CompletionItem{
				Label:  field.Name,
				Kind:   symbolKindToCompletionKind(field.Kind),
				Detail: "interface field " + field.Name + " : " + field.Type,
			})
		}
		items = dedupeCompletionItems(items)
	} else if strings.HasPrefix(typ, "class:") {
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
		items = completionItemsForClass(classSym, receiver, hasParens)
	} else if typ == "object" {
		items = []CompletionItem{}
		for _, field := range sym.Fields {
			items = append(items, CompletionItem{Label: field.Name, Kind: symbolKindToCompletionKind(field.Kind), Detail: field.Detail + " : " + field.Type})
		}
		items = append(items, getNativeTypeCompletions("object", hasParens)...)
		items = dedupeCompletionItems(items)
	} else {
		items = getNativeTypeCompletions(typ, hasParens)
	}

	// Post-process to add ?. if accessing a nullable type
	if isNullableLSPType(typ) {
		line := getLine(text, pos.Line)
		before := ""
		if pos.Character <= len(line) {
			before = line[:pos.Character]
		} else {
			before = line
		}
		if dotIndex, shouldReplaceDot := findLastDotIndex(before); shouldReplaceDot {
			edit := TextEdit{
				Range: LSPRange{
					Start: Position{Line: pos.Line, Character: dotIndex},
					End:   Position{Line: pos.Line, Character: dotIndex + 1},
				},
				NewText: "?.",
			}
			for i := range items {
				items[i].AdditionalTextEdits = append(items[i].AdditionalTextEdits, edit)
			}
		}
	}

	return items
}

func getUnionTypeCompletions(scope *Scope, typ string, receiver string, hasParens bool) []CompletionItem {
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
					Label:            method.Name,
					Kind:             2,
					Detail:           formatFunctionSignature(method.Name, method.Params, method.Returns),
					InsertText:       callableInsertText(method.Name, hasParens),
					InsertTextFormat: 2,
				})
			}

			continue
		}

		if strings.HasPrefix(part, "interface:") {
			ifaceName := strings.TrimPrefix(part, "interface:")
			ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName)
			if ok {
				for _, field := range ifaceSym.Fields {
					items = append(items, CompletionItem{
						Label:  field.Name,
						Kind:   symbolKindToCompletionKind(field.Kind),
						Detail: "interface field " + field.Name + " : " + field.Type,
					})
				}
			}
			continue
		}

		if strings.HasPrefix(part, "std:") {
			module := strings.TrimPrefix(part, "std:")
			items = append(items, getStdCompletions(module, hasParens)...)
			continue
		}

		if part == "object" {
			items = append(items, getNativeTypeCompletions("object", hasParens)...)
			continue
		}

		items = append(items, getNativeTypeCompletions(part, hasParens)...)
	}

	return dedupeCompletionItems(items)
}

func hasCallParensAfter(after string) bool {
	i := 0
	for i < len(after) && isIdentChar(after[i]) {
		i++
	}
	trimmed := strings.TrimSpace(after[i:])
	return len(trimmed) > 0 && trimmed[0] == '('
}

func getCompletions(uri string, text string, pos Position) []CompletionItem {
	line := getLine(text, pos.Line)

	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	if isInsideStdImportString(line, pos.Character) {
		return rankedCompletionItems(stdModuleNameCompletions())
	}

	if isInsideLibraryImportString(line, pos.Character) {
		return rankedCompletionItems(libraryImportPathCompletions(line, pos.Character))
	}

	if isInsidePluginImportString(line, pos.Character) {
		return rankedCompletionItems(pluginImportPathCompletions())
	}

	if isInsideFileImportString(line, pos.Character) {
		return rankedCompletionItems(fileImportPathCompletions(uri, line, pos.Character))
	}

	before := line[:pos.Character]
	// Strip trailing identifier characters to support autocomplete while typing a member name
	i := len(before) - 1
	for i >= 0 && isIdentChar(before[i]) {
		i--
	}
	beforeTrimmed := before[:i+1]
	receiver := receiverBeforeDot(beforeTrimmed)

	after := line[pos.Character:]
	hasParens := hasCallParensAfter(after)

	scope := scopeAtPosition(uri, text, pos)

	editorCtx := parseEditorContext(text, pos, scope)

	if editorCtx.InsideString {
		if editorCtx.InsideObject && editorCtx.ObjectInterfaceType != "" && editorCtx.IsObjectStringKey {
			return rankedCompletionItems(objectLiteralCompletionsWithContext(editorCtx))
		}
		return nil
	}

	if editorCtx.InsideObject && editorCtx.IsObjectKeyPosition {
		if editorCtx.ObjectInterfaceType != "" {
			return rankedCompletionItems(objectLiteralCompletionsWithContext(editorCtx))
		}
		return nil
	}

	if receiver == "" {
		return rankedCompletionItems(scopeCompletions(scope, uri, text, hasParens))
	}

	return rankedCompletionItems(completionItemsForReceiver(scope, text, pos, receiver, hasParens))
}

func completionItemsFromMembers(members map[string]SymbolInfo, hasParens bool) []CompletionItem {
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
			InsertText:       completionInsertText(member, hasParens),
			InsertTextFormat: completionInsertTextFormat(member, hasParens),
		})
	}

	return items
}

func callableInsertText(name string, hasParens bool) string {
	if hasParens {
		return name
	}
	return name + "($0)"
}

func completionInsertText(sym SymbolInfo, hasParens bool) string {
	switch sym.Kind {
	case SymbolFunction, SymbolClass:
		return callableInsertText(sym.Name, hasParens)
	default:
		return sym.Name
	}
}

func completionInsertTextFormat(sym SymbolInfo, hasParens bool) int {
	switch sym.Kind {
	case SymbolFunction, SymbolClass:
		return 2
	default:
		return 0
	}
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

func cleanMemberName(member string) string {
	if idx := strings.Index(member, "("); idx >= 0 {
		return member[:idx]
	}
	return member
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
	depthParen := 0

	for i >= 0 {
		ch := text[i]

		if depthParen > 0 {
			switch ch {
			case ')':
				depthParen++
			case '(':
				depthParen--
			}
			i--
			continue
		}

		if isIdentChar(ch) || ch == '.' || ch == '?' {
			i--
			continue
		}

		if ch == ')' {
			depthParen++
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
