package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"

	tinycompiler "language.com/src/compiler"
	tinyloader "language.com/src/loader"
	"language.com/src/vm"
)

type rootTinyRunResult struct {
	Stdout string
	Panic  any
}

func runTinyFile(t *testing.T, path string, args ...string) (res rootTinyRunResult) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			res.Panic = r
		}
	}()

	program := tinyloader.LoadProgram(path)
	compiler := tinycompiler.NewCompiler()
	mainInstructions, _, functions, classes, interfaces, _ := compiler.CompileProgram(program)
	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
	})
	tinyVM.SetCLIArgs(args)
	tinyVM.Run()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	res.Stdout = out.String()
	return res
}

func runTinyCode(t *testing.T, code string, args ...string) (res rootTinyRunResult) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			res.Panic = r
		}
	}()

	lexer := vm.NewLexer(code, "<test>")
	parser := vm.NewParser(lexer)
	program := parser.ParseProgramTolerant()

	compiler := tinycompiler.NewCompiler()
	mainInstructions, _, functions, classes, interfaces, _ := compiler.CompileProgram(vm.Program{Statements: program.Statements})
	mainInstructions = vm.OptimizeBytecode(mainInstructions)
	for name, fn := range functions {
		fn.Instructions = vm.OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	oldStdout := os.Stdout
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = writer
	defer func() {
		os.Stdout = oldStdout
	}()

	tinyVM := vm.NewVM(vm.VMInfo{
		MainInstructions: mainInstructions,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
	})
	tinyVM.SetCLIArgs(args)
	tinyVM.Run()

	if err := writer.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	var out bytes.Buffer
	if _, err := io.Copy(&out, reader); err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	res.Stdout = out.String()
	return res
}

func requireTinySuccess(t *testing.T, result rootTinyRunResult) string {
	t.Helper()
	if result.Panic != nil {
		t.Fatalf("Tiny program panicked: %v", result.Panic)
	}
	return result.Stdout
}

func fixturePath(parts ...string) string {
	all := append([]string{"testdata", "tiny"}, parts...)
	return filepath.Join(all...)
}
