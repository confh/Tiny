package compiler

import (
	"os"
	"reflect"
	"testing"

	. "language.com/src/tinyerrors"
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
	_, _, _, _, _, globalIndex := compiler.CompileProgram(program)

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

func TestCompilerNamedSpreadCall(t *testing.T) {
	source := `
fn collect(...values) {
    return values;
}

let values = [1, 2, 3];
collect("prefix", ...values);
`
	parser := NewParser(NewLexer(source, "test.tiny"))
	program := parser.ParseProgram()

	compiler := NewCompiler()
	mainInstructions, _, _, _, _, _ := compiler.CompileProgram(program)

	found := false
	for _, instr := range mainInstructions {
		if instr.Op == OP_CALL_VALUE_SPREAD {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected named spread call to emit OP_CALL_VALUE_SPREAD, got %#v", mainInstructions)
	}
}

func TestCompilerStdPrintSpreadCall(t *testing.T) {
	source := "import std \"io\";\n\n" +
		"fn log(workerName: string, ...r: any) {\n" +
		"    io.println(`[FROM: ${workerName}]`, ...r);\n" +
		"}\n\n" +
		"log(\"worker\", \"hello\", \"world\");\n"
	parser := NewParser(NewLexer(source, "test.tiny"))
	program := parser.ParseProgram()

	compiler := NewCompiler()
	_, _, functions, _, _, _ := compiler.CompileProgram(program)

	logFn, ok := functions["log"]
	if !ok {
		t.Fatalf("expected log function to compile")
	}

	found := false
	for _, instr := range logFn.Instructions {
		info, ok := instr.Value.(PrintInfo)
		if instr.Op == OP_PRINT && ok && len(info.SpreadArgs) == 2 && !info.SpreadArgs[0] && info.SpreadArgs[1] {
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected io.println spread call to emit OP_PRINT with spread metadata, got %#v", logFn.Instructions)
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
	_, _, _, _, _, globalIndex := compiler.CompileProgram(program)

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

func TestCompilerNamespaceImportResolution(t *testing.T) {
	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Testing",
				Statements: []Stmt{
					ImportStmt{
						Path:   "http",
						Std:    true,
						Alias:  "http",
						File:   "testing.tiny",
						Line:   1,
						Column: 1,
					},
					ExportStmt{
						Inner: VariableStmt{
							Name:     "ass",
							Constant: true,
							Value: MemberCallExpr{
								Object: IdentExpr{Name: "http", File: "testing.tiny", Line: 2, Column: 20},
								Method: "server",
								Args:   []Expr{NumberExpr{Value: 3000, File: "testing.tiny", Line: 2, Column: 32}},
								File:   "testing.tiny",
								Line:   2,
								Column: 20,
							},
							File:   "testing.tiny",
							Line:   2,
							Column: 14,
						},
						File:   "testing.tiny",
						Line:   2,
						Column: 1,
					},
				},
				File:   "main.tiny",
				Line:   1,
				Column: 1,
			},
		},
	}

	compiler := NewCompiler()
	// This should not panic or trigger semantic errors like "undefined variable: http"
	_, _, _, _, _, globalIndex := compiler.CompileProgram(program)

	if _, ok := globalIndex["Testing.ass"]; !ok {
		t.Fatalf("expected Testing.ass to be compiled successfully")
	}
}

func TestCompilerNamespaceEmbedAndExternalResolution(t *testing.T) {
	// Create dummy file for embedding
	err := os.WriteFile("alfredSecret.txt", []byte("secret-token"), 0644)
	if err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}
	defer os.Remove("alfredSecret.txt")

	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Testing",
				Statements: []Stmt{
					ExportStmt{
						Inner: EmbedStmt{
							Kind:         EmbedText,
							Name:         "authToken",
							EmbeddedPath: "alfredSecret.txt",
							Constant:     true,
							TypeHint:     TypeHint{Name: "string"},
							File:         "testing.tiny",
							Line:         1,
							Column:       1,
						},
						File:   "testing.tiny",
						Line:   1,
						Column: 1,
					},
					ExportStmt{
						Inner: InterfaceStmt{
							Name:   "IVal",
							Fields: map[string]TypeHint{"val": {Name: "string"}},
							File:   "testing.tiny",
							Line:   2,
							Column: 1,
						},
						File:   "testing.tiny",
						Line:   2,
						Column: 1,
					},
					ExportStmt{
						Inner: ExternalGlobalStmt{
							Name:   "hostGlobal",
							Type:   TypeHint{Name: "number"},
							File:   "testing.tiny",
							Line:   3,
							Column: 1,
						},
						File:   "testing.tiny",
						Line:   3,
						Column: 1,
					},
				},
				File:   "main.tiny",
				Line:   1,
				Column: 1,
			},
		},
	}

	compiler := NewCompiler()
	_, _, _, _, _, globalIndex := compiler.CompileProgram(program)

	if _, ok := globalIndex["Testing.authToken"]; !ok {
		t.Fatalf("expected Testing.authToken to be compiled successfully")
	}
	if _, ok := globalIndex["Testing.hostGlobal"]; !ok {
		t.Fatalf("expected Testing.hostGlobal to be compiled successfully")
	}
}

func TestCompilerNamespacePrivateEnforcement(t *testing.T) {
	// Program has a namespace Testing with export ass and private secret.
	// We try to access Testing.secret from the top level, which should trigger an undefined variable error.
	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Testing",
				Statements: []Stmt{
					ExportStmt{
						Inner: VariableStmt{
							Name:     "ass",
							Constant: true,
							Value:    NumberExpr{Value: 10, File: "testing.tiny", Line: 1, Column: 17},
							File:     "testing.tiny",
							Line:     1,
							Column:   14,
						},
						File:   "testing.tiny",
						Line:   1,
						Column: 1,
					},
					VariableStmt{
						Name:     "secret",
						Constant: true,
						Value:    NumberExpr{Value: 42, File: "testing.tiny", Line: 2, Column: 17},
						File:     "testing.tiny",
						Line:     2,
						Column:   14,
					},
				},
				File:   "main.tiny",
				Line:   1,
				Column: 1,
			},
			ExprStmt{
				Value: MemberCallExpr{
					Object: IdentExpr{Name: "Testing", File: "main.tiny", Line: 4, Column: 1},
					Method: "secret", // secret is private!
					File:   "main.tiny",
					Line:   4,
					Column: 1,
				},
			},
		},
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected compilation to fail because 'Testing.secret' is private")
		} else {
			err, ok := r.(LangErrorType)
			if !ok {
				t.Fatalf("expected LangErrorType, got %T", r)
			}
			if err.File != "main.tiny" {
				t.Errorf("expected File to be 'main.tiny', got '%s'", err.File)
			}
			if err.Line != 4 {
				t.Errorf("expected Line to be 4, got %d", err.Line)
			}
			if err.Column != 1 {
				t.Errorf("expected Column to be 1, got %d", err.Column)
			}
		}
	}()

	compiler := NewCompiler()
	compiler.CompileProgram(program)
}

func TestCompilerNamespaceNestedDestructure(t *testing.T) {
	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Parent",
				Statements: []Stmt{
					NamespaceStmt{
						Name: "Child",
						Statements: []Stmt{
							ExportStmt{
								Inner: DestructureStmt{
									Target: ArrayDestructurePattern{
										Elements: []ArrayDestructureElement{
											{Name: "valX"},
											{Name: "valY"},
										},
									},
									Value: ArrayExpr{
										Elements: []Expr{
											NumberExpr{Value: 100, File: "testing.tiny", Line: 2, Column: 28},
											NumberExpr{Value: 200, File: "testing.tiny", Line: 2, Column: 33},
										},
									},
									Constant: true,
									File:     "testing.tiny",
									Line:     2,
									Column:   14,
								},
								File:   "testing.tiny",
								Line:   2,
								Column: 1,
							},
						},
						File:   "testing.tiny",
						Line:   1,
						Column: 1,
					},
				},
				File:   "main.tiny",
				Line:   1,
				Column: 1,
			},
		},
	}

	compiler := NewCompiler()
	_, _, _, _, _, globalIndex := compiler.CompileProgram(program)

	if _, ok := globalIndex["Parent.Child.valX"]; !ok {
		t.Fatalf("expected Parent.Child.valX to be compiled successfully")
	}
}
