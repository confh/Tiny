package main

import (
	"os"
	"path/filepath"
)

func LoadProgram(path string) Program {
	visited := map[string]bool{}
	statements := loadFile(path, visited)

	return Program{Statements: statements}
}

func loadFile(path string, visited map[string]bool) []Stmt {
	absPath, err := filepath.Abs(path)
	if err != nil {
		langError(ErrorImport, "%v", err)
	}

	if visited[absPath] {
		return nil
	}

	visited[absPath] = true

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		langError(ErrorImport, "failed to read file %s: %v", path, err)
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
			importedStatements := loadFile(importPath, visited)
			result = append(result, importedStatements...)

		default:
			result = append(result, stmt)
		}
	}

	return result
}
