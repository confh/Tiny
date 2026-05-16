package tinyerrors

import (
	"fmt"
	"os"
	"path/filepath"
)

type ErrorKind string

const (
	ErrorSyntax   ErrorKind = "SyntaxError"
	ErrorName     ErrorKind = "NameError"
	ErrorType     ErrorKind = "TypeError"
	ErrorRuntime  ErrorKind = "RuntimeError"
	ErrorConst    ErrorKind = "ConstError"
	ErrorImport   ErrorKind = "ImportError"
	ErrorInternal ErrorKind = "InternalError"
	ErrorUser     ErrorKind = "Error"
)

type LangErrorType struct {
	Kind    ErrorKind
	Message string

	File   string
	Line   int
	Column int
}

func (e LangErrorType) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func LangError(kind ErrorKind, format string, args ...any) {
	panic(LangErrorType{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	})
}

func LangErrorAt(kind ErrorKind, file string, line int, column int, format string, args ...any) {
	panic(LangErrorType{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
		File:    file,
		Line:    line,
		Column:  column,
	})
}

func HandleLangError() {
	if r := recover(); r != nil {
		switch err := r.(type) {
		case LangErrorType:
			if err.File != "" && err.Line > 0 {
				root, errDir := os.Getwd()
				if errDir != nil {
					fmt.Println("Error getting current directory:", err)
					return
				}
				relPath, errPath := filepath.Rel(root, err.File)
				if errPath != nil {
					relPath = err.File
				}
				fmt.Printf("%s:%d:%d %s: %s\n", relPath, err.Line, err.Column, err.Kind, err.Message)
			} else {
				fmt.Printf("%s: %s\n", err.Kind, err.Message)
			}
		case error:
			fmt.Println("InternalError:", err)
		default:
			fmt.Println("InternalError:", r)
		}
	}
}
