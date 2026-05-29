package main

import (
	"fmt"

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
