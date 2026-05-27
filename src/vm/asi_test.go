package vm

import (
	"testing"
)

func TestASI(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // number of statements
	}{
		{
			"simple let",
			"let x = 1\nlet y = 2",
			2,
		},
		{
			"simple const",
			"const x = 1\nconst y = 2",
			2,
		},
		{
			"function call",
			"io.println(1)\nio.println(2)",
			2,
		},
		{
			"return without value",
			"fn test() {\nreturn\n}",
			1,
		},
		{
			"return with value",
			"fn test() {\nreturn 1\n}",
			1,
		},
		{
			"multiline expression with binary op",
			"let x = 1 +\n2",
			1,
		},
		{
			"object literal",
			"let x = {\na: 1\n}",
			1,
		},
		{
			"array literal",
			"let x = [\n1\n]",
			1,
		},
		{
			"multiple semicolons",
			"let x = 1;;;let y = 2",
			2,
		},
		{
			"semicolon after block",
			"if true {\nio.println(1)\n}; io.println(2)",
			2,
		},
		{
			"if else same line",
			"if true { io.println(1) } else { io.println(2) }",
			1,
		},
		{
			"if else multiline",
			"if true {\nio.println(1)\n} else {\nio.println(2)\n}",
			1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lexer := NewLexer(tt.input, "test.tiny")
			parser := NewParser(lexer)
			program := parser.ParseProgram()

			if len(program.Statements) != tt.expected {
				t.Fatalf("expected %d statements, got %d", tt.expected, len(program.Statements))
			}
		})
	}
}

func TestASILexer(t *testing.T) {
	input := "x = 1\ny = 2"
	lexer := NewLexer(input, "test.tiny")
	
	tokens := []TokenType{}
	for {
		tok := lexer.NextToken()
		tokens = append(tokens, tok.Type)
		if tok.Type == TOKEN_EOF {
			break
		}
	}

	expected := []TokenType{
		TOKEN_IDENT, TOKEN_ASSIGN, TOKEN_NUMBER, TOKEN_SEMI,
		TOKEN_IDENT, TOKEN_ASSIGN, TOKEN_NUMBER, TOKEN_SEMI,
		TOKEN_EOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("expected %d tokens, got %d: %v", len(expected), len(tokens), tokens)
	}

	for i, type_ := range expected {
		if tokens[i] != type_ {
			t.Errorf("token %d: expected %s, got %s", i, type_, tokens[i])
		}
	}
}
