package tinyerrors

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/fatih/color"
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

var stackFrameRegex = regexp.MustCompile(`^(\s*at\s+)(.*?)(?:\s+\((.*):(\d+)(?::(\d+))?\))?$`)
var messageAccentRegex = regexp.MustCompile(`'[^']+'|"[^"]+"|` + "`[^`]+`" + `|\b\d+\b`)

var (
	errorKindStyle    = color.New(color.FgHiRed, color.Bold).SprintFunc()
	errorMessageStyle = color.New(color.FgHiWhite).SprintFunc()
	errorPathStyle    = color.New(color.FgBlue).SprintFunc()
	errorLineStyle    = color.New(color.FgBlue).SprintFunc()
	errorColumnStyle  = color.New(color.FgBlue).SprintFunc()
	errorStackStyle   = color.New(color.FgHiBlack, color.Bold).SprintFunc()
	errorFrameStyle   = color.New(color.FgHiCyan).SprintFunc()
	errorAtStyle      = color.New(color.FgBlack, color.Bold).SprintFunc()
	errorAccentStyle  = color.New(color.FgHiWhite, color.Bold).SprintFunc()
)

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

var (
	errorCollectorMu sync.Mutex
	errorCollector   *[]LangErrorType
)

func SetErrorCollector(collector *[]LangErrorType) {
	errorCollectorMu.Lock()
	defer errorCollectorMu.Unlock()
	errorCollector = collector
}

func ClearErrorCollector() {
	errorCollectorMu.Lock()
	defer errorCollectorMu.Unlock()
	errorCollector = nil
}

func LangError(kind ErrorKind, format string, args ...any) {
	err := LangErrorType{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
	}
	if tryCollect(err) {
		return
	}
	panic(err)
}

func LangErrorAt(kind ErrorKind, file string, line int, column int, format string, args ...any) {
	err := LangErrorType{
		Kind:    kind,
		Message: fmt.Sprintf(format, args...),
		File:    file,
		Line:    line,
		Column:  column,
	}
	if tryCollect(err) {
		return
	}
	panic(err)
}

func tryCollect(err LangErrorType) bool {
	errorCollectorMu.Lock()
	collector := errorCollector
	errorCollectorMu.Unlock()
	if collector == nil {
		return false
	}
	*collector = append(*collector, err)
	return true
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

		fmt.Printf("%s:%s:%s %s: %s\n", errorPathStyle(relPath), errorLineStyle(err.Line), errorColumnStyle(err.Column), errorKindStyle(err.Kind), formatLangErrorMessage(err.Message))
		return
	}

	fmt.Printf("%s: %s\n", errorKindStyle(err.Kind), formatLangErrorMessage(err.Message))
}

func formatLangErrorMessage(message string) string {
	parts := strings.SplitN(message, "\n\nStack trace:\n", 2)
	if len(parts) == 1 {
		return colorErrorText(message)
	}

	return colorErrorText(parts[0]) + "\n\n" + errorStackStyle("Stack trace:") + "\n" + colorStackTrace(parts[1])
}

func colorErrorText(text string) string {
	matches := messageAccentRegex.FindAllStringIndex(text, -1)
	if len(matches) == 0 {
		return errorMessageStyle(text)
	}

	var builder strings.Builder
	last := 0
	for _, match := range matches {
		if match[0] > last {
			builder.WriteString(errorMessageStyle(text[last:match[0]]))
		}
		builder.WriteString(errorAccentStyle(text[match[0]:match[1]]))
		last = match[1]
	}
	if last < len(text) {
		builder.WriteString(errorMessageStyle(text[last:]))
	}
	return builder.String()
}

func colorStackTrace(trace string) string {
	lines := strings.Split(trace, "\n")
	for i, line := range lines {
		lines[i] = colorStackFrame(line)
	}
	return strings.Join(lines, "\n")
}

func colorStackFrame(line string) string {
	match := stackFrameRegex.FindStringSubmatch(line)
	if match == nil {
		return errorMessageStyle(line)
	}

	prefix := errorAtStyle(match[1])
	name := errorFrameStyle(match[2])
	if match[3] == "" {
		return prefix + name
	}

	location := " (" + errorPathStyle(match[3]) + ":" + errorLineStyle(match[4])
	if match[5] != "" {
		location += ":" + errorColumnStyle(match[5])
	}
	location += ")"

	return prefix + name + location
}
