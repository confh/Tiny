package compiler

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"language.com/src/bytecode"
	tinyloader "language.com/src/loader"
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

	program := tinyloader.LoadProgram(path)
	compiler := NewCompiler()
	mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	return mainInstructions, functions, classes, interfaces, globalIndex
}

func init() {
	vm.SetRuntimeBytecodeLoader(func(data []byte) vm.RuntimeBytecodeProgram {
		mainInstructions, functions, classes, interfaces, globalIndex := bytecode.LoadBytecodeFromBytes(data)
		return vm.RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})

	vm.SetRuntimeSourceLoader(func(source string, file string) vm.RuntimeBytecodeProgram {
		lexer := vm.NewLexer(source, file)
		parser := vm.NewParser(lexer)
		program := parser.ParseProgram()

		compiler := NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = vm.OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return vm.RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})

	vm.SetCompileSourceFunc(func(source string, file string) []byte {
		lexer := vm.NewLexer(source, file)
		parser := vm.NewParser(lexer)
		program := parser.ParseProgram()

		compiler := NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = vm.OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	})

	vm.SetCompileFileFunc(func(path string) []byte {
		program := tinyloader.LoadProgram(path)

		compiler := NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = vm.OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	})
}

func writeTinyBytecodeFile(t *testing.T, sourcePath string, outPath string) {
	t.Helper()

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, sourcePath)
	bytecodeBytes := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	if err := os.WriteFile(outPath, bytecodeBytes, 0644); err != nil {
		t.Fatalf("write bytecode: %v", err)
	}
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

func TestRuntimeNewVMLoadBytecodeAndCall(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.tiny")
	childBytecodePath := filepath.Join(dir, "child.tbc")
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(childPath, []byte(strings.Join([]string{
		`export fn add(a: number, b: number): number {`,
		`    return a + b`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	writeTinyBytecodeFile(t, childPath, childBytecodePath)

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`embedbytes "child.tbc" const childBytes`,
		`const child = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: {}`,
		`})`,
		`child.loadBytecode(childBytes)`,
		`const result = child.call("add", [20, 22])`,
		`if result != 42 {`,
		`    throw "bad result"`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
}

func TestRuntimeNewVMLoadSource(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`const child = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: { io: true },`,
		`    runMainOnLoad: true`,
		`})`,
		`child.loadSource("import std \"io\" as io\nio.println('hey')")`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "hey" {
		t.Fatalf("expected stdout hey, got %q", result.Stdout)
	}
}

func TestRuntimeNewVMLoadSourcePreservesCallableFunctions(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`const child = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: { io: true },`,
		`    runMainOnLoad: true`,
		`})`,
		"child.loadSource(`import std \"io\"",
		"fn test() {",
		"    io.println(\"hey\")",
		"}",
		"`)",
		`child.call("test")`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "hey" {
		t.Fatalf("expected stdout hey, got %q", result.Stdout)
	}
}

func TestRuntimeNewVMExternalNamespaceFallsBackToExposedFunction(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")
	externalPath := filepath.Join(dir, "external.tiny")
	childPath := filepath.Join(dir, "child.tiny")
	childBytecodePath := filepath.Join(dir, "child.tbc")

	if err := os.WriteFile(externalPath, []byte(`export external fn log(...any: any): null`), 0644); err != nil {
		t.Fatalf("write external: %v", err)
	}
	if err := os.WriteFile(childPath, []byte(strings.Join([]string{
		`import std "http" as http`,
		`import "external.tiny" as External`,
		`export fn handleRequest() {`,
		`    let res = http.response(200, "ok")`,
		`    External.log("from external")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	writeTinyBytecodeFile(t, childPath, childBytecodePath)

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`import std "runtime" as runtime`,
		`embedbytes "child.tbc" const childBytes`,
		`const child = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: { http: true }`,
		`})`,
		`child.exposeFunction("log", fn (...r) {`,
		`    io.println(...r)`,
		`})`,
		`child.loadBytecode(childBytes)`,
		`child.run()`,
		`child.call("handleRequest")`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "from external" {
		t.Fatalf("expected stdout from external, got %q", result.Stdout)
	}
}

func TestRuntimeNewVMLoadBytecodeStringSourceCompatibility(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`const child = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: { io: true },`,
		`    runMainOnLoad: true`,
		`})`,
		`child.loadBytecode("import std \"io\" as io\nio.println('hey')")`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "hey" {
		t.Fatalf("expected stdout hey, got %q", result.Stdout)
	}
}

func TestRuntimeNewVMIsolatedStdlibGate(t *testing.T) {
	dir := t.TempDir()
	childPath := filepath.Join(dir, "child.tiny")
	childBytecodePath := filepath.Join(dir, "child.tbc")
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(childPath, []byte(strings.Join([]string{
		`import std "fs" as fs`,
		`export fn touchFs(): bool {`,
		`    return fs.exists("nope.txt")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write child: %v", err)
	}
	writeTinyBytecodeFile(t, childPath, childBytecodePath)

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`embedbytes "child.tbc" const childBytes`,
		`const child = runtime.newVM({ isolated: true })`,
		`child.loadBytecode(childBytes)`,
		`child.run()`,
		`child.call("touchFs", [])`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic == nil {
		t.Fatalf("expected denied stdlib panic")
	}
	langErr, ok := result.Panic.(tinyerrors.LangErrorType)
	if !ok {
		t.Fatalf("expected LangErrorType, got %#v", result.Panic)
	}
	if !strings.Contains(langErr.Message, "standard module 'fs' is not allowed") {
		t.Fatalf("expected stdlib denial, got %q", langErr.Message)
	}
}

func TestRuntimeNewVMCrashHandling(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`import std "io" as io`,
		`const child = runtime.newVM({ isolated: true, runMainOnLoad: true })`,
		`try {`,
		`    child.loadSource("throw 'child error'")`,
		`} catch e {`,
		`    io.println("parent caught: " + e.message)`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "parent caught: child error") || !strings.Contains(result.Stdout, "<runtime>:1:1") {
		t.Fatalf("expected stdout to contain 'parent caught: child error' and '<runtime>:1:1', got %q", result.Stdout)
	}
}

func TestRuntimeNewVMRunErrorIsCatchableByParent(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`import std "io" as io`,
		`const child = runtime.newVM({ runMainOnLoad: false })`,
		`child.loadSource("throw 'child run error'")`,
		`try {`,
		`    child.run()`,
		`} catch e {`,
		`    io.println("caught run: " + e.message)`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "caught run: child run error") || !strings.Contains(result.Stdout, "<runtime>:1:1") {
		t.Fatalf("expected parent to catch child run error with location, got %q", result.Stdout)
	}
}

func TestObserverDuplicateStartIsCatchable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		t.Fatalf("close listener: %v", err)
	}

	dir := t.TempDir()
	parentPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "observer" as observer`,
		`import std "io" as io`,
		fmt.Sprintf(`observer.start({ host: "127.0.0.1", port: %d })`, port),
		`try {`,
		fmt.Sprintf(`    observer.start({ host: "127.0.0.1", port: %d })`, port),
		`} catch e {`,
		`    io.println("caught")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected host panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "caught") {
		t.Fatalf("expected duplicate observer start to be catchable, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestTCPServerStartBindErrorIsCatchable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	defer listener.Close()

	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "net" as net`,
		`import std "io" as io`,
		fmt.Sprintf(`const server = net.tcpServer("127.0.0.1", %d)`, port),
		`try {`,
		`    server.start(true)`,
		`} catch e {`,
		`    io.println("caught")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected host panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "caught") {
		t.Fatalf("expected TCP bind error to be catchable, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestWebsocketServerStartBindErrorIsCatchable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	defer listener.Close()

	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "websocket" as websocket`,
		`import std "io" as io`,
		fmt.Sprintf(`const server = websocket.server({ host: "127.0.0.1", port: %d })`, port),
		`try {`,
		`    server.start(true)`,
		`} catch e {`,
		`    io.println("caught")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected host panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "caught") {
		t.Fatalf("expected websocket bind error to be catchable, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestRuntimeVMStopAllowsResetOfRunningTask(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`import std "time" as time`,
		`import std "io" as io`,
		`const child = runtime.newVM({ runMainOnLoad: false })`,
		`child.loadSource("import std \"time\" as time\nwhile true { time.sleep(1) }")`,
		`spawn() fn() { child.run() }`,
		`time.sleep(20)`,
		`child.stop()`,
		`try {`,
		`    child.reset()`,
		`    io.println("reset")`,
		`} catch e {`,
		`    io.println("failed")`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected host panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "reset") {
		t.Fatalf("expected stopped child VM to reset, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestPrivateFieldAssignmentAllowedInsideClassMethod(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`class Box {`,
		`    field private value = 0`,
		`    public fn set(v) {`,
		`        this.value = v`,
		`    }`,
		`    public fn get() {`,
		`        return this.value`,
		`    }`,
		`}`,
		`const box = Box()`,
		`box.set(42)`,
		`io.println(box.get())`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "42" {
		t.Fatalf("expected private field assignment to work inside method, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestPrivateFieldCompoundAssignmentAllowedInsideClassMethod(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`class Box {`,
		`    field private value = 1`,
		`    public fn inc(v) {`,
		`        this.value += v`,
		`    }`,
		`    public fn get() {`,
		`        return this.value`,
		`    }`,
		`}`,
		`const box = Box()`,
		`box.inc(41)`,
		`io.println(box.get())`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "42" {
		t.Fatalf("expected private field compound assignment to work inside method, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestPrivateFieldAssignmentAllowedInsideClassMethodFromObfuscatedBytecode(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`class Box {`,
		`    field private value = 0`,
		`    public fn set(v) {`,
		`        this.value = v`,
		`    }`,
		`    public fn get() {`,
		`        return this.value`,
		`    }`,
		`}`,
		`const box = Box()`,
		`box.set(42)`,
		`io.println(box.get())`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
	data := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, true)
	loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, loadedGlobalIndex := bytecode.LoadBytecodeFromBytes(data)

	result := runTinyBytecode(t, loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, loadedGlobalIndex)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "42" {
		t.Fatalf("expected private field assignment from obfuscated bytecode to work inside method, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestSoftKeywordsCanBeIdentifiers(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`const embed = fn(v) { return v + 1 }`,
		`const match = fn(v) { return v + 2 }`,
		`let value = embed(1) + match(1)`,
		`fn use(embed, match) { return embed + match }`,
		`io.println(value + use(1, 2))`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "8" {
		t.Fatalf("expected soft keyword identifiers to run, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestSoftKeywordGrammarStillWorks(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io" as io`,
		`class Logger { public fn value() { return 40 } }`,
		`class Service {`,
		`    embed logger`,
		`    public fn init() { this.logger = Logger() }`,
		`}`,
		`let out = 0`,
		`match 1 {`,
		`    1 { out = Service().value() + 2 }`,
		`    _ { out = -1 }`,
		`}`,
		`io.println(out)`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write main: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if strings.TrimSpace(result.Stdout) != "42" {
		t.Fatalf("expected match/embed grammar to still work, stdout=%q stderr=%q", result.Stdout, result.Stderr)
	}
}

func TestRuntimeNewVMStdlibRestrictions(t *testing.T) {
	dir := t.TempDir()
	parentPath := filepath.Join(dir, "parent.tiny")

	if err := os.WriteFile(parentPath, []byte(strings.Join([]string{
		`import std "runtime" as runtime`,
		`import std "io" as io`,
		`const child = runtime.newVM({ isolated: true, allowedStdlib: {}, runMainOnLoad: true })`,
		`try {`,
		`    child.loadSource("import std \"io\" as io\nfn calc() { io.println('hi'); }")`,
		`} catch e {`,
		`    io.println("parent caught: " + e.message)`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("write parent: %v", err)
	}

	result := runTinyFile(t, parentPath)
	if result.Panic != nil {
		t.Fatalf("unexpected panic: %#v", result.Panic)
	}
	if !strings.Contains(result.Stdout, "parent caught:") || !strings.Contains(result.Stdout, "standard module 'io' is not allowed") {
		t.Fatalf("expected stdout to show stdlib denial error, got %q", result.Stdout)
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
	all := append([]string{"..", "testdata", "tiny"}, parts...)
	return filepath.Join(all...)
}

func TestTinyPipelineArithmeticAndStrings(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("arithmetic.tiny")))

	const want = "7\nhello Tiny v1\nstring\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineDestructure(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("destructure.tiny")))

	const want = "Alice\n30\nNYC\n10\n20\n30\nAlice\n30\nAlice\n30\nBob\nLA\n"
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

func TestTinyPipelineObjectCannotForgeClassIdentity(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io";`,
		`class User {`,
		`    field name: string = "real";`,
		`}`,
		`fn accept(user: User) {`,
		`    io.println(user.name);`,
		`}`,
		`let fake = { __class: "User", name: "fake" };`,
		`io.println(fake instanceof User);`,
		`accept(fake);`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	result := runTinyFile(t, mainPath)
	if result.Stdout != "false\n" {
		t.Fatalf("unexpected stdout before type error: %q", result.Stdout)
	}
	requireTinyError(t, result, tinyerrors.ErrorType, "function accept parameter user expected User")
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

	const want = "4\n1\n1-2-3-4\nTiny\n15\nnull\n2,4,6,8\nTiny\nnull\n15\n1\n1\n"
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

func TestTinyPipelineReturnObjectLiteralAsObject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`import std "io"`,
		`export fn authHeaders(token: string): object {`,
		`    return {`,
		`        "Authorization": ` + "`Bot ${token}`" + `,`,
		`        "Content-Type": "application/json"`,
		`    }`,
		`}`,
		`let headers = authHeaders("abc")`,
		`io.println(headers.Authorization)`,
		`io.println(headers["Content-Type"])`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	out := requireTinySuccess(t, runTinyFile(t, path))
	const want = "Bot abc\napplication/json\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineOptionalInterfaceFieldChecksPresentValue(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`interface Payload { name?: string }`,
		`fn usePayload(payload: Payload) {}`,
		`usePayload({ name: 123 })`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	requireTinyError(t, runTinyFile(t, path), tinyerrors.ErrorType, "cannot pass")
}

func TestTinyPipelineInterfaceExtendsCompileTimeCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`interface Base { id: number }`,
		`interface User extends Base { name: string }`,
		`fn useUser(user: User) {}`,
		`useUser({ name: "Ada" })`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	requireTinyError(t, runTinyFile(t, path), tinyerrors.ErrorType, "cannot pass")
}

func TestTinyPipelineGenericInterfaceCompileTimeCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`interface Box:T { value: T }`,
		`fn useBox(box: Box:string) {}`,
		`useBox({ value: 123 })`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}

	requireTinyError(t, runTinyFile(t, path), tinyerrors.ErrorType, "cannot pass")
}

func TestTinyPipelineUninitializedVariablesAndFields(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`import std "io"`,
		`let count: number`,
		`class User {`,
		`    field name: string`,
		`}`,
		`let user = User()`,
		`io.println(count == null)`,
		`io.println(user.name == null)`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, path))
	const want = "true\ntrue\n"
	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineMissingObjectPropertiesReturnNull(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.tiny")
	source := strings.Join([]string{
		`import std "io"`,
		`let object = { name: "Tiny" }`,
		`class User {`,
		`    field name: string = "Ada"`,
		`}`,
		`let user = User()`,
		`io.println(object.age == null)`,
		`io.println(user.age == null)`,
	}, "\n")
	if err := os.WriteFile(path, []byte(source), 0644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, path))
	const want = "true\ntrue\n"
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

func TestTinyPipelineImportedNamespaceCanUseParentClassTypeHint(t *testing.T) {
	dir := t.TempDir()

	messagePath := filepath.Join(dir, "message.tiny")
	if err := os.WriteFile(messagePath, []byte(strings.Join([]string{
		`export class Message {`,
		`    field client: Client`,
		`    fn init(client: Client) {`,
		`        this.client = client`,
		`    }`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write message.tiny: %v", err)
	}

	gatewayPath := filepath.Join(dir, "gateway.tiny")
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "message.tiny" as MessageModule`,
		`export const Message = MessageModule.Message`,
		`export class Client {`,
		`    field name = "client"`,
		`}`,
		`export fn makeMessage(): MessageModule.Message {`,
		`    return MessageModule.Message(Client())`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write gateway.tiny: %v", err)
	}

	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io"`,
		`import "gateway.tiny" as Discord`,
		`const msg = Discord.makeMessage()`,
		`io.println(msg.client.name)`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "client\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineReExportedClassAliasConstructsAndCallsMethods(t *testing.T) {
	dir := t.TempDir()

	builderPath := filepath.Join(dir, "builder.tiny")
	if err := os.WriteFile(builderPath, []byte(strings.Join([]string{
		`export class Builder {`,
		`    field name = ""`,
		`    fn setName(name: string): Builder {`,
		`        this.name = name`,
		`        return this`,
		`    }`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write builder.tiny: %v", err)
	}

	gatewayPath := filepath.Join(dir, "gateway.tiny")
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "builder.tiny" as BuilderModule`,
		`export const Builder = BuilderModule.Builder`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write gateway.tiny: %v", err)
	}

	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(strings.Join([]string{
		`import std "io"`,
		`import "gateway.tiny" as Gateway`,
		`const builder = Gateway.Builder()`,
		`builder.setName("ping")`,
		`io.println(builder.name)`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "ping\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestRuntimeVMCallInitializesImportsBeforeExportedFunction(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "builder.tiny"), []byte(strings.Join([]string{
		`export class Builder {`,
		`    field name = ""`,
		`    fn setName(name: string): Builder {`,
		`        this.name = name`,
		`        return this`,
		`    }`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write builder.tiny: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "gateway.tiny"), []byte(strings.Join([]string{
		`import "builder.tiny" as BuilderModule`,
		`export const Builder = BuilderModule.Builder`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write gateway.tiny: %v", err)
	}

	commandPath := filepath.Join(dir, "command.tiny")
	if err := os.WriteFile(commandPath, []byte(strings.Join([]string{
		`import "gateway.tiny" as Gateway`,
		`export fn info() {`,
		`    const builder = Gateway.Builder()`,
		`    builder.setName("ping")`,
		`    return builder.name`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write command.tiny: %v", err)
	}

	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(fmt.Sprintf(strings.Join([]string{
		`import std "io"`,
		`import std "runtime"`,
		`const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })`,
		`child.loadBytecode(runtime.compileFile(%q))`,
		`io.println(child.call("info"))`,
	}, "\n"), filepath.ToSlash(commandPath))), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "ping\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestRuntimeVMReturnedReExportedClassAliasTypeCheck(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "builder.tiny"), []byte(strings.Join([]string{
		`export class Builder {`,
		`    field name = ""`,
		`    fn setName(name: string): Builder {`,
		`        this.name = name`,
		`        return this`,
		`    }`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write builder.tiny: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "gateway.tiny"), []byte(strings.Join([]string{
		`import "builder.tiny" as BuilderModule`,
		`export const Builder = BuilderModule.Builder`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write gateway.tiny: %v", err)
	}

	commandPath := filepath.Join(dir, "command.tiny")
	if err := os.WriteFile(commandPath, []byte(strings.Join([]string{
		`import "gateway.tiny" as Gateway`,
		`export fn info() {`,
		`    const builder = Gateway.Builder()`,
		`    builder.setName("ping")`,
		`    return builder`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatalf("failed to write command.tiny: %v", err)
	}

	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(fmt.Sprintf(strings.Join([]string{
		`import std "io"`,
		`import std "runtime"`,
		`import "gateway.tiny" as Gateway`,
		`const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })`,
		`child.loadBytecode(runtime.compileFile(%q))`,
		`const builder: Gateway.Builder = child.call("info")`,
		`io.println(builder.name)`,
	}, "\n"), filepath.ToSlash(commandPath))), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "ping\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
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

	data := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
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
	program := tinyloader.LoadProgram(mainPath)
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

func TestTinyPipelineInterfaceExtendsAndValidate(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "validate" as validate;`,
		`interface Entity {`,
		`    id: number`,
		`}`,
		`interface User extends Entity {`,
		`    name: string`,
		`}`,
		`fn printUser(user: User) {`,
		`    io.println(user.id);`,
		`    io.println(user.name);`,
		`}`,
		`let user = { id: 7, name: "Ada" };`,
		`printUser(user);`,
		`io.println(validate.interfaceOf(user, User));`,
		`io.println(validate.interfaceOf({ name: "Bad" }, User));`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "7\nAda\ntrue\nfalse\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineValidateNamespacedInterfaceValue(t *testing.T) {
	dir := t.TempDir()

	modelsContent := strings.Join([]string{
		`export interface Entity {`,
		`    id: number`,
		`}`,
		`export interface User extends Entity {`,
		`    name: string`,
		`}`,
	}, "\n")
	modelsPath := filepath.Join(dir, "models.tiny")
	if err := os.WriteFile(modelsPath, []byte(modelsContent), 0644); err != nil {
		t.Fatalf("failed to write models.tiny: %v", err)
	}

	mainContent := strings.Join([]string{
		`import std "io" as io;`,
		`import std "validate" as validate;`,
		`import "./models.tiny" as models;`,
		`let user = { id: 7, name: "Ada" };`,
		`io.println(validate.interfaceOf(user, models.User));`,
		`io.println(validate.interfaceOf({ name: "Bad" }, models.User));`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	mainInstructions, functions, classes, interfaces, globalIndex := compileTinyFile(t, mainPath)
	data := bytecode.SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, loadedGlobalIndex := bytecode.LoadBytecodeFromBytes(data)
	out := requireTinySuccess(t, runTinyBytecode(t, loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, loadedGlobalIndex))
	const want = "true\nfalse\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineStdModuleEnumProperty(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "crypto";`,
		`import std "io";`,
		`io.println(crypto.Algorithms.MD5);`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "md5\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineTimeParseUnix(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io";`,
		`import std "time";`,
		`const timestamp = "2026-07-01T10:00:08.972000+00:00";`,
		`io.println(time.parseUnix(timestamp, "sec").toString());`,
		`io.println(time.parseUnix(timestamp, time.TimeUnit.Milliseconds).toString());`,
		`io.println(time.parseUnix(timestamp, time.TimeUnit.Nanoseconds).toString());`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "1782900008\n1782900008972\n1782900008972000000\n"
	if out != want {
		t.Fatalf("unexpected output: want %q, got %q", want, out)
	}
}

func TestTinyPipelineLogicalOperatorsShortCircuit(t *testing.T) {
	dir := t.TempDir()

	mainContent := strings.Join([]string{
		`import std "io";`,
		`fn mark(label: string): bool {`,
		`    io.println(label);`,
		`    return true;`,
		`}`,
		`io.println(false and mark("and-skipped"));`,
		`io.println(true or mark("or-skipped"));`,
		`io.println(true and mark("and-run"));`,
		`io.println(false or mark("or-run"));`,
	}, "\n")
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}

	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "false\ntrue\nand-run\ntrue\nor-run\ntrue\n"
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

func TestJitStringJoin(t *testing.T) {
	tests := []struct {
		name string
		code string
		want string
	}{
		{
			name: "interpolation 2 parts",
			code: `
import std "io"
fn greet(name: string): string {
    return "hello " + name + "!"
}
io.println(greet("world"))
io.println(greet("jit"))
`,
			want: "hello world!\nhello jit!\n",
		},
		{
			name: "interpolation 3 parts",
			code: `
import std "io"
fn fmt(a: string, b: string, c: string): string {
    return a + b + c
}
io.println(fmt("[", "test", "]"))
`,
			want: "[test]\n",
		},
		{
			name: "interpolation 4 parts",
			code: `
import std "io"
fn fmt(a: string, b: string, c: string, d: string): string {
    return a + b + c + d
}
io.println(fmt("a", "b", "c", "d"))
`,
			want: "abcd\n",
		},
		{
			name: "string + number",
			code: `
import std "io"
fn show(n: number): string {
    return "count=" + n
}
io.println(show(42))
io.println(show(0))
`,
			want: "count=42\ncount=0\n",
		},
		{
			name: "string in loop",
			code: `
import std "io"
fn build(n: number): string {
    let result = ""
    let i = 0
    while i < n {
        result = result + "x"
        i = i + 1
    }
    return result
}
io.println(build(5))
io.println(build(0))
io.println(build(1))
`,
			want: "xxxxx\n\nx\n",
		},
		{
			name: "string concat with bool",
			code: `
import std "io"
fn showFlag(b: bool): string {
    return "flag=" + b
}
io.println(showFlag(true))
io.println(showFlag(false))
`,
			want: "flag=true\nflag=false\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			mainPath := filepath.Join(dir, "main.tiny")
			if err := os.WriteFile(mainPath, []byte(tt.code), 0644); err != nil {
				t.Fatalf("failed to write main.tiny: %v", err)
			}
			out := requireTinySuccess(t, runTinyFile(t, mainPath))
			if out != tt.want {
				t.Fatalf("unexpected output:\n  want: %q\n  got:  %q", tt.want, out)
			}
		})
	}
}

func TestJitStringJoinLoopCarried(t *testing.T) {
	code := `
import std "io"
fn joinWords(words: array): string {
    let result = ""
    let i = 0
    while i < words.length {
        result = result + words[i]
        if i < words.length - 1 {
            result = result + " "
        }
        i = i + 1
    }
    return result
}
	let words = ["hello", "world", "!"]
	io.println(joinWords(words))
	let empty = ["a", "b", "c"]
	io.println(joinWords(empty))
`
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	if err := os.WriteFile(mainPath, []byte(code), 0644); err != nil {
		t.Fatalf("failed to write main.tiny: %v", err)
	}
	out := requireTinySuccess(t, runTinyFile(t, mainPath))
	const want = "hello world !\na b c\n"
	if out != want {
		t.Fatalf("unexpected output:\n  want: %q\n  got:  %q", want, out)
	}
}

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

	program := tinyloader.LoadProgram(filePath)
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

func TestRuntimeCompileSourceAndFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Helper source file that compileFile will load
	helperFile := filepath.Join(tmpDir, "helper.tiny")
	helperContent := `
	export fn helperVal() {
		return 42;
	}
	`
	if err := os.WriteFile(helperFile, []byte(helperContent), 0644); err != nil {
		t.Fatalf("failed to write helper file: %v", err)
	}

	escapedHelperPath := strings.ReplaceAll(helperFile, "\\", "\\\\")

	// Main test file
	mainContent := fmt.Sprintf(`
	import std "runtime" as runtime
	import std "io" as io

	// 1. Test compileSource
	const source1 = "import std \"io\"\nfn hello() { return 'hello world'; }"
	const bytecode1 = runtime.compileSource(source1)

	const child1 = runtime.newVM()
	child1.loadBytecode(bytecode1)
	const result1 = child1.call("hello")
	io.println(result1)

	// 2. Test compileSource compilation error
	try {
		runtime.compileSource("fn bad() { return x; }") // undefined variable x
		io.println("no compilation error")
	} catch err {
		io.println("caught: " + err.message)
	}

	// 3. Test compileFile
	const bytecode2 = runtime.compileFile("%s")
	const child2 = runtime.newVM()
	child2.loadBytecode(bytecode2)
	const result2 = child2.call("helperVal")
	io.println(result2)

	// 4. Test compileFile sandbox restriction (disallowed fs)
	const child3 = runtime.newVM({
		isolated: true,
		allowedStdlib: { fs: false },
		runMainOnLoad: true
	})

	try {
		child3.loadSource("import std \"runtime\" as runtime\nfn test(path) { runtime.compileFile(path); }")
		child3.call("test", ["%s"])
		io.println("no sandbox error")
	} catch err {
		io.println("sandbox caught: " + err.message)
	}
	`, escapedHelperPath, escapedHelperPath)

	mainFile := filepath.Join(tmpDir, "main.tiny")
	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}

	stdout := res.Stdout
	if !strings.Contains(stdout, "hello world") {
		t.Errorf("expected stdout to contain 'hello world', got: %q", stdout)
	}
	if !strings.Contains(stdout, "caught:") {
		t.Errorf("expected stdout to contain 'caught:', got: %q", stdout)
	}
	if !strings.Contains(stdout, "42") {
		t.Errorf("expected stdout to contain '42', got: %q", stdout)
	}
	if !strings.Contains(stdout, "sandbox caught:") || !strings.Contains(stdout, "fs") {
		t.Errorf("expected stdout to contain 'sandbox caught:' with 'fs' module message, got: %q", stdout)
	}
}

func TestRuntimeVMCallbackKeepsOwningVM(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`import std "io" as io`,
		`export fn register(store) {`,
		`    store(fn(i, client) {`,
		`        io.println("child callback")`,
		`    })`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "io" as io
import std "runtime" as runtime

const state = {}

fn parentCollision(a, b) {
    io.println(b)
}

const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
child.call("register", [fn(handler) { state.handler = handler }])
state.handler({}, {"huge": "client"})
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
	if strings.Contains(res.Stdout, "huge") {
		t.Fatalf("expected child callback to run in child VM, got stdout:\n%s", res.Stdout)
	}
	if strings.TrimSpace(res.Stdout) != "child callback" {
		t.Fatalf("expected child callback output, got stdout:\n%s", res.Stdout)
	}
}

func TestRuntimeVMWrappingDoesNotMutateParentInstanceMethods(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`export fn register(client) {`,
		`    client.store(fn() { return "child callback" })`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "io" as io
import std "runtime" as runtime

let stored = null

class Client {
    fn store(handler) {
        stored = handler
    }

    fn handlePayload() {
        io.println("parent handlePayload")
    }
}

const client = Client()
const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
child.call("register", [client])
client.handlePayload()
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
	if strings.TrimSpace(res.Stdout) != "parent handlePayload" {
		t.Fatalf("expected parent instance method to remain callable, got stdout:\n%s", res.Stdout)
	}
}

func TestRuntimeVMChildCanCallParentInteractionMethod(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`export fn run(interaction) {`,
		`    interaction.replyComponents(["ping"])`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "io" as io
import std "runtime" as runtime

class Interaction {
    field responded = false

    fn replyComponents(components) {
        this.responded = true
    }
}

const interaction = Interaction()
const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
child.call("run", [interaction])
io.println(interaction.responded)
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
	if strings.TrimSpace(res.Stdout) != "true" {
		t.Fatalf("expected child VM to call parent interaction method, got stdout:\n%s", res.Stdout)
	}
}

func TestRuntimeVMAssignedInstanceFunctionFieldDoesNotReceiveThis(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`export fn run(client) {`,
		`    client.tempButton(1, "label", fn() {})`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "io" as io
import std "runtime" as runtime

class Client {
    field tempButton = null
}

const client = Client()
client.tempButton = fn(style, label, handler) {
    io.println(label)
}

const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
child.call("run", [client])
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
	if strings.TrimSpace(res.Stdout) != "label" {
		t.Fatalf("expected assigned function field to be called without hidden receiver, got stdout:\n%s", res.Stdout)
	}
}

func TestRuntimeVMHostFunctionSatisfiesFunctionTypeHint(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`export fn run(register) {`,
		`    register(fn(value) { return value })`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "runtime" as runtime

fn accept(handler: function(string)) {
}

const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
child.call("run", [accept])
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
}

func TestRuntimeVMChildCanCatchHostFunctionError(t *testing.T) {
	tmpDir := t.TempDir()
	mainFile := filepath.Join(tmpDir, "main.tiny")

	childSource := strings.Join([]string{
		`export fn run(fail) {`,
		`    try {`,
		`        fail()`,
		`    } catch err {`,
		`        return "caught: " + err.kind`,
		`    }`,
		`    return "missed"`,
		`}`,
	}, "\n")

	mainContent := fmt.Sprintf(`
import std "io" as io
import std "runtime" as runtime

fn fail() {
    throw "boom"
}

const child = runtime.newVM({ runMainOnLoad: false, disableJIT: true })
child.loadBytecode(runtime.compileSource(%q, "child.tiny"))
io.println(child.call("run", [fail]))
`, childSource)

	if err := os.WriteFile(mainFile, []byte(mainContent), 0644); err != nil {
		t.Fatalf("failed to write main file: %v", err)
	}

	res := runTinyFile(t, mainFile)
	if res.Panic != nil {
		t.Fatalf("unexpected panic during test run: %v", res.Panic)
	}
	if strings.TrimSpace(res.Stdout) != "caught: Error" {
		t.Fatalf("expected child VM to catch host function error, got stdout:\n%s", res.Stdout)
	}
}

func TestCompileDiagnosticCollectsErrors(t *testing.T) {
	src := "let x = 1\nlet x = 2"
	lexer := vm.NewLexer(src, "test.tiny")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()

	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)

	if len(model.Errors) == 0 {
		t.Fatal("expected at least one diagnostic error, got none")
	}

	found := false
	for _, err := range model.Errors {
		if strings.Contains(err.Message, "already declared") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected 'already declared' error, got errors: %v", model.Errors)
	}
}

func TestCompileDiagnosticNoErrorsOnValidCode(t *testing.T) {
	lexer := vm.NewLexer(`fn add(x: number, y: number): number { return x + y }`, "test.tiny")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()

	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)

	if len(model.Errors) != 0 {
		t.Fatalf("expected no errors on valid code, got: %v", model.Errors)
	}
}

func TestCompileDiagnosticReturnsFunctions(t *testing.T) {
	lexer := vm.NewLexer(`fn add(x: number, y: number): number { return x + y }`, "test.tiny")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()

	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)

	if _, ok := model.Functions["add"]; !ok {
		t.Fatal("expected 'add' function in semantic model")
	}
}

func TestCompileDiagnosticReturnsClasses(t *testing.T) {
	src := "class Box {\n    field private value = 0\n    public fn get() {\n        return this.value\n    }\n}"
	lexer := vm.NewLexer(src, "test.tiny")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()

	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)

	if _, ok := model.Classes["Box"]; !ok {
		t.Fatal("expected 'Box' class in semantic model")
	}
}

func TestCompileDiagnosticReturnsInterfaces(t *testing.T) {
	src := "interface Greeter {\n    greet: function\n}"
	lexer := vm.NewLexer(src, "test.tiny")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgram()

	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)

	if _, ok := model.Interfaces["Greeter"]; !ok {
		t.Fatal("expected 'Greeter' interface in semantic model")
	}
}
