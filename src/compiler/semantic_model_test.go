package compiler

import (
	"testing"

	. "language.com/src/vm"
)

func compileForModel(t *testing.T, code string) SemanticModel {
	t.Helper()
	lexer := NewLexer(code, "test.tiny")
	parser := NewParser(lexer)
	program := parser.ParseProgram()
	c := NewCompiler()
	c.SetDiagnosticMode(true)
	model := c.CompileDiagnostic(program)
	return model
}

func TestMemberTypeClassField(t *testing.T) {
	model := compileForModel(t, `class Person {
  field name: string
  field age: number
}`)
	member, ok := model.MemberType("Person", "name")
	if !ok {
		t.Fatal("expected to find member 'name' on Person")
	}
	if member.Type != "string" {
		t.Fatalf("expected type 'string', got '%s'", member.Type)
	}
	if member.Kind != "property" {
		t.Fatalf("expected kind 'property', got '%s'", member.Kind)
	}
}

func TestMemberTypeClassMethod(t *testing.T) {
	model := compileForModel(t, `class Greeter {
  fn greet(name: string): string {
    return name
  }
}`)
	member, ok := model.MemberType("Greeter", "greet")
	if !ok {
		t.Fatal("expected to find method 'greet' on Greeter")
	}
	if member.Kind != "method" {
		t.Fatalf("expected kind 'method', got '%s'", member.Kind)
	}
	if len(member.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(member.Params))
	}
	if member.ReturnType.Name != "string" {
		t.Fatalf("expected return type 'string', got '%s'", member.ReturnType.Name)
	}
}

func TestMemberTypeInterfaceField(t *testing.T) {
	model := compileForModel(t, `interface Logger {
  level: number
}`)
	member, ok := model.MemberType("Logger", "level")
	if !ok {
		t.Fatal("expected to find field 'level' on Logger")
	}
	if member.Type != "number" {
		t.Fatalf("expected type 'number', got '%s'", member.Type)
	}
}

func TestMemberTypeBuiltinString(t *testing.T) {
	model := compileForModel(t, `let x = 1`)
	member, ok := model.MemberType("string", "length")
	if !ok {
		t.Fatal("expected to find 'length' on string")
	}
	if member.Type != "number" {
		t.Fatalf("expected type 'number', got '%s'", member.Type)
	}
}

func TestMemberTypeBuiltinArray(t *testing.T) {
	model := compileForModel(t, `let x = 1`)
	member, ok := model.MemberType("array<string>", "length")
	if !ok {
		t.Fatal("expected to find 'length' on array")
	}
	if member.Type != "number" {
		t.Fatalf("expected type 'number', got '%s'", member.Type)
	}
}

func TestMemberTypeNotFound(t *testing.T) {
	model := compileForModel(t, `class Foo {}`)
	_, ok := model.MemberType("Foo", "nonexistent")
	if ok {
		t.Fatal("expected member not found")
	}
}

func TestGetFunctionSignature(t *testing.T) {
	model := compileForModel(t, `fn add(a: number, b: number): number {
  return a + b
}`)
	fn, ok := model.GetFunctionSignature("add")
	if !ok {
		t.Fatal("expected to find function 'add'")
	}
	if len(fn.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fn.Params))
	}
	if fn.ReturnType.Name != "number" {
		t.Fatalf("expected return type 'number', got '%s'", fn.ReturnType.Name)
	}
}

func TestGetClass(t *testing.T) {
	model := compileForModel(t, `class Person {
  field name: string
  field age: number
}`)
	cls, ok := model.GetClass("Person")
	if !ok {
		t.Fatal("expected to find class 'Person'")
	}
	if len(cls.Fields) != 2 {
		t.Fatalf("expected 2 fields, got %d", len(cls.Fields))
	}
}

func TestGetInterface(t *testing.T) {
	model := compileForModel(t, `interface Logger {
  level: number
}`)
	iface, ok := model.GetInterface("Logger")
	if !ok {
		t.Fatal("expected to find interface 'Logger'")
	}
	if len(iface.Fields) != 1 {
		t.Fatalf("expected 1 field, got %d", len(iface.Fields))
	}
}
