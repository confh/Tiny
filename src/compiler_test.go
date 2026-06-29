package main

import (
	"reflect"
	"testing"

	. "language.com/src/vm"
)

func TestSortedNativeImportsDeterministic(t *testing.T) {
	imports := map[string]bool{
		`"strings"`: true,
		`"fmt"`:     true,
		`"math"`:    true,
	}

	want := []string{`"fmt"`, `"math"`, `"strings"`}

	for i := 0; i < 20; i++ {
		got := sortedNativeImports(imports)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("sortedNativeImports() = %#v, want %#v", got, want)
		}
	}
}

func TestCompilerExportsExternalDeclarations(t *testing.T) {
	source := `
export external fn hostCall(input: string): number
export external const hostValue: string
`
	parser := NewParser(NewLexer(source, "test.tiny"))
	program := parser.ParseProgram()

	compiler := NewCompiler()
	_, _, _, _, globalIndex := compiler.CompileProgram(program)

	if _, ok := globalIndex["hostCall"]; !ok {
		t.Fatalf("expected exported external function global")
	}
	if _, ok := compiler.externalFunctions["hostCall"]; !ok {
		t.Fatalf("expected external function metadata")
	}
	if _, ok := globalIndex["hostValue"]; !ok {
		t.Fatalf("expected exported external global")
	}
}

func TestCompilerNamespaceExportsExternalDeclarations(t *testing.T) {
	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Host",
				Statements: []Stmt{
					ExportStmt{Inner: ExternalFnStmt{
						Name:       "call",
						Params:     []Param{{Name: "input", TypeHint: TypeHint{Name: "string"}}},
						ReturnType: TypeHint{Name: "number"},
					}},
					ExportStmt{Inner: ExternalGlobalStmt{
						Name: "value",
						Type: TypeHint{Name: "string"},
					}},
				},
			},
		},
	}

	compiler := NewCompiler()
	_, _, _, _, globalIndex := compiler.CompileProgram(program)

	if _, ok := globalIndex["Host.call"]; !ok {
		t.Fatalf("expected namespaced external function global")
	}
	if _, ok := compiler.externalFunctions["Host.call"]; !ok {
		t.Fatalf("expected namespaced external function metadata")
	}
	if _, ok := globalIndex["Host.value"]; !ok {
		t.Fatalf("expected namespaced external global")
	}
}
