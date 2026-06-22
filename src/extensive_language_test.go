package main

import (
	"strings"
	"testing"
)

func TestTinyJit(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("comprehensive_jit.tiny")))

	want := strings.Join([]string{
		"=== Running Comprehensive JIT Suite ===",
		"Objects Result: true",
		"Arrays Result: true",
		"Coercion Result: true",
		"Recursion Result: true",
		"Logical Flow Result: true",
		"All JIT tests passed successfully!",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestTinyPipelineExtensiveLanguage(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("extensive_language.tiny")))

	want := strings.Join([]string{
		"=== 1. Arithmetic & Precedence & Types ===",
		"18",
		"float",
		"string",
		"bool",
		"=== 2. Control Flow ===",
		"yes",
		"12",
		"4",
		"60",
		"=== 3. Functions & Closures ===",
		"Hello, Guest!",
		"Hello, Alice!",
		"Hi, Bob!",
		"10",
		"6",
		"7",
		"8",
		"=== 4. Destructuring ===",
		"100",
		"200",
		"10",
		"Point",
		"=== 5. Classes & Interfaces & Composition ===",
		"[LOG] confis (1337)",
		"true",
		"=== 6. Enums & Pattern Matching ===",
		"Large value: 150",
		"Small value: 42",
		"No value present",
		"=== 7. Defer & Try-Catch ===",
		"In try",
		"Caught: {",
		"    kind: 'Error',",
		"    message: 'Exception triggered'",
		"}",
		"In finally",
		"Defer 1",
		"=== 8. Generics ===",
		"999",
		"generic-text",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}
