package vm

import (
	"strings"
	"testing"

	"language.com/src/tinyerrors"
)

func parseSourceForTest(t *testing.T, source string) Program {
	t.Helper()

	lexer := NewLexer(source, "test.tiny")
	parser := NewParser(lexer)
	return parser.ParseProgram()
}

func requireParserError(t *testing.T, source string, kind tinyerrors.ErrorKind, contains string) {
	t.Helper()

	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("expected parser error %s containing %q", kind, contains)
		}

		err, ok := r.(tinyerrors.LangErrorType)
		if !ok {
			t.Fatalf("expected LangErrorType, got %T: %v", r, r)
		}

		if err.Kind != kind {
			t.Fatalf("expected %s, got %s: %s", kind, err.Kind, err.Message)
		}

		if contains != "" && !strings.Contains(err.Message, contains) {
			t.Fatalf("expected message containing %q, got %q", contains, err.Message)
		}
	}()

	parseSourceForTest(t, source)
}

func TestParserTypeHintsAndUnionTypes(t *testing.T) {
	program := parseSourceForTest(t, `
let value: string | null = null;
fn identity(input: string | number): any {
    return input;
}
`)

	if len(program.Statements) != 2 {
		t.Fatalf("expected 2 statements, got %d", len(program.Statements))
	}

	variable, ok := program.Statements[0].(VariableStmt)
	if !ok {
		t.Fatalf("expected variable statement, got %T", program.Statements[0])
	}

	if got := variable.TypeHint.String(); got != "string | null" {
		t.Fatalf("unexpected variable type hint: %q", got)
	}

	fn, ok := program.Statements[1].(FunctionStmt)
	if !ok {
		t.Fatalf("expected function statement, got %T", program.Statements[1])
	}

	if got := fn.Params[0].TypeHint.String(); got != "string | number" {
		t.Fatalf("unexpected param type hint: %q", got)
	}

	if got := fn.ReturnType.String(); got != "any" {
		t.Fatalf("unexpected return type hint: %q", got)
	}
}

func TestParserDefaultParams(t *testing.T) {
	program := parseSourceForTest(t, `
fn greet(name, prefix = "Hello") {
    return prefix;
}
`)

	fn := program.Statements[0].(FunctionStmt)
	if len(fn.Params) != 2 {
		t.Fatalf("expected 2 params, got %d", len(fn.Params))
	}

	if !fn.Params[1].HasDefault || fn.Params[1].DefaultValue.Value != "Hello" {
		t.Fatalf("expected second param default value, got %#v", fn.Params[1])
	}
}

func TestParserVariadicParams(t *testing.T) {
	program := parseSourceForTest(t, `
fn collect(...values) {
    return values;
}
`)

	fn := program.Statements[0].(FunctionStmt)
	if len(fn.Params) != 1 {
		t.Fatalf("expected 1 param, got %d", len(fn.Params))
	}

	if !fn.Params[0].Variadic {
		t.Fatalf("expected param to be variadic")
	}
}

func TestParserRejectsRequiredParamAfterDefault(t *testing.T) {
	requireParserError(t, `fn bad(first = 1, second) { return second; }`, tinyerrors.ErrorSyntax, "required parameter")
}

func TestParserClassFieldsMethodsAndEmbed(t *testing.T) {
	program := parseSourceForTest(t, `
class Service {
    embed logger;
    field private const token: string = "secret";
    public fn run() {
        return this.token;
    }
}
`)

	classStmt := program.Statements[0].(ClassStmt)
	if classStmt.Name != "Service" {
		t.Fatalf("unexpected class name: %s", classStmt.Name)
	}
	if len(classStmt.Embeds) != 1 || classStmt.Embeds[0] != "logger" {
		t.Fatalf("unexpected embeds: %#v", classStmt.Embeds)
	}
	if len(classStmt.Fields) != 1 || !classStmt.Fields[0].Private || !classStmt.Fields[0].Constant {
		t.Fatalf("unexpected fields: %#v", classStmt.Fields)
	}
	if len(classStmt.Methods) != 1 || classStmt.Methods[0].Name != "run" {
		t.Fatalf("unexpected methods: %#v", classStmt.Methods)
	}
}
