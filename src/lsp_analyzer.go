// lsp_analyzer.go

package main

import (
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "language.com/src/vm"
)

type SymbolKind string

const (
	SymbolVariable  SymbolKind = "variable"
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolStd       SymbolKind = "std"
	SymbolNamespace SymbolKind = "namespace"
	SymbolField     SymbolKind = "field"
)

type SymbolInfo struct {
	Name      string
	Kind      SymbolKind
	Type      string
	Detail    string
	Line      int
	Column    int
	SourceURI string

	Fields  map[string]SymbolInfo
	Params  []StdArg
	Returns string
	Methods map[string]SymbolInfo
	Members map[string]SymbolInfo
}

type Scope struct {
	Symbols map[string]SymbolInfo
	Parent  *Scope
}

func NewScope(parent *Scope) *Scope {
	return &Scope{
		Symbols: map[string]SymbolInfo{},
		Parent:  parent,
	}
}

func (s *Scope) Define(sym SymbolInfo) {
	s.Symbols[sym.Name] = sym
}

func (s *Scope) Resolve(name string) (SymbolInfo, bool) {
	for scope := s; scope != nil; scope = scope.Parent {
		if sym, ok := scope.Symbols[name]; ok {
			return sym, true
		}
	}

	return SymbolInfo{}, false
}

type AnalysisResult struct {
	GlobalScope *Scope
	Imports     map[string]string
}

var typeNamePattern = `[A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*`
var unionTypePattern = typeNamePattern + `(?:\s*\|\s*` + typeNamePattern + `)*`

var variableLineRegex = regexp.MustCompile(
	`^(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*=\s*(.+?);?$`,
)
var fieldLineRegex = regexp.MustCompile(
	`^field\s+(?:(?:public|private|const)\s+)*([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*(?:=\s*(.+?))?;?$`,
)
var functionLineRegex = regexp.MustCompile(
	`^(?:(?:public|private)\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*(?::\s*(` + unionTypePattern + `))?`,
)
var classLineRegex = regexp.MustCompile(`^class\s+([A-Za-z_][A-Za-z0-9_]*)`)
var memberCallRegex = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
var normalCallRegex = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
var classEmbedRegex = regexp.MustCompile(`\bembed\s+([A-Za-z_][A-Za-z0-9_]*)\s*;?`)
var returnRegex = regexp.MustCompile(`return\s+(.+?);`)
var fileImportRegex = regexp.MustCompile(`import\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*;?`)
var exportLineRegex = regexp.MustCompile(`^\s*export\s+`)
var memberAccessRegex = regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\.([A-Za-z_][A-Za-z0-9_]*)\b`)
var catchVarRegex = regexp.MustCompile(`\bcatch\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)

type blockInfo struct {
	Kind       string
	Name       string
	ParamsText string
	ReturnType string
	Body       string
	Header     string
	Start      int
	End        int
	Line       int
	Column     int
	Exported   bool
}

func splitUnionType(typ string) []string {
	typ = strings.TrimSpace(typ)

	if typ == "" {
		return []string{}
	}

	if !strings.Contains(typ, "|") {
		return []string{typ}
	}

	parts := strings.Split(typ, "|")
	result := []string{}

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		result = append(result, part)
	}

	return result
}

func isNullishLSPType(typ string) bool {
	typ = strings.TrimSpace(typ)
	return typ == "null" || typ == "undefined"
}

func scanCatchVariables(scope *Scope, text string, pos Position, uri string) {
	lines := strings.Split(text, "\n")

	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}

	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])

		match := catchVarRegex.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		name := match[1]

		scope.Define(SymbolInfo{
			Name:      name,
			Kind:      SymbolVariable,
			Type:      "error",
			Detail:    "catch error " + name,
			Line:      lineIndex + 1,
			Column:    indexColumn(line, name),
			SourceURI: uri,
		})
	}
}

func checkUndefinedMethodOnLine(scope *Scope, rawLine string, lineIndex int) []map[string]any {
	diagnostics := []map[string]any{}
	code := stripStringsAndComments(rawLine)

	matches := memberAccessRegex.FindAllStringSubmatchIndex(code, -1)

	for _, match := range matches {
		receiver := code[match[2]:match[3]]
		member := code[match[4]:match[5]]

		// Ignore this.field for now. It needs current-class tracking.
		if receiver == "this" {
			receiverSym, ok := scope.Resolve("this")
			if !ok {
				diagnostics = append(diagnostics, makeRangeDiagnostic(
					lineIndex,
					match[2],
					match[3],
					2,
					"undefined variable: this",
				))
				continue
			}

			if memberExistsOnSymbol(scope, receiverSym, member) {
				continue
			}

			diagnostics = append(diagnostics, makeRangeDiagnostic(
				lineIndex,
				match[4],
				match[5],
				2,
				"undefined method or property: this."+member,
			))

			continue
		}

		receiverSym, ok := scope.Resolve(receiver)
		if !ok {
			continue
		}

		if !shouldCheckMemberAccess(receiverSym.Type) {
			continue
		}

		if memberExistsOnSymbol(scope, receiverSym, member) {
			continue
		}

		diagnostics = append(diagnostics, makeRangeDiagnostic(
			lineIndex,
			match[4],
			match[5],
			2,
			"undefined method or property: "+receiver+"."+member,
		))
	}

	return diagnostics
}

func resolveClassSymbol(scope *Scope, className string) (SymbolInfo, bool) {
	if sym, ok := scope.Resolve(className); ok && sym.Kind == SymbolClass {
		return sym, true
	}

	if strings.Contains(className, ".") {
		parts := strings.SplitN(className, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			member, ok := ns.Members[memberName]
			if ok && member.Kind == SymbolClass {
				return member, true
			}
		}
	}

	return SymbolInfo{}, false
}

func memberExistsOnSymbol(scope *Scope, sym SymbolInfo, member string) bool {
	if strings.HasPrefix(sym.Type, "task:") {
		return member == "await"
	}

	if sym.Kind == SymbolNamespace {
		_, ok := sym.Members[member]
		return ok
	}

	if strings.HasPrefix(sym.Type, "std:") {
		module := strings.TrimPrefix(sym.Type, "std:")

		info, ok := GetStdModuleInfo(module)
		if !ok {
			return false
		}

		_, ok = info.Methods[member]
		return ok
	}

	if strings.HasPrefix(sym.Type, "class:") {
		className := strings.TrimPrefix(sym.Type, "class:")

		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			return false
		}

		if _, ok := classSym.Methods[member]; ok {
			return true
		}

		if _, ok := classSym.Fields[member]; ok {
			return true
		}

		return false
	}

	if sym.Type == "object" && sym.Fields != nil {
		if _, ok := sym.Fields[member]; ok {
			return true
		}
	}

	if _, ok := GetNativeMethodInfo(sym.Type, member); ok {
		return true
	}

	// Global fallback.
	if member == "toString" {
		return true
	}

	return false
}

var identifierRegex = regexp.MustCompile(`\b[A-Za-z_][A-Za-z0-9_]*\b`)

func checkUndefinedVariablesOnLine(scope *Scope, rawLine string, lineIndex int) []map[string]any {
	diagnostics := []map[string]any{}
	code := stripStringsAndComments(rawLine)

	matches := identifierRegex.FindAllStringIndex(code, -1)

	for _, match := range matches {
		name := code[match[0]:match[1]]

		if shouldIgnoreIdentifierInSemanticCheck(code, name, match[0], match[1]) {
			continue
		}

		if _, ok := scope.Resolve(name); ok {
			continue
		}

		diagnostics = append(diagnostics, makeRangeDiagnostic(
			lineIndex,
			match[0],
			match[1],
			2,
			"undefined variable: "+name,
		))
	}

	return diagnostics
}

func shouldCheckMemberAccess(receiverType string) bool {
	if receiverType == "" {
		return false
	}

	if receiverType == "any" || receiverType == "unknown" || receiverType == "undefined" || receiverType == "null" {
		return false
	}

	if strings.Contains(receiverType, "|") {
		parts := strings.Split(receiverType, "|")

		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "any" || part == "unknown" || part == "null" || part == "undefined" {
				return false
			}
		}
	}

	return true
}

func shouldIgnoreIdentifierInSemanticCheck(code string, name string, start int, end int) bool {
	if tinyKeywords[name] || name == "_" {
		return true
	}

	switch name {
	case "true", "false", "null", "undefined":
		return true
	case "string", "number", "bool", "object", "array", "any", "void":
		return true
	}

	trimmed := strings.TrimSpace(code)

	// declarations
	declLine := strings.TrimSpace(trimmed)

	if strings.HasPrefix(declLine, "private ") {
		declLine = strings.TrimSpace(strings.TrimPrefix(declLine, "private "))
	}

	if strings.HasPrefix(declLine, "public ") {
		declLine = strings.TrimSpace(strings.TrimPrefix(declLine, "public "))
	}

	if strings.HasPrefix(declLine, "let "+name) ||
		strings.HasPrefix(declLine, "const "+name) ||
		strings.HasPrefix(declLine, "fn "+name) ||
		strings.HasPrefix(declLine, "class "+name) ||
		strings.HasPrefix(declLine, "field "+name) {
		return true
	}
	// Ignore property/member names after dot: obj.name
	if start > 0 && code[start-1] == '.' {
		return true
	}

	// Ignore receiver member access member name with spaces: obj . name
	i := start - 1
	for i >= 0 && (code[i] == ' ' || code[i] == '\t') {
		i--
	}
	if i >= 0 && code[i] == '.' {
		return true
	}

	// Ignore object literal keys: { name: "confis" }
	j := end
	for j < len(code) && (code[j] == ' ' || code[j] == '\t') {
		j++
	}
	if j < len(code) && code[j] == ':' {
		return true
	}

	// Ignore type hints: name: string
	i = start - 1
	for i >= 0 && (code[i] == ' ' || code[i] == '\t') {
		i--
	}
	if i >= 0 && code[i] == ':' {
		return true
	}

	return false
}

func makeRangeDiagnostic(line int, start int, end int, severity int, message string) map[string]any {
	if start < 0 {
		start = 0
	}

	if end <= start {
		end = start + 1
	}

	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{
				"line":      line,
				"character": start,
			},
			"end": map[string]any{
				"line":      line,
				"character": end,
			},
		},
		"severity": severity,
		"message":  message,
		"source":   "tiny",
	}
}

func stripStringsAndComments(line string) string {
	var out strings.Builder

	inString := byte(0)
	escaped := false

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if inString != 0 {
			if escaped {
				escaped = false
				out.WriteByte(' ')
				continue
			}

			if ch == '\\' {
				escaped = true
				out.WriteByte(' ')
				continue
			}

			if ch == inString {
				inString = 0
				out.WriteByte(' ')
				continue
			}

			out.WriteByte(' ')
			continue
		}

		if i+1 < len(line) && ch == '/' && line[i+1] == '/' {
			for ; i < len(line); i++ {
				out.WriteByte(' ')
			}
			break
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			out.WriteByte(' ')
			continue
		}

		out.WriteByte(ch)
	}

	return out.String()
}

func uriToPath(uri string) string {
	parsed, err := url.Parse(uri)
	if err != nil {
		return uri
	}

	if parsed.Scheme != "file" {
		return uri
	}

	path := parsed.Path

	if len(path) >= 3 && path[0] == '/' && path[2] == ':' {
		path = path[1:]
	}

	return filepath.FromSlash(path)
}

func pathToFileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}

	return "file:///" + filepath.ToSlash(path)
}

func resolveImportPath(currentURI string, importPath string) string {
	currentPath := uriToPath(currentURI)
	baseDir := filepath.Dir(currentPath)

	if filepath.IsAbs(importPath) {
		return importPath
	}

	return filepath.Join(baseDir, importPath)
}

func scopeAtPosition(uri string, text string, pos Position) *Scope {
	return fallbackScopeAtPosition(uri, text, pos)
}

func fallbackScopeAtPosition(uri string, text string, pos Position) *Scope {
	scope := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		scope.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolStd,
			Type:      "std:" + module,
			Detail:    "std module " + module,
			Line:      1,
			Column:    1,
			SourceURI: uri,
		})
	}

	scanFileImportsIntoScope(scope, uri, text)

	lines := strings.Split(text, "\n")
	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}
	if maxLine < 0 {
		maxLine = 0
	}

	// Pass 1: cheap one-line functions/classes so constructors/calls are known early.
	// Scan class names, but don't leak class methods as global functions.
	classBlocks := findBlocks(text, "class")

	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])
		if line == "" {
			continue
		}

		// Always scan classes.
		scanClassLine(scope, line, lineIndex+1, uri)

		lineOffset := offsetAtLine(text, lineIndex+1)
		insideClass := blockInsideAny(lineOffset, classBlocks)

		// Only scan functions if this line is NOT inside a class.
		if !insideClass {
			scanFunctionLine(scope, line, lineIndex+1, uri)
		}
	}

	// Pass 2: full blocks. This overwrites cheap symbols with params/methods/return types.
	scanFullClasses(scope, text, maxLine, uri)
	scanFullFunctions(scope, text, maxLine, uri)
	scanAnonymousFunctions(scope, text, maxLine, uri)
	scanInlineAnonymousFunctionParams(scope, text, pos, uri)
	scanCatchVariables(scope, text, pos, uri)

	// Pass 3: variables after functions/classes/imports are known.
	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])
		if line == "" {
			continue
		}

		scanVariableLine(scope, line, lineIndex+1, uri)
		scanFieldLine(scope, line, lineIndex+1, uri)
	}

	// Add parameters from the function/method/anonymous function that contains the cursor.
	// This makes params autocomplete inside function bodies.
	currentFunction := functionBlockAtLine(text, pos.Line)
	if currentFunction != nil {
		for _, param := range parseFunctionParams(currentFunction.ParamsText) {
			scope.Define(SymbolInfo{
				Name:      param.Name,
				Kind:      SymbolVariable,
				Type:      normalizeLSPType(scope, param.Type),
				Detail:    "parameter " + param.Name,
				Line:      currentFunction.Line,
				Column:    1,
				SourceURI: uri,
			})
		}
	}

	return scope
}

func cleanLine(line string) string {
	return strings.TrimSpace(strings.TrimSuffix(line, "\r"))
}

func scanFunctionLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := functionLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]
	paramsText := match[2]
	returnType := "any"

	if len(match) > 3 && match[3] != "" {
		returnType = normalizeLSPType(scope, match[3])
	}

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolFunction,
		Type:      "function",
		Detail:    "fn " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
		Params:    normalizeStdArgs(scope, parseFunctionParams(paramsText)),
		Returns:   returnType,
	})
}

func scanClassLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := classLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolClass,
		Type:      "class:" + name,
		Detail:    "class " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
		Methods:   map[string]SymbolInfo{},
	})
}

func scanFieldLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.Replace(strings.Replace(strings.Replace(line, "private ", "", 1), "public ", "", 1), "const ", "", 1)
	match := fieldLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]
	typeHint := match[2]
	exprText := strings.TrimSpace(match[3])

	typ := "unknown"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		typ = inferExprTypeFromText(scope, exprText)
		if typ == "object" {
			fields = inferObjectFieldsFromText(scope, exprText, uri, lineNumber)
		}
	}

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolVariable,
		Type:      typ,
		Detail:    "field " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
		Fields:    fields,
	})
}

func scanVariableLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := variableLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]
	typeHint := match[2]
	exprText := strings.TrimSpace(match[3])

	typ := "unknown"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		typ = inferExprTypeFromText(scope, exprText)
		if typ == "object" {
			fields = inferObjectFieldsFromText(scope, exprText, uri, lineNumber)
		}
	}

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolVariable,
		Type:      typ,
		Detail:    "variable " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
		Fields:    fields,
	})
}

func scanFullFunctions(scope *Scope, text string, maxLine int, uri string) {
	classBlocks := findBlocks(text, "class")

	for _, block := range findBlocks(text, "fn") {
		if block.Line > maxLine+1 {
			continue
		}

		if blockInsideAny(block.Start, classBlocks) {
			continue
		}

		params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
		returnType := inferReturnTypeFromBody(scope, block.Body, block.ReturnType)

		scope.Define(SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolFunction,
			Type:      "function",
			Detail:    "fn " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Params:    params,
			Returns:   returnType,
		})
	}
}

func scanClassFields(scope *Scope, classBody string, uri string, baseLine int) map[string]SymbolInfo {
	fields := map[string]SymbolInfo{}
	lines := strings.Split(classBody, "\n")

	for i, raw := range lines {
		line := cleanLine(raw)

		if !strings.HasPrefix(line, "field ") {
			continue
		}

		line = strings.TrimSpace(strings.TrimPrefix(line, "field "))

		isPrivate := false
		isConst := false

		// Remove modifiers after "field".
		// Keep looping so this works:
		// field private const name = "x";
		// field const private name = "x";
		for {
			if strings.HasPrefix(line, "public ") {
				line = strings.TrimSpace(strings.TrimPrefix(line, "public "))
				continue
			}

			if strings.HasPrefix(line, "private ") {
				isPrivate = true
				line = strings.TrimSpace(strings.TrimPrefix(line, "private "))
				continue
			}

			if strings.HasPrefix(line, "const ") {
				isConst = true
				line = strings.TrimSpace(strings.TrimPrefix(line, "const "))
				continue
			}

			break
		}

		// Support fields without default:
		// field name: string;
		// Turn it into a fake assignment for variableLineRegex.
		fakeLine := "let " + line
		if !strings.Contains(fakeLine, "=") {
			fakeLine = strings.TrimSuffix(fakeLine, ";") + " = undefined;"
		}

		match := variableLineRegex.FindStringSubmatch(fakeLine)
		if match == nil {
			continue
		}

		name := match[1]
		typeHint := match[2]
		expr := strings.TrimSpace(match[3])

		typ := "unknown"
		if typeHint != "" {
			typ = normalizeLSPType(scope, typeHint)
		} else {
			typ = inferExprTypeFromText(scope, expr)
		}

		detail := "field " + name
		if isPrivate {
			detail = "private " + detail
		}
		if isConst {
			detail = "const " + detail
		}

		fields[name] = SymbolInfo{
			Name:      name,
			Kind:      SymbolField,
			Type:      typ,
			Detail:    detail,
			Line:      baseLine + i,
			Column:    indexColumn(raw, name),
			SourceURI: uri,
		}
	}

	return fields
}

func isPrivateMethodBlock(header string) bool {
	return strings.Contains(header, "private fn ")
}

func scanFullClasses(scope *Scope, text string, maxLine int, uri string) {
	// Two passes let embed work even if classes appear later.
	classBlocks := findBlocks(text, "class")

	for _, block := range classBlocks {
		if block.Line > maxLine+1 {
			continue
		}

		existing, _ := scope.Resolve(block.Name)
		existing.Name = block.Name
		existing.Kind = SymbolClass
		existing.Type = "class:" + block.Name
		existing.Detail = "class " + block.Name
		existing.Line = block.Line
		existing.Column = block.Column
		existing.SourceURI = uri
		if existing.Methods == nil {
			existing.Methods = map[string]SymbolInfo{}
		}
		scope.Define(existing)
	}

	for _, block := range classBlocks {
		if block.Line > maxLine+1 {
			continue
		}

		methods := map[string]SymbolInfo{}

		collectEmbeddedMethods(scope, block.Body, methods)

		for _, methodBlock := range findBlocks(block.Body, "fn") {
			params := normalizeStdArgs(scope, parseFunctionParams(methodBlock.ParamsText))
			returnType := inferReturnTypeFromBody(scope, methodBlock.Body, methodBlock.ReturnType)

			detail := "method " + block.Name + "." + methodBlock.Name

			if isPrivateFunctionAt(block.Body, methodBlock.Start) {
				detail = "private " + detail
			}

			methods[methodBlock.Name] = SymbolInfo{
				Name:      methodBlock.Name,
				Kind:      SymbolFunction,
				Type:      "function",
				Detail:    detail,
				Line:      block.Line + methodBlock.Line - 1,
				Column:    methodBlock.Column,
				SourceURI: uri,
				Params:    params,
				Returns:   returnType,
			}
		}

		fields := scanClassFields(scope, block.Body, uri, block.Line)

		scope.Define(SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolClass,
			Type:      "class:" + block.Name,
			Detail:    "class " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Methods:   methods,
			Fields:    fields,
		})
	}
}

func blockInsideAny(offset int, blocks []blockInfo) bool {
	for _, block := range blocks {
		if offset >= block.Start && offset < block.End {
			return true
		}
	}
	return false
}

func collectEmbeddedMethods(scope *Scope, classBody string, methods map[string]SymbolInfo) {
	matches := classEmbedRegex.FindAllStringSubmatch(classBody, -1)

	for _, match := range matches {
		embeddedClassName := match[1]

		embeddedSym, ok := scope.Resolve(embeddedClassName)
		if !ok || embeddedSym.Kind != SymbolClass {
			continue
		}

		for methodName, method := range embeddedSym.Methods {
			if _, exists := methods[methodName]; exists {
				continue
			}
			methods[methodName] = method
		}
	}
}

func scanAnonymousFunctions(scope *Scope, text string, maxLine int, uri string) {
	lines := strings.Split(text, "\n")

	for i := 0; i <= maxLine && i < len(lines); i++ {
		line := cleanLine(lines[i])

		if !strings.Contains(line, "= spawn fn") && !strings.Contains(line, "= fn") {
			continue
		}

		name := variableNameFromLine(line)
		if name == "" {
			continue
		}

		absoluteOffset := offsetAtLine(text, i+1) + strings.Index(lines[i], name)
		fnIndex := strings.Index(text[absoluteOffset:], "fn")
		if fnIndex < 0 {
			continue
		}

		fnOffset := absoluteOffset + fnIndex
		block, ok := parseFunctionLikeBlockAt(text, fnOffset, "fn")
		if !ok {
			continue
		}

		returnType := inferReturnTypeFromBody(scope, block.Body, block.ReturnType)
		params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))

		if strings.Contains(line, "= spawn fn") {
			scope.Define(SymbolInfo{
				Name:      name,
				Kind:      SymbolVariable,
				Type:      "task:" + returnType,
				Detail:    "task " + name,
				Line:      i + 1,
				Column:    indexColumn(line, name),
				SourceURI: uri,
				Params:    params,
				Returns:   returnType,
			})
			continue
		}

		scope.Define(SymbolInfo{
			Name:      name,
			Kind:      SymbolFunction,
			Type:      "function",
			Detail:    "anonymous function " + name,
			Line:      i + 1,
			Column:    indexColumn(line, name),
			SourceURI: uri,
			Params:    params,
			Returns:   returnType,
		})
	}
}

func variableNameFromLine(line string) string {
	match := variableLineRegex.FindStringSubmatch(line)
	if match == nil {
		return ""
	}
	return match[1]
}

func findBlocks(text string, kind string) []blockInfo {
	blocks := []blockInfo{}

	offset := 0
	for {
		idx := strings.Index(text[offset:], kind)
		if idx < 0 {
			break
		}

		start := offset + idx

		if !isWordBoundaryAt(text, start, len(kind)) {
			offset = start + len(kind)
			continue
		}

		block, ok := parseFunctionLikeBlockAt(text, start, kind)
		if ok {
			blocks = append(blocks, block)
			offset = block.End
			continue
		}

		offset = start + len(kind)
	}

	return blocks
}

func parseFunctionLikeBlockAt(text string, start int, kind string) (blockInfo, bool) {
	i := start + len(kind)

	if !isSpaceAroundKeyword(text, start, kind) {
		return blockInfo{}, false
	}

	i = skipSpaces(text, i)

	nameStart := i
	for i < len(text) && isIdentByte(text[i]) {
		i++
	}

	if nameStart == i && kind != "fn" {
		return blockInfo{}, false
	}

	name := text[nameStart:i]

	if kind == "fn" {
		// Anonymous fn has no name.
		if name == "" || (i < len(text) && text[i] == '(') {
			if name == "" {
				name = ""
			}
		}
	}

	i = skipSpaces(text, i)

	paramsText := ""
	returnType := ""

	if kind == "fn" {
		if i >= len(text) || text[i] != '(' {
			return blockInfo{}, false
		}

		closeParen := findMatching(text, i, '(', ')')
		if closeParen < 0 {
			return blockInfo{}, false
		}

		paramsText = text[i+1 : closeParen]
		i = closeParen + 1
		i = skipSpaces(text, i)

		if i < len(text) && text[i] == ':' {
			i++
			i = skipSpaces(text, i)

			retStart := i
			for i < len(text) {
				ch := text[i]
				if isIdentByte(ch) || ch == '.' || ch == '|' || ch == ' ' || ch == '\t' {
					i++
					continue
				}
				break
			}

			returnType = strings.TrimSpace(text[retStart:i])
			i = skipSpaces(text, i)
		}
	}

	if i >= len(text) || text[i] != '{' {
		return blockInfo{}, false
	}

	closeBrace := findMatching(text, i, '{', '}')
	if closeBrace < 0 {
		return blockInfo{}, false
	}

	line := lineNumberAtOffset(text, start)
	column := findColumnAtLine(text, firstNonEmpty(name, kind), line)

	return blockInfo{
		Kind:       kind,
		Name:       name,
		ParamsText: paramsText,
		ReturnType: returnType,
		Body:       text[i+1 : closeBrace],
		Start:      start,
		End:        closeBrace + 1,
		Line:       line,
		Column:     column,
	}, true
}

func isSpaceAroundKeyword(text string, start int, kind string) bool {
	if start > 0 && isIdentByte(text[start-1]) {
		return false
	}

	end := start + len(kind)
	if end < len(text) && isIdentByte(text[end]) {
		return false
	}

	return true
}

func isWordBoundaryAt(text string, start int, length int) bool {
	if start > 0 && isIdentByte(text[start-1]) {
		return false
	}

	end := start + length
	if end < len(text) && isIdentByte(text[end]) {
		return false
	}

	return true
}

func skipSpaces(text string, i int) int {
	for i < len(text) && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i++
	}
	return i
}

func findMatching(text string, openIndex int, open byte, close byte) int {
	depth := 0
	inString := byte(0)
	escaped := false

	for i := openIndex; i < len(text); i++ {
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

		if ch == open {
			depth++
			continue
		}

		if ch == close {
			depth--
			if depth == 0 {
				return i
			}
		}
	}

	return -1
}

func inferReturnTypeFromBody(scope *Scope, body string, explicitReturn string) string {
	if explicitReturn != "" {
		return normalizeLSPType(scope, explicitReturn)
	}

	matches := returnRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return "undefined"
	}

	for _, match := range matches {
		expr := strings.TrimSpace(match[1])
		if expr == "" {
			continue
		}

		typ := inferExprTypeFromText(scope, expr)
		if typ != "unknown" && typ != "any" {
			return typ
		}
	}

	expr := strings.TrimSpace(matches[0][1])
	return inferExprTypeFromText(scope, expr)
}

func inferExprTypeFromText(scope *Scope, expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimSuffix(expr, ";")

	if expr == "" {
		return "unknown"
	}

	if strings.HasPrefix(expr, `"`) || strings.HasPrefix(expr, "`") || strings.HasPrefix(expr, "'") {
		return "string"
	}

	if strings.HasPrefix(expr, "[") {
		return "array"
	}

	if strings.HasPrefix(expr, "{") {
		return "object"
	}

	if expr == "true" || expr == "false" {
		return "bool"
	}

	if expr == "null" {
		return "null"
	}

	if expr == "undefined" {
		return "undefined"
	}

	if strings.HasPrefix(expr, "spawn fn") {
		block, ok := parseFunctionLikeBlockAt(expr, strings.Index(expr, "fn"), "fn")
		if ok {
			return "task:" + inferReturnTypeFromBody(scope, block.Body, block.ReturnType)
		}
		return "task:any"
	}

	if strings.HasPrefix(expr, "fn") {
		return "function"
	}

	if isNumberText(expr) {
		return "number"
	}

	if typ := inferTernaryTypeFromText(scope, expr); typ != "" {
		return typ
	}

	if isComparisonExprText(expr) {
		return "bool"
	}

	if typ := inferMemberCallTypeFromText(scope, expr); typ != "" {
		return typ
	}

	if typ := inferNormalCallTypeFromText(scope, expr); typ != "" {
		return typ
	}

	if sym, ok := scope.Resolve(expr); ok {
		return sym.Type
	}

	return "unknown"
}

func isComparisonExprText(expr string) bool {
	ops := []string{
		"==", "!=", "<=", ">=", "<", ">",
		" instanceof ",
		" in ",
		" and ",
		" or ",
	}

	for _, op := range ops {
		if strings.Contains(expr, op) {
			return true
		}
	}

	return false
}

func inferMemberCallTypeFromText(scope *Scope, expr string) string {
	match := memberCallRegex.FindStringSubmatch(expr)
	if match == nil {
		return ""
	}

	receiver := match[1]
	method := match[2]

	sym, ok := scope.Resolve(receiver)
	if !ok {
		return ""
	}

	if strings.HasPrefix(sym.Type, "task:") {
		if method == "await" {
			return strings.TrimPrefix(sym.Type, "task:")
		}
		return ""
	}

	if sym.Kind == SymbolNamespace {
		member, ok := sym.Members[method]
		if !ok {
			return ""
		}

		if member.Kind == SymbolFunction {
			if member.Returns == "" {
				return "any"
			}
			return member.Returns
		}

		if member.Kind == SymbolClass {
			return "class:" + receiver + "." + member.Name
		}

		return member.Type
	}

	if strings.HasPrefix(sym.Type, "class:") {
		className := strings.TrimPrefix(sym.Type, "class:")

		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			return ""
		}

		methodSym, ok := classSym.Methods[method]
		if !ok {
			return ""
		}

		if methodSym.Returns == "" {
			return "any"
		}

		return methodSym.Returns
	}

	if strings.HasPrefix(sym.Type, "std:") {
		module := strings.TrimPrefix(sym.Type, "std:")

		info, ok := GetStdModuleInfo(module)
		if !ok {
			return ""
		}

		methodInfo, ok := info.Methods[method]
		if !ok {
			return ""
		}

		return methodInfo.Returns
	}

	methodInfo, ok := GetNativeMethodInfo(sym.Type, method)
	if ok {
		return methodInfo.Returns
	}

	if sym.Type == "object" && sym.Fields != nil {
		if field, ok := sym.Fields[method]; ok {
			return field.Type
		}
	}

	return ""
}

func inferNormalCallTypeFromText(scope *Scope, expr string) string {
	match := normalCallRegex.FindStringSubmatch(expr)
	if match == nil {
		return ""
	}

	name := match[1]

	sym, ok := scope.Resolve(name)
	if !ok {
		return ""
	}

	if sym.Kind == SymbolClass {
		return "class:" + sym.Name
	}

	if sym.Kind == SymbolFunction {
		if sym.Returns == "" {
			return "any"
		}
		return sym.Returns
	}

	return ""
}

func inferObjectFieldsFromText(scope *Scope, expr string, uri string, lineNumber int) map[string]SymbolInfo {
	expr = strings.TrimSpace(expr)
	if !strings.HasPrefix(expr, "{") {
		return nil
	}

	end := strings.LastIndex(expr, "}")
	if end < 0 {
		return nil
	}

	body := strings.TrimSpace(expr[1:end])
	fields := map[string]SymbolInfo{}

	parts := splitTopLevel(body, ',')
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || !strings.Contains(part, ":") {
			continue
		}

		pair := splitTopLevel(part, ':')
		if len(pair) < 2 {
			continue
		}

		name := strings.TrimSpace(pair[0])
		name = strings.Trim(name, `"'`+"`")
		if name == "" {
			continue
		}

		value := strings.TrimSpace(strings.Join(pair[1:], ":"))
		typ := inferExprTypeFromText(scope, value)

		fields[name] = SymbolInfo{
			Name:      name,
			Kind:      SymbolField,
			Type:      typ,
			Detail:    "field " + name,
			Line:      lineNumber,
			Column:    1,
			SourceURI: uri,
		}
	}

	return fields
}

func splitTopLevel(text string, delimiter byte) []string {
	parts := []string{}
	start := 0
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
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

		switch ch {
		case '"', '\'', '`':
			inString = ch
		case '(':
			depthParen++
		case ')':
			depthParen--
		case '[':
			depthBracket++
		case ']':
			depthBracket--
		case '{':
			depthBrace++
		case '}':
			depthBrace--
		default:
			if ch == delimiter && depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
				parts = append(parts, text[start:i])
				start = i + 1
			}
		}
	}

	parts = append(parts, text[start:])
	return parts
}

func parseFunctionParams(paramsText string) []StdArg {
	params := []StdArg{}
	rawParams := splitTopLevel(paramsText, ',')

	for _, raw := range rawParams {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		if strings.Contains(raw, "=") {
			raw = strings.TrimSpace(strings.SplitN(raw, "=", 2)[0])
		}

		isVariadic := false
		if strings.HasPrefix(raw, "...") {
			isVariadic = true
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "..."))
		}

		name := raw
		typ := "any"

		if strings.Contains(raw, ":") {
			parts := strings.SplitN(raw, ":", 2)
			name = strings.TrimSpace(parts[0])
			typ = strings.TrimSpace(parts[1])
		}

		if isVariadic {
			typ = "array"
		}

		params = append(params, StdArg{
			Name:     name,
			Type:     typ,
			Optional: false,
		})
	}

	return params
}

func normalizeStdArgs(scope *Scope, params []StdArg) []StdArg {
	out := make([]StdArg, len(params))
	for i, p := range params {
		out[i] = p
		out[i].Type = normalizeLSPType(scope, p.Type)
	}
	return out
}

func loadTinyFileExports(path string, visited map[string]bool) map[string]SymbolInfo {
	exports := map[string]SymbolInfo{}

	abs, err := filepath.Abs(path)
	if err != nil {
		return exports
	}

	if visited[abs] {
		return exports
	}
	visited[abs] = true

	bytes, err := os.ReadFile(abs)
	if err != nil {
		return exports
	}

	text := string(bytes)
	uri := pathToFileURI(abs)

	scope := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		scope.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolStd,
			Type:      "std:" + module,
			Detail:    "std module " + module,
			SourceURI: uri,
		})
	}

	scanFileImportsIntoScopeWithVisited(scope, uri, text, visited)

	// First load all exported classes/functions, then exported vars.
	scanExportedClasses(scope, text, exports, uri)
	scanExportedFunctions(scope, text, exports, uri)

	for _, sym := range exports {
		scope.Define(sym)
	}

	scanExportedVariables(scope, text, exports, uri)

	return exports
}

func scanFileImportsIntoScope(scope *Scope, currentURI string, text string) {
	scanFileImportsIntoScopeWithVisited(scope, currentURI, text, map[string]bool{})
}

func scanFileImportsIntoScopeWithVisited(scope *Scope, currentURI string, text string, visited map[string]bool) {
	matches := fileImportRegex.FindAllStringSubmatch(text, -1)

	for _, match := range matches {
		importPath := match[1]
		alias := ""

		if len(match) > 2 {
			alias = match[2]
		}

		resolved := resolveImportPath(currentURI, importPath)
		exports := loadTinyFileExports(resolved, visited)

		if alias != "" {
			scope.Define(SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				Detail:    "import " + importPath,
				Members:   exports,
				SourceURI: pathToFileURI(resolved),
			})
			continue
		}

		for name, sym := range exports {
			sym.Name = name
			scope.Define(sym)
		}
	}
}

func scanExportedFunctions(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	for _, block := range findBlocks(text, "fn") {
		if !hasExportBefore(text, block.Start) {
			continue
		}

		params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
		returnType := inferReturnTypeFromBody(scope, block.Body, block.ReturnType)

		sym := SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolFunction,
			Type:      "function",
			Detail:    "export fn " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Params:    params,
			Returns:   returnType,
		}

		exports[block.Name] = sym
		scope.Define(sym)
	}
}

func scanExportedClasses(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	for _, block := range findBlocks(text, "class") {
		if !hasExportBefore(text, block.Start) {
			continue
		}

		methods := map[string]SymbolInfo{}
		collectEmbeddedMethods(scope, block.Body, methods)

		for _, methodBlock := range findBlocks(block.Body, "fn") {
			params := normalizeStdArgs(scope, parseFunctionParams(methodBlock.ParamsText))
			returnType := inferReturnTypeFromBody(scope, methodBlock.Body, methodBlock.ReturnType)

			methods[methodBlock.Name] = SymbolInfo{
				Name:      methodBlock.Name,
				Kind:      SymbolFunction,
				Type:      "function",
				Detail:    "method " + block.Name + "." + methodBlock.Name,
				Line:      block.Line + methodBlock.Line - 1,
				Column:    methodBlock.Column,
				SourceURI: uri,
				Params:    params,
				Returns:   returnType,
			}
		}

		sym := SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolClass,
			Type:      "class:" + block.Name,
			Detail:    "export class " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Methods:   methods,
		}

		exports[block.Name] = sym
		scope.Define(sym)
	}
}

func scanExportedVariables(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	lines := strings.Split(text, "\n")

	for i, rawLine := range lines {
		line := cleanLine(rawLine)
		if !strings.HasPrefix(line, "export ") {
			continue
		}

		withoutExport := strings.TrimSpace(strings.TrimPrefix(line, "export "))
		match := variableLineRegex.FindStringSubmatch(withoutExport)
		if match == nil {
			continue
		}

		name := match[1]
		typeHint := match[2]
		expr := strings.TrimSpace(match[3])

		typ := "unknown"
		fields := map[string]SymbolInfo(nil)

		if typeHint != "" {
			typ = normalizeLSPType(scope, typeHint)
		} else {
			typ = inferExprTypeFromText(scope, expr)
			if typ == "object" {
				fields = inferObjectFieldsFromText(scope, expr, uri, i+1)
			}
		}

		sym := SymbolInfo{
			Name:      name,
			Kind:      SymbolVariable,
			Type:      typ,
			Detail:    "export variable " + name,
			Line:      i + 1,
			Column:    indexColumn(line, name),
			SourceURI: uri,
			Fields:    fields,
		}

		exports[name] = sym
		scope.Define(sym)
	}
}

func hasExportBefore(text string, start int) bool {
	lineStart := strings.LastIndex(text[:start], "\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}

	prefix := strings.TrimSpace(text[lineStart:start])
	return prefix == "export"
}

func lineNumberAtOffset(text string, offset int) int {
	line := 1
	for i := 0; i < offset && i < len(text); i++ {
		if text[i] == '\n' {
			line++
		}
	}
	return line
}

func offsetAtLine(text string, lineNumber int) int {
	if lineNumber <= 1 {
		return 0
	}

	line := 1
	for i := 0; i < len(text); i++ {
		if line == lineNumber {
			return i
		}
		if text[i] == '\n' {
			line++
		}
	}

	return len(text)
}

func findColumnAtLine(text string, word string, lineNumber int) int {
	lines := strings.Split(text, "\n")
	if lineNumber <= 0 || lineNumber > len(lines) {
		return 1
	}

	line := lines[lineNumber-1]
	return indexColumn(line, word)
}

func indexColumn(line string, word string) int {
	column := strings.Index(line, word)
	if column < 0 {
		return 1
	}
	return column + 1
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func isNumberText(text string) bool {
	if text == "" {
		return false
	}

	for i, ch := range text {
		if ch >= '0' && ch <= '9' {
			continue
		}
		if ch == '.' {
			continue
		}
		if ch == '-' && i == 0 {
			continue
		}
		return false
	}

	return true
}

func isIdentByte(ch byte) bool {
	return (ch >= 'a' && ch <= 'z') ||
		(ch >= 'A' && ch <= 'Z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

func wordAtPosition(text string, pos Position) string {
	line := getLine(text, pos.Line)

	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	start := pos.Character
	end := pos.Character

	for start > 0 && isIdentChar(line[start-1]) {
		start--
	}

	for end < len(line) && isIdentChar(line[end]) {
		end++
	}

	return line[start:end]
}

func memberExprAtPosition(text string, pos Position) (string, string, bool) {
	line := getLine(text, pos.Line)

	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	method := wordAtPosition(text, pos)
	if method == "" {
		return "", "", false
	}

	methodStart := pos.Character
	for methodStart > 0 && isIdentChar(line[methodStart-1]) {
		methodStart--
	}

	i := methodStart - 1
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}

	if i < 0 || line[i] != '.' {
		return "", "", false
	}

	i--
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}

	end := i + 1

	for i >= 0 && isIdentChar(line[i]) {
		i--
	}

	receiver := line[i+1 : end]
	if receiver == "" {
		return "", "", false
	}

	return receiver, method, true
}

func isIdentChar(ch byte) bool {
	return isIdentByte(ch)
}

// Parser-backed analysis is still kept for diagnostics/future upgrades, but editor scope uses fallback scanning.
func analyzeTiny(uri string, text string) AnalysisResult {
	stmts, diagnostics := parseTinyForLSP(uri, text)

	global := NewScope(nil)

	result := AnalysisResult{
		GlobalScope: global,
		Imports:     parseStdImports(text),
	}

	if len(diagnostics) > 0 {
		for alias, module := range result.Imports {
			global.Define(SymbolInfo{
				Name:      alias,
				Kind:      SymbolStd,
				Type:      "std:" + module,
				Detail:    "std module " + module,
				SourceURI: uri,
			})
		}
		return result
	}

	for _, stmt := range stmts {
		analyzeTopLevelStmt(result, global, stmt)
	}

	return result
}

func analyzeTopLevelStmt(result AnalysisResult, scope *Scope, stmt Stmt) {
	switch s := stmt.(type) {
	case ImportStmt:
		if s.Std {
			alias := s.Path
			if s.Alias != "" {
				alias = s.Alias
			}

			scope.Define(SymbolInfo{
				Name:   alias,
				Kind:   SymbolStd,
				Type:   "std:" + s.Path,
				Detail: "std module " + s.Path,
			})
		}

	case FunctionStmt:
		scope.Define(SymbolInfo{
			Name:    s.Name,
			Kind:    SymbolFunction,
			Type:    "function",
			Detail:  "fn " + s.Name,
			Params:  paramsFromAST(scope, s.Params),
			Returns: normalizeLSPType(scope, typeHintName(s.ReturnType, "any")),
		})

	case ClassStmt:
		scope.Define(SymbolInfo{
			Name:    s.Name,
			Kind:    SymbolClass,
			Type:    "class:" + s.Name,
			Detail:  "class " + s.Name,
			Methods: map[string]SymbolInfo{},
		})

	case VariableStmt:
		scope.Define(SymbolInfo{
			Name:   s.Name,
			Kind:   SymbolVariable,
			Type:   inferExprType(scope, s.Value),
			Detail: "variable " + s.Name,
		})

	case FieldStmt:
		scope.Define(SymbolInfo{
			Name:   s.Name,
			Kind:   SymbolVariable,
			Type:   inferExprType(scope, s.Value),
			Detail: "field " + s.Name,
		})
	}
}

func paramsFromAST(scope *Scope, params []Param) []StdArg {
	args := []StdArg{}

	for _, param := range params {
		typ := typeHintName(param.TypeHint, "any")

		if param.Variadic {
			typ = "array"
		} else {
			typ = normalizeLSPType(scope, typ)
		}

		args = append(args, StdArg{
			Name:     param.Name,
			Type:     typ,
			Optional: param.HasDefault,
		})
	}

	return args
}

func typeHintName(hint TypeHint, fallback string) string {
	if hint.IsEmpty() {
		return fallback
	}
	return hint.Name
}

func inferTernaryTypeFromText(scope *Scope, expr string) string {
	q := strings.Index(expr, "?")
	if q < 0 {
		return ""
	}

	colon := strings.LastIndex(expr, ":")
	if colon < 0 || colon < q {
		return ""
	}

	thenExpr := strings.TrimSpace(expr[q+1 : colon])
	elseExpr := strings.TrimSpace(expr[colon+1:])

	thenType := inferExprTypeFromText(scope, thenExpr)
	elseType := inferExprTypeFromText(scope, elseExpr)

	if thenType == elseType {
		return thenType
	}

	if thenType == "unknown" {
		return elseType
	}

	if elseType == "unknown" {
		return thenType
	}

	return "any"
}

func inferExprType(scope *Scope, expr Expr) string {
	switch e := expr.(type) {
	case StringExpr:
		return "string"
	case NumberExpr:
		return "number"
	case BoolExpr:
		return "bool"
	case ArrayExpr:
		return "array"
	case ObjectExpr:
		return "object"
	case NullExpr:
		return "null"
	case TernaryExpr:
		thenType := inferExprType(scope, e.ThenExpr)
		elseType := inferExprType(scope, e.ElseExpr)

		if thenType == elseType {
			return thenType
		}

		if thenType == "unknown" {
			return elseType
		}

		if elseType == "unknown" {
			return thenType
		}

		return "any"
	case BinaryExpr:
		switch e.Op {
		case TOKEN_EQ, TOKEN_NEQ, TOKEN_LT, TOKEN_GT, TOKEN_LTE, TOKEN_GTE, TOKEN_AND, TOKEN_OR, TOKEN_INSTANCEOF, TOKEN_IN:
			return "bool"
		case TOKEN_PLUS_ASSIGN, TOKEN_MINUS, TOKEN_STAR, TOKEN_SLASH, TOKEN_PERCENT:
			left := inferExprType(scope, e.Left)
			right := inferExprType(scope, e.Right)

			if e.Op == TOKEN_PLUS && (left == "string" || right == "string") {
				return "string"
			}

			return "number"
		default:
			return "unknown"
		}
	case IdentExpr:
		if sym, ok := scope.Resolve(e.Name); ok {
			return sym.Type
		}
		return "unknown"
	case CallExpr:
		if sym, ok := scope.Resolve(e.Name); ok {
			if sym.Kind == SymbolClass {
				return "class:" + e.Name
			}
			if sym.Kind == SymbolFunction && sym.Returns != "" {
				return sym.Returns
			}
		}
		return "unknown"
	case MemberCallExpr:
		return inferMemberCallTypeFromParts(scope, inferExprType(scope, e.Object), e.Method)
	default:
		return "unknown"
	}
}

func inferMemberCallTypeFromParts(scope *Scope, receiverType string, method string) string {
	if strings.HasPrefix(receiverType, "task:") && method == "await" {
		return strings.TrimPrefix(receiverType, "task:")
	}

	if strings.HasPrefix(receiverType, "std:") {
		module := strings.TrimPrefix(receiverType, "std:")
		if info, ok := GetStdModuleInfo(module); ok {
			if methodInfo, ok := info.Methods[method]; ok {
				return methodInfo.Returns
			}
		}
	}

	if strings.HasPrefix(receiverType, "class:") {
		className := strings.TrimPrefix(receiverType, "class:")
		if classSym, ok := resolveClassSymbol(scope, className); ok {
			if methodSym, ok := classSym.Methods[method]; ok {
				return firstNonEmpty(methodSym.Returns, "any")
			}
		}
	}

	if methodInfo, ok := GetNativeMethodInfo(receiverType, method); ok {
		return methodInfo.Returns
	}

	return ""
}

var inlineAnonFnRegex = regexp.MustCompile(`fn\s*\(([^)]*)\)\s*\{`)

func scanInlineAnonymousFunctionParams(scope *Scope, text string, pos Position, uri string) {
	lines := strings.Split(text, "\n")

	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}

	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])

		matches := inlineAnonFnRegex.FindAllStringSubmatch(line, -1)

		for _, match := range matches {
			paramsText := match[1]
			params := parseFunctionParams(paramsText)

			for _, param := range params {
				scope.Define(SymbolInfo{
					Name:      param.Name,
					Kind:      SymbolVariable,
					Type:      normalizeLSPType(scope, param.Type),
					Detail:    "anonymous function parameter " + param.Name,
					Line:      lineIndex + 1,
					Column:    indexColumn(line, param.Name),
					SourceURI: uri,
				})
			}
		}
	}
}

func formatSymbolFunctionSignature(sym SymbolInfo) string {
	parts := []string{}

	for _, param := range sym.Params {
		name := param.Name
		if param.Optional {
			name += "?"
		}

		parts = append(parts, name+": "+param.Type)
	}

	returns := sym.Returns
	if returns == "" {
		returns = "any"
	}

	return sym.Name + "(" + strings.Join(parts, ", ") + "): " + returns
}

func isPrivateFunctionAt(text string, fnStart int) bool {
	lineStart := strings.LastIndex(text[:fnStart], "\n")
	if lineStart == -1 {
		lineStart = 0
	} else {
		lineStart++
	}

	beforeFn := strings.TrimSpace(text[lineStart:fnStart])
	return strings.Contains(beforeFn, "private")
}

func isPrivateSymbol(sym SymbolInfo) bool {
	return strings.Contains(sym.Detail, "private method") ||
		strings.Contains(sym.Detail, "private field")
}

func getHover(uri string, text string, pos Position) any {
	word := wordAtPosition(text, pos)

	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	receiver, method, ok := memberExprAtPosition(text, pos)
	if ok {
		sym, exists := scope.Resolve(receiver)
		if !exists {
			return nil
		}

		if strings.HasPrefix(sym.Type, "class:") {
			className := strings.TrimPrefix(sym.Type, "class:")

			classSym, ok := resolveClassSymbol(scope, className)
			if !ok || classSym.Kind != SymbolClass {
				return nil
			}

			if methodSym, ok := classSym.Methods[method]; ok {
				signature := formatFunctionSignature(className+"."+methodSym.Name, methodSym.Params, methodSym.Returns)

				return HoverResult{
					Contents: MarkupContent{
						Kind:  "markdown",
						Value: "```tiny\n" + signature + "\n```\n" + methodSym.Detail,
					},
				}
			}

			if fieldSym, ok := classSym.Fields[method]; ok {
				return HoverResult{
					Contents: MarkupContent{
						Kind:  "markdown",
						Value: "**" + receiver + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + fieldSym.Detail,
					},
				}
			}
		}

		if strings.HasPrefix(sym.Type, "std:") {
			module := strings.TrimPrefix(sym.Type, "std:")
			info, ok := GetStdModuleInfo(module)
			if !ok {
				return nil
			}

			methodInfo, ok := info.Methods[method]
			if !ok {
				return nil
			}

			signature := formatStdSignature(module, methodInfo)
			return HoverResult{
				Contents: MarkupContent{
					Kind:  "markdown",
					Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description,
				},
			}
		}

		if strings.HasPrefix(sym.Type, "task:") && method == "await" {
			returnType := strings.TrimPrefix(sym.Type, "task:")
			return HoverResult{
				Contents: MarkupContent{
					Kind:  "markdown",
					Value: "```tiny\ntask.await(): " + returnType + "\n```\nWaits for the task and returns its result.",
				},
			}
		}

		methodInfo, ok := GetNativeMethodInfo(sym.Type, method)
		if ok {
			signature := formatNativeSignature(sym.Type, methodInfo)
			return HoverResult{
				Contents: MarkupContent{
					Kind:  "markdown",
					Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description,
				},
			}
		}

		return nil
	}

	sym, exists := scope.Resolve(word)
	if !exists {
		return nil
	}

	if sym.Kind == SymbolFunction {
		signature := formatFunctionSignature(sym.Name, sym.Params, sym.Returns)
		return HoverResult{
			Contents: MarkupContent{
				Kind:  "markdown",
				Value: "```tiny\n" + signature + "\n```\n" + sym.Detail,
			},
		}
	}

	return HoverResult{
		Contents: MarkupContent{
			Kind:  "markdown",
			Value: "**" + sym.Name + "**\n\nType: `" + sym.Type + "`\n\n" + sym.Detail,
		},
	}
}

// =====================
// AST-based semantic diagnostics
// =====================

type astSemanticAnalyzer struct {
	uri          string
	root         *Scope
	scope        *Scope
	diagnostics  []map[string]any
	currentClass string
}

func semanticDiagnosticsFromAST(uri string, text string) []map[string]any {
	statements, parseDiagnostics := parseTinyForLSP(uri, text)
	if len(parseDiagnostics) > 0 || statements == nil {
		return []map[string]any{}
	}

	root := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		root.Define(SymbolInfo{
			Name: alias, Kind: SymbolStd, Type: "std:" + module,
			Detail: "std module " + module, Line: 1, Column: 1, SourceURI: uri,
		})
	}

	scanFileImportsIntoScope(root, uri, text)

	a := &astSemanticAnalyzer{uri: uri, root: root, scope: root}
	a.predeclareStatements(statements)
	a.visitStatements(statements)

	return a.diagnostics
}

func (a *astSemanticAnalyzer) pushScope() *Scope {
	child := NewScope(a.scope)
	a.scope = child
	return child
}

func (a *astSemanticAnalyzer) popScope() {
	if a.scope != nil && a.scope.Parent != nil {
		a.scope = a.scope.Parent
	}
}

func (a *astSemanticAnalyzer) define(sym SymbolInfo) {
	if sym.SourceURI == "" {
		sym.SourceURI = a.uri
	}
	a.scope.Define(sym)
}

func (a *astSemanticAnalyzer) resolve(name string) (SymbolInfo, bool) {
	return a.scope.Resolve(name)
}

func (a *astSemanticAnalyzer) addDiagnostic(line int, column int, message string) {
	if line <= 0 {
		line = 1
	}
	if column <= 0 {
		column = 1
	}
	a.diagnostics = append(a.diagnostics, makeRangeDiagnostic(line-1, column-1, column, 2, message))
}

func stdArgsFromParams(scope *Scope, params []Param) []StdArg {
	args := []StdArg{}

	for _, p := range params {
		name := p.Name
		typ := typeHintName(p.TypeHint, "any")

		if p.Variadic {
			typ = "array"
		} else {
			typ = normalizeLSPType(scope, typ)
		}

		args = append(args, StdArg{
			Name:     name,
			Type:     typ,
			Optional: p.HasDefault,
		})
	}

	return args
}

func returnTypeName(h TypeHint) string {
	if h.IsEmpty() {
		return "any"
	}
	return h.Name
}

func returnTypeNameScoped(scope *Scope, h TypeHint) string {
	if h.IsEmpty() {
		return "any"
	}
	return normalizeLSPType(scope, h.Name)
}

func (a *astSemanticAnalyzer) predeclareStatements(stmts []Stmt) {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		switch s := stmt.(type) {
		case ImportStmt:
			alias := s.Alias
			if alias == "" {
				alias = s.Path
			}

			if s.Std {
				a.root.Define(SymbolInfo{
					Name: alias, Kind: SymbolStd, Type: "std:" + s.Path,
					Detail: "std module " + s.Path, Line: s.Line, Column: s.Column, SourceURI: a.uri,
				})
				break
			}

			// Non-std imports are already loaded by scanFileImportsIntoScope(root, ...).
			// Do NOT overwrite namespace imports like:
			// import "user.tiny" as models;
			// because that destroys models.User for diagnostics.
			if existing, ok := a.root.Resolve(alias); ok {
				existing.Line = s.Line
				existing.Column = s.Column
				a.root.Define(existing)
				break
			}

			a.root.Define(SymbolInfo{
				Name: alias, Kind: SymbolVariable, Type: "any",
				Detail: "import " + s.Path, Line: s.Line, Column: s.Column, SourceURI: a.uri,
			})

		case FunctionStmt:
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, s.Params), Returns: returnTypeNameScoped(a.root, s.ReturnType)})

		case ClassStmt:
			a.root.Define(a.classSymbol(s))

		case VariableStmt:
			// Define early as unknown so forward-ish references don't explode while typing.
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

		case NamespaceStmt:
			members := map[string]SymbolInfo{}
			for _, rawInner := range s.Statements {
				inner, exported := unwrapExport(rawInner)
				if !exported {
					_ = exported
				}
				switch m := inner.(type) {
				case FunctionStmt:
					members[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + m.Name, Line: m.Line, Column: m.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, m.Params), Returns: returnTypeNameScoped(a.root, m.ReturnType)}
				case VariableStmt:
					members[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + m.Name, Line: m.Line, Column: m.Column, SourceURI: a.uri}
				case ClassStmt:
					members[m.Name] = a.classSymbol(m)
				}
			}
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolNamespace, Type: "namespace", Detail: "namespace " + s.Name, Line: 1, Column: 1, SourceURI: a.uri, Members: members})
		}
	}
}

func (a *astSemanticAnalyzer) classSymbol(cls ClassStmt) SymbolInfo {
	fields := map[string]SymbolInfo{}
	for _, f := range cls.Fields {
		typ := typeHintName(f.TypeHint, "any")
		if typ == "any" && f.Value != nil {
			typ = a.inferExprType(f.Value)
		} else {
			typ = normalizeLSPType(a.root, typ)
		}
		detail := "field " + f.Name
		if f.Private {
			detail = "private " + detail
		}
		if f.Constant {
			detail = "const " + detail
		}
		fields[f.Name] = SymbolInfo{Name: f.Name, Kind: SymbolField, Type: typ, Detail: detail, Line: f.Line, Column: f.Column, SourceURI: a.uri}
	}

	methods := map[string]SymbolInfo{}
	for _, m := range cls.Methods {
		methods[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolFunction, Type: "function", Detail: "method " + cls.Name + "." + m.Name, Line: m.Line, Column: m.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, m.Params), Returns: returnTypeNameScoped(a.root, m.ReturnType)}
	}

	return SymbolInfo{Name: cls.Name, Kind: SymbolClass, Type: "class:" + cls.Name, Detail: "class " + cls.Name, Line: cls.Line, Column: cls.Column, SourceURI: a.uri, Fields: fields, Methods: methods}
}

func (a *astSemanticAnalyzer) visitStatements(stmts []Stmt) {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		a.visitStmt(stmt)
	}
}

func (a *astSemanticAnalyzer) visitStmt(stmt Stmt) {
	switch s := stmt.(type) {
	case ImportStmt:
		// Already declared in prepass.

	case VariableStmt:
		typ := a.inferExprType(s.Value)
		fields := map[string]SymbolInfo(nil)
		if typ == "object" {
			fields = a.fieldsFromObjectExpr(s.Value, s.Line)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Fields: fields})

	case FieldStmt:
		// Class fields are handled by classSymbol. Top-level field is treated like a variable if parser allows it.
		typ := typeHintName(s.TypeHint, "any")
		if typ == "any" {
			typ = a.inferExprType(s.Value)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "field " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

	case FunctionStmt:
		a.visitFunction(s)

	case ClassStmt:
		a.define(a.classSymbol(s))
		for _, m := range s.Methods {
			a.visitMethod(s.Name, m)
		}

	case NamespaceStmt:
		a.pushScope()
		a.visitStatements(s.Statements)
		a.popScope()

	case EnumStmt:
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: "object", Detail: "enum " + s.Name, SourceURI: a.uri})

	case ExprStmt:
		a.inferExprType(s.Value)

	case AssignStmt:
		if _, ok := a.resolve(s.Name); !ok {
			a.addDiagnostic(s.Line, s.Column, "undefined variable: "+s.Name)
		}
		a.inferExprType(s.Value)

	case ReturnStmt:
		if s.HasValue {
			a.inferExprType(s.Value)
		}

	case IfStmt:
		a.inferExprType(s.Condition)
		a.pushScope()
		a.visitStatements(s.ThenBody)
		a.popScope()
		a.pushScope()
		a.visitStatements(s.ElseBody)
		a.popScope()

	case WhileStmt:
		a.inferExprType(s.Condition)
		a.pushScope()
		a.visitStatements(s.Body)
		a.popScope()

	case ForStmt:
		a.pushScope()
		if s.Init != nil {
			a.visitStmt(s.Init)
		}
		a.inferExprType(s.Condition)
		a.visitStatements(s.Body)
		if s.Update != nil {
			a.visitStmt(s.Update)
		}
		a.popScope()

	case ForInStmt:
		a.inferExprType(s.Iterable)
		a.pushScope()
		a.define(SymbolInfo{Name: s.ItemName, Kind: SymbolVariable, Type: "any", Detail: "for item " + s.ItemName, SourceURI: a.uri})
		if s.IndexName != "" {
			a.define(SymbolInfo{Name: s.IndexName, Kind: SymbolVariable, Type: "number", Detail: "for index " + s.IndexName, SourceURI: a.uri})
		}
		a.visitStatements(s.Body)
		a.popScope()

	case TryCatchStmt:
		a.pushScope()
		a.visitStatements(s.TryBody)
		a.popScope()
		a.pushScope()
		a.define(SymbolInfo{Name: s.ErrorName, Kind: SymbolVariable, Type: "error", Detail: "catch error " + s.ErrorName, SourceURI: a.uri})
		a.visitStatements(s.CatchBody)
		a.popScope()
		a.pushScope()
		a.visitStatements(s.FinallyBody)
		a.popScope()

	case ThrowStmt:
		a.inferExprType(s.Value)

	case IndexAssignStmt:
		a.inferExprType(s.Object)
		a.inferExprType(s.Index)
		a.inferExprType(s.Value)

	case PropertyAssignStmt:
		a.checkMember(s.Object, s.Name, s.Line, s.Column)
		a.inferExprType(s.Value)

	case MatchStmt:
		a.inferExprType(s.Value)
		for _, mc := range s.Cases {
			a.inferExprType(mc.Value)
			a.pushScope()
			a.visitStatements(mc.Body)
			a.popScope()
		}
		if s.Default != nil {
			a.pushScope()
			a.visitStatements(s.Default)
			a.popScope()
		}
	}
}

func (a *astSemanticAnalyzer) visitFunction(fn FunctionStmt) {
	a.pushScope()
	for _, p := range fn.Params {
		a.define(paramSymbol(a.scope, p, a.uri, fn.Line, fn.Column))
	}
	a.visitStatements(fn.Body)
	a.popScope()
}

func (a *astSemanticAnalyzer) visitMethod(className string, fn FunctionStmt) {
	oldClass := a.currentClass
	a.currentClass = className
	a.pushScope()
	a.define(SymbolInfo{Name: "this", Kind: SymbolVariable, Type: "class:" + className, Detail: "current class instance", Line: fn.Line, Column: fn.Column, SourceURI: a.uri})
	for _, p := range fn.Params {
		if p.Name != "this" {
			a.define(paramSymbol(a.scope, p, a.uri, fn.Line, fn.Column))
		}
	}
	a.visitStatements(fn.Body)
	a.popScope()
	a.currentClass = oldClass
}

func paramSymbol(scope *Scope, param Param, uri string, line int, column int) SymbolInfo {
	typ := typeHintName(param.TypeHint, "any")

	if param.Variadic {
		typ = "array"
	} else {
		typ = normalizeLSPType(scope, typ)
	}

	return SymbolInfo{
		Name:      param.Name,
		Kind:      SymbolVariable,
		Type:      typ,
		Detail:    "parameter " + param.Name,
		Line:      line,
		Column:    column,
		SourceURI: uri,
	}
}
func (a *astSemanticAnalyzer) fieldsFromObjectExpr(expr Expr, line int) map[string]SymbolInfo {
	obj, ok := expr.(ObjectExpr)
	if !ok {
		return nil
	}
	fields := map[string]SymbolInfo{}
	for _, f := range obj.Fields {
		fields[f.Name] = SymbolInfo{Name: f.Name, Kind: SymbolField, Type: a.inferExprType(f.Value), Detail: "field " + f.Name, Line: line, Column: 1, SourceURI: a.uri}
	}
	return fields
}

func normalizeLSPType(scope *Scope, typ string) string {
	typ = strings.TrimSpace(typ)

	if typ == "" {
		return "any"
	}

	if strings.Contains(typ, "|") {
		parts := strings.Split(typ, "|")
		out := []string{}

		for _, part := range parts {
			out = append(out, normalizeLSPType(scope, strings.TrimSpace(part)))
		}

		return strings.Join(out, " | ")
	}

	switch typ {
	case "string", "number", "bool", "object", "array", "any", "null", "undefined", "function", "error":
		return typ
	}

	if sym, ok := scope.Resolve(typ); ok && sym.Kind == SymbolClass {
		return "class:" + typ
	}

	if strings.Contains(typ, ".") {
		parts := strings.SplitN(typ, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			member, ok := ns.Members[memberName]
			if ok && member.Kind == SymbolClass {
				return "class:" + typ
			}
		}
	}

	return typ
}

func (a *astSemanticAnalyzer) inferExprType(expr Expr) string {
	switch e := expr.(type) {
	case nil:
		return "unknown"
	case StringExpr, InterpolatedStringExpr:
		return "string"
	case NumberExpr, FloatExpr:
		return "number"
	case BoolExpr:
		return "bool"
	case NullExpr:
		return "null"
	case UndefinedExpr:
		return "undefined"
	case ArrayExpr:
		for _, el := range e.Elements {
			a.inferExprType(el)
		}
		return "array"
	case ObjectExpr:
		for _, f := range e.Fields {
			a.inferExprType(f.Value)
		}
		return "object"
	case IdentExpr:
		if sym, ok := a.resolve(e.Name); ok {
			return sym.Type
		}
		if !tinyKeywords[e.Name] && e.Name != "_" {
			a.addDiagnostic(e.Line, e.Column, "undefined variable: "+e.Name)
		}
		return "unknown"
	case ThisExpr:
		if sym, ok := a.resolve("this"); ok {
			return sym.Type
		}
		a.addDiagnostic(1, 1, "cannot use this outside of a method")
		return "unknown"
	case PropertyExpr:
		// Special case: namespace property access like models.User
		if ident, ok := e.Object.(IdentExpr); ok {
			if sym, exists := a.resolve(ident.Name); exists && sym.Kind == SymbolNamespace {
				memberSym, ok := sym.Members[e.Name]
				if !ok {
					a.addDiagnostic(e.Line, e.Column, "undefined method or property: "+e.Name)
					return "unknown"
				}

				if memberSym.Kind == SymbolClass {
					return "class:" + ident.Name + "." + memberSym.Name
				}

				return memberSym.Type
			}
		}

		a.checkMember(e.Object, e.Name, e.Line, e.Column)
		objType := a.inferExprType(e.Object)
		return a.memberType(objType, e.Name)
	case MemberCallExpr:
		// Special case: namespace member call like models.User()
		if ident, ok := e.Object.(IdentExpr); ok {
			if sym, exists := a.resolve(ident.Name); exists && sym.Kind == SymbolNamespace {
				memberSym, ok := sym.Members[e.Method]
				if !ok {
					a.addDiagnostic(e.Line, e.Column, "undefined method or property: "+e.Method)
					return "unknown"
				}

				for _, arg := range e.Args {
					a.inferExprType(arg)
				}

				if memberSym.Kind == SymbolClass {
					return "class:" + ident.Name + "." + memberSym.Name
				}

				if memberSym.Kind == SymbolFunction {
					if memberSym.Returns != "" {
						return memberSym.Returns
					}
					return "any"
				}

				return memberSym.Type
			}
		}

		objType := a.inferExprType(e.Object)

		for _, arg := range e.Args {
			a.inferExprType(arg)
		}

		if shouldCheckMemberAccess(objType) {
			if !a.memberExistsByType(objType, e.Method) {
				a.addDiagnostic(e.Line, e.Column, "undefined method or property: "+e.Method)
			}
		}

		return a.memberType(objType, e.Method)
	case CallExpr:
		for _, arg := range e.Args {
			a.inferExprType(arg)
		}
		if sym, ok := a.resolve(e.Name); ok {
			if sym.Kind == SymbolClass {
				return "class:" + sym.Name
			}
			if sym.Kind == SymbolFunction {
				if sym.Returns != "" {
					return sym.Returns
				}
				return "any"
			}
			return sym.Type
		}
		a.addDiagnostic(e.Line, e.Column, "undefined variable: "+e.Name)
		return "unknown"
	case CallValueExpr:
		calleeType := a.inferExprType(e.Callee)
		for _, arg := range e.Args {
			a.inferExprType(arg)
		}
		return calleeType
	case FunctionExpr:
		a.pushScope()
		for _, p := range e.Params {
			a.define(paramSymbol(a.scope, p, a.uri, e.Line, e.Column))
		}
		a.visitStatements(e.Body)
		a.popScope()
		return "function"
	case SpawnExpr:
		t := a.inferExprType(e.Function)
		if t == "function" {
			return "task:any"
		}
		return "task:" + t
	case UnaryExpr:
		a.inferExprType(e.Right)
		if e.Op == TOKEN_BANG {
			return "bool"
		}
		return "number"
	case BinaryExpr:
		lt := a.inferExprType(e.Left)
		rt := a.inferExprType(e.Right)
		switch e.Op {
		case TOKEN_EQ, TOKEN_NEQ, TOKEN_LT, TOKEN_GT, TOKEN_LTE, TOKEN_GTE, TOKEN_AND, TOKEN_OR:
			return "bool"
		case TOKEN_PLUS:
			if lt == "string" || rt == "string" {
				return "string"
			}
			return "number"
		default:
			return "number"
		}
	case TernaryExpr:
		a.inferExprType(e.Condition)
		t := a.inferExprType(e.ThenExpr)
		f := a.inferExprType(e.ElseExpr)
		if t == f {
			return t
		}
		if t == "unknown" {
			return f
		}
		if f == "unknown" {
			return t
		}
		return "any"
	case IndexExpr:
		a.inferExprType(e.Object)
		a.inferExprType(e.Index)
		return "any"
	case TypeOfExpr:
		a.inferExprType(e.Value)
		return "string"
	case InstanceOfExpr:
		a.inferExprType(e.Object)
		a.inferExprType(e.Class)
		return "bool"
	case ObjectInExpr:
		a.inferExprType(e.Object)
		a.inferExprType(e.Key)
		return "bool"
	default:
		return "unknown"
	}
}

func (a *astSemanticAnalyzer) checkMember(object Expr, member string, line int, column int) {
	objType := a.inferExprType(object)
	if !shouldCheckMemberAccess(objType) {
		return
	}
	if !a.memberExistsByType(objType, member) {
		a.addDiagnostic(line, column, "undefined method or property: "+member)
	}
}

func (a *astSemanticAnalyzer) memberExistsByType(typ string, member string) bool {
	typ = strings.TrimSpace(typ)

	if strings.Contains(typ, "|") {
		for _, part := range splitUnionType(typ) {
			if isNullishLSPType(part) {
				continue
			}

			if a.memberExistsByType(part, member) {
				return true
			}
		}

		return false
	}

	// Raw class name: User
	if sym, ok := resolveClassSymbol(a.root, typ); ok && sym.Kind == SymbolClass {
		typ = "class:" + typ
	}

	if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")

		classSym, ok := resolveClassSymbol(a.root, className)
		if !ok || classSym.Kind != SymbolClass {
			return false
		}

		if _, ok := classSym.Methods[member]; ok {
			return true
		}

		if _, ok := classSym.Fields[member]; ok {
			return true
		}

		return false
	}

	if strings.HasPrefix(typ, "std:") {
		module := strings.TrimPrefix(typ, "std:")
		info, ok := GetStdModuleInfo(module)
		if !ok {
			return false
		}

		_, ok = info.Methods[member]
		return ok
	}

	if strings.HasPrefix(typ, "task:") {
		return member == "await"
	}

	if _, ok := GetNativeMethodInfo(typ, member); ok {
		return true
	}

	if member == "toString" {
		return true
	}

	return false
}

func (a *astSemanticAnalyzer) memberType(typeName string, member string) string {
	typeName = strings.TrimSpace(typeName)

	if strings.Contains(typeName, "|") {
		for _, part := range splitUnionType(typeName) {
			if isNullishLSPType(part) {
				continue
			}

			result := a.memberType(part, member)
			if result != "" && result != "unknown" {
				return result
			}
		}

		return "unknown"
	}

	// Raw class name: User
	if sym, ok := resolveClassSymbol(a.root, typeName); ok && sym.Kind == SymbolClass {
		typeName = "class:" + typeName
	}

	if strings.HasPrefix(typeName, "class:") {
		className := strings.TrimPrefix(typeName, "class:")

		classSym, ok := resolveClassSymbol(a.root, className)
		if !ok || classSym.Kind != SymbolClass {
			return "unknown"
		}

		if methodSym, ok := classSym.Methods[member]; ok {
			return firstNonEmpty(methodSym.Returns, "any")
		}

		if fieldSym, ok := classSym.Fields[member]; ok {
			return firstNonEmpty(fieldSym.Type, "any")
		}

		return "unknown"
	}
	if sym, ok := a.root.Resolve(typeName); ok && sym.Kind == SymbolClass {
		typeName = "class:" + typeName
	}

	if strings.Contains(typeName, "|") {
		parts := strings.Split(typeName, "|")

		for _, part := range parts {
			part = strings.TrimSpace(part)

			if part == "null" || part == "undefined" {
				continue
			}

			memberTyp := a.memberType(part, member)
			if memberTyp != "" && memberTyp != "unknown" {
				return memberTyp
			}
		}

		return "unknown"
	}

	if strings.HasPrefix(typeName, "task:") && member == "await" {
		return strings.TrimPrefix(typeName, "task:")
	}
	if strings.HasPrefix(typeName, "std:") {
		info, ok := GetStdModuleInfo(strings.TrimPrefix(typeName, "std:"))
		if ok {
			if m, ok := info.Methods[member]; ok {
				return m.Returns
			}
		}
	}
	if strings.HasPrefix(typeName, "class:") {
		className := strings.TrimPrefix(typeName, "class:")
		classSym, ok := a.root.Resolve(className)
		if ok {
			if m, ok := classSym.Methods[member]; ok {
				if m.Returns != "" {
					return m.Returns
				}
				return "any"
			}
			if f, ok := classSym.Fields[member]; ok {
				return f.Type
			}
		}
	}
	if m, ok := GetNativeMethodInfo(typeName, member); ok {
		return m.Returns
	}
	return "unknown"
}
