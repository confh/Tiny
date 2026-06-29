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
	Name           string
	Kind           SymbolKind
	Type           string
	TypeRef        LSPTypeRef
	Detail         string
	Line           int
	Column         int
	SourceURI      string
	Doc            string
	TypeParameters []string
	Implements     []string

	Fields  map[string]SymbolInfo
	Params  []StdArg
	Returns string
	Methods map[string]SymbolInfo
	Members map[string]SymbolInfo
}

type LSPTypeKind string

const (
	LSPTypeUnknown   LSPTypeKind = "unknown"
	LSPTypeAny       LSPTypeKind = "any"
	LSPTypePrimitive LSPTypeKind = "primitive"
	LSPTypeFunction  LSPTypeKind = "function"
	LSPTypeClass     LSPTypeKind = "class"
	LSPTypeInterface LSPTypeKind = "interface"
	LSPTypeEnum      LSPTypeKind = "enum"
	LSPTypeNamespace LSPTypeKind = "namespace"
	LSPTypeStd       LSPTypeKind = "std"
	LSPTypeArray     LSPTypeKind = "array"
	LSPTypeTask      LSPTypeKind = "task"
	LSPTypeUnion     LSPTypeKind = "union"
)

type LSPTypeRef struct {
	Kind LSPTypeKind
	Name string
	Args []LSPTypeRef
}

func (t LSPTypeRef) IsZero() bool {
	return t.Kind == "" && t.Name == "" && len(t.Args) == 0
}

func (t LSPTypeRef) String() string {
	switch t.Kind {
	case "":
		return ""
	case LSPTypeAny:
		return "any"
	case LSPTypeUnknown:
		return "unknown"
	case LSPTypeFunction:
		if t.Name != "" {
			return t.Name
		}
		return "function"
	case LSPTypeClass:
		return "class:" + t.Name
	case LSPTypeInterface:
		return "interface:" + t.Name
	case LSPTypeEnum:
		return "enum:" + t.Name
	case LSPTypeNamespace:
		return "namespace:" + t.Name
	case LSPTypeStd:
		return "std:" + t.Name
	case LSPTypeArray:
		if len(t.Args) == 0 {
			return "array"
		}
		return "array:" + t.Args[0].String()
	case LSPTypeTask:
		if len(t.Args) == 0 {
			return "task:any"
		}
		return "task:" + t.Args[0].String()
	case LSPTypeUnion:
		parts := make([]string, 0, len(t.Args))
		for _, arg := range t.Args {
			parts = append(parts, arg.String())
		}
		return strings.Join(parts, " | ")
	default:
		if t.Name != "" {
			return t.Name
		}
		return string(t.Kind)
	}
}

func parseLSPTypeRef(typ string) LSPTypeRef {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return LSPTypeRef{Kind: LSPTypeUnknown}
	}
	if strings.Contains(typ, "|") {
		parts := splitUnionType(typ)
		if len(parts) == 1 && strings.TrimSpace(parts[0]) == typ {
			return LSPTypeRef{Kind: LSPTypePrimitive, Name: typ}
		}
		args := make([]LSPTypeRef, 0, len(parts))
		for _, part := range parts {
			args = append(args, parseLSPTypeRef(part))
		}
		return LSPTypeRef{Kind: LSPTypeUnion, Args: args}
	}
	switch {
	case typ == "any":
		return LSPTypeRef{Kind: LSPTypeAny}
	case typ == "unknown":
		return LSPTypeRef{Kind: LSPTypeUnknown}
	case typ == "function" || strings.HasPrefix(typ, "function("):
		return LSPTypeRef{Kind: LSPTypeFunction, Name: typ}
	case strings.HasPrefix(typ, "class:"):
		return LSPTypeRef{Kind: LSPTypeClass, Name: strings.TrimPrefix(typ, "class:")}
	case strings.HasPrefix(typ, "interface:"):
		return LSPTypeRef{Kind: LSPTypeInterface, Name: strings.TrimPrefix(typ, "interface:")}
	case strings.HasPrefix(typ, "enum:"):
		return LSPTypeRef{Kind: LSPTypeEnum, Name: strings.TrimPrefix(typ, "enum:")}
	case strings.HasPrefix(typ, "namespace:"):
		return LSPTypeRef{Kind: LSPTypeNamespace, Name: strings.TrimPrefix(typ, "namespace:")}
	case strings.HasPrefix(typ, "std:"):
		return LSPTypeRef{Kind: LSPTypeStd, Name: strings.TrimPrefix(typ, "std:")}
	case strings.HasPrefix(typ, "array:"):
		return LSPTypeRef{Kind: LSPTypeArray, Args: []LSPTypeRef{parseLSPTypeRef(strings.TrimPrefix(typ, "array:"))}}
	case typ == "array":
		return LSPTypeRef{Kind: LSPTypeArray}
	case strings.HasPrefix(typ, "task:"):
		return LSPTypeRef{Kind: LSPTypeTask, Args: []LSPTypeRef{parseLSPTypeRef(strings.TrimPrefix(typ, "task:"))}}
	default:
		return LSPTypeRef{Kind: LSPTypePrimitive, Name: typ}
	}
}

type LSPSymbol struct {
	Info SymbolInfo
}

type LSPIndex struct {
	URI        string
	Globals    map[string]LSPSymbol
	Functions  map[string]LSPSymbol
	Classes    map[string]LSPSymbol
	Interfaces map[string]LSPSymbol
	Enums      map[string]LSPSymbol
	Namespaces map[string]LSPSymbol
	Variables  map[string]LSPSymbol
}

func NewLSPIndex(uri string) *LSPIndex {
	return &LSPIndex{
		URI:        uri,
		Globals:    map[string]LSPSymbol{},
		Functions:  map[string]LSPSymbol{},
		Classes:    map[string]LSPSymbol{},
		Interfaces: map[string]LSPSymbol{},
		Enums:      map[string]LSPSymbol{},
		Namespaces: map[string]LSPSymbol{},
		Variables:  map[string]LSPSymbol{},
	}
}

func (idx *LSPIndex) Define(sym SymbolInfo) {
	if idx == nil || strings.TrimSpace(sym.Name) == "" {
		return
	}
	sym = symbolWithTypeRef(sym)
	wrapped := LSPSymbol{Info: sym}
	idx.Globals[sym.Name] = wrapped
	switch sym.Kind {
	case SymbolFunction:
		idx.Functions[sym.Name] = wrapped
	case SymbolClass:
		idx.Classes[sym.Name] = wrapped
	case SymbolInterface:
		idx.Interfaces[sym.Name] = wrapped
	case SymbolEnum:
		idx.Enums[sym.Name] = wrapped
	case SymbolNamespace, SymbolStd:
		idx.Namespaces[sym.Name] = wrapped
	default:
		idx.Variables[sym.Name] = wrapped
	}
}

func (idx *LSPIndex) ToScope() *Scope {
	scope := NewScope(nil)
	if idx == nil {
		return scope
	}
	names := make([]string, 0, len(idx.Globals))
	for name := range idx.Globals {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		scope.Define(idx.Globals[name].Info)
	}
	return scope
}

func symbolWithTypeRef(sym SymbolInfo) SymbolInfo {
	if sym.TypeRef.IsZero() {
		sym.TypeRef = parseLSPTypeRef(sym.Type)
	}
	if sym.Type == "" && !sym.TypeRef.IsZero() {
		sym.Type = sym.TypeRef.String()
	}
	return sym
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
	sym = symbolWithTypeRef(sym)
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

var classEmbedRegex = regexp.MustCompile(`(?m)\bembed\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?:;|\r?$)`)
var returnRegex = regexp.MustCompile(`(?m)return\s+(.+?)(?:;|\r?$)`)
var fileImportRegex = regexp.MustCompile(`(?m)^\s*import\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*(?:;|\r?$)`)
var libraryImportRegex = regexp.MustCompile(`(?m)^\s*import\s+(?:library|lib)\s+"([^"]+)"(?:\s+as\s+([A-Za-z_][A-Za-z0-9_]*))?\s*;?`)
var catchVarRegex = regexp.MustCompile(`(?m)\bcatch\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)
var exportedEnumBlockRegex = regexp.MustCompile(`(?s)\bexport\s+enum\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{(.*?)\}`)
var spawnFnRegex = regexp.MustCompile(`=\s*spawn\s*(?:\([^)]*\))?\s*(?:async\s+)?fn\b`)
var spawnPrefixRegex = regexp.MustCompile(`^spawn\s*(?:\([^)]*\))?\s*(?:async\s+)?(fn)\b`)
var destructuringObjectRegex = regexp.MustCompile(
	`(?m)^\s*(?:export\s+)?(?:let|const)\s+\{([^}]+)\}\s*=\s*(.+?)(?:;|\r?$)`,
)
var destructuringArrayRegex = regexp.MustCompile(
	`(?m)^\s*(?:export\s+)?(?:let|const)\s+\[([^\]]+)\]\s*=\s*(.+?)(?:;|\r?$)`,
)

type blockInfo struct {
	Kind           string
	Name           string
	ParamsText     string
	ReturnType     string
	Body           string
	Header         string
	Start          int
	End            int
	Line           int
	Column         int
	Exported       bool
	IsAsync        bool
	TypeParameters []string
}

type lexerLineVariable struct {
	Name      string
	TypeHint  string
	ExprText  string
	NameCol   int
	Nullable  bool
	HasAssign bool
}

func parseVariableLineWithLexer(line string) (lexerLineVariable, bool) {
	defer func() {
		_ = recover()
	}()
	decl, ok := lexerVariableDeclarationOnLine(line, 0)
	if !ok {
		return lexerLineVariable{}, false
	}
	exprEnd := variableInitializerEnd(line, decl.ExprStart)
	if exprEnd < decl.ExprStart {
		return lexerLineVariable{}, false
	}
	return lexerLineVariable{
		Name:      decl.Name,
		TypeHint:  decl.TypeHint,
		ExprText:  strings.TrimSpace(line[decl.ExprStart:exprEnd]),
		NameCol:   indexColumn(line, decl.Name),
		HasAssign: true,
	}, true
}

func parseFieldLineWithLexer(line string) (lexerLineVariable, bool) {
	defer func() {
		_ = recover()
	}()
	lexer := NewLexer(line, "")
	lexer.EnableASI = false
	tok := lexer.NextToken()
	if tok.Type != TOKEN_FIELD {
		return lexerLineVariable{}, false
	}
	for {
		tok = lexer.NextToken()
		if tok.Type != TOKEN_PUBLIC && tok.Type != TOKEN_PRIVATE && tok.Type != TOKEN_CONST {
			break
		}
	}
	if tok.Type != TOKEN_IDENT {
		return lexerLineVariable{}, false
	}
	name := tok.Literal
	nameCol := tok.Column
	nullable := false
	typeStart := -1
	typeEnd := -1
	exprStart := -1
	for {
		tok = lexer.NextToken()
		switch tok.Type {
		case TOKEN_QUESTION:
			nullable = true
		case TOKEN_COLON:
			if typeStart < 0 {
				typeStart = tok.Column
			}
		case TOKEN_ASSIGN:
			if typeStart >= 0 {
				typeEnd = tok.Column - 1
			}
			exprStart = tok.Column
			goto done
		case TOKEN_SEMI, TOKEN_EOF:
			if typeStart >= 0 {
				typeEnd = tok.Column - 1
			}
			goto done
		}
	}
done:
	typeHint := ""
	if typeStart >= 0 && typeEnd >= typeStart && typeEnd <= len(line) {
		typeHint = strings.TrimSpace(line[typeStart:typeEnd])
	}
	exprText := ""
	hasAssign := exprStart >= 0
	if hasAssign {
		for exprStart < len(line) && (line[exprStart] == ' ' || line[exprStart] == '\t') {
			exprStart++
		}
		exprEnd := variableInitializerEnd(line, exprStart)
		if exprEnd >= exprStart {
			exprText = strings.TrimSpace(line[exprStart:exprEnd])
		}
	}
	return lexerLineVariable{Name: name, TypeHint: typeHint, ExprText: exprText, NameCol: nameCol, Nullable: nullable, HasAssign: hasAssign}, true
}

type lexerEmbedLine struct {
	Kind string
	Name string
}

func parseEmbedLineWithLexer(line string) (lexerEmbedLine, bool) {
	defer func() {
		_ = recover()
	}()
	lexer := NewLexer(line, "")
	lexer.EnableASI = false
	tok := lexer.NextToken()
	if tok.Type == TOKEN_EXPORT {
		tok = lexer.NextToken()
	}
	kind := ""
	switch tok.Type {
	case TOKEN_EMBED_TEXT:
		kind = "embedtext"
	case TOKEN_EMBED_BYTES:
		kind = "embedbytes"
	case TOKEN_EMBED_FOLDER:
		kind = "embedfolder"
	default:
		return lexerEmbedLine{}, false
	}
	if lexer.NextToken().Type != TOKEN_STRING {
		return lexerEmbedLine{}, false
	}
	tok = lexer.NextToken()
	if tok.Type != TOKEN_LET && tok.Type != TOKEN_CONST {
		return lexerEmbedLine{}, false
	}
	nameTok := lexer.NextToken()
	if nameTok.Type != TOKEN_IDENT {
		return lexerEmbedLine{}, false
	}
	return lexerEmbedLine{Kind: kind, Name: nameTok.Literal}, true
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

	parts := []string{}
	start := 0
	depthParen := 0
	depthBracket := 0
	depthBrace := 0
	inString := byte(0)
	escaped := false

	for i := 0; i < len(typ); i++ {
		ch := typ[i]

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
			if ch == '|' && depthParen == 0 && depthBracket == 0 && depthBrace == 0 {
				part := strings.TrimSpace(typ[start:i])
				if part != "" {
					parts = append(parts, part)
				}
				start = i + 1
			}
		}
	}

	part := strings.TrimSpace(typ[start:])
	if part != "" {
		parts = append(parts, part)
	}

	return parts
}

func isNullishLSPType(typ string) bool {
	typ = strings.TrimSpace(typ)
	return typ == "null"
}

func globalPropertyMethodInfo(member string) (StdMethodInfo, bool) {
	switch strings.TrimSpace(member) {
	case "toString":
		return StdMethodInfo{
			Name:        "toString",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns a stringified version of the value.",
		}, true
	default:
		return StdMethodInfo{}, false
	}
}

func isGlobalPropertyMethod(member string) bool {
	_, ok := globalPropertyMethodInfo(member)
	return ok
}

func globalPropertyMethodType(member string) string {
	if _, ok := globalPropertyMethodInfo(member); ok {
		return "function"
	}
	return "unknown"
}

func globalPropertyMethodReturnType(member string) string {
	info, ok := globalPropertyMethodInfo(member)
	if !ok {
		return "unknown"
	}
	return firstNonEmpty(info.Returns, "any")
}

func hoverForGlobalPropertyMethod(receiverType string, member string) (HoverResult, bool) {
	info, ok := globalPropertyMethodInfo(member)
	if !ok {
		return HoverResult{}, false
	}

	receiverType = firstNonEmpty(unwrapNullableType(receiverType), "any")
	signature := formatNativeSignature(receiverType, info)

	return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + info.Description}}, true
}

func scanCatchVariables(scope *Scope, text string, pos Position, uri string) {
	posOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	if posOffset < 0 {
		posOffset = 0
	}
	if posOffset > len(text) {
		posOffset = len(text)
	}

	re := regexp.MustCompile(`\bcatch\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)
	matches := re.FindAllStringSubmatchIndex(text, -1)
	for _, match := range matches {
		start := match[0]
		if isOffsetInStringOrComment(text, start) {
			continue
		}

		name := text[match[2]:match[3]]
		openBrace := strings.LastIndex(text[match[0]:match[1]], "{")
		if openBrace < 0 {
			continue
		}
		openBrace += match[0]

		closeBrace := findMatching(text, openBrace, '{', '}')
		if closeBrace < 0 {
			closeBrace = len(text)
		}

		if posOffset <= openBrace || posOffset > closeBrace {
			continue
		}

		line := lineNumberAtOffset(text, start)
		lineText := ""
		lines := strings.Split(text, "\n")
		if line-1 >= 0 && line-1 < len(lines) {
			lineText = cleanLine(lines[line-1])
		}

		scope.Define(SymbolInfo{
			Name:      name,
			Kind:      SymbolVariable,
			Type:      "error",
			Detail:    "catch error " + name,
			Line:      line,
			Column:    indexColumn(lineText, name),
			SourceURI: uri,
		})
	}
}

func scanLoopVariables(scope *Scope, text string, cursorLine int, uri string) {
	lines := strings.Split(text, "\n")

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

		if cursorLine >= startLine && cursorLine <= endLine {
			header := strings.TrimSpace(text[start+3 : foundOpenBrace])

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

			inStart, inEnd, hasIn := topLevelInKeywordRange(header)
			if hasIn {
				lhs := strings.TrimSpace(header[:inStart])
				rhs := strings.TrimSpace(header[inEnd:])

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
					if isValidIdentifierName(itemName) {
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
					if isValidIdentifierName(itemName) {
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
					if isValidIdentifierName(indexName) {
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
				parts := strings.Split(header, ";")
				if len(parts) > 0 {
					initPart := strings.TrimSpace(parts[0])
					if strings.HasPrefix(initPart, "let ") || strings.HasPrefix(initPart, "const ") {
						decl, ok := parseVariableLineWithLexer(initPart)
						if ok {
							name := decl.Name
							typeHint := decl.TypeHint
							exprText := decl.ExprText

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

							if isValidIdentifierName(name) {
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

func scanMatchBindVariables(scope *Scope, text string, cursorLine int, uri string) {
	lines := strings.Split(text, "\n")
	offset := 0

	for {
		idx := strings.Index(text[offset:], "match")
		if idx < 0 {
			break
		}

		start := offset + idx
		if !isWordBoundaryAt(text, start, 5) {
			offset = start + 5
			continue
		}

		startLine := lineNumberAtOffset(text, start)
		if startLine > cursorLine {
			break
		}

		foundOpenBrace := -1
		parenDepth := 0
		bracketDepth := 0
		braceDepth := 0
		inString := byte(0)
		escaped := false

		for i := start + 5; i < len(text); i++ {
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
			if ch == '/' && i+1 < len(text) && text[i+1] == '/' {
				for i < len(text) && text[i] != '\n' {
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
			offset = start + 5
			continue
		}

		matchValueExpr := strings.TrimSpace(text[start+5 : foundOpenBrace])
		matchedType := inferExprTypeFromText(scope, matchValueExpr)
		matchedType = normalizeLSPType(scope, matchedType)

		closeBrace := findMatching(text, foundOpenBrace, '{', '}')
		if closeBrace < 0 {
			closeBrace = len(text)
		}

		matchBody := text[foundOpenBrace+1 : closeBrace]

		casePattern := regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s+if\b`)
		for _, caseMatch := range casePattern.FindAllStringSubmatchIndex(matchBody, -1) {
			bindName := matchBody[caseMatch[2]:caseMatch[3]]
			absOffset := foundOpenBrace + 1 + caseMatch[2]
			bindLine := lineNumberAtOffset(text, absOffset)

			if bindLine > cursorLine {
				continue
			}

			lineText := ""
			if bindLine-1 >= 0 && bindLine-1 < len(lines) {
				lineText = lines[bindLine-1]
			}

			scope.Define(SymbolInfo{
				Name:      bindName,
				Kind:      SymbolVariable,
				Type:      matchedType,
				Detail:    "match bind variable " + bindName,
				Line:      bindLine,
				Column:    indexColumn(lineText, bindName),
				SourceURI: uri,
			})
		}

		payloadPattern := regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_\.]*)\.([A-Za-z_][A-Za-z0-9_]*)\s*\(([^)]*)\)\s*\{`)
		for _, caseMatch := range payloadPattern.FindAllStringSubmatchIndex(matchBody, -1) {
			argsText := matchBody[caseMatch[6]:caseMatch[7]]
			if strings.TrimSpace(argsText) == "" {
				continue
			}

			caseAbsOffset := foundOpenBrace + 1 + caseMatch[0]
			caseLine := lineNumberAtOffset(text, caseAbsOffset)
			if caseLine > cursorLine {
				continue
			}

			lineText := ""
			if caseLine-1 >= 0 && caseLine-1 < len(lines) {
				lineText = lines[caseLine-1]
			}

			for _, rawArg := range splitTopLevel(argsText, ',') {
				argName := strings.TrimSpace(rawArg)
				if argName == "" || argName == "_" {
					continue
				}
				if !isValidIdentifierName(argName) {
					continue
				}

				scope.Define(SymbolInfo{
					Name:      argName,
					Kind:      SymbolVariable,
					Type:      matchedType,
					Detail:    "match payload variable " + argName,
					Line:      caseLine,
					Column:    indexColumn(lineText, argName),
					SourceURI: uri,
				})
			}
		}

		offset = foundOpenBrace + 1
	}
}

func resolveInterfaceSymbol(scope *Scope, ifaceName string) (SymbolInfo, bool) {
	ifaceName = strings.TrimSpace(ifaceName)
	baseName := ifaceName
	typeArgs := []string{}
	if strings.Contains(ifaceName, ":") {
		parsed, _ := parseOneLSPType(scope, strings.Split(ifaceName, ":"))
		baseName = parsed.Name
		for _, arg := range parsed.Args {
			typeArgs = append(typeArgs, formatLSPTypeStruct(arg))
		}
	}

	resolveBase := func() (SymbolInfo, bool) {
		if strings.Contains(baseName, ".") {
			parts := strings.SplitN(baseName, ".", 2)
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

		if sym, ok := scope.Resolve(baseName); ok && sym.Kind == SymbolInterface {
			return sym, true
		}

		for s := scope; s != nil; s = s.Parent {
			for _, sym := range s.Symbols {
				if sym.Kind == SymbolNamespace {
					if member, ok := sym.Members[baseName]; ok && member.Kind == SymbolInterface && !isPrivateImportMember(member) {
						return member, true
					}
				}
			}
		}

		shortName := baseName
		if idx := strings.LastIndex(baseName, "."); idx >= 0 {
			shortName = baseName[idx+1:]
		}
		for _, entry := range lspImportExportCache {
			if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolInterface && !isPrivateImportMember(member) {
				return member, true
			}
		}
		return SymbolInfo{}, false
	}

	sym, ok := resolveBase()
	if ok {
		return instantiateSymbol(sym, typeArgs), true
	}

	return SymbolInfo{}, false
}

func resolveClassSymbol(scope *Scope, className string) (SymbolInfo, bool) {
	className = strings.TrimSpace(className)
	baseName := className
	typeArgs := []string{}
	if strings.Contains(className, ":") {
		parsed, _ := parseOneLSPType(scope, strings.Split(className, ":"))
		baseName = parsed.Name
		for _, arg := range parsed.Args {
			typeArgs = append(typeArgs, formatLSPTypeStruct(arg))
		}
	}

	resolveBase := func() (SymbolInfo, bool) {
		if sym, ok := scope.Resolve(baseName); ok && sym.Kind == SymbolClass {
			return sym, true
		}

		if strings.Contains(baseName, ".") {
			parts := strings.SplitN(baseName, ".", 2)
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
					if member, ok := sym.Members[baseName]; ok && member.Kind == SymbolClass && !isPrivateImportMember(member) {
						return member, true
					}
				}
			}
		}

		shortName := baseName
		if idx := strings.LastIndex(baseName, "."); idx >= 0 {
			shortName = baseName[idx+1:]
		}
		for _, entry := range lspImportExportCache {
			if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolClass && !isPrivateImportMember(member) {
				return member, true
			}
		}
		return SymbolInfo{}, false
	}

	sym, ok := resolveBase()
	if ok {
		return instantiateSymbol(sym, typeArgs), true
	}

	return SymbolInfo{}, false
}

func instantiateSymbol(sym SymbolInfo, typeArgs []string) SymbolInfo {
	if len(typeArgs) == 0 || len(sym.TypeParameters) == 0 {
		return sym
	}

	subst := map[string]string{}
	for i, tp := range sym.TypeParameters {
		if i < len(typeArgs) {
			subst[tp] = typeArgs[i]
		}
	}

	newSym := sym
	if sym.Fields != nil {
		newSym.Fields = make(map[string]SymbolInfo)
		for k, v := range sym.Fields {
			newField := v
			newField.Type = substituteLSPType(v.Type, subst)
			newSym.Fields[k] = newField
		}
	}

	if sym.Methods != nil {
		newSym.Methods = make(map[string]SymbolInfo)
		for k, v := range sym.Methods {
			newMethod := v
			newMethod.Returns = substituteLSPType(v.Returns, subst)
			newMethod.Params = make([]StdArg, len(v.Params))
			for i, p := range v.Params {
				newMethod.Params[i] = p
				newMethod.Params[i].Type = substituteLSPType(p.Type, subst)
			}
			newSym.Methods[k] = newMethod
		}
	}

	if len(sym.Implements) > 0 {
		newSym.Implements = make([]string, len(sym.Implements))
		for i, imp := range sym.Implements {
			newSym.Implements[i] = substituteLSPType(imp, subst)
		}
	}

	return newSym
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
				if member, ok := sym.Members[enumName]; ok && member.Kind == SymbolEnum && !isPrivateImportMember(member) {
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
		if member, ok := entry.exports[shortName]; ok && member.Kind == SymbolEnum && !isPrivateImportMember(member) {
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

	if isGlobalPropertyMethod(member) {
		return true
	}

	if sym.Kind == SymbolNamespace {
		memberSym, ok := sym.Members[member]
		return ok && !isPrivateImportMember(memberSym)
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

func makeRangeDiagnosticFromByteRange(r byteIdentifierRange, severity int, message string) map[string]any {
	return makeRangeDiagnostic(r.Line, r.Start, r.End, severity, message)
}

func byteRangeFromLineColumn(text string, line int, column int) (byteIdentifierRange, bool) {
	if line <= 0 || column <= 0 {
		return byteIdentifierRange{}, false
	}
	lineIndex := line - 1
	lineText := getLine(text, lineIndex)
	if lineIndex < 0 || column-1 > len(lineText) {
		return byteIdentifierRange{}, false
	}
	start := column - 1
	return byteIdentifierRange{
		Line:  lineIndex,
		Start: start,
		End:   start + wordLengthAtColumn(lineText, start),
	}, true
}

func byteRangeForNameAtLineColumn(text string, line int, column int, name string) (byteIdentifierRange, bool) {
	if strings.TrimSpace(name) == "" {
		return byteRangeFromLineColumn(text, line, column)
	}
	if line <= 0 {
		return byteIdentifierRange{}, false
	}
	lineIndex := line - 1
	lineText := getLine(text, lineIndex)
	code := stripLineComment(lineText)
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
	if match := re.FindStringIndex(code); match != nil {
		return byteIdentifierRange{Line: lineIndex, Start: match[0], End: match[1]}, true
	}
	if column > 0 {
		return byteRangeFromLineColumn(text, line, column)
	}
	return byteIdentifierRange{}, false
}

func byteRangeForSymbol(text string, sym SymbolInfo) (byteIdentifierRange, bool) {
	return byteRangeForNameAtLineColumn(text, sym.Line, sym.Column, sym.Name)
}

func byteRangeForExpr(text string, expr Expr) (byteIdentifierRange, bool) {
	switch e := expr.(type) {
	case IdentExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, e.Name)
	case NumberExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, strconv.Itoa(e.Value))
	case FloatExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, strconv.FormatFloat(e.Value, 'f', -1, 64))
	case ThisExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, "this")
	case PropertyExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, e.Name)
	case MemberCallExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, e.Method)
	case CallExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, e.Name)
	case CallValueExpr:
		if r, ok := byteRangeForExpr(text, e.Callee); ok {
			return r, true
		}
		return byteRangeFromLineColumn(text, e.Line, e.Column)
	case InstantiatedExpr:
		if r, ok := byteRangeForExpr(text, e.Object); ok {
			return r, true
		}
		return byteRangeFromLineColumn(text, e.Line, e.Column)
	case AwaitExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, "await")
	case DeferExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, "defer")
	case ObjectInExpr:
		return byteRangeForNameAtLineColumn(text, e.Line, e.Column, "in")
	case NullishCoalescingExpr:
		return byteRangeFromLineColumn(text, e.Line, e.Column)
	case BinaryExpr:
		if r, ok := byteRangeForExpr(text, e.Left); ok {
			return r, true
		}
		return byteRangeForExpr(text, e.Right)
	case UnaryExpr:
		return byteRangeForExpr(text, e.Right)
	case SpreadExpr:
		return byteRangeForExpr(text, e.Value)
	}
	line, column := nodePosition(expr)
	return byteRangeFromLineColumn(text, line, column)
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

	statements, _ := parseTinyForLSP(uri, text)
	index := buildLSPIndexFromAST(uri, text, statements)
	scope = index.ToScope()

	lspBaseScopeCache[path] = lspBaseScopeCacheEntry{
		text:  text,
		scope: scope,
	}

	return scope
}

func buildLSPIndexFromAST(uri string, text string, statements []Stmt) *LSPIndex {
	scope := NewScope(nil)
	index := NewLSPIndex(uri)
	collectImportsFromAST(index, scope, uri, statements)
	collectTopLevelSymbolsFromAST(index, scope, text, uri, statements)
	return index
}

func defineIndexedSymbol(index *LSPIndex, scope *Scope, sym SymbolInfo) {
	if index != nil {
		index.Define(sym)
	}
	if scope != nil {
		scope.Define(sym)
	}
}

func collectImportsFromAST(index *LSPIndex, scope *Scope, uri string, statements []Stmt) {
	for _, raw := range statements {
		stmt, _ := unwrapExport(raw)
		importStmt, ok := stmt.(ImportStmt)
		if !ok {
			continue
		}

		if importStmt.Std {
			alias := importStmt.Alias
			if alias == "" {
				alias = importStmt.Path
			}
			resolvedPath := "std:" + importStmt.Path
			exports := loadTinyFileExports(resolvedPath, map[string]bool{})
			defineIndexedSymbol(index, scope, SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				TypeRef:   LSPTypeRef{Kind: LSPTypeNamespace, Name: alias},
				Detail:    "std module " + importStmt.Path,
				Line:      importStmt.Line,
				Column:    importStmt.Column,
				Members:   exports,
				SourceURI: pathToFileURI(resolvedPath),
			})
			continue
		}

		if importStmt.Library {
			alias := importStmt.Alias
			if alias == "" {
				alias = defaultLibraryAlias(importStmt.Path)
			}
			resolvedPath := resolveLibraryImportPath(importStmt.Path, uri)
			exports := loadLibraryFileExportsForLSP(importStmt.Path, uri, map[string]bool{})
			defineIndexedSymbol(index, scope, SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				TypeRef:   LSPTypeRef{Kind: LSPTypeNamespace, Name: alias},
				Detail:    "library " + importStmt.Path,
				Line:      importStmt.Line,
				Column:    importStmt.Column,
				Members:   exports,
				SourceURI: pathToFileURI(resolvedPath),
			})
			continue
		}

		if importStmt.Plugin {
			continue
		}

		importPath := resolveImportPath(uri, importStmt.Path)
		exports := loadTinyFileExports(importPath, map[string]bool{})
		if importStmt.Alias != "" {
			defineIndexedSymbol(index, scope, SymbolInfo{
				Name:      importStmt.Alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + importStmt.Alias,
				TypeRef:   LSPTypeRef{Kind: LSPTypeNamespace, Name: importStmt.Alias},
				Detail:    "module " + importStmt.Path,
				Line:      importStmt.Line,
				Column:    importStmt.Column,
				Members:   exports,
				SourceURI: pathToFileURI(importPath),
			})
			continue
		}
		for _, sym := range exports {
			defineIndexedSymbol(index, scope, sym)
		}
	}
}

func scanInterfaceFields(scope *Scope, body string, uri string, baseLine int) map[string]SymbolInfo {
	fields := map[string]SymbolInfo{}
	lines := strings.Split(body, "\n")

	for i, raw := range lines {
		line := cleanLine(raw)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") {
			continue
		}

		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		rawName := strings.TrimSpace(parts[0])
		typeHint := strings.TrimSpace(strings.TrimSuffix(parts[1], ";"))

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

func collectTopLevelSymbolsFromAST(index *LSPIndex, scope *Scope, text string, uri string, statements []Stmt) {
	analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: scope, scope: scope}
	for _, raw := range statements {
		stmt, exported := unwrapExport(raw)
		switch s := stmt.(type) {
		case ImportStmt:
			continue
		case InterfaceStmt:
			sym := interfaceSymbolFromStmt(scope, s, uri, text)
			defineIndexedSymbol(index, scope, sym)
		case EnumStmt:
			sym := enumSymbolFromStmt(s, uri, text)
			defineIndexedSymbol(index, scope, sym)
		case ClassStmt:
			sym := classSymbolFromStmt(scope, s, uri, text)
			defineIndexedSymbol(index, scope, sym)
		case FunctionStmt:
			sym := functionSymbolFromStmt(scope, s, uri, text, "fn "+s.Name)
			defineIndexedSymbol(index, scope, sym)
		case NativeFnStmt:
			sym := nativeFunctionSymbolFromStmt(scope, s, uri, text)
			defineIndexedSymbol(index, scope, sym)
		case ExternalFnStmt:
			sym := externalFunctionSymbolFromStmt(scope, s, uri)
			defineIndexedSymbol(index, scope, sym)
		case ExternalGlobalStmt:
			sym := externalGlobalSymbolFromStmt(scope, s, uri)
			defineIndexedSymbol(index, scope, sym)
		case EmbedStmt:
			sym := embedSymbolFromStmt(s, uri)
			defineIndexedSymbol(index, scope, sym)
		case VariableStmt:
			defineIndexedSymbol(index, scope, variableSymbolFromStmt(scope, analyzer, s, uri, exported))
		case DestructureStmt:
			defineDestructuredSymbolsFromStmt(index, scope, analyzer, s, uri, exported)
		case NamespaceStmt:
			sym := namespaceSymbolFromStmt(analyzer, s)
			defineIndexedSymbol(index, scope, sym)
		}
	}

	fallbackSymbols := map[string]bool{}
	for _, block := range findBlocks(text, "class") {
		if _, exists := scope.Resolve(block.Name); !exists {
			fallbackSymbols[block.Name] = true
			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolClass,
				Type:           "class:" + block.Name,
				Detail:         "class " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Fields:         map[string]SymbolInfo{},
				Methods:        map[string]SymbolInfo{},
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			defineIndexedSymbol(index, scope, sym)
		}
	}
	for _, block := range findBlocks(text, "interface") {
		if _, exists := scope.Resolve(block.Name); !exists {
			fallbackSymbols[block.Name] = true
			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolInterface,
				Type:           "interface:" + block.Name,
				Detail:         "interface " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Fields:         map[string]SymbolInfo{},
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			defineIndexedSymbol(index, scope, sym)
		}
	}
	for _, block := range findBlocks(text, "enum") {
		if _, exists := scope.Resolve(block.Name); !exists {
			fallbackSymbols[block.Name] = true
			sym := SymbolInfo{
				Name:      block.Name,
				Kind:      SymbolEnum,
				Type:      "enum:" + block.Name,
				Detail:    "enum " + block.Name,
				Line:      block.Line,
				Column:    block.Column,
				SourceURI: uri,
				Members:   map[string]SymbolInfo{},
				Doc:       findDocumentationComments(text, block.Line-1),
			}
			defineIndexedSymbol(index, scope, sym)
		}
	}
	for _, block := range findBlocks(text, "namespace") {
		if _, exists := scope.Resolve(block.Name); !exists {
			fallbackSymbols[block.Name] = true
			sym := SymbolInfo{
				Name:      block.Name,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + block.Name,
				Detail:    "namespace " + block.Name,
				Line:      block.Line,
				Column:    block.Column,
				SourceURI: uri,
				Members:   map[string]SymbolInfo{},
			}
			defineIndexedSymbol(index, scope, sym)
		}
	}

	for _, block := range findBlocks(text, "namespace") {
		if fallbackSymbols[block.Name] {
			if sym, exists := scope.Resolve(block.Name); exists && sym.Kind == SymbolNamespace {
				members := map[string]SymbolInfo{}
				for _, cb := range findBlocks(text, "class") {
					if cb.Start > block.Start && cb.Start < block.End {
						members[cb.Name] = SymbolInfo{
							Name:      cb.Name,
							Kind:      SymbolClass,
							Type:      "class:" + block.Name + "." + cb.Name,
							Detail:    "class " + cb.Name,
							Line:      cb.Line,
							Column:    cb.Column,
							SourceURI: uri,
							Fields:    map[string]SymbolInfo{},
							Methods:   map[string]SymbolInfo{},
						}
					}
				}
				for _, f := range findBlocks(text, "fn") {
					if f.Start > block.Start && f.Start < block.End {
						isMethod := false
						for _, cb := range findBlocks(text, "class") {
							if f.Start > cb.Start && f.Start < cb.End {
								isMethod = true
								break
							}
						}
						if isMethod {
							continue
						}

						params := normalizeStdArgs(scope, parseFunctionParams(f.ParamsText))
						nestedScope := NewScope(scope)
						for _, p := range params {
							nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
						}
						returnType := inferReturnTypeFromBody(nestedScope, f.Body, f.ReturnType)
						if f.IsAsync {
							returnType = "task:" + returnType
						}
						members[f.Name] = SymbolInfo{
							Name:      f.Name,
							Kind:      SymbolFunction,
							Type:      "function",
							Detail:    "fn " + f.Name,
							Line:      f.Line,
							Column:    f.Column,
							SourceURI: uri,
							Params:    params,
							Returns:   returnType,
						}
					}
				}
				for _, eb := range findBlocks(text, "enum") {
					if eb.Start > block.Start && eb.Start < block.End {
						members[eb.Name] = SymbolInfo{
							Name:      eb.Name,
							Kind:      SymbolEnum,
							Type:      "enum:" + block.Name + "." + eb.Name,
							Detail:    "enum " + eb.Name,
							Line:      eb.Line,
							Column:    eb.Column,
							SourceURI: uri,
							Members:   map[string]SymbolInfo{},
						}
					}
				}
				sym.Members = members
				defineIndexedSymbol(index, scope, sym)
			}
		}
	}
	for _, block := range findBlocks(text, "class") {
		if fallbackSymbols[block.Name] {
			if sym, exists := scope.Resolve(block.Name); exists && sym.Kind == SymbolClass {
				fields := scanClassFields(scope, block.Body, uri, block.Line)
				methods := map[string]SymbolInfo{}
				collectEmbeddedSymbolsFromBody(scope, block.Body, fields, methods, uri, block.Line)

				for _, b := range findBlocks(text, "fn") {
					if b.Start > block.Start && b.Start < block.End {
						params := normalizeStdArgs(scope, parseFunctionParams(b.ParamsText))
						nestedScope := NewScope(scope)
						for _, p := range params {
							nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
						}
						returnType := inferReturnTypeFromBody(nestedScope, b.Body, b.ReturnType)
						if b.IsAsync {
							returnType = "task:" + returnType
						}
						detail := "method " + block.Name + "." + b.Name
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

				sym.Fields = fields
				sym.Methods = methods
				defineIndexedSymbol(index, scope, sym)
			}
		}
	}

	for _, block := range findBlocks(text, "interface") {
		if fallbackSymbols[block.Name] {
			if sym, exists := scope.Resolve(block.Name); exists && sym.Kind == SymbolInterface {
				fields := scanInterfaceFields(scope, block.Body, uri, block.Line)
				sym.Fields = fields
				defineIndexedSymbol(index, scope, sym)
			}
		}
	}

	for _, block := range findBlocks(text, "enum") {
		if fallbackSymbols[block.Name] {
			if sym, exists := scope.Resolve(block.Name); exists && sym.Kind == SymbolEnum {
				members := map[string]SymbolInfo{}
				memberType := determineEnumMemberTypeFromText(block.Body)

				rawMembers := splitTopLevel(block.Body, ',')
				for _, raw := range rawMembers {
					name := strings.TrimSpace(raw)
					if name == "" {
						continue
					}

					if strings.Contains(name, "=") {
						name = strings.TrimSpace(strings.SplitN(name, "=", 2)[0])
					}
					if strings.Contains(name, "(") {
						name = strings.TrimSpace(strings.SplitN(name, "(", 2)[0])
					}

					members[name] = SymbolInfo{
						Name:      name,
						Kind:      SymbolVariable,
						Type:      memberType,
						Detail:    "enum member " + block.Name + "." + name,
						Line:      block.Line,
						Column:    block.Column,
						SourceURI: uri,
					}
				}
				sym.Members = members
				defineIndexedSymbol(index, scope, sym)
			}
		}
	}

	classBlocks := findBlocks(text, "class")
	for _, block := range findBlocks(text, "fn") {
		isMethod := false
		for _, cb := range classBlocks {
			if block.Start > cb.Start && block.Start < cb.End {
				isMethod = true
				break
			}
		}
		if isMethod {
			continue
		}

		if _, exists := scope.Resolve(block.Name); !exists {
			params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
			nestedScope := NewScope(scope)
			for _, p := range params {
				nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
			}
			returnType := inferReturnTypeFromBody(nestedScope, block.Body, block.ReturnType)
			if block.IsAsync {
				returnType = "task:" + returnType
			}

			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolFunction,
				Type:           "function",
				Detail:         "fn " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Params:         params,
				Returns:        returnType,
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			defineIndexedSymbol(index, scope, sym)
		}
	}
}

func interfaceSymbolFromStmt(scope *Scope, s InterfaceStmt, uri string, text string) SymbolInfo {
	fields := map[string]SymbolInfo{}
	for fieldName, fieldHint := range s.Fields {
		line, column := interfaceFieldPositionFromText(text, s, fieldName)
		fields[fieldName] = SymbolInfo{
			Name:      fieldName,
			Kind:      SymbolField,
			Type:      normalizeLSPType(scope, fieldHint.Name),
			TypeRef:   parseLSPTypeRef(normalizeLSPType(scope, fieldHint.Name)),
			Detail:    "interface field " + fieldName,
			Line:      line,
			Column:    column,
			SourceURI: uri,
		}
	}
	return SymbolInfo{
		Name:           s.Name,
		Kind:           SymbolInterface,
		Type:           "interface:" + s.Name,
		TypeRef:        LSPTypeRef{Kind: LSPTypeInterface, Name: s.Name},
		Detail:         "interface " + s.Name,
		Line:           s.Line,
		Column:         s.Column,
		SourceURI:      uri,
		Fields:         fields,
		Doc:            findDocumentationComments(text, s.Line-1),
		TypeParameters: s.TypeParameters,
	}
}

func interfaceFieldPositionFromText(text string, iface InterfaceStmt, fieldName string) (int, int) {
	for _, block := range findBlocks(text, "interface") {
		if block.Name != iface.Name || block.Line != iface.Line {
			continue
		}
		bodyStart := block.Start + strings.Index(text[block.Start:], "{") + 1
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(fieldName) + `\??\s*:`)
		if loc := re.FindStringIndex(block.Body); loc != nil {
			absOffset := bodyStart + loc[0]
			line := lineNumberAtOffset(text, absOffset)
			column := findColumnAtLine(text, fieldName, line)
			return line, column
		}
	}
	return iface.Line, iface.Column
}

func functionSymbolFromStmt(scope *Scope, s FunctionStmt, uri string, text string, detail string) SymbolInfo {
	returns := returnTypeNameScoped(scope, s.ReturnType)
	if s.ReturnType.IsEmpty() {
		returns = inferReturnTypeFromFunctionStmt(scope, s, uri, text)
	}
	return SymbolInfo{
		Name:           s.Name,
		Kind:           SymbolFunction,
		Type:           "function",
		TypeRef:        LSPTypeRef{Kind: LSPTypeFunction},
		Detail:         detail,
		Line:           s.Line,
		Column:         s.Column,
		SourceURI:      uri,
		Params:         stdArgsFromParams(scope, s.Params),
		Returns:        returns,
		Doc:            findDocumentationComments(text, s.Line-1),
		TypeParameters: s.TypeParameters,
	}
}

func inferReturnTypeFromFunctionStmt(scope *Scope, fn FunctionStmt, uri string, text string) string {
	fnScope := NewScope(scope)
	for _, param := range fn.Params {
		typ := typeHintName(param.TypeHint, "any")
		typ = normalizeLSPType(fnScope, typ)
		fnScope.Define(SymbolInfo{
			Name:      param.Name,
			Kind:      SymbolVariable,
			Type:      typ,
			Detail:    "parameter " + param.Name,
			Line:      fn.Line,
			Column:    fn.Column,
			SourceURI: uri,
		})
	}

	analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: scope, scope: fnScope}
	returns := collectReturnTypesFromStmts(analyzer, fn.Body)
	if len(returns) == 0 {
		return "null"
	}
	return strings.Join(dedupeStrings(returns), " | ")
}

func collectReturnTypesFromStmts(analyzer *astSemanticAnalyzer, statements []Stmt) []string {
	var returns []string
	for _, raw := range statements {
		stmt, _ := unwrapExport(raw)
		switch s := stmt.(type) {
		case ReturnStmt:
			if !s.HasValue {
				returns = append(returns, "null")
			} else {
				returns = append(returns, normalizeLSPType(analyzer.scope, analyzer.inferExprType(s.Value)))
			}
		case IfStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.ThenBody)...)
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.ElseBody)...)
		case WhileStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.Body)...)
		case ForStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.Body)...)
		case ForInStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.Body)...)
		case TryCatchStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.TryBody)...)
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.CatchBody)...)
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.FinallyBody)...)
		case LockStmt:
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.Block)...)
		case MatchStmt:
			for _, c := range s.Cases {
				returns = append(returns, collectReturnTypesFromStmts(analyzer, c.Body)...)
			}
			returns = append(returns, collectReturnTypesFromStmts(analyzer, s.Default)...)
		}
	}
	return returns
}

func dedupeStrings(values []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func nativeFunctionSymbolFromStmt(scope *Scope, s NativeFnStmt, uri string, text string) SymbolInfo {
	return SymbolInfo{
		Name:      s.Name,
		Kind:      SymbolFunction,
		Type:      "function",
		TypeRef:   LSPTypeRef{Kind: LSPTypeFunction},
		Detail:    "native fn " + s.Name,
		Line:      s.Line,
		Column:    s.Column,
		SourceURI: uri,
		Params:    stdArgsFromParams(scope, s.Params),
		Returns:   returnTypeNameScoped(scope, s.ReturnType),
		Doc:       findDocumentationComments(text, s.Line-1),
	}
}

func externalFunctionSymbolFromStmt(scope *Scope, s ExternalFnStmt, uri string) SymbolInfo {
	return SymbolInfo{
		Name:      s.Name,
		Kind:      SymbolFunction,
		Type:      "function",
		Detail:    "external fn " + s.Name,
		Line:      s.Line,
		Column:    s.Column,
		SourceURI: uri,
		Params:    stdArgsFromParams(scope, s.Params),
		Returns:   returnTypeNameScoped(scope, s.ReturnType),
	}
}

func externalGlobalSymbolFromStmt(scope *Scope, s ExternalGlobalStmt, uri string) SymbolInfo {
	typ := typeHintName(s.Type, "any")
	return SymbolInfo{
		Name:      s.Name,
		Kind:      SymbolVariable,
		Type:      typ,
		Detail:    "external const " + s.Name,
		Line:      s.Line,
		Column:    s.Column,
		SourceURI: uri,
	}
}

func embedSymbolFromStmt(s EmbedStmt, uri string) SymbolInfo {
	typ := s.TypeHint.Name
	if typ == "" {
		typ = "string"
	}
	return SymbolInfo{
		Name:      s.Name,
		Kind:      SymbolVariable,
		Type:      typ,
		TypeRef:   parseLSPTypeRef(typ),
		Detail:    "variable " + s.Name,
		Line:      s.Line,
		Column:    s.Column,
		SourceURI: uri,
	}
}

func variableSymbolFromStmt(scope *Scope, analyzer *astSemanticAnalyzer, s VariableStmt, uri string, exported bool) SymbolInfo {
	typ := "unknown"
	fields := map[string]SymbolInfo(nil)
	if !s.TypeHint.IsEmpty() {
		typ = normalizeLSPType(scope, s.TypeHint.Name)
	} else {
		typ = normalizeLSPType(scope, analyzer.inferExprType(s.Value))
		if typ == "object" {
			fields = analyzer.fieldsFromObjectExpr(s.Value, s.Line)
		}
	}
	detail := "variable " + s.Name
	if s.Constant {
		detail = "constant " + s.Name
	}
	if !exported {
		detail = "private " + detail
	}
	return SymbolInfo{
		Name:      s.Name,
		Kind:      SymbolVariable,
		Type:      typ,
		TypeRef:   parseLSPTypeRef(typ),
		Detail:    detail,
		Line:      s.Line,
		Column:    s.Column,
		SourceURI: uri,
		Fields:    fields,
	}
}

func defineDestructuredSymbolsFromStmt(index *LSPIndex, scope *Scope, analyzer *astSemanticAnalyzer, s DestructureStmt, uri string, exported bool) {
	typ := normalizeLSPType(scope, analyzer.inferExprType(s.Value))
	fields := map[string]SymbolInfo(nil)
	if typ == "object" {
		fields = analyzer.fieldsFromObjectExpr(s.Value, s.Line)
	}
	ifaceSym, hasIface := resolveInterfaceFieldsForDestructure(scope, typ)

	for _, info := range collectDestructuredFieldInfo(s.Target, s.Line, s.Column) {
		fieldTyp := "any"
		if !info.IsSpread {
			if fields != nil {
				if sym, ok := fields[info.Key]; ok {
					fieldTyp = sym.Type
				}
			}
			if fieldTyp == "any" && hasIface {
				if resolved := resolveFieldTypeFromInterface(ifaceSym, info.Key); resolved != "" {
					fieldTyp = resolved
				}
			}
		}

		detail := "variable " + info.VarName
		if s.Constant {
			detail = "constant " + info.VarName
		}
		if !exported {
			detail = "private " + detail
		}
		defineIndexedSymbol(index, scope, SymbolInfo{
			Name:      info.VarName,
			Kind:      SymbolVariable,
			Type:      fieldTyp,
			TypeRef:   parseLSPTypeRef(fieldTyp),
			Detail:    detail,
			Line:      firstPositiveInt(info.Line, s.Line),
			Column:    firstPositiveInt(info.Column, s.Column),
			SourceURI: uri,
		})
	}
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
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
	scanCatchVariables(scope, text, pos, uri)

	currentFunction := functionBlockAtLine(text, pos.Line)
	if currentFunction != nil {
		inferredTypes := expectedInlineFunctionParamTypes(scope, text, bytePositionAtOffset(text, currentFunction.Start), currentFunction.Start)
		if len(inferredTypes) == 0 {
			inferredTypes = expectedInlineFunctionParamTypesFromObject(scope, text, currentFunction.Start)
		}
		for i, param := range parseFunctionParams(currentFunction.ParamsText) {
			paramType := normalizeLSPType(scope, param.Type)
			if param.Type == "any" && i < len(inferredTypes) && strings.TrimSpace(inferredTypes[i]) != "" {
				paramType = normalizeLSPType(scope, inferredTypes[i])
			}

			scope.Define(SymbolInfo{
				Name:      param.Name,
				Kind:      SymbolVariable,
				Type:      paramType,
				Detail:    "parameter " + param.Name,
				Line:      currentFunction.Line,
				Column:    1,
				SourceURI: uri,
			})
		}
	}

	classBlocks := findBlocks(text, "class")
	functionBlocks := findBlocks(text, "fn")
	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		line := cleanLine(lines[lineIndex])
		if line == "" {
			continue
		}

		lineOffset := offsetAtLine(text, lineIndex+1)
		if isOffsetInStringOrComment(text, lineOffset) {
			continue
		}

		if declarationVisibleInScope(lineOffset, currentFunction, functionBlocks) {
			scanVariableLine(scope, text, line, lineIndex+1, uri)
			scanDestructuringLine(scope, line, lineIndex+1, uri)
			scanEmbedLine(scope, line, lineIndex+1, uri)
		}

		if !blockInsideAny(lineOffset, classBlocks) {
			scanFieldLine(scope, line, lineIndex+1, uri)
		}
	}

	if ifLine, isInElse, ok := findEnclosingIfAndElse(text, pos); ok {
		applyTypeNarrowing(scope, ifLine, isInElse)
	}

	scanVariableDeclarations(scope, text, maxLine, uri, pos)

	scanInlineAnonymousFunctionParams(scope, text, pos, uri)
	applyPriorGuardReturnNarrowing(scope, text, pos)

	if ifLine, isInElse, ok := findEnclosingIfAndElse(text, pos); ok {
		applyTypeNarrowing(scope, ifLine, isInElse)
	}

	scanLoopVariables(scope, text, pos.Line+1, uri)
	scanMatchBindVariables(scope, text, pos.Line+1, uri)

	if ifLine, isInElse, ok := findEnclosingIfAndElse(text, pos); ok {
		applyTypeNarrowing(scope, ifLine, isInElse)
	}

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

func findObjectLiteralBraceOffset(text string, cursorOffset int) int {
	braceDepth := 0
	for i := cursorOffset - 1; i >= 0; i-- {
		ch := text[i]
		if ch == '}' {
			braceDepth++
		} else if ch == '{' {
			if braceDepth == 0 {
				return i
			}
			braceDepth--
		}
	}
	return -1
}

func objectLiteralCompletions(scope *Scope, text string, pos Position) []CompletionItem {
	if !isCursorInsideObjectLiteral(text, pos) {
		return nil
	}

	cursorOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	objBraceOffset := findObjectLiteralBraceOffset(text, cursorOffset)
	var typeName string
	var ok bool
	if objBraceOffset != -1 {
		typeName, ok = findObjectTypeHintAtOffset(text, objBraceOffset, scope)
	}

	if !ok {
		typeName, ok = findObjectTypeHintAtPosition(text, pos)
		if !ok {
			typeName, ok = findFunctionArgumentTypeHint(scope, text, pos)
			if !ok {
				return nil
			}
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

func isNumericExpr(e Expr) bool {
	switch val := e.(type) {
	case NumberExpr, FloatExpr:
		return true
	case UnaryExpr:
		return isNumericExpr(val.Right)
	}
	return false
}

func determineEnumMemberTypeFromStmt(enum EnumStmt) string {
	if len(enum.Members) == 0 {
		return "string"
	}
	for _, member := range enum.Members {
		if len(member.VariantParams) > 0 {
			return "any"
		}
	}
	firstVal := enum.Members[0].Value
	if isNumericExpr(firstVal) {
		return "number"
	}
	return "string"
}

func isSimpleIdentifier(expr string) bool {
	expr = strings.TrimSpace(expr)
	return isValidIdentifierName(expr)
}

func isValidIdentifierName(name string) bool {
	if name == "" {
		return false
	}
	if !(name[0] == '_' || (name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= 'a' && name[0] <= 'z')) {
		return false
	}
	for i := 1; i < len(name); i++ {
		if !isIdentByte(name[i]) {
			return false
		}
	}
	return true
}

func topLevelInKeywordRange(text string) (int, int, bool) {
	tokens := lexedTokensForText(text)
	for _, tok := range tokens {
		if tok.Token.Type == TOKEN_IN {
			return tok.Offset, tok.Offset + len(tok.Token.Literal), true
		}
	}
	return 0, 0, false
}

func determineEnumMemberTypeFromText(body string) string {
	rawMembers := splitTopLevel(body, ',')
	if len(rawMembers) == 0 {
		return "string"
	}
	for _, raw := range rawMembers {
		if strings.Contains(raw, "(") {
			return "any"
		}
	}

	for _, m := range rawMembers {
		if strings.Contains(m, "iota") {
			return "number"
		}
	}

	first := strings.TrimSpace(rawMembers[0])
	if strings.Contains(first, "=") {
		parts := strings.SplitN(first, "=", 2)
		val := strings.TrimSpace(parts[1])
		if strings.HasPrefix(val, "\"") || strings.HasPrefix(val, "'") || strings.HasPrefix(val, "`") {
			return "string"
		}
		cleanVal := strings.TrimPrefix(val, "-")
		cleanVal = strings.TrimSpace(cleanVal)
		if _, err := strconv.ParseFloat(cleanVal, 64); err == nil {
			return "number"
		}
	}

	return "string"
}

func scanFieldLine(scope *Scope, line string, lineNumber int, uri string) {
	decl, ok := parseFieldLineWithLexer(line)
	if !ok {
		return
	}

	name := decl.Name

	if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
		return
	}

	typeHint := decl.TypeHint
	exprText := decl.ExprText

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

	if decl.Nullable {
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
	embed, ok := parseEmbedLineWithLexer(line)
	if !ok {
		return
	}

	kind := embed.Kind
	name := embed.Name

	typ := "string"
	if kind == "embedbytes" {
		typ = "buffer"
	} else if kind == "embedfolder" {
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

func scanVariableLine(scope *Scope, sourceText string, line string, lineNumber int, uri string) {
	decl, ok := parseVariableLineWithLexer(line)
	if !ok {
		return
	}

	name := decl.Name

	if existing, ok := scope.Resolve(name); ok && existing.Type != "any" && existing.Type != "unknown" {
		return
	}

	typeHint := decl.TypeHint
	exprText := decl.ExprText

	typ := "any"
	fields := map[string]SymbolInfo(nil)

	if typeHint != "" {
		typ = normalizeLSPType(scope, typeHint)
	} else {
		if narrowed := narrowedTypeFromEnclosingIf(scope, sourceText, lineNumber, exprText); narrowed != "" {
			typ = narrowed
		} else {
			typ = inferExprTypeFromText(scope, exprText)
			typ = normalizeLSPType(scope, typ)
			if typ == "object" {
				fields = inferObjectFieldsFromText(scope, exprText, uri, lineNumber)
			}
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

func scanDestructuringLine(scope *Scope, line string, lineNumber int, uri string) {
	line = strings.TrimPrefix(line, "export ")

	isConst := strings.Contains(line, "const ")

	match := destructuringObjectRegex.FindStringSubmatch(line)
	if match != nil {
		exprText := strings.TrimSpace(match[2])
		typ := inferExprTypeFromText(scope, exprText)
		typ = normalizeLSPType(scope, typ)
		fields := map[string]SymbolInfo(nil)
		if typ == "object" {
			fields = inferObjectFieldsFromText(scope, exprText, uri, lineNumber)
		}

		ifaceSym, hasIface := resolveInterfaceFieldsForDestructure(scope, typ)

		names := extractDestructuredNames(match[1])
		for _, name := range names {
			if existing, ok := scope.Resolve(name); ok && existing.Type != "any" && existing.Type != "unknown" {
				continue
			}

			var fieldTyp string
			if fields != nil {
				if sym, ok := fields[name]; ok {
					fieldTyp = sym.Type
				}
			}
			if fieldTyp == "" && hasIface {
				fieldTyp = resolveFieldTypeFromInterface(ifaceSym, name)
			}
			if fieldTyp == "" {
				fieldTyp = "any"
			}

			detail := "variable " + name
			if isConst {
				detail = "constant " + name
			}

			scope.Define(SymbolInfo{
				Name:      name,
				Kind:      SymbolVariable,
				Type:      fieldTyp,
				Detail:    detail,
				Line:      lineNumber,
				Column:    indexColumn(line, name),
				SourceURI: uri,
			})
		}
		return
	}

	match = destructuringArrayRegex.FindStringSubmatch(line)
	if match != nil {
		exprText := strings.TrimSpace(match[2])
		typ := inferExprTypeFromText(scope, exprText)
		typ = normalizeLSPType(scope, typ)

		names := extractDestructuredNames(match[1])
		for _, name := range names {
			if strings.HasPrefix(name, "...") {
				name = name[3:]
			}

			if existing, ok := scope.Resolve(name); ok && existing.Type != "any" && existing.Type != "unknown" {
				continue
			}

			detail := "variable " + name
			if isConst {
				detail = "constant " + name
			}

			scope.Define(SymbolInfo{
				Name:      name,
				Kind:      SymbolVariable,
				Type:      "any",
				Detail:    detail,
				Line:      lineNumber,
				Column:    indexColumn(line, name),
				SourceURI: uri,
			})
		}
	}
}

func extractDestructuredNames(pattern string) []string {
	var names []string
	pattern = strings.TrimSpace(pattern)
	for len(pattern) > 0 {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			break
		}

		if strings.HasPrefix(pattern, "...") {
			rest := pattern[3:]
			rest = strings.TrimSpace(rest)
			commaIdx := strings.Index(rest, ",")
			if commaIdx >= 0 {
				names = append(names, "..."+strings.TrimSpace(rest[:commaIdx]))
				pattern = rest[commaIdx+1:]
			} else {
				names = append(names, "..."+strings.TrimSpace(rest))
				break
			}
			continue
		}

		if strings.HasPrefix(pattern, "{") {
			depth := 1
			i := 1
			for i < len(pattern) && depth > 0 {
				if pattern[i] == '{' {
					depth++
				} else if pattern[i] == '}' {
					depth--
				}
				i++
			}
			inner := strings.TrimSpace(pattern[1 : i-1])
			names = append(names, extractDestructuredNames(inner)...)
			pattern = strings.TrimSpace(pattern[i:])
			if strings.HasPrefix(pattern, ",") {
				pattern = pattern[1:]
			}
			continue
		}

		if strings.HasPrefix(pattern, "[") {
			depth := 1
			i := 1
			for i < len(pattern) && depth > 0 {
				if pattern[i] == '[' {
					depth++
				} else if pattern[i] == ']' {
					depth--
				}
				i++
			}
			inner := strings.TrimSpace(pattern[1 : i-1])
			names = append(names, extractDestructuredNames(inner)...)
			pattern = strings.TrimSpace(pattern[i:])
			if strings.HasPrefix(pattern, ",") {
				pattern = pattern[1:]
			}
			continue
		}

		commaIdx := strings.Index(pattern, ",")
		var token string
		if commaIdx >= 0 {
			token = strings.TrimSpace(pattern[:commaIdx])
			pattern = pattern[commaIdx+1:]
		} else {
			token = strings.TrimSpace(pattern)
			pattern = ""
		}

		if colonIdx := strings.Index(token, ":"); colonIdx >= 0 {
			token = strings.TrimSpace(token[colonIdx+1:])
		}

		eqIdx := strings.Index(token, "=")
		if eqIdx >= 0 {
			token = strings.TrimSpace(token[:eqIdx])
		}

		if token != "" && token != "_" {
			names = append(names, token)
		}
	}
	return names
}

func scanVariableDeclarations(scope *Scope, text string, maxLine int, uri string, pos Position) {
	lines := strings.Split(text, "\n")
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}

	currentFunction := functionBlockAtLine(text, pos.Line)
	functionBlocks := findBlocks(text, "fn")
	for lineIndex := 0; lineIndex <= maxLine; lineIndex++ {
		raw := lines[lineIndex]
		decl, ok := lexerVariableDeclarationOnLine(text, lineIndex)
		if !ok {
			continue
		}

		lineOffset := offsetAtLine(text, lineIndex+1)
		if !declarationVisibleInScope(lineOffset, currentFunction, functionBlocks) {
			continue
		}

		exprEnd := variableInitializerEnd(text, decl.ExprStart)
		if exprEnd < decl.ExprStart {
			continue
		}

		scanVariableDeclaration(scope, text, decl.Name, decl.TypeHint, strings.TrimSpace(text[decl.ExprStart:exprEnd]), lineIndex+1, indexColumn(raw, decl.Name), uri)
	}
}

func declarationVisibleInScope(declarationOffset int, currentFunction *blockInfo, functionBlocks []blockInfo) bool {
	if currentFunction != nil && declarationOffset >= currentFunction.Start && declarationOffset <= currentFunction.End {
		return true
	}

	for _, block := range functionBlocks {
		if declarationOffset >= block.Start && declarationOffset <= block.End {
			return false
		}
	}

	return true
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
		if (typ == "" || typ == "any" || typ == "unknown") && isSimpleIdentifier(exprText) {
			if narrowed := narrowedTypeFromEnclosingIf(scope, sourceText, lineNumber, exprText); narrowed != "" {
				typ = narrowed
			}
		}
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
	call, ok := lexerLeadingCallFromExpr(expr)
	if !ok || !call.IsMember {
		return "", false
	}

	receiver := call.Receiver
	member := call.Method
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

func narrowedTypeFromEnclosingIf(scope *Scope, text string, lineNumber int, name string) string {
	if strings.TrimSpace(text) == "" || name == "" {
		return ""
	}

	lineIndex := lineNumber - 1
	if lineIndex < 0 {
		lineIndex = 0
	}
	ifLine, isInElse, ok := findEnclosingIfAndElse(text, Position{Line: lineIndex, Character: 0})
	if !ok {
		return ""
	}
	if isInElse {
		return ""
	}

	if match := typeOfRegex.FindStringSubmatch(ifLine); match != nil && match[1] == name {
		return match[2]
	}
	if match := typeOfNotRegex.FindStringSubmatch(ifLine); match != nil && match[1] == name {
		return ""
	}
	return ""
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

		fakeLine := "let " + line
		if !strings.Contains(fakeLine, "=") {
			fakeLine = strings.TrimSuffix(fakeLine, ";") + " = undefined"
		}

		decl, ok := parseVariableLineWithLexer(fakeLine)
		if !ok {
			continue
		}

		name := strings.TrimSuffix(decl.Name, "?")
		isNullable := strings.HasSuffix(decl.Name, "?") || strings.Contains(line, decl.Name+"?")
		typeHint := decl.TypeHint
		expr := decl.ExprText

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

func blockInsideAny(offset int, blocks []blockInfo) bool {
	for _, block := range blocks {
		if offset >= block.Start && offset <= block.End {
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
	decl, ok := parseVariableLineWithLexer(line)
	if !ok {
		return ""
	}
	return decl.Name
}

func findBlocks(text string, kind string) []blockInfo {
	cacheKey := lspTextCacheKey(kind, text)
	if cached, ok := lspBlockCache[cacheKey]; ok {
		return cached.blocks
	}

	blocks := getCachedBlocks("", text)
	if blocks == nil {
		_, _ = parseTinyForLSP("", text)
		blocks = getCachedBlocks("", text)
	}

	var result []blockInfo
	for _, b := range blocks {
		if b.Kind == kind {
			result = append(result, b)
		}
	}
	if len(result) == 0 {
		result = findBlocksTextFallback(text, kind)
	}

	lspBlockCache[cacheKey] = lspBlockCacheEntry{blocks: result}
	return result
}

func findBlocksTextFallback(text string, kind string) (blocks []blockInfo) {
	defer func() {
		if recover() != nil {
			blocks = nil
		}
	}()

	result := []blockInfo{}
	lexer := NewLexer(text, "")
	lexer.EnableASI = false
	for {
		tok := lexer.NextToken()
		if tok.Type == TOKEN_EOF {
			break
		}
		if !tokenMatchesBlockKind(tok, kind) {
			continue
		}

		offset := offsetFromLineCol(text, tok.Line, tok.Column)
		block, ok := parseFunctionLikeBlockAt(text, offset, kind)
		if ok {
			result = append(result, block)
		}
	}
	return result
}

func tokenMatchesBlockKind(tok Token, kind string) bool {
	switch kind {
	case "fn":
		return tok.Type == TOKEN_FN
	case "class":
		return tok.Type == TOKEN_CLASS
	case "interface":
		return tok.Type == TOKEN_INTERFACE
	case "enum":
		return tok.Type == TOKEN_ENUM
	case "namespace":
		return tok.Type == TOKEN_IDENT && tok.Literal == "namespace"
	default:
		return false
	}
}

func parseFunctionLikeBlockAt(text string, start int, kind string) (blockInfo, bool) {
	isAsync := false
	if kind == "fn" {
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
		if name == "" || (i < len(text) && text[i] == '(') {
			if name == "" {
				name = ""
			}
		}
	}

	i = skipSpaces(text, i)

	var typeParams []string
	if i < len(text) && text[i] == ':' {
		for i < len(text) && text[i] == ':' {
			i++
			i = skipSpaces(text, i)
			tpStart := i
			for i < len(text) && isIdentByte(text[i]) {
				i++
			}
			if i > tpStart {
				typeParams = append(typeParams, text[tpStart:i])
			}
			i = skipSpaces(text, i)
		}
	}

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

	if kind == "class" {
		i = skipSpaces(text, i)
		if i+10 <= len(text) && text[i:i+10] == "implements" && !isIdentByte(text[i+10]) {
			i += 10
			i = skipSpaces(text, i)
			for i < len(text) && text[i] != '{' {
				ch := text[i]
				if isIdentByte(ch) || ch == '.' || ch == ':' || ch == ',' || ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
					i++
				} else {
					break
				}
			}
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
		Kind:           kind,
		Name:           name,
		ParamsText:     paramsText,
		ReturnType:     returnType,
		Body:           text[i+1 : closeBrace],
		Start:          start,
		End:            closeBrace + 1,
		Line:           line,
		Column:         column,
		IsAsync:        isAsync,
		TypeParameters: typeParams,
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

	bodyScope := cloneScope(scope)
	scanBodyDeclarationsForReturnInference(bodyScope, body)

	matches := returnRegex.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		return "null"
	}

	for _, match := range matches {
		expr := strings.TrimSpace(match[1])
		if expr == "" {
			continue
		}

		typ := inferExprTypeFromText(bodyScope, expr)
		if typ != "unknown" && typ != "any" {
			return typ
		}
	}

	expr := strings.TrimSpace(matches[0][1])
	return inferExprTypeFromText(bodyScope, expr)
}

func scanBodyDeclarationsForReturnInference(scope *Scope, body string) {
	if strings.TrimSpace(body) == "" {
		return
	}

	lines := strings.Split(body, "\n")
	for i, raw := range lines {
		line := cleanLine(raw)
		if line == "" {
			continue
		}

		if idx := strings.Index(line, "return "); idx >= 0 {
			break
		}

		lineOffset := offsetAtLine(body, i+1)
		if isOffsetInStringOrComment(body, lineOffset) {
			continue
		}

		scanVariableLine(scope, body, line, i+1, "")
		scanDestructuringLine(scope, line, i+1, "")
		scanEmbedLine(scope, line, i+1, "")
	}
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
			return "array:any"
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
			nestedScope := NewScope(scope)
			params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
			for _, p := range params {
				nestedScope.Define(SymbolInfo{
					Name: p.Name,
					Kind: SymbolVariable,
					Type: p.Type,
				})
			}
			return "task:" + inferReturnTypeFromBody(nestedScope, block.Body, block.ReturnType)
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

type lexerLeadingCall struct {
	Name       string
	Receiver   string
	Method     string
	OpenOffset int
	IsMember   bool
}

func lexerLeadingCallFromExpr(expr string) (lexerLeadingCall, bool) {
	defer func() {
		_ = recover()
	}()
	tokens := lexedTokensForText(expr)
	i := 0
	for i < len(tokens) && tokens[i].Token.Type == TOKEN_SEMI {
		i++
	}
	if i >= len(tokens) || tokens[i].Token.Type != TOKEN_IDENT {
		return lexerLeadingCall{}, false
	}

	parts := []string{tokens[i].Token.Literal}
	j := i + 1
	for j+1 < len(tokens) && tokens[j].Token.Type == TOKEN_DOT && tokens[j+1].Token.Type == TOKEN_IDENT {
		parts = append(parts, tokens[j+1].Token.Literal)
		j += 2
	}
	if j >= len(tokens) || tokens[j].Token.Type != TOKEN_LPAREN {
		return lexerLeadingCall{}, false
	}
	openOffset := tokens[j].Offset
	if !leadingCallConsumesExpr(expr, openOffset) {
		return lexerLeadingCall{}, false
	}
	if len(parts) == 1 {
		return lexerLeadingCall{Name: parts[0], OpenOffset: openOffset}, true
	}
	return lexerLeadingCall{
		Name:       strings.Join(parts, "."),
		Receiver:   strings.Join(parts[:len(parts)-1], "."),
		Method:     parts[len(parts)-1],
		OpenOffset: openOffset,
		IsMember:   true,
	}, true
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
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return false
	}

	parenDepth := 0
	bracketDepth := 0
	braceDepth := 0
	inString := byte(0)
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < len(expr); i++ {
		ch := expr[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < len(expr) && expr[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if inString != '`' && ch == '\\' {
				escaped = true
				continue
			}
			if ch == inString {
				inString = 0
			}
			continue
		}

		if ch == '/' && i+1 < len(expr) {
			if expr[i+1] == '/' {
				inLineComment = true
				i++
				continue
			}
			if expr[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
		}

		switch ch {
		case '"', '\'', '`':
			inString = ch
			continue
		case '(':
			parenDepth++
			continue
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
			continue
		case '[':
			bracketDepth++
			continue
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
			continue
		case '{':
			braceDepth++
			continue
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
			continue
		}

		if parenDepth != 0 || bracketDepth != 0 || braceDepth != 0 {
			continue
		}

		if i+1 < len(expr) {
			two := expr[i : i+2]
			if two == "==" || two == "!=" || two == "<=" || two == ">=" {
				return true
			}
		}

		if ch == '<' || ch == '>' {
			return true
		}

		if isTopLevelWordOperatorAt(expr, i, "instanceof") ||
			isTopLevelWordOperatorAt(expr, i, "in") ||
			isTopLevelWordOperatorAt(expr, i, "and") ||
			isTopLevelWordOperatorAt(expr, i, "or") {
			return true
		}
	}

	return false
}

func isTopLevelWordOperatorAt(text string, index int, word string) bool {
	end := index + len(word)
	if index < 0 || end > len(text) {
		return false
	}

	if text[index:end] != word {
		return false
	}

	if index > 0 && isIdentByte(text[index-1]) {
		return false
	}

	if end < len(text) && isIdentByte(text[end]) {
		return false
	}

	return true
}

func inferMemberCallTypeFromText(scope *Scope, expr string) string {
	call, ok := lexerLeadingCallFromExpr(expr)
	if !ok || !call.IsMember {
		return ""
	}

	if parsedType := inferParsedExprTypeFromText(scope, expr); parsedType != "" {
		return parsedType
	}

	receiver := call.Receiver
	method := call.Method

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
		if !ok || isPrivateImportMember(member) {
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
			return qualifyNamespaceType(receiver, firstNonEmpty(ret, "any"), sym.Members)
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
				if isPrivateImportMember(memberSym) {
					return "", false
				}
				ret := firstNonEmpty(memberSym.Returns, "any")
				return qualifyNamespaceType(namespace, ret, ns.Members), true
			}
		}
	}

	return "", false
}

func isPrivateImportMember(sym SymbolInfo) bool {
	return strings.HasPrefix(sym.Detail, "private ")
}

func qualifyNamespaceType(namespace string, typ string, members map[string]SymbolInfo) string {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return typ
	}

	if strings.Contains(typ, "|") {
		parts := splitUnionType(typ)
		out := []string{}

		for _, part := range parts {
			out = append(out, qualifyNamespaceType(namespace, strings.TrimSpace(part), members))
		}

		return strings.Join(out, " | ")
	}

	if strings.HasPrefix(typ, "task:") {
		inner := strings.TrimPrefix(typ, "task:")
		return "task:" + qualifyNamespaceType(namespace, inner, members)
	}

	if strings.HasPrefix(typ, "array:") {
		inner := strings.TrimPrefix(typ, "array:")
		return "array:" + qualifyNamespaceType(namespace, inner, members)
	}

	if strings.HasPrefix(typ, "class:") {
		name := strings.TrimPrefix(typ, "class:")
		if name == "" || strings.Contains(name, ".") {
			return typ
		}

		member, ok := members[name]
		if ok && member.Kind == SymbolClass {
			return "class:" + namespace + "." + name
		}

		return typ
	}

	if strings.HasPrefix(typ, "interface:") {
		name := strings.TrimPrefix(typ, "interface:")
		if name == "" || strings.Contains(name, ".") {
			return typ
		}

		member, ok := members[name]
		if ok && member.Kind == SymbolInterface {
			return "interface:" + namespace + "." + name
		}

		return typ
	}

	if strings.HasPrefix(typ, "enum:") {
		name := strings.TrimPrefix(typ, "enum:")
		if name == "" || strings.Contains(name, ".") {
			return typ
		}

		member, ok := members[name]
		if ok && member.Kind == SymbolEnum {
			return "enum:" + namespace + "." + name
		}

		return typ
	}

	if strings.Contains(typ, ".") {
		return typ
	}

	if member, ok := members[typ]; ok {
		switch member.Kind {
		case SymbolClass:
			return "class:" + namespace + "." + typ
		case SymbolInterface:
			return "interface:" + namespace + "." + typ
		case SymbolEnum:
			return "enum:" + namespace + "." + typ
		}
	}

	return typ
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

		if methodSym, ok := classSym.Methods[method]; ok {
			return firstNonEmpty(methodSym.Returns, "any")
		}
		if fieldSym, ok := classSym.Fields[method]; ok {
			return firstNonEmpty(fieldSym.Type, "any")
		}

		return ""
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

	if isGlobalPropertyMethod(method) {
		return globalPropertyMethodReturnType(method)
	}

	return ""
}

func inferNormalCallTypeFromText(scope *Scope, expr string) string {
	call, ok := lexerLeadingCallFromExpr(expr)
	if !ok || call.IsMember {
		return ""
	}

	name := call.Name

	sym, ok := scope.Resolve(name)
	if !ok {
		return ""
	}

	if sym.Kind == SymbolClass {
		if parsedType := inferParsedExprTypeFromText(scope, expr); parsedType != "" {
			return parsedType
		}
		return "class:" + sym.Name
	}

	if sym.Kind == SymbolFunction {
		if parsedType := inferParsedExprTypeFromText(scope, expr); parsedType != "" {
			return parsedType
		}
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
			typ = "array:" + typ
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
		if visited[cacheKey] {
			return exports
		}
		visited[cacheKey] = true
		defer delete(visited, cacheKey)

		statements, _ := parseTinyForLSP(uri, text)
		if statements == nil {
			return exports
		}

		scope := NewScope(nil)
		for alias, module := range parseStdImports(text) {
			resolvedPath := "std:" + module
			memberExports := map[string]SymbolInfo{}
			if resolvedPath != path {
				memberExports = loadTinyFileExports(resolvedPath, visited)
			}

			scope.Define(SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				Detail:    "std module " + module,
				Members:   memberExports,
				SourceURI: pathToFileURI(resolvedPath),
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
					Name:           s.Name,
					Kind:           SymbolInterface,
					Type:           "interface:" + s.Name,
					Detail:         detail,
					Line:           s.Line,
					Column:         s.Column,
					SourceURI:      uri,
					Fields:         map[string]SymbolInfo{},
					Doc:            findDocumentationComments(text, s.Line-1),
					TypeParameters: s.TypeParameters,
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
					Name:           s.Name,
					Kind:           SymbolFunction,
					Type:           "function",
					Detail:         detail,
					Line:           s.Line,
					Column:         s.Column,
					SourceURI:      uri,
					Params:         stdArgsFromParams(scope, s.Params),
					Returns:        returnTypeNameScoped(scope, s.ReturnType),
					Doc:            findDocumentationComments(text, s.Line-1),
					TypeParameters: s.TypeParameters,
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case ExternalFnStmt:
				sym := externalFunctionSymbolFromStmt(scope, s, uri)
				if !exported {
					sym.Detail = "private " + sym.Detail
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case ExternalGlobalStmt:
				sym := externalGlobalSymbolFromStmt(scope, s, uri)
				if !exported {
					sym.Detail = "private " + sym.Detail
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case ClassStmt:
				sym := classSymbolFromStmt(scope, s, uri, text)
				if !exported {
					sym.Detail = "private " + sym.Detail
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case EnumStmt:
				sym := enumSymbolFromStmt(s, uri, text)
				if !exported {
					sym.Detail = "private " + sym.Detail
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

	for _, sym := range exports {
		scope.Define(sym)
	}

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
	statements, _ := parseTinyForLSP(uri, text)
	if statements != nil {
		analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: scope, scope: scope}
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
					Name:           s.Name,
					Kind:           SymbolInterface,
					Type:           "interface:" + s.Name,
					Detail:         detail,
					Line:           s.Line,
					Column:         s.Column,
					SourceURI:      uri,
					Fields:         map[string]SymbolInfo{},
					Doc:            findDocumentationComments(text, s.Line-1),
					TypeParameters: s.TypeParameters,
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
				sym := enumSymbolFromStmt(s, uri, text)
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

			case ExternalFnStmt:
				sym := externalFunctionSymbolFromStmt(scope, s, uri)
				if !exported {
					sym.Detail = "private " + sym.Detail
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case ExternalGlobalStmt:
				sym := externalGlobalSymbolFromStmt(scope, s, uri)
				if !exported {
					sym.Detail = "private " + sym.Detail
				}
				exports[s.Name] = sym
				scope.Define(sym)

			case EmbedStmt:
				if !exported {
					continue
				}
				typ := s.TypeHint.Name
				if typ == "" {
					typ = "string"
				}
				detail := "variable " + s.Name
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

			case NamespaceStmt:
				sym := namespaceSymbolFromStmt(analyzer, s)
				exports[s.Name] = sym
				scope.Define(sym)

			case VariableStmt:
				if !exported {
					continue
				}
				typ := "unknown"
				fields := map[string]SymbolInfo(nil)
				if !s.TypeHint.IsEmpty() {
					typ = normalizeLSPType(scope, s.TypeHint.Name)
				} else {
					typ = analyzer.inferExprType(s.Value)

					if typ == "object" {
						fields = inferObjectFieldsFromText(scope, "", uri, s.Line)
					}
				}

				detail := "variable " + s.Name

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

			case DestructureStmt:
				if !exported {
					continue
				}
				typ := analyzer.inferExprType(s.Value)
				typ = normalizeLSPType(scope, typ)
				fields := map[string]SymbolInfo(nil)
				if typ == "object" {
					fields = inferObjectFieldsFromText(scope, "", uri, s.Line)
				}

				ifaceSym, hasIface := resolveInterfaceFieldsForDestructure(scope, typ)

				fieldInfos := collectDestructuredFieldInfo(s.Target, s.Line, s.Column)
				for _, info := range fieldInfos {
					if info.IsSpread {
						detail := "variable " + info.VarName
						if s.Constant {
							detail = "constant " + info.VarName
						}
						sym := SymbolInfo{Name: info.VarName, Kind: SymbolVariable, Type: "any", Detail: detail, Line: s.Line, Column: s.Column, SourceURI: uri}
						exports[info.VarName] = sym
						scope.Define(sym)
						continue
					}

					fieldTyp := ""
					if fields != nil {
						if sym, ok := fields[info.Key]; ok {
							fieldTyp = sym.Type
						}
					}
					if fieldTyp == "" && hasIface {
						fieldTyp = resolveFieldTypeFromInterface(ifaceSym, info.Key)
					}
					if fieldTyp == "" {
						fieldTyp = "any"
					}

					detail := "variable " + info.VarName
					if s.Constant {
						detail = "constant " + info.VarName
					}

					sym := SymbolInfo{
						Name:      info.VarName,
						Kind:      SymbolVariable,
						Type:      fieldTyp,
						Detail:    detail,
						Line:      s.Line,
						Column:    s.Column,
						SourceURI: uri,
					}
					exports[info.VarName] = sym
					scope.Define(sym)
				}
			}
		}
	}

	isExported := func(block blockInfo) bool {
		lines := strings.Split(text, "\n")
		if block.Line-1 >= 0 && block.Line-1 < len(lines) {
			return strings.Contains(lines[block.Line-1], "export")
		}
		return false
	}

	fallbackExports := map[string]bool{}
	for _, block := range findBlocks(text, "class") {
		if _, exists := exports[block.Name]; !exists && isExported(block) {
			fallbackExports[block.Name] = true
			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolClass,
				Type:           "class:" + block.Name,
				Detail:         "class " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Fields:         map[string]SymbolInfo{},
				Methods:        map[string]SymbolInfo{},
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			exports[block.Name] = sym
			scope.Define(sym)
		}
	}
	for _, block := range findBlocks(text, "interface") {
		if _, exists := exports[block.Name]; !exists && isExported(block) {
			fallbackExports[block.Name] = true
			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolInterface,
				Type:           "interface:" + block.Name,
				Detail:         "interface " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Fields:         map[string]SymbolInfo{},
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			exports[block.Name] = sym
			scope.Define(sym)
		}
	}
	for _, block := range findBlocks(text, "enum") {
		if _, exists := exports[block.Name]; !exists && isExported(block) {
			fallbackExports[block.Name] = true
			sym := SymbolInfo{
				Name:      block.Name,
				Kind:      SymbolEnum,
				Type:      "enum:" + block.Name,
				Detail:    "enum " + block.Name,
				Line:      block.Line,
				Column:    block.Column,
				SourceURI: uri,
				Members:   map[string]SymbolInfo{},
				Doc:       findDocumentationComments(text, block.Line-1),
			}
			exports[block.Name] = sym
			scope.Define(sym)
		}
	}
	for _, block := range findBlocks(text, "namespace") {
		if _, exists := exports[block.Name]; !exists && isExported(block) {
			fallbackExports[block.Name] = true
			sym := SymbolInfo{
				Name:      block.Name,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + block.Name,
				Detail:    "namespace " + block.Name,
				Line:      block.Line,
				Column:    block.Column,
				SourceURI: uri,
				Members:   map[string]SymbolInfo{},
			}
			exports[block.Name] = sym
			scope.Define(sym)
		}
	}

	for _, block := range findBlocks(text, "namespace") {
		if fallbackExports[block.Name] {
			if sym, exists := exports[block.Name]; exists && sym.Kind == SymbolNamespace {
				members := map[string]SymbolInfo{}
				for _, cb := range findBlocks(text, "class") {
					if cb.Start > block.Start && cb.Start < block.End {
						members[cb.Name] = SymbolInfo{
							Name:      cb.Name,
							Kind:      SymbolClass,
							Type:      "class:" + block.Name + "." + cb.Name,
							Detail:    "class " + cb.Name,
							Line:      cb.Line,
							Column:    cb.Column,
							SourceURI: uri,
							Fields:    map[string]SymbolInfo{},
							Methods:   map[string]SymbolInfo{},
						}
					}
				}
				for _, f := range findBlocks(text, "fn") {
					if f.Start > block.Start && f.Start < block.End {
						isMethod := false
						for _, cb := range findBlocks(text, "class") {
							if f.Start > cb.Start && f.Start < cb.End {
								isMethod = true
								break
							}
						}
						if isMethod {
							continue
						}

						params := normalizeStdArgs(scope, parseFunctionParams(f.ParamsText))
						nestedScope := NewScope(scope)
						for _, p := range params {
							nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
						}
						returnType := inferReturnTypeFromBody(nestedScope, f.Body, f.ReturnType)
						if f.IsAsync {
							returnType = "task:" + returnType
						}
						members[f.Name] = SymbolInfo{
							Name:      f.Name,
							Kind:      SymbolFunction,
							Type:      "function",
							Detail:    "fn " + f.Name,
							Line:      f.Line,
							Column:    f.Column,
							SourceURI: uri,
							Params:    params,
							Returns:   returnType,
						}
					}
				}
				for _, eb := range findBlocks(text, "enum") {
					if eb.Start > block.Start && eb.Start < block.End {
						members[eb.Name] = SymbolInfo{
							Name:      eb.Name,
							Kind:      SymbolEnum,
							Type:      "enum:" + block.Name + "." + eb.Name,
							Detail:    "enum " + eb.Name,
							Line:      eb.Line,
							Column:    eb.Column,
							SourceURI: uri,
							Members:   map[string]SymbolInfo{},
						}
					}
				}
				sym.Members = members
				exports[block.Name] = sym
				scope.Define(sym)
			}
		}
	}
	for _, block := range findBlocks(text, "class") {
		if fallbackExports[block.Name] {
			if sym, exists := exports[block.Name]; exists && sym.Kind == SymbolClass {
				fields := scanClassFields(scope, block.Body, uri, block.Line)
				methods := map[string]SymbolInfo{}
				collectEmbeddedSymbolsFromBody(scope, block.Body, fields, methods, uri, block.Line)

				for _, b := range findBlocks(text, "fn") {
					if b.Start > block.Start && b.Start < block.End {
						params := normalizeStdArgs(scope, parseFunctionParams(b.ParamsText))
						nestedScope := NewScope(scope)
						for _, p := range params {
							nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
						}
						returnType := inferReturnTypeFromBody(nestedScope, b.Body, b.ReturnType)
						if b.IsAsync {
							returnType = "task:" + returnType
						}
						detail := "method " + block.Name + "." + b.Name
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

				sym.Fields = fields
				sym.Methods = methods
				exports[block.Name] = sym
				scope.Define(sym)
			}
		}
	}

	for _, block := range findBlocks(text, "interface") {
		if fallbackExports[block.Name] {
			if sym, exists := exports[block.Name]; exists && sym.Kind == SymbolInterface {
				fields := scanInterfaceFields(scope, block.Body, uri, block.Line)
				sym.Fields = fields
				exports[block.Name] = sym
				scope.Define(sym)
			}
		}
	}

	for _, block := range findBlocks(text, "enum") {
		if fallbackExports[block.Name] {
			if sym, exists := exports[block.Name]; exists && sym.Kind == SymbolEnum {
				members := map[string]SymbolInfo{}
				memberType := determineEnumMemberTypeFromText(block.Body)

				rawMembers := splitTopLevel(block.Body, ',')
				for _, raw := range rawMembers {
					name := strings.TrimSpace(raw)
					if name == "" {
						continue
					}

					if strings.Contains(name, "=") {
						name = strings.TrimSpace(strings.SplitN(name, "=", 2)[0])
					}
					if strings.Contains(name, "(") {
						name = strings.TrimSpace(strings.SplitN(name, "(", 2)[0])
					}

					members[name] = SymbolInfo{
						Name:      name,
						Kind:      SymbolVariable,
						Type:      memberType,
						Detail:    "enum member " + block.Name + "." + name,
						Line:      block.Line,
						Column:    block.Column,
						SourceURI: uri,
					}
				}
				sym.Members = members
				exports[block.Name] = sym
				scope.Define(sym)
			}
		}
	}

	classBlocks := findBlocks(text, "class")
	for _, block := range findBlocks(text, "fn") {
		isMethod := false
		for _, cb := range classBlocks {
			if block.Start > cb.Start && block.Start < cb.End {
				isMethod = true
				break
			}
		}
		if isMethod {
			continue
		}

		if _, exists := exports[block.Name]; !exists && isExported(block) {
			params := normalizeStdArgs(scope, parseFunctionParams(block.ParamsText))
			nestedScope := NewScope(scope)
			for _, p := range params {
				nestedScope.Define(SymbolInfo{Name: p.Name, Kind: SymbolVariable, Type: p.Type})
			}
			returnType := inferReturnTypeFromBody(nestedScope, block.Body, block.ReturnType)
			if block.IsAsync {
				returnType = "task:" + returnType
			}

			sym := SymbolInfo{
				Name:           block.Name,
				Kind:           SymbolFunction,
				Type:           "function",
				Detail:         "fn " + block.Name,
				Line:           block.Line,
				Column:         block.Column,
				SourceURI:      uri,
				Params:         params,
				Returns:        returnType,
				Doc:            findDocumentationComments(text, block.Line-1),
				TypeParameters: block.TypeParameters,
			}
			exports[block.Name] = sym
			scope.Define(sym)
		}
	}
}

func collectDestructuredNames(pattern DestructurePattern) []string {
	switch p := pattern.(type) {
	case ObjectDestructurePattern:
		var names []string
		for _, field := range p.Fields {
			if field.HasNested {
				names = append(names, collectDestructuredNames(field.Pattern)...)
			} else {
				name := field.Key
				if field.AliasIsRenamed {
					name = field.Alias
				}
				names = append(names, name)
			}
		}
		if p.HasSpread {
			names = append(names, p.Spread)
		}
		return names
	case ArrayDestructurePattern:
		var names []string
		for _, elem := range p.Elements {
			if elem.HasNested {
				names = append(names, collectDestructuredNames(elem.Pattern)...)
			} else {
				names = append(names, elem.Name)
			}
		}
		return names
	}
	return nil
}

type destructuredFieldInfo struct {
	Key      string
	VarName  string
	Line     int
	Column   int
	IsSpread bool
	Nested   *destructuredFieldInfo
}

func collectDestructuredFieldInfo(pattern DestructurePattern, line, column int) []destructuredFieldInfo {
	switch p := pattern.(type) {
	case ObjectDestructurePattern:
		var fields []destructuredFieldInfo
		for _, f := range p.Fields {
			fieldLine := line
			fieldCol := column
			info := destructuredFieldInfo{
				Key:     f.Key,
				VarName: f.Key,
				Line:    fieldLine,
				Column:  fieldCol,
			}
			if f.AliasIsRenamed {
				info.VarName = f.Alias
			}
			if f.HasNested {
				info.Nested = &destructuredFieldInfo{}
				nestedInfos := collectDestructuredFieldInfo(f.Pattern, line, column)
				if len(nestedInfos) > 0 {
					info.Nested = &nestedInfos[0]
				}
			}
			fields = append(fields, info)
		}
		if p.HasSpread {
			fields = append(fields, destructuredFieldInfo{
				Key:      p.Spread,
				VarName:  p.Spread,
				IsSpread: true,
			})
		}
		return fields
	case ArrayDestructurePattern:
		var fields []destructuredFieldInfo
		for _, elem := range p.Elements {
			info := destructuredFieldInfo{
				Key:     elem.Name,
				VarName: elem.Name,
				Line:    line,
				Column:  column,
			}
			if elem.HasNested {
				info.Nested = &destructuredFieldInfo{}
				nestedInfos := collectDestructuredFieldInfo(elem.Pattern, line, column)
				if len(nestedInfos) > 0 {
					info.Nested = &nestedInfos[0]
				}
			}
			fields = append(fields, info)
		}
		return fields
	}
	return nil
}

func resolveInterfaceFieldsForDestructure(scope *Scope, typ string) (SymbolInfo, bool) {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		return resolveInterfaceSymbol(scope, ifaceName)
	}
	if sym, ok := scope.Resolve(typ); ok && sym.Kind == SymbolInterface {
		return sym, true
	}
	return SymbolInfo{}, false
}

func resolveFieldTypeFromInterface(ifaceSym SymbolInfo, key string) string {
	if fieldSym, ok := ifaceSym.Fields[key]; ok {
		return fieldSym.Type
	}
	return ""
}

func classSymbolFromStmt(scope *Scope, cls ClassStmt, uri string, text string) SymbolInfo {
	classScope := NewScope(scope)
	classScope.Define(SymbolInfo{
		Name:           cls.Name,
		Kind:           SymbolClass,
		Type:           "class:" + cls.Name,
		TypeParameters: cls.TypeParameters,
	})

	fields := map[string]SymbolInfo{}
	for _, f := range cls.Fields {
		typ := typeHintName(f.TypeHint, "any")
		if typ == "any" && f.Value != nil {
			analyzer := &astSemanticAnalyzer{uri: uri, text: text, root: classScope, scope: classScope}

			typ = analyzer.inferExprType(f.Value)
		} else {
			typ = normalizeLSPType(classScope, typ)
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
			TypeRef:   parseLSPTypeRef(typ),
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
			Name:           m.Name,
			Kind:           SymbolFunction,
			Type:           "function",
			TypeRef:        LSPTypeRef{Kind: LSPTypeFunction},
			Detail:         detail,
			Line:           m.Line,
			Column:         m.Column,
			SourceURI:      uri,
			Params:         stdArgsFromParams(classScope, m.Params),
			Returns:        returnTypeNameScoped(classScope, m.ReturnType),
			Doc:            findDocumentationComments(text, m.Line-1),
			TypeParameters: m.TypeParameters,
		}
	}
	collectEmbeddedSymbolsFromAST(classScope, cls.Embeds, cls.Methods, fields, methods, uri, cls.Line)

	return SymbolInfo{
		Name:           cls.Name,
		Kind:           SymbolClass,
		Type:           "class:" + cls.Name,
		TypeRef:        LSPTypeRef{Kind: LSPTypeClass, Name: cls.Name},
		Detail:         "class " + cls.Name,
		Line:           cls.Line,
		Column:         cls.Column,
		SourceURI:      uri,
		Fields:         fields,
		Methods:        methods,
		Doc:            findDocumentationComments(text, cls.Line-1),
		TypeParameters: cls.TypeParameters,
	}
}

func enumSymbolFromStmt(enum EnumStmt, uri string, text string) SymbolInfo {
	members := map[string]SymbolInfo{}
	memberType := determineEnumMemberTypeFromStmt(enum)

	for _, member := range enum.Members {
		line, column := enumMemberPositionFromText(text, enum, member.Name)
		members[member.Name] = SymbolInfo{
			Name:      member.Name,
			Kind:      SymbolVariable,
			Type:      memberType,
			TypeRef:   parseLSPTypeRef(memberType),
			Detail:    "enum member " + enum.Name + "." + member.Name,
			Line:      line,
			Column:    column,
			SourceURI: uri,
		}
	}

	return SymbolInfo{
		Name:      enum.Name,
		Kind:      SymbolEnum,
		Type:      "enum:" + enum.Name,
		TypeRef:   LSPTypeRef{Kind: LSPTypeEnum, Name: enum.Name},
		Detail:    "enum " + enum.Name,
		Line:      enum.Line,
		Column:    enum.Column,
		SourceURI: uri,
		Members:   members,
		Doc:       findDocumentationComments(text, enum.Line-1),
	}
}

func enumMemberPositionFromText(text string, enum EnumStmt, memberName string) (int, int) {
	for _, block := range findBlocks(text, "enum") {
		if block.Name != enum.Name || block.Line != enum.Line {
			continue
		}
		bodyStart := block.Start + strings.Index(text[block.Start:], "{") + 1
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(memberName) + `\b`)
		if loc := re.FindStringIndex(block.Body); loc != nil {
			absOffset := bodyStart + loc[0]
			line := lineNumberAtOffset(text, absOffset)
			column := findColumnAtLine(text, memberName, line)
			return line, column
		}
	}
	return enum.Line, enum.Column
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
		case DestructureStmt:
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

func stripNativeGoBlocks(text string) string {
	result := make([]byte, len(text))
	copy(result, text)

	i := 0
	for i < len(result) {
		if result[i] == 'g' && i+2 < len(result) && result[i+1] == 'o' && result[i+2] == ' ' {
			isNativeContext := false
			j := i - 1
			for j >= 0 && (result[j] == ' ' || result[j] == '\t' || result[j] == '\r' || result[j] == '\n') {
				j--
			}
			if j >= 11 {
				word := string(result[j-11 : j+1])
				if word == "native fn " || word == "native fn\t" || word == "native fn\r" || word == "native fn\n" {
					isNativeContext = true
				}
			}
			if !isNativeContext {
				for k := j; k >= 0; k-- {
					if result[k] == '\n' || k == 0 {
						lineStart := k
						if k == 0 {
							lineStart = 0
						} else {
							lineStart = k + 1
						}
						lineText := string(result[lineStart : j+1])
						if strings.Contains(lineText, "native fn") {
							isNativeContext = true
							break
						}
						break
					}
				}
			}

			if isNativeContext {
				goStart := i
				for i < len(result) && result[i] != '{' {
					i++
				}
				if i < len(result) {
					braceCount := 0
					for i < len(result) {
						if result[i] == '{' {
							braceCount++
						} else if result[i] == '}' {
							braceCount--
							if braceCount == 0 {
								for k := goStart; k <= i; k++ {
									result[k] = ' '
								}
								i++
								break
							}
						}
						i++
					}
				}
				continue
			}
		}
		i++
	}

	return string(result)
}

func scanFileImportsIntoScope(scope *Scope, currentURI string, text string) {
	scanFileImportsIntoScopeWithVisited(scope, currentURI, text, map[string]bool{})
}

func scanFileImportsIntoScopeWithVisited(scope *Scope, currentURI string, text string, visited map[string]bool) {
	cleanedText := stripNativeGoBlocks(text)
	matches := fileImportRegex.FindAllStringSubmatch(cleanedText, -1)

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

	libraryMatches := libraryImportRegex.FindAllStringSubmatch(cleanedText, -1)
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
		exports := map[string]SymbolInfo{}
		if libraryImportRootExists(importPath, currentURI) {
			exports = loadTinyFileExports(resolved, visited)
		}

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
	memberType := determineEnumMemberTypeFromText(body)

	for _, raw := range splitTopLevel(body, ',') {
		memberName := strings.TrimSpace(raw)
		if memberName == "" {
			continue
		}

		if strings.Contains(memberName, "=") {
			memberName = strings.TrimSpace(strings.SplitN(memberName, "=", 2)[0])
		}
		if strings.Contains(memberName, "(") {
			memberName = strings.TrimSpace(strings.SplitN(memberName, "(", 2)[0])
		}
		if memberName == "" {
			continue
		}

		memberLine := lineNumber
		memberColumn := column
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(memberName) + `\b`)
		loc := re.FindStringIndex(body)
		if loc != nil {
			newlines := 0
			for j := 0; j < loc[0]; j++ {
				if body[j] == '\n' {
					newlines++
				}
			}
			memberLine = lineNumber + newlines

			lineStart := 0
			for j := loc[0] - 1; j >= 0; j-- {
				if body[j] == '\n' {
					lineStart = j + 1
					break
				}
			}
			memberColumn = (loc[0] - lineStart) + 1
		}

		members[memberName] = SymbolInfo{
			Name:      memberName,
			Kind:      SymbolVariable,
			Type:      memberType,
			Detail:    "enum member " + enumName + "." + memberName,
			Line:      memberLine,
			Column:    memberColumn,
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
		if _, exists := exports[block.Name]; exists {
			continue
		}

		fnScope := NewScope(scope)
		for _, tp := range block.TypeParameters {
			fnScope.Define(SymbolInfo{
				Name: tp,
				Kind: SymbolVariable,
				Type: tp,
			})
		}

		params := normalizeStdArgs(fnScope, parseFunctionParams(block.ParamsText))
		returnType := inferReturnTypeFromBody(fnScope, block.Body, block.ReturnType)

		if block.IsAsync {
			returnType = "task:" + returnType
		}

		sym := SymbolInfo{
			Name:           block.Name,
			Kind:           SymbolFunction,
			Type:           "function",
			Detail:         "export fn " + block.Name,
			Line:           block.Line,
			Column:         block.Column,
			SourceURI:      uri,
			Params:         params,
			Returns:        returnType,
			Doc:            findDocumentationComments(text, block.Line-1),
			TypeParameters: block.TypeParameters,
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

		classScope := NewScope(scope)
		for _, tp := range block.TypeParameters {
			classScope.Define(SymbolInfo{
				Name: tp,
				Kind: SymbolVariable,
				Type: tp,
			})
		}

		methods := map[string]SymbolInfo{}
		fields := scanClassFields(classScope, block.Body, uri, block.Line)
		collectEmbeddedSymbolsFromBody(classScope, block.Body, fields, methods, uri, block.Line)

		for _, methodBlock := range findBlocks(block.Body, "fn") {
			methodScope := NewScope(classScope)
			for _, tp := range methodBlock.TypeParameters {
				methodScope.Define(SymbolInfo{
					Name: tp,
					Kind: SymbolVariable,
					Type: tp,
				})
			}

			params := normalizeStdArgs(methodScope, parseFunctionParams(methodBlock.ParamsText))
			for _, p := range params {
				methodScope.Define(SymbolInfo{
					Name: p.Name,
					Kind: SymbolVariable,
					Type: p.Type,
				})
			}

			returnType := inferReturnTypeFromBody(methodScope, methodBlock.Body, methodBlock.ReturnType)

			if methodBlock.IsAsync {
				returnType = "task:" + returnType
			}

			methods[methodBlock.Name] = SymbolInfo{
				Name:           methodBlock.Name,
				Kind:           SymbolFunction,
				Type:           "function",
				Detail:         "method " + block.Name + "." + methodBlock.Name,
				Line:           block.Line + methodBlock.Line - 1,
				Column:         methodBlock.Column,
				SourceURI:      uri,
				Params:         params,
				Returns:        returnType,
				Doc:            findDocumentationComments(text, block.Line+methodBlock.Line-2),
				TypeParameters: methodBlock.TypeParameters,
			}
		}

		sym := SymbolInfo{
			Name:           block.Name,
			Kind:           SymbolClass,
			Type:           "class:" + block.Name,
			Detail:         "export class " + block.Name,
			Line:           block.Line,
			Column:         block.Column,
			SourceURI:      uri,
			Methods:        methods,
			Fields:         fields,
			Doc:            findDocumentationComments(text, block.Line-1),
			TypeParameters: block.TypeParameters,
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

		embed, ok := parseEmbedLineWithLexer(line)
		if !ok {
			continue
		}

		kind := embed.Kind
		name := embed.Name

		typ := "string"
		if kind == "embedbytes" {
			typ = "buffer"
		} else if kind == "embedfolder" {
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

		decl, ok := parseVariableLineWithLexer(line)
		if !ok {
			continue
		}

		name := decl.Name

		if existing, ok := scope.Resolve(name); ok && (existing.Type == "function" || strings.HasPrefix(existing.Type, "task:")) {
			continue
		}

		typeHint := decl.TypeHint
		expr := decl.ExprText

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

		if isIdentChar(ch) || ch == '.' || ch == '?' || ch == ':' {
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

		matches := inlineAnonFnRegex.FindAllStringSubmatchIndex(line, -1)

		for _, matchIndices := range matches {
			lineStartOffset := offsetAtLine(text, lineIndex+1)
			leadingWS := len(lines[lineIndex]) - len(strings.TrimLeft(lines[lineIndex], " \t"))
			fnOffset := lineStartOffset + leadingWS + matchIndices[0]
			openBraceInLine := matchIndices[1] - 1
			openBraceOffset := lineStartOffset + leadingWS + openBraceInLine

			if openBraceOffset >= len(text) || text[openBraceOffset] != '{' {
				openBraceOffset = -1
				matchEndOffset := lineStartOffset + leadingWS + matchIndices[1]
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

			if cursorLine >= startLine && cursorLine <= endLine {
				paramsText := line[matchIndices[2]:matchIndices[3]]
				params := parseFunctionParams(paramsText)
				inferredTypes := expectedInlineFunctionParamTypes(scope, text, bytePositionAtOffset(text, fnOffset), fnOffset)
				if len(inferredTypes) == 0 {
					inferredTypes = expectedInlineFunctionParamTypesFromObject(scope, text, fnOffset)
				}

				for i, param := range params {
					var paramType string

					if param.Variadic {
						paramType = "array"
					} else if param.Type == "any" && i < len(inferredTypes) && strings.TrimSpace(inferredTypes[i]) != "" {
						paramType = normalizeLSPType(scope, inferredTypes[i])
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

func expectedInlineFunctionParamTypes(scope *Scope, text string, pos Position, fnOffset int) []string {
	if fnOffset < 0 || fnOffset > len(text) {
		return nil
	}

	open := findUnclosedCallParen(text[:fnOffset])
	if open < 0 {
		return nil
	}

	callee := extractCalleeBefore(text, open)
	if callee == "" {
		return nil
	}

	argIndex := countTopLevelCommas(text[open+1 : fnOffset])

	baseName, typeArgs := splitCalleeAndTypeArgs(callee)
	params := paramsForCallName(scope, text, pos, baseName)
	if argIndex < 0 || argIndex >= len(params) {
		return nil
	}

	subst := map[string]string{}
	if sym, ok := scope.Resolve(baseName); ok && len(sym.TypeParameters) > 0 {
		if len(typeArgs) > 0 {
			for i, tp := range sym.TypeParameters {
				if i < len(typeArgs) {
					subst[tp] = normalizeLSPType(scope, typeArgs[i])
				}
			}
		} else {
			for _, tp := range sym.TypeParameters {
				subst[tp] = "any"
			}
			argTexts := splitTopLevel(text[open+1:fnOffset], ',')
			for i, argText := range argTexts {
				if i >= argIndex || i >= len(sym.Params) {
					break
				}
				argType := inferTypeFromArgText(scope, strings.TrimSpace(argText))
				if argType == "" || argType == "unknown" {
					continue
				}
				for _, tp := range sym.TypeParameters {
					if res, ok := inferTypeParam(sym.Params[i].Type, argType, tp); ok {
						subst[tp] = res
					}
				}
			}
		}
	}

	expectedType := normalizeLSPType(scope, params[argIndex].Type)
	if len(subst) > 0 {
		expectedType = substituteLSPType(expectedType, subst)
	}

	paramTypes, ok := callableParamTypesFromType(expectedType)
	if !ok {
		return nil
	}

	return paramTypes
}

func expectedInlineFunctionParamTypesFromObject(scope *Scope, text string, fnOffset int) []string {
	i := fnOffset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 || text[i] != ':' {
		return nil
	}
	i-- // skip ':'
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 || !isIdentByte(text[i]) {
		return nil
	}
	endField := i + 1
	for i >= 0 && isIdentByte(text[i]) {
		i--
	}
	fieldName := text[i+1 : endField]

	// Find the enclosing object opening brace '{'
	braceDepth := 0
	objBraceOffset := -1
	for i >= 0 {
		ch := text[i]
		if ch == '}' {
			braceDepth++
		} else if ch == '{' {
			if braceDepth == 0 {
				objBraceOffset = i
				break
			}
			braceDepth--
		}
		i--
	}
	if objBraceOffset == -1 {
		return nil
	}

	typ, ok := findObjectTypeHintAtOffset(text, objBraceOffset, scope)
	if !ok {
		return nil
	}

	typ = strings.TrimSpace(typ)
	var expectedSym SymbolInfo
	var okSym bool

	if strings.HasPrefix(typ, "interface:") {
		ifaceName := strings.TrimPrefix(typ, "interface:")
		expectedSym, okSym = resolveInterfaceSymbol(scope, ifaceName)
	} else if strings.HasPrefix(typ, "class:") {
		className := strings.TrimPrefix(typ, "class:")
		expectedSym, okSym = resolveClassSymbol(scope, className)
	}

	if !okSym {
		return nil
	}

	fieldSym, ok := expectedSym.Fields[fieldName]
	if !ok {
		fieldSym, ok = expectedSym.Methods[fieldName]
	}
	if !ok {
		return nil
	}

	if strings.HasPrefix(fieldSym.Type, "function(") {
		paramTypes, _ := callableFunctionParamTypes(fieldSym.Type)
		return paramTypes
	}

	return nil
}

func inferTypeFromArgText(scope *Scope, text string) string {
	if text == "" {
		return ""
	}
	if (strings.HasPrefix(text, "\"") && strings.HasSuffix(text, "\"")) ||
		(strings.HasPrefix(text, "'") && strings.HasSuffix(text, "'")) ||
		(strings.HasPrefix(text, "`") && strings.HasSuffix(text, "`")) {
		return "string"
	}
	if text == "true" || text == "false" {
		return "boolean"
	}
	if text == "null" {
		return "null"
	}
	if text == "undefined" {
		return "undefined"
	}
	if _, err := strconv.ParseFloat(text, 64); err == nil {
		return "number"
	}
	if strings.HasPrefix(text, "[") {
		return "array:any"
	}
	if strings.HasPrefix(text, "{") {
		return "object:any"
	}
	if sym, ok := scope.Resolve(text); ok {
		return sym.Type
	}
	return ""
}

func splitCalleeAndTypeArgs(callee string) (string, []string) {
	idx := strings.Index(callee, ":")
	if idx < 0 {
		return callee, nil
	}
	baseName := callee[:idx]
	typePart := callee[idx+1:]
	typeArgs := splitTopLevel(typePart, ',')
	return baseName, typeArgs
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

func hoverForMember(scope *Scope, receiver string, receiverType string, member string) (HoverResult, bool) {
	receiverType = unwrapNullableType(receiverType)

	if strings.HasPrefix(receiverType, "std:") {
		module := strings.TrimPrefix(receiverType, "std:")
		info, ok := GetStdModuleInfo(module)
		if !ok {
			return HoverResult{}, false
		}

		methodInfo, ok := info.Methods[member]
		if !ok {
			return HoverResult{}, false
		}

		signature := formatStdSignature(module, methodInfo)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description}}, true
	}

	if strings.HasPrefix(receiverType, "task:") && member == "await" {
		returnType := strings.TrimPrefix(receiverType, "task:")
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\nawait task: " + returnType + "\n```\nWaits for the task and returns its result."}}, true
	}

	if methodInfo, ok := GetNativeMethodInfo(receiverType, member); ok {
		signature := formatNativeSignature(receiverType, methodInfo)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + methodInfo.Description}}, true
	}

	if memberSym, _, ok := resolveMemberFromStaticType(scope, receiverType, member); ok {
		if memberSym.Kind == SymbolFunction {
			namePrefix := receiver
			if strings.HasPrefix(receiverType, "class:") {
				namePrefix = strings.TrimPrefix(receiverType, "class:")
			}
			signature := formatFunctionSignature(namePrefix+"."+memberSym.Name, memberSym.Params, memberSym.Returns)
			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(memberSym.Detail, memberSym.Doc)}}, true
		}
		if memberSym.Kind == SymbolClass {
			constructor := constructorSymbolFromClass(memberSym, receiver+"."+memberSym.Name)
			signature := formatFunctionSignature(constructor.Name, constructor.Params, constructor.Returns)
			return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(constructor.Detail, memberSym.Doc)}}, true
		}
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "**" + receiver + "." + memberSym.Name + "**\n\nType: `" + firstNonEmpty(memberSym.Type, "any") + "`\n\n" + appendDoc(memberSym.Detail, memberSym.Doc)}}, true
	}

	if hover, ok := hoverForGlobalPropertyMethod(receiverType, member); ok {
		return hover, true
	}

	return HoverResult{}, false
}

func getHover(uri string, text string, pos Position) any {
	posOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	if isOffsetInStringOrComment(text, posOffset) {
		return nil
	}

	word := wordAtPosition(text, pos)

	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

	receiver, member, ok := memberExprAtPosition(text, pos)
	if ok {
		_, receiverType, exists := resolveReceiverPath(scope, text, pos, receiver)
		if !exists {
			return nil
		}

		if hover, ok := hoverForMember(scope, receiver, receiverType, member); ok {
			return hover
		}

		return nil
	}

	if hover, ok := hoverForDeclarationMember(scope, text, pos, word); ok {
		return hover
	}

	if hover, ok := hoverForObjectLiteralFieldContext(scope, text, pos, word); ok {
		return hover
	}

	if hover, ok := hoverForObjectLiteralField(scope, text, pos, word); ok {
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

	if enumName := enumNameAtPosition(text, pos); enumName != "" {
		if enumSym, ok := resolveEnumSymbol(scope, enumName); ok && enumSym.Kind == SymbolEnum {
			if memberSym, ok := enumSym.Members[word]; ok && memberSym.Line == line {
				value := "**" + enumName + "." + memberSym.Name + "**\n\nType: `" + firstNonEmpty(memberSym.Type, "any") + "`\n\n" + appendDoc(memberSym.Detail, memberSym.Doc)
				return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: value}}, true
			}
		}
	}

	return HoverResult{}, false
}

func hoverForObjectLiteralFieldContext(scope *Scope, text string, pos Position, word string) (HoverResult, bool) {
	if word == "" {
		return HoverResult{}, false
	}

	if !isHoverOnObjectLiteralKey(text, pos, word) {
		return HoverResult{}, false
	}

	ctx := parseEditorContext(text, pos, scope)
	if !ctx.InsideObject {
		return HoverResult{}, false
	}

	for _, parentSym := range ctx.ObjectInterfaceSyms {
		if hover, ok := hoverForObjectLiteralFieldSymbol(parentSym, word); ok {
			return hover, true
		}
	}

	if ctx.ObjectInterfaceSym.Kind == SymbolInterface || ctx.ObjectInterfaceSym.Kind == SymbolClass {
		if hover, ok := hoverForObjectLiteralFieldSymbol(ctx.ObjectInterfaceSym, word); ok {
			return hover, true
		}
	}

	return HoverResult{}, false
}

func hoverForObjectLiteralField(scope *Scope, text string, pos Position, word string) (HoverResult, bool) {
	if word == "" {
		return HoverResult{}, false
	}

	if !isHoverOnObjectLiteralKey(text, pos, word) {
		return HoverResult{}, false
	}

	cursorOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	objBraceOffset := findObjectLiteralBraceOffset(text, cursorOffset)
	if objBraceOffset == -1 {
		return HoverResult{}, false
	}

	typeName, ok := findObjectTypeHintAtOffset(text, objBraceOffset, scope)
	if !ok {
		return HoverResult{}, false
	}

	for _, parentSym := range resolveUnionInterfaceSymbols(scope, typeName) {
		if hover, ok := hoverForObjectLiteralFieldSymbol(parentSym, word); ok {
			return hover, true
		}
	}

	return HoverResult{}, false
}

func isHoverOnObjectLiteralKey(text string, pos Position, word string) bool {
	line := getLine(text, pos.Line)
	if pos.Character > len(line) {
		pos.Character = len(line)
	}

	start := pos.Character
	for start > 0 && isIdentChar(line[start-1]) {
		start--
	}
	end := pos.Character
	for end < len(line) && isIdentChar(line[end]) {
		end++
	}
	if start == end || line[start:end] != word {
		return false
	}

	i := end
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	return i < len(line) && line[i] == ':'
}

func hoverForObjectLiteralFieldSymbol(parentSym SymbolInfo, word string) (HoverResult, bool) {
	if fieldSym, ok := parentSym.Fields[word]; ok {
		value := "**" + parentSym.Name + "." + fieldSym.Name + "**\n\nType: `" + fieldSym.Type + "`\n\n" + appendDoc(fieldSym.Detail, fieldSym.Doc)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: value}}, true
	}

	if methodSym, ok := parentSym.Methods[word]; ok {
		signature := formatFunctionSignature(parentSym.Name+"."+methodSym.Name, methodSym.Params, methodSym.Returns)
		return HoverResult{Contents: MarkupContent{Kind: "markdown", Value: "```tiny\n" + signature + "\n```\n" + appendDoc(methodSym.Detail, methodSym.Doc)}}, true
	}

	return HoverResult{}, false
}

type astSemanticAnalyzer struct {
	uri                  string
	text                 string
	root                 *Scope
	scope                *Scope
	diagnostics          []map[string]any
	currentClass         string
	currentReturnType    string
	activeTypeParams     []string
	pendingCallbackTypes map[[2]int][]string
	expectedTypeContext  string
}

func semanticDiagnosticsFromAST(uri string, text string) []map[string]any {
	statements, parseDiagnostics := parseTinyForLSP(URIToPath(uri), text)
	recovered := false
	if len(parseDiagnostics) > 0 || statements == nil {
		statements = recoverStatementsForLSP(uri, text, parseDiagnostics)
		if statements == nil {
			return []map[string]any{}
		}
		recovered = true
	}

	diagnostics := CheckProgramSemantics(uri, text, statements, true)
	if recovered {
		return filterDiagnosticsNearParseErrors(diagnostics, parseDiagnostics)
	}
	return diagnostics
}

func CheckProgramSemantics(uri string, text string, statements []Stmt, includeUnused bool) []map[string]any {
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
	seedImportSymbolsFromStatements(root, uri, statements)

	scanFileImportsIntoScope(root, uri, text)

	a := &astSemanticAnalyzer{uri: uri, text: text, root: root, scope: root}
	a.predeclareStatements(statements)
	a.visitStatements(statements)
	if includeUnused {
		a.addUnusedSymbolDiagnostics(text, statements)
	}
	return a.diagnostics
}

func seedImportSymbolsFromStatements(root *Scope, uri string, statements []Stmt) {
	for _, raw := range statements {
		stmt, _ := unwrapExport(raw)
		importStmt, ok := stmt.(ImportStmt)
		if !ok {
			continue
		}

		alias := importStmt.Alias
		if alias == "" {
			if importStmt.Std {
				alias = importStmt.Path
			} else if importStmt.Library {
				alias = defaultLibraryAlias(importStmt.Path)
			} else {
				alias = strings.TrimSuffix(filepath.Base(importStmt.Path), filepath.Ext(importStmt.Path))
			}
		}

		if importStmt.Std {
			resolvedPath := "std:" + importStmt.Path
			root.Define(SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				TypeRef:   LSPTypeRef{Kind: LSPTypeNamespace, Name: alias},
				Detail:    "std module " + importStmt.Path,
				Line:      importStmt.Line,
				Column:    importStmt.Column,
				Members:   loadTinyFileExports(resolvedPath, map[string]bool{}),
				SourceURI: pathToFileURI(resolvedPath),
			})
			continue
		}

		if importStmt.Library {
			resolvedPath := resolveLibraryImportPath(importStmt.Path, uri)
			root.Define(SymbolInfo{
				Name:      alias,
				Kind:      SymbolNamespace,
				Type:      "namespace:" + alias,
				TypeRef:   LSPTypeRef{Kind: LSPTypeNamespace, Name: alias},
				Detail:    "library " + importStmt.Path,
				Line:      importStmt.Line,
				Column:    importStmt.Column,
				Members:   loadLibraryFileExportsForLSP(importStmt.Path, uri, map[string]bool{}),
				SourceURI: pathToFileURI(resolvedPath),
			})
			continue
		}

		if importStmt.Plugin {
			root.Define(SymbolInfo{Name: alias, Kind: SymbolVariable, Type: "any", Detail: "plugin " + importStmt.Path, Line: importStmt.Line, Column: importStmt.Column, SourceURI: uri})
		}
	}
}

func loadLibraryFileExportsForLSP(importPath string, currentURI string, visited map[string]bool) map[string]SymbolInfo {
	if !libraryImportRootExists(importPath, currentURI) {
		return map[string]SymbolInfo{}
	}
	return loadTinyFileExports(resolveLibraryImportPath(importPath, currentURI), visited)
}

func recoverStatementsForLSP(uri string, text string, diagnostics []LSPDiagnostic) []Stmt {
	if len(diagnostics) == 0 {
		return nil
	}

	lines := strings.Split(text, "\n")
	for _, diagnostic := range diagnostics {
		if diagnostic.Line >= 0 && diagnostic.Line < len(lines) {
			lines[diagnostic.Line] = ""
		}
	}

	statements, parseDiagnostics := parseTinyForLSP(URIToPath(uri), strings.Join(lines, "\n"))
	if len(parseDiagnostics) > 0 || statements == nil {
		return nil
	}
	return statements
}

func filterDiagnosticsNearParseErrors(diagnostics []map[string]any, parseDiagnostics []LSPDiagnostic) []map[string]any {
	if len(parseDiagnostics) == 0 || len(diagnostics) == 0 {
		return diagnostics
	}

	nearParseError := func(line int) bool {
		for _, diagnostic := range parseDiagnostics {
			if line >= diagnostic.Line-1 && line <= diagnostic.Line+1 {
				return true
			}
		}
		return false
	}

	filtered := diagnostics[:0]
	for _, diagnostic := range diagnostics {
		rangeValue, ok := diagnostic["range"].(map[string]any)
		if !ok {
			filtered = append(filtered, diagnostic)
			continue
		}
		startValue, ok := rangeValue["start"].(map[string]any)
		if !ok {
			filtered = append(filtered, diagnostic)
			continue
		}
		if !nearParseError(intFromAny(startValue["line"])) {
			filtered = append(filtered, diagnostic)
		}
	}
	return filtered
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

func (a *astSemanticAnalyzer) defineDestructuredNested(scope *Scope, parentKey, parentVarName, parentTyp string, info *destructuredFieldInfo, constant bool, line, column int) {
	if info == nil {
		return
	}

	nestedTyp := ""
	ifaceSym, hasIface := resolveInterfaceFieldsForDestructure(scope, parentTyp)
	if hasIface {
		nestedTyp = resolveFieldTypeFromInterface(ifaceSym, info.Key)
	}

	if nestedTyp == "" {
		nestedTyp = "any"
	}

	detail := "variable " + info.VarName
	if constant {
		detail = "constant " + info.VarName
	}

	a.define(SymbolInfo{Name: info.VarName, Kind: SymbolVariable, Type: nestedTyp, Detail: detail, Line: line, Column: column, SourceURI: a.uri})

	if info.Nested != nil && info.Nested.Key != "" {
		a.defineDestructuredNested(scope, info.Key, info.VarName, nestedTyp, info.Nested, constant, line, column)
	}
}

func (a *astSemanticAnalyzer) resolve(name string) (SymbolInfo, bool) {
	if strings.Contains(name, ".") {
		parts := strings.Split(name, ".")
		sym, ok := a.scope.Resolve(parts[0])
		if !ok {
			return SymbolInfo{}, false
		}
		for _, member := range parts[1:] {
			if sym.Kind == SymbolNamespace {
				if m, ok := sym.Members[member]; ok {
					sym = m
				} else {
					return SymbolInfo{}, false
				}
			} else if sym.Kind == SymbolClass {
				if m, ok := sym.Methods[member]; ok {
					sym = m
				} else if f, ok := sym.Fields[member]; ok {
					sym = f
				} else {
					return SymbolInfo{}, false
				}
			} else {
				return SymbolInfo{}, false
			}
		}
		return sym, true
	}
	return a.scope.Resolve(name)
}

func (a *astSemanticAnalyzer) addDiagnostic(line int, column int, message string) {
	name := extractNameFromMessage(message)

	if line > 0 && column > 0 {
		if name != "" {
			lineText := getLine(a.text, line-1)
			code := stripLineComment(lineText)
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if match := re.FindStringIndex(code); match != nil {
				column = match[0] + 1
			}
		}
	} else if name != "" {
		line, column = findWordFirstOccurrence(a.text, name)
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

func (a *astSemanticAnalyzer) addDiagnosticAtRange(r byteIdentifierRange, message string) {
	a.diagnostics = append(a.diagnostics, makeRangeDiagnosticFromByteRange(r, 2, message))
}

func (a *astSemanticAnalyzer) addStatementDiagnostic(stmt Stmt, message string) {
	line, column := nodePosition(stmt)
	if line <= 0 || column <= 0 {
		return
	}

	lineIndex := line - 1
	lineText := getLine(a.text, lineIndex)
	code := stripLineComment(lineText)
	start := 0
	for start < len(code) && (code[start] == ' ' || code[start] == '\t') {
		start++
	}
	end := len(code)
	for end > start && (code[end-1] == ' ' || code[end-1] == '\t') {
		end--
	}
	if end <= start {
		colIndex := column - 1
		wordLen := wordLengthAtColumn(lineText, colIndex)
		start = colIndex
		end = colIndex + wordLen
	}

	a.diagnostics = append(a.diagnostics, makeRangeDiagnostic(
		lineIndex,
		start,
		end,
		2,
		message,
	))
}

func (a *astSemanticAnalyzer) addErrorAtRange(r byteIdentifierRange, message string) {
	a.diagnostics = append(a.diagnostics, makeRangeDiagnosticFromByteRange(r, 1, message))
}

func (a *astSemanticAnalyzer) addError(line int, column int, message string) {
	name := extractNameFromMessage(message)

	if line > 0 && column > 0 {
		if name != "" {
			lineText := getLine(a.text, line-1)
			code := stripLineComment(lineText)
			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
			if match := re.FindStringIndex(code); match != nil {
				column = match[0] + 1
			}
		}
	} else if name != "" {
		line, column = findWordFirstOccurrence(a.text, name)
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

		case DestructureStmt:
			if !isExported {
				for _, name := range collectDestructuredNames(s.Target) {
					decls = append(decls, unusedSymbolDecl{name: name, kind: "variable", line: s.Line, col: s.Column})
				}
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

		typ = normalizeLSPType(scope, typ)
		if p.Variadic {
			typ = "array:" + typ
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

		case ExternalFnStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(externalFunctionSymbolFromStmt(a.root, s, a.uri))

		case ExternalGlobalStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(externalGlobalSymbolFromStmt(a.root, s, a.uri))

		case FunctionStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{
				Name: s.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri,
				Params: stdArgsFromParams(a.scope, s.Params), Returns: returnTypeNameScoped(a.root, s.ReturnType),
				TypeParameters: s.TypeParameters,
			})

		case ClassStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(a.classSymbol(s))

		case VariableStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

		case DestructureStmt:
			for _, name := range collectDestructuredNames(s.Target) {
				a.checkNamingConflict(name, s.Line, s.Column)
				a.root.Define(SymbolInfo{Name: name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + name, Line: s.Line, Column: s.Column, SourceURI: a.uri})
			}

		case EnumStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(enumSymbolFromStmt(s, a.uri, a.text))

		case EmbedStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: s.TypeHint.Name, Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

		case InterfaceStmt:
			a.checkNamingConflict(s.Name, s.Line, s.Column)
			sym := SymbolInfo{
				Name:           s.Name,
				Kind:           SymbolInterface,
				Type:           "interface:" + s.Name,
				Detail:         "interface " + s.Name,
				Line:           s.Line,
				Column:         s.Column,
				SourceURI:      a.uri,
				Fields:         map[string]SymbolInfo{},
				TypeParameters: s.TypeParameters,
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
				case NamespaceStmt:
					members[m.Name] = namespaceSymbolFromStmt(a, m)
				case FunctionStmt:
					members[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + m.Name, Line: m.Line, Column: m.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, m.Params), Returns: returnTypeNameScoped(a.root, m.ReturnType)}
				case VariableStmt:
					members[m.Name] = SymbolInfo{Name: m.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + m.Name, Line: m.Line, Column: m.Column, SourceURI: a.uri}
				case DestructureStmt:
					for _, name := range collectDestructuredNames(m.Target) {
						members[name] = SymbolInfo{Name: name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + name, Line: m.Line, Column: m.Column, SourceURI: a.uri}
					}
				case ClassStmt:
					members[m.Name] = a.classSymbol(m)
				case EnumStmt:
					enumSym := enumSymbolFromStmt(m, a.uri, a.text)
					enumSym.Type = "enum:" + s.Name + "." + m.Name
					enumSym.Detail = "enum " + m.Name
					members[m.Name] = enumSym
				}
			}
			a.root.Define(SymbolInfo{Name: s.Name, Kind: SymbolNamespace, Type: "namespace", Detail: "namespace " + s.Name, Line: 1, Column: 1, SourceURI: a.uri, Members: members})
		}
	}
}

func namespaceSymbolFromStmt(a *astSemanticAnalyzer, ns NamespaceStmt) SymbolInfo {
	members := map[string]SymbolInfo{}
	for _, raw := range ns.Statements {
		inner, _ := unwrapExport(raw)
		switch s := inner.(type) {
		case NamespaceStmt:
			members[s.Name] = namespaceSymbolFromStmt(a, s)
		case FunctionStmt:
			members[s.Name] = SymbolInfo{Name: s.Name, Kind: SymbolFunction, Type: "function", Detail: "fn " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Params: stdArgsFromParams(a.scope, s.Params), Returns: returnTypeNameScoped(a.root, s.ReturnType)}
		case VariableStmt:
			members[s.Name] = SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: "unknown", Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri}
		case ClassStmt:
			members[s.Name] = a.classSymbol(s)
		case EnumStmt:
			enumSym := enumSymbolFromStmt(s, a.uri, a.text)
			enumSym.Type = "enum:" + ns.Name + "." + s.Name
			members[s.Name] = enumSym
		case InterfaceStmt:
			members[s.Name] = interfaceSymbolFromStmt(a.root, s, a.uri, a.text)
		}
	}
	return SymbolInfo{Name: ns.Name, Kind: SymbolNamespace, Type: "namespace:" + ns.Name, TypeRef: LSPTypeRef{Kind: LSPTypeNamespace, Name: ns.Name}, Detail: "namespace " + ns.Name, Line: ns.Line, Column: ns.Column, SourceURI: a.uri, Members: members}
}

func (a *astSemanticAnalyzer) classSymbol(cls ClassStmt) SymbolInfo {
	fields := map[string]SymbolInfo{}
	for _, f := range cls.Fields {
		typ := typeHintName(f.TypeHint, "any")
		if typ == "any" && f.Value != nil {
			typ = a.inferExprType(f.Value)
		} else {
			typ = normalizeLSPType(a.scope, typ)
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
		methods[m.Name] = SymbolInfo{
			Name: m.Name, Kind: SymbolFunction, Type: "function", Detail: detail, Line: m.Line, Column: m.Column, SourceURI: a.uri,
			Params: stdArgsFromParams(a.scope, m.Params), Returns: returnTypeNameScoped(a.scope, m.ReturnType),
			TypeParameters: m.TypeParameters,
		}
	}
	collectEmbeddedSymbolsFromAST(a.root, cls.Embeds, cls.Methods, fields, methods, a.uri, cls.Line)

	resolvedImplements := []string{}
	for _, imp := range cls.Implements {
		resolvedImplements = append(resolvedImplements, normalizeLSPType(a.scope, imp))
	}

	return SymbolInfo{
		Name: cls.Name, Kind: SymbolClass, Type: "class:" + cls.Name, Detail: "class " + cls.Name, Line: cls.Line, Column: cls.Column, SourceURI: a.uri, Fields: fields, Methods: methods,
		TypeParameters: cls.TypeParameters,
		Implements:     resolvedImplements,
	}
}

func (a *astSemanticAnalyzer) visitStatements(stmts []Stmt) {
	unreachable := false
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		if unreachable {
			line, col := nodePosition(stmt)
			if line > 0 && col > 0 {
				a.addStatementDiagnostic(stmt, "unreachable code detected")
			}
		}
		a.visitStmt(stmt)
		if !unreachable && alwaysExits(stmt) {
			unreachable = true
		}
	}
}

func (a *astSemanticAnalyzer) visitStmt(stmt Stmt) {
	switch s := stmt.(type) {
	case ImportStmt:

	case VariableStmt:
		a.validateTypeHint(s.TypeHint, s.Line, s.Column)
		oldContext := a.expectedTypeContext
		if !s.TypeHint.IsEmpty() {
			a.expectedTypeContext = normalizeLSPType(a.root, s.TypeHint.Name)
		}
		typ := a.inferExprType(s.Value)
		a.expectedTypeContext = oldContext

		if !s.TypeHint.IsEmpty() {
			expectedType := normalizeLSPType(a.root, s.TypeHint.Name)
			a.validateObjectExprAgainstType(s.Value, expectedType, s.Line, s.Column)
			typ = expectedType
		} else {
			typ = normalizeLSPType(a.root, typ)
		}
		fields := map[string]SymbolInfo(nil)
		if typ == "object" || strings.HasPrefix(typ, "interface:") || strings.HasPrefix(typ, "class:") {
			fields = a.fieldsFromObjectExpr(s.Value, s.Line)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "variable " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri, Fields: fields})

	case DestructureStmt:
		typ := a.inferExprType(s.Value)
		typ = normalizeLSPType(a.root, typ)

		fields := map[string]SymbolInfo(nil)
		if typ == "object" {
			fields = a.fieldsFromObjectExpr(s.Value, s.Line)
		}

		ifaceSym, hasIface := resolveInterfaceFieldsForDestructure(a.root, typ)

		fieldInfos := collectDestructuredFieldInfo(s.Target, s.Line, s.Column)
		for _, info := range fieldInfos {
			if info.IsSpread {
				detail := "variable " + info.VarName
				if s.Constant {
					detail = "constant " + info.VarName
				}
				a.define(SymbolInfo{Name: info.VarName, Kind: SymbolVariable, Type: "any", Detail: detail, Line: s.Line, Column: s.Column, SourceURI: a.uri})
				continue
			}

			fieldTyp := ""
			if fields != nil {
				if sym, ok := fields[info.Key]; ok {
					fieldTyp = sym.Type
				}
			}
			if fieldTyp == "" && hasIface {
				fieldTyp = resolveFieldTypeFromInterface(ifaceSym, info.Key)
			}

			if fieldTyp == "" && hasIface {
				a.addError(s.Line, s.Column, "property '"+info.Key+"' does not exist on interface '"+ifaceSym.Name+"'")
				fieldTyp = "any"
			} else if fieldTyp == "" {
				fieldTyp = "any"
			}

			detail := "variable " + info.VarName
			if s.Constant {
				detail = "constant " + info.VarName
			}

			a.define(SymbolInfo{Name: info.VarName, Kind: SymbolVariable, Type: fieldTyp, Detail: detail, Line: s.Line, Column: s.Column, SourceURI: a.uri})

			if info.Nested != nil && info.Nested.Key != "" {
				a.defineDestructuredNested(a.root, info.Key, info.VarName, fieldTyp, info.Nested, s.Constant, s.Line, s.Column)
			}
		}

	case FieldStmt:
		a.validateTypeHint(s.TypeHint, s.Line, s.Column)
		typ := typeHintName(s.TypeHint, "any")
		if typ == "any" {
			typ = a.inferExprType(s.Value)
		}
		a.define(SymbolInfo{Name: s.Name, Kind: SymbolVariable, Type: typ, Detail: "field " + s.Name, Line: s.Line, Column: s.Column, SourceURI: a.uri})

	case FunctionStmt:
		a.visitFunction(s)

	case NativeFnStmt, ExternalFnStmt, ExternalGlobalStmt:

	case ClassStmt:
		oldTypeParams := a.activeTypeParams
		a.activeTypeParams = append(append([]string{}, oldTypeParams...), s.TypeParameters...)

		for _, f := range s.Fields {
			a.validateTypeHint(f.TypeHint, f.Line, f.Column)
			if f.Value != nil {
				a.inferExprType(f.Value)
			}
		}
		classSym := a.classSymbol(s)
		a.define(classSym)

		// Verify implemented interfaces
		for _, imp := range classSym.Implements {
			ifaceName := strings.TrimPrefix(stripLSPGenerics(imp), "interface:")
			if ifaceSym, ok := resolveInterfaceSymbol(a.scope, ifaceName); ok {
				// Check that each field of the interface is defined in the class
				for fieldName := range ifaceSym.Fields {
					if _, hasField := classSym.Fields[fieldName]; !hasField {
						if _, hasMethod := classSym.Methods[fieldName]; !hasMethod {
							a.addError(s.Line, s.Column, fmt.Sprintf("class '%s' is missing property '%s' from interface '%s'", s.Name, fieldName, ifaceSym.Name))
						}
					}
				}
			}
		}

		for _, m := range s.Methods {
			a.visitMethod(s.Name, m)
		}
		a.activeTypeParams = oldTypeParams

	case NamespaceStmt:
		a.pushScope()
		if nsSym, ok := a.root.Resolve(s.Name); ok {
			for _, member := range nsSym.Members {
				a.scope.Define(member)
			}
		}
		a.visitStatements(s.Statements)
		a.popScope()

	case EnumStmt:
		a.define(enumSymbolFromStmt(s, a.uri, a.text))

	case ExprStmt:
		a.inferExprType(s.Value)

	case AssignStmt:
		var targetType string
		if sym, ok := a.resolve(s.Name); ok {
			if sym.Kind == SymbolVariable && sym.Line > 0 {
				startLine := innermostBlockStartLine(a.text, sym.Line-1)
				if usageLine, usageCol := findNameBefore(a.text, s.Name, sym.Line, startLine); usageLine > 0 && (sym.Line > usageLine || (sym.Line == usageLine && sym.Column > usageCol)) {
					a.addError(usageLine, usageCol, "'"+s.Name+"' is used before initialization")
				}
			}
			targetType = sym.Type
		} else {
			a.addError(s.Line, s.Column, "undefined variable: "+s.Name)
		}
		oldContext := a.expectedTypeContext
		a.expectedTypeContext = targetType
		a.inferExprType(s.Value)
		a.expectedTypeContext = oldContext

		if targetType != "" {
			a.validateObjectExprAgainstType(s.Value, targetType, s.Line, s.Column)
		}

	case ReturnStmt:
		if s.HasValue {
			oldContext := a.expectedTypeContext
			a.expectedTypeContext = a.currentReturnType
			returnedType := a.inferExprType(s.Value)
			a.expectedTypeContext = oldContext

			if a.currentReturnType != "" && a.currentReturnType != "any" {
				a.validateObjectExprAgainstType(s.Value, a.currentReturnType, s.Line, s.Column)

				if !a.compareLSPTypes(returnedType, a.currentReturnType) {
					msg := fmt.Sprintf("cannot return type '%s' from this function (expected '%s')", returnedType, a.currentReturnType)
					if r, ok := byteRangeForExpr(a.text, s.Value); ok {
						a.addDiagnosticAtRange(r, msg)
					} else {
						a.addDiagnostic(s.Line, s.Column, msg)
					}
				}
			}
		} else {
			if a.currentReturnType != "" && a.currentReturnType != "any" && a.currentReturnType != "null" {
				msg := fmt.Sprintf("cannot return empty value from this function (expected '%s')", a.currentReturnType)
				if r, ok := byteRangeForNameAtLineColumn(a.text, s.Line, s.Column, "return"); ok {
					a.addDiagnosticAtRange(r, msg)
				} else {
					a.addDiagnostic(s.Line, s.Column, msg)
				}
			}
		}

	case IfStmt:
		a.inferExprType(s.Condition)
		ifLine := a.ifConditionLine(s)

		a.pushScope()
		if ifLine != "" {
			applyTypeNarrowing(a.scope, ifLine, false)
		}
		applyExprTypeNarrowing(a.scope, s.Condition, false)
		a.visitStatements(s.ThenBody)
		a.popScope()

		a.pushScope()
		if ifLine != "" {
			applyTypeNarrowing(a.scope, ifLine, true)
		}
		applyExprTypeNarrowing(a.scope, s.Condition, true)
		a.visitStatements(s.ElseBody)
		a.popScope()

		a.applyPostIfGuardNarrowing(s, ifLine)

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
		matchedType := a.inferExprType(s.Value)
		matchedType = normalizeLSPType(a.scope, matchedType)
		for _, mc := range s.Cases {
			a.inferMatchPatternExpr(mc.Value)
			a.pushScope()
			if call, ok := mc.Value.(MemberCallExpr); ok {
				for _, arg := range call.Args {
					if ident, ok := arg.(IdentExpr); ok && ident.Name != "_" {
						a.scope.Define(SymbolInfo{
							Name:      ident.Name,
							Kind:      SymbolVariable,
							Type:      "any",
							Detail:    "match payload variable " + ident.Name,
							SourceURI: a.uri,
						})
					}
				}
			}
			if mc.BindName != "" {
				a.scope.Define(SymbolInfo{
					Name:   mc.BindName,
					Kind:   SymbolVariable,
					Type:   matchedType,
					Detail: "match bind variable " + mc.BindName,
				})
			}
			if mc.Guard != nil {
				a.inferExprType(mc.Guard)
			}
			a.visitStatements(mc.Body)
			a.popScope()
		}
		if s.Default != nil {
			a.pushScope()
			a.visitStatements(s.Default)
			a.popScope()
		}

		if strings.HasPrefix(matchedType, "enum:") {
			enumName := strings.TrimPrefix(matchedType, "enum:")
			if enumSym, ok := resolveEnumSymbol(a.root, enumName); ok {
				allMembers := map[string]bool{}
				for mName := range enumSym.Members {
					allMembers[mName] = true
				}

				for _, mc := range s.Cases {
					if prop, ok := mc.Value.(PropertyExpr); ok {
						delete(allMembers, prop.Name)
					} else if ident, ok := mc.Value.(IdentExpr); ok {
						delete(allMembers, ident.Name)
					}
				}

				if len(allMembers) > 0 && s.Default == nil {
					unhandled := []string{}
					for mName := range allMembers {
						unhandled = append(unhandled, mName)
					}
					sort.Strings(unhandled)
					a.addError(s.Line, s.Column, fmt.Sprintf("match is not exhaustive on '%s': missing case '%s'", enumName, strings.Join(unhandled, "', '")))
				}
			}
		}
	}
}

func (a *astSemanticAnalyzer) inferMatchPatternExpr(expr Expr) {
	switch e := expr.(type) {
	case nil:
		return
	case MemberCallExpr:
		a.inferExprType(e.Object)
		for _, arg := range e.Args {
			switch arg.(type) {
			case IdentExpr:
				continue
			default:
				a.inferExprType(arg)
			}
		}
	case PropertyExpr:
		a.inferExprType(e.Object)
	case BinaryExpr:
		a.inferMatchPatternExpr(e.Left)
		a.inferMatchPatternExpr(e.Right)
	case UnaryExpr:
		a.inferMatchPatternExpr(e.Right)
	default:
		a.inferExprType(expr)
	}
}

func (a *astSemanticAnalyzer) visitFunction(fn FunctionStmt) {
	oldTypeParams := a.activeTypeParams
	a.activeTypeParams = append(append([]string{}, oldTypeParams...), fn.TypeParameters...)
	defer func() {
		a.activeTypeParams = oldTypeParams
	}()

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

	retType := a.currentReturnType
	if !fn.ReturnType.IsEmpty() && retType != "any" && retType != "null" {
		if !alwaysReturnsOrThrowsBlock(fn.Body) {
			a.addDiagnostic(fn.Line, fn.Column, fmt.Sprintf("missing return: function '%s' expects return type '%s'", fn.Name, retType))
		}
	}

	a.currentReturnType = oldReturn
}

func (a *astSemanticAnalyzer) ifConditionLine(stmt IfStmt) string {
	if a.text == "" {
		return ""
	}

	lines := strings.Split(a.text, "\n")
	if stmt.Line-1 < 0 || stmt.Line-1 >= len(lines) {
		return ""
	}

	return strings.TrimSpace(lines[stmt.Line-1])
}

func (a *astSemanticAnalyzer) applyPostIfGuardNarrowing(stmt IfStmt, ifLine string) {
	thenExits := alwaysReturnsOrThrowsBlock(stmt.ThenBody)
	elseExits := len(stmt.ElseBody) > 0 && alwaysReturnsOrThrowsBlock(stmt.ElseBody)

	if thenExits && !elseExits {
		if ifLine != "" {
			applyTypeNarrowing(a.scope, ifLine, true)
		}
		applyExprTypeNarrowing(a.scope, stmt.Condition, true)
		return
	}

	if elseExits && !thenExits {
		if ifLine != "" {
			applyTypeNarrowing(a.scope, ifLine, false)
		}
		applyExprTypeNarrowing(a.scope, stmt.Condition, false)
	}
}

func (a *astSemanticAnalyzer) visitMethod(className string, fn FunctionStmt) {
	oldClass := a.currentClass
	a.currentClass = className

	oldReturn := a.currentReturnType
	a.currentReturnType = returnTypeNameScoped(a.root, fn.ReturnType)

	oldTypeParams := a.activeTypeParams
	var classTypeParams []string
	if classSym, ok := resolveClassSymbol(a.root, className); ok && classSym.Kind == SymbolClass {
		classTypeParams = classSym.TypeParameters
	}
	a.activeTypeParams = append(append(append([]string{}, oldTypeParams...), classTypeParams...), fn.TypeParameters...)
	defer func() {
		a.activeTypeParams = oldTypeParams
	}()

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

	retType := a.currentReturnType
	if !fn.ReturnType.IsEmpty() && retType != "any" && retType != "null" {
		if !alwaysReturnsOrThrowsBlock(fn.Body) {
			a.addDiagnostic(fn.Line, fn.Column, fmt.Sprintf("missing return: function '%s' expects return type '%s'", fn.Name, retType))
		}
	}

	a.currentClass = oldClass
	a.currentReturnType = oldReturn
}

func (a *astSemanticAnalyzer) validateTypeHint(hint TypeHint, line int, column int) {
	if hint.IsEmpty() {
		return
	}

	for _, part := range splitUnionType(hint.Name) {
		if params, ok := callableFunctionParamTypes(part); ok {
			for _, param := range params {
				a.validateTypeHint(TypeHint{Name: param}, line, column)
			}
			continue
		}

		if !a.typeNameExists(part) {
			a.addDiagnostic(line, column, "unknown type: "+part)
		}
	}
}

func (a *astSemanticAnalyzer) typeNameExists(typ string) bool {
	typ = stripLSPGenerics(typ)
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return true
	}

	for _, tp := range a.activeTypeParams {
		if typ == tp {
			return true
		}
	}

	if strings.HasPrefix(typ, "array:") {
		return a.typeNameExists(strings.TrimPrefix(typ, "array:"))
	}
	if params, ok := callableFunctionParamTypes(typ); ok {
		for _, param := range params {
			if !a.typeNameExists(param) {
				return false
			}
		}
		return true
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

	typ = normalizeLSPType(scope, typ)
	if param.Variadic {
		typ = "array:" + typ
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

func isLSPPrefix(typ string) bool {
	prefixes := []string{"array:", "class:", "interface:", "enum:", "namespace:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(typ, prefix) {
			return true
		}
	}
	return false
}

type LSPType struct {
	Prefix string
	Name   string
	Args   []LSPType
	Inner  *LSPType
}

func parseOneLSPType(scope *Scope, tokens []string) (LSPType, []string) {
	if len(tokens) == 0 {
		return LSPType{}, nil
	}

	tok := tokens[0]
	if (tok == "class" || tok == "interface" || tok == "enum" || tok == "namespace" || tok == "array") && len(tokens) > 1 {
		inner, remaining := parseOneLSPType(scope, tokens[1:])

		if len(remaining) > 0 && inner.Inner == nil {
			for len(remaining) > 0 {
				var arg LSPType
				arg, remaining = parseOneLSPType(scope, remaining)
				inner.Args = append(inner.Args, arg)
			}
		}

		return LSPType{
			Prefix: tok + ":",
			Inner:  &inner,
		}, remaining
	}

	expectedArgs := getTypeParamCount(scope, tok)
	remaining := tokens[1:]
	args := []LSPType{}
	for i := 0; i < expectedArgs && len(remaining) > 0; i++ {
		var arg LSPType
		arg, remaining = parseOneLSPType(scope, remaining)
		args = append(args, arg)
	}

	return LSPType{
		Name: tok,
		Args: args,
	}, remaining
}

func getTypeParamCount(scope *Scope, name string) int {
	if sym, ok := scope.Resolve(name); ok {
		return len(sym.TypeParameters)
	}
	if classSym, ok := resolveClassSymbol(scope, name); ok {
		return len(classSym.TypeParameters)
	}
	if ifaceSym, ok := resolveInterfaceSymbol(scope, name); ok {
		return len(ifaceSym.TypeParameters)
	}
	if strings.Contains(name, ".") {
		parts := strings.SplitN(name, ".", 2)
		if ns, ok := scope.Resolve(parts[0]); ok && ns.Kind == SymbolNamespace {
			if member, ok := ns.Members[parts[1]]; ok {
				return len(member.TypeParameters)
			}
		}
	}
	return 0
}

func normalizeLSPTypeStruct(scope *Scope, t LSPType, hasTypePrefix bool) LSPType {
	if t.Inner != nil {
		innerHasPrefix := hasTypePrefix
		if t.Prefix == "class:" || t.Prefix == "interface:" || t.Prefix == "enum:" || t.Prefix == "namespace:" {
			innerHasPrefix = true
		}
		normalizedInner := normalizeLSPTypeStruct(scope, *t.Inner, innerHasPrefix)
		t.Inner = &normalizedInner
		return t
	}

	normalizedArgs := make([]LSPType, len(t.Args))
	for i, arg := range t.Args {
		normalizedArgs[i] = normalizeLSPTypeStruct(scope, arg, false)
	}
	t.Args = normalizedArgs

	if t.Name == "" {
		return t
	}

	name := t.Name
	prefix := t.Prefix

	if prefix == "" && !hasTypePrefix {
		switch name {
		case "string", "number", "bool", "object", "array", "any", "null", "function", "error", "buffer", "void":
			if name == "array" {
				name = "any"
				prefix = "array:"
			}
		default:
			if sym, ok := scope.Resolve(name); ok {
				switch sym.Kind {
				case SymbolClass:
					prefix = "class:"
				case SymbolInterface:
					prefix = "interface:"
				case SymbolEnum:
					prefix = "enum:"
				}
			} else if strings.Contains(name, ".") {
				parts := strings.SplitN(name, ".", 2)
				nsName := parts[0]
				memberName := parts[1]
				if ns, ok := scope.Resolve(nsName); ok && ns.Kind == SymbolNamespace {
					if member, ok := ns.Members[memberName]; ok {
						switch member.Kind {
						case SymbolClass:
							prefix = "class:"
						case SymbolInterface:
							prefix = "interface:"
						case SymbolEnum:
							prefix = "enum:"
						}
					}
				}
			}
		}
	}

	t.Prefix = prefix
	t.Name = name
	return t
}

func formatLSPTypeStruct(t LSPType) string {
	if t.Inner != nil {
		return t.Prefix + formatLSPTypeStruct(*t.Inner)
	}
	res := t.Prefix + t.Name
	for _, arg := range t.Args {
		res += ":" + formatLSPTypeStruct(arg)
	}
	return res
}

func normalizeLSPType(scope *Scope, typ string) string {
	typ = strings.TrimSpace(typ)
	if typ == "" {
		return "any"
	}
	if strings.HasSuffix(typ, "?") {
		base := strings.TrimSuffix(typ, "?")
		normalized := normalizeLSPType(scope, base)
		if normalized == "" {
			normalized = "any"
		}
		return normalized + " | null"
	}
	if strings.HasPrefix(typ, "function(") {
		return normalizeFunctionLSPType(scope, typ)
	}

	if parts := splitUnionType(typ); len(parts) > 1 {
		out := []string{}
		for _, part := range parts {
			out = append(out, normalizeLSPType(scope, part))
		}
		return strings.Join(out, " | ")
	}

	tokens := strings.Split(typ, ":")
	parsed, remaining := parseOneLSPType(scope, tokens)
	for len(remaining) > 0 {
		var next LSPType
		next, remaining = parseOneLSPType(scope, remaining)
		parsed.Args = append(parsed.Args, next)
	}

	normalized := normalizeLSPTypeStruct(scope, parsed, false)
	return formatLSPTypeStruct(normalized)
}

func normalizeFunctionLSPType(scope *Scope, typ string) string {
	params, ok := callableFunctionParamTypes(typ)
	if !ok {
		return typ
	}

	for i, param := range params {
		if strings.HasSuffix(param, "?") {
			params[i] = strings.TrimSuffix(params[i], "?") + " | null"
		}
		params[i] = normalizeLSPType(scope, param)
	}

	return "function(" + strings.Join(params, ", ") + ")"
}

func callableFunctionParamTypes(typ string) ([]string, bool) {
	typ = strings.TrimSpace(typ)
	if !strings.HasPrefix(typ, "function(") {
		return nil, false
	}

	open := strings.Index(typ, "(")
	if open < 0 {
		return nil, false
	}

	depth := 0
	inString := byte(0)
	escaped := false
	close := -1
	for i := open; i < len(typ); i++ {
		ch := typ[i]
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
			depth++
		case ')':
			depth--
			if depth == 0 {
				close = i
				i = len(typ)
			}
		}
	}
	if close < 0 {
		return nil, false
	}

	inner := strings.TrimSpace(typ[open+1 : close])
	if inner == "" {
		return []string{}, true
	}

	params := splitTopLevel(inner, ',')
	for i, param := range params {
		params[i] = strings.TrimSpace(param)
	}
	return params, true
}

func stripLSPPrefix(t string) string {
	prefixes := []string{"class:", "interface:", "enum:", "namespace:"}
	for _, p := range prefixes {
		if strings.HasPrefix(t, p) {
			return strings.TrimPrefix(t, p)
		}
	}
	return t
}

func inferTypeParam(paramType string, argType string, tp string) (string, bool) {
	paramType = stripLSPPrefix(strings.TrimSpace(paramType))
	argType = stripLSPPrefix(strings.TrimSpace(argType))

	if strings.Contains(paramType, "|") {
		for _, part := range splitUnionType(paramType) {
			if isNullishLSPType(part) {
				continue
			}
			if res, ok := inferTypeParam(strings.TrimSpace(part), argType, tp); ok {
				return res, true
			}
		}
	}

	if strings.Contains(argType, "|") {
		for _, part := range splitUnionType(argType) {
			if isNullishLSPType(part) {
				continue
			}
			if res, ok := inferTypeParam(paramType, strings.TrimSpace(part), tp); ok {
				return res, true
			}
		}
	}

	if paramType == tp {
		return argType, true
	}

	if strings.HasPrefix(paramType, "array:") && strings.HasPrefix(argType, "array:") {
		return inferTypeParam(strings.TrimPrefix(paramType, "array:"), strings.TrimPrefix(argType, "array:"), tp)
	}

	if strings.Contains(paramType, ":") && strings.Contains(argType, ":") {
		pParts := strings.Split(paramType, ":")
		aParts := strings.Split(argType, ":")
		if len(pParts) >= 2 && len(aParts) >= 2 && pParts[0] == aParts[0] {
			for i := 1; i < len(pParts); i++ {
				if i >= len(aParts) {
					break
				}

				if pParts[i] == tp {
					return strings.Join(aParts[i:], ":"), true
				}

				if res, ok := inferTypeParam(pParts[i], aParts[i], tp); ok {
					return res, true
				}
			}
		}
	}

	return "", false
}

func stripLSPGenerics(typ string) string {
	if typ == "" {
		return ""
	}
	if parts := splitUnionType(typ); len(parts) > 1 {
		for i, part := range parts {
			parts[i] = stripLSPGenerics(part)
		}
		return strings.Join(parts, " | ")
	}
	prefixes := []string{"array:", "class:", "interface:", "enum:", "namespace:"}
	for _, prefix := range prefixes {
		if strings.HasPrefix(typ, prefix) {
			return prefix + stripLSPGenerics(strings.TrimPrefix(typ, prefix))
		}
	}
	if strings.Contains(typ, ":") {
		return strings.Split(typ, ":")[0]
	}
	return typ
}

func substituteLSPType(typ string, subst map[string]string) string {
	if typ == "" {
		return ""
	}
	if val, ok := subst[typ]; ok {
		return val
	}
	if parts := splitUnionType(typ); len(parts) > 1 {
		for i, part := range parts {
			parts[i] = substituteLSPType(part, subst)
		}
		return strings.Join(parts, " | ")
	}
	if strings.HasPrefix(typ, "array:") {
		return "array:" + substituteLSPType(strings.TrimPrefix(typ, "array:"), subst)
	}
	if strings.HasPrefix(typ, "function(") {
		paramTypes, ok := callableFunctionParamTypes(typ)
		if ok {
			substituted := make([]string, len(paramTypes))
			for i, pt := range paramTypes {
				substituted[i] = substituteLSPType(strings.TrimSpace(pt), subst)
			}
			return "function(" + strings.Join(substituted, ", ") + ")"
		}
	}
	if strings.Contains(typ, ":") {
		parts := strings.Split(typ, ":")
		for i := 1; i < len(parts); i++ {
			parts[i] = substituteLSPType(parts[i], subst)
		}
		return strings.Join(parts, ":")
	}
	return typ
}

func typeHintArgsToLSPTypes(scope *Scope, args []TypeHint) []string {
	out := []string{}
	for _, arg := range args {
		name := strings.TrimSpace(arg.Name)
		if name == "" {
			name = "any"
		}
		out = append(out, normalizeLSPType(scope, name))
	}
	return out
}

func classTypeFromSymbol(scope *Scope, qualifiedName string, sym SymbolInfo, explicitTypeArgs []TypeHint) string {
	name := qualifiedName
	if strings.TrimSpace(name) == "" {
		name = sym.Name
	}

	formattedArgs := typeHintArgsToLSPTypes(scope, explicitTypeArgs)
	if len(formattedArgs) == 0 {
		return "class:" + name
	}
	return "class:" + name + ":" + strings.Join(formattedArgs, ":")
}

func (a *astSemanticAnalyzer) classConstructorTypeAndCheckCall(qualifiedName string, sym SymbolInfo, explicitTypeArgs []TypeHint, args []Expr, line int, column int) string {
	classType := classTypeFromSymbol(a.root, qualifiedName, sym, explicitTypeArgs)

	initSym, hasInit := sym.Methods["init"]
	if !hasInit {
		return classType
	}

	a.checkArgumentCount(qualifiedName, len(args), initSym.Params, line, column)

	subst := map[string]string{}
	if len(explicitTypeArgs) > 0 {
		for i, tp := range sym.TypeParameters {
			if i < len(explicitTypeArgs) {
				subst[tp] = normalizeLSPType(a.root, explicitTypeArgs[i].Name)
			}
		}
	} else {
		for _, tp := range sym.TypeParameters {
			subst[tp] = "any"
		}
		for i, arg := range args {
			if i >= len(initSym.Params) {
				break
			}
			param := initSym.Params[i]
			argType := a.inferExprType(arg)
			for _, tp := range sym.TypeParameters {
				if res, ok := inferTypeParam(param.Type, argType, tp); ok {
					subst[tp] = res
				}
			}
		}
	}

	for _, tp := range sym.TypeParameters {
		if _, ok := subst[tp]; !ok {
			subst[tp] = "any"
		}
	}

	substitutedParams := make([]StdArg, len(initSym.Params))
	for i, param := range initSym.Params {
		substitutedParams[i] = param
		substitutedParams[i].Type = substituteLSPType(param.Type, subst)
	}
	a.checkArgumentTypes(qualifiedName, args, substitutedParams, line, column)

	if len(explicitTypeArgs) == 0 && len(sym.TypeParameters) > 0 {
		formattedArgs := []string{}
		for _, tp := range sym.TypeParameters {
			formattedArgs = append(formattedArgs, subst[tp])
		}
		if len(formattedArgs) > 0 {
			return "class:" + qualifiedName + ":" + strings.Join(formattedArgs, ":")
		}
	}

	return classType
}

func (a *astSemanticAnalyzer) checkCall(name string, args []Expr, sym SymbolInfo, explicitTypeArgs []TypeHint, line int, column int) string {
	a.checkArgumentCount(name, len(args), sym.Params, line, column)

	subst := map[string]string{}
	if len(explicitTypeArgs) > 0 {
		for i, tp := range sym.TypeParameters {
			if i < len(explicitTypeArgs) {
				subst[tp] = normalizeLSPType(a.root, explicitTypeArgs[i].Name)
			}
		}
	} else {
		for _, tp := range sym.TypeParameters {
			subst[tp] = "any"
		}
		for i, arg := range args {
			if i >= len(sym.Params) {
				break
			}
			param := sym.Params[i]
			argType := a.inferExprType(arg)
			for _, tp := range sym.TypeParameters {
				if res, ok := inferTypeParam(param.Type, argType, tp); ok {
					subst[tp] = res
				}
			}
		}
	}

	substitutedParams := make([]StdArg, len(sym.Params))
	for i, param := range sym.Params {
		substitutedParams[i] = param
		substitutedParams[i].Type = substituteLSPType(param.Type, subst)
	}

	a.checkArgumentTypes(name, args, substitutedParams, line, column)

	returnType := sym.Returns
	if returnType != "" {
		returnType = substituteLSPType(returnType, subst)
	}
	return firstNonEmpty(returnType, "any")
}

func inferredCallResultType(typ string) string {
	if typ == "function" || strings.HasPrefix(typ, "function(") {
		return "any"
	}
	return typ
}

func isCallableLSPType(typ string) bool {
	typ = strings.TrimSpace(typ)
	return typ == "function" || strings.HasPrefix(typ, "function(")
}

func (a *astSemanticAnalyzer) checkCallableTypeCall(name string, callableType string, args []Expr, line int, column int) string {
	if !isCallableLSPType(callableType) || callableType == "any" || callableType == "unknown" || callableType == "" {
		return inferredCallResultType(callableType)
	}

	if callableType == "function" {
		return "any"
	}

	paramTypes, ok := callableFunctionParamTypes(callableType)
	if !ok {
		return "any"
	}

	params := make([]StdArg, len(paramTypes))
	for i, pt := range paramTypes {
		pt = strings.TrimSpace(pt)
		pt = normalizeLSPType(a.root, pt)
		params[i] = StdArg{
			Name: "p" + strconv.Itoa(i+1),
			Type: pt,
		}
	}

	a.checkArgumentCount(name, len(args), params, line, column)
	a.checkArgumentTypes(name, args, params, line, column)

	returnType := "any"
	if parenIdx := strings.Index(callableType, "("); parenIdx >= 0 {
		closeIdx := strings.LastIndex(callableType, ")")
		if closeIdx > parenIdx {
			after := strings.TrimSpace(callableType[closeIdx+1:])
			if after != "" {
				returnType = normalizeLSPType(a.root, after)
			}
		}
	}

	return returnType
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
			return "array:any"
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
		expectedType := strings.TrimSpace(a.expectedTypeContext)
		var expectedSym SymbolInfo
		var okSym bool

		if strings.HasPrefix(expectedType, "interface:") {
			ifaceName := strings.TrimPrefix(expectedType, "interface:")
			expectedSym, okSym = resolveInterfaceSymbol(a.root, ifaceName)
		} else if strings.HasPrefix(expectedType, "class:") {
			className := strings.TrimPrefix(expectedType, "class:")
			expectedSym, okSym = resolveClassSymbol(a.root, className)
		}

		for _, f := range e.Fields {
			oldContext := a.expectedTypeContext
			if okSym {
				if expectedField, exists := expectedSym.Fields[f.Name]; exists {
					a.expectedTypeContext = expectedField.Type
				} else {
					a.expectedTypeContext = "any"
				}
			} else {
				a.expectedTypeContext = "any"
			}
			a.inferExprType(f.Value)
			a.expectedTypeContext = oldContext
		}
		return "object"
	case IdentExpr:
		if sym, ok := a.resolve(e.Name); ok {
			if sym.Kind == SymbolVariable && (sym.Line > e.Line || (sym.Line == e.Line && sym.Column > e.Column)) {
				a.addError(e.Line, e.Column, "'"+e.Name+"' is used before initialization")
			}
			return sym.Type
		}
		if !tinyKeywords[e.Name] && e.Name != "_" {
			a.addError(e.Line, e.Column, "undefined variable: "+e.Name)
		}
		return "unknown"
	case InstantiatedExpr:
		typ := a.inferExprType(e.Object)
		base := stripLSPGenerics(typ)
		formattedArgs := typeHintArgsToLSPTypes(a.root, e.TypeArgs)
		if len(formattedArgs) > 0 {
			return base + ":" + strings.Join(formattedArgs, ":")
		}
		return base

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
				if !ok || isPrivateImportMember(memberSym) {
					msg := "undefined export: " + ident.Name + "." + e.Name
					if r, ok := byteRangeForExpr(a.text, e); ok {
						a.addDiagnosticAtRange(r, msg)
					} else {
						a.addDiagnostic(e.Line, e.Column, msg)
					}
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
		callLine, callColumn := nodePosition(e.Callee)
		if callLine <= 0 || callColumn <= 0 {
			callLine, callColumn = e.Line, e.Column
		}

		for _, arg := range e.Args {
			a.inferExprType(arg)
		}

		switch callee := e.Callee.(type) {
		case InstantiatedExpr:
			switch instObj := callee.Object.(type) {
			case IdentExpr:
				if sym, ok := a.resolve(instObj.Name); ok {
					if sym.Kind == SymbolClass {
						return a.classConstructorTypeAndCheckCall(sym.Name, sym, callee.TypeArgs, e.Args, callLine, callColumn)
					}

					if sym.Kind == SymbolFunction {
						return a.checkCall(instObj.Name, e.Args, sym, callee.TypeArgs, callLine, callColumn)
					}
				}
			case PropertyExpr:
				if ident, ok := instObj.Object.(IdentExpr); ok {
					if ns, exists := a.resolve(ident.Name); exists && ns.Kind == SymbolNamespace {
						memberSym, ok := ns.Members[instObj.Name]
						if !ok || isPrivateImportMember(memberSym) {
							return "unknown"
						}

						qualifiedName := ident.Name + "." + memberSym.Name
						if memberSym.Kind == SymbolClass {
							return a.classConstructorTypeAndCheckCall(qualifiedName, memberSym, callee.TypeArgs, e.Args, callLine, callColumn)
						}

						if memberSym.Kind == SymbolFunction {
							ret := a.checkCall(qualifiedName, e.Args, memberSym, callee.TypeArgs, callLine, callColumn)
							return qualifyNamespaceType(ident.Name, firstNonEmpty(ret, "any"), ns.Members)
						}
					}
				}
			}

		case IdentExpr:
			if sym, ok := a.resolve(callee.Name); ok {
				if sym.Kind == SymbolClass {
					return a.classConstructorTypeAndCheckCall(sym.Name, sym, nil, e.Args, callLine, callColumn)
				}

				if sym.Kind == SymbolFunction {
					a.storeExpectedCallbackTypesFromParams(sym.Params, e.Args)
					return a.checkCall(callee.Name, e.Args, sym, nil, callLine, callColumn)
				}

				if !isCallableLSPType(sym.Type) && sym.Type != "any" && sym.Type != "unknown" && sym.Type != "" {
					a.addError(callLine, callColumn, fmt.Sprintf("cannot call non-function type '%s'", sym.Type))
				}
				if isCallableLSPType(sym.Type) {
					return a.checkCallableTypeCall(callee.Name, sym.Type, e.Args, callLine, callColumn)
				}
				return inferredCallResultType(sym.Type)
			}

		case PropertyExpr:
			objType := a.inferExprType(callee.Object)

			if ident, ok := callee.Object.(IdentExpr); ok {
				if ns, exists := a.resolve(ident.Name); exists && ns.Kind == SymbolNamespace {
					memberSym, ok := ns.Members[callee.Name]
					if !ok || isPrivateImportMember(memberSym) {
						return "unknown"
					}

					if memberSym.Kind == SymbolClass {
						return a.classConstructorTypeAndCheckCall(ident.Name+"."+memberSym.Name, memberSym, nil, e.Args, callLine, callColumn)
					}

					if memberSym.Kind == SymbolFunction {
						ret := a.checkCall(ident.Name+"."+callee.Name, e.Args, memberSym, nil, callLine, callColumn)
						return qualifyNamespaceType(ident.Name, firstNonEmpty(ret, "any"), ns.Members)
					}

					if !isCallableLSPType(memberSym.Type) && memberSym.Type != "any" && memberSym.Type != "unknown" && memberSym.Type != "" {
						a.addError(callLine, callColumn, fmt.Sprintf("cannot call non-function type '%s'", memberSym.Type))
					}
					if isCallableLSPType(memberSym.Type) {
						return a.checkCallableTypeCall(ident.Name+"."+callee.Name, memberSym.Type, e.Args, callLine, callColumn)
					}
					return inferredCallResultType(memberSym.Type)
				}
			}

			mType := a.memberType(objType, callee.Name)
			if !isCallableLSPType(mType) && mType != "any" && mType != "unknown" && mType != "" {
				a.addError(callLine, callColumn, fmt.Sprintf("cannot call non-function type '%s'", mType))
			}
			if isCallableLSPType(mType) {
				return a.checkCallableTypeCall(callee.Name, mType, e.Args, callLine, callColumn)
			}
			return inferredCallResultType(mType)
		}

		calleeType := a.inferExprType(e.Callee)
		if !isCallableLSPType(calleeType) && calleeType != "any" && calleeType != "unknown" && calleeType != "" {
			a.addError(callLine, callColumn, fmt.Sprintf("cannot call non-function type '%s'", calleeType))
		}
		if isCallableLSPType(calleeType) {
			return a.checkCallableTypeCall("(call)", calleeType, e.Args, callLine, callColumn)
		}
		return inferredCallResultType(calleeType)

	case MemberCallExpr:
		if ident, ok := e.Object.(IdentExpr); ok {
			sym, exists := a.resolve(ident.Name)
			if exists && sym.Kind == SymbolNamespace {
				memberSym, ok := sym.Members[e.Method]
				if !ok || isPrivateImportMember(memberSym) {
					msg := "undefined export: " + ident.Name + "." + e.Method
					if r, ok := byteRangeForExpr(a.text, e); ok {
						a.addDiagnosticAtRange(r, msg)
					} else {
						a.addDiagnostic(e.Line, e.Column, msg)
					}
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
					ret := a.checkCall(ident.Name+"."+e.Method, e.Args, memberSym, nil, e.Line, e.Column)
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
						return qualifyNamespaceType(ident.Name, ret, sym.Members)
					}
					return "any"
				}

				return inferredCallResultType(memberSym.Type)
			}
		}

		objType := a.inferExprType(e.Object)

		a.storeExpectedCallbackTypes(objType, e.Method, e.Args)

		for _, arg := range e.Args {
			a.inferExprType(arg)
		}

		if shouldCheckMemberAccess(objType) {
			if a.privateMemberByType(objType, e.Method) && !a.canAccessPrivateMember(e.Object, objType) {
				msg := "private member is not accessible: " + e.Method
				if r, ok := byteRangeForExpr(a.text, e); ok {
					a.addDiagnosticAtRange(r, msg)
				} else {
					a.addDiagnostic(e.Line, e.Column, msg)
				}
			} else if !a.memberExistsByType(objType, e.Method) {
				msg := "undefined method or property: " + e.Method
				if r, ok := byteRangeForExpr(a.text, e); ok {
					a.addDiagnosticAtRange(r, msg)
				} else {
					a.addDiagnostic(e.Line, e.Column, msg)
				}
			}
		}
		a.checkKnownMemberCall(objType, e.Method, e.Args, e.Line, e.Column)

		mType := a.memberType(objType, e.Method)
		if isCallableLSPType(mType) {
			return a.checkCallableTypeCall(e.Method, mType, e.Args, e.Line, e.Column)
		}

		return mType
	case CallExpr:
		for _, arg := range e.Args {
			a.inferExprType(arg)
		}
		if sym, ok := a.resolve(e.Name); ok {
			if sym.Kind == SymbolClass {
				return "class:" + sym.Name
			}
			if sym.Kind == SymbolFunction {
				return a.checkCall(e.Name, e.Args, sym, nil, e.Line, e.Column)
			}
			if isCallableLSPType(sym.Type) {
				return a.checkCallableTypeCall(e.Name, sym.Type, e.Args, e.Line, e.Column)
			}
			return inferredCallResultType(sym.Type)
		}
		a.addError(e.Line, e.Column, "undefined variable: "+e.Name)
		return "unknown"
	case FunctionExpr:
		a.pushScope()
		var expectedTypes []string
		if a.pendingCallbackTypes != nil {
			key := [2]int{e.Line, e.Column}
			expectedTypes = a.pendingCallbackTypes[key]
			delete(a.pendingCallbackTypes, key)
		}
		if len(expectedTypes) == 0 && strings.HasPrefix(a.expectedTypeContext, "function(") {
			expectedTypes, _ = callableFunctionParamTypes(a.expectedTypeContext)
		}
		for _, p := range e.Params {
			a.validateTypeHint(p.TypeHint, e.Line, e.Column)
		}
		for i, p := range e.Params {
			sym := paramSymbol(a.scope, p, a.uri, e.Line, e.Column)
			if sym.Type == "any" && i < len(expectedTypes) && strings.TrimSpace(expectedTypes[i]) != "" {
				sym.Type = normalizeLSPType(a.root, expectedTypes[i])
			}
			a.define(sym)
		}

		oldReturn := a.currentReturnType
		if !e.ReturnType.IsEmpty() {
			a.currentReturnType = returnTypeNameScoped(a.root, e.ReturnType)
		} else {
			a.currentReturnType = "any"
		}

		a.visitStatements(e.Body)

		a.currentReturnType = oldReturn
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
		if e.Op == TOKEN_TILDE {
			return "number"
		}
		return "number"
	case BinaryExpr:
		lt := a.inferExprType(e.Left)
		rt := a.inferExprType(e.Right)
		switch e.Op {
		case TOKEN_EQ, TOKEN_NEQ, TOKEN_LT, TOKEN_GT, TOKEN_LTE, TOKEN_GTE, TOKEN_AND, TOKEN_OR:
			return "bool"
		case TOKEN_AMP, TOKEN_PIPE, TOKEN_CARET, TOKEN_LSHIFT, TOKEN_RSHIFT:
			return "number"
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
				var explicitClassArgs []TypeHint
				if strings.Contains(className, ":") {
					parts := strings.Split(className, ":")
					for _, p := range parts[1:] {
						explicitClassArgs = append(explicitClassArgs, TypeHint{Name: p})
					}
				}

				mergedSym := methodSym
				mergedSym.TypeParameters = append(append([]string{}, classSym.TypeParameters...), methodSym.TypeParameters...)

				a.checkCall(className+"."+method, args, mergedSym, explicitClassArgs, line, column)
			}
		}
		return
	}

	if methodInfo, ok := GetNativeMethodInfo(receiverType, method); ok {
		a.checkArgumentCount(receiverType+"."+method, len(args), methodInfo.Args, line, column)
		a.checkArgumentTypes(receiverType+"."+method, args, methodInfo.Args, line, column)
	}
}

func (a *astSemanticAnalyzer) storeExpectedCallbackTypes(receiverType string, method string, args []Expr) {
	receiverType = strings.TrimSpace(receiverType)
	if receiverType == "" || strings.Contains(receiverType, "|") {
		return
	}

	var params []StdArg

	if strings.HasPrefix(receiverType, "class:") {
		className := strings.TrimPrefix(receiverType, "class:")
		if classSym, ok := resolveClassSymbol(a.root, className); ok {
			if methodSym, ok := classSym.Methods[method]; ok {
				params = methodSym.Params
			}
		}
	} else if methodInfo, ok := GetNativeMethodInfo(receiverType, method); ok {
		params = methodInfo.Args
	}

	a.storeExpectedCallbackTypesFromParams(params, args)
}

func (a *astSemanticAnalyzer) storeExpectedCallbackTypesFromParams(params []StdArg, args []Expr) {
	if len(params) == 0 {
		return
	}

	for i, arg := range args {
		fnExpr, ok := arg.(FunctionExpr)
		if !ok {
			continue
		}
		var param StdArg
		if i < len(params) {
			param = params[i]
		} else if len(params) > 0 && params[len(params)-1].Variadic {
			param = params[len(params)-1]
		} else {
			continue
		}

		expectedType := param.Type
		if param.Variadic {
			if strings.HasPrefix(expectedType, "array:") {
				expectedType = strings.TrimPrefix(expectedType, "array:")
			}
		}
		expectedParamTypes, hasSignature := callableParamTypesFromType(expectedType)
		if !hasSignature || len(expectedParamTypes) == 0 {
			continue
		}

		if a.pendingCallbackTypes == nil {
			a.pendingCallbackTypes = make(map[[2]int][]string)
		}
		a.pendingCallbackTypes[[2]int{fnExpr.Line, fnExpr.Column}] = expectedParamTypes
	}
}

func callableParamTypesFromType(typ string) ([]string, bool) {
	typ = strings.TrimSpace(typ)
	if strings.HasPrefix(typ, "function(") {
		return callableFunctionParamTypes(typ)
	}
	if !strings.Contains(typ, "|") {
		return nil, false
	}
	for _, part := range splitUnionType(typ) {
		if params, ok := callableFunctionParamTypes(part); ok {
			return params, true
		}
	}
	return nil, false
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
	addMemberDiagnostic := func(message string) {
		if r, ok := byteRangeForNameAtLineColumn(a.text, line, column, member); ok {
			a.addDiagnosticAtRange(r, message)
			return
		}
		a.addDiagnostic(line, column, message)
	}

	if ident, ok := object.(IdentExpr); ok {
		if sym, exists := a.resolve(ident.Name); exists {
			if sym.Type == "object" {
				return
			}

			if memberExistsOnSymbol(a.root, sym, member) {
				if a.privateMemberByType(sym.Type, member) && !a.canAccessPrivateMember(object, sym.Type) {
					addMemberDiagnostic("private member is not accessible: " + member)
				}
				return
			}

			if !shouldCheckMemberAccess(sym.Type) {
				return
			}

			addMemberDiagnostic("undefined method or property: " + member)
			return
		}
	}

	objType := a.inferExprType(object)
	if !shouldCheckMemberAccess(objType) {
		return
	}
	if a.privateMemberByType(objType, member) && !a.canAccessPrivateMember(object, objType) {
		addMemberDiagnostic("private member is not accessible: " + member)
		return
	}
	if !a.memberExistsByType(objType, member) {
		addMemberDiagnostic("undefined method or property: " + member)
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

	if isGlobalPropertyMethod(member) {
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

	if _, _, ok := resolveMemberFromStaticType(a.root, typ, member); ok {
		return true
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

	if isGlobalPropertyMethod(member) {
		return globalPropertyMethodType(member)
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

	if _, memberType, ok := resolveMemberFromStaticType(a.root, typ, member); ok {
		return firstNonEmpty(memberType, "any")
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
		enumName := strings.TrimPrefix(typ, "enum:")
		if enumSym, ok := resolveEnumSymbol(a.root, enumName); ok {
			if memberSym, ok := enumSym.Members[member]; ok {
				return memberSym.Type
			}
		}
		return "any"
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

		var ret string = "unknown"
		if methodSym, ok := classSym.Methods[member]; ok {
			ret = firstNonEmpty(methodSym.Returns, "any")
		} else if fieldSym, ok := classSym.Fields[member]; ok {
			ret = firstNonEmpty(fieldSym.Type, "any")
		}

		if ret != "unknown" {
			if strings.Contains(className, ":") {
				parts := strings.Split(className, ":")
				subst := map[string]string{}
				for i, pName := range classSym.TypeParameters {
					if i+1 < len(parts) {
						subst[pName] = parts[i+1]
					}
				}
				ret = substituteLSPType(ret, subst)
			}
			return ret
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

func findEnclosingIfAndElse(text string, pos Position) (string, bool, bool) {
	lines := strings.Split(text, "\n")
	if pos.Line >= len(lines) {
		return "", false, false
	}

	depth := 0
	isInElse := false
	for i := pos.Line; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])

		if strings.HasPrefix(line, "//") || line == "" {
			continue
		}

		if depth == 0 && strings.Contains(line, "else") {
			isInElse = true
		}

		if strings.Contains(line, "}") {
			depth--
		}
		if strings.Contains(line, "{") {
			depth++
		}

		if depth > 0 && strings.HasPrefix(line, "if ") {
			return line, isInElse, true
		}
	}
	return "", false, false
}

func findEnclosingIfBlock(text string, pos Position) (string, bool) {
	ifLine, _, ok := findEnclosingIfAndElse(text, pos)
	return ifLine, ok
}

func applyPriorGuardReturnNarrowing(scope *Scope, text string, pos Position) {
	if strings.TrimSpace(text) == "" {
		return
	}

	lines := strings.Split(text, "\n")
	maxLine := pos.Line
	if maxLine >= len(lines) {
		maxLine = len(lines) - 1
	}
	if maxLine < 0 {
		return
	}

	cursorOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	currentFunction := functionBlockAtLine(text, pos.Line)
	functionStart := 0
	functionEnd := len(text)
	if currentFunction != nil {
		functionStart = currentFunction.Start
		functionEnd = currentFunction.End
	}

	for lineIndex := 0; lineIndex < maxLine; lineIndex++ {
		lineOffset := offsetAtLine(text, lineIndex+1)
		if lineOffset < functionStart || lineOffset >= functionEnd {
			continue
		}

		rawLine := getLine(text, lineIndex)
		line := strings.TrimSpace(stripLineComment(rawLine))
		if !(strings.HasPrefix(line, "if ") || strings.HasPrefix(line, "if(")) {
			continue
		}

		braceInLine := strings.Index(rawLine, "{")
		openBrace := -1
		if braceInLine >= 0 {
			openBrace = lineOffset + braceInLine
		} else {
			searchEnd := cursorOffset
			if functionEnd < searchEnd {
				searchEnd = functionEnd
			}
			if found := strings.Index(text[lineOffset:searchEnd], "{"); found >= 0 {
				openBrace = lineOffset + found
			}
		}
		if openBrace < 0 {
			continue
		}

		closeBrace := findMatching(text, openBrace, '{', '}')
		if closeBrace < 0 || closeBrace >= cursorOffset {
			continue
		}

		if guardHasElseAfter(text, closeBrace, cursorOffset) {
			continue
		}

		body := text[openBrace+1 : closeBrace]
		if !guardBodyReturnsOrThrows(body) {
			continue
		}

		applyTypeNarrowing(scope, line, true)
	}
}

func guardBodyReturnsOrThrows(body string) bool {
	lines := strings.Split(body, "\n")
	for _, raw := range lines {
		line := strings.TrimSpace(stripLineComment(raw))
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "return") || strings.HasPrefix(line, "throw")
	}
	return false
}

func guardHasElseAfter(text string, closeBrace int, limit int) bool {
	if closeBrace < 0 || closeBrace >= len(text) {
		return false
	}
	if limit > len(text) {
		limit = len(text)
	}

	i := closeBrace + 1
	for i < limit {
		ch := text[i]
		if ch == ' ' || ch == '\t' || ch == '\r' || ch == '\n' {
			i++
			continue
		}
		if ch == '/' && i+1 < limit && text[i+1] == '/' {
			for i < limit && text[i] != '\n' {
				i++
			}
			continue
		}
		break
	}

	return i+4 <= limit && strings.HasPrefix(text[i:limit], "else") && isWordBoundaryAt(text, i, 4)
}

var nullCheckRegex = regexp.MustCompile(`^if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s*!=\s*(null)\s*\)?\s*\{?\s*$`)
var nullEqualsRegex = regexp.MustCompile(`^if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s*==\s*(null)\s*\)?\s*\{?\s*$`)
var truthyIdentRegex = regexp.MustCompile(`^if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)?\s*\{?\s*$`)
var falsyIdentRegex = regexp.MustCompile(`^if\s*\(?\s*!\s*([A-Za-z_][A-Za-z0-9_]*)\s*\)?\s*\{?\s*$`)
var typeOfRegex = regexp.MustCompile("^if\\s*\\(?\\s*typeof\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*==\\s*[\"'\u0060](string|number|bool|object|array|function)[\"'\u0060]\\s*\\)?\\s*\\{?\\s*$")
var typeOfNotRegex = regexp.MustCompile("^if\\s*\\(?\\s*typeof\\s+([A-Za-z_][A-Za-z0-9_]*)\\s*!=\\s*[\"'\u0060](string|number|bool|object|array|function)[\"'\u0060]\\s*\\)?\\s*\\{?\\s*$")
var instanceOfRegex = regexp.MustCompile(`^if\s*\(?\s*([A-Za-z_][A-Za-z0-9_]*)\s+instanceof\s+([A-Za-z_][A-Za-z0-9_]*(?:\.[A-Za-z_][A-Za-z0-9_]*)*)\s*\)?\s*\{?\s*$`)
var orWordRegex = regexp.MustCompile(`\bor\b`)
var andWordRegex = regexp.MustCompile(`\band\b`)

func applyTypeOfNarrowing(scope *Scope, name string, narrowedType string, invert bool) {
	if sym, ok := scope.Resolve(name); ok {
		if invert {
			narrowSymbolRemovingType(scope, name, narrowedType)
		} else {
			if narrowedType == "function" {
				hasFunction := false
				for _, part := range splitUnionType(sym.Type) {
					if strings.TrimSpace(part) == "function" {
						hasFunction = true
						break
					}
				}
				if hasFunction {
					sym.Type = "function"
					scope.Define(sym)
				}
			} else {
				sym.Type = narrowedType
				scope.Define(sym)
			}
		}
	}
}

func narrowSymbolRemovingType(scope *Scope, name string, typeToRemove string) {
	if sym, ok := scope.Resolve(name); ok {
		parts := splitUnionType(sym.Type)
		newParts := []string{}
		for _, part := range parts {
			if strings.TrimSpace(part) != typeToRemove {
				newParts = append(newParts, strings.TrimSpace(part))
			}
		}
		if len(newParts) > 0 && len(newParts) != len(parts) {
			sym.Type = strings.Join(newParts, " | ")
			scope.Define(sym)
		}
	}
}

func exprIdentComparedWithNull(expr Expr) (string, bool) {
	bin, ok := expr.(BinaryExpr)
	if !ok {
		return "", false
	}

	if ident, ok := bin.Left.(IdentExpr); ok {
		if _, isNull := bin.Right.(NullExpr); isNull {
			return ident.Name, true
		}
	}

	if ident, ok := bin.Right.(IdentExpr); ok {
		if _, isNull := bin.Left.(NullExpr); isNull {
			return ident.Name, true
		}
	}

	return "", false
}

func forceSymbolNull(scope *Scope, name string) {
	if sym, ok := scope.Resolve(name); ok {
		sym.Type = "null"
		scope.Define(sym)
	}
}

func applyExprTypeNarrowing(scope *Scope, expr Expr, invert bool) {
	switch e := expr.(type) {
	case BinaryExpr:
		switch e.Op {
		case TOKEN_OR:
			if invert {
				applyExprTypeNarrowing(scope, e.Left, true)
				applyExprTypeNarrowing(scope, e.Right, true)
			}
			return

		case TOKEN_AND:
			if !invert {
				applyExprTypeNarrowing(scope, e.Left, false)
				applyExprTypeNarrowing(scope, e.Right, false)
			}
			return

		case TOKEN_EQ, TOKEN_NEQ:
			name, ok := exprIdentComparedWithNull(e)
			if !ok {
				return
			}

			conditionMeansNull := e.Op == TOKEN_EQ
			if invert {
				conditionMeansNull = !conditionMeansNull
			}

			if conditionMeansNull {
				forceSymbolNull(scope, name)
			} else {
				narrowSymbolRemovingNull(scope, name)
			}
		}

	case UnaryExpr:
		if e.Op == TOKEN_BANG {
			applyExprTypeNarrowing(scope, e.Right, !invert)
		}

	case IdentExpr:
		if invert {
			forceSymbolNull(scope, e.Name)
		} else {
			narrowSymbolRemovingNull(scope, e.Name)
		}
	}
}

func applyTypeNarrowing(scope *Scope, ifLine string, invert bool) {
	cond := strings.TrimSpace(ifLine)
	if strings.HasPrefix(cond, "if ") || strings.HasPrefix(cond, "if(") {
		cond = strings.TrimPrefix(cond, "if")
		cond = strings.TrimSpace(cond)
		if strings.HasSuffix(cond, "{") {
			cond = strings.TrimSuffix(cond, "{")
			cond = strings.TrimSpace(cond)
		}
		for strings.HasPrefix(cond, "(") && strings.HasSuffix(cond, ")") {
			depth := 0
			outermostMatch := true
			for i, char := range cond {
				if char == '(' {
					depth++
				} else if char == ')' {
					depth--
					if depth == 0 && i < len(cond)-1 {
						outermostMatch = false
						break
					}
				}
			}
			if outermostMatch && depth == 0 {
				cond = cond[1 : len(cond)-1]
				cond = strings.TrimSpace(cond)
			} else {
				break
			}
		}

		if !invert {
			if andWordRegex.MatchString(cond) {
				if orWordRegex.MatchString(cond) {
					return
				}
				parts := andWordRegex.Split(cond, -1)
				for _, part := range parts {
					applyTypeNarrowing(scope, "if "+strings.TrimSpace(part), false)
				}
				return
			}
		} else {
			if orWordRegex.MatchString(cond) {
				if andWordRegex.MatchString(cond) {
					return
				}
				parts := orWordRegex.Split(cond, -1)
				for _, part := range parts {
					applyTypeNarrowing(scope, "if "+strings.TrimSpace(part), true)
				}
				return
			}
		}
	}

	if match := nullCheckRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		if invert {
			if sym, ok := scope.Resolve(name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		} else {
			narrowSymbolRemovingNull(scope, name)
		}
		return
	}

	if match := nullEqualsRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		if invert {
			narrowSymbolRemovingNull(scope, name)
		} else {
			if sym, ok := scope.Resolve(name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		}
		return
	}

	if match := typeOfNotRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		narrowedType := match[2]
		applyTypeOfNarrowing(scope, name, narrowedType, !invert)
		return
	}

	if match := typeOfRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		narrowedType := match[2]
		applyTypeOfNarrowing(scope, name, narrowedType, invert)
		return
	}

	if match := instanceOfRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		className := match[2]
		if invert {
			narrowSymbolRemovingType(scope, name, "class:"+className)
		} else {
			if sym, ok := scope.Resolve(name); ok {
				sym.Type = "class:" + className
				scope.Define(sym)
			}
		}
		return
	}

	if match := falsyIdentRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		if invert {
			narrowSymbolRemovingNull(scope, name)
		} else {
			if sym, ok := scope.Resolve(name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		}
		return
	}

	if match := truthyIdentRegex.FindStringSubmatch(ifLine); match != nil {
		name := match[1]
		if invert {
			if sym, ok := scope.Resolve(name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		} else {
			narrowSymbolRemovingNull(scope, name)
		}
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

func isOffsetInStringOrComment(text string, offset int) bool {
	if offset < 0 {
		offset = 0
	}
	if offset > len(text) {
		offset = len(text)
	}

	type parserState struct {
		isString bool
		quote    byte
		braces   int
	}

	stack := []parserState{{isString: false}}
	escaped := false
	inLineComment := false
	inBlockComment := false

	for i := 0; i < offset; i++ {
		ch := text[i]
		curr := &stack[len(stack)-1]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
			}
			continue
		}

		if inBlockComment {
			if ch == '*' && i+1 < offset && text[i+1] == '/' {
				inBlockComment = false
				i++
			}
			continue
		}

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
				stack = stack[:len(stack)-1]
				continue
			}
			if (curr.quote == '"' || curr.quote == '\'') && ch == '\n' {
				stack = stack[:len(stack)-1]
				continue
			}
			if curr.quote == '`' && ch == '$' && i+1 < offset && text[i+1] == '{' {
				stack = append(stack, parserState{isString: false, braces: 1})
				i++
				continue
			}
		} else {
			if ch == '/' && i+1 < offset && text[i+1] == '/' {
				inLineComment = true
				i++
				continue
			}
			if ch == '/' && i+1 < offset && text[i+1] == '*' {
				inBlockComment = true
				i++
				continue
			}
			if ch == '"' || ch == '\'' || ch == '`' {
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
						stack = stack[:len(stack)-1]
					}
				}
			}
		}
	}

	return inLineComment || inBlockComment || stack[len(stack)-1].isString
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
				stack = stack[:len(stack)-1]
				continue
			}
			if curr.quote == '`' && ch == '$' && i+1 < len(line) && line[i+1] == '{' {
				stack = append(stack, parserState{isString: false, braces: 1})
				i++
				continue
			}
		} else {
			if i+1 < len(line) && ch == '/' && line[i+1] == '/' {
				return true
			}
			if ch == '"' || ch == '\'' || ch == '`' {
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

func lspTypeStructBaseName(t LSPType) string {
	if t.Inner != nil {
		return lspTypeStructBaseName(*t.Inner)
	}
	return t.Name
}

func lspTypeStructArgs(t LSPType) []LSPType {
	if t.Inner != nil {
		return lspTypeStructArgs(*t.Inner)
	}
	return t.Args
}

func lspTypeStructPrefix(t LSPType) string {
	if t.Inner != nil {
		return t.Prefix + lspTypeStructPrefix(*t.Inner)
	}
	return t.Prefix
}

func isValidateSchemaClassLSPType(scope *Scope, typ string) bool {
	typ = strings.TrimSpace(typ)
	if typ == "" || strings.Contains(typ, "|") {
		return false
	}

	parsed, remaining := parseOneLSPType(scope, strings.Split(typ, ":"))
	if len(remaining) > 0 {
		for len(remaining) > 0 {
			var extra LSPType
			extra, remaining = parseOneLSPType(scope, remaining)
			if parsed.Inner != nil {
				parsed.Inner.Args = append(parsed.Inner.Args, extra)
			} else {
				parsed.Args = append(parsed.Args, extra)
			}
		}
	}

	if lspTypeStructPrefix(parsed) != "class:" {
		return false
	}

	base := lspTypeStructBaseName(parsed)
	return base == "Schema" || strings.HasSuffix(base, ".Schema")
}

func isObjectAssignableLSPType(scope *Scope, got string) bool {
	got = strings.TrimSpace(got)
	if got == "object" {
		return true
	}

	if isValidateSchemaClassLSPType(scope, got) {
		return false
	}

	return strings.HasPrefix(got, "class:") || strings.HasPrefix(got, "interface:")
}

func (a *astSemanticAnalyzer) compareGenericLSPTypeStruct(got LSPType, expected LSPType) bool {
	gotBase := lspTypeStructBaseName(got)
	expectedBase := lspTypeStructBaseName(expected)
	if gotBase == "" || expectedBase == "" || gotBase != expectedBase {
		return false
	}

	gotPrefix := lspTypeStructPrefix(got)
	expectedPrefix := lspTypeStructPrefix(expected)
	if gotPrefix != expectedPrefix {
		return false
	}

	gotArgs := lspTypeStructArgs(got)
	expectedArgs := lspTypeStructArgs(expected)
	if len(expectedArgs) == 0 {
		return len(gotArgs) == 0
	}
	if len(gotArgs) != len(expectedArgs) {
		return false
	}

	for i := range gotArgs {
		if !a.compareLSPTypeStruct(gotArgs[i], expectedArgs[i]) {
			return false
		}
	}
	return true
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
			if strings.HasPrefix(e, "function(") && g == "function" {
				matched = true
				break
			}
			if e == "function" && strings.HasPrefix(g, "function(") {
				matched = true
				break
			}
			if e == "any" || e == "unknown" || g == "any" || g == "unknown" {
				matched = true
				break
			}
			if g == e {
				matched = true
				break
			}
			if a.compareStructuredLSPType(g, e) {
				matched = true
				break
			}
			if e == "object" && isObjectAssignableLSPType(a.root, g) {
				matched = true
				break
			}
			if strings.HasPrefix(g, "enum:") && (e == "string" || e == "number") {
				matched = true
				break
			}
			if strings.HasPrefix(e, "enum:") && (g == "string" || g == "number") {
				matched = true
				break
			}
			if strings.HasPrefix(e, "interface:") && g == "object" {
				matched = true
				break
			}
			if strings.HasPrefix(e, "interface:") && strings.HasPrefix(g, "class:") {
				if a.classImplementsInterface(g, e) {
					matched = true
					break
				}
			}
			if strings.HasPrefix(e, "array:") && g == "array:any" {
				matched = true
				break
			} else if e == "array:any" && (strings.HasPrefix(g, "array:") || g == "array" || g == "array:any") {
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

func (a *astSemanticAnalyzer) classImplementsInterface(gotClass string, expectedInterface string) bool {
	classBase := stripLSPGenerics(gotClass)
	className := strings.TrimPrefix(classBase, "class:")

	interfaceBase := stripLSPGenerics(expectedInterface)

	classSym, ok := resolveClassSymbol(a.root, className)
	if !ok {
		return false
	}

	for _, imp := range classSym.Implements {
		impBase := stripLSPGenerics(imp)
		if impBase == interfaceBase {
			if strings.Contains(expectedInterface, ":") {
				if imp == expectedInterface {
					return true
				}
				continue
			}
			return true
		}
	}

	return false
}

func (a *astSemanticAnalyzer) compareStructuredLSPType(got string, expected string) bool {
	got = strings.TrimSpace(got)
	expected = strings.TrimSpace(expected)

	if got == "" || expected == "" {
		return false
	}

	gotParsed, gotRemaining := parseOneLSPType(a.root, strings.Split(got, ":"))
	if len(gotRemaining) > 0 {
		return false
	}

	expectedParsed, expectedRemaining := parseOneLSPType(a.root, strings.Split(expected, ":"))
	if len(expectedRemaining) > 0 {
		return false
	}

	return a.compareLSPTypeStruct(gotParsed, expectedParsed)
}

func (a *astSemanticAnalyzer) compareLSPTypeStruct(got LSPType, expected LSPType) bool {
	if got.Inner != nil || expected.Inner != nil {
		if got.Inner == nil || expected.Inner == nil {
			return false
		}
		if expected.Prefix == "any:" || expected.Prefix == "unknown:" {
			return true
		}
		if got.Prefix != expected.Prefix {
			return false
		}
		return a.compareLSPTypeStruct(*got.Inner, *expected.Inner)
	}

	if expected.Name == "any" || expected.Name == "unknown" || got.Name == "any" || got.Name == "unknown" {
		return true
	}

	if got.Prefix != expected.Prefix || got.Name != expected.Name {
		return false
	}

	if len(got.Args) != len(expected.Args) {
		return a.compareGenericLSPTypeStruct(got, expected)
	}

	for i := range got.Args {
		if !a.compareLSPTypeStruct(got.Args[i], expected.Args[i]) {
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

		expectedType := param.Type
		if param.Variadic {
			if strings.HasPrefix(expectedType, "array:") {
				expectedType = strings.TrimPrefix(expectedType, "array:")
			} else if expectedType == "array" {
				expectedType = "any"
			}
		}

		if expectedType == "" || expectedType == "any" {
			continue
		}

		if strings.HasPrefix(expectedType, "function(") || (strings.Contains(expectedType, "|") && isCallableLSPType(expectedType)) {
			fnType := expectedType
			if strings.Contains(expectedType, "|") {
				for _, part := range splitUnionType(expectedType) {
					if strings.HasPrefix(strings.TrimSpace(part), "function(") {
						fnType = strings.TrimSpace(part)
						break
					}
				}
			}
			if fnExpr, ok := arg.(FunctionExpr); ok {
				expectedParamTypes, hasSignature := callableFunctionParamTypes(fnType)
				if hasSignature {
					if len(fnExpr.Params) < len(expectedParamTypes) {
						msg := fmt.Sprintf("not enough parameters in callback for '%s': expected %d, got %d", param.Name, len(expectedParamTypes), len(fnExpr.Params))
						if r, ok := byteRangeForExpr(a.text, arg); ok {
							a.addErrorAtRange(r, msg)
						} else {
							a.addError(line, column, msg)
						}
					} else if len(fnExpr.Params) > len(expectedParamTypes) {
						msg := fmt.Sprintf("too many parameters in callback for '%s': expected %d, got %d", param.Name, len(expectedParamTypes), len(fnExpr.Params))
						if r, ok := byteRangeForExpr(a.text, arg); ok {
							a.addErrorAtRange(r, msg)
						} else {
							a.addError(line, column, msg)
						}
					}
				}
			}
		}

		oldContext := a.expectedTypeContext
		a.expectedTypeContext = expectedType
		argType := a.inferExprType(arg)
		a.expectedTypeContext = oldContext

		a.validateObjectExprAgainstType(arg, expectedType, line, column)

		if argType == "any" || argType == "unknown" {
			continue
		}

		if !a.compareLSPTypes(argType, expectedType) {
			msg := fmt.Sprintf("cannot pass type '%s' to parameter '%s' of function '%s' (expected '%s')", argType, param.Name, name, displayLSPType(expectedType))
			if r, ok := byteRangeForExpr(a.text, arg); ok {
				a.addErrorAtRange(r, msg)
			} else {
				a.addError(line, column, msg)
			}
		}
	}
}

func displayLSPType(typ string) string {
	typ = strings.TrimSpace(typ)
	typ = strings.TrimPrefix(typ, "class:")
	typ = strings.TrimPrefix(typ, "interface:")
	typ = strings.TrimPrefix(typ, "enum:")
	return typ
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

func findNameBefore(text string, name string, beforeLine int, startLine int) (int, int) {
	if name == "" || beforeLine <= 1 {
		return 0, 0
	}
	if startLine < 1 {
		startLine = 1
	}
	lines := strings.Split(text, "\n")
	for lineIdx := startLine - 1; lineIdx < beforeLine-1 && lineIdx < len(lines); lineIdx++ {
		code := stripLineComment(lines[lineIdx])
		re := regexp.MustCompile(`\b` + regexp.QuoteMeta(name) + `\b`)
		if match := re.FindStringIndex(code); match != nil {
			return lineIdx + 1, match[0] + 1
		}
	}
	return 0, 0
}

func innermostBlockStartLine(text string, lineIndex int) int {
	lines := strings.Split(text, "\n")
	startLines := []int{1}
	depth := 0
	for i := 0; i <= lineIndex && i < len(lines); i++ {
		code := stripLineComment(lines[i])
		inString := byte(0)
		escaped := false
		for j := 0; j < len(code); j++ {
			ch := code[j]
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
			if ch == '{' {
				depth++
				startLines = append(startLines, i+1)
			} else if ch == '}' {
				depth--
				if len(startLines) > 1 {
					startLines = startLines[:len(startLines)-1]
				}
			}
		}
	}
	return startLines[len(startLines)-1]
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
		if offset >= block.Start && offset <= block.End {
			return block.Name
		}
	}
	return ""
}

func enumNameAtPosition(text string, pos Position) string {
	offset := offsetAtLine(text, pos.Line+1)
	for _, block := range findBlocks(text, "enum") {
		if offset >= block.Start && offset <= block.End {
			return block.Name
		}
	}
	return ""
}

func getImplementations(uri string, text string, pos Position) []Location {
	posOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	if isOffsetInStringOrComment(text, posOffset) {
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
	posOffset := offsetAtLine(text, pos.Line+1) + pos.Character
	if isOffsetInStringOrComment(text, posOffset) {
		return nil
	}

	word := wordAtPosition(text, pos)
	if word == "" || tinyKeywords[word] {
		return nil
	}

	scope := scopeAtPosition(uri, text, pos)

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
						Kind:           12,
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
						Kind:           7,
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
							Kind:           6,
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
					Kind:           6,
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
			Kind:           12,
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
							Kind:           6,
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
							Kind:           12,
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
								Kind:           6,
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
					Kind:           12,
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
	FrameDestructure
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
			isBlock := isBlockOpening(text, i)
			if !isBlock && len(stack) > 0 && stack[len(stack)-1].Kind == FrameBlock {
				isBlock = isCaseBodyBrace(text, i)
			}
			if isBlock {
				stack = append(stack, ParseFrame{Kind: FrameBlock})
			} else {
				var typ string
				var sym SymbolInfo
				var symbols []SymbolInfo
				var exists bool

				isTopLevelDestructure := false
				if len(stack) == 0 {
					k := i - 1
					for k >= 0 && (text[k] == ' ' || text[k] == '\t' || text[k] == '\r' || text[k] == '\n') {
						k--
					}
					if k >= 3 && strings.HasSuffix(text[:k+1], "let") {
						isTopLevelDestructure = true
					} else if k >= 5 && strings.HasSuffix(text[:k+1], "const") {
						isTopLevelDestructure = true
					}
				}

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
					} else if parent.Kind == FrameDestructure {
						if parent.CurrentKey != "" {
							typ, sym, exists = resolveObjectFieldType(scope, parent.Symbol, parent.CurrentKey)
						}
					} else if parent.Kind == FrameBlock {
						typ, exists = findObjectTypeHintAtOffset(text, i, scope)
						if exists {
							sym, exists = resolveTypeSymbol(scope, typ)
						}
					}
				} else if isTopLevelDestructure {
					rhsTyp := findDestructureRhsType(text, i, scope)
					if rhsTyp != "" {
						typ = rhsTyp
						sym, exists = resolveTypeSymbol(scope, typ)
						exists = true
					}
				} else {
					typ, exists = findObjectTypeHintAtOffset(text, i, scope)
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

				if isTopLevelDestructure {
					stack = append(stack, ParseFrame{
						Kind:    FrameDestructure,
						Type:    typ,
						Symbol:  sym,
						Symbols: symbols,
					})
				} else if len(stack) > 0 {
					parent := &stack[len(stack)-1]
					if parent.Kind == FrameDestructure && parent.CurrentKey != "" {
						stack = append(stack, ParseFrame{
							Kind:    FrameDestructure,
							Type:    typ,
							Symbol:  sym,
							Symbols: symbols,
						})
					} else {
						stack = append(stack, ParseFrame{
							Kind:    FrameObject,
							Type:    typ,
							Symbol:  sym,
							Symbols: symbols,
						})
					}
				} else {
					stack = append(stack, ParseFrame{
						Kind:    FrameObject,
						Type:    typ,
						Symbol:  sym,
						Symbols: symbols,
					})
				}
			}

		case '}':
			for len(stack) > 0 {
				top := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				if top.Kind == FrameBlock || top.Kind == FrameObject || top.Kind == FrameDestructure {
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
				} else if top.Kind == FrameObject || top.Kind == FrameDestructure {
					top.CurrentKey = ""
				}
			}

		case ':':
			if len(stack) > 0 {
				top := &stack[len(stack)-1]
				if top.Kind == FrameObject || top.Kind == FrameDestructure {
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
		if top.Kind == FrameObject || top.Kind == FrameDestructure {
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

func findDestructureRhsType(text string, openingBrace int, scope *Scope) string {
	lineStart := strings.LastIndex(text[:openingBrace], "\n")
	if lineStart < 0 {
		lineStart = 0
	} else {
		lineStart++
	}
	line := text[lineStart:openingBrace]

	match := destructuringObjectRegex.FindStringSubmatch(line)
	if match == nil {
		match = destructuringArrayRegex.FindStringSubmatch(line)
	}
	if match != nil {
		exprText := strings.TrimSpace(match[2])
		typ := inferExprTypeFromText(scope, exprText)
		return normalizeLSPType(scope, typ)
	}

	return ""
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

func findEnclosingFunctionReturnType(text string, offset int, scope *Scope) (string, bool) {
	braceDepth := 0
	parenDepth := 0
	i := offset - 1
	for i >= 0 {
		ch := text[i]
		if ch == '}' {
			braceDepth++
		} else if ch == '{' {
			braceDepth--
		} else if ch == ')' {
			parenDepth++
		} else if ch == '(' {
			parenDepth--
		} else if isIdentByte(ch) && braceDepth == -1 && parenDepth == 0 {
			// Extract the word
			end := i + 1
			for i >= 0 && isIdentByte(text[i]) {
				i--
			}
			word := text[i+1 : end]
			if word == "fn" {
				j := end
				for j < len(text) && (text[j] == ' ' || text[j] == '\t' || text[j] == '\n' || text[j] == '\r') {
					j++
				}
				if j < len(text) && isIdentByte(text[j]) {
					endJ := j
					for endJ < len(text) && isIdentByte(text[endJ]) {
						endJ++
					}
					fnName := text[j:endJ]
					sym, ok := scope.Resolve(fnName)
					if ok {
						return sym.Returns, true
					}
					className := classNameAtPosition(text, bytePositionAtOffset(text, offset))
					if className != "" {
						if classSym, ok := resolveClassSymbol(scope, className); ok {
							if methodSym, ok := classSym.Methods[fnName]; ok {
								return methodSym.Returns, true
							}
						}
					}
				}
			}
			continue
		}
		i--
	}
	return "", false
}

func findVariableTypeHintBefore(text string, offset int) (string, bool) {
	i := offset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i < 0 || text[i] != '=' {
		return "", false
	}
	i--
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	endType := i + 1
	hasColon := false
	for i >= 0 {
		ch := text[i]
		if ch == ':' {
			hasColon = true
			break
		}
		if !isFunctionReturnTypeByte(ch) && ch != ' ' && ch != '\t' && ch != '\r' && ch != '\n' {
			break
		}
		i--
	}
	if !hasColon {
		return "", false
	}
	typeStr := strings.TrimSpace(text[i+1 : endType])
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i >= 0 && isIdentByte(text[i]) {
		for i >= 0 && isIdentByte(text[i]) {
			i--
		}
		for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
			i--
		}
		if i >= 0 {
			endWord := i + 1
			for i >= 0 && isIdentByte(text[i]) {
				i--
			}
			word := text[i+1 : endWord]
			if word == "let" || word == "const" || word == "field" {
				return typeStr, true
			}
		}
	}
	return "", false
}

func findObjectTypeHintAtOffset(text string, offset int, scope *Scope) (string, bool) {
	// 1. Try to find a return statement context
	i := offset - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i >= 5 {
		endWord := i + 1
		for i >= 0 && isIdentByte(text[i]) {
			i--
		}
		word := text[i+1 : endWord]
		if word == "return" {
			if retType, ok := findEnclosingFunctionReturnType(text, offset, scope); ok {
				return retType, true
			}
		}
	}

	// 2. Try to find variable assignment type hint
	if typ, ok := findVariableTypeHintBefore(text, offset); ok {
		return typ, true
	}

	// 3. Original line-based fallback
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
			if typ, ok := typeHintBeforeAssignInLine(line); ok {
				return typ, true
			}
		}
	}
	return "", false
}

func typeHintBeforeAssignInLine(line string) (string, bool) {
	assign := findTopLevelByte(line, 0, '=')
	if assign < 0 {
		return "", false
	}
	colon := -1
	depthParen, depthBrace, depthBracket := 0, 0, 0
	for i := 0; i < assign; i++ {
		switch line[i] {
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
		case ':':
			if colon < 0 && depthParen == 0 && depthBrace == 0 && depthBracket == 0 {
				colon = i
			}
		}
	}
	if colon < 0 {
		return "", false
	}
	typ := strings.TrimSpace(line[colon+1 : assign])
	return typ, typ != ""
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
				break
			}
		}
		if narrowedSym != nil {
			activeSyms = []SymbolInfo{*narrowedSym}
		}
	}

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

func findCommentStartOnLine(text string, lineStart, lineEnd int) int {
	inStr := byte(0)
	escaped := false
	i := lineStart
	for i < lineEnd {
		ch := text[i]
		if inStr != 0 {
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
			if ch == inStr {
				inStr = 0
			}
			i++
			continue
		}
		if ch == '"' || ch == '\'' || ch == '`' {
			inStr = ch
			i++
			continue
		}
		if i+1 < lineEnd && ch == '/' && text[i+1] == '/' {
			return i
		}
		i++
	}
	return -1
}

func isBlockOpening(text string, braceOffset int) bool {
	i := braceOffset - 1

	for i >= 0 && (text[i] == ' ' || text[i] == '\t' || text[i] == '\r' || text[i] == '\n') {
		i--
	}
	if i >= 0 && isIdentByte(text[i]) {
		end := i + 1
		for i >= 0 && isIdentByte(text[i]) {
			i--
		}
		if text[i+1:end] == "return" {
			return false
		}
		i = braceOffset - 1
	}

	braceDepth := 0
	parenDepth := 0
	bracketDepth := 0
	hasSemicolon := false

	for i >= 0 {
		// Find line boundaries for the current character
		lineStart := i
		for lineStart > 0 && text[lineStart-1] != '\n' {
			lineStart--
		}
		lineEnd := i
		for lineEnd < len(text) && text[lineEnd] != '\n' && text[lineEnd] != '\r' {
			lineEnd++
		}

		commentIdx := findCommentStartOnLine(text, lineStart, lineEnd)
		if commentIdx != -1 && i >= commentIdx {
			i = commentIdx - 1
			continue
		}

		ch := text[i]

		// Handle strings
		if ch == '"' || ch == '\'' || ch == '`' {
			quote := ch
			i--
			for i >= 0 {
				if text[i] == quote {
					slashCount := 0
					for k := i - 1; k >= 0 && text[k] == '\\'; k-- {
						slashCount++
					}
					if slashCount%2 == 0 {
						break
					}
				}
				i--
			}
			i--
			continue
		}

		// Handle nesting
		if ch == '}' {
			if braceDepth == 0 {
				// Hit a closing brace of a previous/sibling block, stop.
				return false
			}
			braceDepth++
		} else if ch == '{' {
			if braceDepth == 0 {
				// Hit the parent block opening brace, stop.
				return false
			}
			braceDepth--
		} else if ch == ')' {
			parenDepth++
		} else if ch == '(' {
			if parenDepth == 0 {
				// Underflow parenthesis
			} else {
				parenDepth--
			}
		} else if ch == ']' {
			bracketDepth++
		} else if ch == '[' {
			if bracketDepth == 0 {
				// Underflow
			} else {
				bracketDepth--
			}
		} else if ch == ';' {
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				hasSemicolon = true
			}
		} else if isIdentByte(ch) {
			if braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				// Extract the full word
				end := i + 1
				for i >= 0 && isIdentByte(text[i]) {
					i--
				}
				word := text[i+1 : end]
				if isBlockKeyword(word) || word == "implements" {
					if word == "for" || !hasSemicolon {
						return true
					}
				}
				continue
			}
		}

		i--
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

func isCaseBodyBrace(text string, braceOffset int) bool {
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
	if text[i] == '=' {
		return false
	}
	if isIdentByte(text[i]) {
		end := i + 1
		for i >= 0 && isIdentByte(text[i]) {
			i--
		}
		word := text[i+1 : end]
		switch word {
		case "let", "const", "fn", "class", "if", "else", "for", "while", "return", "import", "throw", "try", "catch", "finally", "match":
			return false
		}
		return true
	}

	for i >= 0 && !isIdentByte(text[i]) && text[i] != ')' && text[i] != '\'' && text[i] != '"' && text[i] != '`' {
		i--
	}
	if i < 0 {
		return false
	}
	if !isIdentByte(text[i]) {
		return false
	}
	end := i + 1
	for i >= 0 && isIdentByte(text[i]) {
		i--
	}
	word := text[i+1 : end]
	if word == "if" {
		j := i
		for j >= 0 && (text[j] == ' ' || text[j] == '\t' || text[j] == '\r' || text[j] == '\n') {
			j--
		}
		if j >= 0 && (isIdentByte(text[j]) || text[j] == ')') {
			return true
		}
	}

	return false
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
	case "class", "fn", "if", "while", "for", "else", "catch", "interface", "enum", "try", "finally", "match", "lock":
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

func alwaysExits(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case ReturnStmt, BreakStmt, ContinueStmt, ThrowStmt:
		return true
	case IfStmt:
		if bExpr, ok := s.Condition.(BoolExpr); ok {
			if bExpr.Value {
				return alwaysExitsBlock(s.ThenBody)
			}
			return alwaysExitsBlock(s.ElseBody)
		}
		return len(s.ElseBody) > 0 && alwaysExitsBlock(s.ThenBody) && alwaysExitsBlock(s.ElseBody)
	case TryCatchStmt:
		if alwaysExitsBlock(s.FinallyBody) {
			return true
		}
		return alwaysExitsBlock(s.TryBody) && alwaysExitsBlock(s.CatchBody)
	case MatchStmt:
		if len(s.Default) == 0 || !alwaysExitsBlock(s.Default) {
			return false
		}
		for _, c := range s.Cases {
			if !alwaysExitsBlock(c.Body) {
				return false
			}
		}
		return true
	case NamespaceStmt:
		return alwaysExitsBlock(s.Statements)
	}
	return false
}

func alwaysExitsBlock(stmts []Stmt) bool {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		if alwaysExits(stmt) {
			return true
		}
	}
	return false
}

func alwaysReturnsOrThrows(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case ReturnStmt, ThrowStmt:
		return true
	case IfStmt:
		if bExpr, ok := s.Condition.(BoolExpr); ok {
			if bExpr.Value {
				return alwaysReturnsOrThrowsBlock(s.ThenBody)
			}
			return alwaysReturnsOrThrowsBlock(s.ElseBody)
		}
		return len(s.ElseBody) > 0 && alwaysReturnsOrThrowsBlock(s.ThenBody) && alwaysReturnsOrThrowsBlock(s.ElseBody)
	case TryCatchStmt:
		if alwaysReturnsOrThrowsBlock(s.FinallyBody) {
			return true
		}
		return alwaysReturnsOrThrowsBlock(s.TryBody) && alwaysReturnsOrThrowsBlock(s.CatchBody)
	case MatchStmt:
		if len(s.Default) == 0 || !alwaysReturnsOrThrowsBlock(s.Default) {
			return false
		}
		for _, c := range s.Cases {
			if !alwaysReturnsOrThrowsBlock(c.Body) {
				return false
			}
		}
		return true
	case NamespaceStmt:
		return alwaysReturnsOrThrowsBlock(s.Statements)
	case WhileStmt:
		if bExpr, ok := s.Condition.(BoolExpr); ok && bExpr.Value {
			if alwaysReturnsOrThrowsBlock(s.Body) && !containsBreakBlock(s.Body) {
				return true
			}
		}
	}
	return false
}

func alwaysReturnsOrThrowsBlock(stmts []Stmt) bool {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		if alwaysReturnsOrThrows(stmt) {
			return true
		}
	}
	return false
}

func containsBreak(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case BreakStmt:
		return true
	case IfStmt:
		return containsBreakBlock(s.ThenBody) || containsBreakBlock(s.ElseBody)
	case TryCatchStmt:
		return containsBreakBlock(s.TryBody) || containsBreakBlock(s.CatchBody) || containsBreakBlock(s.FinallyBody)
	case MatchStmt:
		if containsBreakBlock(s.Default) {
			return true
		}
		for _, c := range s.Cases {
			if containsBreakBlock(c.Body) {
				return true
			}
		}
		return false
	case NamespaceStmt:
		return containsBreakBlock(s.Statements)
	}
	return false
}

func containsBreakBlock(stmts []Stmt) bool {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		if containsBreak(stmt) {
			return true
		}
	}
	return false
}

func nodePosition(node any) (int, int) {
	if node == nil {
		return 0, 0
	}
	switch n := node.(type) {
	case Stmt:
		switch s := n.(type) {
		case AssignStmt:
			return s.Line, s.Column
		case NamespaceStmt:
			return s.Line, s.Column
		case EnumStmt:
			return s.Line, s.Column
		case ExportStmt:
			return s.Line, s.Column
		case ForStmt:
			return s.Line, s.Column
		case PropertyAssignStmt:
			return s.Line, s.Column
		case ImportStmt:
			return s.Line, s.Column
		case VariableStmt:
			return s.Line, s.Column
		case DestructureStmt:
			return s.Line, s.Column
		case ForInStmt:
			return s.Line, s.Column
		case MatchStmt:
			return s.Line, s.Column
		case ClassStmt:
			return s.Line, s.Column
		case FieldStmt:
			return s.Line, s.Column
		case WhileStmt:
			return s.Line, s.Column
		case IfStmt:
			return s.Line, s.Column
		case LockStmt:
			return s.Line, s.Column
		case ExprStmt:
			return nodePosition(s.Value)
		case ThrowStmt:
			return s.Line, s.Column
		case InterfaceStmt:
			return s.Line, s.Column
		case EmbedStmt:
			return s.Line, s.Column
		case IndexAssignStmt:
			return s.Line, s.Column
		case FunctionStmt:
			return s.Line, s.Column
		case NativeFnStmt:
			return s.Line, s.Column
		case TryCatchStmt:
			return s.Line, s.Column
		case ReturnStmt:
			return s.Line, s.Column
		case IncrementStmt:
			return s.Line, s.Column
		case DecrementStmt:
			return s.Line, s.Column
		}
	case Expr:
		switch e := n.(type) {
		case NumberExpr:
			return e.Line, e.Column
		case IdentExpr:
			return e.Line, e.Column
		case CallExpr:
			return e.Line, e.Column
		case CallValueExpr:
			line, column := nodePosition(e.Callee)
			if line > 0 && column > 0 {
				return line, column
			}
			return e.Line, e.Column
		case MemberCallExpr:
			return e.Line, e.Column
		case ObjectInExpr:
			return e.Line, e.Column
		case AwaitExpr:
			return e.Line, e.Column
		case DeferExpr:
			return e.Line, e.Column
		case ThisExpr:
			return e.Line, e.Column
		case FunctionExpr:
			return e.Line, e.Column
		case PropertyExpr:
			return e.Line, e.Column
		}
	}
	return 0, 0
}

func (a *astSemanticAnalyzer) validateObjectExprAgainstType(expr Expr, expectedType string, fallbackLine, fallbackCol int) {
	objExpr, ok := expr.(ObjectExpr)
	if !ok {
		return
	}

	expectedType = strings.TrimSpace(expectedType)
	var expectedSym SymbolInfo
	var okSym bool

	if strings.HasPrefix(expectedType, "interface:") {
		ifaceName := strings.TrimPrefix(expectedType, "interface:")
		expectedSym, okSym = resolveInterfaceSymbol(a.root, ifaceName)
	} else if strings.HasPrefix(expectedType, "class:") {
		className := strings.TrimPrefix(expectedType, "class:")
		expectedSym, okSym = resolveClassSymbol(a.root, className)
	}

	if !okSym {
		return
	}

	expectedMembers := make(map[string]SymbolInfo)
	for name, field := range expectedSym.Fields {
		expectedMembers[name] = field
	}
	for name, method := range expectedSym.Methods {
		methodType := "function"
		if len(method.Params) > 0 {
			paramTypes := make([]string, len(method.Params))
			for idx, p := range method.Params {
				paramTypes[idx] = p.Type
			}
			methodType = "function(" + strings.Join(paramTypes, ", ") + ")"
		}
		expectedMembers[name] = SymbolInfo{
			Name: name,
			Type: methodType,
		}
	}

	objFields := make(map[string]ObjectField)
	for _, f := range objExpr.Fields {
		objFields[f.Name] = f
	}

	line, col := fallbackLine, fallbackCol
	if len(objExpr.Fields) > 0 {
		if l, c := nodePosition(objExpr.Fields[0].Value); l > 0 && c > 0 {
			line, col = l, c
		}
	}

	for memberName, expectedField := range expectedMembers {
		objField, exists := objFields[memberName]
		if !exists {
			a.addDiagnostic(line, col, fmt.Sprintf("object literal is missing property '%s' from '%s'", memberName, stripLSPPrefix(expectedType)))
			continue
		}

		oldContext := a.expectedTypeContext
		a.expectedTypeContext = expectedField.Type
		fieldType := a.inferExprType(objField.Value)
		a.expectedTypeContext = oldContext

		fieldType = normalizeLSPType(a.root, fieldType)
		if !a.compareLSPTypes(fieldType, expectedField.Type) {
			msg := fmt.Sprintf("type mismatch for property '%s': expected '%s', got '%s'", memberName, stripLSPPrefix(expectedField.Type), stripLSPPrefix(fieldType))
			if r, ok := byteRangeForExpr(a.text, objField.Value); ok {
				a.addDiagnosticAtRange(r, msg)
			} else {
				fieldLine, fieldCol := line, col
				if l, c := nodePosition(objField.Value); l > 0 && c > 0 {
					fieldLine, fieldCol = l, c
				}
				a.addDiagnostic(fieldLine, fieldCol, msg)
			}
		}
	}
}
