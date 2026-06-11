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
	Stderr string
	Panic  any
}

func runTinyFile(t *testing.T, path string, args ...string) (res tinyRunResult) {
	t.Helper()

	defer func() {
		if r := recover(); r != nil {
			res = tinyRunResult{
				Panic: r,
			}
		}
	}()

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, path)
	return runTinyBytecode(t, mainInstructions, functions, classes, interfaces, globalIndex, args...)
}

func compileTinyFile(t *testing.T, path string) ([]vm.Instruction, map[string]vm.Function, map[string]vm.Class, map[string]vm.Interface, map[string]int) {
	t.Helper()

	program := LoadProgram(path)
	compiler := NewCompiler()
	mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	return mainInstructions, functions, classes, interfaces, globalIndex
}

func runTinyBytecode(
	t *testing.T,
	mainInstructions []vm.Instruction,
	functions map[string]vm.Function,
	classes map[string]vm.Class,
	interfaces map[string]vm.Interface,
	globalIndex map[string]int,
	args ...string,
) tinyRunResult {
	t.Helper()

	stdoutCaptureMu.Lock()
	defer stdoutCaptureMu.Unlock()

	oldStdout := os.Stdout
	oldStderr := os.Stderr

	stdoutReader, stdoutWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stdout pipe: %v", err)
	}
	stderrReader, stderrWriter, err := os.Pipe()
	if err != nil {
		t.Fatalf("create stderr pipe: %v", err)
	}

	os.Stdout = stdoutWriter
	os.Stderr = stderrWriter

	var panicValue any
	func() {
		defer func() {
			panicValue = recover()
		}()

		tinyVM := vm.NewVM(mainInstructions, functions, classes, interfaces, globalIndex, false)
		tinyVM.SetCLIArgs(args)
		tinyVM.Run()
	}()

	stdoutWriteErr := stdoutWriter.Close()
	stderrWriteErr := stderrWriter.Close()
	os.Stdout = oldStdout
	os.Stderr = oldStderr

	var stdoutOutput bytes.Buffer
	_, stdoutCopyErr := io.Copy(&stdoutOutput, stdoutReader)
	stdoutCloseErr := stdoutReader.Close()

	var stderrOutput bytes.Buffer
	_, stderrCopyErr := io.Copy(&stderrOutput, stderrReader)
	stderrCloseErr := stderrReader.Close()

	if stdoutWriteErr != nil {
		t.Fatalf("close stdout writer: %v", stdoutWriteErr)
	}
	if stderrWriteErr != nil {
		t.Fatalf("close stderr writer: %v", stderrWriteErr)
	}
	if stdoutCopyErr != nil {
		t.Fatalf("read captured stdout: %v", stdoutCopyErr)
	}
	if stderrCopyErr != nil {
		t.Fatalf("read captured stderr: %v", stderrCopyErr)
	}
	if stdoutCloseErr != nil {
		t.Fatalf("close stdout reader: %v", stdoutCloseErr)
	}
	if stderrCloseErr != nil {
		t.Fatalf("close stderr reader: %v", stderrCloseErr)
	}

	return tinyRunResult{
		Stdout: stdoutOutput.String(),
		Stderr: stderrOutput.String(),
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

	const want = "4\n1\n1-2-3-4\nTiny\n15\nnull\n2,4,6,8\n"
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

func TestTinyPipelineMethodTypeHintErrors(t *testing.T) {
	requireTinyError(
		t,
		runTinyFile(t, fixturePath("errors", "method_type_hint.tiny")),
		tinyerrors.ErrorType,
		"expected Payload",
	)
}

func TestTinyPipelineInterfaceReturnObjectLiteral(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("interface_return_object.tiny")))

	const want = "true\n123\nfalse\nnope\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineNamespaceMethodReturnsInterfaceObjectLiteral(t *testing.T) {
	program := vm.Program{
		Statements: []vm.Stmt{
			vm.NamespaceStmt{
				Name: "TinyJWT",
				Statements: []vm.Stmt{
					vm.InterfaceStmt{
						Name: "VerifyData",
						Fields: map[string]vm.TypeHint{
							"valid": {Name: "bool"},
						},
					},
					vm.ClassStmt{
						Name: "JWT",
						Methods: []vm.FunctionStmt{
							{
								Name:       "verify",
								ReturnType: vm.TypeHint{Name: "VerifyData"},
								Body: []vm.Stmt{
									vm.ReturnStmt{
										HasValue: true,
										Value: vm.ObjectExpr{
											Fields: []vm.ObjectField{
												{Name: "valid", Value: vm.BoolExpr{Value: true}},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	compiler := NewCompiler()
	_, _, _, interfaces, _ := compiler.CompileProgram(program)

	if _, ok := interfaces["TinyJWT.VerifyData"]; !ok {
		t.Fatalf("expected namespaced interface to be compiled, got %#v", interfaces)
	}
}

func TestTinyPipelineNamespacedLibraryImportsCollision(t *testing.T) {
	program := vm.Program{
		Statements: []vm.Stmt{
			vm.NamespaceStmt{
				Name: "NS1",
				Statements: []vm.Stmt{
					vm.NamespaceStmt{
						Name: "Dep",
						Statements: []vm.Stmt{
							vm.FunctionStmt{
								Name:       "val",
								ReturnType: vm.TypeHint{Name: "string"},
								Body: []vm.Stmt{
									vm.ReturnStmt{
										HasValue: true,
										Value:    vm.StringExpr{Value: "NS1.Dep"},
									},
								},
							},
						},
					},
					vm.FunctionStmt{
						Name:       "test",
						ReturnType: vm.TypeHint{Name: "string"},
						Body: []vm.Stmt{
							vm.ReturnStmt{
								HasValue: true,
								Value: vm.MemberCallExpr{
									Object: vm.IdentExpr{Name: "Dep"},
									Method: "val",
									Args:   []vm.Expr{},
								},
							},
						},
					},
				},
			},
			vm.NamespaceStmt{
				Name: "NS2",
				Statements: []vm.Stmt{
					vm.NamespaceStmt{
						Name: "Dep",
						Statements: []vm.Stmt{
							vm.FunctionStmt{
								Name:       "val",
								ReturnType: vm.TypeHint{Name: "string"},
								Body: []vm.Stmt{
									vm.ReturnStmt{
										HasValue: true,
										Value:    vm.StringExpr{Value: "NS2.Dep"},
									},
								},
							},
						},
					},
					vm.FunctionStmt{
						Name:       "test",
						ReturnType: vm.TypeHint{Name: "string"},
						Body: []vm.Stmt{
							vm.ReturnStmt{
								HasValue: true,
								Value: vm.MemberCallExpr{
									Object: vm.IdentExpr{Name: "Dep"},
									Method: "val",
									Args:   []vm.Expr{},
								},
							},
						},
					},
				},
			},
			vm.ImportStmt{
				Path:  "io",
				Std:   true,
				Alias: "io",
			},
			vm.ExprStmt{
				Value: vm.MemberCallExpr{
					Object: vm.IdentExpr{Name: "io"},
					Method: "println",
					Args: []vm.Expr{
						vm.MemberCallExpr{
							Object: vm.IdentExpr{Name: "NS1"},
							Method: "test",
							Args:   []vm.Expr{},
						},
					},
				},
			},
			vm.ExprStmt{
				Value: vm.MemberCallExpr{
					Object: vm.IdentExpr{Name: "io"},
					Method: "println",
					Args: []vm.Expr{
						vm.MemberCallExpr{
							Object: vm.IdentExpr{Name: "NS2"},
							Method: "test",
							Args:   []vm.Expr{},
						},
					},
				},
			},
		},
	}

	compiler := NewCompiler()
	mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	res := runTinyBytecode(t, mainInstructions, functions, classes, interfaces, globalIndex)
	out := requireTinySuccess(t, res)

	const want = "NS1.Dep\nNS2.Dep\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineBytecodeRoundTrip(t *testing.T) {
	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, fixturePath("arithmetic.tiny"))

	data := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false)
	loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, _ := bytecode.LoadBytecodeFromBytes(data)

	out := requireTinySuccess(t, runTinyBytecode(t, loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, globalIndex))

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

func TestCompileNestedNamespaceInterfaceReturn(t *testing.T) {
	dir := t.TempDir()

	// Write results.tiny
	resultsContent := strings.Join([]string{
		`export interface SuccessResult {`,
		`    success: bool`,
		`}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "results.tiny"), []byte(resultsContent), 0644); err != nil {
		t.Fatalf("failed to write results.tiny: %v", err)
	}

	// Write client.tiny
	clientContent := strings.Join([]string{
		`import "results.tiny" as results;`,
		`export fn disconnect(): results.SuccessResult {`,
		`    return {`,
		`        success: true`,
		`    }`,
		`}`,
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "client.tiny"), []byte(clientContent), 0644); err != nil {
		t.Fatalf("failed to write client.tiny: %v", err)
	}

	// Write main.tiny
	mainContent := strings.Join([]string{
		`import "client.tiny" as Client;`,
		`Client.disconnect();`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	// Compile should succeed without TypeError
	program := LoadProgram(mainPath)
	compiler := NewCompiler()
	_, _, _, _, _ = compiler.CompileProgram(program)
}

func TestTinyPipelineEnumValidation(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`enum TestEnum {`,
		`    tester`,
		`}`,
		`fn test(ss: TestEnum) {`,
		`}`,
		`test(TestEnum.tester);`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	requireTinySuccess(t, runTinyFile(t, mainPath))
}

func TestTinyPipelineFloatDivisionJit(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`fn divide(a: number, b: number): number {`,
		`    return a / b;`,
		`}`,
		`io.println(divide(7.5, 2).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "3.75\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineGenericInterfaces(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`interface Box:T {`,
		`    value: T`,
		`}`,
		`fn printBox:T(b: Box:T) {`,
		`    io.println(b.value);`,
		`}`,
		`let b: Box:number = { value: 42 };`,
		`printBox:number(b);`,
		`let s: Box:string = { value: "hello" };`,
		`printBox:string(s);`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "42\nhello\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineGenericInterfaceErrors(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`interface Box:T {`,
		`    value: T`,
		`}`,
		`let b: Box:number = { value: "not-a-number" };`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	requireTinyError(t, runTinyFile(t, mainPath), tinyerrors.ErrorType, "expected number, got string")
}

func TestTinyPipelineGenerics(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`class Box:T {`,
		`    field value: T = null`,
		`    fn init(val: T) {`,
		`        this.value = val;`,
		`    }`,
		`}`,
		`fn identity:T(x: T): T {`,
		`    return x;`,
		`}`,
		`let b: Box:number = Box:number(42);`,
		`io.println(b.value.toString());`,
		`let id: string = identity:string("hello");`,
		`io.println(id);`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "42\nhello\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineGenericTypeErrors(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`class Box:T {`,
		`    field value: T = null`,
		`    fn init(val: T) {`,
		`        this.value = val;`,
		`    }`,
		`}`,
		`let b: Box:number = Box:number("hello");`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	result := tinyRunResult{}
	func() {
		defer func() {
			result.Panic = recover()
		}()
		compileTinyFile(t, mainPath)
	}()

	requireTinyError(t, result, tinyerrors.ErrorType, "cannot pass string to parameter 'val' of function 'class Box constructor' (expected number)")
}

func TestTinyPipelineArrayLoopJit(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`fn arrayLoop(limit: number): number {`,
		`    let arr = [];`,
		`    let i = 0;`,
		`    while i < limit {`,
		`        arr.push(i * 3 + 1);`,
		`        i = i + 1;`,
		`    }`,
		`    let total = 0;`,
		`    i = 0;`,
		`    while i < limit {`,
		`        total = total + arr[i];`,
		`        i = i + 1;`,
		`    }`,
		`    return total;`,
		`}`,
		`io.println(arrayLoop(5).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "35\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineJitDirectCallFromAnonymousFunction(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "tests" as tests;`,
		`fn arrayLoop(limit: number): number {`,
		`    let arr = [];`,
		`    let i = 0;`,
		`    while i < limit {`,
		`        arr.push(i * 3 + 1);`,
		`        i = i + 1;`,
		`    }`,
		`    let total = 0;`,
		`    i = 0;`,
		`    while i < limit {`,
		`        total = total + arr[i];`,
		`        i = i + 1;`,
		`    }`,
		`    return total;`,
		`}`,
		`io.println(arrayLoop(5).toString());`,
		`io.println(tests.measureMs(fn() { arrayLoop(5); }).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	result := runTinyFile(t, mainPath)
	out := requireTinySuccess(t, result)
	if strings.Contains(result.Stderr, "[JIT ERROR]") {
		t.Fatalf("unexpected JIT error:\n%s", result.Stderr)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || lines[0] != "35" {
		t.Fatalf("unexpected output lines: %q", lines)
	}
}
