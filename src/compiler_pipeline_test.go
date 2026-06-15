package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
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
	_ map[string]int,
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

		tinyVM := vm.NewVM(vm.VMInfo{
			MainInstructions: mainInstructions,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			Packed:           false,
		})
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

func TestTinyPipelineJitOutlinesUnsafeNumericWhileLoop(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`fn wrapper(n: number): number {`,
		`    io.println("before");`,
		`    let total = 0;`,
		`    let i = 0;`,
		`    while i < n {`,
		`        i = i + 1;`,
		`        if i == 2 {`,
		`            continue;`,
		`        }`,
		`        if i > 5 {`,
		`            break;`,
		`        }`,
		`        total = total + i;`,
		`    }`,
		`    io.println("after");`,
		`    return total;`,
		`}`,
		`io.println(wrapper(10).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
	helperName := ""
	for name := range functions {
		if strings.HasPrefix(name, "__jit_region_wrapper_") {
			helperName = name
			break
		}
	}
	if helperName == "" {
		t.Fatalf("expected wrapper loop to be outlined into a JIT helper")
	}

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
	})
	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}
	jitFn := jitFuncs.MapIndex(reflect.ValueOf(helperName))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected outlined helper %s to be JIT-compiled", helperName)
	}

	out := requireTinySuccess(t, runTinyBytecode(t, mainInstructions, functions, classes, interfaces, globalIndex))
	const want = "before\nafter\n13\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineJitOutliningRejectsUnsafeLoops(t *testing.T) {
	tests := []struct {
		name    string
		content string
	}{
		{
			name: "return crosses loop boundary",
			content: strings.Join([]string{
				`fn wrapper(n: number): number {`,
				`    let total = 0;`,
				`    let i = 0;`,
				`    while i < n {`,
				`        if i > 3 {`,
				`            return total;`,
				`        }`,
				`        total = total + i;`,
				`        i = i + 1;`,
				`    }`,
				`    return total;`,
				`}`,
			}, "\n"),
		},
		{
			name: "stdlib call inside loop",
			content: strings.Join([]string{
				`import std "io" as io;`,
				`fn wrapper(n: number): number {`,
				`    let total = 0;`,
				`    let i = 0;`,
				`    while i < n {`,
				`        io.println(i);`,
				`        total = total + i;`,
				`        i = i + 1;`,
				`    }`,
				`    return total;`,
				`}`,
			}, "\n"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainPath := filepath.Join(dir, "main.tiny")
			if err := os.WriteFile(mainPath, []byte(tt.content), 0644); err != nil {
				t.Fatalf("failed to write main.tiny: %v", err)
			}

			_, functions, _, _, _ := compileTinyFile(t, mainPath)
			for name := range functions {
				if strings.HasPrefix(name, "__jit_region_wrapper_") {
					t.Fatalf("expected loop to remain interpreted, but generated helper %s", name)
				}
			}
		})
	}
}

func TestTinyPipelineJitOutlinesNestedNumericForLoopWithStdlibPoisonPill(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "http";`,
		`fn mandelbrot(px: number, py: number, maxIter: number): number {`,
		`    let cr = py - 0.5`,
		`    let ci = px`,
		`    let zr = 0.0`,
		`    let zi = 0.0`,
		`    let i = 0`,
		`    let maxI = maxIter`,
		`    while i < maxI {`,
		`        let zr2 = zr * zr`,
		`        let zi2 = zi * zi`,
		`        if zr2 + zi2 > 4.0 {`,
		`            return i`,
		`        }`,
		`        let temp = zr * zi * 2.0`,
		`        zr = zr2 - zi2 + cr`,
		`        zi = temp + ci`,
		`        i = i + 1`,
		`    }`,
		`    return maxI`,
		`}`,
		`fn run_mandelbrot(size: number, maxIter: number): number {`,
		`    http.server(3000)`,
		`    let count = 0`,
		`    let size_f = (size)`,
		`    for let y = 0; y < size; y = y + 1 {`,
		`        let py = ((y) / size_f) * 2.5 - 1.25`,
		`        for let x = 0; x < size; x = x + 1 {`,
		`            let px = ((x) / size_f) * 3.0 - 2.0`,
		`            count = count + mandelbrot(px, py, maxIter)`,
		`        }`,
		`    }`,
		`    return count`,
		`}`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	_, functions, _, _, _ := compileTinyFile(t, mainPath)
	helperName := ""
	for name := range functions {
		if strings.HasPrefix(name, "__jit_region_run_mandelbrot_") {
			helperName = name
			break
		}
	}
	if helperName == "" {
		t.Fatalf("expected nested numeric for loop to be outlined into a JIT helper")
	}

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: nil,
		Functions:        functions,
		Classes:          nil,
		Interfaces:       nil,
		Packed:           false,
	})
	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}
	jitFn := jitFuncs.MapIndex(reflect.ValueOf(helperName))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected outlined helper %s to be JIT-compiled", helperName)
	}
}

func TestTinyPipelineJitOutlinesNumericLoopWithMathIntrinsics(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "http";`,
		`import std "math";`,
		`fn hot_math(n: number): number {`,
		`    http.server(3000)`,
		`    let total = 0.0`,
		`    for let i = 1; i < n; i = i + 1 {`,
		`        let root = math.sqrt(i)`,
		`        total = total + math.abs(root - math.floor(root))`,
		`        total = total + math.pow(2, 3)`,
		`    }`,
		`    return total`,
		`}`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	_, functions, _, _, _ := compileTinyFile(t, mainPath)
	helperName := ""
	for name := range functions {
		if strings.HasPrefix(name, "__jit_region_hot_math_") {
			helperName = name
			break
		}
	}
	if helperName == "" {
		t.Fatalf("expected math-heavy numeric loop to be outlined into a JIT helper")
	}

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: nil,
		Functions:        functions,
		Classes:          nil,
		Interfaces:       nil,
		Packed:           false,
	})
	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}
	jitFn := jitFuncs.MapIndex(reflect.ValueOf(helperName))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected outlined helper %s to be JIT-compiled", helperName)
	}
}

func TestTinyPipelineJitOutlinesObjectArrayAndStringRegions(t *testing.T) {
	tests := []struct {
		name   string
		prefix string
		body   []string
	}{
		{
			name:   "object field mutation",
			prefix: "__jit_region_object_hot_",
			body: []string{
				`fn object_hot(n: number): number {`,
				`    http.server(3000)`,
				`    let state = { total: 0, flag: false, label: "hot" }`,
				`    for let i = 0; i < n; i = i + 1 {`,
				`        state.total = state.total + i`,
				`        state.flag = state.total > 10`,
				`        state.label = state.label + "!"`,
				`    }`,
				`    return state.total`,
				`}`,
			},
		},
		{
			name:   "array index and length",
			prefix: "__jit_region_array_hot_",
			body: []string{
				`fn array_hot(n: number): number {`,
				`    http.server(3000)`,
				`    let items = [0, 1]`,
				`    for let i = 0; i < n; i = i + 1 {`,
				`        items[0] = items[0] + i`,
				`        items.push(i)`,
				`        let size = items.length()`,
				`    }`,
				`    return items[0]`,
				`}`,
			},
		},
		{
			name:   "string concat and length",
			prefix: "__jit_region_string_hot_",
			body: []string{
				`fn string_hot(n: number): number {`,
				`    http.server(3000)`,
				`    let s = ""`,
				`    for let i = 0; i < n; i = i + 1 {`,
				`        s = s + "x"`,
				`        let size = s.length()`,
				`    }`,
				`    return s.length()`,
				`}`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainContent := strings.Join(append([]string{`import std "http";`}, tt.body...), "\n")
			mainPath := filepath.Join(dir, "main.tiny")
			if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
				t.Fatalf("failed to write main.tiny: %v", err)
			}

			_, functions, _, _, _ := compileTinyFile(t, mainPath)
			helperName := ""
			for name := range functions {
				if strings.HasPrefix(name, tt.prefix) {
					helperName = name
					break
				}
			}
			if helperName == "" {
				t.Fatalf("expected region to be outlined into a JIT helper")
			}

			tinyVM := vm.NewVM(vm.VMInfo{
				MainInstructions: nil,
				Functions:        functions,
				Classes:          nil,
				Interfaces:       nil,
				Packed:           false,
			})
			val := reflect.ValueOf(tinyVM).Elem()
			jitFuncs := val.FieldByName("jitFunctions")
			if !jitFuncs.IsValid() {
				t.Fatalf("jitFunctions field not found on VM")
			}
			jitFn := jitFuncs.MapIndex(reflect.ValueOf(helperName))
			if !jitFn.IsValid() || jitFn.IsNil() {
				t.Fatalf("expected outlined helper %s to be JIT-compiled", helperName)
			}
		})
	}
}

func TestTinyPipelineStringBuildBenchmarkRegression(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io";`,
		`fn string_build(n: number): number {`,
		`    let s = ""`,
		`    let checksum = 0`,
		`    for let i = 0; i < n; i++ {`,
		`        if i % 4 == 0 {`,
		`            s = s + "a"`,
		`        } else if i % 4 == 1 {`,
		`            s = s + "bb"`,
		`        } else if i % 4 == 2 {`,
		`            s = s + "ccc"`,
		`        } else {`,
		`            s = s + "d"`,
		`        }`,
		`        if i % 50 == 0 {`,
		`            checksum = checksum + s.length()`,
		`        }`,
		`    }`,
		`    return checksum + s.length()`,
		`}`,
		`io.println(string_build(12000).toString())`,
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
	if strings.TrimSpace(out) != "2530920" {
		t.Fatalf("unexpected string_build result: got %q want %q", strings.TrimSpace(out), "2530920")
	}
}

func TestTinyPipelineJitMemoInvalidatesOnStdObjectMutation(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "object" as object;`,
		`fn sum_field(obj): number {`,
		`    let total = 0;`,
		`    for let i = 0; i < 1000; i++ {`,
		`        total = total + obj.value;`,
		`    }`,
		`    return total;`,
		`}`,
		`let obj = { value: 1 };`,
		`io.println(sum_field(obj).toString());`,
		`object.set(obj, "value", 2);`,
		`io.println(sum_field(obj).toString());`,
		`object.delete(obj, "value");`,
		`object.set(obj, "value", 3);`,
		`io.println(sum_field(obj).toString());`,
		`object.clear(obj);`,
		`object.set(obj, "value", 4);`,
		`io.println(sum_field(obj).toString());`,
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

	const want = "1000\n2000\n3000\n4000\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineJitOutliningRejectsDynamicStringMethods(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "http";`,
		`fn string_hot(n: number): number {`,
		`    http.server(3000)`,
		`    let s = "prefix"`,
		`    let total = 0`,
		`    for let i = 0; i < n; i = i + 1 {`,
		`        if s.includes("x") {`,
		`            total = total + 1`,
		`        }`,
		`        s = s + "x"`,
		`    }`,
		`    return total`,
		`}`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	_, functions, _, _, _ := compileTinyFile(t, mainPath)
	for name := range functions {
		if strings.HasPrefix(name, "__jit_region_string_hot_") {
			t.Fatalf("expected dynamic string method loop to remain interpreted, but generated helper %s", name)
		}
	}
}

func TestTinyPipelineJitOutlinesMultipleEscapingSetupValues(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "http";`,
		`fn mutate_stress(n: number): number {`,
		`    http.server(3000)`,
		`    let arr = [1, 2, 3, 4, 5, 6, 7, 8]`,
		`    let obj = { count: 0, flips: 0, checksum: 1 }`,
		`    let s = "tiny-language-jit-outline-test"`,
		`    let slen = s.length()`,
		`    let total = 0`,
		`    for let i = 0; i < n; i = i + 1 {`,
		`        let idx = i % 8`,
		`        let old = arr[idx]`,
		`        arr[idx] = old + idx + slen`,
		`        obj.count = obj.count + arr[idx]`,
		`        if arr[idx] % 5 == 0 {`,
		`            obj.flips = obj.flips + 1`,
		`            arr[idx] = arr[idx] - slen`,
		`        } else {`,
		`            obj.checksum = obj.checksum + (arr[idx] % 97)`,
		`        }`,
		`        total = total + arr[idx] + obj.flips`,
		`    }`,
		`    return total + obj.count + obj.flips + obj.checksum + arr[0] + arr[7]`,
		`}`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	_, functions, _, _, _ := compileTinyFile(t, mainPath)
	helperName := ""
	for name := range functions {
		if strings.HasPrefix(name, "__jit_region_mutate_stress_") {
			helperName = name
			break
		}
	}
	if helperName == "" {
		t.Fatalf("expected multiple-live-out loop to be outlined into a JIT helper")
	}

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: nil,
		Functions:        functions,
		Classes:          nil,
		Interfaces:       nil,
		Packed:           false,
	})
	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}
	jitFn := jitFuncs.MapIndex(reflect.ValueOf(helperName))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected outlined helper %s to be JIT-compiled", helperName)
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

func TestTinyPipelineJitComprehensiveEdgeCases(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "tests" as tests;`,

		`fn f1(n: number): number {`,
		`    let count = 0;`,
		`    let i = 0;`,
		`    while i < n {`,
		`        let obj = { val: i * 2, active: i % 2 == 0 };`,
		`        if obj.active {`,
		`            count = count + obj.val;`,
		`        }`,
		`        i = i + 1;`,
		`    }`,
		`    return count;`,
		`}`,

		`fn f2(n: number): number {`,
		`    let count = 0;`,
		`    let i = 0;`,
		`    while i < n {`,
		`        let obj = { val: i * 3, active: i % 3 == 0 };`,
		`        if obj.active {`,
		`            count = count + obj.val;`,
		`        }`,
		`        i = i + 1;`,
		`    }`,
		`    return count;`,
		`}`,

		`fn testTruthiness(x: any): number {`,
		`    if x {`,
		`        return 1;`,
		`    }`,
		`    return 0;`,
		`}`,

		`fn testLogic(a: bool, b: bool): bool {`,
		`    return a and b or !a;`,
		`}`,

		`fn fib(n: number): number {`,
		`    if n <= 1 {`,
		`        return n;`,
		`    }`,
		`    return fib(n - 1) + fib(n - 2);`,
		`}`,

		`io.println(f1(5).toString());`,
		`io.println(f2(5).toString());`,
		`io.println(testTruthiness("hello").toString());`,
		`io.println(testTruthiness("").toString());`,
		`io.println(testTruthiness(null).toString());`,
		`io.println(testTruthiness(123).toString());`,
		`io.println(testLogic(true, false).toString());`,
		`io.println(testLogic(false, true).toString());`,
		`io.println(fib(6).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
	})
	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}
	compiledName := ""
	for _, name := range jitFuncs.MapKeys() {
		key := name.String()
		if strings.HasPrefix(key, "__jit_region_f1_") {
			compiledName = key
			break
		}
	}
	if compiledName == "" {
		t.Fatalf("expected f1 loop to be outlined into a JIT helper")
	}
	jitFn := jitFuncs.MapIndex(reflect.ValueOf(compiledName))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected outlined helper %s to be JIT-compiled", compiledName)
	}

	result := runTinyBytecode(t, mainInstructions, functions, classes, interfaces, globalIndex)
	out := requireTinySuccess(t, result)
	if strings.Contains(result.Stderr, "[JIT ERROR]") {
		t.Fatalf("unexpected JIT error:\n%s", result.Stderr)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	expected := []string{"12", "9", "1", "0", "0", "1", "false", "true", "8"}
	if len(lines) != len(expected) {
		t.Fatalf("unexpected number of output lines: got %d, want %d (out: %q)", len(lines), len(expected), lines)
	}
	for i, val := range expected {
		if lines[i] != val {
			t.Errorf("line %d: got %q, want %q", i, lines[i], val)
		}
	}
}

func TestTinyPipelineJitForInObjectAggregation(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "array" as array;`,
		``,
		`fn generate_logs() {`,
		`    let logs = [];`,
		`    for let i = 0; i < 1000; i++ {`,
		`        let status = 200;`,
		`        if i % 5 == 0 {`,
		`            status = 500;`,
		`        }`,
		`        let time_ms = (i % 10) * 15 + 10;`,
		`        let bytes_sent = (i % 3) * 500 + 100;`,
		`        let success = true;`,
		`        if status == 500 {`,
		`            success = false;`,
		`        }`,
		``,
		`        logs.push({`,
		`            status: status,`,
		`            time: time_ms,`,
		`            bytes: bytes_sent,`,
		`            success: success`,
		`        });`,
		`    }`,
		`    return logs;`,
		`}`,
		``,
		`fn aggregate(logs: array) {`,
		`    let total_time = 0;`,
		`    let total_bytes = 0;`,
		`    let success_count = 0;`,
		`    let fail_count = 0;`,
		``,
		`    for log in logs {`,
		`        if log.success {`,
		`            success_count = success_count + 1;`,
		`            total_time = total_time + log.time;`,
		`            total_bytes = total_bytes + log.bytes;`,
		`        } else {`,
		`            fail_count = fail_count + 1;`,
		`        }`,
		`    }`,
		``,
		`    return {`,
		`        success_count: success_count,`,
		`        fail_count: fail_count,`,
		`        avg_time: total_time / success_count,`,
		`        total_bytes: total_bytes`,
		`    };`,
		`}`,
		``,
		`let logs_data = generate_logs();`,
		`let final_result = {};`,
		`for let i = 0; i < 5000; i++ {`,
		`    final_result = aggregate(logs_data);`,
		`}`,
		`io.println(final_result.success_count.toString());`,
		`io.println(final_result.fail_count.toString());`,
		`io.println(final_result.avg_time.toString());`,
		`io.println(final_result.total_bytes.toString());`,
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

	const want = "800\n200\n85\n479500\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

// func TestTinyPipelineJitPropertyTypeInference(t *testing.T) {
// 	dir := t.TempDir()
// 	mainContent := strings.Join([]string{
// 		`import std "io";`,
// 		`fn calculateTotal() {`,
// 		`    let obj = { price: 10.5, qty: 3.0 };`,
// 		`    let total = obj.price * obj.qty;`,
// 		`    return total;`,
// 		`}`,
// 		`io.println(calculateTotal().toString());`,
// 	}, "\n")
// 	mainPath := filepath.Join(dir, "main.tiny")
// 	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
// 		t.Fatalf("failed to write main.tiny: %v", err)
// 	}

// 	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
// 	tinyVM := vm.NewVM(mainInstructions, functions, classes, interfaces, globalIndex, false)

// 	val := reflect.ValueOf(tinyVM).Elem()
// 	jitFuncs := val.FieldByName("jitFunctions")
// 	if !jitFuncs.IsValid() {
// 		t.Fatalf("jitFunctions field not found on VM")
// 	}
// 	calculateTotalJit := jitFuncs.MapIndex(reflect.ValueOf("calculateTotal"))
// 	if !calculateTotalJit.IsValid() || calculateTotalJit.IsNil() {
// 		t.Fatalf("expected calculateTotal to be JIT-compiled but it was not found in jitFunctions")
// 	}
// }

// func TestTinyPipelineJitCoalesceTypeofThrow(t *testing.T) {
// 	dir := t.TempDir()
// 	mainContent := strings.Join([]string{
// 		`import std "io";`,
// 		`fn getType(x) { return typeof x; }`,
// 		`fn getDefault(x, y): string { return x ?? y; }`,
// 		`fn throwError(x) { if x { throw "err"; } return 42; }`,
// 		`io.println(getType(42));`,
// 		`io.println(getDefault(null, "ok"));`,
// 		`try { throwError(true); } catch err {}`,
// 	}, "\n")
// 	mainPath := filepath.Join(dir, "main.tiny")
// 	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
// 		t.Fatalf("failed to write main.tiny: %v", err)
// 	}

// 	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
// 	tinyVM := vm.NewVM(mainInstructions, functions, classes, interfaces, globalIndex, false)

// 	val := reflect.ValueOf(tinyVM).Elem()
// 	jitFuncs := val.FieldByName("jitFunctions")
// 	if !jitFuncs.IsValid() {
// 		t.Fatalf("jitFunctions field not found on VM")
// 	}

// 	for _, fnName := range []string{"getType", "getDefault", "throwError"} {
// 		jitFn := jitFuncs.MapIndex(reflect.ValueOf(fnName))
// 		if !jitFn.IsValid() || jitFn.IsNil() {
// 			t.Fatalf("expected function %s to be JIT-compiled but it was not found in jitFunctions", fnName)
// 		}
// 	}

// }

func TestJitDefaultParameters(t *testing.T) {
	content := `
	fn interpNumeric(n: number, dummy = 0): number {
		let total = 0
		let i = 0
		while i < n {
			total = total + i
			i = i + 1
		}
		return total
	}
	`
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.tiny")
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	program := LoadProgram(filePath)
	compiler := NewCompiler()
	mainBytecode, functions, classes, interfaces, _ := compiler.CompileProgram(program)

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainBytecode,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
	})

	val := reflect.ValueOf(tinyVM).Elem()
	jitFuncs := val.FieldByName("jitFunctions")
	if !jitFuncs.IsValid() {
		t.Fatalf("jitFunctions field not found on VM")
	}

	jitFn := jitFuncs.MapIndex(reflect.ValueOf("interpNumeric"))
	if !jitFn.IsValid() || jitFn.IsNil() {
		t.Fatalf("expected interpNumeric to be JIT-compiled but it was not")
	}
}
