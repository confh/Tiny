package tinyerrors

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

type ErrorKind string

const (
	ErrorSyntax   ErrorKind = "SyntaxError"
	ErrorName     ErrorKind = "NameError"
	ErrorType     ErrorKind = "TypeError"
	ErrorRuntime  ErrorKind = "RuntimeError"
	ErrorIndex    ErrorKind = "IndexError"
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

type FatalCrashInfo struct {
	Kind    ErrorKind
	Message string
	File    string
	Line    int
	Column  int
	Raw     any
}

var fatalHookMu sync.RWMutex
var fatalHook func(FatalCrashInfo) bool

func SetFatalHook(fn func(FatalCrashInfo) bool) {
	fatalHookMu.Lock()
	defer fatalHookMu.Unlock()

	fatalHook = fn
}

func ClearFatalHook() {
	fatalHookMu.Lock()
	defer fatalHookMu.Unlock()

	fatalHook = nil
}

func runFatalHook(info FatalCrashInfo) bool {
	fatalHookMu.RLock()
	hook := fatalHook
	fatalHookMu.RUnlock()

	if hook == nil {
		return false
	}

	handled := false

	func() {
		defer func() {
			if recover() != nil {
				handled = false
			}
		}()

		handled = hook(info)
	}()

	return handled
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
			info := FatalCrashInfo{
				Kind:    err.Kind,
				Message: err.Message,
				File:    err.File,
				Line:    err.Line,
				Column:  err.Column,
				Raw:     err,
			}

			if runFatalHook(info) {
				return
			}

			printLangError(err)

		case *LangErrorType:
			info := FatalCrashInfo{
				Kind:    err.Kind,
				Message: err.Message,
				File:    err.File,
				Line:    err.Line,
				Column:  err.Column,
				Raw:     err,
			}

			if runFatalHook(info) {
				return
			}

			printLangError(*err)

		case error:
			info := FatalCrashInfo{
				Kind:    ErrorInternal,
				Message: err.Error(),
				Raw:     err,
			}

			if runFatalHook(info) {
				return
			}

			fmt.Println("InternalError:", err)

		default:
			info := FatalCrashInfo{
				Kind:    ErrorInternal,
				Message: fmt.Sprint(r),
				Raw:     r,
			}

			if runFatalHook(info) {
				return
			}

			fmt.Println("InternalError:", r)
		}
	}
}

func printLangError(err LangErrorType) {
	if err.File != "" && err.Line > 0 {
		root, errDir := os.Getwd()
		if errDir != nil {
			fmt.Println("Error getting current directory:", errDir)
			return
		}

		relPath, errPath := filepath.Rel(root, err.File)
		if errPath != nil {
			relPath = err.File
		}

		fmt.Printf("%s:%d:%d %s: %s\n", relPath, err.Line, err.Column, err.Kind, err.Message)
		return
	}

	fmt.Printf("%s: %s\n", err.Kind, err.Message)
}
