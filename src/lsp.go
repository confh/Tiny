// lsp.go

package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

	. "language.com/src/vm"
)

type LSPMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result"`
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

type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type CompletionParams struct {
	TextDocument TextDocumentIdentifier `json:"textDocument"`
	Position     Position               `json:"position"`
}

type CompletionItem struct {
	Label  string `json:"label"`
	Kind   int    `json:"kind,omitempty"`
	Detail string `json:"detail,omitempty"`
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

	"fn":    true,
	"let":   true,
	"const": true,
	"class": true,
	"embed": true,

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
	line := getLine(text, pos.Line)

	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	before := line[:pos.Character]

	open := strings.LastIndex(before, "(")
	if open == -1 {
		return CallContext{}, false
	}

	prefix := strings.TrimSpace(before[:open])
	if prefix == "" {
		return CallContext{}, false
	}

	argText := before[open+1:]
	argIndex := 0
	for _, ch := range argText {
		if ch == ',' {
			argIndex++
		}
	}

	// member call: io.println(
	if dot := strings.LastIndex(prefix, "."); dot != -1 {
		receiver := strings.TrimSpace(prefix[:dot])
		method := strings.TrimSpace(prefix[dot+1:])

		return CallContext{
			Receiver: receiver,
			Method:   method,
			ArgIndex: argIndex,
			IsMember: true,
		}, true
	}

	// normal call: fib(
	name := ""

	i := len(prefix) - 1
	for i >= 0 && isIdentChar(prefix[i]) {
		i--
	}
	name = prefix[i+1:]

	if name == "" {
		return CallContext{}, false
	}

	return CallContext{
		Name:     name,
		ArgIndex: argIndex,
		IsMember: false,
	}, true
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
			Label:  method.Name,
			Kind:   2,
			Detail: formatStdSignature(module, method),
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
	initLSPLogger()

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

func getSignatureHelp(uri string, text string, pos Position) any {
	ctx, ok := callContextAtPosition(text, pos)
	if !ok {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	if ctx.IsMember {
		sym, exists := scope.Resolve(ctx.Receiver)
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

		if strings.HasPrefix(sym.Type, "class:") {
			className := strings.TrimPrefix(sym.Type, "class:")

			classSym, ok := scope.Resolve(className)
			if !ok || classSym.Kind != SymbolClass {
				return nil
			}

			methodSym, ok := classSym.Methods[ctx.Method]
			if !ok {
				return nil
			}

			return signatureHelpFromFunction(methodSym, ctx.ArgIndex)
		}

		if strings.HasPrefix(sym.Type, "std:") {
			module := strings.TrimPrefix(sym.Type, "std:")

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

		method, ok := GetNativeMethodInfo(sym.Type, ctx.Method)
		if ok {
			return signatureHelpFromMethod(sym.Type+"."+ctx.Method, method, ctx.ArgIndex)
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
		receiverSym, exists := scope.Resolve(receiver)
		if !exists {
			return nil
		}

		if receiverSym.Kind == SymbolNamespace {
			if memberSym, ok := receiverSym.Members[member]; ok {
				return locationFromSymbol(uri, memberSym)
			}
			return nil
		}

		if strings.HasPrefix(receiverSym.Type, "class:") {
			className := strings.TrimPrefix(receiverSym.Type, "class:")
			classSym, ok := scope.Resolve(className)
			if !ok {
				return nil
			}

			if methodSym, ok := classSym.Methods[member]; ok {
				return locationFromSymbol(uri, methodSym)
			}
			return nil
		}

		if receiverSym.Type == "object" && receiverSym.Fields != nil {
			if fieldSym, ok := receiverSym.Fields[member]; ok {
				return locationFromSymbol(uri, fieldSym)
			}
		}

		return nil
	}

	sym, ok := scope.Resolve(word)
	if !ok {
		return nil
	}

	return locationFromSymbol(uri, sym)
}

func locationFromSymbol(defaultURI string, sym SymbolInfo) any {
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

	return Location{
		URI: targetURI,
		Range: LSPRange{
			Start: Position{
				Line:      line,
				Character: column,
			},
			End: Position{
				Line:      line,
				Character: column + len(sym.Name),
			},
		},
	}
}

func getDocumentSymbols(uri string, text string) []DocumentSymbol {
	lines := strings.Split(text, "\n")
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
			Character: column,
		},
		End: Position{
			Line:      lineIndex,
			Character: column + len(name),
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

func documentSymbolFromSymbol(sym SymbolInfo) DocumentSymbol {
	line := sym.Line - 1
	column := sym.Column - 1
	if column < 0 {
		column = 0
	}

	rng := LSPRange{
		Start: Position{
			Line:      line,
			Character: column,
		},
		End: Position{
			Line:      line,
			Character: column + len(sym.Name),
		},
	}

	children := []DocumentSymbol{}

	if sym.Kind == SymbolClass && len(sym.Methods) > 0 {
		methodNames := make([]string, 0, len(sym.Methods))
		for methodName := range sym.Methods {
			methodNames = append(methodNames, methodName)
		}
		sort.Strings(methodNames)

		for _, methodName := range methodNames {
			method := sym.Methods[methodName]
			children = append(children, documentSymbolFromSymbol(method))
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
			children = append(children, documentSymbolFromSymbol(member))
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
						"triggerCharacters": []string{"."},
					},
					"signatureHelpProvider": map[string]any{
						"triggerCharacters": []string{"(", ","},
					},
					"documentFormattingProvider": true,
					"definitionProvider":         true,
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
			Result: nil,
		})

	case "exit":
		os.Exit(0)

	case "textDocument/didOpen":
		var params DidOpenParams
		json.Unmarshal(msg.Params, &params)

		lspDocs[params.TextDocument.URI] = params.TextDocument.Text
		publishDiagnostics(params.TextDocument.URI, params.TextDocument.Text)

	case "textDocument/didChange":
		var params DidChangeParams
		json.Unmarshal(msg.Params, &params)

		if len(params.ContentChanges) > 0 {
			text := params.ContentChanges[0].Text
			lspDocs[params.TextDocument.URI] = text
			publishDiagnostics(params.TextDocument.URI, text)
		}

	case "textDocument/completion":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]
		items := getCompletions(params.TextDocument.URI, text, params.Position)

		writeLSPMessage(LSPMessage{
			ID:     msg.ID,
			Result: items,
		})

	case "textDocument/signatureHelp":
		var params CompletionParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]

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
			Result: result,
		})

	case "textDocument/definition":
		var params HoverParams
		json.Unmarshal(msg.Params, &params)

		text := lspDocs[params.TextDocument.URI]

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
			Result: result,
		})

	default:
		if msg.ID != nil {
			writeLSPMessage(LSPMessage{
				ID:     msg.ID,
				Result: nil,
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

func initLSPLogger() {
	file, err := os.OpenFile("tiny-lsp.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		lspLogFile = file
	}
}

// func lspDebug(format string, args ...any) {
// 	if lspLogFile == nil {
// 		return
// 	}

// 	fmt.Fprintf(lspLogFile, "[tiny-lsp] "+format+"\n", args...)
// 	lspLogFile.Sync()
// }

func publishDiagnostics(uri string, text string) {
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

	params, _ := json.Marshal(map[string]any{
		"uri":         uri,
		"diagnostics": diagnostics,
	})

	writeLSPMessage(LSPMessage{
		Method: "textDocument/publishDiagnostics",
		Params: params,
	})
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
				Label:  "await",
				Kind:   2,
				Detail: "task.await(): " + strings.TrimPrefix(typeName, "task:"),
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
			Label:  method.Name,
			Kind:   2,
			Detail: formatNativeSignature(typeName, method),
		})
	}

	items = append(items, CompletionItem{
		Label: "toString",
		Kind:  2,
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

func scopeCompletions(scope *Scope) []CompletionItem {
	items := []CompletionItem{
		{Label: "import", Kind: 14, Detail: "import statement"},
		{Label: "export", Kind: 14, Detail: "export statement"},
		{Label: "std", Kind: 14, Detail: "standard library import"},
		{Label: "fn", Kind: 14, Detail: "function"},
		{Label: "let", Kind: 14, Detail: "variable"},
		{Label: "const", Kind: 14, Detail: "constant"},
		{Label: "class", Kind: 7, Detail: "class"},
		{Label: "embed", Kind: 14, Detail: "embed class methods"},
		{Label: "if", Kind: 14, Detail: "if statement"},
		{Label: "else", Kind: 14, Detail: "else"},
		{Label: "while", Kind: 14, Detail: "while loop"},
		{Label: "for", Kind: 14, Detail: "for loop"},
		{Label: "return", Kind: 14, Detail: "return"},
		{Label: "spawn", Kind: 14, Detail: "spawn task"},
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
				Label:  sym.Name,
				Kind:   symbolKindToCompletionKind(sym.Kind),
				Detail: symbolDetail(sym),
			})
		}
	}

	return items
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
		return scopeCompletions(scope)
	}

	sym, ok := scope.Resolve(receiver)
	if !ok {
		return []CompletionItem{}
	}

	if sym.Kind == SymbolNamespace {
		return completionItemsFromMembers(sym.Members)
	}

	if sym.Type == "object" && sym.Fields != nil {
		return completionItemsFromMembers(sym.Fields)
	}

	if strings.HasPrefix(sym.Type, "std:") {
		module := strings.TrimPrefix(sym.Type, "std:")
		return getStdCompletions(module)
	}

	if strings.HasPrefix(sym.Type, "class:") {
		className := strings.TrimPrefix(sym.Type, "class:")

		classSym, ok := scope.Resolve(className)
		if !ok || classSym.Kind != SymbolClass {
			return []CompletionItem{}
		}

		return completionItemsFromMembers(classSym.Methods)
	}

	return getNativeTypeCompletions(sym.Type)
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
			Label:  member.Name,
			Kind:   symbolKindToCompletionKind(member.Kind),
			Detail: detail,
		})
	}

	return items
}

func symbolDetail(sym SymbolInfo) string {
	switch sym.Kind {
	case SymbolFunction:
		return formatFunctionSignature(sym.Name, sym.Params, sym.Returns)
	case SymbolClass:
		return sym.Detail
	case SymbolNamespace:
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

	if !strings.HasSuffix(text, ".") {
		return ""
	}

	text = strings.TrimSuffix(text, ".")
	text = strings.TrimRight(text, " \t")

	i := len(text) - 1

	for i >= 0 {
		ch := text[i]

		if (ch >= 'a' && ch <= 'z') ||
			(ch >= 'A' && ch <= 'Z') ||
			(ch >= '0' && ch <= '9') ||
			ch == '_' {
			i--
			continue
		}

		break
	}

	return text[i+1:]
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

func topLevelCompletions(uri string, text string) []CompletionItem {
	analysis := analyzeTiny(uri, text)

	items := []CompletionItem{
		{Label: "import", Kind: 14, Detail: "import statement"},
		{Label: "fn", Kind: 14, Detail: "function"},
		{Label: "let", Kind: 14, Detail: "variable"},
		{Label: "const", Kind: 14, Detail: "constant"},
		{Label: "class", Kind: 7, Detail: "class"},
		{Label: "if", Kind: 14, Detail: "if statement"},
		{Label: "while", Kind: 14, Detail: "while loop"},
		{Label: "return", Kind: 14, Detail: "return"},
	}

	for _, sym := range analysis.GlobalScope.Symbols {
		items = append(items, CompletionItem{
			Label:  sym.Name,
			Kind:   symbolKindToCompletionKind(sym.Kind),
			Detail: sym.Detail + " : " + sym.Type,
		})
	}

	return items
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
	default:
		return 6
	}
}
