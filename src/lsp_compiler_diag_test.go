package main

import (
	"strings"
	"testing"
)

func TestCompilerDiagnosticsUndefinedVar(t *testing.T) {
	text := `let x = y`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for undefined var, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "undefined") {
			return
		}
	}
	t.Fatal("expected undefined variable error")
}

func TestCompilerDiagnosticsDuplicateDecl(t *testing.T) {
	text := `let x = 1
let x = 2`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for duplicate decl, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "already declared") {
			return
		}
	}
	t.Fatal("expected duplicate declaration error")
}

func TestCompilerDiagnosticsReturnTypeMismatch(t *testing.T) {
	text := `fn greet(name: string): number {
  return name
}`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for return type mismatch, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "cannot return") {
			return
		}
	}
	t.Fatal("expected return type mismatch error")
}

func TestCompilerDiagnosticsNoErrors(t *testing.T) {
	text := `fn greet(name: string): string {
  return name
}`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) != 0 {
		t.Fatalf("expected no compiler diagnostics, got %d: %v", len(diagnostics), diagnostics)
	}
}

func TestCompilerDiagnosticsArgCountTooMany(t *testing.T) {
	text := `fn greet(name: string): string {
  return name
}
greet("John", 42)`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for too many args, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "wrong argument count") {
			return
		}
	}
	t.Fatal("expected wrong argument count error")
}

func TestCompilerDiagnosticsArgCountTooFew(t *testing.T) {
	text := `fn greet(name: string): string {
  return name
}
greet()`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for too few args, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "wrong argument count") {
			return
		}
	}
	t.Fatal("expected wrong argument count error")
}

func TestCompilerDiagnosticsMissingReturn(t *testing.T) {
	text := `fn greet(name: string): string {
  let x = 1
}`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	if len(diagnostics) == 0 {
		t.Fatal("expected compiler diagnostics for missing return, got none")
	}
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		t.Logf("compiler diagnostic: %s", msg)
		if strings.Contains(msg, "missing return") {
			return
		}
	}
	t.Fatal("expected missing return error")
}

func TestCompilerDiagnosticsMissingReturnNotOnIfElse(t *testing.T) {
	text := `fn greet(name: string): string {
  if (name == "x") {
    return "yes"
  } else {
    return "no"
  }
}`
	diagnostics := compilerDiagnostics("file:///test.tiny", text)
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "missing return") {
			t.Fatalf("should not flag if/else with returns in both branches: %s", msg)
		}
	}
}
