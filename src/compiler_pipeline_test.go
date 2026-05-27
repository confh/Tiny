package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"language.com/src/bytecode"
	"language.com/src/tinyerrors"
	"language.com/src/vm"
)

var stdoutCaptureMu sync.Mutex

type tinyRunResult struct {
	Stdout string
	Panic  any
}

func runTinyFile(t *testing.T, path string, args ...string) tinyRunResult {
	t.Helper()

	mainInstructions, functions, classes := compileTinyFile(t, path)
	return runTinyBytecode(t, mainInstructions, functions, classes, args...)
}

func compileTinyFile(t *testing.T, path string) ([]vm.Instruction, map[string]vm.Function, map[string]vm.Class) {
	t.Helper()

	program := LoadProgram(path)
	compiler := NewCompiler()
	mainInstructions, functions, classes := compiler.CompileProgram(program)

	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	return mainInstructions, functions, classes
}

func runTinyBytecode(
	t *testing.T,
	mainInstructions []vm.Instruction,
	functions map[string]vm.Function,
	classes map[string]vm.Class,
	args ...string,
) tinyRunResult {
	t.Helper()

	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	oldStdout := os.Stdout

	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}

	os.Stdout = writer

	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()

		tinyVM := vm.NewVM(mainInstructions, functions, classes)
		tinyVM.SetCLIArgs(args)
		tinyVM.Run()
	}()

	writeErr := writer.Close()
	os.Stdout = oldStdout

	var output bytes.Buffer
	_, copyErr := io.Copy(&output, reader)
	closeErr := reader.Close()

	if writeErr != nil {
		t.Fatalf("close stdout writer: %v", writeErr)
	}
	if copyErr != nil {
		t.Fatalf("read captured stdout: %v", copyErr)
	}
	if closeErr != nil {
		t.Fatalf("close stdout reader: %v", closeErr)
	}

	return tinyRunResult{
		Stdout: output.String(),
		Panic:  panicValue,
	}
}

func requireTinySuccess(t *testing.T, result tinyRunResult) string {
	t.Helper()

	if result.Panic != nil {
		t.Fatalf("Tiny program panicked: %v", result.Panic)
	}

	return result.Stdout
}

func requireTinyError(t *testing.T, result tinyRunResult, kind tinyerrors.ErrorKind, contains string) {
	t.Helper()

	if result.Panic == nil {
		t.Fatalf("expected %s containing %q, got success with stdout:\n%s", kind, contains, result.Stdout)
	}

	langErr, ok := result.Panic.(tinyerrors.LangErrorType)
	if !ok {
		t.Fatalf("expected LangErrorType, got %T: %v", result.Panic, result.Panic)
	}

	if langErr.Kind != kind {
		t.Fatalf("expected error kind %s, got %s: %s", kind, langErr.Kind, langErr.Message)
	}

	if !strings.Contains(langErr.Message, contains) {
		t.Fatalf("expected error message to contain %q, got %q", contains, langErr.Message)
	}
}

func fixturePath(parts ...string) string {
	all := append([]string{"testdata", "tiny"}, parts...)
	return filepath.Join(all...)
}

func TestTinyPipelineArithmeticAndStrings(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("arithmetic.tiny")))

	const want = "7\nhello Tiny v1\nstring\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineControlFlow(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("control_flow.tiny")))

	const want = "0\n1\n2\n6\none\nfallback\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineFunctionsDefaultsVariadicAndClosures(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("functions.tiny")))

	const want = "Hello, Tiny\nWelcome, Tiny\n6\n1\n2\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineClasses(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("classes.tiny")))

	const want = "Tiny user: 42\ntrue\ncalled through embedded logger\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineNamespacedImports(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("imports", "main.tiny")))

	const want = "report: green\nready\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineArraysObjectsAndNativeMethods(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("arrays_objects.tiny")))

	const want = "4\n1\n1-2-3-4\nTiny\n15\nundefined\n2,4,6,8\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineTryCatchFinallyAndThrow(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("errors", "try_catch.tiny")))

	const want = "ValidationError\nname required\nfinally\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineCLIArgs(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("cli_args.tiny"), "alpha", "beta"))

	const want = "2\nalpha\nalpha-beta\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineTypeHintErrors(t *testing.T) {
	requireTinyError(
		t,
		runTinyFile(t, fixturePath("errors", "type_hint.tiny")),
		tinyerrors.ErrorType,
		"expected number",
	)
}

func TestTinyPipelineBytecodeRoundTrip(t *testing.T) {
	mainInstructions, functions, classes := compileTinyFile(t, fixturePath("arithmetic.tiny"))

	data := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, false)
	loadedMain, loadedFunctions, loadedClasses := bytecode.LoadBytecodeFromBytes(data)

	out := requireTinySuccess(t, runTinyBytecode(t, loadedMain, loadedFunctions, loadedClasses))

	const want = "7\nhello Tiny v1\nstring\n"
	if out != want {
		t.Fatalf("unexpected output after bytecode round trip:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineDefer(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("defer.tiny")))

	const want = "after defer\ndeferred\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineReportsRuntimeErrors(t *testing.T) {
	requireTinyError(
		t,
		runTinyFile(t, fixturePath("errors", "const_assignment.tiny")),
		tinyerrors.ErrorConst,
		"cannot assign to constant global",
	)
}

func TestTinyPipelineReportsCompileErrors(t *testing.T) {
	result := tinyRunResult{}

	func() {
		defer func() {
			result.Panic = recover()
		}()

		compileTinyFile(t, fixturePath("errors", "undefined_variable.tiny"))
	}()

	requireTinyError(
		t,
		result,
		tinyerrors.ErrorName,
		"undefined variable",
	)
}
