package vm

import (
	"testing"
)

func parseProgramForRangeTest(t *testing.T, code string) Program {
	t.Helper()
	return parseSourceForTest(t, code)
}

func findStmt[T any](prog Program) T {
	for _, stmt := range prog.Statements {
		if s, ok := stmt.(T); ok {
			return s
		}
	}
	var zero T
	return zero
}

func TestImportRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `import "foo.tiny" as foo`)
	stmt := findStmt[ImportStmt](prog)
	if stmt.Range.Start.Line == 0 {
		t.Fatal("import range not populated")
	}
	if stmt.PathRange.Start.Line == 0 {
		t.Fatal("import path range not populated")
	}
	if stmt.AliasRange.Start.Line == 0 {
		t.Fatal("import alias range not populated")
	}
	if stmt.Range.Start.Column != 1 {
		t.Fatalf("expected import range start col 1, got %d", stmt.Range.Start.Column)
	}
}

func TestStdImportRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `import std "http" as http`)
	stmt := findStmt[ImportStmt](prog)
	if stmt.Range.Start.Line == 0 {
		t.Fatal("std import range not populated")
	}
	if stmt.PathRange.Start.Line == 0 {
		t.Fatal("std import path range not populated")
	}
	if stmt.AliasRange.Start.Line == 0 {
		t.Fatal("std import alias range not populated")
	}
}

func TestFunctionStmtRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `fn add(a: number, b: number): number {
  return a + b
}`)
	stmt := findStmt[FunctionStmt](prog)
	if stmt.Range.Start.Line == 0 {
		t.Fatal("function range not populated")
	}
	if stmt.NameRange.Start.Line == 0 {
		t.Fatal("function name range not populated")
	}
	if stmt.ParamsRange.Start.Line == 0 {
		t.Fatal("function params range not populated")
	}
	if stmt.ReturnTypeRange.Start.Line == 0 {
		t.Fatal("function return type range not populated")
	}
	if stmt.NameRange.Start.Column != 4 {
		t.Fatalf("expected name col 4, got %d", stmt.NameRange.Start.Column)
	}
}

func TestFunctionParamRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `fn greet(name: string): void {}`)
	stmt := findStmt[FunctionStmt](prog)
	if len(stmt.Params) == 0 {
		t.Fatal("expected at least 1 param")
	}
	param := stmt.Params[0]
	if param.Range.Start.Line == 0 {
		t.Fatal("param range not populated")
	}
	if param.NameRange.Start.Line == 0 {
		t.Fatal("param name range not populated")
	}
	if param.TypeRange.Start.Line == 0 {
		t.Fatal("param type range not populated")
	}
	if param.NameRange.Start.Column != 10 {
		t.Fatalf("expected param name col 10, got %d", param.NameRange.Start.Column)
	}
}

func TestInterfaceRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `interface Person {
  name: string
  age: number
}`)
	stmt := findStmt[InterfaceStmt](prog)
	if stmt.Range.Start.Line == 0 {
		t.Fatal("interface range not populated")
	}
	if stmt.NameRange.Start.Line == 0 {
		t.Fatal("interface name range not populated")
	}
	if len(stmt.FieldRanges) != 2 {
		t.Fatalf("expected 2 field ranges, got %d", len(stmt.FieldRanges))
	}
	if stmt.FieldNameRanges["name"].Start.Line == 0 {
		t.Fatal("name field range not populated")
	}
	if stmt.FieldTypeRanges["name"].Start.Line == 0 {
		t.Fatal("name field type range not populated")
	}
	if stmt.FieldNameRanges["age"].Start.Line == 0 {
		t.Fatal("age field range not populated")
	}
}

func TestObjectExprRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `let p = { name: "John", age: 30 }`)
	stmt := findStmt[VariableStmt](prog)
	if stmt.Value == nil {
		t.Fatal("expected var value")
	}
	obj, ok := stmt.Value.(ObjectExpr)
	if !ok {
		t.Fatalf("expected ObjectExpr, got %T", stmt.Value)
	}
	if obj.Range.Start.Line == 0 {
		t.Fatal("object range not populated")
	}
	if len(obj.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(obj.Fields))
	}
	for _, f := range obj.Fields {
		if f.Range.Start.Line == 0 {
			t.Fatalf("field %s range not populated", f.Name)
		}
		if f.NameRange.Start.Line == 0 {
			t.Fatalf("field %s name range not populated", f.Name)
		}
	}
}

func TestPropertyExprRange(t *testing.T) {
	prog := parseProgramForRangeTest(t, `let x = obj.name`)
	stmt := findStmt[VariableStmt](prog)
	if stmt.Value == nil {
		t.Fatal("expected var value")
	}
	prop, ok := stmt.Value.(PropertyExpr)
	if !ok {
		t.Fatalf("expected PropertyExpr, got %T", stmt.Value)
	}
	if prop.Range.Start.Line == 0 {
		t.Fatal("property range not populated")
	}
	if prop.Name != "name" {
		t.Fatalf("expected property name 'name', got '%s'", prop.Name)
	}
}
