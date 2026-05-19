package main

import (
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type Loader struct {
	states map[string]ImportState
	stack  []string
	cache  map[string][]Stmt
}

func (l *Loader) loadFile(path string) []Stmt {
	absPath, err := filepath.Abs(path)
	if err != nil {
		LangError(ErrorImport, "%v", err)
	}

	absPath = filepath.Clean(absPath)

	state := l.states[absPath]

	if state == ImportLoading {
		cycle := l.formatImportCycle(absPath)
		LangError(ErrorImport, "circular import detected: %s", cycle)
	}

	if state == ImportLoaded {
		return l.cache[absPath]
	}

	l.states[absPath] = ImportLoading
	l.stack = append(l.stack, absPath)

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		LangError(ErrorImport, "failed to read file %s: %v", path, err)
	}

	lexer := NewLexer(string(bytes), absPath)
	parser := NewParser(lexer)
	program := parser.ParseProgram()

	var result []Stmt
	dir := filepath.Dir(absPath)

	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case ImportStmt:
			if s.Std {
				result = append(result, s)
				continue
			}

			importPath := filepath.Join(dir, s.Path)
			importedStatements := l.loadFile(importPath)

			if s.Alias != "" {
				result = append(result, NamespaceStmt{
					Name:       s.Alias,
					Statements: importedStatements,
				})
				continue
			}

			result = append(result, importedStatements...)

		default:
			result = append(result, stmt)
		}
	}

	l.stack = l.stack[:len(l.stack)-1]
	l.states[absPath] = ImportLoaded
	l.cache[absPath] = result

	return result
}

func (l *Loader) formatImportCycle(repeatedPath string) string {
	parts := []string{}

	start := 0

	for i, path := range l.stack {
		if path == repeatedPath {
			start = i
			break
		}
	}

	for _, path := range l.stack[start:] {
		parts = append(parts, filepath.Base(path))
	}

	parts = append(parts, filepath.Base(repeatedPath))

	return strings.Join(parts, " -> ")
}

func LoadProgram(path string) Program {
	loader := &Loader{
		states: map[string]ImportState{},
		stack:  []string{},
		cache:  map[string][]Stmt{},
	}

	statements := loader.loadFile(path)

	return Program{Statements: statements}
}
