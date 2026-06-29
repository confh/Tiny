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
	"sync"
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

type TextDocumentIdentifierParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
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

type DocumentHighlight struct {
	Range LSPRange `json:"range"`
	Kind  int      `json:"kind,omitempty"`
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

	"fn":          true,
	"let":         true,
	"const":       true,
	"class":       true,
	"embed":       true,
	"native":      true,
	"external":    true,
	"field":       true,
	"private":     true,
	"public":      true,
	"enum":        true,
	"iota":        true,
	"interface":   true,
	"embedtext":   true,
	"embedbytes":  true,
	"embedfolder": true,

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
	"implements": true,
}

var lspDocs = map[string]string{}

var semanticTokenTypes = []string{
	"namespace",
	"type",
	"class",
	"enum",
	"interface",
	"function",
	"method",
	"property",
	"variable",
	"parameter",
	"keyword",
	"string",
	"number",
	"operator",
}

var semanticTokenTypeIndex = func() map[string]int {
	result := map[string]int{}
	for i, name := range semanticTokenTypes {
		result[name] = i
	}
	return result
}()

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
		if isIdentChar(ch) || ch == '.' || ch == '?' || ch == ':' {
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

		func() {
			defer func() {
				if r := recover(); r != nil {
					fmt.Fprintf(os.Stderr, "[tiny-lsp] recovered from request panic: %v\n", r)
				}
			}()
			handleLSPMessage(msg)
		}()
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

var lspWriteMu sync.Mutex

func writeLSPMessage(msg LSPMessage) {
	lspWriteMu.Lock()
	defer lspWriteMu.Unlock()

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
			if !ok || isPrivateImportMember(member) {
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
	posOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	if isOffsetInStringOrComment(text, posOffset) {
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
		targetPath = resolveLibraryImportPath(pathText, uri)
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
			matchOffset := offsetAtLine(docText, matchPos.Line+1) + matchPos.Character
			if isOffsetInStringOrComment(docText, matchOffset) || isObjectLiteralKeyReferencePosition(docText, matchPos, name) {
				continue
			}
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

func getDocumentHighlights(uri string, text string, pos Position) []DocumentHighlight {
	locations := getReferences(uri, text, pos, true)
	highlights := []DocumentHighlight{}

	for _, loc := range locations {
		if loc.URI != uri {
			continue
		}

		highlights = append(highlights, DocumentHighlight{
			Range: loc.Range,
			Kind:  1,
		})
	}

	return highlights
}

func symbolAtPositionForReferences(text string, pos Position, scope *Scope) (SymbolInfo, bool) {
	word := wordAtPosition(text, pos)
	if word == "" || isObjectLiteralKeyReferencePosition(text, pos, word) {
		return SymbolInfo{}, false
	}
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
	if className := classNameAtPosition(text, pos); className != "" {
		if classSym, ok := resolveClassSymbol(scope, className); ok {
			line := pos.Line + 1
			if methodSym, ok := classSym.Methods[word]; ok && methodSym.Line == line {
				return methodSym, true
			}
			if fieldSym, ok := classSym.Fields[word]; ok && fieldSym.Line == line {
				return fieldSym, true
			}
		}
	}
	return scope.Resolve(word)
}

func isObjectLiteralKeyReferencePosition(text string, pos Position, name string) bool {
	line := getLine(text, pos.Line)
	if pos.Character < 0 || pos.Character+len(name) > len(line) {
		return false
	}
	before := strings.TrimSpace(line[:pos.Character])
	after := strings.TrimSpace(line[pos.Character+len(name):])
	if !strings.HasPrefix(after, ":") {
		return false
	}
	if strings.HasPrefix(before, "let ") ||
		strings.HasPrefix(before, "const ") ||
		strings.HasPrefix(before, "field ") ||
		strings.HasPrefix(before, "fn ") ||
		strings.HasPrefix(before, "import ") ||
		strings.Contains(before, "(") {
		return false
	}
	return before == "" || strings.HasSuffix(before, "{") || strings.HasSuffix(before, ",")
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
	hints = append(hints, callbackParameterTypeInlayHintsForText(uri, text, rng)...)

	sort.SliceStable(hints, func(i, j int) bool {
		if hints[i].Position.Line != hints[j].Position.Line {
			return hints[i].Position.Line < hints[j].Position.Line
		}
		return hints[i].Position.Character < hints[j].Position.Character
	})
	return hints
}

func callbackParameterTypeInlayHintsForText(uri string, text string, rng LSPRange) []InlayHint {
	hints := []InlayHint{}
	lines := strings.Split(text, "\n")

	for lineIndex, line := range lines {
		if lineIndex < rng.Start.Line || lineIndex > rng.End.Line {
			continue
		}

		matches := inlineAnonFnRegex.FindAllStringSubmatchIndex(line, -1)
		for _, match := range matches {
			lineStartOffset := offsetAtLine(text, lineIndex+1)
			fnOffset := lineStartOffset + match[0]
			paramsStart := match[2]
			paramsEnd := match[3]
			paramsText := line[paramsStart:paramsEnd]

			scope := scopeAtPosition(uri, text, bytePositionAtOffset(text, fnOffset))
			inferredTypes := expectedInlineFunctionParamTypes(scope, text, bytePositionAtOffset(text, fnOffset), fnOffset)
			if len(inferredTypes) == 0 {
				inferredTypes = expectedInlineFunctionParamTypesFromObject(scope, text, fnOffset)
			}
			if len(inferredTypes) == 0 {
				continue
			}

			parts, partOffsets := topLevelPartsWithOffsets(paramsText, ',')
			for i, part := range parts {
				if i >= len(partOffsets) || i >= len(inferredTypes) {
					break
				}

				name, _, nameEnd, hasExplicitType := callbackParamNameBounds(part)
				if name == "" || hasExplicitType {
					continue
				}

				typ := normalizeLSPType(scope, inferredTypes[i])
				if typ == "" || typ == "any" || typ == "unknown" {
					continue
				}

				pos := Position{Line: lineIndex, Character: paramsStart + partOffsets[i] + nameEnd}
				if !positionInRange(pos, rng) {
					continue
				}

				hints = append(hints, InlayHint{
					Position:    pos,
					Label:       ": " + inlayTypeLabel(typ),
					Kind:        1,
					PaddingLeft: false,
				})
				_ = name
			}
		}
	}

	return hints
}

func topLevelPartsWithOffsets(text string, delimiter byte) ([]string, []int) {
	parts := splitTopLevel(text, delimiter)
	offsets := make([]int, 0, len(parts))
	offset := 0
	for _, part := range parts {
		offsets = append(offsets, offset)
		offset += len(part) + 1
	}
	return parts, offsets
}

func callbackParamNameBounds(part string) (string, int, int, bool) {
	trimmedLeft := len(part) - len(strings.TrimLeft(part, " \t\r\n"))
	i := trimmedLeft
	if strings.HasPrefix(part[i:], "...") {
		i += 3
		for i < len(part) && (part[i] == ' ' || part[i] == '\t') {
			i++
		}
	}

	start := i
	for i < len(part) && isIdentChar(part[i]) {
		i++
	}
	if i == start {
		return "", 0, 0, false
	}

	j := i
	for j < len(part) && (part[j] == ' ' || part[j] == '\t') {
		j++
	}
	if j < len(part) && part[j] == '?' {
		j++
		for j < len(part) && (part[j] == ' ' || part[j] == '\t') {
			j++
		}
	}

	return part[start:i], start, i, j < len(part) && part[j] == ':'
}

func variableTypeInlayHintsForText(uri string, text string, rng LSPRange) []InlayHint {
	lines := strings.Split(text, "\n")
	hints := []InlayHint{}

	for lineIndex, line := range lines {
		if !positionInRange(Position{Line: lineIndex, Character: 0}, LSPRange{
			Start: Position{Line: rng.Start.Line, Character: 0},
			End:   Position{Line: rng.End.Line, Character: len(line)},
		}) {
			continue
		}

		decl, ok := lexerVariableDeclarationOnLine(text, lineIndex)
		if !ok || decl.HasTypeHint {
			continue
		}

		exprEnd := variableInitializerEnd(text, decl.ExprStart)
		if exprEnd < decl.ExprStart {
			continue
		}

		scope := scopeAtPosition(uri, text, Position{Line: lineIndex, Character: len(line)})
		expr := strings.TrimSpace(text[decl.ExprStart:exprEnd])
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

		hints = append(hints, InlayHint{
			Position:    Position{Line: lineIndex, Character: decl.NameEnd},
			Label:       ": " + inlayTypeLabel(typ),
			Kind:        1,
			PaddingLeft: false,
		})
	}

	return hints
}

func inlayTypeLabel(typ string) string {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return typ
	}
	if strings.Contains(typ, "|") {
		parts := splitUnionType(typ)
		for i, part := range parts {
			parts[i] = inlayTypeLabel(part)
		}
		return strings.Join(parts, " | ")
	}
	if strings.HasPrefix(typ, "array:") {
		return "array:" + inlayTypeLabel(strings.TrimPrefix(typ, "array:"))
	}
	if strings.HasPrefix(typ, "task:") {
		return "task:" + inlayTypeLabel(strings.TrimPrefix(typ, "task:"))
	}
	if strings.HasPrefix(typ, "function(") {
		params, ok := callableFunctionParamTypes(typ)
		if !ok {
			return typ
		}
		for i, param := range params {
			params[i] = inlayTypeLabel(param)
		}
		return "function(" + strings.Join(params, ", ") + ")"
	}

	parts := strings.Split(typ, ":")
	newParts := []string{}
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "class" || part == "interface" || part == "enum" || part == "namespace" {
			continue
		}
		if idx := strings.LastIndex(part, "."); idx >= 0 {
			part = part[idx+1:]
		}
		if part != "" {
			newParts = append(newParts, part)
		}
	}
	return strings.Join(newParts, ":")
}

func parameterInlayHintsForText(uri string, text string, rng LSPRange) []InlayHint {
	hints := []InlayHint{}
	for _, call := range lexerCallSites(text) {
		name := call.Name
		if tinyKeywords[name] {
			continue
		}

		open := call.OpenOffset
		close := findMatching(text, open, '(', ')')
		if close < 0 {
			continue
		}

		callPos := bytePositionAtOffset(text, call.NameOffset)

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

type lexerVariableDeclaration struct {
	Name        string
	NameEnd     int
	ExprStart   int
	HasTypeHint bool
	TypeHint    string
}

func lexerVariableDeclarationOnLine(text string, lineIndex int) (lexerVariableDeclaration, bool) {
	defer func() {
		_ = recover()
	}()
	line := getLine(text, lineIndex)
	lineOffset := offsetAtLine(text, lineIndex+1)
	lexer := NewLexer(line, "")
	lexer.EnableASI = false

	tok := lexer.NextToken()
	if tok.Type == TOKEN_EXPORT {
		tok = lexer.NextToken()
	}
	if tok.Type != TOKEN_LET && tok.Type != TOKEN_CONST {
		return lexerVariableDeclaration{}, false
	}

	nameTok := lexer.NextToken()
	if nameTok.Type != TOKEN_IDENT {
		return lexerVariableDeclaration{}, false
	}

	if isOffsetInStringOrComment(text, lineOffset+nameTok.Column-1) {
		return lexerVariableDeclaration{}, false
	}

	hasTypeHint := false
	typeStart := -1
	typeEnd := -1
	for {
		tok = lexer.NextToken()
		if tok.Type == TOKEN_EOF || tok.Type == TOKEN_SEMI {
			return lexerVariableDeclaration{}, false
		}
		if tok.Type == TOKEN_COLON {
			if !hasTypeHint {
				hasTypeHint = true
				typeStart = lineOffset + tok.Column
			}
			continue
		}
		if tok.Type == TOKEN_ASSIGN {
			if hasTypeHint {
				typeEnd = lineOffset + tok.Column - 1
			}
			exprStart := lineOffset + tok.Column
			for exprStart < len(text) && (text[exprStart] == ' ' || text[exprStart] == '\t') {
				exprStart++
			}
			typeHint := ""
			if typeStart >= 0 && typeEnd >= typeStart && typeEnd <= len(text) {
				typeHint = strings.TrimSpace(text[typeStart:typeEnd])
			}
			return lexerVariableDeclaration{
				Name:        nameTok.Literal,
				NameEnd:     nameTok.Column - 1 + len(nameTok.Literal),
				ExprStart:   exprStart,
				HasTypeHint: hasTypeHint,
				TypeHint:    typeHint,
			}, true
		}
	}
}

type lexerCallSite struct {
	Name       string
	NameOffset int
	OpenOffset int
}

type lspLexedToken struct {
	Token  Token
	Offset int
}

func lexedTokensForText(text string) []lspLexedToken {
	defer func() {
		_ = recover()
	}()
	lexer := NewLexer(text, "")
	lexer.EnableASI = false
	tokens := []lspLexedToken{}
	for {
		tok := lexer.NextToken()
		offset := offsetFromLineCol(text, tok.Line, tok.Column)
		tokens = append(tokens, lspLexedToken{Token: tok, Offset: offset})
		if tok.Type == TOKEN_EOF {
			break
		}
	}
	return tokens
}

func lexerCallSites(text string) []lexerCallSite {
	defer func() {
		_ = recover()
	}()
	tokens := lexedTokensForText(text)
	calls := []lexerCallSite{}
	for i := 0; i < len(tokens); i++ {
		tok := tokens[i].Token
		if tok.Type != TOKEN_IDENT {
			continue
		}
		if i > 0 && tokens[i-1].Token.Type == TOKEN_DOT {
			continue
		}
		if i > 0 && tokens[i-1].Token.Type == TOKEN_FN {
			continue
		}

		parts := []string{tok.Literal}
		nameOffset := tokens[i].Offset
		j := i + 1
		for j+1 < len(tokens) && tokens[j].Token.Type == TOKEN_DOT && tokens[j+1].Token.Type == TOKEN_IDENT {
			parts = append(parts, tokens[j+1].Token.Literal)
			j += 2
		}
		if j >= len(tokens) || tokens[j].Token.Type != TOKEN_LPAREN {
			continue
		}
		calls = append(calls, lexerCallSite{
			Name:       strings.Join(parts, "."),
			NameOffset: nameOffset,
			OpenOffset: tokens[j].Offset,
		})
	}
	return calls
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
		if strings.HasPrefix(receiverType, "enum:") {
			if params, ok := enumVariantParamsFromText(text, strings.TrimPrefix(receiverType, "enum:"), member); ok {
				return params
			}
		}
		if sym.Kind == SymbolNamespace {
			if memberSym, ok := sym.Members[member]; ok {
				if isPrivateImportMember(memberSym) {
					return nil
				}
				if memberSym.Kind == SymbolClass {
					return constructorSymbolFromClass(memberSym, memberSym.Name).Params
				}
				if memberSym.Kind == SymbolEnum {
					if params, ok := enumVariantParamsFromText(text, memberSym.Name, member); ok {
						return params
					}
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

func enumVariantParamsFromText(text string, enumName string, variantName string) ([]StdArg, bool) {
	for _, block := range findBlocks(text, "enum") {
		if block.Name != enumName {
			continue
		}

		for _, raw := range splitTopLevel(block.Body, ',') {
			member := strings.TrimSpace(raw)
			if member == "" {
				continue
			}

			if strings.Contains(member, "=") {
				member = strings.TrimSpace(strings.SplitN(member, "=", 2)[0])
			}

			if !strings.HasPrefix(member, variantName+"(") {
				continue
			}

			op := strings.Index(member, "(")
			close := strings.LastIndex(member, ")")
			if op < 0 || close < op {
				continue
			}

			paramsText := strings.TrimSpace(member[op+1 : close])
			if paramsText == "" {
				return []StdArg{}, true
			}

			params := []StdArg{}
			for _, rawArg := range splitTopLevel(paramsText, ',') {
				arg := strings.TrimSpace(rawArg)
				if arg == "" {
					continue
				}
				if idx := strings.Index(arg, ":"); idx >= 0 {
					params = append(params, StdArg{Name: strings.TrimSpace(arg[:idx]), Type: strings.TrimSpace(arg[idx+1:])})
					continue
				}
				params = append(params, StdArg{Name: arg, Type: "any"})
			}
			return params, true
		}
	}

	return nil, false
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
	if action, ok := organizeImportsAction(uri, text); ok {
		actions = append(actions, action)
	}
	if action, ok := createMissingFunctionAction(uri, text, params.Range.Start, params.Context.Diagnostics); ok {
		actions = append(actions, action)
	}
	if action, ok := addImportForSymbolAction(uri, text, params.Range.Start); ok {
		actions = append(actions, action)
	}
	if action, ok := installMissingLibraryAction(uri, line); ok {
		actions = append(actions, action)
	}
	if action, ok := implementMissingMethodsAction(uri, text, params.Range.Start); ok {
		actions = append(actions, action)
	}
	if action, ok := ifElseToMatchAction(uri, text, params.Range.Start); ok {
		actions = append(actions, action)
	}

	return actions
}

func inferredTypeHintAction(uri string, text string, line string, lineIndex int) (CodeAction, bool) {
	decl, ok := lexerVariableDeclarationOnLine(text, lineIndex)
	if !ok || decl.HasTypeHint {
		return CodeAction{}, false
	}
	scope := scopeAtPosition(uri, text, Position{Line: lineIndex, Character: len(line)})
	exprEnd := variableInitializerEnd(text, decl.ExprStart)
	if exprEnd < decl.ExprStart {
		return CodeAction{}, false
	}
	typ := inferExprTypeFromText(scope, strings.TrimSpace(text[decl.ExprStart:exprEnd]))
	typ = normalizeLSPType(scope, typ)
	if typ == "" || typ == "any" || typ == "unknown" {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Add inferred type hint",
		Kind:  "quickfix",
		Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
			Range:   lspRangeFromByteColumns(text, lineIndex, decl.NameEnd, decl.NameEnd),
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

type lspImportLine struct {
	Line  int
	Text  string
	Kind  string
	Path  string
	Alias string
	Key   string
}

func organizeImportsAction(uri string, text string) (CodeAction, bool) {
	edit, ok := organizeImportsEdit(uri, text)
	if !ok {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Organize imports",
		Kind:  "source.organizeImports",
		Edit:  WorkspaceEdit{Changes: map[string][]TextEdit{uri: {edit}}},
	}, true
}

func organizeImportsEdit(uri string, text string) (TextEdit, bool) {
	lines := strings.Split(stripNativeGoBlocks(text), "\n")
	imports := topImportBlock(lines)
	if len(imports) == 0 {
		return TextEdit{}, false
	}

	usedAliases := usedImportAliases(text, imports)
	seen := map[string]bool{}
	kept := []string{}
	for _, imp := range imports {
		if seen[imp.Key] {
			continue
		}
		seen[imp.Key] = true
		if !usedAliases[imp.Alias] {
			continue
		}
		kept = append(kept, strings.TrimSpace(imp.Text))
	}
	sort.Strings(kept)

	firstLine := imports[0].Line
	lastLine := imports[len(imports)-1].Line
	newText := ""
	if len(kept) > 0 {
		newText = strings.Join(kept, "\n") + "\n"
	}

	oldText := strings.Join(lines[firstLine:lastLine+1], "\n")
	if len(kept) > 0 {
		oldText += "\n"
	}
	if oldText == newText {
		return TextEdit{}, false
	}

	return TextEdit{
		Range: LSPRange{
			Start: Position{Line: firstLine, Character: 0},
			End:   Position{Line: lastLine + 1, Character: 0},
		},
		NewText: newText,
	}, true
}

const minIfElseToMatchArms = 5

type ifElseMatchCase struct {
	Pattern string
	Body    string
}

type ifElseMatchCandidate struct {
	Subject string
	Cases   []ifElseMatchCase
	Default string
	Start   int
	End     int
}

func ifElseToMatchAction(uri string, text string, pos Position) (CodeAction, bool) {
	candidate, ok := findIfElseMatchCandidateAt(text, pos)
	if !ok {
		return CodeAction{}, false
	}
	newText := renderMatchCandidate(text, candidate)
	if strings.TrimSpace(newText) == "" {
		return CodeAction{}, false
	}
	return CodeAction{
		Title: "Convert if/else chain to match",
		Kind:  "refactor.rewrite",
		Edit: WorkspaceEdit{Changes: map[string][]TextEdit{uri: {{
			Range:   lspRangeFromOffsets(text, candidate.Start, candidate.End),
			NewText: newText,
		}}}},
	}, true
}

func findIfElseMatchCandidateAt(text string, pos Position) (ifElseMatchCandidate, bool) {
	defer func() {
		_ = recover()
	}()
	cursor := offsetAtLine(text, pos.Line+1) + pos.Character
	lexer := NewLexer(text, "")
	lexer.EnableASI = false
	for {
		tok := lexer.NextToken()
		if tok.Type == TOKEN_EOF {
			break
		}
		if tok.Type != TOKEN_IF {
			continue
		}
		start := offsetFromLineCol(text, tok.Line, tok.Column)
		candidate, ok := parseIfElseMatchCandidate(text, start)
		if !ok {
			continue
		}
		if cursor >= candidate.Start && cursor <= candidate.End {
			return candidate, true
		}
	}
	return ifElseMatchCandidate{}, false
}

func parseIfElseMatchCandidate(text string, start int) (ifElseMatchCandidate, bool) {
	i := start
	subject := ""
	cases := []ifElseMatchCase{}
	defaultBody := ""

	for {
		if !keywordAt(text, i, "if") {
			return ifElseMatchCandidate{}, false
		}
		conditionStart := skipSpaces(text, i+len("if"))
		openBrace := findTopLevelByte(text, conditionStart, '{')
		if openBrace < 0 {
			return ifElseMatchCandidate{}, false
		}
		condition := strings.TrimSpace(text[conditionStart:openBrace])
		caseSubject, pattern, ok := splitMatchableEquality(condition)
		if !ok {
			return ifElseMatchCandidate{}, false
		}
		if subject == "" {
			subject = caseSubject
		} else if caseSubject != subject {
			return ifElseMatchCandidate{}, false
		}

		closeBrace := findMatching(text, openBrace, '{', '}')
		if closeBrace < 0 {
			return ifElseMatchCandidate{}, false
		}
		cases = append(cases, ifElseMatchCase{
			Pattern: pattern,
			Body:    text[openBrace+1 : closeBrace],
		})

		i = skipSpacesAndSemis(text, closeBrace+1)
		if !keywordAt(text, i, "else") {
			return makeIfElseMatchCandidate(subject, cases, defaultBody, start, closeBrace+1)
		}
		i = skipSpaces(text, i+len("else"))
		if keywordAt(text, i, "if") {
			continue
		}
		if i >= len(text) || text[i] != '{' {
			return ifElseMatchCandidate{}, false
		}
		defaultClose := findMatching(text, i, '{', '}')
		if defaultClose < 0 {
			return ifElseMatchCandidate{}, false
		}
		defaultBody = text[i+1 : defaultClose]
		return makeIfElseMatchCandidate(subject, cases, defaultBody, start, defaultClose+1)
	}
}

func makeIfElseMatchCandidate(subject string, cases []ifElseMatchCase, defaultBody string, start int, end int) (ifElseMatchCandidate, bool) {
	armCount := len(cases)
	if strings.TrimSpace(defaultBody) != "" {
		armCount++
	}
	if subject == "" || len(cases) == 0 || armCount < minIfElseToMatchArms {
		return ifElseMatchCandidate{}, false
	}
	return ifElseMatchCandidate{Subject: subject, Cases: cases, Default: defaultBody, Start: start, End: end}, true
}

func splitMatchableEquality(condition string) (string, string, bool) {
	idx := findTopLevelOperator(condition, "==")
	if idx < 0 {
		return "", "", false
	}
	left := strings.TrimSpace(condition[:idx])
	right := strings.TrimSpace(condition[idx+2:])
	if left == "" || right == "" || strings.Contains(condition, "!=") {
		return "", "", false
	}
	if isMatchSubjectExpr(left) && !isMatchSubjectExpr(right) {
		return left, right, true
	}
	if isMatchSubjectExpr(right) && !isMatchSubjectExpr(left) {
		return right, left, true
	}
	return "", "", false
}

func isMatchSubjectExpr(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}
	if expr[0] == '"' || expr[0] == '\'' || expr[0] == '`' {
		return false
	}
	if expr[0] >= '0' && expr[0] <= '9' {
		return false
	}
	if expr == "true" || expr == "false" || expr == "null" {
		return false
	}
	return true
}

func findTopLevelOperator(text string, op string) int {
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inString := byte(0)
	escaped := false
	for i := 0; i < len(text); i++ {
		ch := text[i]
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
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}
		switch ch {
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		}
		if depthParen == 0 && depthBrace == 0 && depthBracket == 0 && strings.HasPrefix(text[i:], op) {
			return i
		}
	}
	return -1
}

func findTopLevelByte(text string, start int, target byte) int {
	depthParen, depthBrace, depthBracket := 0, 0, 0
	inString := byte(0)
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
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
		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}
		if depthParen == 0 && depthBrace == 0 && depthBracket == 0 && ch == target {
			return i
		}
		switch ch {
		case '(':
			depthParen++
		case ')':
			if depthParen > 0 {
				depthParen--
			}
		case '{':
			depthBrace++
		case '}':
			if depthBrace > 0 {
				depthBrace--
			}
		case '[':
			depthBracket++
		case ']':
			if depthBracket > 0 {
				depthBracket--
			}
		}
	}
	return -1
}

func skipSpacesAndSemis(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n' || text[i] == ';') {
		i++
	}
	return i
}

func keywordAt(text string, offset int, keyword string) bool {
	if offset < 0 || offset+len(keyword) > len(text) || text[offset:offset+len(keyword)] != keyword {
		return false
	}
	return isSpaceAroundKeyword(text, offset, keyword)
}

func renderMatchCandidate(text string, candidate ifElseMatchCandidate) string {
	baseIndent := lineIndentAtOffset(text, candidate.Start)
	indentUnit := indentUnitForText(text)
	bodyIndent := baseIndent + indentUnit
	out := strings.Builder{}
	out.WriteString(baseIndent)
	out.WriteString("match ")
	out.WriteString(candidate.Subject)
	out.WriteString(" {\n")
	for _, c := range candidate.Cases {
		out.WriteString(bodyIndent)
		out.WriteString(c.Pattern)
		out.WriteString(" {")
		out.WriteString(renderMatchBody(c.Body, bodyIndent))
		out.WriteString("\n")
		out.WriteString(bodyIndent)
		out.WriteString("}\n")
	}
	if strings.TrimSpace(candidate.Default) != "" {
		out.WriteString(bodyIndent)
		out.WriteString("_ {")
		out.WriteString(renderMatchBody(candidate.Default, bodyIndent))
		out.WriteString("\n")
		out.WriteString(bodyIndent)
		out.WriteString("}\n")
	}
	out.WriteString(baseIndent)
	out.WriteString("}")
	return out.String()
}

func indentUnitForText(text string) string {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "\t") {
			return "\t"
		}
		if strings.HasPrefix(line, "    ") {
			return "    "
		}
	}
	return "\t"
}

func renderMatchBody(body string, armIndent string) string {
	trimmed := strings.Trim(body, " \t\r\n")
	if strings.TrimSpace(trimmed) == "" {
		return "\n"
	}
	indentUnit := indentUnitForText(body)
	lines := strings.Split(trimmed, "\n")
	minIndent := minNonBlankIndent(lines)
	out := strings.Builder{}
	for _, line := range lines {
		out.WriteString("\n")
		out.WriteString(armIndent)
		out.WriteString(indentUnit)
		if len(line) >= minIndent {
			out.WriteString(line[minIndent:])
		} else {
			out.WriteString(strings.TrimLeft(line, " \t"))
		}
	}
	return out.String()
}

func minNonBlankIndent(lines []string) int {
	minIndent := -1
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		indent := len(line) - len(strings.TrimLeft(line, " \t"))
		if minIndent == -1 || indent < minIndent {
			minIndent = indent
		}
	}
	if minIndent < 0 {
		return 0
	}
	return minIndent
}

func lineIndentAtOffset(text string, offset int) string {
	lineStart := offset
	for lineStart > 0 && text[lineStart-1] != '\n' {
		lineStart--
	}
	i := lineStart
	for i < len(text) && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[lineStart:i]
}

func lspRangeFromOffsets(text string, start int, end int) LSPRange {
	return LSPRange{
		Start: bytePositionAtOffset(text, start),
		End:   bytePositionAtOffset(text, end),
	}
}

func topImportBlock(lines []string) []lspImportLine {
	imports := []lspImportLine{}
	inBlock := false
	for i, raw := range lines {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			if inBlock {
				continue
			}
			continue
		}
		if !strings.HasPrefix(trimmed, "import ") {
			if inBlock {
				break
			}
			continue
		}
		imp, ok := parseLSPImportLine(raw, i)
		if !ok {
			if inBlock {
				break
			}
			continue
		}
		inBlock = true
		imports = append(imports, imp)
	}
	return imports
}

func parseLSPImportLine(line string, lineIndex int) (lspImportLine, bool) {
	defer func() {
		_ = recover()
	}()
	lexer := NewLexer(line, "")
	lexer.EnableASI = false
	tok := lexer.NextToken()
	if tok.Type != TOKEN_IMPORT {
		return lspImportLine{}, false
	}

	kind := "file"
	tok = lexer.NextToken()
	if tok.Type == TOKEN_IDENT {
		switch tok.Literal {
		case "std", "plugin", "library", "lib":
			kind = tok.Literal
			tok = lexer.NextToken()
		}
	}
	if tok.Type != TOKEN_STRING {
		return lspImportLine{}, false
	}
	path := tok.Literal
	alias := ""
	tok = lexer.NextToken()
	if tok.Type == TOKEN_IDENT && tok.Literal == "as" {
		aliasTok := lexer.NextToken()
		if aliasTok.Type != TOKEN_IDENT {
			return lspImportLine{}, false
		}
		alias = aliasTok.Literal
		tok = lexer.NextToken()
	}
	if tok.Type == TOKEN_SEMI {
		tok = lexer.NextToken()
	}
	if tok.Type != TOKEN_EOF {
		return lspImportLine{}, false
	}
	if alias == "" {
		switch kind {
		case "std":
			alias = path
		case "library", "lib":
			alias = defaultLibraryAlias(path)
		default:
			alias = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		}
	}

	key := kind + ":" + filepath.ToSlash(path) + ":" + alias
	return lspImportLine{
		Line:  lineIndex,
		Text:  line,
		Kind:  kind,
		Path:  path,
		Alias: alias,
		Key:   key,
	}, true
}

func usedImportAliases(text string, imports []lspImportLine) map[string]bool {
	importLines := map[int]bool{}
	for _, imp := range imports {
		importLines[imp.Line] = true
	}

	used := map[string]bool{}
	for _, imp := range imports {
		for _, r := range identifierRangesInText(text, imp.Alias) {
			if !importLines[r.Line] {
				used[imp.Alias] = true
				break
			}
		}
	}
	return used
}

func createMissingFunctionAction(uri string, text string, pos Position, diagnostics []map[string]any) (CodeAction, bool) {
	name := wordAtPosition(text, pos)
	if name == "" || tinyKeywords[name] {
		return CodeAction{}, false
	}
	if len(diagnostics) > 0 && !diagnosticsContainMessage(diagnostics, "undefined variable: "+name) {
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

func diagnosticsContainMessage(diagnostics []map[string]any, message string) bool {
	for _, diagnostic := range diagnostics {
		if diagnosticMessage, _ := diagnostic["message"].(string); diagnosticMessage == message {
			return true
		}
	}
	return false
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

func findClassStmtInAST(statements []Stmt, name string) (ClassStmt, bool) {
	for _, raw := range statements {
		stmt, _ := unwrapExport(raw)
		if cls, ok := stmt.(ClassStmt); ok && cls.Name == name {
			return cls, true
		}
		if ns, ok := stmt.(NamespaceStmt); ok {
			if nested, ok := findClassStmtInAST(ns.Statements, name); ok {
				return nested, true
			}
		}
	}
	return ClassStmt{}, false
}

func implementMissingMethodsAction(uri string, text string, pos Position) (CodeAction, bool) {
	offset := offsetAtLine(text, pos.Line+1)

	var targetBlock *blockInfo
	for _, block := range findBlocks(text, "class") {
		if offset >= block.Start && offset <= block.End {
			targetBlock = &block
			break
		}
	}
	if targetBlock == nil {
		return CodeAction{}, false
	}

	statements, _ := parseTinyForLSP(URIToPath(uri), text)
	if statements == nil {
		return CodeAction{}, false
	}

	cls, found := findClassStmtInAST(statements, targetBlock.Name)
	if !found {
		return CodeAction{}, false
	}
	if len(cls.Embeds) == 0 {
		return CodeAction{}, false
	}

	scope := scopeAtPosition(uri, text, pos)

	localFields := map[string]bool{}
	for _, f := range cls.Fields {
		localFields[f.Name] = true
	}
	localMethods := map[string]bool{}
	for _, m := range cls.Methods {
		localMethods[m.Name] = true
	}

	newText := ""
	for _, embedName := range cls.Embeds {
		var embedSym SymbolInfo
		var ok bool
		if embedSym, ok = resolveInterfaceSymbol(scope, embedName); !ok {
			if embedSym, ok = resolveClassSymbol(scope, embedName); !ok {
				continue
			}
		}

		for fName, fSym := range embedSym.Fields {
			if fSym.Type == "function" {
				if !localMethods[fName] {
					newText += fmt.Sprintf("    fn %s() {\n    }\n\n", fName)
					localMethods[fName] = true
				}
			} else {
				if !localFields[fName] {
					defaultVal := "null"
					switch fSym.Type {
					case "string":
						defaultVal = `""`
					case "number":
						defaultVal = "0"
					case "bool":
						defaultVal = "false"
					}
					newText += fmt.Sprintf("    field %s: %s = %s\n", fName, fSym.Type, defaultVal)
					localFields[fName] = true
				}
			}
		}

		for mName, mSym := range embedSym.Methods {
			if !localMethods[mName] {
				paramParts := []string{}
				for _, param := range mSym.Params {
					part := param.Name
					if param.Type != "" {
						part += ": " + param.Type
					}
					paramParts = append(paramParts, part)
				}
				paramStr := strings.Join(paramParts, ", ")
				retStr := ""
				if mSym.Returns != "" && mSym.Returns != "any" {
					retStr = ": " + mSym.Returns
				}
				newText += fmt.Sprintf("    fn %s(%s)%s {\n    }\n\n", mName, paramStr, retStr)
				localMethods[mName] = true
			}
		}
	}

	if newText == "" {
		return CodeAction{}, false
	}

	insertPos := bytePositionAtOffset(text, targetBlock.End-1)
	editRange := LSPRange{Start: insertPos, End: insertPos}

	return CodeAction{
		Title: "Implement missing methods/fields",
		Kind:  "quickfix",
		Edit: WorkspaceEdit{
			Changes: map[string][]TextEdit{
				uri: {
					{
						Range:   editRange,
						NewText: "\n" + newText,
					},
				},
			},
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

func stripTrailingLineComment(line string) string {
	inString := byte(0)
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if escaped {
			escaped = false
			continue
		}

		if inString != 0 {
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			continue
		}

		if ch == '/' && i+1 < len(line) && line[i+1] == '/' {
			return line[:i]
		}
	}

	return line
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
	docs := map[string]string{}
	docKeys := map[string]string{}
	addReferenceDocument(docs, docKeys, uri, text, true)
	for openDocument, openText := range lspDocs {
		openURI := openDocument
		if !strings.HasPrefix(openURI, "file:") {
			openURI = pathToFileURI(openURI)
		}
		addReferenceDocument(docs, docKeys, openURI, openText, false)
	}
	collectImportedReferenceDocuments(uri, text, docs, map[string]bool{})
	return docs
}

func addReferenceDocument(docs map[string]string, docKeys map[string]string, uri string, text string, prefer bool) {
	key := referenceDocumentKey(uri)
	if existingURI, exists := docKeys[key]; exists {
		if !prefer {
			return
		}
		delete(docs, existingURI)
	}

	docs[uri] = text
	docKeys[key] = uri
}

func referenceDocumentKey(uri string) string {
	path := filepath.Clean(URIToPath(uri))
	if runtime.GOOS == "windows" {
		path = strings.ToLower(path)
	}
	return path
}

func collectImportedReferenceDocuments(uri string, text string, docs map[string]string, visited map[string]bool) {
	if visited[uri] {
		return
	}
	visited[uri] = true

	cleanedText := stripNativeGoBlocks(text)
	matches := fileImportRegex.FindAllStringSubmatch(cleanedText, -1)
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

	libraryMatches := libraryImportRegex.FindAllStringSubmatch(cleanedText, -1)
	for _, match := range libraryMatches {
		resolved := resolveLibraryImportPath(match[1], uri)
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
	_ = line

	lexer := NewLexer(rawLine, "")
	next := func() Token {
		return lexer.NextToken()
	}

	tok := next()
	exported := false
	if tok.Type == TOKEN_EXPORT {
		exported = true
		tok = next()
	}

	detailPrefix := ""
	if exported {
		detailPrefix = "export "
	}

	switch tok.Type {
	case TOKEN_FN:
		name := next()
		if !isDocumentSymbolNameToken(name) {
			return DocumentSymbol{}, false
		}
		return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"function", 12), true

	case TOKEN_INTERFACE:
		name := next()
		if name.Type != TOKEN_IDENT {
			return DocumentSymbol{}, false
		}
		return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"interface", 11), true

	case TOKEN_CLASS:
		name := next()
		if name.Type != TOKEN_IDENT {
			return DocumentSymbol{}, false
		}
		return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"class", 5), true

	case TOKEN_LET, TOKEN_CONST:
		name := next()
		if name.Type != TOKEN_IDENT {
			return DocumentSymbol{}, false
		}
		return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"variable", 13), true

	case TOKEN_EXTERNAL:
		kind := next()
		name := next()
		if !isDocumentSymbolNameToken(name) {
			return DocumentSymbol{}, false
		}
		switch kind.Type {
		case TOKEN_FN:
			return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"external function", 12), true
		case TOKEN_CONST:
			if name.Type != TOKEN_IDENT {
				return DocumentSymbol{}, false
			}
			return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+"external global", 13), true
		}

	case TOKEN_EMBED_TEXT, TOKEN_EMBED_BYTES, TOKEN_EMBED_FOLDER:
		path := next()
		storage := next()
		name := next()
		if path.Type != TOKEN_STRING || (storage.Type != TOKEN_LET && storage.Type != TOKEN_CONST) || name.Type != TOKEN_IDENT {
			return DocumentSymbol{}, false
		}
		return makeDocumentSymbol(rawLine, lineIndex, name.Literal, detailPrefix+tok.Literal, 13), true
	}

	return DocumentSymbol{}, false
}

func isDocumentSymbolNameToken(tok Token) bool {
	if tok.Type == TOKEN_EOF || tok.Literal == "" {
		return false
	}

	first := tok.Literal[0]
	if !(first == '_' || first >= 'A' && first <= 'Z' || first >= 'a' && first <= 'z') {
		return false
	}

	for i := 1; i < len(tok.Literal); i++ {
		ch := tok.Literal[i]
		if !(ch == '_' || ch >= 'A' && ch <= 'Z' || ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9') {
			return false
		}
	}

	return true
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
			if strings.TrimSpace(member.Name) == "" || isPrivateImportMember(member) {
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

func refreshLSPDocumentFast(uri string, text string) {
	path := URIToPath(uri)
	lspDocs[path] = text
	invalidateLSPImportCacheForURI(path)
	publishDiagnostics(uri, text)
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
					"codeActionProvider": map[string]any{
						"codeActionKinds": []string{"quickfix", "source.organizeImports"},
					},
					"inlayHintProvider":         true,
					"documentSymbolProvider":    true,
					"documentHighlightProvider": true,
					"hoverProvider":             true,
					"implementationProvider":    true,
					"callHierarchyProvider":     true,
					"semanticTokensProvider": map[string]any{
						"legend": map[string]any{
							"tokenTypes":     semanticTokenTypes,
							"tokenModifiers": []string{},
						},
						"full":  true,
						"range": false,
					},
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
			refreshLSPDocumentFast(params.TextDocument.URI, text)
			publishDiagnosticsForImportDependents(params.TextDocument.URI)
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

	case "textDocument/semanticTokens/full":
		var params TextDocumentIdentifierParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		result := getSemanticTokens(params.TextDocument.URI, text)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: result,
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

	case "textDocument/documentHighlight":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[URIToPath(params.TextDocument.URI)]
		params.Position = lspPositionToBytePosition(text, params.Position)

		var result any
		func() {
			defer func() {
				if r := recover(); r != nil {
					result = []DocumentHighlight{}
				}
			}()

			result = getDocumentHighlights(params.TextDocument.URI, text, params.Position)
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

func classBlockAtLine(text string, lineIndex int) *blockInfo {
	offset := offsetAtLine(text, lineIndex+1)

	for _, block := range findBlocks(text, "class") {
		if offset >= block.Start && offset <= block.End {
			return &block
		}
	}

	return nil
}

func functionBlockAtLine(text string, lineIndex int) *blockInfo {
	offset := offsetAtLine(text, lineIndex+1)

	var best *blockInfo

	for _, block := range findBlocks(text, "fn") {
		if offset >= block.Start && offset <= block.End {
			copy := block

			if best == nil || copy.Start > best.Start {
				best = &copy
			}
		}
	}

	return best
}

type semanticTokenCandidate struct {
	Line  int
	Start int
	End   int
	Type  string
}

func getSemanticTokens(uri string, text string) map[string]any {
	tokens := collectSemanticTokens(uri, text)
	sort.SliceStable(tokens, func(i, j int) bool {
		if tokens[i].Line != tokens[j].Line {
			return tokens[i].Line < tokens[j].Line
		}
		if tokens[i].Start != tokens[j].Start {
			return tokens[i].Start < tokens[j].Start
		}
		return tokens[i].End < tokens[j].End
	})

	data := []int{}
	lastLine := 0
	lastStart := 0
	seen := map[string]bool{}

	for _, token := range tokens {
		if token.End <= token.Start {
			continue
		}

		typeIndex, ok := semanticTokenTypeIndex[token.Type]
		if !ok {
			continue
		}

		key := strconv.Itoa(token.Line) + ":" + strconv.Itoa(token.Start) + ":" + strconv.Itoa(token.End)
		if seen[key] {
			continue
		}
		seen[key] = true

		lineText := getLine(text, token.Line)
		startChar := byteColumnToUTF16Column(lineText, token.Start)
		endChar := byteColumnToUTF16Column(lineText, token.End)
		length := endChar - startChar
		if length <= 0 {
			continue
		}

		deltaLine := token.Line - lastLine
		deltaStart := startChar
		if deltaLine == 0 {
			deltaStart = startChar - lastStart
		}

		data = append(data, deltaLine, deltaStart, length, typeIndex, 0)
		lastLine = token.Line
		lastStart = startChar
	}

	return map[string]any{"data": data}
}

type stringInterval struct {
	start int
	end   int
}

type lspParserState struct {
	kind       int // 0: normal, 1: string, 2: template
	quoteChar  byte
	braceDepth int
}

func getStringIntervals(text string) []stringInterval {
	var states []lspParserState
	states = append(states, lspParserState{kind: 0})

	var intervals []stringInterval
	currentStringStart := -1

	for i := 0; i < len(text); i++ {
		ch := text[i]
		currentState := &states[len(states)-1]

		switch currentState.kind {
		case 0:
			if ch == '"' || ch == '\'' {
				states = append(states, lspParserState{kind: 1, quoteChar: ch})
				currentStringStart = i
			} else if ch == '`' {
				states = append(states, lspParserState{kind: 2})
				currentStringStart = i
			} else if ch == '{' {
				if len(states) > 1 {
					currentState.braceDepth++
				}
			} else if ch == '}' {
				if len(states) > 1 {
					currentState.braceDepth--
					if currentState.braceDepth == 0 {
						states = states[:len(states)-1]
						currentStringStart = i + 1
					}
				}
			} else if ch == '/' && i+1 < len(text) && text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
			} else if ch == '/' && i+1 < len(text) && text[i+1] == '*' {
				i += 2
				for i+1 < len(text) && !(text[i] == '*' && text[i+1] == '/') {
					i++
				}
				i++
			}

		case 1:
			if ch == '\\' {
				i++
			} else if ch == currentState.quoteChar {
				intervals = append(intervals, stringInterval{start: currentStringStart, end: i + 1})
				states = states[:len(states)-1]
				currentStringStart = -1
			}

		case 2:
			if ch == '\\' {
				i++
			} else if ch == '`' {
				intervals = append(intervals, stringInterval{start: currentStringStart, end: i + 1})
				states = states[:len(states)-1]
				currentStringStart = -1
			} else if ch == '$' && i+1 < len(text) && text[i+1] == '{' {
				intervals = append(intervals, stringInterval{start: currentStringStart, end: i})
				states = append(states, lspParserState{kind: 0, braceDepth: 1})
				i++
				currentStringStart = -1
			}
		}
	}

	if currentStringStart != -1 {
		intervals = append(intervals, stringInterval{start: currentStringStart, end: len(text)})
	}

	return intervals
}

func isInsideString(intervals []stringInterval, start int, end int) bool {
	for _, interval := range intervals {
		if start >= interval.start && end <= interval.end {
			return true
		}
	}
	return false
}

func collectSemanticTokens(uri string, text string) []semanticTokenCandidate {
	tokens := []semanticTokenCandidate{}
	scope := fileBaseScope(uri, text)
	lines := strings.Split(text, "\n")
	identRe := regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]*`)
	numberRe := regexp.MustCompile(`\b[0-9]+(?:\.[0-9]+)?\b`)

	intervals := getStringIntervals(text)

	lineOffsets := make([]int, len(lines))
	currentOffset := 0
	for lineIndex, line := range lines {
		lineOffsets[lineIndex] = currentOffset
		currentOffset += len(line) + 1
	}

	for lineIndex, rawLine := range lines {
		code := stripLineComment(rawLine)

		for _, match := range numberRe.FindAllStringIndex(code, -1) {
			start := match[0]
			end := match[1]
			tokenStart := lineOffsets[lineIndex] + start
			tokenEnd := lineOffsets[lineIndex] + end
			if isInsideString(intervals, tokenStart, tokenEnd) {
				continue
			}

			if isIdentifierBoundary(code, start-1) && isIdentifierBoundary(code, end) {
				tokens = append(tokens, semanticTokenCandidate{Line: lineIndex, Start: start, End: end, Type: "number"})
			}
		}

		for _, match := range identRe.FindAllStringIndex(code, -1) {
			start := match[0]
			end := match[1]
			tokenStart := lineOffsets[lineIndex] + start
			tokenEnd := lineOffsets[lineIndex] + end
			if isInsideString(intervals, tokenStart, tokenEnd) {
				continue
			}

			name := code[start:end]
			tokenType := semanticTypeForIdentifier(scope, code, name, start, end)
			if tokenType == "" {
				continue
			}
			tokens = append(tokens, semanticTokenCandidate{Line: lineIndex, Start: start, End: end, Type: tokenType})
		}
	}

	return tokens
}

func semanticTypeForIdentifier(scope *Scope, line string, name string, start int, end int) string {
	if tinyKeywords[name] {
		return "keyword"
	}

	prev := previousIdentifierInLine(line, start)
	if prev == "class" {
		return "class"
	}
	if prev == "interface" {
		return "interface"
	}
	if prev == "enum" {
		return "enum"
	}
	if prev == "fn" {
		return "function"
	}
	if prev == "field" {
		return "property"
	}
	if prev == "as" {
		return "namespace"
	}

	if isMemberIdentifier(line, start) {
		if nextNonSpaceByte(line, end) == '(' {
			return "method"
		}
		return "property"
	}

	if isFunctionParameterIdentifier(line, start, end) {
		return "parameter"
	}

	if isTypePosition(line, start) || isBuiltinTypeName(name) {
		return "type"
	}

	if sym, ok := scope.Resolve(name); ok {
		switch sym.Kind {
		case SymbolNamespace, SymbolStd:
			return "namespace"
		case SymbolClass:
			return "class"
		case SymbolInterface:
			return "interface"
		case SymbolEnum:
			return "enum"
		case SymbolFunction:
			return "function"
		case SymbolField:
			return "property"
		case SymbolVariable:
			return "variable"
		}
	}

	if nextNonSpaceByte(line, end) == '(' {
		return "function"
	}

	return "variable"
}

func previousIdentifierInLine(line string, start int) string {
	i := start - 1
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}
	end := i + 1
	for i >= 0 && isIdentChar(line[i]) {
		i--
	}
	if end <= i+1 {
		return ""
	}
	return line[i+1 : end]
}

func nextNonSpaceByte(line string, end int) byte {
	for i := end; i < len(line); i++ {
		if line[i] == ' ' || line[i] == '\t' {
			continue
		}
		return line[i]
	}
	return 0
}

func previousNonSpaceByte(line string, start int) byte {
	for i := start - 1; i >= 0; i-- {
		if line[i] == ' ' || line[i] == '\t' {
			continue
		}
		return line[i]
	}
	return 0
}

func isMemberIdentifier(line string, start int) bool {
	i := start - 1
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}
	return i >= 0 && line[i] == '.'
}

func isTypePosition(line string, start int) bool {
	prev := previousNonSpaceByte(line, start)
	if prev == ':' {
		return true
	}

	prevIdent := previousIdentifierInLine(line, start)
	return prevIdent == "instanceof"
}

func isBuiltinTypeName(name string) bool {
	switch name {
	case "any", "string", "number", "bool", "object", "array", "null", "error":
		return true
	default:
		return false
	}
}

func isFunctionParameterIdentifier(line string, start int, end int) bool {
	fnIdx := strings.Index(line, "fn")
	if fnIdx < 0 || fnIdx > start {
		return false
	}

	openIdx := strings.Index(line[fnIdx:], "(")
	if openIdx < 0 {
		return false
	}
	openIdx += fnIdx

	closeIdx := strings.Index(line[openIdx:], ")")
	if closeIdx < 0 {
		closeIdx = len(line)
	} else {
		closeIdx += openIdx
	}

	if start <= openIdx || end > closeIdx {
		return false
	}

	prev := previousNonSpaceByte(line, start)
	next := nextNonSpaceByte(line, end)
	return prev == '(' || prev == ',' || next == ':' || next == ',' || next == ')'
}

func semanticDiagnostics(uri string, text string) []map[string]any {
	return semanticDiagnosticsFromAST(uri, text)
}

func publishDiagnostics(uri string, text string) {
	publishDiagnosticsWithMode(uri, text, true)
}

func publishParseDiagnostics(uri string, text string) {
	publishDiagnosticsWithMode(uri, text, false)
}

func publishDiagnosticsWithMode(uri string, text string, includeSlowChecks bool) {
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
		column := diagnostic.Column

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

	if includeSlowChecks {
		diagnostics = append(diagnostics, semanticDiagnostics(uri, text)...)
	}
	if includeSlowChecks {
		diagnostics = append(diagnostics, importDiagnostics(uri, text)...)
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
	if statements, parseDiagnostics := parseTinyForLSP(URIToPath(uri), text); len(parseDiagnostics) == 0 && statements != nil {
		for _, raw := range statements {
			stmt, _ := unwrapExport(raw)
			imp, ok := stmt.(ImportStmt)
			if !ok || imp.Std {
				continue
			}
			message := importDiagnosticMessage(uri, imp)
			if message == "" {
				continue
			}
			start := imp.Column - 1
			if start < 0 {
				start = 0
			}
			diagnostics = append(diagnostics, map[string]any{
				"range":    lspRangeFromByteColumns(text, imp.Line-1, start, start+len(imp.Path)),
				"severity": 1,
				"message":  message,
				"source":   "tiny",
			})
		}
		return diagnostics
	}

	cleanedText := stripStringsAndCommentsForImportScan(stripNativeGoBlocks(text))
	lines := strings.Split(cleanedText, "\n")
	for lineIndex, line := range lines {
		code := stripTrailingLineComment(line)
		for _, imp := range importPathsInLine(code) {
			message := importPathDiagnosticMessage(uri, imp.Kind, imp.Path)
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

func importDiagnosticMessage(uri string, imp ImportStmt) string {
	kind := "file"
	if imp.Library {
		kind = "library"
	} else if imp.Plugin {
		kind = "plugin"
	}
	return importPathDiagnosticMessage(uri, kind, imp.Path)
}

func importPathDiagnosticMessage(uri string, kind string, path string) string {
	switch kind {
	case "library":
		if !isCompleteLibraryImportPath(path) {
			return ""
		}
		if !libraryImportRootExists(path, uri) {
			return "library is not installed: " + path
		}
		resolved := resolveLibraryImportPath(path, uri)
		if _, err := os.Stat(resolved); err != nil {
			return "library entry file not found: " + path
		}
	case "file":
		resolved := resolveImportPath(uri, path)
		if _, err := os.Stat(resolved); err != nil {
			return "import file not found: " + path
		}
	case "plugin":
		resolved := resolveImportPath(uri, path)
		if _, err := os.Stat(resolved); err != nil {
			return "plugin file not found: " + path
		}
	}
	return ""
}

func stripStringsAndCommentsForImportScan(text string) string {
	out := []byte(text)
	inString := byte(0)
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(out); i++ {
		ch := out[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			} else {
				out[i] = ' '
			}
			continue
		}

		if inBlockComment {
			if ch == '\n' {
				continue
			}
			if ch == '*' && i+1 < len(out) && out[i+1] == '/' {
				out[i] = ' '
				out[i+1] = ' '
				i++
				inBlockComment = false
				continue
			}
			out[i] = ' '
			continue
		}

		if inString != 0 {
			if ch == '\n' {
				if inString != '`' {
					inString = 0
					escaped = false
				}
				continue
			}
			out[i] = ' '
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

		if ch == '/' && i+1 < len(out) && out[i+1] == '/' {
			out[i] = ' '
			out[i+1] = ' '
			i++
			inLineComment = true
			continue
		}

		if ch == '/' && i+1 < len(out) && out[i+1] == '*' {
			out[i] = ' '
			out[i+1] = ' '
			i++
			inBlockComment = true
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			out[i] = ' '
			inString = ch
			escaped = false
			continue
		}
	}

	return string(out)
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

func isCompleteLibraryImportPath(path string) bool {
	lib, ok := parseLibraryImportPath(path)
	return ok && lib.Owner != "" && lib.Repo != ""
}

func publishDiagnosticsForImportDependents(changedURI string) {
	for _, uri := range dependentDocumentURIs(changedURI) {
		if text, ok := lspDocs[uri]; ok {
			publishDiagnostics(uri, text)
			continue
		}

		path := URIToPath(uri)
		if text, ok := lspDocs[path]; ok {
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
	for docPath, text := range lspDocs {
		cleanDocPath := filepath.Clean(URIToPath(docPath))
		if cleanDocPath == changedPath {
			continue
		}
		if documentImportsPath(cleanDocPath, text, changedPath, map[string]bool{}) {
			dependents = append(dependents, pathToFileURI(cleanDocPath))
		}
	}

	sort.Strings(dependents)
	return dependents
}

func documentImportsPath(uri string, text string, targetPath string, visited map[string]bool) bool {
	docPath := filepath.Clean(URIToPath(uri))
	if visited[docPath] {
		return false
	}
	visited[docPath] = true

	targetPath = filepath.Clean(targetPath)

	cleanedText2 := stripNativeGoBlocks(text)
	for _, match := range fileImportRegex.FindAllStringSubmatch(cleanedText2, -1) {
		importPath := filepath.Clean(resolveImportPath(docPath, match[1]))
		if importPath == targetPath {
			return true
		}

		importURI := pathToFileURI(importPath)
		importText, ok := tinyFileTextForLSP(importPath, importURI)
		if ok && documentImportsPath(importPath, importText, targetPath, visited) {
			return true
		}
	}

	for _, match := range libraryImportRegex.FindAllStringSubmatch(cleanedText2, -1) {
		importPath := filepath.Clean(resolveLibraryImportPath(match[1], docPath))
		if importPath == targetPath {
			return true
		}

		importURI := pathToFileURI(importPath)
		importText, ok := tinyFileTextForLSP(importPath, importURI)
		if ok && documentImportsPath(importPath, importText, targetPath, visited) {
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

func libraryImportPathCompletions(uri string, line string, character int) []CompletionItem {
	prefix := libraryImportPrefixAt(line, character)
	if prefix == "" || strings.Count(prefix, "/") < 2 {
		return libraryPackageCompletions()
	}

	lib, ok := parseLibraryImportPath(prefix)
	if !ok {
		return libraryPackageCompletions()
	}

	root := resolveLibraryRoot(lib.Owner, lib.Repo, uri)
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
		snippetCompletion("lock", "lock statement", "lock ${0:mutex} {\n\t $1\n}"),
		snippetCompletion("embedtext", "embedtext statement", "embedtext \"$0\" const $1"),
		snippetCompletion("embedbytes", "embedbytes statement", "embedbytes \"$0\" const $1"),
		snippetCompletion("embedfolder", "embedfolder statement", "embedfolder \"$0\" const $1"),
		snippetCompletion("native fn", "native fn statement", "native fn ${0:Name}(): null {\n\tgo {\n$1\n\t}\n}"),
		{Label: "await ", Kind: 14, Detail: "await statement"},
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
	items = append(items, libraryAutoImportCompletions(scope, uri, text, hasParens)...)

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
	lines := strings.Split(stripNativeGoBlocks(text), "\n")
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
	cleanedText3 := stripNativeGoBlocks(text)
	for _, match := range fileImportRegex.FindAllStringSubmatch(cleanedText3, -1) {
		if filepath.ToSlash(match[1]) == filepath.ToSlash(importPath) {
			return true
		}
	}
	return false
}

func libraryImportAlreadyPresent(text string, importPath string) bool {
	cleanedText := stripNativeGoBlocks(text)
	for _, match := range libraryImportRegex.FindAllStringSubmatch(cleanedText, -1) {
		if filepath.ToSlash(match[1]) == filepath.ToSlash(importPath) {
			return true
		}
	}
	return false
}

func libraryImportAlias(importPath string) string {
	lib, ok := parseLibraryImportPath(importPath)
	if !ok {
		return "Lib"
	}
	if lib.Rest == "" {
		return cleanAliasString(lib.Repo)
	}
	base := strings.TrimSuffix(filepath.Base(lib.Rest), filepath.Ext(lib.Rest))
	return cleanAliasString(base)
}

func cleanAliasString(s string) string {
	parts := strings.FieldsFunc(s, func(r rune) bool {
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
		return s
	}
	return alias
}

func libraryAutoImportCompletions(scope *Scope, uri string, text string, hasParens bool) []CompletionItem {
	currentPath := URIToPath(uri)
	if currentPath == "" {
		return nil
	}

	type libraryRef struct {
		Owner string
		Repo  string
	}
	libs := []libraryRef{}
	seenLib := map[string]bool{}

	addLib := func(owner, repo string) {
		if owner == "" || repo == "" {
			return
		}
		key := strings.ToLower(owner + "/" + repo)
		if seenLib[key] {
			return
		}
		seenLib[key] = true
		libs = append(libs, libraryRef{Owner: owner, Repo: repo})
	}

	var config TinyProjectConfig
	var configOk bool
	config, configOk = loadTinyConfigFromPath(currentPath)
	if !configOk {
		config, configOk = loadTinyConfig()
	}
	if configOk {
		for _, dep := range config.Dependencies {
			if dep.Source != "" {
				spec := parseGitHubPackageSource(dep.Source)
				addLib(spec.Owner, spec.Repo)
			}
		}
	}

	for _, lib := range scanInstalledLibraries() {
		addLib(lib.Owner, lib.Repo)
	}

	items := []CompletionItem{}

	for _, lib := range libs {
		root := resolveLibraryRoot(lib.Owner, lib.Repo, currentPath)
		if root == "" {
			continue
		}
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}

		entryFile := "main.tiny"
		libConfig, ok := loadTinyConfigFrom(filepath.Join(root, "tiny.json"))
		if ok && libConfig.Entry != "" {
			entryFile = filepath.Clean(libConfig.Entry)
		}

		filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return nil
			}
			if info.IsDir() {
				if strings.HasPrefix(info.Name(), ".") || info.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			if filepath.Ext(path) != ".tiny" {
				return nil
			}

			relPath, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}

			relImport := filepath.ToSlash(relPath)
			libImportPath := lib.Owner + "/" + lib.Repo + "/" + relImport

			if filepath.Clean(relPath) == filepath.Clean(entryFile) {
				libImportPath = lib.Owner + "/" + lib.Repo
			}

			if libraryImportAlreadyPresent(text, libImportPath) {
				return nil
			}

			exports := loadTinyFileExports(path, map[string]bool{})
			if len(exports) == 0 {
				return nil
			}

			alias := libraryImportAlias(libImportPath)
			importEdit := importTextEdit(text, `import lib "`+libImportPath+`" as `+alias+`;`)

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
					Detail:              "auto import from lib " + libImportPath,
					InsertText:          insert,
					InsertTextFormat:    format,
					AdditionalTextEdits: []TextEdit{importEdit},
				})
			}

			return nil
		})
	}

	return items
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
	seen := map[string]int{}
	result := []CompletionItem{}

	for _, item := range items {
		if existingIndex, ok := seen[item.Label]; ok {
			result[existingIndex] = mergeCompletionItems(result[existingIndex], item)
			continue
		}

		seen[item.Label] = len(result)
		result = append(result, item)
	}

	return result
}

func mergeCompletionItems(base CompletionItem, incoming CompletionItem) CompletionItem {
	if base.Kind == 0 {
		base.Kind = incoming.Kind
	}
	if base.Detail == "" {
		base.Detail = incoming.Detail
	}
	if base.InsertText == "" {
		base.InsertText = incoming.InsertText
	}
	if base.InsertTextFormat == 0 {
		base.InsertTextFormat = incoming.InsertTextFormat
	}
	if base.SortText == "" {
		base.SortText = incoming.SortText
	}
	if base.TextEdit == nil {
		base.TextEdit = incoming.TextEdit
	}

	base.AdditionalTextEdits = mergeTextEdits(base.AdditionalTextEdits, incoming.AdditionalTextEdits)
	return base
}

func mergeTextEdits(base []TextEdit, incoming []TextEdit) []TextEdit {
	if len(incoming) == 0 {
		return base
	}

	for _, edit := range incoming {
		alreadyExists := false
		for _, existing := range base {
			if existing.Range.Start.Line == edit.Range.Start.Line &&
				existing.Range.Start.Character == edit.Range.Start.Character &&
				existing.Range.End.Line == edit.Range.End.Line &&
				existing.Range.End.Character == edit.Range.End.Character &&
				existing.NewText == edit.NewText {
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			base = append(base, edit)
		}
	}

	return base
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
	offset := offsetFromLineCol(text, pos.Line+1, pos.Character+1)
	for _, b := range findBlocks(text, "class") {
		if offset >= b.Start && offset <= b.End {
			return b.Name
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
	if !sym.TypeRef.IsZero() {
		switch sym.TypeRef.Kind {
		case LSPTypeClass, LSPTypeInterface, LSPTypeEnum:
			if strings.Contains(receiver, ".") {
				ref := sym.TypeRef
				ref.Name = receiver
				return ref.String()
			}
		}
		if text := sym.TypeRef.String(); text != "" {
			return text
		}
	}

	switch sym.Kind {
	case SymbolClass:
		if strings.Contains(receiver, ".") {
			return "class:" + receiver
		}
		return "class:" + sym.Name

	case SymbolInterface:
		if strings.Contains(receiver, ".") {
			return "interface:" + receiver
		}
		return "interface:" + sym.Name

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
	receiver = strings.TrimSuffix(receiver, "?")

	var parts []string
	var current strings.Builder

	inString := byte(0)
	escaped := false
	parenDepth := 0
	bracketDepth := 0

	i := 0
	for i < len(receiver) {
		ch := receiver[i]

		if inString != 0 {
			if escaped {
				escaped = false
			} else if ch == '\\' {
				escaped = true
			} else if ch == inString {
				inString = 0
			}
			current.WriteByte(ch)
			i++
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			current.WriteByte(ch)
			i++
			continue
		}

		if ch == '(' {
			parenDepth++
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == ')' {
			if parenDepth > 0 {
				parenDepth--
			}
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == '[' {
			bracketDepth++
			current.WriteByte(ch)
			i++
			continue
		}
		if ch == ']' {
			if bracketDepth > 0 {
				bracketDepth--
			}
			current.WriteByte(ch)
			i++
			continue
		}

		if parenDepth == 0 && bracketDepth == 0 {
			if strings.HasPrefix(receiver[i:], "?.") {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
				i += 2
				continue
			}
			if ch == '.' {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
				i++
				continue
			}
		}

		current.WriteByte(ch)
		i++
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

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

	if strings.HasPrefix(typ, "namespace:") {
		nsName := strings.TrimPrefix(typ, "namespace:")
		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			if memberSym, ok := ns.Members[member]; ok {
				if isPrivateImportMember(memberSym) {
					return SymbolInfo{}, "unknown", false
				}
				return memberSym, staticTypeOfSymbol(nsName+"."+member, memberSym), true
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

		parts := strings.Split(className, ":")
		parsed, _ := parseOneLSPType(scope, parts)
		subst := map[string]string{}
		for i, tp := range classSym.TypeParameters {
			if i < len(parsed.Args) {
				subst[tp] = formatLSPTypeStruct(parsed.Args[i])
			} else {
				subst[tp] = "any"
			}
		}

		if fieldSym, ok := classSym.Fields[member]; ok {
			resolvedSym := fieldSym
			resolvedSym.Type = substituteLSPType(fieldSym.Type, subst)
			return resolvedSym, resolvedSym.Type, true
		}

		if methodSym, ok := classSym.Methods[member]; ok {
			resolvedSym := methodSym
			resolvedSym.Returns = substituteLSPType(methodSym.Returns, subst)
			if len(methodSym.Params) > 0 {
				resolvedSym.Params = make([]StdArg, len(methodSym.Params))
				for i, param := range methodSym.Params {
					resolvedSym.Params[i] = param
					resolvedSym.Params[i].Type = substituteLSPType(param.Type, subst)
				}
			}
			return resolvedSym, firstNonEmpty(resolvedSym.Returns, "function"), true
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

func stripIndexAccesses(part string) (string, int) {
	base := part
	count := 0
	for {
		base = strings.TrimSpace(base)
		if !strings.HasSuffix(base, "]") {
			break
		}
		depth := 0
		found := -1
		for i := len(base) - 1; i >= 0; i-- {
			if base[i] == ']' {
				depth++
			} else if base[i] == '[' {
				depth--
				if depth == 0 {
					found = i
					break
				}
			}
		}
		if found >= 0 {
			base = base[:found]
			count++
		} else {
			break
		}
	}
	return strings.TrimSpace(base), count
}

func applyIndexAccessType(typ string, count int) string {
	for i := 0; i < count; i++ {
		typ = strings.TrimSpace(typ)
		if strings.HasPrefix(typ, "array:") {
			typ = strings.TrimPrefix(typ, "array:")
		} else if typ == "array" {
			typ = "any"
		} else {
			return "any"
		}
	}
	return typ
}

func resolveReceiverPath(scope *Scope, text string, pos Position, receiver string) (SymbolInfo, string, bool) {
	if strings.Contains(receiver, "(") || strings.Contains(receiver, "[") {
		typ := normalizeLSPType(scope, inferExprTypeFromText(scope, receiver))
		if typ != "" && typ != "unknown" {
			return SymbolInfo{
				Name: receiver,
				Kind: SymbolVariable,
				Type: typ,
			}, typ, true
		}
	}

	parts := splitReceiverPath(receiver)
	if len(parts) == 0 {
		return SymbolInfo{}, "", false
	}

	var sym SymbolInfo
	var typ string
	ok := false

	baseName, indexCount := stripIndexAccesses(parts[0])

	if baseName == "this" {
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
		sym, ok = scope.Resolve(baseName)
		if !ok {
			return SymbolInfo{}, "", false
		}
		typ = staticTypeOfSymbol(baseName, sym)
	}

	typ = applyIndexAccessType(typ, indexCount)

	if len(parts) == 1 {
		return sym, typ, true
	}

	for _, member := range parts[1:] {
		cleanMember, memberIndexCount := stripIndexAccesses(member)
		cleanMember = cleanMemberName(cleanMember)

		if sym.Kind == SymbolNamespace {
			memberSym, exists := sym.Members[cleanMember]
			if !exists || isPrivateImportMember(memberSym) {
				return SymbolInfo{}, "unknown", false
			}
			nsName := strings.TrimPrefix(typ, "namespace:")
			if nsName == "" || nsName == typ {
				nsName = sym.Name
			}
			sym = memberSym
			typ = staticTypeOfSymbol(nsName+"."+cleanMember, memberSym)
		} else if fieldSym, exists := sym.Fields[cleanMember]; exists {
			sym = fieldSym
			typ = fieldSym.Type
		} else if methodSym, exists := sym.Methods[cleanMember]; exists {
			sym = methodSym
			typ = firstNonEmpty(methodSym.Returns, "function")
		} else if nextSym, nextType, exists := resolveMemberFromStaticType(scope, typ, cleanMember); exists {
			sym = nextSym
			typ = nextType
		} else if sym.Type == "object" && sym.Fields != nil {
			if fieldSym, exists := sym.Fields[cleanMember]; exists {
				sym = fieldSym
				typ = fieldSym.Type
			} else {
				return SymbolInfo{}, "unknown", false
			}
		} else {
			return SymbolInfo{}, "unknown", false
		}

		typ = applyIndexAccessType(typ, memberIndexCount)
	}

	return sym, typ, true
}

func currentClassSymbolAtPosition(scope *Scope, text string, pos Position, className string) (SymbolInfo, bool) {
	classLine := -1
	body := ""
	bodyBaseLine := 1

	if block := classBlockAtLine(text, pos.Line); block != nil && block.Name == className {
		classLine = block.Line - 1
		body = block.Body
		bodyBaseLine = block.Line
	} else {
		var found bool
		for _, b := range findBlocks(text, "class") {
			if b.Name == className {
				classLine = b.Line - 1
				body = b.Body
				bodyBaseLine = b.Line
				found = true
				break
			}
		}

		if !found {
			return resolveClassSymbol(scope, className)
		}
	}

	fields := scanClassFields(scope, body, "", bodyBaseLine)
	methods := map[string]SymbolInfo{}
	collectEmbeddedSymbolsFromBody(scope, body, fields, methods, "", bodyBaseLine)

	var classBlock *blockInfo
	for _, b := range findBlocks(text, "class") {
		if b.Name == className {
			classBlock = &b
			break
		}
	}

	if classBlock != nil {
		for _, b := range findBlocks(text, "fn") {
			if b.Start > classBlock.Start && b.Start < classBlock.End {
				params := normalizeStdArgs(scope, parseFunctionParams(b.ParamsText))

				nestedScope := NewScope(scope)
				for _, p := range params {
					nestedScope.Define(SymbolInfo{
						Name: p.Name,
						Kind: SymbolVariable,
						Type: p.Type,
					})
				}
				if classSym, exists := resolveClassSymbol(scope, className); exists {
					nestedScope.Define(SymbolInfo{
						Name:    "this",
						Kind:    SymbolVariable,
						Type:    "class:" + className,
						Fields:  classSym.Fields,
						Methods: classSym.Methods,
					})
				}

				returnType := inferReturnTypeFromBody(nestedScope, b.Body, b.ReturnType)
				if b.IsAsync {
					returnType = "task:" + returnType
				}

				detail := "method " + className + "." + b.Name
				lines := strings.Split(text, "\n")
				if b.Line-1 >= 0 && b.Line-1 < len(lines) {
					line := lines[b.Line-1]
					fnIdx := strings.Index(line, "fn")
					if fnIdx > 0 && strings.Contains(line[:fnIdx], "private") {
						detail = "private " + detail
					}
				}

				methods[b.Name] = SymbolInfo{
					Name:    b.Name,
					Kind:    SymbolFunction,
					Type:    "function",
					Detail:  detail,
					Line:    b.Line,
					Column:  b.Column,
					Params:  params,
					Returns: returnType,
				}
			}
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

func nullableReceiverTextEdit(text string, pos Position) (TextEdit, bool) {
	line := getLine(text, pos.Line)
	before := ""
	if pos.Character <= len(line) {
		before = line[:pos.Character]
	} else {
		before = line
	}

	dotIndex, ok := findLastDotIndex(before)
	if !ok {
		return TextEdit{}, false
	}

	return TextEdit{
		Range: LSPRange{
			Start: Position{Line: pos.Line, Character: dotIndex},
			End:   Position{Line: pos.Line, Character: dotIndex + 1},
		},
		NewText: "?.",
	}, true
}

func addAdditionalTextEditToCompletions(items []CompletionItem, edit TextEdit) []CompletionItem {
	for i := range items {
		items[i].AdditionalTextEdits = mergeTextEdits(items[i].AdditionalTextEdits, []TextEdit{edit})
	}
	return items
}

func completionItemsForReceiver(scope *Scope, text string, pos Position, receiver string, hasParens bool) []CompletionItem {
	sym, typ, ok := resolveReceiverPath(scope, text, pos, receiver)
	if !ok {
		return []CompletionItem{}
	}

	if (typ == "any" || typ == "unknown") && receiver != "" {
		if ifLine, isInElse, ok := findEnclosingIfAndElse(text, pos); ok {
			narrowedScope := cloneScope(scope)
			applyTypeNarrowing(narrowedScope, ifLine, isInElse)
			if narrowedSym, narrowedType, narrowedOK := resolveReceiverPath(narrowedScope, text, pos, receiver); narrowedOK {
				sym = narrowedSym
				typ = narrowedType
			}
		}
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

		items = completionItemsForInterface(ifaceSym)
	} else if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")
		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			classSym = SymbolInfo{Kind: SymbolClass}
		} else {
			classSym = copyAndSubstituteClassSym(scope, classSym, className)
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

	items = dedupeCompletionItems(items)

	if isNullableLSPType(typ) {
		if edit, ok := nullableReceiverTextEdit(text, pos); ok {
			items = addAdditionalTextEditToCompletions(items, edit)
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
				items = append(items, completionItemsForInterface(ifaceSym)...)
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

func completionItemsForInterface(ifaceSym SymbolInfo) []CompletionItem {
	items := []CompletionItem{}
	names := make([]string, 0, len(ifaceSym.Fields))
	for name := range ifaceSym.Fields {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		field := ifaceSym.Fields[name]
		items = append(items, CompletionItem{
			Label:  field.Name,
			Kind:   symbolKindToCompletionKind(field.Kind),
			Detail: "interface field " + field.Name + " : " + field.Type,
		})
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
		return rankedCompletionItems(libraryImportPathCompletions(uri, line, pos.Character))
	}

	if isInsidePluginImportString(line, pos.Character) {
		return rankedCompletionItems(pluginImportPathCompletions())
	}

	if isInsideFileImportString(line, pos.Character) {
		return rankedCompletionItems(fileImportPathCompletions(uri, line, pos.Character))
	}

	before := line[:pos.Character]
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
		if isPrivateImportMember(member) {
			continue
		}
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
	depthBracket := 0

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

		if depthBracket > 0 {
			switch ch {
			case ']':
				depthBracket++
			case '[':
				depthBracket--
			}
			i--
			continue
		}

		if isIdentChar(ch) || ch == '.' || ch == '?' || ch == ':' {
			i--
			continue
		}

		if ch == ')' {
			depthParen++
			i--
			continue
		}

		if ch == ']' {
			depthBracket++
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

	statements, diagnostics := parseTinyForLSP("", text)
	if statements != nil && len(diagnostics) == 0 {
		for _, raw := range statements {
			stmt, _ := unwrapExport(raw)
			importStmt, ok := stmt.(ImportStmt)
			if !ok || !importStmt.Std {
				continue
			}

			module := importStmt.Path
			alias := module
			if importStmt.Alias != "" {
				alias = importStmt.Alias
			}

			result[alias] = module
		}
		return result
	}

	cleanedText := stripNativeGoBlocks(text)
	for _, rawLine := range strings.Split(cleanedText, "\n") {
		module, alias, ok := parseStdImportLineWithoutRegex(stripTrailingLineComment(rawLine))
		if ok {
			result[alias] = module
		}
	}

	return result
}

func parseStdImportLineWithoutRegex(line string) (module string, alias string, ok bool) {
	line = strings.TrimSpace(strings.TrimSuffix(line, ";"))
	if !strings.HasPrefix(line, "import ") {
		return "", "", false
	}

	rest := strings.TrimSpace(strings.TrimPrefix(line, "import "))
	if !strings.HasPrefix(rest, "std ") {
		return "", "", false
	}

	rest = strings.TrimSpace(strings.TrimPrefix(rest, "std "))
	if !strings.HasPrefix(rest, "\"") {
		return "", "", false
	}

	rest = rest[1:]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return "", "", false
	}

	module = rest[:end]
	if module == "" {
		return "", "", false
	}

	alias = module
	rest = strings.TrimSpace(rest[end+1:])
	if rest != "" {
		parts := strings.Fields(rest)
		if len(parts) == 2 && parts[0] == "as" && isSimpleIdentifier(parts[1]) {
			alias = parts[1]
		}
	}

	return module, alias, true
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

func copyAndSubstituteClassSym(scope *Scope, classSym SymbolInfo, className string) SymbolInfo {
	parts := strings.Split(className, ":")
	parsed, _ := parseOneLSPType(scope, parts)

	subst := map[string]string{}
	for i, tp := range classSym.TypeParameters {
		if i < len(parsed.Args) {
			subst[tp] = formatLSPTypeStruct(parsed.Args[i])
		} else {
			subst[tp] = "any"
		}
	}

	copied := classSym

	if len(classSym.Fields) > 0 {
		copied.Fields = map[string]SymbolInfo{}
		for name, field := range classSym.Fields {
			f := field
			f.Type = substituteLSPType(field.Type, subst)
			copied.Fields[name] = f
		}
	}

	if len(classSym.Methods) > 0 {
		copied.Methods = map[string]SymbolInfo{}
		for name, method := range classSym.Methods {
			m := method
			m.Returns = substituteLSPType(method.Returns, subst)
			if len(method.Params) > 0 {
				m.Params = make([]StdArg, len(method.Params))
				for i, p := range method.Params {
					m.Params[i] = p
					m.Params[i].Type = substituteLSPType(p.Type, subst)
				}
			}
			copied.Methods[name] = m
		}
	}

	return copied
}
