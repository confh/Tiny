//lsp_parse.go

package main

import (
	"fmt"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type LSPDiagnostic struct {
	Line    int
	Column  int
	Message string
	Kind    string
}

func parseTinyForLSP(uri string, text string) (statements []Stmt, diagnostics []LSPDiagnostic) {
	defer func() {
		if r := recover(); r != nil {
			diagnostics = append(diagnostics, diagnosticFromRecover(r))
			statements = nil
		}
	}()

	lexer := NewLexer(text, uri)
	parser := NewParser(lexer)
	program := parser.ParseProgram()

	return program.Statements, diagnostics
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

func lineEndCharacter(text string, line int, column int) int {
	lines := strings.Split(text, "\n")

	if line < 0 || line >= len(lines) {
		return column + 1
	}

	end := column + 1
	if end > len(lines[line]) {
		end = len(lines[line])
	}

	if end < column {
		end = column
	}

	return end
}
