package main

import (
	"fmt"
	"strings"
	"sync"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type LSPDiagnostic struct {
	Line    int
	Column  int
	Message string
	Kind    string
}

var (
	lspFileBlocksMu sync.RWMutex
	lspFileBlocks   = make(map[string]lspParsedBlockCacheEntry)
)

type lspParsedBlockCacheEntry struct {
	cacheKey string
	blocks   []blockInfo
	text     string
}

type lspParseCacheEntry struct {
	statements  []Stmt
	diagnostics []LSPDiagnostic
}

var (
	lspParseCacheMu sync.RWMutex
	lspParseCache   = make(map[string]lspParseCacheEntry)
)

func lspParseCacheKey(uri string, text string) string {
	return lspTextCacheKey(filepathOrURIKey(uri), text)
}

func filepathOrURIKey(uri string) string {
	if uri == "" {
		return "inline"
	}
	return uri
}

func clearLSPParseCachesForURI(uri string) {
	lspParseCacheMu.Lock()
	defer lspParseCacheMu.Unlock()

	prefix := filepathOrURIKey(uri) + ":"
	for key := range lspParseCache {
		if strings.HasPrefix(key, prefix) {
			delete(lspParseCache, key)
		}
	}

	lspFileBlocksMu.Lock()
	defer lspFileBlocksMu.Unlock()
	delete(lspFileBlocks, uri)
}

func clearAllLSPParseCaches() {
	lspParseCacheMu.Lock()
	lspParseCache = make(map[string]lspParseCacheEntry)
	lspParseCacheMu.Unlock()

	lspFileBlocksMu.Lock()
	lspFileBlocks = make(map[string]lspParsedBlockCacheEntry)
	lspFileBlocksMu.Unlock()
}

func storeLSPParseCache(cacheKey string, statements []Stmt, diagnostics []LSPDiagnostic) {
	lspParseCacheMu.Lock()
	defer lspParseCacheMu.Unlock()

	const maxLSPParseCacheEntries = 256
	if len(lspParseCache) >= maxLSPParseCacheEntries {
		lspParseCache = make(map[string]lspParseCacheEntry)
	}
	lspParseCache[cacheKey] = lspParseCacheEntry{
		statements:  statements,
		diagnostics: diagnostics,
	}
}

func getCachedBlocks(uri string, text string) []blockInfo {
	lspFileBlocksMu.RLock()
	defer lspFileBlocksMu.RUnlock()

	cacheKey := lspTextCacheKey(filepathOrURIKey(uri), text)
	if uri != "" {
		if entry, ok := lspFileBlocks[uri]; ok && entry.cacheKey == cacheKey {
			return entry.blocks
		}
	}
	if entry, ok := lspFileBlocks[text]; ok && entry.text == text {
		return entry.blocks
	}
	return nil
}

func cacheBlocks(uri string, text string, blocks []blockInfo) {
	lspFileBlocksMu.Lock()
	defer lspFileBlocksMu.Unlock()

	if uri != "" {
		lspFileBlocks[uri] = lspParsedBlockCacheEntry{cacheKey: lspTextCacheKey(filepathOrURIKey(uri), text), blocks: blocks}
	}
	lspFileBlocks[text] = lspParsedBlockCacheEntry{text: text, blocks: blocks}
}

func parseTinyForLSP(uri string, text string) (statements []Stmt, diagnostics []LSPDiagnostic) {
	cacheKey := lspParseCacheKey(uri, text)
	lspParseCacheMu.RLock()
	if cached, ok := lspParseCache[cacheKey]; ok {
		lspParseCacheMu.RUnlock()
		return cached.statements, cached.diagnostics
	}
	lspParseCacheMu.RUnlock()

	defer func() {
		if r := recover(); r != nil {
			diagnostics = append(diagnostics, diagnosticFromRecover(r))
			statements = nil
		}
		storeLSPParseCache(cacheKey, statements, diagnostics)
	}()

	lexer := NewLexer(text, uri)
	parser := NewParser(lexer)
	program := parser.ParseProgramTolerant()

	blocks := blockInfosFromParsedBlocks(text, parser.Blocks)
	populateASTParams(blocks, program.Statements)
	cacheBlocks(uri, text, blocks)

	for _, d := range parser.Diagnostics {
		diagnostics = append(diagnostics, LSPDiagnostic{
			Line:    maxInt(0, d.Line-1),
			Column:  maxInt(0, d.Column-1),
			Message: d.Message,
			Kind:    string(d.Kind),
		})
	}

	return program.Statements, diagnostics
}

func populateASTParams(blocks []blockInfo, stmts []Stmt) {
	astFuncs := map[[2]int][]Param{}

	var collect func(stmts []Stmt)
	collect = func(stmts []Stmt) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			switch s := stmt.(type) {
			case FunctionStmt:
				key := [2]int{s.Line, s.Column}
				astFuncs[key] = s.Params
				collect(s.Body)
			case IfStmt:
				collect(s.ThenBody)
				collect(s.ElseBody)
			case ForStmt:
				collect(s.Body)
			case ForInStmt:
				collect(s.Body)
			case WhileStmt:
				collect(s.Body)
			case TryCatchStmt:
				collect(s.TryBody)
				collect(s.CatchBody)
				collect(s.FinallyBody)
			case MatchStmt:
				for _, c := range s.Cases {
					collect(c.Body)
				}
			}
		}
	}
	collect(stmts)

	for i := range blocks {
		b := &blocks[i]
		if b.Kind != "fn" {
			continue
		}
		key := [2]int{b.Line, b.Column}
		if params, ok := astFuncs[key]; ok {
			b.ASTParams = params
		}
	}
}

func offsetFromLineCol(text string, line int, col int) int {
	lines := strings.Split(text, "\n")
	offset := 0
	for i := 0; i < line-1; i++ {
		if i < len(lines) {
			offset += len(lines[i]) + 1
		}
	}
	if line-1 >= 0 && line-1 < len(lines) {
		c := col - 1
		if c < 0 {
			c = 0
		}
		if c > len(lines[line-1]) {
			c = len(lines[line-1])
		}
		offset += c
	}
	if offset < 0 {
		return 0
	}
	if offset > len(text) {
		return len(text)
	}
	return offset
}

func blockInfosFromParsedBlocks(text string, parsedBlocks []ParsedBlockInfo) []blockInfo {
	blocks := make([]blockInfo, len(parsedBlocks))
	for i, pb := range parsedBlocks {
		startOffset := offsetFromLineCol(text, pb.StartLine, pb.StartCol)
		endOffset := len(text)
		if pb.EndLine > 0 {
			endOffset = offsetFromLineCol(text, pb.EndLine, pb.EndCol)
		}

		var paramsText string
		if pb.LparenLine > 0 {
			pStart := offsetFromLineCol(text, pb.LparenLine, pb.LparenCol) + 1
			pEnd := offsetFromLineCol(text, pb.RparenLine, pb.RparenCol)
			if pStart >= 0 && pEnd >= pStart && pEnd <= len(text) {
				paramsText = text[pStart:pEnd]
			}
		}

		var returnType string
		if pb.RparenLine > 0 && pb.LbraceLine > 0 {
			rStart := offsetFromLineCol(text, pb.RparenLine, pb.RparenCol) + 1
			rEnd := offsetFromLineCol(text, pb.LbraceLine, pb.LbraceCol)
			if rStart >= 0 && rEnd >= rStart && rEnd <= len(text) {
				rawRet := strings.TrimSpace(text[rStart:rEnd])
				if strings.HasPrefix(rawRet, ":") {
					rawRet = strings.TrimSpace(rawRet[1:])
				}
				returnType = rawRet
			}
		}

		var body string
		var header string
		if pb.LbraceLine > 0 {
			lbrOffset := offsetFromLineCol(text, pb.LbraceLine, pb.LbraceCol)
			rbrOffset := len(text)
			if pb.RbraceLine > 0 {
				rbrOffset = offsetFromLineCol(text, pb.RbraceLine, pb.RbraceCol)
			}
			if lbrOffset >= 0 && rbrOffset >= lbrOffset && rbrOffset <= len(text) {
				body = text[lbrOffset+1 : rbrOffset]
			}
			if lbrOffset >= startOffset && lbrOffset <= len(text) {
				header = text[startOffset:lbrOffset]
			}
		}

		blocks[i] = blockInfo{
			Kind:           pb.Kind,
			Name:           pb.Name,
			ParamsText:     paramsText,
			ReturnType:     returnType,
			Body:           body,
			Header:         header,
			Start:          startOffset,
			End:            endOffset,
			Line:           pb.StartLine,
			Column:         pb.StartCol,
			IsAsync:        pb.IsAsync,
			TypeParameters: pb.TypeParameters,
		}
	}
	return blocks
}

func diagnosticFromRecover(r any) LSPDiagnostic {
	switch err := r.(type) {
	case LangErrorType:
		return LSPDiagnostic{
			Line:    maxInt(0, err.Line-1),
			Column:  maxInt(0, err.Column-1),
			Message: err.Message,
			Kind:    string(err.Kind),
		}

	case *LangErrorType:
		return LSPDiagnostic{
			Line:    maxInt(0, err.Line-1),
			Column:  maxInt(0, err.Column-1),
			Message: err.Message,
			Kind:    string(err.Kind),
		}

	case error:
		return LSPDiagnostic{
			Line:    0,
			Column:  0,
			Message: err.Error(),
			Kind:    "Error",
		}

	default:
		return LSPDiagnostic{
			Line:    0,
			Column:  0,
			Message: fmt.Sprint(r),
			Kind:    "Error",
		}
	}
}

func maxInt(a int, b int) int {
	if a > b {
		return a
	}

	return b
}
