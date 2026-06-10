package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	. "language.com/src/vm"
)

type SymbolKind string

const (
	SymbolVariable  SymbolKind = "variable"
	SymbolFunction  SymbolKind = "function"
	SymbolClass     SymbolKind = "class"
	SymbolInterface SymbolKind = "interface"
	SymbolStd       SymbolKind = "std"
	SymbolNamespace SymbolKind = "namespace"
	SymbolField     SymbolKind = "field"
	SymbolEnum      SymbolKind = "enum"
)

type SymbolInfo struct {
	Name      string
	Kind      SymbolKind
	Type      string
	Detail    string
	Line      int
	Column    int
	SourceURI string
	Doc       string

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

func cloneScope(s *Scope) *Scope {
	if s == nil {
		return nil
	}
	newScope := NewScope(cloneScope(s.Parent))
	for k, v := range s.Symbols {
		newScope.Symbols[k] = v
	}
	return newScope
}

func (s *Scope) Define(sym SymbolInfo) {
	if strings.TrimSpace(sym.Name) == "" {
		return
	}
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

var typeNamePattern = `[A-Za-z_][A-Za-z0-9_]*(?:[\.:][A-Za-z_][A-Za-z0-9_]*)*`
var unionTypePattern = typeNamePattern + `(?:\s*\|\s*` + typeNamePattern + `)*`

var variableLineRegex = regexp.MustCompile(
	`(?m)^(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*=\s*(.+?)(?:;|\r?$)`,
)
var fieldLineRegex = regexp.MustCompile(
	`(?m)^field\s+(?:(?:public|private|const)\s+)*([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*(?:=\s*(.+?))?(?:;|\r?$)`,
)
var classFieldNameWithQuestionRegex = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\?(\s*[:=;]|$)`)
var fieldNameWithQuestionRegex = regexp.MustCompile(`^field\s+([A-Za-z_][A-Za-z0-9_]*)\?(\s*[:=;]|$)`)
var functionLineRegex = regexp.MustCompile(
	`^(?:export\s+)?(?:async\s+)?(?:(?:public|private)\s+)?fn\s+([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*(?::\s*(` + unionTypePattern + `))?`,
)
var classLineRegex = regexp.MustCompile(`(?m)^(?:export\s+)?class\s+([A-Za-z_][A-Za-z0-9_]*)`)
var memberCallRegex = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
var normalCallRegex = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
var classEmbedRegex = regexp.MustCompile(`(?m)\bembed\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:;|\r?$)`)
var returnRegex = regexp.MustCompile(`(?m)return\s+(.+?)(?:;|\r?$)`)
var fileImportRegex = regexp.MustCompile(`(?m)import\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*(?:;|\r?$)`)
var libraryImportRegex = regexp.MustCompile(`(?m)^\s*import\s+(?:library|lib)\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*;?`)
var catchVarRegex = regexp.MustCompile(`(?m)\bcatch\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)
var enumLineRegex = regexp.MustCompile(`(?m)^(?:export\s+)?enum\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{([^}]*)\}`)
var exportedEnumBlockRegex = regexp.MustCompile(`(?s)\bexport\s+enum\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{(.*?)\}`)
var interfaceLineRegex = regexp.MustCompile(`(?m)^(?:export\s+)?interface\s+([A-Za-z_][A-Za-z0-9_]*)`)
var interfaceFieldRegex = regexp.MustCompile(`^\s*([A-Za-z_][A-Za-z0-9_]*\??)\s*:\s*([^,;\r\n]+)`)
var embedLineRegex = regexp.MustCompile(
	`(?m)^(embedstr|embedbin|embeddir)\s+"([^"]+)"\s+(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:;|\r?$)`,
)
var spawnFnRegex = regexp.MustCompile(`=\s*spawn\s*(?:\([^)]*\))?\s*(?:async\s+)?fn\b`)
var spawnPrefixRegex = regexp.MustCompile(`^spawn\s*(?:\([^)]*\))?\s*(?:async\s+)?(fn)\b`)
var loopInRegex = regexp.MustCompile(`\bin\b`)
var loopIdentifierRegex = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
	IsAsync    bool
}

type lspBlockCacheEntry struct {
	blocks []blockInfo
}

type lspProjectFilesCacheEntry struct {
	root      string
	files     []string
	expiresAt time.Time
}

var lspBlockCache = map[string]lspBlockCacheEntry{}
var lspProjectFilesCache = map[string]lspProjectFilesCacheEntry{}

func invalidateLSPFastCaches() {
	lspBlockCache = map[string]lspBlockCacheEntry{}
	lspProjectFilesCache = map[string]lspProjectFilesCacheEntry{}
}

func lspTextCacheKey(kind string, text string) string {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211

	hash := uint64(offset64)
	for i := 0; i < len(text); i++ {
		hash ^= uint64(text[i])
		hash *= prime64
	}

	return kind + ":" + strconv.Itoa(len(text)) + ":" + strconv.FormatUint(hash, 16)
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
	return typ == "null"
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

func scanLoopVariables(scope *Scope, text string, cursorLine int, uri string) {
	lines := strings.Split(text, "\n")
	isValidIdentifier := func(name string) bool {
		return loopIdentifierRegex.MatchString(name)
	}

	offset := 0
	for {
		idx := strings.Index(text[offset:], "for")
		if idx < 0 {
			break
		}

		start := offset + idx
		if !isWordBoundaryAt(text, start, 3) {
			offset = start + 3
			continue
		}

		startLine := lineNumberAtOffset(text, start)
		if startLine > cursorLine {
			break
		}

		// Found a "for" keyword. Now find the matching opening brace '{'
		parenDepth := 0
		bracketDepth := 0
		braceDepth := 0
		inString := byte(0)
		escaped := false
		foundOpenBrace := -1

		for i := start + 3; i < len(text); i++ {
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

			// Skip comments
			if ch == '/' && i+1 < len(text) && text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
					i++
				}
				continue
			}
			if ch == '/' && i+1 < len(text) && text[i+1] == '*' {
				i += 2
				for i < len(text) {
					if text[i] == '*' && i+1 < len(text) && text[i+1] == '/' {
						i += 1
						break
					}
					i++
				}
				continue
			}

			if ch == '(' {
				parenDepth++
			} else if ch == ')' {
				if parenDepth > 0 {
					parenDepth--
				}
			} else if ch == '[' {
				bracketDepth++
			} else if ch == ']' {
				if bracketDepth > 0 {
					bracketDepth--
				}
			} else if ch == '{' {
				if parenDepth == 0 && bracketDepth == 0 {
					foundOpenBrace = i
					break
				}
				braceDepth++
			} else if ch == '}' {
				if braceDepth > 0 {
					braceDepth--
				}
			}
		}

		if foundOpenBrace < 0 {
			offset = start + 3
			continue
		}

		closeBrace := findMatching(text, foundOpenBrace, '{', '}')
		if closeBrace < 0 {
			closeBrace = len(text)
		}

		endLine := lineNumberAtOffset(text, closeBrace)

		// Check if the cursor is within the loop scope
		if cursorLine >= startLine && cursorLine <= endLine {
			header := strings.TrimSpace(text[start+3 : foundOpenBrace])

			// Strip outer parentheses from header
			for strings.HasPrefix(header, "(") && strings.HasSuffix(header, ")") {
				if findMatching(header, 0, '(', ')') == len(header)-1 {
					header = strings.TrimSpace(header[1 : len(header)-1])
				} else {
					break
				}
			}

			lineText := ""
			if startLine-1 >= 0 && startLine-1 < len(lines) {
				lineText = lines[startLine-1]
			}

			// Check for " in " to identify for-in loops
			loc := loopInRegex.FindStringIndex(header)
			if loc != nil {
				// For-in loop
				lhs := strings.TrimSpace(header[:loc[0]])
				rhs := strings.TrimSpace(header[loc[1]:])

				iterableType := inferExprTypeFromText(scope, rhs)
				itemType := "any"
				if iterableType == "string" {
					itemType = "string"
				} else if strings.HasPrefix(iterableType, "array:") {
					itemType = strings.TrimPrefix(iterableType, "array:")
				}

				vars := strings.Split(lhs, ",")
				if len(vars) == 1 {
					itemName := strings.TrimSpace(vars[0])
					if isValidIdentifier(itemName) {
						scope.Define(SymbolInfo{
							Name:      itemName,
							Kind:      SymbolVariable,
							Type:      itemType,
							Detail:    "loop variable " + itemName,
							Line:      startLine,
							Column:    indexColumn(lineText, itemName),
							SourceURI: uri,
						})
					}
				} else if len(vars) >= 2 {
					itemName := strings.TrimSpace(vars[0])
					indexName := strings.TrimSpace(vars[1])
					if isValidIdentifier(itemName) {
						scope.Define(SymbolInfo{
							Name:      itemName,
							Kind:      SymbolVariable,
							Type:      itemType,
							Detail:    "loop variable " + itemName,
							Line:      startLine,
							Column:    indexColumn(lineText, itemName),
							SourceURI: uri,
						})
					}
					if isValidIdentifier(indexName) {
						scope.Define(SymbolInfo{
							Name:      indexName,
							Kind:      SymbolVariable,
							Type:      "number",
							Detail:    "loop index variable " + indexName,
							Line:      startLine,
							Column:    indexColumn(lineText, indexName),
							SourceURI: uri,
						})
					}
				}
			} else {
				// Standard for loop (split by semicolon)
				parts := strings.Split(header, ";")
				if len(parts) > 0 {
					initPart := strings.TrimSpace(parts[0])
					if strings.HasPrefix(initPart, "let ") || strings.HasPrefix(initPart, "const ") {
						match := variableLineRegex.FindStringSubmatch(initPart)
						if match != nil {
							name := match[1]
							typeHint := match[2]
							exprText := strings.TrimSpace(match[3])

							typ := "any"
							fields := map[string]SymbolInfo(nil)

							if typeHint != "" {
								typ = normalizeLSPType(scope, typeHint)
							} else {
								typ = inferExprTypeFromText(scope, exprText)
								typ = normalizeLSPType(scope, typ)
								if typ == "object" {
									fields = inferObjectFieldsFromText(scope, exprText, uri, startLine)
								}
							}

							if isValidIdentifier(name) {
								scope.Define(SymbolInfo{
									Name:      name,
									Kind:      SymbolVariable,
									Type:      typ,
									Detail:    "loop variable " + name,
									Line:      startLine,
									Column:    indexColumn(lineText, name),
									SourceURI: uri,
									Fields:    fields,
								})
							}
						}
					}
				}
			}
		}

		offset = start + 3
	}
}

func resolveInterfaceSymbol(scope *Scope, ifaceName string) (SymbolInfo, bool) {
	ifaceName = strings.TrimSpace(ifaceName)

	if strings.Contains(ifaceName, ".") {
		parts := strings.SplitN(ifaceName, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			member, ok := ns.Members[memberName]
			if ok && member.Kind == SymbolInterface {
				return member, true
			}
		}
	}

	if sym, ok := scope.Resolve(ifaceName); ok && sym.Kind == SymbolInterface {
		return sym, true
	}

	for s := scope; s != nil; s = s.Parent {
		for _, sym := range s.Symbols {
			if sym.Kind == SymbolNamespace {
				if member, ok := sym.Members[ifaceName]; ok && member.Kind == SymbolInterface {
					return member, true
				}
			}
		}
	}

	shortName := ifaceName
	if idx := strings.LastIndex(ifaceName, "."); idx >= 0 {
		shortName = ifaceName[idx+1:]
	}
	for _, entry := range lspImportExportCache {
		if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolInterface {
			return member, true
		}
	}

	return SymbolInfo{}, false
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

	for s := scope; s != nil; s = s.Parent {
		for _, sym := range s.Symbols {
			if sym.Kind == SymbolNamespace {
				if member, ok := sym.Members[className]; ok && member.Kind == SymbolClass {
					return member, true
				}
			}
		}
	}

	shortName := className
	if idx := strings.LastIndex(className, "."); idx >= 0 {
		shortName = className[idx+1:]
	}
	for _, entry := range lspImportExportCache {
		if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolClass {
			return member, true
		}
	}

	return SymbolInfo{}, false
}

func resolveEnumSymbol(scope *Scope, enumName string) (SymbolInfo, bool) {
	if sym, ok := scope.Resolve(enumName); ok && sym.Kind == SymbolEnum {
		return sym, true
	}

	if strings.Contains(enumName, ".") {
		parts := strings.SplitN(enumName, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			member, ok := ns.Members[memberName]
			if ok && member.Kind == SymbolEnum {
				return member, true
			}
		}
	}

	for s := scope; s != nil; s = s.Parent {
		for _, sym := range s.Symbols {
			if sym.Kind == SymbolNamespace {
				if member, ok := sym.Members[enumName]; ok && member.Kind == SymbolEnum {
					return member, true
				}
			}
		}
	}

	shortName := enumName
	if idx := strings.LastIndex(enumName, "."); idx >= 0 {
		shortName = enumName[idx+1:]
	}
	for _, entry := range lspImportExportCache {
		if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolEnum {
			return member, true
		}
	}

	return SymbolInfo{}, false
}

func memberExistsOnSymbol(scope *Scope, sym SymbolInfo, member string) bool {
	if strings.HasPrefix(sym.Type, "task:") {
		return member == "await"
	}

	if sym.Type == "error" {
		return member == "kind" || member == "message" || member == "toString"
	}

	if sym.Type == "object" {
		return true
	}

	if sym.Kind == SymbolNamespace {
		_, ok := sym.Members[member]
		return ok
	}

	if sym.Kind == SymbolEnum {
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

	if strings.HasPrefix(sym.Type, "interface:") {
		ifaceName := strings.TrimPrefix(sym.Type, "interface:")
		ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName)
		if !ok {
			return false
		}
		_, ok = ifaceSym.Fields[member]
		return ok
	}

	if ifaceSym, ok := resolveInterfaceSymbol(scope, sym.Type); ok && ifaceSym.Kind == SymbolInterface {
		_, ok = ifaceSym.Fields[member]
		return ok
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

func shouldCheckMemberAccess(receiverType string) bool {
	receiverType = strings.TrimSpace(receiverType)

	if receiverType == "" {
		return false
	}

	if receiverType == "any" ||
		receiverType == "unknown" ||
		receiverType == "object" ||
		receiverType == "null" {
		return false
	}

	if strings.Contains(receiverType, "|") {
		for _, part := range splitUnionType(receiverType) {
			if part == "any" ||
				part == "unknown" ||
				part == "object" ||
				part == "null" {
				return false
			}
		}
	}

	return true
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

func pathToFileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}

	return "file:///" + filepath.ToSlash(path)
}

func resolveImportPath(currentURI string, importPath string) string {
	currentPath := URIToPath(currentURI)
	baseDir := filepath.Dir(currentPath)

	if filepath.IsAbs(importPath) {
		return importPath
	}

	return filepath.Join(baseDir, importPath)
}

type lspBaseScopeCacheEntry struct {
	text  string
	scope *Scope
}

type lspLineScopeCacheEntry struct {
	text  string
	scope *Scope
}

var lspBaseScopeCache = map[string]lspBaseScopeCacheEntry{}
var lspLineScopeCache = map[string]lspLineScopeCacheEntry{}

func fileBaseScope(uri string, text string) *Scope {
	path := filepath.Clean(URIToPath(uri))
	if cached, ok := lspBaseScopeCache[path]; ok && cached.text == text {
		return cached.scope
	}

	scope := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		resolvedPath := "std:" + module
		exports := loadTinyFileExports(resolvedPath, map[string]bool{})

		scope.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolNamespace,
			Type:      "namespace:" + alias,
			Detail:    "std module " + module,
			Members:   exports,
			SourceURI: pathToFileURI(resolvedPath),
		})
	}

	scanFileImportsIntoScope(scope, uri, text)

	lines := strings.Split(text, "\n")
	classBlocks := findBlocks(text, "class")

	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := cleanLine(lines[lineIndex])
		if line == "" {
			continue
		}

		scanEnumLine(scope, line, lineIndex+1, uri)
		scanClassLine(scope, line, lineIndex+1, uri)
		scanInterfaceLine(scope, line, lineIndex+1, uri)

		lineOffset := offsetAtLine(text, lineIndex+1)
		insideClass := blockInsideAny(lineOffset, classBlocks)

		if !insideClass {
			scanFunctionLine(scope, line, lineIndex+1, uri)
		}
	}

	scanFullInterfaces(scope, text, len(lines), uri)
	scanFullEnums(scope, text, len(lines), uri)
	scanFullClasses(scope, text, len(lines), uri)
	scanFullFunctions(scope, text, len(lines), uri)

	lspBaseScopeCache[path] = lspBaseScopeCacheEntry{
		text:  text,
		scope: scope,
	}

	return scope
}

func scopeAtPosition(uri string, text string, pos Position) *Scope {
	path := filepath.Clean(URIToPath(uri))
	lineKey := path + ":" + strconv.Itoa(pos.Line)

	if cached, ok := lspLineScopeCache[lineKey]; ok && cached.text == text {
		return cloneScope(cached.scope)
	}

	baseScope := fileBaseScope(uri, text)
	scope := cloneScope(baseScope)

	lines := strings.Split(text, "\n")
	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}
	if maxLine < 0 {
		maxLine = 0
	}

	className := classNameAtPosition(text, pos)
	if className != "" {
		if classSym, exists := resolveClassSymbol(scope, className); exists {
			scope.Define(SymbolInfo{
				Name:    "this",
				Kind:    SymbolVariable,
				Type:    "class:" + className,
				Detail:  "current class instance",
				Fields:  classSym.Fields,
				Methods: classSym.Methods,
			})
		}
	}

	scanAnonymousFunctions(scope, text, maxLine, uri)
	scanInlineAnonymousFunctionParams(scope, text, pos, uri)
	scanCatchVariables(scope, text, pos, uri)

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

	classBlocks := findBlocks(text, "class")
	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])
		if line == "" {
			continue
		}

		scanVariableLine(scope, line, lineIndex+1, uri)
		scanEmbedLine(scope, line, lineIndex+1, uri)

		lineOffset := offsetAtLine(text, lineIndex+1)
		if !blockInsideAny(lineOffset, classBlocks) {
			scanFieldLine(scope, line, lineIndex+1, uri)
		}
	}
	scanVariableDeclarations(scope, text, maxLine, uri)

	if ifLine, ok := findEnclosingIfBlock(text, pos); ok {
		applyTypeNarrowing(scope, ifLine)
	}

	scanLoopVariables(scope, text, pos.Line+1, uri)

	lspLineScopeCache[lineKey] = lspLineScopeCacheEntry{
		text:  text,
		scope: cloneScope(scope),
	}

	return scope
}

func findObjectTypeHintAtPosition(text string, pos Position) (string, bool) {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return "", false
	}

	depth := 0

	for i := pos.Line; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])

		if strings.Contains(line, "}") {
			depth--
		}
		if strings.Contains(line, "{") {
			depth++
		}

		if depth > 0 && strings.Contains(line, ":") && strings.Contains(line, "=") {
			match := regexp.MustCompile(`(?::\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*))\s*=`).FindStringSubmatch(line)
			if match != nil {
				return match[1], true
			}
		}
	}
	return "", false
}

func objectLiteralCompletions(scope *Scope, text string, pos Position) []CompletionItem {
	if !isCursorInsideObjectLiteral(text, pos) {
		return nil
	}

	typeName, ok := findObjectTypeHintAtPosition(text, pos)
	if !ok {
		typeName, ok = findFunctionArgumentTypeHint(scope, text, pos)
		if !ok {
			return nil
		}
	}

	var sym SymbolInfo
	var exists bool

	for _, part := range splitUnionType(typeName) {
		part = strings.TrimSpace(part)
		if isNullishLSPType(part) || part == "any" {
			continue
		}

		if strings.Contains(part, ".") {
			parts := strings.SplitN(part, ".", 2)
			nsName := parts[0]
			memberName := parts[1]

			ns, ok := scope.Resolve(nsName)
			if ok && ns.Kind == SymbolNamespace {
				sym, exists = ns.Members[memberName]
			}
		}

		if !exists {
			if iface, ok := resolveInterfaceSymbol(scope, part); ok {
				sym = iface
				exists = true
			} else if class, ok := resolveClassSymbol(scope, part); ok {
				sym = class
				exists = true
			}
		}

		if exists {
			break
		}
	}

	if !exists {
		return nil
	}

	items := []CompletionItem{}

	if sym.Kind == SymbolInterface {
		names := make([]string, 0, len(sym.Fields))
		for name := range sym.Fields {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			field := sym.Fields[name]
			items = append(items, CompletionItem{
				Label:            field.Name + ": ",
				Kind:             5,
				Detail:           "required interface field: " + field.Type,
				InsertText:       field.Name + ": $0",
				InsertTextFormat: 2,
			})
		}
	}

	if sym.Kind == SymbolClass {
		names := make([]string, 0, len(sym.Fields))
		for name := range sym.Fields {
			names = append(names, name)
		}
		sort.Strings(names)

		for _, name := range names {
			field := sym.Fields[name]
			items = append(items, CompletionItem{
				Label:            field.Name + ": ",
				Kind:             5,
				Detail:           "class field: " + field.Type,
				InsertText:       field.Name + ": $0",
				InsertTextFormat: 2,
			})
		}
	}

	return items
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

	// Check for async in the line
	if strings.Contains(line, "async ") {
		returnType = "task:" + returnType
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

func scanEnumLine(scope *Scope, line string, lineNumber int, uri string) {
	match := enumLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	enumName := match[1]
	body := match[2]

	members := map[string]SymbolInfo{}

	rawMembers := splitTopLevel(body, ',')
	for i, raw := range rawMembers {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}

		if strings.Contains(name, "=") {
			name = strings.TrimSpace(strings.SplitN(name, "=", 2)[0])
		}

		members[name] = SymbolInfo{
			Name:      name,
			Kind:      SymbolVariable,
			Type:      "any",
			Detail:    "enum member " + enumName + "." + name,
			Line:      lineNumber,
			Column:    indexColumn(line, name),
			SourceURI: uri,
		}

		_ = i
	}

	scope.Define(SymbolInfo{
		Name:      enumName,
		Kind:      SymbolEnum,
		Type:      "enum:" + enumName,
		Detail:    "enum " + enumName,
		Line:      lineNumber,
		Column:    indexColumn(line, enumName),
		SourceURI: uri,
		Members:   members,
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

func scanInterfaceLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := interfaceLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolInterface,
		Type:      "interface:" + name,
		Detail:    "interface " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
		Fields:    map[string]SymbolInfo{},
	})
}

func scanFullInterfaces(scope *Scope, text string, maxLine int, uri string) {
	interfaceBlocks := findBlocks(text, "interface")

	for _, block := range interfaceBlocks {
		if block.Line > maxLine+1 {
			continue
		}

		existing, _ := scope.Resolve(block.Name)
		existing.Name = block.Name
		existing.Kind = SymbolInterface
		existing.Type = "interface:" + block.Name
		existing.Detail = "interface " + block.Name
		existing.Line = block.Line
		existing.Column = block.Column
		existing.SourceURI = uri
		existing.Doc = findDocumentationComments(text, block.Line-1)
		if existing.Fields == nil {
			existing.Fields = map[string]SymbolInfo{}
		}
		scope.Define(existing)
	}

	for _, block := range interfaceBlocks {
		if block.Line > maxLine+1 {
			continue
		}

		fields := scanInterfaceFields(scope, block.Body, uri, block.Line)

		scope.Define(SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolInterface,
			Type:      "interface:" + block.Name,
			Detail:    "interface " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Fields:    fields,
			Doc:       findDocumentationComments(text, block.Line-1),
		})
	}
}

func scanInterfaceFields(scope *Scope, body string, uri string, baseLine int) map[string]SymbolInfo {
	fields := map[string]SymbolInfo{}
	lines := strings.Split(body, "\n")

	for i, raw := range lines {
		line := cleanLine(raw)
		if line == "" {
			continue
		}

		match := interfaceFieldRegex.FindStringSubmatch(line)
		if match == nil {
			continue
		}

		rawName := match[1]
		typeHint := strings.TrimSpace(match[2])

		isOptional := strings.HasSuffix(rawName, "?")
		name := strings.TrimSuffix(rawName, "?")

		typ := "any"
		if typeHint != "" {
			typ = normalizeLSPType(scope, typeHint)
		}

		if isOptional {
			typ = appendNullableLSPType(typ)
		}

		fields[name] = SymbolInfo{
			Name:      name,
			Kind:      SymbolField,
			Type:      typ,
			Detail:    "interface field " + name,
			Line:      baseLine + i,
			Column:    indexColumn(raw, name),
			SourceURI: uri,
		}
	}

	return fields
}

func scanFullEnums(scope *Scope, text string, maxLine int, uri string) {
	for _, block := range findBlocks(text, "enum") {
		if block.Line-1 > maxLine {
			continue
		}

		members := map[string]SymbolInfo{}
		for _, raw := range splitTopLevel(block.Body, ',') {
			memberName := strings.TrimSpace(raw)
			if memberName == "" {
				continue
			}

			if strings.Contains(memberName, "=") {
				memberName = strings.TrimSpace(strings.SplitN(memberName, "=", 2)[0])
			}
			if memberName == "" {
				continue
			}

			members[memberName] = SymbolInfo{
				Name:      memberName,
				Kind:      SymbolVariable,
				Type:      "any",
				Detail:    "enum member " + block.Name + "." + memberName,
				Line:      block.Line,
				Column:    block.Column,
				SourceURI: uri,
			}
		}

		scope.Define(SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolEnum,
			Type:      "enum:" + block.Name,
			Detail:    "enum " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Members:   members,
			Doc:       findDocumentationComments(text, block.Line-1),
		})
	}
}

func scanFieldLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.Replace(strings.Replace(strings.Replace(line, "private ", "", 1), "public ", "", 1), "const ", "", 1)

	isNullable := false
	if match := fieldNameWithQuestionRegex.FindStringSubmatch(line); match != nil {
		isNullable = true
		name := match[1]
		idx := strings.Index(line, name+"?")
		if idx >= 0 {
			line = line[:idx] + name + line[idx+len(name)+1:]
		}
	}

	match := fieldLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]

	if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
		return
	}

	typeHint := match[2]
	exprText := strings.TrimSpace(match[3])

	typ := "unknown"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		typ = inferExprTypeFromText(scope, exprText)
		typ = normalizeLSPType(scope, typ)
		if typ == "object" {
			fields = inferObjectFieldsFromText(scope, exprText, uri, lineNumber)
		}
	}

	if isNullable {
		if typ == "unknown" || typ == "null" {
			typ = "any | null"
		} else if !strings.Contains(typ, "null") {
			typ = typ + " | null"
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

func scanEmbedLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := embedLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	kind := match[1]
	name := match[3]

	typ := "string"
	if kind == "embedbin" {
		typ = "buffer"
	} else if kind == "embeddir" {
		typ = "object"
	}

	scope.Define(SymbolInfo{
		Name:      name,
		Kind:      SymbolVariable,
		Type:      typ,
		Detail:    kind + " " + name,
		Line:      lineNumber,
		Column:    indexColumn(line, name),
		SourceURI: uri,
	})
}

func scanVariableLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")
	match := variableLineRegex.FindStringSubmatch(line)
	if match == nil {
		return
	}

	name := match[1]

	if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
		return
	}

	typeHint := match[2]
	exprText := strings.TrimSpace(match[3])

	typ := "any"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		typ = inferExprTypeFromText(scope, exprText)
		typ = normalizeLSPType(scope, typ)
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

func scanVariableDeclarations(scope *Scope, text string, maxLine int, uri string) {
	lines := strings.Split(text, "\n")
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}

	startRegex := regexp.MustCompile(`^\s*(?:export\s+)?(?:let|const)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*(` + unionTypePattern + `))?\s*=\s*`)
	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		raw := lines[lineIndex]
		match := startRegex.FindStringSubmatchIndex(raw)
		if match == nil {
			continue
		}

		name := raw[match[2]:match[3]]
		typeHint := ""
		if match[4] >= 0 {
			typeHint = raw[match[4]:match[5]]
		}

		exprStart := offsetAtLine(text, lineIndex+1) + match[1]
		exprEnd := variableInitializerEnd(text, exprStart)
		if exprEnd < exprStart {
			continue
		}

		scanVariableDeclaration(scope, text, name, typeHint, strings.TrimSpace(text[exprStart:exprEnd]), lineIndex+1, indexColumn(raw, name), uri)
	}
}

func variableInitializerEnd(text string, start int) int {
	depth := 0
	inString := byte(0)
	escaped := false
	inLineComment := false
	lastNonSpace := start

	for i := start; i < len(text); i++ {
		ch := text[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				if depth == 0 {
					return lastNonSpace
				}
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
			lastNonSpace = i + 1
			continue
		}

		if i+1 < len(text) && ch == '/' && text[i+1] == '/' {
			inLineComment = true
			i++
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			lastNonSpace = i + 1
			continue
		}

		switch ch {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				return i
			}
		case '\n':
			if depth == 0 {
				return lastNonSpace
			}
		}

		if ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			lastNonSpace = i + 1
		}
	}

	return lastNonSpace
}

func scanVariableDeclaration(scope *Scope, sourceText string, name string, typeHint string, exprText string, lineNumber int, column int, uri string) {
	if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
		return
	}

	typ := "any"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		typ = inferExprTypeFromText(scope, exprText)
		if (typ == "" || typ == "any" || typ == "unknown") && sourceText != "" {
			if fallback, ok := inferNamespaceFunctionReturnFromText(scope, sourceText, exprText); ok {
				typ = fallback
			}
		}
		typ = normalizeLSPType(scope, typ)
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
		Column:    column,
		SourceURI: uri,
		Fields:    fields,
	})
}

func inferNamespaceFunctionReturnFromText(scope *Scope, text string, expr string) (string, bool) {
	match := memberCallRegex.FindStringSubmatch(expr)
	if match == nil {
		return "", false
	}

	receiver := match[1]
	member := match[2]
	for _, nsBlock := range findBlocks(text, "namespace") {
		if nsBlock.Name != receiver {
			continue
		}
		for _, fnBlock := range findBlocks(nsBlock.Body, "fn") {
			if fnBlock.Name == member {
				return firstNonEmpty(inferReturnTypeFromBody(scope, fnBlock.Body, fnBlock.ReturnType), "any"), true
			}
		}
	}

	return "", false
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

		if block.IsAsync {
			returnType = "task:" + returnType
		}

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
			Doc:       findDocumentationComments(text, block.Line-1),
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

		isNullable := false
		if match := classFieldNameWithQuestionRegex.FindStringSubmatch(line); match != nil {
			isNullable = true
			name := match[1]
			idx := strings.Index(line, name+"?")
			if idx >= 0 {
				line = line[:idx] + name + line[idx+len(name)+1:]
			}
		}

		fakeLine := "let " + line
		if !strings.Contains(fakeLine, "=") {
			fakeLine = strings.TrimSuffix(fakeLine, ";") + " = undefined"
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

		if isNullable {
			if typ == "unknown" || typ == "null" {
				typ = "any | null"
			} else if !strings.Contains(typ, "null") {
				typ = typ + " | null"
			}
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

func scanFullClasses(scope *Scope, text string, maxLine int, uri string) {
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
		existing.Doc = findDocumentationComments(text, block.Line-1)
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
		fields := scanClassFields(scope, block.Body, uri, block.Line)
		collectEmbeddedSymbolsFromBody(scope, block.Body, fields, methods, uri, block.Line)

		for _, methodBlock := range findBlocks(block.Body, "fn") {
			params := normalizeStdArgs(scope, parseFunctionParams(methodBlock.ParamsText))
			returnType := inferReturnTypeFromBody(scope, methodBlock.Body, methodBlock.ReturnType)

			if methodBlock.IsAsync {
				returnType = "task:" + returnType
			}

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
				Doc:       findDocumentationComments(text, block.Line+methodBlock.Line-2),
			}
		}

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
			Doc:       findDocumentationComments(text, block.Line-1),
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

func collectEmbeddedSymbolsFromBody(scope *Scope, classBody string, fields map[string]SymbolInfo, methods map[string]SymbolInfo, uri string, baseLine int) {
	matches := classEmbedRegex.FindAllStringSubmatch(classBody, -1)
	assignments := embeddedClassAssignmentsFromText(classBody)

	for _, match := range matches {
		embedName := match[1]

		embeddedSym, ok := resolveEmbeddedClassSymbol(scope, embedName, assignments[embedName])
		if !ok {
			continue
		}

		if _, exists := fields[embedName]; !exists {
			fields[embedName] = SymbolInfo{
				Name:      embedName,
				Kind:      SymbolField,
				Type:      "class:" + embeddedSym.Name,
				Detail:    "embed field " + embedName,
				Line:      baseLine + lineOffsetOfEmbeddedField(classBody, embedName),
				Column:    1,
				SourceURI: uri,
			}
		}

		for methodName, method := range embeddedSym.Methods {
			if _, exists := methods[methodName]; exists {
				continue
			}
			methods[methodName] = method
		}
	}
}

func embeddedClassAssignmentsFromText(text string) map[string]string {
	assignments := map[string]string{}
	re := regexp.MustCompile(`\bthis\.([A-Za-z_][A-Za-z0-9_]*)\s*=\s*([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	for _, match := range re.FindAllStringSubmatch(text, -1) {
		assignments[match[1]] = match[2]
	}
	return assignments
}

func lineOffsetOfEmbeddedField(text string, embedName string) int {
	lines := strings.Split(text, "\n")
	for i, raw := range lines {
		if classEmbedRegex.FindStringSubmatch(cleanLine(raw)) != nil && strings.Contains(raw, embedName) {
			return i
		}
	}
	return 0
}

func resolveEmbeddedClassSymbol(scope *Scope, embedName string, assignedClassName string) (SymbolInfo, bool) {
	for _, name := range embeddedClassCandidates(embedName, assignedClassName) {
		if sym, ok := scope.Resolve(name); ok && sym.Kind == SymbolClass {
			return sym, true
		}
	}
	return SymbolInfo{}, false
}

func embeddedClassCandidates(embedName string, assignedClassName string) []string {
	candidates := []string{}
	if assignedClassName != "" {
		candidates = append(candidates, assignedClassName)
	}
	candidates = append(candidates, embedName)
	if embedName != "" {
		candidates = append(candidates, strings.ToUpper(embedName[:1])+embedName[1:])
	}
	return candidates
}

func scanAnonymousFunctions(scope *Scope, text string, maxLine int, uri string) {
	lines := strings.Split(text, "\n")

	for i := 0; i <= maxLine && i < len(lines); i++ {
		line := cleanLine(lines[i])

		if !strings.Contains(line, "= fn") && !strings.Contains(line, "= async fn") && !strings.Contains(line, "= spawn") {
			continue
		}

		isSpawn := spawnFnRegex.MatchString(line)
		isNormalFn := strings.Contains(line, "= fn") || strings.Contains(line, "= async fn")

		if !isSpawn && !isNormalFn {
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

		if isSpawn || block.IsAsync {
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
	cacheKey := lspTextCacheKey(kind, text)
	if cached, ok := lspBlockCache[cacheKey]; ok {
		return cached.blocks
	}

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

	lspBlockCache[cacheKey] = lspBlockCacheEntry{blocks: blocks}
	return blocks
}

func parseFunctionLikeBlockAt(text string, start int, kind string) (blockInfo, bool) {
	isAsync := false
	if kind == "fn" {
		// Check for async before fn
		i := start - 1
		for i >= 0 && (text[i] == ' ' || text[i] == '\t') {
			i--
		}
		if i >= 4 && text[i-4:i+1] == "async" {
			if i-5 < 0 || !isIdentByte(text[i-5]) {
				isAsync = true
			}
		}
	}

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
		IsAsync:    isAsync,
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
		return "null"
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

func inferNullishCoalescingTypeFromText(scope *Scope, expr string) string {
	idx := strings.Index(expr, "??")
	if idx < 0 {
		return ""
	}

	leftExpr := strings.TrimSpace(expr[:idx])
	rightExpr := strings.TrimSpace(expr[idx+2:])

	leftType := inferExprTypeFromText(scope, leftExpr)
	rightType := inferExprTypeFromText(scope, rightExpr)

	if leftType == "unknown" {
		if rightType == "unknown" {
			return "unknown"
		}
		return rightType + " | unknown"
	}

	if rightType == "unknown" {
		return leftType
	}

	// Filter out nullish types from left side
	parts := splitUnionType(leftType)
	newParts := []string{}
	for _, p := range parts {
		if !isNullishLSPType(p) {
			newParts = append(newParts, p)
		}
	}

	if len(newParts) == 0 {
		return rightType
	}

	filteredLeft := strings.Join(newParts, " | ")
	if filteredLeft == rightType {
		return rightType
	}

	return filteredLeft + " | " + rightType
}

func inferExprTypeFromText(scope *Scope, expr string) string {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimSuffix(expr, ";")

	for strings.HasPrefix(expr, "(") && strings.HasSuffix(expr, ")") {
		if findMatching(expr, 0, '(', ')') == len(expr)-1 {
			expr = strings.TrimSpace(expr[1 : len(expr)-1])
		} else {
			break
		}
	}

	if expr == "" {
		return "unknown"
	}

	if strings.HasPrefix(expr, "await ") {
		inner := strings.TrimSpace(strings.TrimPrefix(expr, "await "))
		innerType := inferExprTypeFromText(scope, inner)
		if strings.HasPrefix(innerType, "task:") {
			return strings.TrimPrefix(innerType, "task:")
		}
		return innerType
	}

	if isQuotedLiteralOnly(expr) {
		return "string"
	}

	if strings.HasPrefix(expr, "[") && strings.HasSuffix(expr, "]") {
		inner := strings.TrimSpace(expr[1 : len(expr)-1])
		if inner == "" {
			return "array:empty"
		}
		parts := splitTopLevel(inner, ',')
		var elemTypes []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			elemType := inferExprTypeFromText(scope, part)
			if elemType != "" && elemType != "unknown" {
				elemTypes = append(elemTypes, elemType)
			}
		}
		if len(elemTypes) == 0 {
			return "array:any"
		}
		first := elemTypes[0]
		allSame := true
		for _, t := range elemTypes[1:] {
			if t != first {
				allSame = false
				break
			}
		}
		if allSame {
			return "array:" + first
		}
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

	if loc := spawnPrefixRegex.FindStringSubmatchIndex(expr); loc != nil {
		fnIndex := loc[2]
		block, ok := parseFunctionLikeBlockAt(expr, fnIndex, "fn")
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

	if typ := inferNullishCoalescingTypeFromText(scope, expr); typ != "" {
		return typ
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

	if typ := inferParsedExprTypeFromText(scope, expr); typ != "" {
		return typ
	}

	if sym, ok := scope.Resolve(expr); ok {
		return sym.Type
	}

	return "unknown"
}

func inferParsedExprTypeFromText(scope *Scope, expr string) string {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return ""
	}

	statements, diagnostics := parseTinyForLSP("lsp://expr", "const __tiny_lsp_expr = "+expr+";")
	if len(diagnostics) > 0 || len(statements) != 1 {
		return ""
	}

	variable, ok := statements[0].(VariableStmt)
	if !ok {
		return ""
	}

	analyzer := &astSemanticAnalyzer{uri: "lsp://expr", text: "", root: scope, scope: scope}
	typ := analyzer.inferExprType(variable.Value)
	typ = normalizeLSPType(scope, typ)

	if typ == "unknown" {
		return ""
	}
	return typ
}

func leadingCallConsumesExpr(expr string, openParen int) bool {
	if openParen < 0 || openParen >= len(expr) || expr[openParen] != '(' {
		return false
	}

	closeParen := findMatching(expr, openParen, '(', ')')
	if closeParen < 0 {
		return false
	}

	return strings.TrimSpace(expr[closeParen+1:]) == ""
}

func isQuotedLiteralOnly(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}

	quote := expr[0]
	if quote != '"' && quote != '\'' && quote != '`' {
		return false
	}

	escaped := false
	for i := 1; i < len(expr); i++ {
		ch := expr[i]
		if quote != '`' && escaped {
			escaped = false
			continue
		}
		if quote != '`' && ch == '\\' {
			escaped = true
			continue
		}
		if ch == quote {
			return strings.TrimSpace(expr[i+1:]) == ""
		}
	}

	return false
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
	matchIndex := memberCallRegex.FindStringSubmatchIndex(expr)
	if matchIndex == nil || !leadingCallConsumesExpr(expr, matchIndex[1]-1) {
		return ""
	}

	match := memberCallRegex.FindStringSubmatch(expr)
	if match == nil {
		return ""
	}

	receiver := match[1]
	method := match[2]

	if strings.Contains(receiver, ".") {
		if sym, receiverType, ok := resolveReceiverPath(scope, "", Position{}, receiver); ok {
			if sym.Kind == SymbolFunction {
				return firstNonEmpty(sym.Returns, "any")
			}
			if receiverType != "" {
				return inferMemberCallTypeByTypeString(scope, receiverType, method, sym.Fields)
			}
		}
	}

	sym, ok := scope.Resolve(receiver)
	if !ok {
		if typ, ok := namespaceFunctionReturnFromScope(scope, receiver, method); ok {
			return typ
		}
		return ""
	}

	if sym.Kind == SymbolNamespace {
		member, ok := sym.Members[method]
		if !ok {
			return ""
		}

		if member.Kind == SymbolFunction {
			ret := member.Returns
			if (sym.Type == "std:array" || sym.Detail == "std module array") && method == "from" {
				if openIdx := strings.Index(expr, "("); openIdx >= 0 {
					closeIdx := findMatching(expr, openIdx, '(', ')')
					if closeIdx > openIdx {
						argText := strings.TrimSpace(expr[openIdx+1 : closeIdx])
						if argText != "" {
							args := splitTopLevel(argText, ',')
							if len(args) > 0 {
								firstArg := strings.TrimSpace(args[0])
								argType := inferExprTypeFromText(scope, firstArg)
								if strings.HasPrefix(argType, "array:") {
									ret = argType
								} else if argType == "string" {
									ret = "array:string"
								} else if argType == "array" {
									ret = "array:any"
								}
							}
						}
					}
				}
			}
			return firstNonEmpty(ret, "any")
		}

		if member.Kind == SymbolClass {
			return "class:" + receiver + "." + member.Name
		}

		return member.Type
	}

	return inferMemberCallTypeByTypeString(scope, sym.Type, method, sym.Fields)
}

func namespaceFunctionReturnFromScope(scope *Scope, namespace string, member string) (string, bool) {
	for s := scope; s != nil; s = s.Parent {
		if ns, ok := s.Symbols[namespace]; ok && ns.Kind == SymbolNamespace {
			if memberSym, ok := ns.Members[member]; ok && memberSym.Kind == SymbolFunction {
				return firstNonEmpty(memberSym.Returns, "any"), true
			}
		}
	}
	return "", false
}

func inferMemberCallTypeByTypeString(scope *Scope, typ string, method string, fields map[string]SymbolInfo) string {
	typ = strings.TrimSpace(typ)

	if strings.Contains(typ, "|") {
		for _, part := range splitUnionType(typ) {
			if isNullishLSPType(part) {
				continue
			}

			result := inferMemberCallTypeByTypeString(scope, part, method, fields)
			if result != "" && result != "unknown" {
				return result
			}
		}

		return ""
	}

	if strings.HasPrefix(typ, "task:") {
		if method == "await" {
			return strings.TrimPrefix(typ, "task:")
		}
		return ""
	}

	if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")

		classSym, ok := resolveClassSymbol(scope, className)
		if !ok || classSym.Kind != SymbolClass {
			return ""
		}

		methodSym, ok := classSym.Methods[method]
		if !ok {
			return ""
		}

		return firstNonEmpty(methodSym.Returns, "any")
	}

	if strings.HasPrefix(typ, "std:") {
		module := strings.TrimPrefix(typ, "std:")

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

	methodInfo, ok := GetNativeMethodInfo(typ, method)
	if ok {
		return methodInfo.Returns
	}

	if typ == "object" && fields != nil {
		if field, ok := fields[method]; ok {
			return field.Type
		}
	}

	return ""
}

func inferNormalCallTypeFromText(scope *Scope, expr string) string {
	matchIndex := normalCallRegex.FindStringSubmatchIndex(expr)
	if matchIndex == nil || !leadingCallConsumesExpr(expr, matchIndex[1]-1) {
		return ""
	}

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

func appendNullableLSPType(typ string) string {
	parts := splitUnionType(typ)

	hasNull := false

	for _, part := range parts {
		part = strings.TrimSpace(part)

		if part == "null" {
			hasNull = true
		}
	}

	if !hasNull {
		parts = append(parts, "null")
	}

	return strings.Join(parts, " | ")
}

func parseFunctionParams(paramsText string) []StdArg {
	params := []StdArg{}
	rawParams := splitTopLevel(paramsText, ',')

	for _, raw := range rawParams {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}

		hasDefault := false
		var defaultValueText string
		if strings.Contains(raw, "=") {
			parts := strings.SplitN(raw, "=", 2)
			raw = strings.TrimSpace(parts[0])
			defaultValueText = strings.TrimSpace(parts[1])
			hasDefault = true
		}

		isVariadic := false
		if strings.HasPrefix(raw, "...") {
			isVariadic = true
			raw = strings.TrimSpace(strings.TrimPrefix(raw, "..."))
		}

		name := raw
		typ := "any"
		nullable := false

		if strings.Contains(raw, ":") {
			parts := strings.SplitN(raw, ":", 2)
			name = strings.TrimSpace(parts[0])
			typ = strings.TrimSpace(parts[1])
		}

		if strings.HasSuffix(name, "?") {
			nullable = true
			name = strings.TrimSpace(strings.TrimSuffix(name, "?"))
		}

		if hasDefault {
			if defaultValueText == "null" {
				nullable = true
			}
			if typ == "any" && defaultValueText != "" {
				if strings.HasPrefix(defaultValueText, "\"") || strings.HasPrefix(defaultValueText, "'") || strings.HasPrefix(defaultValueText, "`") {
					typ = "string"
				} else if defaultValueText == "true" || defaultValueText == "false" {
					typ = "bool"
				} else if defaultValueText == "null" {
					typ = "null"
				} else {
					numText := defaultValueText
					if strings.HasPrefix(numText, "-") {
						numText = strings.TrimSpace(strings.TrimPrefix(numText, "-"))
					}
					if _, err := strconv.ParseFloat(numText, 64); err == nil {
						typ = "number"
					}
				}
			}
		}

		if isVariadic {
			typ = "array"
		} else if nullable {
			typ = appendNullableLSPType(typ)
		}

		params = append(params, StdArg{
			Name:     name,
			Type:     typ,
			Optional: nullable || hasDefault,
			Variadic: isVariadic,
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

type lspImportCacheEntry struct {
	text    string
	exports map[string]SymbolInfo
}

var lspImportExportCache = map[string]lspImportCacheEntry{}

func invalidateLSPImportCacheForURI(uri string) {
	invalidateLSPImportCacheForURIRecursive(uri, map[string]bool{})
}

func invalidateLSPImportCacheForURIRecursive(uri string, visited map[string]bool) {
	path := filepath.Clean(URIToPath(uri))
	if visited[path] {
		return
	}
	visited[path] = true

	delete(lspImportExportCache, path)
	delete(lspBaseScopeCache, path)

	for key := range lspLineScopeCache {
		if strings.HasPrefix(key, path+":") {
			delete(lspLineScopeCache, key)
		}
	}

	for docURI, docText := range lspDocs {
		docPath := filepath.Clean(URIToPath(docURI))
		if docPath == path {
			continue
		}
		if documentImportsPath(docURI, docText, path, map[string]bool{}) {
			invalidateLSPImportCacheForURIRecursive(docURI, visited)
		}
	}
}

func scanExportedInterfaces(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	for _, block := range findBlocks(text, "interface") {
		if !hasExportBefore(text, block.Start) {
			continue
		}

		fields := scanInterfaceFields(scope, block.Body, uri, block.Line)

		sym := SymbolInfo{
			Name:      block.Name,
			Kind:      SymbolInterface,
			Type:      "interface:" + block.Name,
			Detail:    "export interface " + block.Name,
			Line:      block.Line,
			Column:    block.Column,
			SourceURI: uri,
			Fields:    fields,
			Doc:       findDocumentationComments(text, block.Line-1),
		}

		exports[block.Name] = sym
		scope.Define(sym)
	}
}

func loadTinyFileExports(path string, visited map[string]bool) map[string]SymbolInfo {
	exports := map[string]SymbolInfo{}

	if strings.HasPrefix(path, "std:") {
		uri := pathToFileURI(path)
		text, ok := tinyFileTextForLSP(path, uri)
		if !ok {
			return exports
		}

		cacheKey := "std:" + strings.TrimPrefix(path, "std:")
		if cached, ok := lspImportExportCache[cacheKey]; ok && cached.text == text {
			return cloneSymbolMap(cached.exports)
		}

		statements, _ := parseTinyForLSP(uri, text)
		if statements == nil {
			return exports
		}

		scope := NewScope(nil)
		for alias, module := range parseStdImports(text) {
			scope.Define(SymbolInfo{
				Name: alias, Kind: SymbolStd, Type: "std:" + module,
				Detail: "std module " + module, SourceURI: uri,
			})
		}

		for _, raw := range statements {
			stmt, exported := unwrapExport(raw)

			switch s := stmt.(type) {
			case InterfaceStmt:
				detail := "interface " + s.Name
				if !exported {
					detail = "private " + detail
				}
				sym := SymbolInfo{
					Name:      s.Name,
					Kind:      SymbolInterface,
					Type:      "interface:" + s.Name,
					Detail:    detail,
					Line:      s.Line,
					Column:    s.Column,
					SourceURI: uri,
					Fields:    map[string]SymbolInfo{},
					Doc:       findDocumentationComments(text, s.Line-1),
				}
				for fieldName, fieldHint := range s.Fields {
					sym.Fields[fieldName] = SymbolInfo{
						Name:      fieldName,
						Kind:      SymbolField,
						Type:      normalizeLSPType(scope, fieldHint.Name),
						Detail:    "interface field " + fieldName,
						Line:      s.Line,
						SourceURI: uri,
					}
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case FunctionStmt:
				detail := "fn " + s.Name
				if !exported {
					detail = "private " + detail
				}
				sym := SymbolInfo{
					Name:      s.Name,
					Kind:      SymbolFunction,
					Type:      "function",
					Detail:    detail,
					Line:      s.Line,
					Column:    s.Column,
					SourceURI: uri,
					Params:    stdArgsFromParams(scope, s.Params),
					Returns:   returnTypeNameScoped(scope, s.ReturnType),
					Doc:       findDocumentationComments(text, s.Line-1),
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case ClassStmt:
				sym := classSymbolFromStmt(scope, s, uri, text)
				if !exported {
					sym.Detail = "private " + sym.Detail // Tag as private!
				}
				exports[s.Name] = sym
				scope.Define(sym)
			}
		}

		lspImportExportCache[cacheKey] = lspImportCacheEntry{
			text:    text,
			exports: cloneSymbolMap(exports),
		}
		return exports
	}

	abs, err := filepath.Abs(path)
	if err != nil {
		return exports
	}

	if visited[abs] {
		return exports
	}
	visited[abs] = true

	uri := pathToFileURI(abs)
	text, ok := tinyFileTextForLSP(abs, uri)
	if !ok {
		return exports
	}

	cacheKey := filepath.Clean(abs)
	if cached, ok := lspImportExportCache[cacheKey]; ok && cached.text == text {
		return cloneSymbolMap(cached.exports)
	}

	scope := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		resolvedPath := "std:" + module
		exports := loadTinyFileExports(resolvedPath, map[string]bool{})

		scope.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolNamespace,
			Type:      "namespace:" + alias,
			Detail:    "std module " + module,
			Members:   exports,
			SourceURI: pathToFileURI(resolvedPath),
		})
	}

	scanFileImportsIntoScopeWithVisited(scope, uri, text, visited)

	collectExportsFromAST(scope, text, exports, uri)

	scanExportedEnums(scope, text, exports, uri)
	scanExportedClasses(scope, text, exports, uri)
	scanExportedFunctions(scope, text, exports, uri)
	scanExportedInterfaces(scope, text, exports, uri)

	for _, sym := range exports {
		scope.Define(sym)
	}

	scanExportedVariables(scope, text, exports, uri)
	scanExportedEmbeds(scope, text, exports, uri)

	lspImportExportCache[cacheKey] = lspImportCacheEntry{
		text:    text,
		exports: cloneSymbolMap(exports),
	}

	return exports
}

func cloneSymbolMap(in map[string]SymbolInfo) map[string]SymbolInfo {
	out := make(map[string]SymbolInfo, len(in))
	for name, sym := range in {
		if sym.Fields != nil {
			sym.Fields = cloneSymbolMap(sym.Fields)
		}
		if sym.Methods != nil {
			sym.Methods = cloneSymbolMap(sym.Methods)
		}
		if sym.Members != nil {
			sym.Members = cloneSymbolMap(sym.Members)
		}
		out[name] = sym
	}
	return out
}

func collectExportsFromAST(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	statements, diagnostics := parseTinyForLSP(uri, text)
	if len(diagnostics) > 0 || statements == nil {
		return
	}

	for _, raw := range statements {
		stmt, exported := unwrapExport(raw)

		switch s := stmt.(type) {
		case ClassStmt:
			sym := classSymbolFromStmt(scope, s, uri, text)
			if !exported {
				sym.Detail = "private " + sym.Detail
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case NativeFnStmt:
			detail := "native fn " + s.Name
			if !exported {
				detail = "private " + detail
			}
			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolFunction,
				Type:      "function",
				Detail:    detail,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: uri,
				Params:    stdArgsFromParams(scope, s.Params),
				Returns:   returnTypeNameScoped(scope, s.ReturnType),
				Doc:       findDocumentationComments(text, s.Line-1),
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case InterfaceStmt:
			detail := "interface " + s.Name
			if !exported {
				detail = "private " + detail
			}
			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolInterface,
				Type:      "interface:" + s.Name,
				Detail:    detail,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: uri,
				Fields:    map[string]SymbolInfo{},
				Doc:       findDocumentationComments(text, s.Line-1),
			}
			for fieldName, fieldHint := range s.Fields {
				sym.Fields[fieldName] = SymbolInfo{
					Name:      fieldName,
					Kind:      SymbolField,
					Type:      normalizeLSPType(scope, fieldHint.Name),
					Detail:    "interface field " + fieldName,
					Line:      s.Line,
					SourceURI: uri,
				}
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case EnumStmt:
			sym := enumSymbolFromStmt(s, uri)
			if !exported {
				sym.Detail = "private " + sym.Detail
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case FunctionStmt:
			detail := "fn " + s.Name
			if !exported {
				detail = "private " + detail
			}
			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolFunction,
				Type:      "function",
				Detail:    detail,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: uri,
				Params:    stdArgsFromParams(scope, s.Params),
				Returns:   returnTypeNameScoped(scope, s.ReturnType),
				Doc:       findDocumentationComments(text, s.Line-1),
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case EmbedStmt:
			typ := s.TypeHint.Name
			if typ == "" {
				typ = "string"
			}
			detail := "variable " + s.Name
			if !exported {
				detail = "private " + detail
			}
			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolVariable,
				Type:      typ,
				Detail:    detail,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: uri,
			}
			exports[s.Name] = sym
			scope.Define(sym)

		case VariableStmt:
			typ := "unknown"
			fields := map[string]SymbolInfo(nil)
			if !s.TypeHint.IsEmpty() {
				typ = normalizeLSPType(scope, s.TypeHint.Name)
			} else {
				analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: scope, scope: scope}

				typ = analyzer.inferExprType(s.Value)

				if typ == "object" {
					fields = inferObjectFieldsFromText(scope, "", uri, s.Line)
				}
			}

			detail := "variable " + s.Name
			if !exported {
				detail = "private " + detail
			}

			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolVariable,
				Type:      typ,
				Detail:    detail,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: uri,
				Fields:    fields,
			}
			exports[s.Name] = sym
			scope.Define(sym)
		}
	}
}

func classSymbolFromStmt(scope *Scope, cls ClassStmt, uri string, text string) SymbolInfo {
	fields := map[string]SymbolInfo{}
	for _, f := range cls.Fields {
		typ := typeHintName(f.TypeHint, "any")
		if typ == "any" && f.Value != nil {
			analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: scope, scope: scope}

			typ = analyzer.inferExprType(f.Value)
		} else {
			typ = normalizeLSPType(scope, typ)
		}

		detail := "field " + f.Name
		if f.Private {
			detail = "private " + detail
		}
		if f.Constant {
			detail = "const " + detail
		}

		fields[f.Name] = SymbolInfo{
			Name:      f.Name,
			Kind:      SymbolField,
			Type:      typ,
			Detail:    detail,
			Line:      f.Line,
			Column:    f.Column,
			SourceURI: uri,
			Doc:       findDocumentationComments(text, f.Line-1),
		}
	}

	methods := map[string]SymbolInfo{}
	for _, m := range cls.Methods {
		detail := "method " + cls.Name + "." + m.Name
		if m.Private {
			detail = "private " + detail
		}
		methods[m.Name] = SymbolInfo{
			Name:      m.Name,
			Kind:      SymbolFunction,
			Type:      "function",
			Detail:    detail,
			Line:      m.Line,
			Column:    m.Column,
			SourceURI: uri,
			Params:    stdArgsFromParams(scope, m.Params),
			Returns:   returnTypeNameScoped(scope, m.ReturnType),
			Doc:       findDocumentationComments(text, m.Line-1),
		}
	}
	collectEmbeddedSymbolsFromAST(scope, cls.Embeds, cls.Methods, fields, methods, uri, cls.Line)

	return SymbolInfo{
		Name:      cls.Name,
		Kind:      SymbolClass,
		Type:      "class:" + cls.Name,
		Detail:    "export class " + cls.Name,
		Line:      cls.Line,
		Column:    cls.Column,
		SourceURI: uri,
		Fields:    fields,
		Methods:   methods,
		Doc:       findDocumentationComments(text, cls.Line-1),
	}
}

func enumSymbolFromStmt(enum EnumStmt, uri string) SymbolInfo {
	members := map[string]SymbolInfo{}

	for _, member := range enum.Members {
		members[member.Name] = SymbolInfo{
			Name:      member.Name,
			Kind:      SymbolVariable,
			Type:      "any",
			Detail:    "enum member " + enum.Name + "." + member.Name,
			Line:      enum.Line,
			Column:    enum.Column,
			SourceURI: uri,
		}
	}

	return SymbolInfo{
		Name:      enum.Name,
		Kind:      SymbolEnum,
		Type:      "enum:" + enum.Name,
		Detail:    "export enum " + enum.Name,
		Line:      enum.Line,
		Column:    enum.Column,
		SourceURI: uri,
		Members:   members,
	}
}

func collectEmbeddedSymbolsFromAST(scope *Scope, embeds []string, methods []FunctionStmt, fields map[string]SymbolInfo, methodSymbols map[string]SymbolInfo, uri string, line int) {
	assignments := embeddedClassAssignmentsFromMethods(methods)
	for _, embedName := range embeds {
		embeddedSym, ok := resolveEmbeddedClassSymbol(scope, embedName, assignments[embedName])
		if !ok {
			continue
		}

		if _, exists := fields[embedName]; !exists {
			fields[embedName] = SymbolInfo{
				Name:      embedName,
				Kind:      SymbolField,
				Type:      "class:" + embeddedSym.Name,
				Detail:    "embed field " + embedName,
				Line:      line,
				Column:    1,
				SourceURI: uri,
			}
		}

		for methodName, method := range embeddedSym.Methods {
			if _, exists := methodSymbols[methodName]; exists {
				continue
			}
			methodSymbols[methodName] = method
		}
	}
}

func embeddedClassAssignmentsFromMethods(methods []FunctionStmt) map[string]string {
	assignments := map[string]string{}
	for _, method := range methods {
		collectEmbeddedClassAssignmentsFromStmts(method.Body, assignments)
	}
	return assignments
}

func collectEmbeddedClassAssignmentsFromStmts(stmts []Stmt, assignments map[string]string) {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		switch s := stmt.(type) {
		case PropertyAssignStmt:
			if _, ok := s.Object.(ThisExpr); ok {
				if className := classNameFromConstructorExpr(s.Value); className != "" {
					assignments[s.Name] = className
				}
			}
			collectEmbeddedClassAssignmentsFromExpr(s.Value, assignments)
		case VariableStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Value, assignments)
		case ExprStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Value, assignments)
		case ReturnStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Value, assignments)
		case IfStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Condition, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.ThenBody, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.ElseBody, assignments)
		case WhileStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Condition, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.Body, assignments)
		case ForStmt:
			if s.Init != nil {
				collectEmbeddedClassAssignmentsFromStmts([]Stmt{s.Init}, assignments)
			}
			collectEmbeddedClassAssignmentsFromExpr(s.Condition, assignments)
			if s.Update != nil {
				collectEmbeddedClassAssignmentsFromStmts([]Stmt{s.Update}, assignments)
			}
			collectEmbeddedClassAssignmentsFromStmts(s.Body, assignments)
		case ForInStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Iterable, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.Body, assignments)
		case TryCatchStmt:
			collectEmbeddedClassAssignmentsFromStmts(s.TryBody, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.CatchBody, assignments)
			collectEmbeddedClassAssignmentsFromStmts(s.FinallyBody, assignments)
		case MatchStmt:
			collectEmbeddedClassAssignmentsFromExpr(s.Value, assignments)
			for _, c := range s.Cases {
				collectEmbeddedClassAssignmentsFromExpr(c.Value, assignments)
				collectEmbeddedClassAssignmentsFromStmts(c.Body, assignments)
			}
			collectEmbeddedClassAssignmentsFromStmts(s.Default, assignments)
		}
	}
}

func collectEmbeddedClassAssignmentsFromExpr(expr Expr, assignments map[string]string) {
	switch e := expr.(type) {
	case CallValueExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Callee, assignments)
		for _, arg := range e.Args {
			collectEmbeddedClassAssignmentsFromExpr(arg, assignments)
		}
	case MemberCallExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Object, assignments)
		for _, arg := range e.Args {
			collectEmbeddedClassAssignmentsFromExpr(arg, assignments)
		}
	case PropertyExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Object, assignments)
	case ArrayExpr:
		for _, item := range e.Elements {
			collectEmbeddedClassAssignmentsFromExpr(item, assignments)
		}
	case ObjectExpr:
		for _, field := range e.Fields {
			collectEmbeddedClassAssignmentsFromExpr(field.Value, assignments)
		}
	case BinaryExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Left, assignments)
		collectEmbeddedClassAssignmentsFromExpr(e.Right, assignments)
	case TernaryExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Condition, assignments)
		collectEmbeddedClassAssignmentsFromExpr(e.ThenExpr, assignments)
		collectEmbeddedClassAssignmentsFromExpr(e.ElseExpr, assignments)
	case UnaryExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Right, assignments)
	case IndexExpr:
		collectEmbeddedClassAssignmentsFromExpr(e.Object, assignments)
		collectEmbeddedClassAssignmentsFromExpr(e.Index, assignments)
	}
}

func classNameFromConstructorExpr(expr Expr) string {
	switch e := expr.(type) {
	case CallExpr:
		return e.Name
	case CallValueExpr:
		if ident, ok := e.Callee.(IdentExpr); ok {
			return ident.Name
		}
	}
	return ""
}

func tinyFileTextForLSP(path string, uri string) (string, bool) {
	if strings.Contains(path, "std:") || strings.Contains(uri, "std:") {
		moduleName := ""
		if idx := strings.Index(path, "std:"); idx >= 0 {
			moduleName = path[idx+4:]
		} else if idx := strings.Index(uri, "std:"); idx >= 0 {
			moduleName = uri[idx+4:]
		}

		moduleName = strings.TrimSuffix(moduleName, ".tiny")
		moduleName = strings.ReplaceAll(moduleName, "/", "")
		moduleName = strings.ReplaceAll(moduleName, "\\", "")
		moduleName = strings.TrimSpace(moduleName)

		bytes, err := lspStubs.ReadFile("lsp_stubs/" + moduleName + ".tiny")
		if err != nil {
			return "", false
		}
		return string(bytes), true
	}

	if text, ok := lspDocs[uri]; ok {
		return text, true
	}

	normalizedPath := filepath.Clean(path)
	for openURI, text := range lspDocs {
		if filepath.Clean(URIToPath(openURI)) == normalizedPath {
			return text, true
		}
	}

	bytes, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}

	return string(bytes), true
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

	libraryMatches := libraryImportRegex.FindAllStringSubmatch(text, -1)
	for _, match := range libraryMatches {
		importPath := match[1]
		alias := ""

		if len(match) > 2 {
			alias = match[2]
		}
		if alias == "" {
			alias = defaultLibraryAlias(importPath)
		}

		resolved := resolveLibraryImportPath(importPath, currentURI)
		exports := loadTinyFileExports(resolved, visited)

		scope.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolNamespace,
			Type:      "namespace:" + alias,
			Detail:    "library " + importPath,
			Members:   exports,
			SourceURI: pathToFileURI(resolved),
		})
	}
}

func scanExportedEnums(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	matches := exportedEnumBlockRegex.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		fullStart := match[0]
		enumName := text[match[2]:match[3]]
		body := text[match[4]:match[5]]
		lineNumber := lineNumberAtOffset(text, fullStart)
		line := getLine(text, lineNumber-1)

		defineExportedEnum(scope, exports, uri, enumName, body, lineNumber, indexColumn(line, enumName))
	}
}

func defineExportedEnum(scope *Scope, exports map[string]SymbolInfo, uri string, enumName string, body string, lineNumber int, column int) {
	members := map[string]SymbolInfo{}

	for _, raw := range splitTopLevel(body, ',') {
		memberName := strings.TrimSpace(raw)
		if memberName == "" {
			continue
		}

		if strings.Contains(memberName, "=") {
			memberName = strings.TrimSpace(strings.SplitN(memberName, "=", 2)[0])
		}

		members[memberName] = SymbolInfo{
			Name:      memberName,
			Kind:      SymbolVariable,
			Type:      "number",
			Detail:    "enum member " + enumName + "." + memberName,
			Line:      lineNumber,
			Column:    column,
			SourceURI: uri,
		}
	}

	sym := SymbolInfo{
		Name:      enumName,
		Kind:      SymbolEnum,
		Type:      "enum:" + enumName,
		Detail:    "export enum " + enumName,
		Line:      lineNumber,
		Column:    column,
		SourceURI: uri,
		Members:   members,
	}

	exports[enumName] = sym
	scope.Define(sym)
}

func scanExportedFunctions(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	for _, block := range findBlocks(text, "fn") {
		if !hasExportBefore(text, block.Start) {
			continue
		}

		params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
		returnType := inferReturnTypeFromBody(scope, block.Body, block.ReturnType)

		if block.IsAsync {
			returnType = "task:" + returnType
		}

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
			Doc:       findDocumentationComments(text, block.Line-1),
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
		fields := scanClassFields(scope, block.Body, uri, block.Line)
		collectEmbeddedSymbolsFromBody(scope, block.Body, fields, methods, uri, block.Line)

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
				Doc:       findDocumentationComments(text, block.Line+methodBlock.Line-2),
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
			Fields:    fields,
			Doc:       findDocumentationComments(text, block.Line-1),
		}

		exports[block.Name] = sym
		scope.Define(sym)
	}
}

func scanExportedEmbeds(scope *Scope, text string, exports map[string]SymbolInfo, uri string) {
	lines := strings.Split(text, "\n")

	for i, rawLine := range lines {
		line := cleanLine(rawLine)
		if !strings.HasPrefix(line, "export ") {
			continue
		}

		withoutExport := strings.TrimSpace(strings.TrimPrefix(line, "export "))
		match := embedLineRegex.FindStringSubmatch(withoutExport)
		if match == nil {
			continue
		}

		kind := match[1]
		name := match[3]

		typ := "string"
		if kind == "embedbin" {
			typ = "buffer"
		} else if kind == "embeddir" {
			typ = "object"
		}

		sym := SymbolInfo{
			Name:      name,
			Kind:      SymbolVariable,
			Type:      typ,
			Detail:    "export " + kind + " " + name,
			Line:      i + 1,
			Column:    indexColumn(line, name),
			SourceURI: uri,
		}

		exports[name] = sym
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

		if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
			continue
		}

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

	if i > 0 && line[i-1] == '?' {
		i--
	}

	receiverEnd := i
	i--
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}

	parenDepth := 0
	bracketDepth := 0

	for i >= 0 {
		ch := line[i]

		if ch == ')' {
			parenDepth++
			i--
			continue
		}
		if ch == '(' {
			if parenDepth == 0 {
				break
			}
			parenDepth--
			i--
			continue
		}
		if ch == ']' {
			bracketDepth++
			i--
			continue
		}
		if ch == '[' {
			if bracketDepth == 0 {
				break
			}
			bracketDepth--
			i--
			continue
		}

		if parenDepth > 0 || bracketDepth > 0 {
			i--
			continue
		}

		if isIdentChar(ch) || ch == '.' || ch == '?' {
			i--
			continue
		}
		break
	}

	receiver := strings.TrimSpace(line[i+1 : receiverEnd])
	receiver = strings.TrimSuffix(receiver, "?")
	if receiver == "" {
		return "", "", false
	}

	return receiver, method, true
}

func isIdentChar(ch byte) bool {
	return isIdentByte(ch)
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

var inlineAnonFnRegex = regexp.MustCompile(`fn\s*\(([^)]*)\)\s*\{`)

func scanInlineAnonymousFunctionParams(scope *Scope, text string, pos Position, uri string) {
	lines := strings.Split(text, "\n")
	cursorLine := pos.Line + 1

	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}

	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])

		// Find all matches with their indices on the line
		matches := inlineAnonFnRegex.FindAllStringSubmatchIndex(line, -1)

		for _, matchIndices := range matches {
			lineStartOffset := offsetAtLine(text, lineIndex+1)
			openBraceInLine := matchIndices[1] - 1 // '{' is the last character of the match "fn(...) {"
			openBraceOffset := lineStartOffset + openBraceInLine

			// Check if we actually have '{' at that offset
			if openBraceOffset >= len(text) || text[openBraceOffset] != '{' {
				// Fallback search for '{' after the end of "fn(...)"
				openBraceOffset = -1
				matchEndOffset := lineStartOffset + matchIndices[1]
				for idx := matchEndOffset - 1; idx < len(text); idx++ {
					if text[idx] == '{' {
						openBraceOffset = idx
						break
					}
				}
			}

			if openBraceOffset < 0 {
				continue
			}

			closeBraceOffset := findMatching(text, openBraceOffset, '{', '}')
			if closeBraceOffset < 0 {
				closeBraceOffset = len(text)
			}

			startLine := lineNumberAtOffset(text, openBraceOffset)
			endLine := lineNumberAtOffset(text, closeBraceOffset)

			// Only register the parameters if the cursor is inside the function body
			if cursorLine >= startLine && cursorLine <= endLine {
				paramsText := line[matchIndices[2]:matchIndices[3]]
				params := parseFunctionParams(paramsText)

				for _, param := range params {
					var paramType string

					if param.Variadic {
						paramType = "array"
					} else {
						paramType = normalizeLSPType(scope, param.Type)
					}

					scope.Define(SymbolInfo{
						Name:      param.Name,
						Kind:      SymbolVariable,
						Type:      paramType,
						Detail:    "anonymous function parameter " + param.Name,
						Line:      lineIndex + 1,
						Column:    indexColumn(line, param.Name),
						SourceURI: uri,
					})
				}
			}
		}
	}
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
	line := getLine(text, pos.Line)
	if isPositionInStringOrComment(line, pos.Character) {
		return nil
	}

	word := wordAtPosition(text, pos)

	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	receiver, member, ok := memberExprAtPosition(text, pos)
	if ok {
		receiverSym, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
		if !exists {
			return nil
		}

		receiverType = unwrapNullableType(receiverType)

		if receiverSym.Kind == SymbolNamespace || receiverSym.Kind == SymbolEnum {
			memberSym, ok := receiverSym.Members[member]
			if !ok {
				return nil
			}

			if memberSym.Kind == SymbolFunction {
				signature := formatFunctionSignature(receiver+"."+memberSym.Name, memberSym.Params, memberSym.Returns)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(memberSym.Detail, memberSym.Doc)}}
			}
			if memberSym.Kind == SymbolClass {
				constructor := constructorSymbolFromClass(memberSym, receiver+"."+memberSym.Name)
				signature := formatFunctionSignature(constructor.Name, constructor.Params, constructor.Returns)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(constructor.Detail, memberSym.Doc)}}
			}

			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + receiver + "." + memberSym.Name + "**\n\nType: `" + firstNonEmpty(memberSym.Type, "any") + "`\n\n" + appendDoc(memberSym.Detail, memberSym.Doc)}}
		}

		if strings.HasPrefix(receiverType, "class:") {
			className := strings.TrimPrefix(receiverType, "class:")
			classSym, ok := resolveClassSymbol(scope, className)
			if !ok || classSym.Kind != SymbolClass {
				return nil
			}

			if methodSym, ok := classSym.Methods[member]; ok {
				signature := formatFunctionSignature(className+"."+methodSym.Name, methodSym.Params, methodSym.Returns)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(methodSym.Detail, methodSym.Doc)}}
			}

			if fieldSym, ok := classSym.Fields[member]; ok {
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + receiver + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)}}
			}
		}

		if strings.HasPrefix(receiverType, "interface:") {
			ifaceName := strings.TrimPrefix(receiverType, "interface:")
			ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName)
			if !ok {
				return nil
			}
			if fieldSym, ok := ifaceSym.Fields[member]; ok {
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + receiver + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)}}
			}
		}

		if strings.HasPrefix(receiverType, "std:") {
			module := strings.TrimPrefix(receiverType, "std:")
			info, ok := GetStdModuleInfo(module)
			if !ok {
				return nil
			}

			methodInfo, ok := info.Methods[member]
			if !ok {
				return nil
			}

			signature := formatStdSignature(module, methodInfo)
			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description}}
		}

		if strings.HasPrefix(receiverType, "task:") && member == "await" {
			returnType := strings.TrimPrefix(receiverType, "task:")
			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\nawait task: " + returnType + "\n```\nWaits for the task and returns its result."}}
		}

		methodInfo, ok := GetNativeMethodInfo(receiverType, member)
		if ok {
			signature := formatNativeSignature(receiverType, methodInfo)
			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description}}
		}

		return nil
	}

	if hover, ok := hoverForDeclarationMember(scope, text, pos, word); ok {
		return hover
	}

	sym, exists := scope.Resolve(word)
	if !exists {
		className := classNameAtPosition(text, pos)
		if className != "" {
			if classSym, ok := resolveClassSymbol(scope, className); ok && classSym.Kind == SymbolClass {
				if methodSym, ok := classSym.Methods[word]; ok {
					signature := formatFunctionSignature(className+"."+methodSym.Name, methodSym.Params, methodSym.Returns)
					return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(methodSym.Detail, methodSym.Doc)}}
				}
				if fieldSym, ok := classSym.Fields[word]; ok {
					return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + className + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)}}
				}
			}
		}
		return nil
	}

	if sym.Kind == SymbolFunction {
		signature := formatFunctionSignature(sym.Name, sym.Params, sym.Returns)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(sym.Detail, sym.Doc)}}
	}
	if sym.Kind == SymbolClass {
		constructor := constructorSymbolFromClass(sym, sym.Name)
		signature := formatFunctionSignature(constructor.Name, constructor.Params, constructor.Returns)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(constructor.Detail, sym.Doc)}}
	}

	return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + sym.Name + "**\n\nType: `" + sym.Type + "`\n\n" + appendDoc(sym.Detail, sym.Doc)}}
}

func hoverForDeclarationMember(scope *Scope, text string, pos Position, word string) (HoverResult, bool) {
	line := pos.Line + 1

	if ifaceName := interfaceNameAtPosition(text, pos); ifaceName != "" {
		if ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName); ok && ifaceSym.Kind == SymbolInterface {
			if fieldSym, ok := ifaceSym.Fields[word]; ok && fieldSym.Line == line {
				value := "**" + ifaceName + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: value}}, true
			}
		}
	}

	if className := classNameAtPosition(text, pos); className != "" {
		if classSym, ok := resolveClassSymbol(scope, className); ok && classSym.Kind == SymbolClass {
			if methodSym, ok := classSym.Methods[word]; ok && methodSym.Line == line {
				signature := formatFunctionSignature(className+"."+methodSym.Name, methodSym.Params, methodSym.Returns)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(methodSym.Detail, methodSym.Doc)}}, true
			}
			if fieldSym, ok := classSym.Fields[word]; ok && fieldSym.Line == line {
				value := "**" + className + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: value}}, true
			}
		}
	}

	return HoverResult{}, false
}

type astSemanticAnalyzer struct {
	uri               string
	text              string
	root              *Scope
	scope             *Scope
	diagnostics       []map[string]any
	currentClass      string
	currentReturnType string
}

func semanticDiagnosticsFromAST(uri string, text string) []map[string]any {
	statements, parseDiagnostics := parseTinyForLSP(URIToPath(uri), text)
	if len(parseDiagnostics) > 0 || statements == nil {
		return []map[string]any{}
	}

	root := NewScope(nil)

	for alias, module := range parseStdImports(text) {
		resolvedPath := "std:" + module
		exports := loadTinyFileExports(resolvedPath, map[string]bool{})

		root.Define(SymbolInfo{
			Name:      alias,
			Kind:      SymbolNamespace,
			Type:      "namespace:" + alias,
			Detail:    "std module " + module,
			Line:      1,
			Column:    1,
			Members:   exports,
			SourceURI: pathToFileURI(resolvedPath),
		})
	}

	scanFileImportsIntoScope(root, uri, text)

	for i, rawLine := range strings.Split(text, "\n") {
		scanEnumLine(root, cleanLine(rawLine), i+1, uri)
	}

	a := &astSemanticAnalyzer{uri: uri, text: text, root: root, scope: root}
	a.predeclareStatements(statements)
	a.visitStatements(statements)
	a.addUnusedSymbolDiagnostics(text, statements)

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
	name := extractNameFromMessage(message)

	validPosition := false
	if line > 0 && column > 0 && name != "" {
		lineIndex := line - 1
		colIndex := column - 1
		lineText := getLine(a.text, lineIndex)

		if colIndex >= 0 && colIndex+len(name) <= len(lineText) {
			if lineText[colIndex:colIndex+len(name)] == name {
				validPosition = true
			}
		}
	} else if line > 0 && column > 0 && name == "" {
		validPosition = true
	}

	if !validPosition && name != "" {
		if line > 0 {
			lineText := getLine(a.text, line-1)
			code := stripLineComment(lineText)
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if match := re.FindStringIndex(code); match != nil {
				column = match[0] + 1
				validPosition = true
			}
		}
		if !validPosition {
			line, column = findWordFirstOccurrence(a.text, name)
		}
	}

	if line <= 0 || column <= 0 {
		return
	}

	lineIndex := line - 1
	colIndex := column - 1

	lineText := getLine(a.text, lineIndex)
	wordLen := wordLengthAtColumn(lineText, colIndex)

	a.diagnostics = append(a.diagnostics, makeRangeDiagnostic(
		lineIndex,
		colIndex,
		colIndex+wordLen,
		2,
		message,
	))
}

func (a *astSemanticAnalyzer) addError(line int, column int, message string) {
	name := extractNameFromMessage(message)

	validPosition := false
	if line > 0 && column > 0 && name != "" {
		lineIndex := line - 1
		colIndex := column - 1
		lineText := getLine(a.text, lineIndex)

		if colIndex >= 0 && colIndex+len(name) <= len(lineText) {
			if lineText[colIndex:colIndex+len(name)] == name {
				validPosition = true
			}
		}
	} else if line > 0 && column > 0 && name == "" {
		validPosition = true
	}

	if !validPosition && name != "" {
		if line > 0 {
			lineText := getLine(a.text, line-1)
			code := stripLineComment(lineText)
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if match := re.FindStringIndex(code); match != nil {
				column = match[0] + 1
				validPosition = true
			}
		}
		if !validPosition {
			line, column = findWordFirstOccurrence(a.text, name)
		}
	}

	if line <= 0 || column <= 0 {
		return
	}

	lineIndex := line - 1
	colIndex := column - 1

	lineText := getLine(a.text, lineIndex)
	wordLen := wordLengthAtColumn(lineText, colIndex)

	a.diagnostics = append(a.diagnostics, makeRangeDiagnostic(
		lineIndex,
		colIndex,
		colIndex+wordLen,
		1,
		message,
	))
}

type unusedSymbolDecl struct {
	name string
	kind string
	line int
	col  int
}

func (a *astSemanticAnalyzer) addUnusedSymbolDiagnostics(text string, statements []Stmt) {
	decls := collectUnusedSymbolDecls(statements, false, false)
	for _, decl := range decls {
		if decl.name == "" || decl.name == "_" || tinyKeywords[decl.name] {
			continue
		}
		uses := len(identifierRangesInText(text, decl.name))
		limit := 1
		if decl.kind == "import" {
			limit = 0
		}
		if uses <= limit {
			line := decl.line
			col := decl.col
			if line <= 0 || col <= 0 {
				line, col = firstIdentifierPosition(text, decl.name)
			}

			lineText := getLine(text, line-1)
			realCol := findIdentifierColumn(lineText, decl.name, col)

			a.diagnostics = append(a.diagnostics, makeRangeDiagnostic(
				line-1,
				realCol-1,
				realCol-1+len(decl.name),
				2,
				"unused "+decl.kind+": "+decl.name,
			))
		}
	}
}

func firstIdentifierPosition(text string, name string) (int, int) {
	ranges := identifierRangesInText(text, name)
	if len(ranges) == 0 {
		return 1, 1
	}
	return ranges[0].Line + 1, ranges[0].Start + 1
}

func collectUnusedSymbolDecls(stmts []Stmt, exported bool, inClass bool) []unusedSymbolDecl {
	decls := []unusedSymbolDecl{}
	for _, raw := range stmts {
		stmt, stmtExported := unwrapExport(raw)
		isExported := exported || stmtExported

		switch s := stmt.(type) {
		case ImportStmt:
			alias := s.Alias
			if alias == "" {
				if s.Std {
					alias = s.Path
				} else if s.Library {
					alias = defaultLibraryAlias(s.Path)
				} else {
					alias = strings.TrimSuffix(filepath.Base(s.Path), filepath.Ext(s.Path))
				}
			}
			decls = append(decls, unusedSymbolDecl{name: alias, kind: "import", line: s.Line, col: s.Column})

		case VariableStmt:
			if !isExported {
				decls = append(decls, unusedSymbolDecl{name: s.Name, kind: "variable", line: s.Line, col: s.Column})
			}

		case FunctionStmt:
			if !isExported && !inClass {
				decls = append(decls, unusedSymbolDecl{name: s.Name, kind: "function", line: s.Line, col: s.Column})
			}

		case ClassStmt:
			if !isExported {
				decls = append(decls, unusedSymbolDecl{name: s.Name, kind: "class", line: s.Line, col: s.Column})
			}
			for _, field := range s.Fields {
				if field.Private {
					decls = append(decls, unusedSymbolDecl{name: field.Name, kind: "private field", line: field.Line, col: field.Column})
				}
			}
			for _, method := range s.Methods {
				if method.Private {
					decls = append(decls, unusedSymbolDecl{name: method.Name, kind: "private method", line: method.Line, col: method.Column})
				}
			}

		case NamespaceStmt:
			decls = append(decls, collectUnusedSymbolDecls(s.Statements, isExported, false)...)

		case ForStmt:
			if s.Init != nil {
				decls = append(decls, collectUnusedSymbolDecls([]Stmt{s.Init}, isExported, false)...)
			}
			decls = append(decls, collectUnusedSymbolDecls(s.Body, isExported, false)...)

		case ForInStmt:
			decls = append(decls, unusedSymbolDecl{name: s.ItemName, kind: "variable", line: s.Line, col: s.Column})
			if s.IndexName != "" {
				decls = append(decls, unusedSymbolDecl{name: s.IndexName, kind: "variable", line: s.Line, col: s.Column})
			}
			decls = append(decls, collectUnusedSymbolDecls(s.Body, isExported, false)...)

		case IfStmt:
			decls = append(decls, collectUnusedSymbolDecls(s.ThenBody, isExported, false)...)
			decls = append(decls, collectUnusedSymbolDecls(s.ElseBody, isExported, false)...)

		case WhileStmt:
			decls = append(decls, collectUnusedSymbolDecls(s.Body, isExported, false)...)

		case TryCatchStmt:
			decls = append(decls, collectUnusedSymbolDecls(s.TryBody, isExported, false)...)
			decls = append(decls, unusedSymbolDecl{name: s.ErrorName, kind: "variable", line: s.Line, col: s.Column})
			decls = append(decls, collectUnusedSymbolDecls(s.CatchBody, isExported, false)...)
			decls = append(decls, collectUnusedSymbolDecls(s.FinallyBody, isExported, false)...)
		}
	}
	return decls
}

func stdArgsFromParams(scope *Scope, params []Param) []StdArg {
	args := []StdArg{}

	for _, p := range params {
		name := p.Name
		typ := typeHintName(p.TypeHint, "any")
		if typ == "any" && p.HasDefault {
			typ = TypeName(p.DefaultValue)
			if typ == "float" {
				typ = "number"
			}
		}

		if p.Variadic {
			typ = "array"
		} else {
			typ = normalizeLSPType(scope, typ)
		}

		args = append(args, StdArg{
			Name:     name,
			Type:     typ,
			Optional: p.HasDefault,
			Variadic: p.Variadic,
		})
	}

	return args
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
				if s.Library {
					alias = defaultLibraryAlias(s.Path)
				} else {
					alias = s.Path
				}
			}

			if s.Std {
				break
			}

			if !s.Plugin {
				importPath := ""
				if s.Library {
					importPath = resolveLibraryImportPath(s.Path, a.uri)
				} else {
					importPath = resolveImportPath(a.uri, s.Path)
				}
				if _, err := os.Stat(importPath); err != nil {
					a.addDiagnostic(s.Line, s.Column, "import file not found: "+s.Path)
				}
			}

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

		case NativeFnStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolFunction,
				Type:      "function",
				Detail:    "native fn " + s.Name,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: a.uri,
				Params:    stdArgsFromParams(a.scope, s.Params),
				Returns:   returnTypeNameScoped(a.root, s.ReturnType),
			})

		case FunctionStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, s.Params), Returns: returnTypeNameScoped(a.root, s.ReturnType)})

		case ClassStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(a.classSymbol(s))

		case VariableStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

		case EnumStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(enumSymbolFromStmt(s, a.uri))

		case EmbedStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: s.TypeHint.Name, Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

		case InterfaceStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			sym := SymbolInfo{
				Name:      s.Name,
				Kind:      SymbolInterface,
				Type:      "interface:" + s.Name,
				Detail:    "interface " + s.Name,
				Line:      s.Line,
				Column:    s.Column,
				SourceURI: a.uri,
				Fields:    map[string]SymbolInfo{},
			}

			for fieldName, fieldHint := range s.Fields {
				sym.Fields[fieldName] = SymbolInfo{
					Name:      fieldName,
					Kind:      SymbolField,
					Type:      normalizeLSPType(a.root, fieldHint.Name),
					Detail:    "interface field " + fieldName,
					Line:      s.Line,
					SourceURI: a.uri,
				}
			}
			a.root.Define(sym)

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
				case EnumStmt:
					enumSym := enumSymbolFromStmt(m, a.uri)
					enumSym.Type = "enum:" + s.Name + "." + m.Name
					enumSym.Detail = "enum " + m.Name
					members[m.Name] = enumSym
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
		detail := "method " + cls.Name + "." + m.Name
		if m.Private {
			detail = "private " + detail
		}
		methods[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolFunction, Type: "function", Detail: detail, Line: m.Line, Column: m.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, m.Params), Returns: returnTypeNameScoped(a.root, m.ReturnType)}
	}
	collectEmbeddedSymbolsFromAST(a.root, cls.Embeds, cls.Methods, fields, methods, a.uri, cls.Line)

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

	case VariableStmt:
		a.validateTypeHint(s.TypeHint, s.Line, s.Column)
		typ := a.inferExprType(s.Value)
		if !s.TypeHint.IsEmpty() {
			typ = normalizeLSPType(a.root, s.TypeHint.Name)
		} else {
			typ = normalizeLSPType(a.root, typ)
		}
		fields := map[string]SymbolInfo(nil)
		if typ == "object" {
			fields = a.fieldsFromObjectExpr(s.Value, s.Line)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Fields: fields})

	case FieldStmt:
		a.validateTypeHint(s.TypeHint, s.Line, s.Column)
		typ := typeHintName(s.TypeHint, "any")
		if typ == "any" {
			typ = a.inferExprType(s.Value)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "field " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

	case FunctionStmt:
		a.visitFunction(s)

	case NativeFnStmt:

	case ClassStmt:
		for _, f := range s.Fields {
			a.validateTypeHint(f.TypeHint, f.Line, f.Column)
			if f.Value != nil {
				a.inferExprType(f.Value)
			}
		}
		a.define(a.classSymbol(s))
		for _, m := range s.Methods {
			a.visitMethod(s.Name, m)
		}

	case NamespaceStmt:
		a.pushScope()
		a.visitStatements(s.Statements)
		a.popScope()

	case EnumStmt:
		a.define(enumSymbolFromStmt(s, a.uri))

	case ExprStmt:
		a.inferExprType(s.Value)

	case AssignStmt:
		if _, ok := a.resolve(s.Name); !ok {
			a.addError(s.Line, s.Column, "undefined variable: "+s.Name)
		}
		a.inferExprType(s.Value)

	case ReturnStmt:
		if s.HasValue {
			returnedType := a.inferExprType(s.Value)

			if a.currentReturnType != "" && a.currentReturnType != "any" {
				if !a.compareLSPTypes(returnedType, a.currentReturnType) {
					a.addDiagnostic(s.Line, s.Column, fmt.Sprintf("cannot return type '%s' from this function (expected '%s')", returnedType, a.currentReturnType))
				}
			}
		} else {
			if a.currentReturnType != "" && a.currentReturnType != "any" && a.currentReturnType != "null" {
				a.addDiagnostic(s.Line, s.Column, fmt.Sprintf("cannot return empty value from this function (expected '%s')", a.currentReturnType))
			}
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
	oldReturn := a.currentReturnType
	a.currentReturnType = returnTypeNameScoped(a.root, fn.ReturnType)

	a.pushScope()
	for _, p := range fn.Params {
		a.validateTypeHint(p.TypeHint, fn.Line, fn.Column)
	}
	a.validateTypeHint(fn.ReturnType, fn.Line, fn.Column)
	for _, p := range fn.Params {
		a.define(paramSymbol(a.scope, p, a.uri, fn.Line, fn.Column))
	}
	a.visitStatements(fn.Body)
	a.popScope()

	a.currentReturnType = oldReturn
}

func (a *astSemanticAnalyzer) visitMethod(className string, fn FunctionStmt) {
	oldClass := a.currentClass
	a.currentClass = className

	oldReturn := a.currentReturnType
	a.currentReturnType = returnTypeNameScoped(a.root, fn.ReturnType)

	a.pushScope()
	a.define(SymbolInfo{Name: "this", Kind: SymbolVariable, Type: "class:" + className, Detail: "current class instance", Line: fn.Line, Column: fn.Column, SourceURI: a.uri})
	for _, p := range fn.Params {
		a.validateTypeHint(p.TypeHint, fn.Line, fn.Column)
	}
	a.validateTypeHint(fn.ReturnType, fn.Line, fn.Column)
	for _, p := range fn.Params {
		if p.Name != "this" {
			a.define(paramSymbol(a.scope, p, a.uri, fn.Line, fn.Column))
		}
	}
	a.visitStatements(fn.Body)
	a.popScope()
	a.currentClass = oldClass
	a.currentReturnType = oldReturn
}

func (a *astSemanticAnalyzer) validateTypeHint(hint TypeHint, line int, column int) {
	if hint.IsEmpty() {
		return
	}

	for _, part := range splitUnionType(hint.Name) {
		if !a.typeNameExists(part) {
			a.addDiagnostic(line, column, "unknown type: "+part)
		}
	}
}

func (a *astSemanticAnalyzer) typeNameExists(typ string) bool {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return true
	}

	if strings.HasPrefix(typ, "array:") {
		return a.typeNameExists(strings.TrimPrefix(typ, "array:"))
	}

	switch typ {
	case "string", "number", "bool", "object", "array", "any", "null", "function", "error", "buffer":
		return true
	}

	if sym, ok := a.root.Resolve(typ); ok {
		return sym.Kind == SymbolClass || sym.Kind == SymbolEnum || sym.Kind == SymbolNamespace || sym.Kind == SymbolInterface
	}

	if strings.Contains(typ, ".") {
		parts := strings.SplitN(typ, ".", 2)
		ns, ok := a.root.Resolve(parts[0])
		if !ok || ns.Kind != SymbolNamespace {
			return false
		}

		member, ok := ns.Members[parts[1]]
		return ok && (member.Kind == SymbolClass || member.Kind == SymbolEnum || member.Kind == SymbolInterface)
	}

	return false
}

func paramSymbol(scope *Scope, param Param, uri string, line int, column int) SymbolInfo {
	typ := typeHintName(param.TypeHint, "any")
	if typ == "any" && param.HasDefault {
		typ = TypeName(param.DefaultValue)
		if typ == "float" {
			typ = "number"
		}
	}

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

	if strings.HasPrefix(typ, "array:") {
		elem := strings.TrimPrefix(typ, "array:")
		return "array:" + normalizeLSPType(scope, elem)
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
	case "string", "number", "bool", "object", "array", "any", "null", "function", "error", "buffer":
		if typ == "array" {
			return "array:any"
		}
		return typ
	}

	if sym, ok := scope.Resolve(typ); ok {
		switch sym.Kind {
		case SymbolClass:
			return "class:" + typ
		case SymbolInterface:
			return "interface:" + typ
		case SymbolEnum:
			return "enum:" + typ
		}
	}

	if strings.Contains(typ, ".") {
		parts := strings.SplitN(typ, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			member, ok := ns.Members[memberName]
			if ok {
				switch member.Kind {
				case SymbolClass:
					return "class:" + typ
				case SymbolInterface:
					return "interface:" + typ
				case SymbolEnum:
					return "enum:" + typ
				}
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
	case ArrayExpr:
		if len(e.Elements) == 0 {
			return "array:empty"
		}
		var elemTypes []string
		for _, el := range e.Elements {
			elemType := a.inferExprType(el)
			if elemType != "" && elemType != "unknown" {
				elemTypes = append(elemTypes, elemType)
			}
		}
		if len(elemTypes) == 0 {
			return "array:any"
		}
		first := elemTypes[0]
		allSame := true
		for _, t := range elemTypes[1:] {
			if t != first {
				allSame = false
				break
			}
		}
		if allSame {
			return "array:" + first
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
			a.addError(e.Line, e.Column, "undefined variable: "+e.Name)
		}
		return "unknown"
	case ThisExpr:
		if sym, ok := a.resolve("this"); ok {
			return sym.Type
		}
		a.addDiagnostic(e.Line, e.Column, "cannot use this outside of a method")
		return "unknown"
	case NullishCoalescingExpr:
		leftType := a.inferExprType(e.Left)
		rightType := a.inferExprType(e.Right)

		if leftType == "unknown" {
			if rightType == "unknown" {
				return "unknown"
			}
			return rightType + " | unknown"
		}

		if rightType == "unknown" {
			return leftType
		}

		// Filter out nullish types from left side
		parts := splitUnionType(leftType)
		newParts := []string{}
		for _, p := range parts {
			if !isNullishLSPType(p) {
				newParts = append(newParts, p)
			}
		}

		if len(newParts) == 0 {
			return rightType
		}

		filteredLeft := strings.Join(newParts, " | ")
		if filteredLeft == rightType {
			return rightType
		}

		return filteredLeft + " | " + rightType

	case PropertyExpr:
		if ident, ok := e.Object.(IdentExpr); ok {
			if sym, exists := a.resolve(ident.Name); exists && sym.Kind == SymbolNamespace {
				memberSym, ok := sym.Members[e.Name]
				if !ok {
					a.addDiagnostic(e.Line, e.Column, "undefined export: "+ident.Name+"."+e.Name)
					return "unknown"
				}

				if memberSym.Kind == SymbolClass {
					return "class:" + ident.Name + "." + memberSym.Name
				}

				if memberSym.Kind == SymbolEnum {
					return "enum:" + ident.Name + "." + memberSym.Name
				}

				return memberSym.Type
			}
		}

		a.checkMember(e.Object, e.Name, e.Line, e.Column)
		objType := a.inferExprType(e.Object)
		return a.memberType(objType, e.Name)
	case CallValueExpr:
		for _, arg := range e.Args {
			a.inferExprType(arg)
		}

		switch callee := e.Callee.(type) {
		case IdentExpr:
			if sym, ok := a.resolve(callee.Name); ok {
				if sym.Kind == SymbolClass {
					return "class:" + sym.Name
				}

				if sym.Kind == SymbolFunction {
					a.checkArgumentCount(callee.Name, len(e.Args), sym.Params, e.Line, e.Column)
					a.checkArgumentTypes(callee.Name, e.Args, sym.Params, e.Line, e.Column)

					return firstNonEmpty(sym.Returns, "any")
				}

				return sym.Type
			}

		case PropertyExpr:
			objType := a.inferExprType(callee.Object)

			// Namespace call: models.User()
			if ident, ok := callee.Object.(IdentExpr); ok {
				if ns, exists := a.resolve(ident.Name); exists && ns.Kind == SymbolNamespace {
					memberSym, ok := ns.Members[callee.Name]
					if !ok {
						return "unknown"
					}

					if memberSym.Kind == SymbolClass {
						return "class:" + ident.Name + "." + memberSym.Name
					}

					if memberSym.Kind == SymbolFunction {
						return firstNonEmpty(memberSym.Returns, "any")
					}

					return memberSym.Type
				}
			}

			return a.memberType(objType, callee.Name)
		}

		return a.inferExprType(e.Callee)

	case MemberCallExpr:
		if ident, ok := e.Object.(IdentExpr); ok {
			if sym, exists := a.resolve(ident.Name); exists && sym.Kind == SymbolNamespace {
				memberSym, ok := sym.Members[e.Method]
				if !ok {
					a.addDiagnostic(e.Line, e.Column, "undefined export: "+ident.Name+"."+e.Method)
					return "unknown"
				}

				for _, arg := range e.Args {
					a.inferExprType(arg)
				}

				if memberSym.Kind == SymbolClass {
					return "class:" + ident.Name + "." + memberSym.Name
				}

				if memberSym.Kind == SymbolEnum {
					return "enum:" + ident.Name + "." + memberSym.Name
				}

				if memberSym.Kind == SymbolFunction {

					a.checkArgumentCount(ident.Name+"."+e.Method, len(e.Args), memberSym.Params, e.Line, e.Column)
					a.checkArgumentTypes(ident.Name+"."+e.Method, e.Args, memberSym.Params, e.Line, e.Column)

					ret := memberSym.Returns
					if (sym.Type == "std:array" || sym.Detail == "std module array") && e.Method == "from" && len(e.Args) > 0 {
						argType := a.inferExprType(e.Args[0])
						if strings.HasPrefix(argType, "array:") {
							ret = argType
						} else if argType == "string" {
							ret = "array:string"
						} else if argType == "array" {
							ret = "array:any"
						}
					}

					if ret != "" {
						return ret
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
			if a.privateMemberByType(objType, e.Method) && !a.canAccessPrivateMember(e.Object, objType) {
				a.addDiagnostic(e.Line, e.Column, "private member is not accessible: "+e.Method)
			} else if !a.memberExistsByType(objType, e.Method) {
				a.addDiagnostic(e.Line, e.Column, "undefined method or property: "+e.Method)
			}
		}
		a.checkKnownMemberCall(objType, e.Method, e.Args, e.Line, e.Column)

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
				a.checkArgumentCount(e.Name, len(e.Args), sym.Params, e.Line, e.Column)

				a.checkArgumentTypes(e.Name, e.Args, sym.Params, e.Line, e.Column)

				if sym.Returns != "" {
					return sym.Returns
				}
				return "any"
			}
			return sym.Type
		}
		a.addError(e.Line, e.Column, "undefined variable: "+e.Name)
		return "unknown"
	case FunctionExpr:
		a.pushScope()
		for _, p := range e.Params {
			a.validateTypeHint(p.TypeHint, e.Line, e.Column)
		}
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
		objType := a.inferExprType(e.Object)
		a.inferExprType(e.Index)
		if strings.HasPrefix(objType, "array:") {
			return strings.TrimPrefix(objType, "array:")
		}
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

func (a *astSemanticAnalyzer) checkKnownMemberCall(receiverType string, method string, args []Expr, line int, column int) {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" || strings.Contains(receiverType, "|") {
		return
	}

	if strings.HasPrefix(receiverType, "class:") {
		className := strings.TrimPrefix(receiverType, "class:")
		if classSym, ok := resolveClassSymbol(a.root, className); ok {
			if methodSym, ok := classSym.Methods[method]; ok {
				a.checkArgumentCount(className+"."+method, len(args), methodSym.Params, line, column)
				a.checkArgumentTypes(className+"."+method, args, methodSym.Params, line, column)
			}
		}
		return
	}

	if methodInfo, ok := GetNativeMethodInfo(receiverType, method); ok {
		a.checkArgumentCount(receiverType+"."+method, len(args), methodInfo.Args, line, column)
		a.checkArgumentTypes(receiverType+"."+method, args, methodInfo.Args, line, column)
	}
}

func (a *astSemanticAnalyzer) checkArgumentCount(name string, got int, params []StdArg, line int, column int) {
	required := 0
	variadic := false
	if len(params) > 0 && params[len(params)-1].Variadic {
		variadic = true
	}
	for _, param := range params {
		if !param.Optional && !param.Variadic {
			required++
		}
	}
	if variadic {
		if got < required {
			expected := strconv.Itoa(required) + "+"
			a.addError(line, column, "wrong argument count for "+name+": expected "+expected+", got "+strconv.Itoa(got))
		}
		return
	}
	if got < required || got > len(params) {
		expected := strconv.Itoa(len(params))
		if required != len(params) {
			expected = strconv.Itoa(required) + "-" + strconv.Itoa(len(params))
		}
		a.addError(line, column, "wrong argument count for "+name+": expected "+expected+", got "+strconv.Itoa(got))
	}
}

func (a *astSemanticAnalyzer) checkMember(object Expr, member string, line int, column int) {
	if ident, ok := object.(IdentExpr); ok {
		if sym, exists := a.resolve(ident.Name); exists {
			if sym.Type == "object" {
				return
			}

			if memberExistsOnSymbol(a.root, sym, member) {
				if a.privateMemberByType(sym.Type, member) && !a.canAccessPrivateMember(object, sym.Type) {
					a.addDiagnostic(line, column, "private member is not accessible: "+member)
				}
				return
			}

			if !shouldCheckMemberAccess(sym.Type) {
				return
			}

			a.addDiagnostic(line, column, "undefined method or property: "+member)
			return
		}
	}

	objType := a.inferExprType(object)
	if !shouldCheckMemberAccess(objType) {
		return
	}
	if a.privateMemberByType(objType, member) && !a.canAccessPrivateMember(object, objType) {
		a.addDiagnostic(line, column, "private member is not accessible: "+member)
		return
	}
	if !a.memberExistsByType(objType, member) {
		a.addDiagnostic(line, column, "undefined method or property: "+member)
	}
}

func (a *astSemanticAnalyzer) canAccessPrivateMember(object Expr, typ string) bool {
	className := strings.TrimPrefix(strings.TrimSpace(typ), "class:")
	if className == typ {
		if sym, ok := resolveClassSymbol(a.root, typ); ok && sym.Kind == SymbolClass {
			className = sym.Name
		}
	}
	if className == "" || a.currentClass == "" || className != a.currentClass {
		return false
	}
	_, ok := object.(ThisExpr)
	return ok
}

func (a *astSemanticAnalyzer) privateMemberByType(typ string, member string) bool {
	typ = strings.TrimSpace(typ)
	if strings.Contains(typ, "|") {
		for _, part := range splitUnionType(typ) {
			if a.privateMemberByType(part, member) {
				return true
			}
		}
		return false
	}

	if sym, ok := resolveClassSymbol(a.root, typ); ok && sym.Kind == SymbolClass {
		typ = "class:" + typ
	}
	if !strings.HasPrefix(typ, "class:") {
		return false
	}

	classSym, ok := resolveClassSymbol(a.root, strings.TrimPrefix(typ, "class:"))
	if !ok || classSym.Kind != SymbolClass {
		return false
	}
	if methodSym, ok := classSym.Methods[member]; ok {
		return isPrivateSymbol(methodSym)
	}
	if fieldSym, ok := classSym.Fields[member]; ok {
		return isPrivateSymbol(fieldSym)
	}
	return false
}

func (a *astSemanticAnalyzer) memberExistsByType(typ string, member string) bool {
	typ = strings.TrimSpace(typ)

	if typ == "" || typ == "any" || typ == "unknown" || typ == "null" {
		return true
	}

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

	if typ == "object" {
		return true
	}

	if typ == "error" {
		return member == "kind" || member == "message" || member == "toString"
	}

	if strings.HasPrefix(typ, "task:") {
		return member == "await"
	}

	if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		ifaceSym, ok := resolveInterfaceSymbol(a.root, ifaceName)
		if !ok {
			return false
		}
		_, ok = ifaceSym.Fields[member]
		return ok
	}

	if ifaceSym, ok := resolveInterfaceSymbol(a.root, typ); ok && ifaceSym.Kind == SymbolInterface {
		_, ok = ifaceSym.Fields[member]
		return ok
	}

	if strings.HasPrefix(typ, "enum:") {
		enumName := strings.TrimPrefix(typ, "enum:")
		enumSym, ok := resolveEnumSymbol(a.root, enumName)
		if !ok || enumSym.Kind != SymbolEnum {
			return false
		}

		_, ok = enumSym.Members[member]
		return ok
	}

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

	if _, ok := GetNativeMethodInfo(typ, member); ok {
		return true
	}

	if member == "toString" {
		return true
	}

	return false
}

func (a *astSemanticAnalyzer) memberType(typ string, member string) string {
	typ = strings.TrimSpace(typ)

	if typ == "" || typ == "any" || typ == "unknown" {
		return "any"
	}

	if strings.Contains(typ, "|") {
		for _, part := range splitUnionType(typ) {
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

	if typ == "object" {
		return "any"
	}

	if typ == "error" {
		switch member {
		case "kind", "message":
			return "string"
		case "toString":
			return "function"
		default:
			return "unknown"
		}
	}

	if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		ifaceSym, ok := resolveInterfaceSymbol(a.root, ifaceName)
		if !ok {
			return "unknown"
		}
		if fieldSym, ok := ifaceSym.Fields[member]; ok {
			return firstNonEmpty(fieldSym.Type, "any")
		}
		return "unknown"
	}

	if ifaceSym, ok := resolveInterfaceSymbol(a.root, typ); ok && ifaceSym.Kind == SymbolInterface {
		if fieldSym, ok := ifaceSym.Fields[member]; ok {
			return firstNonEmpty(fieldSym.Type, "any")
		}
		return "unknown"
	}

	if strings.HasPrefix(typ, "enum:") {
		return "number"
	}

	if strings.HasPrefix(typ, "task:") {
		if member == "await" {
			return strings.TrimPrefix(typ, "task:")
		}

		return "unknown"
	}

	if sym, ok := resolveClassSymbol(a.root, typ); ok && sym.Kind == SymbolClass {
		typ = "class:" + typ
	}

	if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")

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

	if methodInfo, ok := GetNativeMethodInfo(typ, member); ok {
		return methodInfo.Returns
	}

	if member == "toString" {
		return "string"
	}

	return "unknown"
}

func findEnclosingIfBlock(text string, pos Position) (string, bool) {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return "", false
	}

	depth := 0
	for i := pos.Line; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])

		if strings.HasPrefix(line, "//") || line == "" {
			continue
		}

		if strings.Contains(line, "}") {
			depth--
		}
		if strings.Contains(line, "{") {
			depth++
		}

		if depth > 0 && strings.HasPrefix(line, "if ") {
			return line, true
		}
	}
	return "", false
}

var nullCheckRegex = regexp.MustCompile(`if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s*!=\s*(null)\s*\)?`)
var nullIsRegex = regexp.MustCompile(`if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s+is\s+null\s*\)?`)
var truthyIdentRegex = regexp.MustCompile(`^if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)?\s*\{?`)
var typeOfRegex = regexp.MustCompile("if\\s*\\(?\\s*typeof\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*==\\s*[\"'\u0060](string|number|bool|object|array)[\"'\u0060]\\s*\\)?")
var instanceOfRegex = regexp.MustCompile(`if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s+instanceof\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\)?`)

func applyTypeNarrowing(scope *Scope, ifLine string) {
	if match := nullCheckRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		narrowSymbolRemovingNull(scope, name)
		return
	}

	if match := nullIsRegex.FindStringSubmatch(ifLine); match != nil {
		if sym, ok := scope.Resolve(match[1]); ok {
			sym.Type = "null"
			scope.Define(sym)
		}
		return
	}

	if match := typeOfRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		narrowedType := match[2]
		if sym, ok := scope.Resolve(name); ok {
			sym.Type = narrowedType
			scope.Define(sym)
		}
		return
	}

	if match := instanceOfRegex.FindStringSubmatch(ifLine); match != nil {
		if sym, ok := scope.Resolve(match[1]); ok {
			sym.Type = "class:" + match[2]
			scope.Define(sym)
		}
		return
	}

	if match := truthyIdentRegex.FindStringSubmatch(ifLine); match != nil {
		narrowSymbolRemovingNull(scope, match[1])
		return
	}
}

func narrowSymbolRemovingNull(scope *Scope, name string) {
	if sym, ok := scope.Resolve(name); ok {
		parts := splitUnionType(sym.Type)
		newParts := []string{}
		for _, part := range parts {
			if strings.TrimSpace(part) != "null" {
				newParts = append(newParts, strings.TrimSpace(part))
			}
		}
		if len(newParts) > 0 && len(newParts) != len(parts) {
			sym.Type = strings.Join(newParts, " | ")
			scope.Define(sym)
		}
	}
}

func scanProjectTinyFiles(startPath string) []string {
	if strings.HasPrefix(startPath, "file://") {
		startPath = URIToPath(startPath)
	}
	var files []string
	root := ""
	if startPath != "" {
		dir := startPath
		if info, err := os.Stat(dir); err == nil && !info.IsDir() {
			dir = filepath.Dir(dir)
		}
		for {
			if _, err := os.Stat(filepath.Join(dir, "tiny.json")); err == nil {
				root = dir
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
		if root == "" {
			fallbackRoot := filepath.Dir(startPath)
			if isFilesystemRoot(fallbackRoot) {
				return nil
			}
			if info, err := os.Stat(fallbackRoot); err != nil || !info.IsDir() {
				return nil
			}
			root = fallbackRoot
		}
	}

	if root == "" {
		var err error
		root, err = os.Getwd()
		if err != nil {
			return nil
		}
	}

	root = filepath.Clean(root)
	now := time.Now()
	if cached, ok := lspProjectFilesCache[root]; ok && now.Before(cached.expiresAt) {
		return append([]string(nil), cached.files...)
	}

	filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			name := entry.Name()
			if name == ".tiny" || name == ".tinydeps" || name == "dist" || name == "bin" || strings.HasPrefix(name, ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if filepath.Ext(path) == ".tiny" {
			files = append(files, path)
		}
		return nil
	})

	lspProjectFilesCache[root] = lspProjectFilesCacheEntry{
		root:      root,
		files:     append([]string(nil), files...),
		expiresAt: now.Add(2 * time.Second),
	}
	return files
}

func isFilesystemRoot(path string) bool {
	if path == "" || path == "." {
		return false
	}
	clean := filepath.Clean(path)
	return filepath.Dir(clean) == clean
}

func isPositionInStringOrComment(line string, charIndex int) bool {
	type parserState struct {
		isString bool
		quote    byte
		braces   int
	}

	stack := []parserState{{isString: false}}
	escaped := false

	for i := 0; i < len(line) && i < charIndex; i++ {
		ch := line[i]
		curr := &stack[len(stack)-1]

		if curr.isString {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == curr.quote {
				// Exit string
				stack = stack[:len(stack)-1]
				continue
			}
			if curr.quote == '`' && ch == '$' && i+1 < len(line) && line[i+1] == '{' {
				// Enter interpolation inside backtick
				stack = append(stack, parserState{isString: false, braces: 1})
				i++ // Skip '{'
				continue
			}
		} else {
			// Code context
			if i+1 < len(line) && ch == '/' && line[i+1] == '/' {
				// Comment starts and goes to the end of the line
				return true
			}
			if ch == '"' || ch == '\'' || ch == '`' {
				// Enter string
				stack = append(stack, parserState{isString: true, quote: ch})
				escaped = false
				continue
			}
			switch ch {
			case '{':
				if curr.braces > 0 {
					curr.braces++
				}
			case '}':
				if curr.braces > 0 {
					curr.braces--
					if curr.braces == 0 {
						// Exit interpolation
						stack = stack[:len(stack)-1]
					}
				}
			}
		}
	}

	return stack[len(stack)-1].isString
}

func findFunctionArgumentTypeHint(scope *Scope, text string, pos Position) (string, bool) {
	ctx, ok := callContextAtPosition(text, pos)
	if !ok {
		return "", false
	}

	var sym SymbolInfo
	var exists bool

	if ctx.IsMember {
		_, receiverType, hasReceiver := resolveReceiverPath(scope, text, pos, ctx.Receiver)
		if !hasReceiver {
			return "", false
		}

		sym, _, exists = resolveMemberFromStaticType(scope, receiverType, ctx.Method)
	} else {
		if strings.Contains(ctx.Name, ".") {
			parts := strings.SplitN(ctx.Name, ".", 2)
			nsName := parts[0]
			memberName := parts[1]

			ns, ok := scope.Resolve(nsName)
			if ok && ns.Kind == SymbolNamespace {
				sym, exists = ns.Members[memberName]
			}
		} else {
			sym, exists = scope.Resolve(ctx.Name)
		}
	}

	if !exists {
		return "", false
	}

	if sym.Kind == SymbolClass {
		sym = constructorSymbolFromClass(sym, sym.Name)
	}

	if sym.Kind != SymbolFunction {
		return "", false
	}

	if ctx.ArgIndex < 0 || ctx.ArgIndex >= len(sym.Params) {
		return "", false
	}

	param := sym.Params[ctx.ArgIndex]

	if param.Type != "" {
		typ := param.Type
		typ = strings.TrimPrefix(typ, "interface:")
		typ = strings.TrimPrefix(typ, "class:")
		return typ, true
	}

	return "", false
}

func wordLengthAtColumn(line string, col int) int {
	if col < 0 || col >= len(line) {
		return 1
	}
	end := col

	for end < len(line) && isIdentChar(line[end]) {
		end++
	}
	if end == col {
		return 1
	}
	return end - col
}

func findIdentifierColumn(line string, name string, fallback int) int {
	if name == "" {
		return fallback
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	match := re.FindStringIndex(line)
	if match != nil {
		return match[0] + 1
	}
	return fallback
}

func isCursorInsideObjectLiteral(text string, pos Position) bool {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return false
	}

	depth := 0

	for i := pos.Line; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])

		if strings.Contains(line, "}") {
			depth--
		}
		if strings.Contains(line, "{") {
			depth++
		}

		if depth > 0 {
			if strings.Contains(line, "fn ") ||
				strings.Contains(line, "class ") ||
				strings.Contains(line, "if ") ||
				strings.Contains(line, "while ") ||
				strings.Contains(line, "for ") ||
				strings.Contains(line, "catch ") {
				return false
			}
			return true
		}
	}
	return false
}

func (a *astSemanticAnalyzer) compareLSPTypes(got string, expected string) bool {
	if expected == "any" || got == "any" || expected == "unknown" || got == "unknown" {
		return true
	}

	gotParts := splitUnionType(got)
	expectedParts := splitUnionType(expected)

	for _, g := range gotParts {
		g = strings.TrimSpace(g)
		matched := false
		for _, e := range expectedParts {
			e = strings.TrimSpace(e)
			if g == e {
				matched = true
				break
			}
			if e == "object" && (strings.HasPrefix(g, "class:") || strings.HasPrefix(g, "interface:") || g == "object") {
				matched = true
				break
			}
			if strings.HasPrefix(e, "interface:") && g == "object" {
				matched = true
				break
			}
			if strings.HasPrefix(e, "array:") && g == "array:empty" {
				matched = true
				break
			} else if e == "array:any" && (strings.HasPrefix(g, "array:") || g == "array" || g == "array:empty") {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func (a *astSemanticAnalyzer) checkArgumentTypes(name string, args []Expr, params []StdArg, line int, column int) {
	for i, arg := range args {
		var param StdArg
		if i < len(params) {
			param = params[i]
		} else if len(params) > 0 && params[len(params)-1].Variadic {
			param = params[len(params)-1]
		} else {
			break
		}

		if param.Variadic {
			continue
		}

		if param.Type == "" || param.Type == "any" {
			continue
		}

		argType := a.inferExprType(arg)
		if argType == "any" || argType == "unknown" {
			continue
		}

		if !a.compareLSPTypes(argType, param.Type) {
			a.addError(line, column, fmt.Sprintf("cannot pass type '%s' to parameter '%s' of function '%s' (expected '%s')", argType, param.Name, name, param.Type))
		}
	}
}

func extractNameFromMessage(msg string) string {
	msg = strings.TrimSpace(msg)

	if strings.HasPrefix(msg, "undefined variable: ") {
		return strings.TrimPrefix(msg, "undefined variable: ")
	}

	if strings.HasPrefix(msg, "unknown type: ") {
		return strings.TrimPrefix(msg, "unknown type: ")
	}

	if strings.HasPrefix(msg, "undefined method or property: ") {
		return strings.TrimPrefix(msg, "undefined method or property: ")
	}

	if strings.HasPrefix(msg, "private member is not accessible: ") {
		return strings.TrimPrefix(msg, "private member is not accessible: ")
	}

	if strings.HasPrefix(msg, "undefined export: ") {
		trimmed := strings.TrimPrefix(msg, "undefined export: ")
		if dot := strings.LastIndex(trimmed, "."); dot != -1 {
			return trimmed[dot+1:]
		}
		return trimmed
	}

	if strings.HasPrefix(msg, "wrong argument count for ") {
		trimmed := strings.TrimPrefix(msg, "wrong argument count for ")
		if idx := strings.LastIndex(trimmed, ": expected"); idx >= 0 {
			trimmed = trimmed[:idx]
		} else if idx := strings.Index(trimmed, ":"); idx >= 0 {
			trimmed = trimmed[:idx]
		}
		trimmed = strings.TrimSpace(trimmed)
		if dot := strings.LastIndex(trimmed, "."); dot != -1 {
			return trimmed[dot+1:]
		}
		return trimmed
	}

	if strings.Contains(msg, " of function '") {
		parts := strings.Split(msg, " of function '")
		if len(parts) > 1 {
			trimmed := parts[1]
			if idx := strings.Index(trimmed, "'"); idx >= 0 {
				fnName := trimmed[:idx]
				if dot := strings.LastIndex(fnName, "."); dot != -1 {
					return fnName[dot+1:]
				}
				return fnName
			}
		}
	}

	return ""
}

func findWordFirstOccurrence(text string, word string) (int, int) {
	if word == "" {
		return 1, 1
	}
	lines := strings.Split(text, "\n")

	for lineIdx, line := range lines {
		code := strings.TrimSpace(stripLineComment(line))

		if strings.HasPrefix(code, "fn ") ||
			strings.HasPrefix(code, "class ") ||
			strings.HasPrefix(code, "interface ") ||
			strings.Contains(code, "export fn ") ||
			strings.Contains(code, "export class ") ||
			strings.Contains(code, "export interface ") {
			continue
		}

		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
		match := re.FindStringIndex(code)
		if match != nil {
			return lineIdx + 1, match[0] + 1
		}
	}

	for lineIdx, line := range lines {
		code := stripLineComment(line)
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(word) + `\b`)
		match := re.FindStringIndex(code)
		if match != nil {
			return lineIdx + 1, match[0] + 1
		}
	}

	return 1, 1
}

func (a *astSemanticAnalyzer) checkNamingConflict(name string, line int, col int) bool {
	if strings.TrimSpace(name) == "" || name == "_" {
		return false
	}

	if existing, exists := a.resolve(name); exists {
		if existing.Line == line && existing.SourceURI == a.uri {
			return false
		}

		var detail string
		if existing.Kind == SymbolStd {
			detail = "imported standard library module"
		} else {
			detail = fmt.Sprintf("existing %s (defined at line %d)", existing.Kind, existing.Line)
		}

		a.addError(line, col, fmt.Sprintf("identifier '%s' conflicts with an %s", name, detail))
		return true
	}
	return false
}

func unwrapNullableType(typ string) string {
	typ = strings.TrimSpace(typ)
	if strings.Contains(typ, "|") {
		parts := splitUnionType(typ)
		nonNullParts := []string{}
		for _, part := range parts {
			if !isNullishLSPType(part) {
				nonNullParts = append(nonNullParts, part)
			}
		}
		if len(nonNullParts) == 1 {
			return nonNullParts[0]
		}
	}
	return typ
}

func classImplementsInterface(classSym SymbolInfo, ifaceSym SymbolInfo) bool {
	for name, ifaceField := range ifaceSym.Fields {
		classField, hasField := classSym.Fields[name]
		_, hasMethod := classSym.Methods[name]
		if !hasField && !hasMethod {
			return false
		}
		if hasMethod {
			if ifaceField.Type != "function" && ifaceField.Type != "" && ifaceField.Type != "any" {
				return false
			}
		}
		if hasField {
			if ifaceField.Type == "function" && classField.Type != "function" {
				return false
			}
		}
	}
	return true
}

func interfaceNameAtPosition(text string, pos Position) string {
	offset := offsetAtLine(text, pos.Line+1)
	for _, block := range findBlocks(text, "interface") {
		if offset >= block.Start && offset < block.End {
			return block.Name
		}
	}
	return ""
}

func getImplementations(uri string, text string, pos Position) []Location {
	line := getLine(text, pos.Line)
	if isPositionInStringOrComment(line, pos.Character) {
		return nil
	}

	word := wordAtPosition(text, pos)
	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	if ifaceSym, ok := resolveInterfaceSymbol(scope, word); ok && ifaceSym.Kind == SymbolInterface {
		return findClassesImplementing(scope, ifaceSym, "", uri)
	}

	ifaceName := interfaceNameAtPosition(text, pos)
	if ifaceName != "" {
		if ifaceSym, ok := resolveInterfaceSymbol(scope, ifaceName); ok && ifaceSym.Kind == SymbolInterface {
			if _, ok := ifaceSym.Fields[word]; ok {
				return findClassesImplementing(scope, ifaceSym, word, uri)
			}
		}
	}

	return nil
}

func findClassesImplementing(scope *Scope, ifaceSym SymbolInfo, methodName string, uri string) []Location {
	locations := []Location{}
	projectFiles := scanProjectTinyFiles(URIToPath(uri))
	fileSet := map[string]bool{}
	for _, f := range projectFiles {
		fileSet[filepath.Clean(f)] = true
	}
	for openPath := range lspDocs {
		fileSet[filepath.Clean(openPath)] = true
	}

	for path := range fileSet {
		uri := pathToFileURI(path)
		text, ok := tinyFileTextForLSP(path, uri)
		if !ok {
			continue
		}

		lines := strings.Split(text, "\n")
		fileScope := scopeAtPosition(uri, text, Position{Line: len(lines), Character: 0})

		for _, sym := range fileScope.Symbols {
			if sym.Kind == SymbolClass {
				if classImplementsInterface(sym, ifaceSym) {
					if methodName == "" {
						locations = append(locations, Location{
							URI: uri,
							Range: LSPRange{
								Start: Position{Line: sym.Line - 1, Character: sym.Column - 1},
								End:   Position{Line: sym.Line - 1, Character: sym.Column - 1 + len(sym.Name)},
							},
						})
					} else {
						if methodSym, ok := sym.Methods[methodName]; ok {
							locations = append(locations, Location{
								URI: uri,
								Range: LSPRange{
									Start: Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1},
									End:   Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1 + len(methodSym.Name)},
								},
							})
						}
					}
				}
			}
		}
	}

	return locations
}

func prepareCallHierarchy(uri string, text string, pos Position) []CallHierarchyItem {
	line := getLine(text, pos.Line)
	if isPositionInStringOrComment(line, pos.Character) {
		return nil
	}

	word := wordAtPosition(text, pos)
	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	// First check if it's a member access: receiver.member (e.g. Utils.helper() or user.verify())
	receiver, member, ok := memberExprAtPosition(text, pos)
	if ok && member == word {
		sym, typ, exists := resolveReceiverPath(scope, text, pos, receiver)
		if exists {
			typ = unwrapNullableType(typ)
			if sym.Kind == SymbolNamespace {
				if memberSym, ok := sym.Members[word]; ok && memberSym.Kind == SymbolFunction {
					rng := LSPRange{
						Start: Position{Line: memberSym.Line - 1, Character: memberSym.Column - 1},
						End:   Position{Line: memberSym.Line - 1, Character: memberSym.Column - 1 + len(memberSym.Name)},
					}
					return []CallHierarchyItem{{
						Name:           memberSym.Name,
						Kind:           12, // Function
						Detail:         memberSym.Name,
						URI:            memberSym.SourceURI,
						Range:          rng,
						SelectionRange: rng,
						Data: map[string]any{
							"name":  memberSym.Name,
							"class": "",
							"uri":   memberSym.SourceURI,
						},
					}}
				} else if memberSym, ok := sym.Members[word]; ok && memberSym.Kind == SymbolClass {
					rng := LSPRange{
						Start: Position{Line: memberSym.Line - 1, Character: memberSym.Column - 1},
						End:   Position{Line: memberSym.Line - 1, Character: memberSym.Column - 1 + len(memberSym.Name)},
					}
					return []CallHierarchyItem{{
						Name:           memberSym.Name,
						Kind:           7, // Class
						Detail:         memberSym.Name,
						URI:            memberSym.SourceURI,
						Range:          rng,
						SelectionRange: rng,
						Data: map[string]any{
							"name":  memberSym.Name,
							"class": memberSym.Name,
							"uri":   memberSym.SourceURI,
						},
					}}
				}
			} else if strings.HasPrefix(typ, "class:") {
				className := strings.TrimPrefix(typ, "class:")
				if classSym, ok := resolveClassSymbol(scope, className); ok {
					if methodSym, ok := classSym.Methods[word]; ok {
						rng := LSPRange{
							Start: Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1},
							End:   Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1 + len(methodSym.Name)},
						}
						sourceURI := classSym.SourceURI
						if sourceURI == "" {
							sourceURI = uri
						}
						return []CallHierarchyItem{{
							Name:           methodSym.Name,
							Kind:           6, // Method
							Detail:         className + "." + methodSym.Name,
							URI:            sourceURI,
							Range:          rng,
							SelectionRange: rng,
							Data: map[string]any{
								"name":  methodSym.Name,
								"class": className,
								"uri":   sourceURI,
							},
						}}
					}
				}
			}
		}
	}

	className := classNameAtPosition(text, pos)
	if className != "" {
		if classSym, ok := resolveClassSymbol(scope, className); ok && classSym.Kind == SymbolClass {
			if methodSym, ok := classSym.Methods[word]; ok {
				rng := LSPRange{
					Start: Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1},
					End:   Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1 + len(methodSym.Name)},
				}
				return []CallHierarchyItem{{
					Name:           methodSym.Name,
					Kind:           6, // Method
					Detail:         className + "." + methodSym.Name,
					URI:            uri,
					Range:          rng,
					SelectionRange: rng,
					Data: map[string]any{
						"name":  methodSym.Name,
						"class": className,
						"uri":   uri,
					},
				}}
			}
		}
	}

	if sym, ok := scope.Resolve(word); ok && sym.Kind == SymbolFunction {
		rng := LSPRange{
			Start: Position{Line: sym.Line - 1, Character: sym.Column - 1},
			End:   Position{Line: sym.Line - 1, Character: sym.Column - 1 + len(sym.Name)},
		}
		return []CallHierarchyItem{{
			Name:           sym.Name,
			Kind:           12, // Function
			Detail:         sym.Name,
			URI:            uri,
			Range:          rng,
			SelectionRange: rng,
			Data: map[string]any{
				"name":  sym.Name,
				"class": "",
				"uri":   uri,
			},
		}}
	}

	return nil
}

func getIncomingCalls(item CallHierarchyItem) []CallHierarchyIncomingCall {
	incomingMap := make(map[string]*CallHierarchyIncomingCall)
	targetName := item.Name
	var targetClass string
	if dataMap, ok := item.Data.(map[string]any); ok {
		if cls, ok := dataMap["class"].(string); ok {
			targetClass = cls
		}
	}
	targetURI := item.URI

	targetLine := -1
	targetText, ok := tinyFileTextForLSP(URIToPath(targetURI), targetURI)
	if ok {
		targetScope := scopeAtPosition(targetURI, targetText, item.Range.Start)
		if targetClass == "" {
			if sym, ok := targetScope.Resolve(targetName); ok {
				targetLine = sym.Line
			}
		} else {
			if classSym, ok := resolveClassSymbol(targetScope, targetClass); ok {
				if methodSym, ok := classSym.Methods[targetName]; ok {
					targetLine = methodSym.Line
				}
			}
		}
	}

	projectFiles := scanProjectTinyFiles(URIToPath(targetURI))

	fileSet := map[string]bool{}
	for _, f := range projectFiles {
		fileSet[filepath.Clean(f)] = true
	}
	fileSet[filepath.Clean(URIToPath(targetURI))] = true

	for path := range fileSet {
		fileURI := pathToFileURI(path)
		text, ok := tinyFileTextForLSP(path, fileURI)
		if !ok {
			continue
		}

		ranges := identifierRangesInText(text, targetName)
		for _, r := range ranges {
			pos := Position{Line: r.Line, Character: r.Start}
			called := false
			scope := scopeAtPosition(fileURI, text, pos)
			if targetClass == "" {
				receiver, member, ok := memberExprAtPosition(text, pos)
				if ok && member == targetName {
					sym, _, exists := resolveReceiverPath(scope, text, pos, receiver)
					if exists && sym.Kind == SymbolNamespace {
						if memberSym, ok := sym.Members[targetName]; ok && memberSym.SourceURI == targetURI && memberSym.Line == targetLine {
							called = true
						}
					}
				} else {
					if sym, ok := scope.Resolve(targetName); ok && sym.Kind == SymbolFunction && sym.SourceURI == targetURI && sym.Line == targetLine {
						called = true
					}
				}
			} else {
				receiver, member, ok := memberExprAtPosition(text, pos)
				if ok && member == targetName {
					_, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
					if exists && unwrapNullableType(receiverType) == "class:"+targetClass {
						called = true
					}
				}
			}

			if called {
				// Skip if it is the declaration itself
				if pos.Line == targetLine-1 {
					lineText := getLine(text, pos.Line)
					beforeIdent := strings.TrimSpace(lineText[:pos.Character])
					if strings.HasSuffix(beforeIdent, "fn") {
						continue
					}
				}
				fnBlock := functionBlockAtLine(text, pos.Line)
				var callerItem CallHierarchyItem
				callerKey := ""

				if fnBlock != nil {
					classBlock := classBlockAtLine(text, pos.Line)
					if classBlock != nil {
						callerKey = fileURI + ":" + classBlock.Name + "." + fnBlock.Name
						rng := LSPRange{
							Start: Position{Line: fnBlock.Line - 1, Character: fnBlock.Column - 1},
							End:   Position{Line: fnBlock.Line - 1, Character: fnBlock.Column - 1 + len(fnBlock.Name)},
						}
						callerItem = CallHierarchyItem{
							Name:           fnBlock.Name,
							Kind:           6, // Method
							Detail:         classBlock.Name + "." + fnBlock.Name,
							URI:            fileURI,
							Range:          rng,
							SelectionRange: rng,
						}
					} else {
						callerKey = fileURI + ":" + fnBlock.Name
						rng := LSPRange{
							Start: Position{Line: fnBlock.Line - 1, Character: fnBlock.Column - 1},
							End:   Position{Line: fnBlock.Line - 1, Character: fnBlock.Column - 1 + len(fnBlock.Name)},
						}
						callerItem = CallHierarchyItem{
							Name:           fnBlock.Name,
							Kind:           12, // Function
							Detail:         fnBlock.Name,
							URI:            fileURI,
							Range:          rng,
							SelectionRange: rng,
						}
					}
				} else {
					callerKey = fileURI + ":<top-level>"
					rng := LSPRange{
						Start: Position{Line: 0, Character: 0},
						End:   Position{Line: 0, Character: 0},
					}
					callerItem = CallHierarchyItem{
						Name:           "<top-level>",
						Kind:           12,
						Detail:         "top-level statements",
						URI:            fileURI,
						Range:          rng,
						SelectionRange: rng,
					}
				}

				callRange := LSPRange{
					Start: Position{Line: r.Line, Character: r.Start},
					End:   Position{Line: r.Line, Character: r.End},
				}

				if call, exists := incomingMap[callerKey]; exists {
					call.FromRanges = append(call.FromRanges, callRange)
				} else {
					incomingMap[callerKey] = &CallHierarchyIncomingCall{
						From:       callerItem,
						FromRanges: []LSPRange{callRange},
					}
				}
			}
		}
	}

	result := []CallHierarchyIncomingCall{}
	for _, call := range incomingMap {
		result = append(result, *call)
	}
	return result
}

func getOutgoingCalls(item CallHierarchyItem) []CallHierarchyOutgoingCall {
	targetURI := item.URI
	text, ok := tinyFileTextForLSP(URIToPath(targetURI), targetURI)
	if !ok {
		return nil
	}

	fnBlock := functionBlockAtLine(text, item.Range.Start.Line)
	if fnBlock == nil {
		return nil
	}

	callRegex := regexp.MustCompile(`\b([A-Za-z_][A-Za-z0-9_]*)\s*\(`)
	matches := callRegex.FindAllStringSubmatchIndex(fnBlock.Body, -1)

	outgoingMap := map[string]*CallHierarchyOutgoingCall{}

	for _, match := range matches {
		wordStart := match[2]
		wordEnd := match[3]
		word := fnBlock.Body[wordStart:wordEnd]

		if tinyKeywords[word] {
			continue
		}

		bodyOffset := fnBlock.Start + len(fnBlock.Header) + 1 + wordStart
		pos := bytePositionAtOffset(text, bodyOffset)

		scope := scopeAtPosition(targetURI, text, pos)

		var calledItem *CallHierarchyItem

		receiver, member, ok := memberExprAtPosition(text, pos)
		if ok && member == word {
			_, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
			if exists {
				receiverType = unwrapNullableType(receiverType)
				if strings.HasPrefix(receiverType, "class:") {
					className := strings.TrimPrefix(receiverType, "class:")
					if classSym, ok := resolveClassSymbol(scope, className); ok {
						if methodSym, ok := classSym.Methods[word]; ok {
							rng := LSPRange{
								Start: Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1},
								End:   Position{Line: methodSym.Line - 1, Character: methodSym.Column - 1 + len(methodSym.Name)},
							}
							calledItem = &CallHierarchyItem{
								Name:           methodSym.Name,
								Kind:           6, // Method
								Detail:         className + "." + methodSym.Name,
								URI:            targetURI,
								Range:          rng,
								SelectionRange: rng,
							}
						}
					}
				}
			}
		} else {
			if sym, ok := scope.Resolve(word); ok && sym.Kind == SymbolFunction {
				rng := LSPRange{
					Start: Position{Line: sym.Line - 1, Character: sym.Column - 1},
					End:   Position{Line: sym.Line - 1, Character: sym.Column - 1 + len(sym.Name)},
				}
				calledItem = &CallHierarchyItem{
					Name:           sym.Name,
					Kind:           12, // Function
					Detail:         sym.Name,
					URI:            sym.SourceURI,
					Range:          rng,
					SelectionRange: rng,
				}
			}
		}

		if calledItem != nil {
			key := calledItem.URI + ":" + calledItem.Detail
			callRange := LSPRange{
				Start: pos,
				End:   Position{Line: pos.Line, Character: pos.Character + len(word)},
			}

			if call, exists := outgoingMap[key]; exists {
				call.FromRanges = append(call.FromRanges, callRange)
			} else {
				outgoingMap[key] = &CallHierarchyOutgoingCall{
					To:         *calledItem,
					FromRanges: []LSPRange{callRange},
				}
			}
		}
	}

	result := []CallHierarchyOutgoingCall{}
	for _, call := range outgoingMap {
		result = append(result, *call)
	}
	return result
}

type ParseFrameKind int

const (
	FrameCall ParseFrameKind = iota
	FrameObject
	FrameArray
	FrameBlock
)

type ParseFrame struct {
	Kind       ParseFrameKind
	Name       string
	ArgIndex   int
	CurrentKey string
	Type       string
	Symbol     SymbolInfo
	Symbols    []SymbolInfo
	Keys       []string
}

type EditorContext struct {
	InsideString         bool
	StringQuote          byte
	InsideInterpolation  bool
	InsideObject         bool
	ObjectInterfaceType  string
	ObjectInterfaceSym   SymbolInfo
	ObjectInterfaceSyms  []SymbolInfo
	ObjectKeys           []string
	IsObjectKeyPosition  bool
	IsObjectStringKey    bool
	TypedStringKeyPrefix string
	CursorPosition       Position
	LineText             string
}

func parseEditorContext(text string, pos Position, scope *Scope) EditorContext {
	cursor := offsetAtLine(text, pos.Line+1) + pos.Character
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(text) {
		cursor = len(text)
	}

	var stack []ParseFrame

	inString := byte(0)
	escaped := false
	inLineComment := false

	type stringInterpolation struct {
		braceDepth int
	}
	var interpStack []stringInterpolation

	var lastIdent string
	var lastString string
	var lastTokenWasString bool

	i := 0
	for i < cursor {
		ch := text[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			i++
			continue
		}

		if inString != 0 {
			if escaped {
				escaped = false
				i++
				continue
			}
			if ch == '\\' {
				escaped = true
				i++
				continue
			}

			if inString == '`' && ch == '$' && i+1 < len(text) && text[i+1] == '{' {
				interpStack = append(interpStack, stringInterpolation{
					braceDepth: 0,
				})
				i += 2
				continue
			}

			if ch == inString {
				lastTokenWasString = true
				strStart := i - 1
				for strStart >= 0 {
					if text[strStart] == inString {
						escapedCount := 0
						for k := strStart - 1; k >= 0 && text[k] == '\\'; k-- {
							escapedCount++
						}
						if escapedCount%2 == 0 {
							break
						}
					}
					strStart--
				}
				if strStart >= 0 {
					lastString = text[strStart+1 : i]
				} else {
					lastString = ""
				}
				inString = 0
			}
			i++
			continue
		}

		if i+1 < len(text) && ch == '/' && text[i+1] == '/' {
			inLineComment = true
			i += 2
			continue
		}

		if ch == '"' || ch == '\'' || ch == '`' {
			inString = ch
			lastString = ""
			i++
			continue
		}

		if len(interpStack) > 0 {
			if ch == '{' {
				interpStack[len(interpStack)-1].braceDepth++
			} else if ch == '}' {
				interpStack[len(interpStack)-1].braceDepth--
				if interpStack[len(interpStack)-1].braceDepth < 0 {
					interpStack = interpStack[:len(interpStack)-1]
					i++
					continue
				}
			}
		}

		switch ch {
		case '(':
			name := extractCalleeBefore(text[:i], i)
			stack = append(stack, ParseFrame{
				Kind: FrameCall,
				Name: name,
			})

		case ')':
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.Kind == FrameCall {
					break
				}
			}

		case '{':
			if isBlockOpening(text, i) {
				stack = append(stack, ParseFrame{Kind: FrameBlock})
			} else {
				var typ string
				var sym SymbolInfo
				var symbols []SymbolInfo
				var exists bool

				if len(stack) > 0 {
					parent := &stack[len(stack)-1]
					if parent.Kind == FrameCall {
						typ, sym, exists = resolveCallArgType(scope, parent.Name, parent.ArgIndex)
					} else if parent.Kind == FrameObject {
						if parent.CurrentKey != "" {
							var resolvedTyp string
							var resolvedSym SymbolInfo
							var resolvedExists bool

							for _, parentSym := range parent.Symbols {
								t, s, ok := resolveObjectFieldType(scope, parentSym, parent.CurrentKey)
								if ok {
									if resolvedTyp == "" {
										resolvedTyp = t
									} else {
										resolvedTyp = resolvedTyp + " | " + t
									}
									resolvedSym = s
									resolvedExists = true
								}
							}

							if resolvedExists {
								typ = resolvedTyp
								sym = resolvedSym
								exists = true
							} else {
								typ, sym, exists = resolveObjectFieldType(scope, parent.Symbol, parent.CurrentKey)
							}
						}
					}
				} else {
					typ, exists = findObjectTypeHintAtOffset(text, i)
					if exists {
						sym, exists = resolveTypeSymbol(scope, typ)
					}
				}

				if exists || typ != "" {
					symbols = resolveUnionInterfaceSymbols(scope, typ)
					if len(symbols) > 0 && !exists {
						sym = symbols[0]
						exists = true
					}
				}

				stack = append(stack, ParseFrame{
					Kind:    FrameObject,
					Type:    typ,
					Symbol:  sym,
					Symbols: symbols,
				})
			}

		case '}':
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.Kind == FrameBlock || top.Kind == FrameObject {
					break
				}
			}

		case '[':
			stack = append(stack, ParseFrame{Kind: FrameArray})

		case ']':
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.Kind == FrameArray {
					break
				}
			}

		case ',':
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				if top.Kind == FrameCall {
					top.ArgIndex++
				} else if top.Kind == FrameObject {
					top.CurrentKey = ""
				}
			}

		case ':':
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				if top.Kind == FrameObject {
					var k string
					if lastTokenWasString {
						k = lastString
					} else {
						k = lastIdent
					}
					top.CurrentKey = k
					found := false
					for _, existing := range top.Keys {
						if existing == k {
							found = true
							break
						}
					}
					if !found && k != "" {
						top.Keys = append(top.Keys, k)
					}
				}
			}

		default:
			if isIdentByte(ch) {
				startIdent := i
				for i < cursor && isIdentByte(text[i]) {
					i++
				}
				lastIdent = text[startIdent:i]
				lastTokenWasString = false
				continue
			}
		}

		i++
	}

	ctx := EditorContext{
		CursorPosition: pos,
		LineText:       getLine(text, pos.Line),
	}

	if inString != 0 {
		if len(interpStack) > 0 {
			ctx.InsideInterpolation = true
		} else {
			ctx.InsideString = true
			ctx.StringQuote = inString
		}
	} else if inLineComment {
		ctx.InsideString = true
	}

	if len(stack) > 0 {
		top := stack[len(stack)-1]
		if top.Kind == FrameObject {
			ctx.InsideObject = true
			ctx.ObjectInterfaceType = top.Type
			ctx.ObjectInterfaceSym = top.Symbol
			ctx.ObjectInterfaceSyms = top.Symbols
			ctx.ObjectKeys = top.Keys

			if top.CurrentKey == "" {
				ctx.IsObjectKeyPosition = true
			}

			if ctx.InsideString && ctx.IsObjectKeyPosition {
				ctx.IsObjectStringKey = true

				strStart := cursor - 1
				for strStart >= 0 {
					if text[strStart] == ctx.StringQuote {
						escapedCount := 0
						for k := strStart - 1; k >= 0 && text[k] == '\\'; k-- {
							escapedCount++
						}
						if escapedCount%2 == 0 {
							break
						}
					}
					strStart--
				}
				if strStart >= 0 {
					ctx.TypedStringKeyPrefix = text[strStart+1 : cursor]
				}
			}
		}
	}

	return ctx
}

func resolveCallArgType(scope *Scope, callName string, argIndex int) (string, SymbolInfo, bool) {
	var sym SymbolInfo
	var exists bool

	if strings.Contains(callName, ".") {
		parts := strings.SplitN(callName, ".", 2)
		nsName := parts[0]
		memberName := parts[1]

		ns, ok := scope.Resolve(nsName)
		if ok && ns.Kind == SymbolNamespace {
			sym, exists = ns.Members[memberName]
		}
	} else {
		sym, exists = scope.Resolve(callName)
	}

	if !exists {
		return "", SymbolInfo{}, false
	}

	if sym.Kind == SymbolClass {
		sym = constructorSymbolFromClass(sym, sym.Name)
	}

	if sym.Kind != SymbolFunction {
		return "", SymbolInfo{}, false
	}

	if argIndex < 0 || argIndex >= len(sym.Params) {
		return "", SymbolInfo{}, false
	}

	param := sym.Params[argIndex]
	if param.Type == "" {
		return "", SymbolInfo{}, false
	}

	typ := param.Type
	typ = strings.TrimPrefix(typ, "interface:")
	typ = strings.TrimPrefix(typ, "class:")

	targetSym, ok := resolveTypeSymbol(scope, typ)
	return typ, targetSym, ok
}

func resolveObjectFieldType(scope *Scope, parentSym SymbolInfo, key string) (string, SymbolInfo, bool) {
	if parentSym.Kind == SymbolInterface || parentSym.Kind == SymbolClass {
		field, ok := parentSym.Fields[key]
		if ok {
			typ := field.Type
			typ = strings.TrimPrefix(typ, "interface:")
			typ = strings.TrimPrefix(typ, "class:")
			sym, ok := resolveTypeSymbol(scope, typ)
			return typ, sym, ok
		}
	}
	return "", SymbolInfo{}, false
}

func resolveTypeSymbol(scope *Scope, typeName string) (SymbolInfo, bool) {
	for _, part := range splitUnionType(typeName) {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "interface:")
		part = strings.TrimPrefix(part, "class:")
		if isNullishLSPType(part) || part == "any" {
			continue
		}

		var sym SymbolInfo
		var exists bool

		if strings.Contains(part, ".") {
			parts := strings.SplitN(part, ".", 2)
			nsName := parts[0]
			memberName := parts[1]

			ns, ok := scope.Resolve(nsName)
			if ok && ns.Kind == SymbolNamespace {
				sym, exists = ns.Members[memberName]
			}
		}

		if !exists {
			if iface, ok := resolveInterfaceSymbol(scope, part); ok {
				sym = iface
				exists = true
			} else if class, ok := resolveClassSymbol(scope, part); ok {
				sym = class
				exists = true
			}
		}

		if exists {
			return sym, true
		}
	}

	return SymbolInfo{}, false
}

func resolveUnionInterfaceSymbols(scope *Scope, typeName string) []SymbolInfo {
	var symbols []SymbolInfo
	for _, part := range splitUnionType(typeName) {
		part = strings.TrimSpace(part)
		part = strings.TrimPrefix(part, "interface:")
		part = strings.TrimPrefix(part, "class:")
		if isNullishLSPType(part) || part == "any" {
			continue
		}

		var sym SymbolInfo
		var exists bool

		if strings.Contains(part, ".") {
			parts := strings.SplitN(part, ".", 2)
			nsName := parts[0]
			memberName := parts[1]

			ns, ok := scope.Resolve(nsName)
			if ok && ns.Kind == SymbolNamespace {
				sym, exists = ns.Members[memberName]
			}
		}

		if !exists {
			if iface, ok := resolveInterfaceSymbol(scope, part); ok {
				sym = iface
				exists = true
			} else if class, ok := resolveClassSymbol(scope, part); ok {
				sym = class
				exists = true
			}
		}

		if exists && (sym.Kind == SymbolInterface || sym.Kind == SymbolClass) {
			symbols = append(symbols, sym)
		}
	}
	return symbols
}

func findObjectTypeHintAtOffset(text string, offset int) (string, bool) {
	lineNum := 0
	for i := 0; i < offset && i < len(text); i++ {
		if text[i] == '\n' {
			lineNum++
		}
	}

	lines := strings.Split(text, "\n")
	if lineNum >= len(lines) {
		return "", false
	}

	depth := 0
	for i := lineNum; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if strings.Contains(line, "}") {
			depth--
		}
		if strings.Contains(line, "{") {
			depth++
		}
		if depth > 0 && strings.Contains(line, ":") && strings.Contains(line, "=") {
			match := regexp.MustCompile(`(?::\s*([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*))\s*=`).FindStringSubmatch(line)
			if match != nil {
				return match[1], true
			}
		}
	}
	return "", false
}

func objectLiteralCompletionsWithContext(ctx EditorContext) []CompletionItem {
	var activeSyms []SymbolInfo = ctx.ObjectInterfaceSyms

	if len(activeSyms) == 0 {
		sym := ctx.ObjectInterfaceSym
		if sym.Kind == SymbolInterface || sym.Kind == SymbolClass {
			activeSyms = []SymbolInfo{sym}
		}
	}

	if len(activeSyms) == 0 {
		return nil
	}

	// Try to narrow down based on unique keys typed so far
	if len(activeSyms) > 1 && len(ctx.ObjectKeys) > 0 {
		var narrowedSym *SymbolInfo
		for _, key := range ctx.ObjectKeys {
			var matches []SymbolInfo
			for _, sym := range activeSyms {
				if _, ok := sym.Fields[key]; ok {
					matches = append(matches, sym)
				}
			}
			if len(matches) == 1 {
				narrowedSym = &matches[0]
				break // Found a key unique to exactly one interface, so we narrow to it!
			}
		}
		if narrowedSym != nil {
			activeSyms = []SymbolInfo{*narrowedSym}
		}
	}

	// Merge fields
	type MergedField struct {
		Name  string
		Types []string
		Kinds []SymbolKind
	}

	mergedFields := make(map[string]*MergedField)
	for _, sym := range activeSyms {
		for name, field := range sym.Fields {
			mf, ok := mergedFields[name]
			if !ok {
				mf = &MergedField{
					Name: name,
				}
				mergedFields[name] = mf
			}
			typeExists := false
			for _, t := range mf.Types {
				if t == field.Type {
					typeExists = true
					break
				}
			}
			if !typeExists {
				mf.Types = append(mf.Types, field.Type)
			}
			kindExists := false
			for _, k := range mf.Kinds {
				if k == sym.Kind {
					kindExists = true
					break
				}
			}
			if !kindExists {
				mf.Kinds = append(mf.Kinds, sym.Kind)
			}
		}
	}

	var names []string
	for name := range mergedFields {
		names = append(names, name)
	}
	sort.Strings(names)

	items := []CompletionItem{}
	quoteStr := ""
	if ctx.IsObjectStringKey {
		quoteStr = string(ctx.StringQuote)
	}

	for _, name := range names {
		mf := mergedFields[name]

		var label string
		var insertText string
		var textEdit *TextEdit

		if ctx.IsObjectStringKey {
			label = quoteStr + mf.Name + quoteStr + ": "

			line := ctx.LineText
			pos := ctx.CursorPosition
			quoteChar := ctx.StringQuote

			quoteStart := pos.Character - 1
			for quoteStart >= 0 {
				if line[quoteStart] == quoteChar {
					escapedCount := 0
					for k := quoteStart - 1; k >= 0 && line[k] == '\\'; k-- {
						escapedCount++
					}
					if escapedCount%2 == 0 {
						break
					}
				}
				quoteStart--
			}

			quoteEnd := pos.Character
			hasClosingQuote := false
			for quoteEnd < len(line) {
				if line[quoteEnd] == quoteChar {
					escapedCount := 0
					for k := quoteEnd - 1; k >= 0 && line[k] == '\\'; k-- {
						escapedCount++
					}
					if escapedCount%2 == 0 {
						hasClosingQuote = true
						break
					}
				}
				quoteEnd++
			}

			endChar := pos.Character
			if hasClosingQuote {
				endChar = quoteEnd + 1
			}

			newText := quoteStr + mf.Name + quoteStr + ": $0"

			textEdit = &TextEdit{
				Range: LSPRange{
					Start: Position{Line: pos.Line, Character: quoteStart},
					End:   Position{Line: pos.Line, Character: endChar},
				},
				NewText: newText,
			}
			insertText = newText
		} else {
			label = mf.Name + ": "
			insertText = mf.Name + ": $0"
		}

		kind := 5

		var cleanedTypes []string
		for _, t := range mf.Types {
			t = strings.TrimPrefix(t, "interface:")
			t = strings.TrimPrefix(t, "class:")
			cleanedTypes = append(cleanedTypes, t)
		}
		combinedType := strings.Join(cleanedTypes, " | ")

		isClass := false
		for _, k := range mf.Kinds {
			if k == SymbolClass {
				isClass = true
				break
			}
		}

		detail := "interface field: " + combinedType
		if isClass {
			detail = "class field: " + combinedType
		}

		items = append(items, CompletionItem{
			Label:            label,
			Kind:             kind,
			Detail:           detail,
			InsertText:       insertText,
			InsertTextFormat: 2,
			TextEdit:         textEdit,
		})
	}

	return items
}

func isBlockOpening(text string, braceOffset int) bool {
	i := braceOffset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 {
		return false
	}
	if text[i] == ')' {
		return true
	}
	if isFunctionReturnTypeBlockOpening(text, i) {
		return true
	}
	if lineStartsBlockBeforeBrace(text, braceOffset) {
		return true
	}

	end := i + 1
	for i >= 0 && isIdentByte(text[i]) {
		i--
	}
	word := text[i+1 : end]
	if isBlockKeyword(word) {
		return true
	}

	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i >= 0 {
		end2 := i + 1
		for i >= 0 && isIdentByte(text[i]) {
			i--
		}
		word2 := text[i+1 : end2]
		if isBlockKeyword(word2) {
			return true
		}
	}

	return false
}

func lineStartsBlockBeforeBrace(text string, braceOffset int) bool {
	lineStart := strings.LastIndex(text[:braceOffset], "\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}

	prefix := strings.TrimSpace(text[lineStart:braceOffset])
	for strings.HasPrefix(prefix, "}") {
		prefix = strings.TrimSpace(strings.TrimPrefix(prefix, "}"))
	}
	if prefix == "" {
		return false
	}

	end := 0
	for end < len(prefix) && isIdentByte(prefix[end]) {
		end++
	}
	if end == 0 {
		return false
	}

	return isBlockKeyword(prefix[:end])
}

func isFunctionReturnTypeBlockOpening(text string, beforeBrace int) bool {
	i := beforeBrace
	for i >= 0 && isFunctionReturnTypeByte(text[i]) {
		i--
	}
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 || text[i] != ':' {
		return false
	}

	i--
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 || text[i] != ')' {
		return false
	}

	openParen := findMatchingBackwards(text, i, '(', ')')
	if openParen < 0 {
		return false
	}

	i = openParen - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 {
		return false
	}

	end := i + 1
	for i >= 0 && isIdentByte(text[i]) {
		i--
	}
	word := text[i+1 : end]
	if word == "fn" {
		return true
	}

	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	end = i + 1
	for i >= 0 && isIdentByte(text[i]) {
		i--
	}
	return text[i+1:end] == "fn"
}

func isFunctionReturnTypeByte(ch byte) bool {
	return isIdentByte(ch) || ch == '.' || ch == '|' || ch == '?' || ch == '[' || ch == ']' || ch == '<' || ch == '>' || ch == ','
}

func isBlockKeyword(word string) bool {
	switch word {
	case "class", "fn", "if", "while", "for", "else", "catch", "interface", "enum", "try", "finally":
		return true
	}
	return false
}

func findMatchingBackwards(text string, closeIndex int, openChar, closeChar byte) int {
	depth := 0
	for i := closeIndex; i >= 0; i-- {
		if text[i] == closeChar {
			depth++
		} else if text[i] == openChar {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

func findDocumentationComments(text string, lineIndex int) string {
	lines := strings.Split(text, "\n")
	if lineIndex <= 0 || lineIndex >= len(lines) {
		return ""
	}

	var comments []string
	for i := lineIndex - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			break
		}
		if strings.HasPrefix(line, "//") {
			content := strings.TrimSpace(strings.TrimPrefix(line, "//"))
			comments = append([]string{content}, comments...)
		} else {
			break
		}
	}
	return strings.Join(comments, "  \n")
}

func appendDoc(detail string, doc string) string {
	if doc == "" {
		return detail
	}
	if detail == "" {
		return doc
	}
	return detail + "\n\n" + doc
}
