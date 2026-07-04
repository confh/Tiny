package main

import (
	"path/filepath"
	"strings"

	. "language.com/src/vm"
)

func unwrapExport(stmt Stmt) (Stmt, bool) {
	if exp, ok := stmt.(ExportStmt); ok {
		return exp.Inner, true
	}
	return stmt, false
}

func defaultLibraryAlias(path string) string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	if len(parts) >= 2 && parts[1] != "" {
		return parts[1]
	}
	return filepath.Base(path)
}
