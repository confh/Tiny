package main

import "fmt"

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

type LangError struct {
	Kind    ErrorKind
	Message string
}

func (e LangError) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func langError(kind ErrorKind, format string, args ...any) {
	panic(LangError{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	})
}

func handleLangError() {
	if r := recover(); r != nil {
		switch err := r.(type) {
		case LangError:
			fmt.Println(err.Error())
		case error:
			fmt.Println("InternalError:", err)
		default:
			fmt.Println("InternalError:", r)
		}
	}
}
