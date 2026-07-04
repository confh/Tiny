package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLSPThisCompletionInPartialClass(t *testing.T) {
	text := strings.Join([]string{
		"class User {",
		"    field name: string = \"Ada\";",
		"    fn label() {",
		"        return this.name;",
		"    }",
		"    fn edit() {",
		"        this.",
	}, "\n")

	items := getCompletions("file:///test.tiny", text, Position{
		Line:      6,
		Character: len("        this."),
	})

	if !completionLabelsContain(items, "name") {
		t.Fatalf("expected this. completions to include field name, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "label") {
		t.Fatalf("expected this. completions to include method label, got %#v", completionLabels(items))
	}
}

func TestLSPCompletionInsideFunctionWithReturnType(t *testing.T) {
	text := strings.Join([]string{
		`let enabled = true`,
		`export fn style(text: string, code: string): string {`,
		`    te`,
		`}`,
	}, "\n")

	items := getCompletions("file:///return_type_completion.tiny", text, Position{
		Line:      2,
		Character: len("    te"),
	})

	if !completionLabelsContain(items, "text") {
		t.Fatalf("expected completion inside return-typed function to include parameter text, got %#v", completionLabels(items))
	}
}

func TestLSPCompletionInsideParenthesislessForBlock(t *testing.T) {
	text := strings.Join([]string{
		`class Context {`,
		`    fn json(data: any) {}`,
		`}`,
		`class App {`,
		`    field middlewares = []`,
		`    fn runMiddlewares(ctx: Context) {`,
		`        for let i = 0; i < this.middlewares.length(); i++ {`,
		`            this.`,
		`            ctx.`,
		`        }`,
		`    }`,
		`}`,
	}, "\n")

	thisItems := getCompletions("file:///loop_completion.tiny", text, Position{
		Line:      7,
		Character: len(`            this.`),
	})
	if !completionLabelsContain(thisItems, "middlewares") {
		t.Fatalf("expected this. completions inside for block to include field middlewares, got %#v", completionLabels(thisItems))
	}
	if !completionLabelsContain(thisItems, "runMiddlewares") {
		t.Fatalf("expected this. completions inside for block to include method runMiddlewares, got %#v", completionLabels(thisItems))
	}

	ctxItems := getCompletions("file:///loop_completion.tiny", text, Position{
		Line:      8,
		Character: len(`            ctx.`),
	})
	if !completionLabelsContain(ctxItems, "json") {
		t.Fatalf("expected ctx. completions inside for block to include Context method json, got %#v", completionLabels(ctxItems))
	}
}

func TestLSPThisFieldChainCompletionUsesFieldType(t *testing.T) {
	text := strings.Join([]string{
		"class TaskManager {",
		"    field tasks = [];",
		"    fn init() {",
		"        this.tasks.",
		"    }",
		"}",
	}, "\n")

	items := getCompletions("file:///task_manager.tiny", text, Position{
		Line:      3,
		Character: len("        this.tasks."),
	})

	if !completionLabelsContain(items, "push") {
		t.Fatalf("expected this.tasks. completions to include array method push, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "length") {
		t.Fatalf("expected this.tasks. completions to include array method length, got %#v", completionLabels(items))
	}
}

func TestLSPPrimitiveMethodCallAssignmentInference(t *testing.T) {
	text := strings.Join([]string{
		`const text = "a,b,c";`,
		`const hasComma = text.includes(",");`,
		`const parts = text.split(",");`,
		`const joined = text.split(",").join("-");`,
		`const literalHas = "tiny".includes("t");`,
		`parts.`,
	}, "\n")

	scope := scopeAtPosition("file:///primitive_methods.tiny", text, Position{
		Line:      5,
		Character: len("parts."),
	})

	cases := map[string]string{
		"hasComma":   "bool",
		"parts":      "array:string",
		"joined":     "string",
		"literalHas": "bool",
	}

	for name, want := range cases {
		sym, ok := scope.Resolve(name)
		if !ok {
			t.Fatalf("expected %s to be in scope", name)
		}
		if sym.Type != want {
			t.Fatalf("%s type = %q, want %q", name, sym.Type, want)
		}
	}

	items := getCompletions("file:///primitive_methods.tiny", text, Position{
		Line:      5,
		Character: len("parts."),
	})
	if !completionLabelsContain(items, "join") {
		t.Fatalf("expected parts. completions to include array method join, got %#v", completionLabels(items))
	}
}

func TestLSPPrimitiveMethodCallAssignmentInferenceFromFunctionParam(t *testing.T) {
	text := strings.Join([]string{
		`fn parse(token: string) {`,
		`    const parts = token.split(".");`,
		`    const hasDot = token.includes(".");`,
		`    parts.`,
		`}`,
	}, "\n")

	scope := scopeAtPosition("file:///primitive_param_methods.tiny", text, Position{
		Line:      3,
		Character: len("    parts."),
	})

	cases := map[string]string{
		"parts":  "array:string",
		"hasDot": "bool",
	}

	for name, want := range cases {
		sym, ok := scope.Resolve(name)
		if !ok {
			t.Fatalf("expected %s to be in scope", name)
		}
		if sym.Type != want {
			t.Fatalf("%s type = %q, want %q", name, sym.Type, want)
		}
	}
}

func TestLSPInfersInlineFunctionParameterTypesFromExpectedFunctionType(t *testing.T) {
	text := strings.Join([]string{
		`fn test(callback: function(number, string)) {`,
		`}`,
		`test(fn(i, v) {`,
		`    i.`,
		`    v.`,
		`});`,
	}, "\n")

	scope := scopeAtPosition("file:///callback_types.tiny", text, Position{
		Line:      3,
		Character: len(`    i.`),
	})
	if got := expectedInlineFunctionParamTypes(scope, text, Position{Line: 2, Character: len(`test(`)}, strings.Index(text, `fn(i, v)`)); len(got) != 2 || got[0] != "number" || got[1] != "string" {
		t.Fatalf("expected inline function param types [number string], got %#v", got)
	}

	iSym, ok := scope.Resolve("i")
	if !ok {
		t.Fatal("expected callback parameter i in scope")
	}
	if iSym.Type != "number" {
		t.Fatalf("i type = %q, want number", iSym.Type)
	}

	vSym, ok := scope.Resolve("v")
	if !ok {
		t.Fatal("expected callback parameter v in scope")
	}
	if vSym.Type != "string" {
		t.Fatalf("v type = %q, want string", vSym.Type)
	}

	items := getCompletions("file:///callback_types.tiny", text, Position{
		Line:      4,
		Character: len(`    v.`),
	})
	if !completionLabelsContain(items, "split") {
		t.Fatalf("expected v. completions to include string method split, got %#v", completionLabels(items))
	}
}

func TestLSPInlineCallbackParamTypeForArrayFindMethod(t *testing.T) {
	text := strings.Join([]string{
		`const test = [""];`,
		`test.find(fn(v) {`,
		`    v.`,
		`});`,
	}, "\n")

	scope := scopeAtPosition("file:///array_find.tiny", text, Position{
		Line:      2,
		Character: len(`    v.`),
	})

	vSym, ok := scope.Resolve("v")
	if !ok {
		t.Fatal("expected callback parameter v in scope")
	}
	if vSym.Type != "string" {
		t.Fatalf("v type = %q, want string", vSym.Type)
	}

	items := getCompletions("file:///array_find.tiny", text, Position{
		Line:      2,
		Character: len(`    v.`),
	})
	if !completionLabelsContain(items, "split") {
		t.Fatalf("expected v. completions to include string method split, got %#v", completionLabels(items))
	}
}

func TestLSPHoverCallbackParameterFromArrayFindMethod(t *testing.T) {
	text := strings.Join([]string{
		`const test = [""];`,
		`test.find(fn(v) {`,
		`    v`,
		`});`,
	}, "\n")

	result := getHover("file:///array_find_hover.tiny", text, Position{
		Line:      2,
		Character: 6,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for v in array.find callback, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "string") {
		t.Fatalf("expected hover to show string type for v, got %q", hover.Contents.Value)
	}
}

func TestLSPHoverAnonymousFnParamResolvesTypeFromCaller(t *testing.T) {
	text := strings.Join([]string{
		`const arr = [""]`,
		`arr.forEach(fn(i, item) {`,
		`    item`,
		`})`,
	}, "\n")

	result := getHover("file:///anon_fn_hover.tiny", text, Position{
		Line:      2,
		Character: 6,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for item in anonymous fn, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "string") {
		t.Fatalf("expected hover to show string type for item, got %q", hover.Contents.Value)
	}
}

func TestLSPObjectForEachIgnoresCallbackStatementReturn(t *testing.T) {
	text := strings.Join([]string{
		`import std "object"`,
		`const commandsData = { ping: { name: "ping" } }`,
		`const commands = []`,
		`object.forEach(commandsData, fn(_, cmd) {`,
		`    commands.push(cmd)`,
		`})`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///object_foreach_callback.tiny", text)
	if diagnosticsContain(diagnostics, "cannot pass type") {
		t.Fatalf("unexpected callback type diagnostic: %#v", diagnostics)
	}
}

func TestLSPCallbackParameterCountDiagnostics(t *testing.T) {
	t.Run("too few callback parameters", func(t *testing.T) {
		text := strings.Join([]string{
			`fn test(callback: function(number, string)) {`,
			`}`,
			`test(fn() {`,
			`});`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///callback_too_few.tiny", text)
		if !diagnosticsContain(diagnostics, "not enough parameters") {
			t.Fatalf("expected too-few callback params diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("too many callback parameters", func(t *testing.T) {
		text := strings.Join([]string{
			`fn test(callback: function(number)) {`,
			`}`,
			`test(fn(i, v) {`,
			`});`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///callback_too_many.tiny", text)
		if !diagnosticsContain(diagnostics, "too many parameters") {
			t.Fatalf("expected too-many callback params diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("exact callback parameters no diagnostic", func(t *testing.T) {
		text := strings.Join([]string{
			`fn test(callback: function(number, string)) {`,
			`}`,
			`test(fn(i, v) {`,
			`});`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///callback_exact.tiny", text)
		if diagnosticsContain(diagnostics, "parameters") {
			t.Fatalf("expected no callback param count diagnostic, got %#v", diagnostics)
		}
	})
}

func TestLSPMethodCallbackParamCountFromTypedReceiver(t *testing.T) {
	t.Run("callback on method of typed class param catches too few", func(t *testing.T) {
		text := strings.Join([]string{
			`class Conn {`,
			`    fn onMessage(handler: function(Conn, Message)) {}`,
			`}`,
			`interface Message {`,
			`    type: string,`,
			`    data: any`,
			`}`,
			`fn onConnection(handler: function(Conn)) {}`,
			`onConnection(fn(conn) {`,
			`    conn.onMessage(fn(msg) {`,
			`    })`,
			`})`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///method_callback.tiny", text)
		if !diagnosticsContain(diagnostics, "not enough parameters") {
			t.Fatalf("expected callback param count diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("callback on method of typed class param catches too many", func(t *testing.T) {
		text := strings.Join([]string{
			`class Conn {`,
			`    fn onMessage(handler: function(Conn)) {}`,
			`}`,
			`fn onConnection(handler: function(Conn)) {}`,
			`onConnection(fn(conn) {`,
			`    conn.onMessage(fn(a, b) {`,
			`    })`,
			`})`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///method_callback_many.tiny", text)
		if !diagnosticsContain(diagnostics, "too many parameters") {
			t.Fatalf("expected too many callback params diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("correct callback count no diagnostic", func(t *testing.T) {
		text := strings.Join([]string{
			`class Conn {`,
			`    fn onMessage(handler: function(Conn, Message)) {}`,
			`}`,
			`interface Message {`,
			`    type: string,`,
			`    data: any`,
			`}`,
			`fn onConnection(handler: function(Conn)) {}`,
			`onConnection(fn(conn) {`,
			`    conn.onMessage(fn(c, msg) {`,
			`    })`,
			`})`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///method_callback_ok.tiny", text)
		if diagnosticsContain(diagnostics, "parameters") {
			t.Fatalf("expected no callback param count diagnostic, got %#v", diagnostics)
		}
	})
}

func TestLSPFunctionTypeHintDiagnosticsAndCallability(t *testing.T) {
	valid := strings.Join([]string{
		`fn run(callback: function(string)) {`,
		`    callback("ok");`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///function_type_valid.tiny", valid)
	if diagnosticsContain(diagnostics, "unknown type: function(string)") {
		t.Fatalf("did not expect function(string) to be reported as unknown, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "cannot call non-function type 'function(string)'") {
		t.Fatalf("expected function(string) parameter to be callable, got %#v", diagnostics)
	}

	invalid := strings.Join([]string{
		`fn run(callback: function(sdfsdf)) {`,
		`}`,
	}, "\n")
	diagnostics = semanticDiagnostics("file:///function_type_invalid.tiny", invalid)
	if !diagnosticsContain(diagnostics, "unknown type: sdfsdf") {
		t.Fatalf("expected unknown inner callback parameter type diagnostic, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "unknown type: function(sdfsdf)") {
		t.Fatalf("expected diagnostic for inner type only, got %#v", diagnostics)
	}
}

func TestLSPFunctionTypeInUnion(t *testing.T) {
	valid := strings.Join([]string{
		`fn run(callback: function(string) | number) {`,
		`    if typeof callback == "function" {`,
		`        callback("ok");`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///func_union.tiny", valid)
	if diagnosticsContain(diagnostics, "unknown type: function(string)") {
		t.Fatalf("did not expect function(string) in union to be reported as unknown, got %#v", diagnostics)
	}
}

func TestLSPNullableFunctionType(t *testing.T) {
	valid := strings.Join([]string{
		`fn run(callback: function(string)?) {`,
		`    if typeof callback == "function" {`,
		`        callback("ok");`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///nullable_func.tiny", valid)
	if diagnosticsContain(diagnostics, "unknown type: function(string)") {
		t.Fatalf("did not expect function(string) to be reported as unknown, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "unknown type: function(string) | null") {
		t.Fatalf("did not expect function(string) | null to be reported as unknown, got %#v", diagnostics)
	}
}

func TestLSPFunctionParamUnionType(t *testing.T) {
	valid := `fn process(x: function(string | array)) {}`

	diagnostics := semanticDiagnostics("file:///func_param_union.tiny", valid)
	if diagnosticsContain(diagnostics, "unknown type") {
		t.Fatalf("did not expect any unknown type diagnostics, got %#v", diagnostics)
	}
}

func TestLSPClassConstructorSignatureUsesInit(t *testing.T) {
	text := strings.Join([]string{
		"class User {",
		"    fn init(name: string, score: number) {",
		"    }",
		"}",
		"const test = User(",
	}, "\n")

	result := getSignatureHelp("file:///constructor.tiny", text, Position{
		Line:      4,
		Character: len("const test = User("),
	})
	help, ok := result.(SignatureHelp)
	if !ok {
		t.Fatalf("expected constructor signature help, got %#v", result)
	}
	if len(help.Signatures) != 1 || !strings.Contains(help.Signatures[0].Label, "User(name: string, score: number)") {
		t.Fatalf("unexpected constructor signature help: %#v", help)
	}
}

func TestLSPHoverNestedStdFunctionCall(t *testing.T) {
	line := `io.println(http.server(8080));`
	text := strings.Join([]string{
		`import std "http";`,
		`import std "io";`,
		line,
	}, "\n")

	result := getHover("file:///nested_hover.tiny", text, Position{
		Line:      2,
		Character: strings.Index(line, "server") + len("ser"),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for nested http.server call, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "server(port: number | interface:ServerOptions)") {
		t.Fatalf("unexpected hover contents: %s", hover.Contents.Value)
	}
	if !strings.Contains(hover.Contents.Value, "Creates a new Server instance from a port or options object.") {
		t.Fatalf("expected std stub documentation in hover contents: %s", hover.Contents.Value)
	}
}

func TestLSPHoverObjectLiteralFieldsFromInterfaceArgument(t *testing.T) {
	lines := []string{
		`import std "runtime";`,
		``,
		`const bytecodeVM = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: {`,
		`        io: true`,
		`    },`,
		`    disableJIT: true`,
		`});`,
	}
	text := strings.Join(lines, "\n")

	cases := []struct {
		line     int
		word     string
		contains []string
	}{
		{line: 3, word: "isolated", contains: []string{"VMOptions.isolated", "bool"}},
		{line: 4, word: "allowedStdlib", contains: []string{"VMOptions.allowedStdlib", "VMStdlibOptions"}},
		{line: 5, word: "io", contains: []string{"VMStdlibOptions.io", "bool"}},
		{line: 7, word: "disableJIT", contains: []string{"VMOptions.disableJIT", "bool"}},
	}

	for _, tc := range cases {
		result := getHover("file:///runtime_options_hover.tiny", text, Position{
			Line:      tc.line,
			Character: strings.Index(lines[tc.line], tc.word) + len(tc.word)/2,
		})
		hover, ok := result.(HoverResult)
		if !ok {
			t.Fatalf("expected hover for %s, got %#v", tc.word, result)
		}
		for _, want := range tc.contains {
			if !strings.Contains(hover.Contents.Value, want) {
				t.Fatalf("expected hover for %s to contain %q, got %q", tc.word, want, hover.Contents.Value)
			}
		}
	}
}

func TestLSPHoverObjectLiteralFieldsFromInterfaceVariable(t *testing.T) {
	lines := []string{
		`interface Options {`,
		`    enabled: bool,`,
		`}`,
		``,
		`const options: Options = {`,
		`    enabled: true`,
		`};`,
	}
	text := strings.Join(lines, "\n")

	result := getHover("file:///typed_object_hover.tiny", text, Position{
		Line:      5,
		Character: strings.Index(lines[5], "enabled") + len("enabled")/2,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for interface-typed object field, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "Options.enabled") || !strings.Contains(hover.Contents.Value, "bool") {
		t.Fatalf("unexpected hover contents: %q", hover.Contents.Value)
	}
}

func TestLSPHoverPrefersInterfaceFieldDeclarationOverFunctionNameCollision(t *testing.T) {
	line := `    status: number,`
	text := strings.Join([]string{
		`export interface HttpResponse {`,
		line,
		`}`,
		`export fn status(code: number, body: any) {}`,
	}, "\n")

	result := getHover("file:///collision.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line, "status") + len("sta"),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for interface field declaration, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "HttpResponse.status") || strings.Contains(hover.Contents.Value, "status(code") {
		t.Fatalf("expected interface field hover, got %q", hover.Contents.Value)
	}
}

func TestLSPHoverPrefersClassMethodDeclarationOverFunctionNameCollision(t *testing.T) {
	line := `    fn get(path: string, handler: function) {}`
	text := strings.Join([]string{
		`class Server {`,
		line,
		`}`,
		`export fn get(url: string) {}`,
	}, "\n")

	result := getHover("file:///collision.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line, "get") + len("ge"),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for class method declaration, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "Server.get(path: string") || strings.Contains(hover.Contents.Value, "get(url") {
		t.Fatalf("expected class method hover, got %q", hover.Contents.Value)
	}
}

func TestLSPInlayHintsDisabledForLatency(t *testing.T) {
	text := strings.Join([]string{
		`fn greet(name: string, excited: bool): string {`,
		`    return name;`,
		`}`,
		`const message = greet("Tiny", true);`,
	}, "\n")

	hints := getInlayHints("file:///inlay.tiny", text, LSPRange{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 3, Character: len(`const message = greet("Tiny", true);`)},
	})
	if len(hints) != 0 {
		t.Fatalf("expected inlay hints to be disabled, got %#v", hints)
	}
}

func TestLSPInlayHintsDisabledForMultilineMemberCall(t *testing.T) {
	text := strings.Join([]string{
		`namespace newJwt {`,
		`    export fn sign(payload: object, ttl: number): string {`,
		`        return "";`,
		`    }`,
		`}`,
		`const input = "hello";`,
		`const token = newJwt.sign({`,
		`    message: input`,
		`}, 10);`,
	}, "\n")

	hints := getInlayHints("file:///inlay_multiline.tiny", text, LSPRange{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 8, Character: len(`}, 10);`)},
	})
	if len(hints) != 0 {
		t.Fatalf("expected inlay hints to be disabled, got %#v", hints)
	}
}

func TestLSPInlayHintsDisabledForInlineFunctionParameterTypes(t *testing.T) {
	text := strings.Join([]string{
		`fn test(callback: function(number, string)) {`,
		`}`,
		`test(fn(i, v) {`,
		`    i;`,
		`    v;`,
		`});`,
	}, "\n")

	hints := getInlayHints("file:///callback_inlay.tiny", text, LSPRange{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 5, Character: len(`});`)},
	})
	if len(hints) != 0 {
		t.Fatalf("expected inlay hints to be disabled, got %#v", hints)
	}
}

func TestLSPInlayHintsDisabledForInternalTypePrefixes(t *testing.T) {
	text := strings.Join([]string{
		`interface Request {`,
		`    path: string`,
		`}`,
		`class User {`,
		`}`,
		`fn use(callback: function(Request)) {`,
		`}`,
		`const user = User();`,
		`use(fn(req) {`,
		`    req;`,
		`});`,
	}, "\n")

	hints := getInlayHints("file:///inlay_prefixes.tiny", text, LSPRange{
		Start: Position{Line: 0, Character: 0},
		End:   Position{Line: 10, Character: len(`});`)},
	})
	if len(hints) != 0 {
		t.Fatalf("expected inlay hints to be disabled, got %#v", hints)
	}
}

func TestLSPMultilineNamespaceCallAssignmentInference(t *testing.T) {
	text := strings.Join([]string{
		`namespace newJwt {`,
		`    export fn sign(payload: object, ttl: number): string {`,
		`        return "";`,
		`    }`,
		`}`,
		`const input = "hello";`,
		`const token = newJwt.sign({`,
		`    message: input`,
		`}, 10);`,
	}, "\n")

	scope := scopeAtPosition("file:///multiline_namespace_call.tiny", text, Position{
		Line:      8,
		Character: len(`}, 10);`),
	})

	token, ok := scope.Resolve("token")
	if !ok {
		t.Fatalf("expected token to be in scope")
	}
	if token.Type != "string" {
		t.Fatalf("token type = %q, want string", token.Type)
	}
}

// func TestLSPInlayHintsReturnTypesForMultilineNamespaceAndStdCalls(t *testing.T) {
// 	text := strings.Join([]string{
// 		`import std "time";`,
// 		`namespace newJwt {`,
// 		`    export fn sign(payload: object, ttl: number): string {`,
// 		`        return "";`,
// 		`    }`,
// 		`}`,
// 		`const input = "hello";`,
// 		`const token = newJwt.sign({`,
// 		`    message: input`,
// 		`}, 10);`,
// 		`const parts = token.split(".");`,
// 		`const now = time.nowMs();`,
// 	}, "\n")

// 	hints := getInlayHints("file:///return_inlay.tiny", text, LSPRange{
// 		Start: Position{Line: 0, Character: 0},
// 		End:   Position{Line: 11, Character: len(`const now = time.nowMs();`)},
// 	})

// 	labelsByLine := map[int][]string{}
// 	for _, hint := range hints {
// 		labelsByLine[hint.Position.Line] = append(labelsByLine[hint.Position.Line], hint.Label)
// 	}

// 	if !strings.Contains(labelsByLine[7], ": string") {
// 		t.Fatalf("expected token type hint on line 7, got %#v", labelsByLine)
// 	}
// 	if !containsString(labelsByLine[10], ": array") {
// 		t.Fatalf("expected split type hint on line 10, got %#v", labelsByLine)
// 	}
// 	if !containsString(labelsByLine[11], ": number") {
// 		t.Fatalf("expected nowMs type hint on line 11, got %#v", labelsByLine)
// 	}
// }

func TestLSPTruthyIfNarrowsNullFromUnion(t *testing.T) {
	text := strings.Join([]string{
		`let token: string | null = "a.b";`,
		`if token {`,
		`    const parts = token.split(".");`,
		`    parts.`,
		`}`,
	}, "\n")

	scope := scopeAtPosition("file:///narrow_truthy.tiny", text, Position{
		Line:      3,
		Character: len(`    parts.`),
	})

	token, ok := scope.Resolve("token")
	if !ok {
		t.Fatal("expected token in scope")
	}
	if token.Type != "string" {
		t.Fatalf("token type = %q, want string", token.Type)
	}

	parts, ok := scope.Resolve("parts")
	if !ok {
		t.Fatal("expected parts in scope")
	}
	if parts.Type != "array:string" {
		t.Fatalf("parts type = %q, want array:string", parts.Type)
	}
}

func TestLSPDefinitionOnLibraryImportString(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	root := libraryGlobalRoot("owner", "widgets", "v1")
	writeConfigForTest(t, filepath.Join(root, "tiny.json"), TinyProjectConfig{Entry: "src/main.tiny"})
	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	entry := filepath.Join(root, "src", "main.tiny")
	if err := os.WriteFile(entry, []byte("export const value = 1;\n"), 0644); err != nil {
		t.Fatal(err)
	}

	text := `import library "owner/widgets";`
	result := getDefinition("file:///project/main.tiny", text, Position{
		Line:      0,
		Character: len(`import library "owner/wid`),
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected import definition location, got %#v", result)
	}
	if loc.URI != pathToFileURI(entry) {
		t.Fatalf("definition URI = %q, want %q", loc.URI, pathToFileURI(entry))
	}
}

func TestLSPDefinitionOnStdImportUsesVirtualStub(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_STDLIB_STUB_DIR", dir)

	text := `import std "http" as http;`
	result := getDefinition("file:///project/main.tiny", text, Position{
		Line:      0,
		Character: len(`import std "ht`),
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected std import definition location, got %#v", result)
	}
	if loc.URI != "tiny-stdlib:/http.tiny" {
		t.Fatalf("definition URI = %q, want tiny-stdlib:/http.tiny", loc.URI)
	}
	if _, err := os.Stat(filepath.Join(dir, "http.tiny")); err != nil {
		t.Fatalf("expected stdlib stub to be written to env dir: %v", err)
	}
}

func TestLSPDefinitionOnSingleQuotedStdImportUsesVirtualStub(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_STDLIB_STUB_DIR", dir)

	text := `import std 'http' as http;`
	result := getDefinition("file:///project/main.tiny", text, Position{
		Line:      0,
		Character: len(`import std 'ht`),
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected std import definition location, got %#v", result)
	}
	if loc.URI != "tiny-stdlib:/http.tiny" {
		t.Fatalf("definition URI = %q, want tiny-stdlib:/http.tiny", loc.URI)
	}
}

func TestLSPStdModuleLocationParsingIsStrict(t *testing.T) {
	if _, ok := stdModuleFromLocationURI("file:///tmp/not-std:http/main.tiny"); ok {
		t.Fatal("did not expect arbitrary URI containing std: to be treated as stdlib")
	}
	if module, ok := stdModuleFromLocationURI("std:http"); !ok || module != "http" {
		t.Fatalf("expected std:http to parse as http, got %q ok=%v", module, ok)
	}
	if module, ok := stdModuleFromLocationURI("tiny-stdlib:/http.tiny"); !ok || module != "http" {
		t.Fatalf("expected virtual URI to parse as http, got %q ok=%v", module, ok)
	}
}

func TestLSPDefinitionOnStdFunctionUsesVirtualStub(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_STDLIB_STUB_DIR", dir)

	text := strings.Join([]string{
		`import std "http" as http`,
		`http.post("", {})`,
	}, "\n")
	result := getDefinition("file:///project/main.tiny", text, Position{
		Line:      1,
		Character: len(`http.po`),
	})
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected std function definition location, got %#v", result)
	}
	if loc.URI != "tiny-stdlib:/http.tiny" {
		t.Fatalf("definition URI = %q, want tiny-stdlib:/http.tiny", loc.URI)
	}
	if loc.Range.Start.Line != 155 {
		t.Fatalf("expected http.post definition line 155, got %#v", loc.Range.Start)
	}
}

func TestLSPVirtualStdlibDocsDoNotPolluteOpenDocs(t *testing.T) {
	oldDocs := lspDocs
	defer func() { lspDocs = oldDocs }()
	lspDocs = map[string]string{}

	refreshLSPDocument("tiny-stdlib:/http.tiny", "bad")
	if len(lspDocs) != 0 {
		t.Fatalf("expected virtual stdlib refresh to be ignored, got %#v", lspDocs)
	}

	text := lspDocumentText("tiny-stdlib:/http.tiny")
	if !strings.Contains(text, "export fn post") {
		t.Fatalf("expected virtual stdlib document text from embedded stub, got %q", text)
	}
}

func TestLSPCodeActionAddsInferredTypeHint(t *testing.T) {
	text := `const parts = "a.b".split(".");`
	actions := getCodeActions("file:///actions.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///actions.tiny"},
		Range: LSPRange{
			Start: Position{Line: 0, Character: 6},
			End:   Position{Line: 0, Character: 11},
		},
	})

	for _, action := range actions {
		if action.Title == "Add inferred type hint" {
			edits := action.Edit.Changes["file:///actions.tiny"]
			if len(edits) == 0 || edits[0].NewText != ": array:string" {
				t.Fatalf("unexpected type hint action edits: %#v", edits)
			}
			return
		}
	}
	t.Fatalf("expected Add inferred type hint action, got %#v", actions)
}

func TestLSPOrganizeImportsActionRemovesDuplicatesAndUnused(t *testing.T) {
	text := strings.Join([]string{
		`import std "tests";`,
		`import std "io";`,
		`import std "io";`,
		``,
		`fn run() {`,
		`    io.println("ok")`,
		`}`,
	}, "\n")

	edit, ok := organizeImportsEdit("file:///imports.tiny", text)
	if !ok {
		t.Fatalf("expected organize imports edit")
	}
	if edit.NewText != "import std \"io\";\n" {
		t.Fatalf("unexpected organize imports text: %q", edit.NewText)
	}
	if edit.Range.Start.Line != 0 || edit.Range.End.Line != 3 {
		t.Fatalf("unexpected organize imports range: %#v", edit.Range)
	}

	actions := getCodeActions("file:///imports.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///imports.tiny"},
		Range:        LSPRange{Start: Position{Line: 0, Character: 0}, End: Position{Line: 0, Character: 0}},
	})
	for _, action := range actions {
		if action.Title == "Organize imports" && action.Kind == "source.organizeImports" {
			return
		}
	}
	t.Fatalf("expected Organize imports source action, got %#v", actions)
}

func TestLSPCodeActionConvertsLargeIfElseChainToMatch(t *testing.T) {
	text := strings.Join([]string{
		`fn describe(status: string) {`,
		`    if status == "new" {`,
		`        return "New";`,
		`    } else if status == "open" {`,
		`        return "Open";`,
		`    } else if status == "done" {`,
		`        return "Done";`,
		`    } else if status == "failed" {`,
		`        return "Failed";`,
		`    } else {`,
		`        return "Unknown";`,
		`    }`,
		`}`,
	}, "\n")

	actions := getCodeActions("file:///match_action.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///match_action.tiny"},
		Range:        LSPRange{Start: Position{Line: 1, Character: 8}, End: Position{Line: 1, Character: 8}},
	})

	for _, action := range actions {
		if action.Title != "Convert if/else chain to match" {
			continue
		}
		if action.Kind != "refactor.rewrite" {
			t.Fatalf("action kind = %q, want refactor.rewrite", action.Kind)
		}
		edits := action.Edit.Changes["file:///match_action.tiny"]
		if len(edits) != 1 {
			t.Fatalf("expected one edit, got %#v", edits)
		}
		want := strings.Join([]string{
			`    match status {`,
			`        "new" {`,
			`            return "New";`,
			`        }`,
			`        "open" {`,
			`            return "Open";`,
			`        }`,
			`        "done" {`,
			`            return "Done";`,
			`        }`,
			`        "failed" {`,
			`            return "Failed";`,
			`        }`,
			`        _ {`,
			`            return "Unknown";`,
			`        }`,
			`    }`,
		}, "\n")
		if edits[0].NewText != want {
			t.Fatalf("unexpected match replacement:\n%s", edits[0].NewText)
		}
		return
	}
	t.Fatalf("expected Convert if/else chain to match action, got %#v", actions)
}

func TestLSPCodeActionDoesNotConvertSmallIfElseChainToMatch(t *testing.T) {
	text := strings.Join([]string{
		`if status == "new" {`,
		`    return "New";`,
		`} else {`,
		`    return "Other";`,
		`}`,
	}, "\n")

	actions := getCodeActions("file:///small_match_action.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///small_match_action.tiny"},
		Range:        LSPRange{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 1}},
	})

	for _, action := range actions {
		if action.Title == "Convert if/else chain to match" {
			t.Fatalf("did not expect match conversion for small if/else, got %#v", actions)
		}
	}
}

func TestLSPCodeActionDoesNotConvertMixedIfElseChainToMatch(t *testing.T) {
	text := strings.Join([]string{
		`if status == "new" {`,
		`    return "New";`,
		`} else if kind == "open" {`,
		`    return "Open";`,
		`} else if status == "done" {`,
		`    return "Done";`,
		`} else if status == "failed" {`,
		`    return "Failed";`,
		`} else {`,
		`    return "Unknown";`,
		`}`,
	}, "\n")

	actions := getCodeActions("file:///mixed_match_action.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///mixed_match_action.tiny"},
		Range:        LSPRange{Start: Position{Line: 0, Character: 1}, End: Position{Line: 0, Character: 1}},
	})

	for _, action := range actions {
		if action.Title == "Convert if/else chain to match" {
			t.Fatalf("did not expect match conversion for mixed conditions, got %#v", actions)
		}
	}
}

func TestLSPThisCompletionIncludesMethodsDeclaredAfterCursor(t *testing.T) {
	text := strings.Join([]string{
		"class TaskManager {",
		"    field tasks = [];",
		"    fn init() {",
		"        this.",
		"    }",
		"    fn load() {",
		"    }",
		"    fn save() {",
		"    }",
		"    fn add(title: string) {",
		"    }",
		"}",
	}, "\n")

	items := getCompletions("file:///task_manager.tiny", text, Position{
		Line:      3,
		Character: len("        this."),
	})

	for _, label := range []string{"tasks", "init", "load", "save", "add"} {
		if !completionLabelsContain(items, label) {
			t.Fatalf("expected this. completions to include %s, got %#v", label, completionLabels(items))
		}
	}
}

func TestLSPThisCompletionIncludesAllTaskManagerMethodsFromNestedMethod(t *testing.T) {
	text := strings.Join([]string{
		"class TaskManager {",
		"    field tasks = [];",
		"",
		"    fn init() {",
		"        this.load();",
		"    }",
		"",
		"    fn load() {",
		"        try {",
		"            const data = fs.readFile(\"tasks.json\");",
		"            this.tasks = json.parse(data);",
		"        } catch err {",
		"            this.tasks = [];",
		"        }",
		"    }",
		"",
		"    private fn save() {",
		"        const data = json.stringify(this.tasks);",
		"        fs.writeFile(\"tasks.json\", data);",
		"    }",
		"",
		"    fn add(title: string) {",
		"        const newTask = {",
		"            title: title,",
		"            done: false",
		"        };",
		"        this.tasks.push(newTask);",
		"        this.save();",
		"    }",
		"",
		"    fn list() {",
		"        for let i = 0; i < this.tasks.length(); i++ {",
		"            const task = this.tasks[i];",
		"            io.println(`${i}. ${task.title}`);",
		"        }",
		"    }",
		"",
		"    fn markDone(index: number) {",
		"        if index >= 0 and index < this.tasks.length() {",
		"            this.",
		"        } else {",
		"            io.println(\"Error\");",
		"        }",
		"    }",
		"",
		"    fn remove(index: number) {",
		"        this.tasks.remove(index);",
		"    }",
		"}",
	}, "\n")

	items := getCompletions("file:///task_manager.tiny", text, Position{
		Line:      39,
		Character: len("            this."),
	})

	for _, label := range []string{"tasks", "init", "load", "save", "add", "list", "markDone", "remove"} {
		if !completionLabelsContain(items, label) {
			t.Fatalf("expected this. completions inside markDone to include %s, got %#v", label, completionLabels(items))
		}
	}
}

func TestLSPEmbeddedClassMethodsFromAssignedEmbedField(t *testing.T) {
	text := strings.Join([]string{
		"import std \"io\";",
		"class Logger {",
		"    fn log(message) {",
		"        io.println(message);",
		"    }",
		"}",
		"",
		"class Service {",
		"    embed logger;",
		"",
		"    fn init() {",
		"        this.logger = Logger();",
		"    }",
		"}",
		"",
		"let service = Service();",
		"service.log(\"delegated through embed\");",
	}, "\n")

	diagnostics := semanticDiagnostics("file:///embed.tiny", text)
	if diagnosticsContain(diagnostics, "undefined method or property: log") {
		t.Fatalf("expected embedded Logger.log to be accepted, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "undefined method or property: logger") {
		t.Fatalf("expected embedded field logger assignment to be accepted, got %#v", diagnostics)
	}

	items := getCompletions("file:///embed.tiny", text+"\nservice.", Position{
		Line:      17,
		Character: len("service."),
	})
	if !completionLabelsContain(items, "log") {
		t.Fatalf("expected service. completions to include embedded method log, got %#v", completionLabels(items))
	}
}

func TestLSPEmbeddedClassMethodsFromImportedTypedEmbedField(t *testing.T) {
	dir := t.TempDir()
	testingPath := filepath.Join(dir, "testing.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(testingPath, []byte(strings.Join([]string{
		"import std \"io\";",
		"export class Test {",
		"    fn tt() {",
		"        io.println(\"hi\")",
		"    }",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import std \"http\";",
		"import \"testing.tiny\" as Testing;",
		"",
		"class Tester {",
		"    field test: Testing.Test",
		"    embed test",
		"",
		"    fn init() {",
		"        this.test = Testing.Test()",
		"    }",
		"}",
		"",
		"const testClass = Tester()",
		"",
		"testClass.tt()",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "undefined method or property: tt") {
		t.Fatalf("expected imported typed embed method tt to be accepted, got %#v", diagnostics)
	}

	items := getCompletions(uri, text+"\ntestClass.", Position{
		Line:      15,
		Character: len("testClass."),
	})
	if !completionLabelsContain(items, "tt") {
		t.Fatalf("expected testClass. completions to include embedded method tt, got %#v", completionLabels(items))
	}
}

func TestLSPCompletionsInsideMatchCaseBody(t *testing.T) {
	text := strings.Join([]string{
		"class Splash {",
		"    fn show() {",
		"        return null",
		"    }",
		"    fn setTitle(title: string) {",
		"        return null",
		"    }",
		"}",
		"",
		"let action = \"ready\"",
		"let splash = Splash()",
		"",
		"match action {",
		"    \"ready\" {",
		"        splash.",
		"    }",
		"    \"load_main\" {",
		"        ",
		"    }",
		"}",
	}, "\n")

	memberItems := getCompletions("file:///match_completion.tiny", text, Position{
		Line:      14,
		Character: len("        splash."),
	})
	if !completionLabelsContain(memberItems, "show") {
		t.Fatalf("expected splash. completions inside match case to include show, got %#v", completionLabels(memberItems))
	}
	if !completionLabelsContain(memberItems, "setTitle") {
		t.Fatalf("expected splash. completions inside match case to include setTitle, got %#v", completionLabels(memberItems))
	}

	scopeItems := getCompletions("file:///match_completion.tiny", text, Position{
		Line:      17,
		Character: len("        "),
	})
	if !completionLabelsContain(scopeItems, "splash") {
		t.Fatalf("expected scope completions inside match case to include splash, got %#v", completionLabels(scopeItems))
	}
}

func TestLSPImportedClassCompletionAndDiagnostics(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "models.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(modelPath, []byte(strings.Join([]string{
		"export class User {",
		"    field name: string = \"Ada\";",
		"    fn greet(): string {",
		"        return this.name;",
		"    }",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import \"models.tiny\" as models;",
		"let user: models.User = models.User();",
		"user.",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "models.User") || diagnosticsContain(diagnostics, "User") {
		t.Fatalf("expected imported exported class to be accepted, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      2,
		Character: len("user."),
	})

	if !completionLabelsContain(items, "name") {
		t.Fatalf("expected imported class completions to include field name, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "greet") {
		t.Fatalf("expected imported class completions to include method greet, got %#v", completionLabels(items))
	}
}

func TestLSPImportedReExportedClassAliasCompletionAndDiagnostics(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	messagePath := filepath.Join(dir, "message.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		"export class CommandBuilder {",
		"    field name = \"\";",
		"    fn setName(name: string) {",
		"        this.name = name;",
		"    }",
		"    fn setDescription(description: string) { }",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, []byte(strings.Join([]string{
		"export class Message {",
		"    field content: string = \"\";",
		"    fn reply(text: string) { }",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		"import \"commands.tiny\" as CommandsModule;",
		"import \"message.tiny\" as MessageModule;",
		"export const CommandBuilder = CommandsModule.CommandBuilder;",
		"export const Message = MessageModule.Message;",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import \"gateway.tiny\" as Discord;",
		"const pingCmd = Discord.CommandBuilder();",
		"pingCmd.",
		"fn boot(bot) {",
		"    bot.onMessage(fn(msg: Discord.Message, client) {",
		"        msg.reply(\"Pong\");",
		"    });",
		"}",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "unknown type: Discord.Message") {
		t.Fatalf("expected re-exported class alias type to resolve, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      2,
		Character: len("pingCmd."),
	})
	if !completionLabelsContain(items, "setName") {
		t.Fatalf("expected re-exported CommandBuilder completions to include setName, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "setDescription") {
		t.Fatalf("expected re-exported CommandBuilder completions to include setDescription, got %#v", completionLabels(items))
	}
}

func TestLSPLibraryImportUsesGlobalDependencyExports(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))
	withWorkingDir(t, dir)

	root := libraryGlobalRoot("owner", "widgets", "v1")
	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"widgets": {Source: "github:owner/widgets", Version: "v1"},
		},
	})
	writeConfigForTest(t, filepath.Join(root, "tiny.json"), TinyProjectConfig{Entry: "src/main.tiny"})

	if err := os.MkdirAll(filepath.Join(root, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "main.tiny"), []byte(strings.Join([]string{
		"export class Widget {",
		"    field name = \"demo\";",
		"    fn label() { return this.name; }",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	mainPath := filepath.Join(dir, "main.tiny")
	text := strings.Join([]string{
		`import library "owner/widgets" as Widgets;`,
		"const widget = Widgets.Widget();",
		"widget.",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "Widget") {
		t.Fatalf("expected library import to resolve Widget, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      2,
		Character: len("widget."),
	})
	if !completionLabelsContain(items, "label") {
		t.Fatalf("expected library class completions to include label, got %#v", completionLabels(items))
	}
}

func TestLSPLibraryImportStringCompletionRestrictedToTinyJson(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))
	withWorkingDir(t, dir)

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"widgets": {Source: "github:owner/widgets", Version: "v1"},
		},
	})

	globalLibRoot := libraryGlobalRoot("other", "unrelated", "v1")
	writeConfigForTest(t, filepath.Join(globalLibRoot, "tiny.json"), TinyProjectConfig{Entry: "main.tiny"})

	mainPath := filepath.Join(dir, "main.tiny")
	text := `import lib "`

	uri := pathToFileURI(mainPath)
	items := getCompletions(uri, text, Position{
		Line:      0,
		Character: len(text),
	})

	if !completionLabelsContain(items, "owner/widgets") {
		t.Fatalf("expected project dependency completion 'owner/widgets', got %#v", completionLabels(items))
	}

	if completionLabelsContain(items, "other/unrelated") {
		t.Fatalf("expected other globally installed library 'other/unrelated' to NOT be completed, got %#v", completionLabels(items))
	}
}

func TestLSPImportedNamespaceClassConstructorInference(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(todoPath, []byte(strings.Join([]string{
		"import std \"fs\";",
		"import std \"json\";",
		"import std \"io\";",
		"",
		"export class TaskManager {",
		"    field tasks = [];",
		"",
		"    fn add(title: string) {",
		"        this.tasks.push({ title: title, done: false });",
		"    }",
		"",
		"    fn list() {",
		"        io.println(\"tasks\");",
		"    }",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import std \"io\";",
		"import \"todo.tiny\" as Todo;",
		"",
		"const manager = Todo.TaskManager();",
		"manager.",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "TaskManager") {
		t.Fatalf("expected Todo.TaskManager() to be accepted, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      4,
		Character: len("manager."),
	})

	if !completionLabelsContain(items, "add") {
		t.Fatalf("expected manager. completions to include add, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "list") {
		t.Fatalf("expected manager. completions to include list, got %#v", completionLabels(items))
	}
}

func TestLSPImportedTodoClassConstructorFromFullExample(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(todoPath, []byte(strings.Join([]string{
		"import std \"fs\";",
		"import std \"json\";",
		"import std \"io\";",
		"",
		"export class TaskManager {",
		"    field tasks = [];",
		"",
		"    fn init() {",
		"        this.load();",
		"    }",
		"",
		"    fn load() {",
		"        try {",
		"            const data = fs.readFile(\"tasks.json\");",
		"            this.tasks = json.parse(data);",
		"        } catch err {",
		"            this.tasks = [];",
		"        }",
		"    }",
		"",
		"    fn save() {",
		"        const data = json.stringify(this.tasks);",
		"        fs.writeFile(\"tasks.json\", data);",
		"    }",
		"",
		"    fn add(title: string) {",
		"        const newTask = {",
		"            title: title,",
		"            done: false",
		"        };",
		"        this.tasks.push(newTask);",
		"        this.save();",
		"        io.println(`Added task: \"${title}\"`);",
		"    }",
		"",
		"    fn list() {",
		"        if this.tasks.length() == 0 {",
		"            io.println(\"No tasks found. Add some with: add <task>\");",
		"            return;",
		"        }",
		"    }",
		"",
		"    fn markDone(index: number) {",
		"        if index >= 0 and index < this.tasks.length() {",
		"            this.tasks[index].done = true;",
		"            this.save();",
		"        }",
		"    }",
		"",
		"    fn remove(index: number) {",
		"        if index >= 0 and index < this.tasks.length() {",
		"            const task = this.tasks[index];",
		"            this.tasks.remove(index);",
		"            this.save();",
		"            io.println(`Removed task: \"${task.title}\"`);",
		"        }",
		"    }",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import std \"io\";",
		"import std \"process\";",
		"import std \"math\";",
		"import \"todo.tiny\" as Todo;",
		"",
		"const args = process.args();",
		"const command = args[0];",
		"const manager = Todo.TaskManager();",
		"manager.",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "TaskManager") {
		t.Fatalf("expected full Todo.TaskManager() example to be accepted, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      8,
		Character: len("manager."),
	})

	if !completionLabelsContain(items, "markDone") {
		t.Fatalf("expected manager. completions to include markDone, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "remove") {
		t.Fatalf("expected manager. completions to include remove, got %#v", completionLabels(items))
	}
}

func TestLSPImportedClassUsesOpenDocumentText(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	todoURI := pathToFileURI(todoPath)

	err := os.WriteFile(todoPath, []byte("export const placeholder = true;\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	lspDocs[todoURI] = strings.Join([]string{
		"export class TaskManager {",
		"    fn list() {}",
		"}",
	}, "\n")
	defer delete(lspDocs, todoURI)

	text := strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"const manager = Todo.TaskManager();",
		"manager.",
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, text)
	if diagnosticsContain(diagnostics, "TaskManager") {
		t.Fatalf("expected open imported document to provide TaskManager, got diagnostics %#v", diagnostics)
	}

	items := getCompletions(uri, text, Position{
		Line:      2,
		Character: len("manager."),
	})
	if !completionLabelsContain(items, "list") {
		t.Fatalf("expected manager. completions from open imported document, got %#v", completionLabels(items))
	}
}

func TestLSPDependentDocumentURIsIncludesOpenImporters(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	todoURI := pathToFileURI(todoPath)
	mainURI := pathToFileURI(mainPath)

	lspDocs[todoURI] = "export class TaskManager {}\n"
	lspDocs[mainURI] = strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"const manager = Todo.TaskManager();",
	}, "\n")
	defer delete(lspDocs, todoURI)
	defer delete(lspDocs, mainURI)

	dependents := dependentDocumentURIs(todoURI)
	if len(dependents) != 1 || dependents[0] != mainURI {
		t.Fatalf("expected main.tiny to refresh when todo.tiny changes, got %#v", dependents)
	}
}

func TestLSPImportedClassDiagnosticsRefreshWithOpenDocumentText(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	todoURI := pathToFileURI(todoPath)
	mainURI := pathToFileURI(mainPath)

	lspDocs[todoURI] = "export const placeholder = true;\n"
	defer delete(lspDocs, todoURI)

	text := strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"const manager = Todo.TaskManager();",
	}, "\n")

	diagnostics := semanticDiagnostics(mainURI, text)
	if !diagnosticsContain(diagnostics, "undefined export: Todo.TaskManager") {
		t.Fatalf("expected missing export diagnostic before imported file changes, got %#v", diagnostics)
	}

	lspDocs[todoURI] = "export class TaskManager {}\n"
	invalidateLSPImportCacheForURI(todoURI)

	diagnostics = semanticDiagnostics(mainURI, text)
	if diagnosticsContain(diagnostics, "TaskManager") {
		t.Fatalf("expected diagnostics to clear after open imported file export changes, got %#v", diagnostics)
	}
}

func TestLSPImportedExternalGlobalExport(t *testing.T) {
	dir := t.TempDir()
	externalPath := filepath.Join(dir, "external_import_test.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	externalURI := pathToFileURI(externalPath)
	mainURI := pathToFileURI(mainPath)

	lspDocs[externalURI] = "export external const ss: string\n"
	defer delete(lspDocs, externalURI)

	text := strings.Join([]string{
		`import std "io";`,
		`import "external_import_test.tiny" as test`,
		``,
		`io.println(test.ss)`,
	}, "\n")

	diagnostics := semanticDiagnostics(mainURI, text)
	if diagnosticsContain(diagnostics, "undefined export: test.ss") {
		t.Fatalf("expected imported external global export to resolve, got %#v", diagnostics)
	}

	exports := loadTinyFileExports(externalPath, map[string]bool{})
	sym, ok := exports["ss"]
	if !ok {
		t.Fatalf("expected ss export, got %#v", exports)
	}
	if sym.Type != "string" {
		t.Fatalf("expected ss type string, got %q", sym.Type)
	}
}

func TestLSPImportedFunctionUsesInferredReturnType(t *testing.T) {
	dir := t.TempDir()
	libPath := filepath.Join(dir, "lib.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(libPath, []byte(strings.Join([]string{
		`export fn answer() {`,
		`    return 123;`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	mainText := strings.Join([]string{
		`import "lib.tiny" as Lib;`,
		`const value = Lib.answer();`,
	}, "\n")

	scope := fileBaseScope(pathToFileURI(mainPath), mainText)
	sym, ok := scope.Resolve("value")
	if !ok {
		t.Fatalf("expected imported call result to be defined")
	}
	if sym.Type != "number" {
		t.Fatalf("expected imported inferred return type number, got %q", sym.Type)
	}

	exports := loadTinyFileExports(libPath, map[string]bool{})
	answer, ok := exports["answer"]
	if !ok {
		t.Fatalf("expected answer export, got %#v", exports)
	}
	if answer.Returns != "number" {
		t.Fatalf("expected exported answer return type number, got %q", answer.Returns)
	}
}

func TestLSPImportedNamespaceInterfaceArgMatchesQualifiedReturn(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export interface ButtonComponent {`,
		`    type: number,`,
		`    label: string`,
		`}`,
		``,
		`export fn primaryButton(label: string): ButtonComponent {`,
		`    return { type: 2, label: label };`,
		`}`,
		``,
		`export fn disabled(component: ButtonComponent): ButtonComponent {`,
		`    return component;`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`const button = CommandsModule.disabled(CommandsModule.primaryButton("Click"));`,
		`button.label;`,
	}, "\n")

	diagnostics := semanticDiagnostics(pathToFileURI(mainPath), text)
	if diagnosticsContain(diagnostics, "cannot pass type 'interface:CommandsModule.ButtonComponent'") {
		t.Fatalf("expected qualified interface return to match unqualified namespace parameter, got %#v", diagnostics)
	}
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLSPTruthyPropertyNarrowingRemovesNull(t *testing.T) {
	text := strings.Join([]string{
		`interface User {`,
		`    id: string`,
		`}`,
		`interface Member {`,
		`    user: User | null`,
		`}`,
		`class Client {`,
		`    fn cacheUser(user: User) {}`,
		`    fn cacheMember(member: Member) {`,
		`        if member.user { this.cacheUser(member.user) }`,
		`    }`,
		`}`,
		`const client = Client()`,
		`client.cacheMember({ user: null })`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///truthy_property_narrowing.tiny", text)
	if diagnosticsContain(diagnostics, "cannot pass type 'interface:User | null'") {
		t.Fatalf("expected truthy member.user guard to narrow away null, got %#v", diagnostics)
	}
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLSPImportedReExportedClassAliasAsTypeHint(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	pingPath := filepath.Join(dir, "commands", "ping.tiny")
	if err := os.MkdirAll(filepath.Dir(pingPath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export class Interaction {`,
		`    fn reply(text: string) {}`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`export const Interaction = CommandsModule.Interaction;`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "../gateway.tiny" as Discord;`,
		`export fn run(interaction: Discord.Interaction) {`,
		`    interaction.reply("pong");`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics(pathToFileURI(pingPath), text)
	if diagnosticsContain(diagnostics, "unknown type: Discord.Interaction") {
		t.Fatalf("expected Discord.Interaction re-export alias to resolve as a type, got %#v", diagnostics)
	}
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLSPHoverImportedReExportedMethodWithoutOpeningImport(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	pingPath := filepath.Join(dir, "commands", "ping.tiny")
	if err := os.MkdirAll(filepath.Dir(pingPath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export class Interaction {`,
		`    fn replyComponents(components: array) {}`,
		`}`,
		`export fn container(children: array): object { return {} }`,
		`export fn textDisplay(content: string): object { return {} }`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`export const Interaction = CommandsModule.Interaction;`,
		`export const container = CommandsModule.container;`,
		`export const textDisplay = CommandsModule.textDisplay;`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "../gateway.tiny" as Discord;`,
		`export fn run(interaction: Discord.Interaction) {`,
		`    interaction.replyComponents([`,
		`        Discord.container([`,
		"            Discord.textDisplay(`## **Ping:** ${client.latency}ms`)",
		`        ])`,
		`    ])`,
		`}`,
	}, "\n")

	line := `    interaction.replyComponents([`
	result := getHover(pathToFileURI(pingPath), text, Position{
		Line:      2,
		Character: strings.Index(line, "replyComponents") + 3,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for imported re-exported method, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "replyComponents") {
		t.Fatalf("expected replyComponents hover, got %q", hover.Contents.Value)
	}
}

func TestLSPDiagnosticsEagerlyIndexReExportedDiscordTypes(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	messagePath := filepath.Join(dir, "message.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	pingPath := filepath.Join(dir, "commands", "ping.tiny")
	if err := os.MkdirAll(filepath.Dir(pingPath), 0755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export class Interaction {`,
		`    fn replyComponents(components: array) {}`,
		`}`,
		`export class CommandBuilder {`,
		`    fn setName(name: string): CommandBuilder { return this }`,
		`    fn setDescription(description: string): CommandBuilder { return this }`,
		`}`,
		`export class EmbedBuilder {}`,
		`export fn container(children: array): object { return {} }`,
		`export fn textDisplay(content: string): object { return {} }`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(messagePath, []byte(strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`export class Message {`,
		`    field interaction: CommandsModule.Interaction | null = null`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "message.tiny" as MessageModule;`,
		`import "commands.tiny" as CommandsModule;`,
		`export const Message = MessageModule.Message;`,
		`export const Interaction = CommandsModule.Interaction;`,
		`export const CommandBuilder = CommandsModule.CommandBuilder;`,
		`export const EmbedBuilder = CommandsModule.EmbedBuilder;`,
		`export const container = CommandsModule.container;`,
		`export const textDisplay = CommandsModule.textDisplay;`,
		`export fn newEmbed(): CommandsModule.EmbedBuilder { return CommandsModule.EmbedBuilder() }`,
		`export class Client {`,
		`    field latency: number = 0`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "../gateway.tiny" as Discord;`,
		`import "../commands.tiny" as Commands;`,
		``,
		`export fn info() {`,
		`    const cmd = Discord.CommandBuilder()`,
		`    cmd.setName("ping")`,
		`    cmd.setDescription("Replies with Pong!")`,
		`    return cmd`,
		`}`,
		``,
		`export fn run(interaction: Discord.Interaction, client: Discord.Client) {`,
		`    Discord.newEmbed()`,
		`    interaction.replyComponents([`,
		`        Discord.container([`,
		"            Discord.textDisplay(`## **Ping:** ${client.latency}ms`)",
		`        ])`,
		`    ])`,
		`}`,
	}, "\n")

	invalidateLSPFastCaches()
	lspImportExportCache = map[string]lspImportCacheEntry{}
	diagnostics := semanticDiagnostics(pathToFileURI(pingPath), text)
	if diagnosticsContain(diagnostics, "unknown type: Discord.Interaction") || diagnosticsContain(diagnostics, "unknown type: Discord.Client") {
		t.Fatalf("expected cold diagnostics to eagerly index Discord re-exported types, got %#v", diagnostics)
	}
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics, got %#v", diagnostics)
	}
}

func TestLSPCompletionAfterMultilineFluentCall(t *testing.T) {
	dir := t.TempDir()
	discordPath := filepath.Join(dir, "discord.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(discordPath, []byte(strings.Join([]string{
		`export class EmbedBuilder {`,
		`    fn setAuthor(name: string): EmbedBuilder { return this }`,
		`    fn setTitle(title: string): EmbedBuilder { return this }`,
		`}`,
		`export fn newEmbed(): EmbedBuilder { return EmbedBuilder() }`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "discord.tiny" as Discord;`,
		`Discord.newEmbed().`,
		`    `,
	}, "\n")

	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      2,
		Character: len(`    `),
	})
	if !completionLabelsContain(items, "setAuthor") {
		t.Fatalf("expected fluent newline completions to include setAuthor, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "setTitle") {
		t.Fatalf("expected fluent newline completions to include setTitle, got %#v", completionLabels(items))
	}
}

func TestLSPCompletionAndHoverAfterTypedMultilineFluentCall(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export class EmbedBuilder {`,
		`    fn setAuthor(name: string): EmbedBuilder { return this }`,
		`    fn setTitle(title: string): EmbedBuilder { return this }`,
		`}`,
		`export fn newEmbed(): EmbedBuilder { return EmbedBuilder() }`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	completionText := strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`CommandsModule.newEmbed().`,
		`    set`,
	}, "\n")
	items := getCompletions(pathToFileURI(mainPath), completionText, Position{
		Line:      2,
		Character: len(`    set`),
	})
	if !completionLabelsContain(items, "setAuthor") {
		t.Fatalf("expected typed multiline fluent completions to include setAuthor, got %#v", completionLabels(items))
	}

	hoverText := strings.Join([]string{
		`import "commands.tiny" as CommandsModule;`,
		`CommandsModule.newEmbed().`,
		`    setAuthor()`,
	}, "\n")
	result := getHover(pathToFileURI(mainPath), hoverText, Position{
		Line:      2,
		Character: len(`    setA`),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for multiline fluent method, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "setAuthor(name: string)") {
		t.Fatalf("expected setAuthor signature hover, got %q", hover.Contents.Value)
	}
}

func TestLSPImportedClassMethodCallbackParamInferenceAndHover(t *testing.T) {
	dir := t.TempDir()
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`export interface Interaction {`,
		`    id: string`,
		`}`,
		`export interface ButtonComponent {`,
		`    type: number`,
		`}`,
		`export class Client {`,
		`    field latency: number = 0`,
		`    fn tempPrimaryButton(label: string, handler: function(Interaction, Client), ttlMs?: number): ButtonComponent {`,
		`        return { type: 2 }`,
		`    }`,
		`}`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		`import "gateway.tiny" as Discord;`,
		`export fn run(interaction: Discord.Interaction, client: Discord.Client) {`,
		`    client.tempPrimaryButton("test", fn(i, v) {`,
		`        i`,
		`        v`,
		`    })`,
		`}`,
	}, "\n")

	uri := pathToFileURI(mainPath)
	iHover := getHover(uri, text, Position{Line: 3, Character: len(`        i`)})
	iResult, ok := iHover.(HoverResult)
	if !ok || !strings.Contains(iResult.Contents.Value, "Interaction") {
		t.Fatalf("expected i hover to infer Interaction, got %#v", iHover)
	}

	vHover := getHover(uri, text, Position{Line: 4, Character: len(`        v`)})
	vResult, ok := vHover.(HoverResult)
	if !ok || !strings.Contains(vResult.Contents.Value, "Client") {
		t.Fatalf("expected v hover to infer Client, got %#v", vHover)
	}

	methodHover := getHover(uri, text, Position{Line: 2, Character: strings.Index(getLine(text, 2), "tempPrimaryButton") + len("temp")})
	methodResult, ok := methodHover.(HoverResult)
	if !ok || !strings.Contains(methodResult.Contents.Value, "tempPrimaryButton") {
		t.Fatalf("expected tempPrimaryButton hover, got %#v", methodHover)
	}
}

func TestLSPHoverOneLineFunctionParameterInBody(t *testing.T) {
	dir := t.TempDir()
	commandsPath := filepath.Join(dir, "commands.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")

	if err := os.WriteFile(commandsPath, []byte(strings.Join([]string{
		`export class EmbedBuilder {}`,
		`export fn embedPayload(card: EmbedBuilder | object): object { return {} }`,
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := `import "commands.tiny" as CommandsModule;
export fn embedPayload(card: CommandsModule.EmbedBuilder | object): object { return CommandsModule.embedPayload(card) }`

	line := `export fn embedPayload(card: CommandsModule.EmbedBuilder | object): object { return CommandsModule.embedPayload(card) }`
	bodyCard := strings.LastIndex(line, "card")
	result := getHover(pathToFileURI(gatewayPath), text, Position{
		Line:      1,
		Character: bodyCard + 1,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for one-line body parameter, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "CommandsModule.EmbedBuilder | object") {
		t.Fatalf("expected card parameter type in hover, got %q", hover.Contents.Value)
	}

	def := getDefinition(pathToFileURI(gatewayPath), text, Position{
		Line:      1,
		Character: bodyCard + 1,
	})
	loc, ok := def.(Location)
	if !ok {
		t.Fatalf("expected definition for one-line body parameter, got %#v", def)
	}
	if loc.Range.Start.Line != 1 || loc.Range.Start.Character >= bodyCard {
		t.Fatalf("expected definition to point at parameter declaration, got %#v", loc)
	}
}

func TestLSPDidChangeDoesNotRunDiagnostics(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gateway.tiny")
	uri := pathToFileURI(path)
	text := strings.Join([]string{
		`import "commands.tiny" as CommandsModule`,
		`export fn embedPayload(card: CommandsModule.EmbedBuilder | object): object {`,
		`    return CommandsModule.embedPayload(card)`,
		`}`,
	}, "\n")

	invalidateLSPFastCaches()
	oldDocs := lspDocs
	defer func() { lspDocs = oldDocs }()
	lspDocs = map[string]string{}
	start := time.Now()
	refreshLSPDocumentFast(uri, text)
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("expected didChange refresh to avoid diagnostics work, took %s", elapsed)
	}
}

func TestLSPNamespaceCompletionIncludesExportedEnumsAndClasses(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(todoPath, []byte(strings.Join([]string{
		"import std \"io\";",
		"",
		"export enum TestEnum {",
		"",
		"}",
		"",
		"export const test = \"sssfsdfsdf\";",
		"export class TaskManager {",
		"    field tasks = [];",
		"    fn list() {",
		"        io.println(\"tasks\");",
		"    }",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"Todo.",
	}, "\n")

	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      1,
		Character: len("Todo."),
	})

	if !completionLabelsContain(items, "test") {
		t.Fatalf("expected Todo. completions to include exported const test, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "TaskManager") {
		t.Fatalf("expected Todo. completions to include exported class TaskManager, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "TestEnum") {
		t.Fatalf("expected Todo. completions to include exported enum TestEnum, got %#v", completionLabels(items))
	}
}

func TestLSPLocalEnumMemberCompletion(t *testing.T) {
	text := strings.Join([]string{
		"enum Status {",
		"    Pending,",
		"    Done = 2",
		"}",
		"Status.",
	}, "\n")

	items := getCompletions("file:///enum.tiny", text, Position{
		Line:      4,
		Character: len("Status."),
	})

	if !completionLabelsContain(items, "Pending") {
		t.Fatalf("expected Status. completions to include Pending, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "Done") {
		t.Fatalf("expected Status. completions to include Done, got %#v", completionLabels(items))
	}
}

func TestLSPImportedEnumMemberCompletion(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(todoPath, []byte(strings.Join([]string{
		"export enum Status {",
		"    Pending,",
		"    Done = 2",
		"}",
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"Todo.Status.",
	}, "\n")

	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      1,
		Character: len("Todo.Status."),
	})

	if !completionLabelsContain(items, "Pending") {
		t.Fatalf("expected Todo.Status. completions to include Pending, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "Done") {
		t.Fatalf("expected Todo.Status. completions to include Done, got %#v", completionLabels(items))
	}
}

func TestLSPNamespaceCompletionIncludesOpenExportedClass(t *testing.T) {
	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	todoURI := pathToFileURI(todoPath)

	err := os.WriteFile(todoPath, []byte("export const test = \"old\";\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	lspDocs[todoURI] = strings.Join([]string{
		"import std \"io\";",
		"",
		"export enum TestEnum {",
		"",
		"}",
		"",
		"export const test = \"sssfsdfsdf\";",
		"export class TaskManager {",
		"    field tasks = [];",
		"    fn list() {",
		"        io.println(\"tasks\");",
		"    }",
		"}",
	}, "\n")
	defer delete(lspDocs, todoURI)

	text := strings.Join([]string{
		"import \"todo.tiny\" as Todo;",
		"Todo.",
	}, "\n")

	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      1,
		Character: len("Todo."),
	})

	if !completionLabelsContain(items, "TaskManager") {
		t.Fatalf("expected Todo. completions from open document to include class TaskManager, got %#v", completionLabels(items))
	}
}

func TestLSPUnknownImportedClassDiagnostics(t *testing.T) {
	dir := t.TempDir()
	modelPath := filepath.Join(dir, "models.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(modelPath, []byte("export class User {}\n"), 0644)
	if err != nil {
		t.Fatal(err)
	}

	text := strings.Join([]string{
		"import \"models.tiny\" as models;",
		"let user: models.Missing = models.Missing();",
	}, "\n")

	diagnostics := semanticDiagnostics(pathToFileURI(mainPath), text)
	if !diagnosticsContain(diagnostics, "unknown type: models.Missing") {
		t.Fatalf("expected unknown imported class diagnostic, got %#v", diagnostics)
	}
}

func TestLSPPrivateMethodsHiddenOutsideClass(t *testing.T) {
	text := strings.Join([]string{
		"class SecretBox {",
		"    private fn unlock() {",
		"    }",
		"    fn open() {",
		"    }",
		"}",
		"const box = SecretBox();",
		"box.",
	}, "\n")

	items := getCompletions("file:///private.tiny", text, Position{
		Line:      7,
		Character: len("box."),
	})

	if completionLabelsContain(items, "unlock") {
		t.Fatalf("expected private method unlock to be hidden outside class, got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "open") {
		t.Fatalf("expected public method open outside class, got %#v", completionLabels(items))
	}
}

func TestLSPPrivateMethodsVisibleOnThis(t *testing.T) {
	text := strings.Join([]string{
		"class SecretBox {",
		"    private fn unlock() {",
		"    }",
		"    fn open() {",
		"        this.",
		"    }",
		"}",
	}, "\n")

	items := getCompletions("file:///private.tiny", text, Position{
		Line:      4,
		Character: len("        this."),
	})

	if !completionLabelsContain(items, "unlock") {
		t.Fatalf("expected private method unlock on this., got %#v", completionLabels(items))
	}
	if !completionLabelsContain(items, "open") {
		t.Fatalf("expected public method open on this., got %#v", completionLabels(items))
	}
}

func TestLSPPrivateMemberAccessDiagnostics(t *testing.T) {
	text := strings.Join([]string{
		"class SecretBox {",
		"    private fn unlock() {",
		"    }",
		"    fn open() {",
		"        this.unlock();",
		"    }",
		"}",
		"const box = SecretBox();",
		"box.unlock();",
	}, "\n")

	diagnostics := semanticDiagnostics("file:///private_access.tiny", text)
	if !diagnosticsContain(diagnostics, "private member is not accessible: unlock") {
		t.Fatalf("expected private member diagnostic, got %#v", diagnostics)
	}
}

func TestLSPCallableCompletionInsertText(t *testing.T) {
	text := strings.Join([]string{
		"fn greet() {",
		"}",
		"gre",
	}, "\n")

	items := getCompletions("file:///completion.tiny", text, Position{
		Line:      2,
		Character: len("gre"),
	})

	item, ok := completionItemByLabel(items, "greet")
	if !ok {
		t.Fatalf("expected greet completion, got %#v", completionLabels(items))
	}
	if item.InsertText != "greet($0)" || item.InsertTextFormat != 2 {
		t.Fatalf("expected callable snippet insert text, got %#v", item)
	}
}

func TestLSPVirtualRootFileAutoImportDoesNotWalkFilesystemRoot(t *testing.T) {
	done := make(chan []string, 1)
	go func() {
		done <- scanProjectTinyFiles("file:///completion.tiny")
	}()

	select {
	case files := <-done:
		if len(files) != 0 {
			t.Fatalf("expected no project files for virtual root file, got %#v", files)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("scanProjectTinyFiles walked too much filesystem for virtual root file")
	}
}

func TestLSPSnippetCompletions(t *testing.T) {
	items := getCompletions("file:///snippets.tiny", "", Position{
		Line:      0,
		Character: 0,
	})

	item, ok := completionItemByLabel(items, "for")
	if !ok {
		t.Fatalf("expected for snippet completion, got %#v", completionLabels(items))
	}
	if item.InsertTextFormat != 2 || !strings.Contains(item.InsertText, "for let ${1:i}") {
		t.Fatalf("expected for loop snippet, got %#v", item)
	}

	item, ok = completionItemByLabel(items, "fn")
	if !ok {
		t.Fatalf("expected fn snippet completion, got %#v", completionLabels(items))
	}
	if item.InsertTextFormat != 2 || !strings.Contains(item.InsertText, "fn ${1:name}") {
		t.Fatalf("expected fn snippet completion, got %#v", item)
	}
}

func TestLSPStdAutoImportCompletion(t *testing.T) {
	text := "io"
	items := getCompletions("file:///std_auto.tiny", text, Position{
		Line:      0,
		Character: len("io"),
	})

	item, ok := completionItemByLabel(items, "io")
	if !ok {
		t.Fatalf("expected io auto-import completion, got %#v", completionLabels(items))
	}
	if len(item.AdditionalTextEdits) != 1 || item.AdditionalTextEdits[0].NewText != "import std \"io\";\n" {
		t.Fatalf("expected io completion to add std import, got %#v", item)
	}
}

func TestLSPStdPrivateReturnTypeCompletion(t *testing.T) {
	text := strings.Join([]string{
		"import std \"http\";",
		"",
		"const server = http.server(3000)",
		"",
		"server.",
	}, "\n")

	items := getCompletions("file:///std_http_server.tiny", text, Position{
		Line:      4,
		Character: len("server."),
	})

	if !completionLabelsContain(items, "get") || !completionLabelsContain(items, "post") || !completionLabelsContain(items, "start") {
		t.Fatalf("expected http server method completions, got %#v", completionLabels(items))
	}

	namespaceItems := getCompletions("file:///std_http_server.tiny", text+"\nhttp.", Position{
		Line:      5,
		Character: len("http."),
	})

	if !completionLabelsContain(namespaceItems, "Server") {
		t.Fatalf("expected exported Server type to be visible, got %#v", completionLabels(namespaceItems))
	}
}

func TestLSPStdHttpRouteHandlerRequestCompletion(t *testing.T) {
	text := strings.Join([]string{
		"import std \"http\";",
		"",
		"const server = http.server(3000)",
		"server.get(\"/\", fn(req) {",
		"    req.",
		"})",
	}, "\n")

	items := getCompletions("file:///std_http_route_handler.tiny", text, Position{
		Line:      4,
		Character: len("    req."),
	})

	if !completionLabelsContain(items, "path") || !completionLabelsContain(items, "method") || !completionLabelsContain(items, "headers") {
		t.Fatalf("expected RequestObject completions for route handler req, got %#v", completionLabels(items))
	}
}

func TestLSPVariableInferenceFromHttpRequestBodyProperty(t *testing.T) {
	text := strings.Join([]string{
		`import std "http" as http`,
		`const server = http.server(3000)`,
		`server.get("/update", fn(i) {`,
		`    const ID = i.body`,
		`})`,
	}, "\n")

	scope := scopeAtPosition("file:///test_http_request_body_var.tiny", text, Position{
		Line:      3,
		Character: len(`    const ID = i.body`),
	})
	sym, ok := scope.Resolve("ID")
	if !ok {
		t.Fatalf("expected ID variable to be in scope")
	}
	if sym.Type != "string" {
		t.Fatalf("expected ID to infer string from i.body, got %q", sym.Type)
	}
}

func TestLSPFileAutoImportCompletion(t *testing.T) {
	lspEnableHeavyAutoImportCompletions = true
	defer func() { lspEnableHeavyAutoImportCompletions = false }()

	dir := t.TempDir()
	todoPath := filepath.Join(dir, "todo.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	if err := os.WriteFile(todoPath, []byte(strings.Join([]string{
		"export class TaskManager {",
		"    fn list() {",
		"    }",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatal(err)
	}

	text := "Task"
	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      0,
		Character: len("Task"),
	})

	item, ok := completionItemByLabel(items, "TaskManager")
	if !ok {
		t.Fatalf("expected TaskManager auto-import completion, got %#v", completionLabels(items))
	}
	if item.InsertText != "Todo.TaskManager($0)" || item.InsertTextFormat != 2 {
		t.Fatalf("expected namespaced constructor snippet, got %#v", item)
	}
	if len(item.AdditionalTextEdits) != 1 || item.AdditionalTextEdits[0].NewText != "import \"todo.tiny\" as Todo;\n" {
		t.Fatalf("expected todo import edit, got %#v", item)
	}
}

func TestLSPLibraryAutoImportCompletion(t *testing.T) {
	lspEnableHeavyAutoImportCompletions = true
	defer func() { lspEnableHeavyAutoImportCompletions = false }()

	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	// Create local library dependency root
	libRoot := filepath.Join(dir, "TinyHttpx")
	if err := os.MkdirAll(filepath.Join(libRoot, "src"), 0755); err != nil {
		t.Fatalf("create libRoot src dir: %v", err)
	}

	// Write tiny.json in project root referencing local path dependency
	projRoot := filepath.Join(dir, "myproject")
	writeConfigForTest(t, filepath.Join(projRoot, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"TinyHttpx": {
				Source: "github:confh/TinyHttpx",
				Path:   "../TinyHttpx",
			},
		},
	})

	// Write files in local dependency
	writeConfigForTest(t, filepath.Join(libRoot, "tiny.json"), TinyProjectConfig{
		Entry: "src/httpx.tiny",
	})
	if err := os.WriteFile(filepath.Join(libRoot, "src", "httpx.tiny"), []byte(strings.Join([]string{
		"export class Context {",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatalf("write httpx.tiny: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libRoot, "src", "file2.tiny"), []byte(strings.Join([]string{
		"export class User {",
		"}",
	}, "\n")), 0644); err != nil {
		t.Fatalf("write file2.tiny: %v", err)
	}

	mainPath := filepath.Join(projRoot, "src", "main.tiny")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0755); err != nil {
		t.Fatalf("create main.tiny dir: %v", err)
	}

	// 1. Completion for Context from the main entry file of the library
	// (Should resolve to import lib "confh/TinyHttpx" as TinyHttpx)
	text := "Cont"
	items := getCompletions(pathToFileURI(mainPath), text, Position{
		Line:      0,
		Character: len("Cont"),
	})

	item, ok := completionItemByLabel(items, "Context")
	if !ok {
		t.Fatalf("expected Context auto-import completion, got %#v", completionLabels(items))
	}
	if item.InsertText != "TinyHttpx.Context($0)" || item.InsertTextFormat != 2 {
		t.Fatalf("expected Context constructor insert text, got %#v", item)
	}
	if len(item.AdditionalTextEdits) != 1 || item.AdditionalTextEdits[0].NewText != "import lib \"confh/TinyHttpx\" as TinyHttpx;\n" {
		t.Fatalf("expected library import edit, got %#v", item)
	}

	// 2. Completion for User from the sub-path file of the library
	// (Should resolve to import lib "confh/TinyHttpx/src/file2.tiny" as File2)
	text2 := "Us"
	items2 := getCompletions(pathToFileURI(mainPath), text2, Position{
		Line:      0,
		Character: len("Us"),
	})

	item2, ok := completionItemByLabel(items2, "User")
	if !ok {
		t.Fatalf("expected User auto-import completion, got %#v", completionLabels(items2))
	}
	if item2.InsertText != "File2.User($0)" || item2.InsertTextFormat != 2 {
		t.Fatalf("expected User constructor insert text, got %#v", item2)
	}
	if len(item2.AdditionalTextEdits) != 1 || item2.AdditionalTextEdits[0].NewText != "import lib \"confh/TinyHttpx/src/file2.tiny\" as File2;\n" {
		t.Fatalf("expected sub-path library import edit, got %#v", item2)
	}
}

func TestLSPFullLibraryImportPathDiagnostics(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	projRoot := filepath.Join(dir, "project")
	mainPath := filepath.Join(projRoot, "src", "main.tiny")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0755); err != nil {
		t.Fatalf("create project src: %v", err)
	}
	writeConfigForTest(t, filepath.Join(projRoot, "tiny.json"), TinyProjectConfig{})

	text := `import lib "confh/TinyColors" as Library`
	diagnostics := importDiagnostics(pathToFileURI(mainPath), text)
	if len(diagnostics) != 1 {
		t.Fatalf("expected one import diagnostic, got %#v", diagnostics)
	}
	got, _ := diagnostics[0]["message"].(string)
	want := "library is not installed: confh/TinyColors"
	if got != want {
		t.Fatalf("expected %q, got %q", want, got)
	}

	for _, partial := range []string{
		`import lib "c`,
		`import lib "co`,
		`import lib "confh`,
		`import lib "confh/`,
	} {
		if diagnostics := importDiagnostics(pathToFileURI(mainPath), partial); len(diagnostics) != 0 {
			t.Fatalf("expected no diagnostics for incomplete import %q, got %#v", partial, diagnostics)
		}
	}
}

func TestLSPImportDiagnosticsIgnoreImportsInsideStrings(t *testing.T) {
	text := strings.Join([]string{
		`import std "runtime" as runtime`,
		``,
		`const vm = runtime.newVM({`,
		`    isolated: true,`,
		`    allowedStdlib: { io: true },`,
		`    runMainOnLoad: true`,
		`})`,
		``,
		"vm.loadSource(`import \"io\"",
		"",
		"`)",
	},
		"\n",
	)

	diagnostics := importDiagnostics("file:///runtime_source_string.tiny", text)
	for _, diagnostic := range diagnostics {
		if message, _ := diagnostic["message"].(string); strings.Contains(message, "import file not found: io") {
			t.Fatalf("expected import inside string to be ignored, got %#v", diagnostics)
		}
	}
}

func TestLSPReferencesAndRename(t *testing.T) {
	text := strings.Join([]string{
		"const total = 1;",
		"io.println(total);",
		"const next = total + 1;",
	}, "\n")

	refs := getReferences("file:///refs.tiny", text, Position{
		Line:      0,
		Character: len("const total") - 1,
	}, true)
	if len(refs) != 3 {
		t.Fatalf("expected 3 references for total, got %#v", refs)
	}

	edit := getRenameEdit("file:///refs.tiny", text, Position{
		Line:      0,
		Character: len("const total") - 1,
	}, "sum")
	if len(edit.Changes["file:///refs.tiny"]) != 3 {
		t.Fatalf("expected 3 rename edits, got %#v", edit)
	}
}

func TestLSPDocumentHighlightsUseReferences(t *testing.T) {
	text := strings.Join([]string{
		`const status = "ok";`,
		`const payload = {`,
		`    status: status,`,
		`    note: "status",`,
		`}`,
		`// status`,
		`io.println(status);`,
	}, "\n")

	highlights := getDocumentHighlights("file:///highlights.tiny", text, Position{
		Line:      0,
		Character: len("const status") - 1,
	})

	if len(highlights) != 3 {
		t.Fatalf("expected declaration and two value references to be highlighted, got %#v", highlights)
	}
	for _, highlight := range highlights {
		line := highlight.Range.Start.Line
		if line == 3 || line == 5 {
			t.Fatalf("string/comment occurrence was highlighted: %#v", highlights)
		}
		if line == 2 && highlight.Range.Start.Character == len(`    `) {
			t.Fatalf("object literal key was highlighted: %#v", highlights)
		}
	}
}

func TestLSPRenameUsesURIKeysForOpenDocuments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "refs.tiny")
	uri := pathToFileURI(path)
	text := strings.Join([]string{
		"const total = 1;",
		"io.println(total);",
		"const next = total + 1;",
	}, "\n")

	lspDocs[path] = text
	defer delete(lspDocs, path)

	edit := getRenameEdit(uri, text, Position{
		Line:      0,
		Character: len("const total") - 1,
	}, "sum")

	if _, ok := edit.Changes[path]; ok {
		t.Fatalf("rename edit used path key %q instead of URI key: %#v", path, edit)
	}
	if len(edit.Changes[uri]) != 3 {
		t.Fatalf("expected 3 rename edits for URI %q, got %#v", uri, edit)
	}
}

func TestLSPRenameIgnoresShadowedCallbackVariableAfterCallback(t *testing.T) {
	text := strings.Join([]string{
		`const test = Httpx.app({`,
		`    port: 3000`,
		`})`,
		``,
		`test.onRequest(fn(ctx: Httpx.Context, next) {`,
		`    let path = ctx.path().replace("/", "")`,
		`    const test = files[path]`,
		`    return test`,
		`})`,
		``,
		`test.start()`,
	}, "\n")

	startScope := scopeAtPosition("file:///sample.tiny", text, Position{
		Line:      10,
		Character: 1,
	})
	sym, ok := startScope.Resolve("test")
	if !ok {
		t.Fatal("expected test to resolve at test.start()")
	}
	if sym.Line != 1 {
		t.Fatalf("expected test.start() to resolve to outer declaration on line 1, got %#v", sym)
	}

	edit := getRenameEdit("file:///sample.tiny", text, Position{
		Line:      0,
		Character: len("const test") - 1,
	}, "app")

	edits := edit.Changes["file:///sample.tiny"]
	if len(edits) != 3 {
		t.Fatalf("expected 3 edits for outer test rename, got %#v", edit)
	}
	for _, textEdit := range edits {
		line := textEdit.Range.Start.Line
		if line != 0 && line != 4 && line != 10 {
			t.Fatalf("outer rename returned unexpected edit range %#v in %#v", textEdit.Range, edit)
		}
	}
}

func TestLSPRenameIgnoresObjectKeysStringsAndComments(t *testing.T) {
	text := strings.Join([]string{
		`const status = "ok";`,
		`const payload = {`,
		`    status: status,`,
		`    note: "status",`,
		`}`,
		`// status`,
		`io.println(status);`,
	}, "\n")

	edit := getRenameEdit("file:///rename_filters.tiny", text, Position{
		Line:      0,
		Character: len("const status") - 1,
	}, "state")

	edits := edit.Changes["file:///rename_filters.tiny"]
	if len(edits) != 3 {
		t.Fatalf("expected declaration and two value references to be renamed, got %#v", edit)
	}
	for _, textEdit := range edits {
		if textEdit.Range.Start.Line == 2 && textEdit.Range.Start.Character == len(`    `) {
			t.Fatalf("object literal key was renamed: %#v", edit)
		}
		if textEdit.Range.Start.Line == 3 || textEdit.Range.Start.Line == 5 {
			t.Fatalf("string/comment occurrence was renamed: %#v", edit)
		}
	}
}

func TestLSPRenameKeepsUnrelatedClassMethodsSeparate(t *testing.T) {
	text := strings.Join([]string{
		`class A {`,
		`    fn run() {}`,
		`}`,
		`class B {`,
		`    fn run() {}`,
		`}`,
		`let a = A()`,
		`let b = B()`,
		`a.run()`,
		`b.run()`,
	}, "\n")

	edit := getRenameEdit("file:///rename_methods.tiny", text, Position{
		Line:      8,
		Character: strings.Index(`a.run()`, "run") + 1,
	}, "start")

	edits := edit.Changes["file:///rename_methods.tiny"]
	if len(edits) != 2 {
		t.Fatalf("expected only A.run declaration and call to be renamed, got %#v", edit)
	}
	for _, textEdit := range edits {
		if textEdit.Range.Start.Line != 1 && textEdit.Range.Start.Line != 8 {
			t.Fatalf("unrelated method occurrence was renamed: %#v", edit)
		}
	}
}

func TestLSPUnusedSymbolDiagnostics(t *testing.T) {
	text := strings.Join([]string{
		"import std \"io\";",
		"const used = 1;",
		"const unused = 2;",
		"io.println(used);",
	}, "\n")

	diagnostics := semanticDiagnostics("file:///unused.tiny", text)
	if !diagnosticsContain(diagnostics, "unused variable: unused") {
		t.Fatalf("expected unused variable diagnostic, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "unused import: io") {
		t.Fatalf("did not expect used import diagnostic, got %#v", diagnostics)
	}
}

func TestLSPUnusedSymbolDiagnosticsCountsTemplateInterpolationUses(t *testing.T) {
	text := strings.Join([]string{
		"import std \"io\";",
		"import std \"time\";",
		"let start = time.clock();",
		"let end = time.clock();",
		"io.println(`Tiny Pure Logic Elapsed: ${end - start}ms`);",
	}, "\n")

	diagnostics := semanticDiagnostics("file:///template.tiny", text)
	if diagnosticsContain(diagnostics, "unused variable: start") {
		t.Fatalf("did not expect start to be unused when referenced in interpolation, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "unused variable: end") {
		t.Fatalf("did not expect end to be unused when referenced in interpolation, got %#v", diagnostics)
	}
}

func TestLSPDiagnosticsUsedBeforeInitialization(t *testing.T) {
	t.Run("const used before declaration line", func(t *testing.T) {
		text := strings.Join([]string{
			`io.println(test.toLowerCase());`,
			`const test = "hello";`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///used_before_init.tiny", text)
		if !diagnosticsContain(diagnostics, "'test' is used before initialization") {
			t.Fatalf("expected used-before-init diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("let assigned before declaration line", func(t *testing.T) {
		text := strings.Join([]string{
			`test = "world";`,
			`const test = "hello";`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///assign_before_decl.tiny", text)
		if !diagnosticsContain(diagnostics, "'test' is used before initialization") {
			t.Fatalf("expected used-before-init diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("normal usage after declaration no diagnostic", func(t *testing.T) {
		text := strings.Join([]string{
			`const test = "hello";`,
			`io.println(test.toLowerCase());`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///normal_use.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic, got %#v", diagnostics)
		}
	})

	t.Run("no false positive across functions with same variable names", func(t *testing.T) {
		text := strings.Join([]string{
			`fn fibText(n) {`,
			`  let result = 0`,
			`  let i = 0`,
			`  return result`,
			`}`,
			`fn joinWords() {`,
			`  let result = ""`,
			`  let i = 0`,
			`  return result`,
			`}`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///cross_func.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic across functions, got %#v", diagnostics)
		}
	})

	t.Run("for loop increment no false positive", func(t *testing.T) {
		text := strings.Join([]string{
			`fn foo() {`,
			`  for (let i = 0; i < 10; i++) {`,
			`    io.println(i)`,
			`  }`,
			`}`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///for_loop.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic in for loop, got %#v", diagnostics)
		}
	})

	t.Run("for loop decrement no false positive", func(t *testing.T) {
		text := strings.Join([]string{
			`fn foo() {`,
			`  for (let i = 10; i > 0; i--) {`,
			`    io.println(i)`,
			`  }`,
			`}`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///for_loop_dec.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic in for loop with decrement, got %#v", diagnostics)
		}
	})

	t.Run("for loop plus assign no false positive", func(t *testing.T) {
		text := strings.Join([]string{
			`fn foo() {`,
			`  for (let i = 0; i < 10; i += 1) {`,
			`    io.println(i)`,
			`  }`,
			`}`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///for_loop_plusassign.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic in for loop with +=, got %#v", diagnostics)
		}
	})

	t.Run("dual for loops same variable no false positive", func(t *testing.T) {
		text := strings.Join([]string{
			`fn foo() {`,
			`  for let i = 0; i < 3; i++ {`,
			`    io.println(i)`,
			`  }`,
			`  for let i = 0; i < 3; i++ {`,
			`    io.println(i)`,
			`  }`,
			`}`,
		}, "\n")

		diagnostics := semanticDiagnostics("file:///dual_for.tiny", text)
		if diagnosticsContain(diagnostics, "used before initialization") {
			t.Fatalf("expected no used-before-init diagnostic in dual for loops, got %#v", diagnostics)
		}
	})
}

func TestLSPSemanticDiagnosticsNewStdModuleScriptDoesNotHang(t *testing.T) {
	bytes, err := os.ReadFile(filepath.Join("scripts", "new_std_module.tiny"))
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan []map[string]any, 1)
	go func() {
		done <- semanticDiagnostics("file:///scripts/new_std_module.tiny", string(bytes))
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("semantic diagnostics hung for scripts/new_std_module.tiny")
	}
}

func TestLSPDocumentSymbolsSkipAnonymousHTTPCallbacks(t *testing.T) {
	text := strings.Join([]string{
		"import std \"http\";",
		"import std \"json\";",
		"import std \"io\";",
		"",
		"let server = http.server(8090);",
		"",
		"server.get(\"/\", fn(req) {",
		"    return json.stringify({",
		"        method: req.method,",
		"        path: req.path,",
		"        query: req.query",
		"    });",
		"});",
		"",
		"server.post(\"/echo\", fn(req) {",
		"    return req.body;",
		"});",
		"",
		"io.println(\"Listening on http://localhost:8090\");",
		"server.start();",
	}, "\n")

	symbols := getDocumentSymbols("file:///server.tiny", text)
	assertDocumentSymbolsHaveNames(t, symbols)
	if !documentSymbolLabelsContain(symbols, "server") {
		t.Fatalf("expected document symbols to include server variable, got %#v", documentSymbolLabels(symbols))
	}
}

func TestLSPDocumentSymbolFromLineUsesLexerSpacing(t *testing.T) {
	cases := []struct {
		line   string
		name   string
		detail string
		kind   int
	}{
		{line: "\t export   fn   enum(values: array:string): array:string {", name: "enum", detail: "export function", kind: 12},
		{line: "  export   external   fn   hostCall(input: string): number", name: "hostCall", detail: "export external function", kind: 12},
		{line: "export\texternal\tconst\thostValue : string", name: "hostValue", detail: "export external global", kind: 13},
		{line: "  embedtext   \"data.txt\"   const   embeddedText", name: "embeddedText", detail: "embedtext", kind: 13},
	}

	for i, tc := range cases {
		sym, ok := documentSymbolFromLine(tc.line, strings.TrimSpace(tc.line), i)
		if !ok {
			t.Fatalf("expected symbol for %q", tc.line)
		}
		if sym.Name != tc.name || sym.Detail != tc.detail || sym.Kind != tc.kind {
			t.Fatalf("symbol for %q = %#v, want name=%q detail=%q kind=%d", tc.line, sym, tc.name, tc.detail, tc.kind)
		}
	}
}

func TestLSPDocumentSymbolFromLineSkipsAnonymousFunction(t *testing.T) {
	if sym, ok := documentSymbolFromLine("server.get(\"/\", fn(req) {", "server.get(\"/\", fn(req) {", 0); ok {
		t.Fatalf("expected anonymous callback to be skipped, got %#v", sym)
	}
}

func TestLSPStdHttpResponseReturnTypeAllowsStatusAccess(t *testing.T) {
	text := strings.Join([]string{
		"import std \"http\";",
		"import std \"io\";",
		"",
		"let req = http.get(\"https://example.com\");",
		"io.println(req.status);",
	}, "\n")

	diagnostics := semanticDiagnostics("file:///http_response.tiny", text)
	if diagnosticsContain(diagnostics, "undefined method or property: status") {
		t.Fatalf("expected http.get result to expose HttpResponse.status, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "unknown type: HttpResponse") {
		t.Fatalf("expected std http HttpResponse type to resolve, got %#v", diagnostics)
	}
}

func completionLabelsContain(items []CompletionItem, label string) bool {
	for _, item := range items {
		if item.Label == label {
			return true
		}
	}
	return false
}

func completionItemByLabel(items []CompletionItem, label string) (CompletionItem, bool) {
	for _, item := range items {
		if item.Label == label {
			return item, true
		}
	}
	return CompletionItem{}, false
}

func completionLabels(items []CompletionItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		labels = append(labels, item.Label)
	}
	return labels
}

func diagnosticsContain(diagnostics []map[string]any, needle string) bool {
	for _, diagnostic := range diagnostics {
		message, _ := diagnostic["message"].(string)
		if strings.Contains(message, needle) {
			return true
		}
	}
	return false
}

func assertDocumentSymbolsHaveNames(t *testing.T, symbols []DocumentSymbol) {
	t.Helper()
	for _, symbol := range symbols {
		if strings.TrimSpace(symbol.Name) == "" {
			t.Fatalf("document symbol has empty name: %#v", symbol)
		}
		assertDocumentSymbolsHaveNames(t, symbol.Children)
	}
}

func documentSymbolLabelsContain(symbols []DocumentSymbol, label string) bool {
	for _, symbol := range symbols {
		if symbol.Name == label || documentSymbolLabelsContain(symbol.Children, label) {
			return true
		}
	}
	return false
}

func documentSymbolLabels(symbols []DocumentSymbol) []string {
	labels := []string{}
	for _, symbol := range symbols {
		labels = append(labels, symbol.Name)
		labels = append(labels, documentSymbolLabels(symbol.Children)...)
	}
	return labels
}

func TestLSPClassHoistingAndThisInference(t *testing.T) {
	text := strings.Join([]string{
		`class A {`,
		`    fn method(bInstance: B) {`,
		`        myGlobalFunc();`,
		`        const typeOfMethodCall = this.aMethod();`,
		`        bInstance.`,
		`    }`,
		`    fn aMethod(): string {`,
		`        return "hello";`,
		`    }`,
		`}`,
		``,
		`class B {`,
		`    fn bMethod(): number {`,
		`        return 42;`,
		`    }`,
		`}`,
		``,
		`fn myGlobalFunc(): bool {`,
		`    return true;`,
		`}`,
	}, "\n")

	// 1. Check Hover inside A.method for myGlobalFunc (defined after A)
	hoverResult := getHover("file:///test.tiny", text, Position{
		Line:      2,
		Character: len("        myGlobal"),
	})
	hover, ok := hoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for myGlobalFunc, got %#v", hoverResult)
	}
	if !strings.Contains(hover.Contents.Value, "myGlobalFunc(): bool") {
		t.Fatalf("unexpected hover content: %q", hover.Contents.Value)
	}

	// 2. Check this.aMethod() return type inference
	scope := scopeAtPosition("file:///test.tiny", text, Position{
		Line:      4,
		Character: len("        bInstance."),
	})
	sym, ok := scope.Resolve("typeOfMethodCall")
	if !ok {
		t.Fatalf("expected typeOfMethodCall to be in scope")
	}
	if sym.Type != "string" {
		t.Fatalf("expected typeOfMethodCall to have type string, got %q", sym.Type)
	}

	// 3. Check bInstance autocomplete shows bMethod (B is defined after A)
	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      4,
		Character: len("        bInstance."),
	})
	if !completionLabelsContain(completions, "bMethod") {
		t.Fatalf("expected bInstance. completions to include bMethod, got %#v", completionLabels(completions))
	}
}

func TestLSPHoverOnNullableClassFieldMember(t *testing.T) {
	text := strings.Join([]string{
		`class Bot {`,
		`    fn sendMessage(text: string) {}`,
		`}`,
		``,
		`class Event {`,
		`    field bot: Bot | null = null`,
		`    fn reply() {`,
		`        this.bot.sendMessage("hello");`,
		`    }`,
		`}`,
	}, "\n")

	hoverResult := getHover("file:///test.tiny", text, Position{
		Line:      7,
		Character: strings.Index(`        this.bot.sendMessage("hello");`, "sendMessage") + 3,
	})
	hover, ok := hoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", hoverResult)
	}
	if !strings.Contains(hover.Contents.Value, "sendMessage(text: string)") {
		t.Fatalf("unexpected hover content: %q", hover.Contents.Value)
	}
}

func TestLSPNullableInterfaceCompletionsAndHover(t *testing.T) {
	text := strings.Join([]string{
		`export interface ChatEvent {`,
		`    chatID: number,`,
		`    text: string,`,
		`}`,
		``,
		`class Event {`,
		`    field data: ChatEvent | null = null`,
		`    fn reply() {`,
		`        this.data.`,
		`    }`,
		`}`,
	}, "\n")

	// 1. Check autocomplete on nullable interface field
	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      8,
		Character: len("        this.data."),
	})
	if !completionLabelsContain(completions, "chatID") {
		t.Fatalf("expected completions to include chatID, got %#v", completionLabels(completions))
	}
	if !completionLabelsContain(completions, "text") {
		t.Fatalf("expected completions to include text, got %#v", completionLabels(completions))
	}

	// 2. Check hover on nullable interface field member
	textWithChatID := strings.Join([]string{
		`export interface ChatEvent {`,
		`    chatID: number,`,
		`    text: string,`,
		`}`,
		``,
		`class Event {`,
		`    field data: ChatEvent | null = null`,
		`    fn reply() {`,
		`        this.data.chatID`,
		`    }`,
		`}`,
	}, "\n")

	hoverResult := getHover("file:///test.tiny", textWithChatID, Position{
		Line:      8,
		Character: len("        this.data.chat"),
	})
	hover, ok := hoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", hoverResult)
	}
	if !strings.Contains(hover.Contents.Value, "chatID") {
		t.Fatalf("unexpected hover content: %q", hover.Contents.Value)
	}
}

func TestLSPNullableQuestionMarkFieldsAndAutoCompletion(t *testing.T) {
	text := strings.Join([]string{
		`class Bot {`,
		`    field handle? = null`,
		`    field handler?: Bot = null`,
		`    fn reply() {`,
		`        this.handle.`,
		`    }`,
		`    fn start() {`,
		`        this.handler.`,
		`    }`,
		`}`,
	}, "\n")

	// 1. Verify that 'handle' is parsed with type 'any | null'
	scope := scopeAtPosition("file:///test.tiny", text, Position{
		Line:      4,
		Character: len("        this.handle."),
	})
	botSym, ok := scope.Resolve("Bot")
	if !ok {
		t.Fatalf("expected Bot in scope")
	}
	handleField, ok := botSym.Fields["handle"]
	if !ok {
		t.Fatalf("expected 'handle' field on Bot")
	}
	if handleField.Type != "any | null" {
		t.Fatalf("expected 'handle' type to be 'any | null', got %q", handleField.Type)
	}

	// 2. Verify that 'handler' is parsed with type 'class:Bot | null'
	handlerField, ok := botSym.Fields["handler"]
	if !ok {
		t.Fatalf("expected 'handler' field on Bot")
	}
	if handlerField.Type != "class:Bot | null" {
		t.Fatalf("expected 'handler' type to be 'class:Bot | null', got %q", handlerField.Type)
	}

	// 3. Check autocomplete on `this.handler.`
	// handler has '?' suffix and type Bot, so its type is class:Bot | null.
	// completions should include 'reply' and 'start' and also have AdditionalTextEdits replacing '.' with '?.'
	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      7,
		Character: len("        this.handler."),
	})
	if !completionLabelsContain(completions, "reply") {
		t.Fatalf("expected completions to include reply, got %#v", completionLabels(completions))
	}
	for _, item := range completions {
		if len(item.AdditionalTextEdits) == 0 {
			t.Fatalf("expected AdditionalTextEdits for nullable receiver completion, got none for item %q", item.Label)
		}
		edit := item.AdditionalTextEdits[0]
		if edit.NewText != "?." {
			t.Fatalf("expected NewText to be '?.', got %q", edit.NewText)
		}
		if edit.Range.Start.Character != len("        this.handler") {
			t.Fatalf("expected edit start character to be %d, got %d", len("        this.handler"), edit.Range.Start.Character)
		}
	}
}

func TestLSPNullableReceiverMethodSignatureHelp(t *testing.T) {
	text := strings.Join([]string{
		`class Bot {`,
		`    field handler?: Bot = null`,
		`    fn reply(text: string, count: number) {`,
		`        this.handler.reply(`,
		`    }`,
		`}`,
	}, "\n")

	result := getSignatureHelp("file:///test.tiny", text, Position{
		Line:      3,
		Character: len("        this.handler.reply("),
	})
	help, ok := result.(SignatureHelp)
	if !ok {
		t.Fatalf("expected signature help, got %#v", result)
	}
	if len(help.Signatures) != 1 || !strings.Contains(help.Signatures[0].Label, "reply(text: string, count: number)") {
		t.Fatalf("unexpected signature help: %#v", help)
	}
}

func TestLSPMemberCompletionWhileTyping(t *testing.T) {
	text := strings.Join([]string{
		`class Bot {`,
		`    field handler?: Bot = null`,
		`    fn reply() {`,
		`        this.handler.rep`,
		`    }`,
		`}`,
	}, "\n")

	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      3,
		Character: len("        this.handler.rep"),
	})
	if !completionLabelsContain(completions, "reply") {
		t.Fatalf("expected completions to include reply when typing, got %#v", completionLabels(completions))
	}
}

func TestLSPCompletionAvoidsDuplicateParens(t *testing.T) {
	text := strings.Join([]string{
		`class Bot {`,
		`    field handler?: Bot = null`,
		`    fn reply() {`,
		`        this.handler.rep()`,
		`    }`,
		`}`,
	}, "\n")

	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      3,
		Character: len("        this.handler.rep"),
	})

	var replyItem *CompletionItem
	for _, item := range completions {
		if item.Label == "reply" {
			replyItem = &item
			break
		}
	}
	if replyItem == nil {
		t.Fatalf("expected reply completion item")
	}

	if replyItem.InsertText != "reply" {
		t.Fatalf("expected insert text to be 'reply' to avoid duplicate parens, got %q", replyItem.InsertText)
	}
}

func TestLSPConstructorObjectLiteralCompletions(t *testing.T) {
	text := strings.Join([]string{
		`export interface BotConfig {`,
		`    token: string,`,
		`    timeout: number`,
		`}`,
		``,
		`class Bot {`,
		`    fn init(config: BotConfig) {`,
		`    }`,
		`}`,
		``,
		`const b = Bot({`,
		`    `,
		`})`,
	}, "\n")

	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      11,
		Character: 4,
	})

	if !completionLabelsContain(completions, "token: ") {
		t.Fatalf("expected completions to include 'token: ', got %#v", completionLabels(completions))
	}
	if !completionLabelsContain(completions, "timeout: ") {
		t.Fatalf("expected completions to include 'timeout: ', got %#v", completionLabels(completions))
	}
}

func TestLSPGenericReturnTypeInference(t *testing.T) {
	text := strings.Join([]string{
		`fn identity:T(x: T) {`,
		`    return x;`,
		`}`,
	}, "\n")

	scope := fileBaseScope("file:///test.tiny", text)
	sym, ok := scope.Resolve("identity")
	if !ok {
		t.Fatalf("expected identity to be resolved")
	}

	if sym.Returns != "T" {
		t.Fatalf("expected return type T, got %q", sym.Returns)
	}
}

func TestLSPClassMethodAndFieldHover(t *testing.T) {
	text := strings.Join([]string{
		`class User {`,
		`    field name: string = "demo"`,
		`    fn verify(code: number): bool {`,
		`        return true;`,
		`    }`,
		`}`,
	}, "\n")

	line1 := `    field name: string = "demo"`
	hoverResult := getHover("file:///test.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line1, "name") + 1,
	})
	hover, ok := hoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for field, got %#v", hoverResult)
	}
	if !strings.Contains(hover.Contents.Value, "User.name") || !strings.Contains(hover.Contents.Value, "string") {
		t.Fatalf("unexpected hover content for field: %q", hover.Contents.Value)
	}

	line2 := `    fn verify(code: number): bool {`
	hoverResult2 := getHover("file:///test.tiny", text, Position{
		Line:      2,
		Character: strings.Index(line2, "verify") + 1,
	})
	hover2, ok := hoverResult2.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for method, got %#v", hoverResult2)
	}
	if !strings.Contains(hover2.Contents.Value, "User.verify(code: number)") {
		t.Fatalf("unexpected hover content for method: %q", hover2.Contents.Value)
	}
}

func TestLSPBacktickStringInterpolation(t *testing.T) {
	text := strings.Join([]string{
		`const message = "hi";`,
		`const info = ` + "`" + `hello ${message} world` + "`" + `;`,
	}, "\n")

	line1 := `const info = ` + "`" + `hello ${message} world` + "`" + `;`

	// 1. Verify hover inside backtick string's ${}
	hoverResult := getHover("file:///test.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line1, "message") + 2,
	})
	hover, ok := hoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result inside backtick interpolation, got %#v", hoverResult)
	}
	if !strings.Contains(hover.Contents.Value, "message") {
		t.Fatalf("unexpected hover content: %q", hover.Contents.Value)
	}

	// 2. Verify hover OUTSIDE ${} but inside backticks (should return nil/no hover)
	hoverResultOutside := getHover("file:///test.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line1, "hello") + 2,
	})
	if hoverResultOutside != nil {
		t.Fatalf("expected no hover outside interpolation inside backticks, got %#v", hoverResultOutside)
	}

	// 3. Verify autocomplete inside `${}` of a backtick string
	completions := getCompletions("file:///test.tiny", text, Position{
		Line:      1,
		Character: strings.Index(line1, "message"),
	})
	if !completionLabelsContain(completions, "message") {
		t.Fatalf("expected completions inside backtick interpolation to include 'message', got %#v", completionLabels(completions))
	}
}

func TestLSPNamespaceAutocompleteAutoPrefix(t *testing.T) {
	dir := t.TempDir()
	utilsPath := filepath.Join(dir, "utils.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	utilsURI := pathToFileURI(utilsPath)
	mainURI := pathToFileURI(mainPath)

	utilsText := strings.Join([]string{
		"export class User {",
		"    field name: string",
		"}",
	}, "\n")
	mainText := strings.Join([]string{
		`import "utils.tiny" as Utils;`,
		`const u = Use`,
	}, "\n")

	if err := os.WriteFile(utilsPath, []byte(utilsText), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(mainText), 0644); err != nil {
		t.Fatal(err)
	}

	lspDocs[utilsURI] = utilsText
	lspDocs[mainURI] = mainText
	defer delete(lspDocs, utilsURI)
	defer delete(lspDocs, mainURI)

	completions := getCompletions(mainURI, mainText, Position{
		Line:      1,
		Character: 13,
	})

	item, ok := completionItemByLabel(completions, "User")
	if !ok {
		t.Fatalf("expected completion label 'User', got %#v", completionLabels(completions))
	}
	if !strings.HasPrefix(item.InsertText, "Utils.User") {
		t.Fatalf("expected insert text to be prefixed with 'Utils.User', got %q", item.InsertText)
	}
}

func TestLSPFindImplementations(t *testing.T) {
	dir := t.TempDir()
	ifacePath := filepath.Join(dir, "iface.tiny")
	classPath := filepath.Join(dir, "class.tiny")
	ifaceURI := pathToFileURI(ifacePath)
	classURI := pathToFileURI(classPath)

	ifaceText := strings.Join([]string{
		"export interface Greeter {",
		"    greet: function",
		"}",
	}, "\n")

	classText := strings.Join([]string{
		`import "iface.tiny" as Iface;`,
		`class SimpleGreeter {`,
		`    fn greet(): string {`,
		`        return "hello";`,
		`    }`,
		`}`,
	}, "\n")

	if err := os.WriteFile(ifacePath, []byte(ifaceText), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(classPath, []byte(classText), 0644); err != nil {
		t.Fatal(err)
	}

	lspDocs[ifaceURI] = ifaceText
	lspDocs[classURI] = classText
	defer delete(lspDocs, ifaceURI)
	defer delete(lspDocs, classURI)

	// 1. Implementation of Greeter interface at line 0, char 18
	locs := getImplementations(ifaceURI, ifaceText, Position{Line: 0, Character: 18})
	if len(locs) != 1 {
		t.Fatalf("expected 1 implementation of Greeter, got %d locations: %#v", len(locs), locs)
	}
	if !strings.Contains(locs[0].URI, "class.tiny") {
		t.Fatalf("expected implementation to be in class.tiny, got %#v", locs[0])
	}

	// 2. Implementation of Greeter.greet method at line 1, char 9
	locsMethod := getImplementations(ifaceURI, ifaceText, Position{Line: 1, Character: 9})
	if len(locsMethod) != 1 {
		t.Fatalf("expected 1 implementation of greet method, got %d locations: %#v", len(locsMethod), locsMethod)
	}
	if locsMethod[0].Range.Start.Line != 2 {
		t.Fatalf("expected greet method implementation on line 2, got line %d", locsMethod[0].Range.Start.Line)
	}
}

func TestLSPCallHierarchy(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	mainURI := pathToFileURI(mainPath)

	mainText := strings.Join([]string{
		"fn helper() {",
		"}",
		"fn worker() {",
		"    helper();",
		"}",
		"fn main() {",
		"    worker();",
		"}",
	}, "\n")

	if err := os.WriteFile(mainPath, []byte(mainText), 0644); err != nil {
		t.Fatal(err)
	}

	lspDocs[mainURI] = mainText
	defer delete(lspDocs, mainURI)

	// 1. Prepare Call Hierarchy for `worker` at line 2, char 3
	items := prepareCallHierarchy(mainURI, mainText, Position{Line: 2, Character: 3})
	if len(items) != 1 {
		t.Fatalf("expected 1 call hierarchy item for worker, got %#v", items)
	}
	workerItem := items[0]
	if workerItem.Name != "worker" {
		t.Fatalf("expected item name 'worker', got %q", workerItem.Name)
	}

	// 2. Get incoming calls for `worker`
	incoming := getIncomingCalls(workerItem)
	if len(incoming) != 1 {
		t.Fatalf("expected 1 incoming call to worker, got %#v", incoming)
	}
	if incoming[0].From.Name != "main" {
		t.Fatalf("expected incoming call from 'main', got %q", incoming[0].From.Name)
	}

	// 3. Get outgoing calls for `worker`
	outgoing := getOutgoingCalls(workerItem)
	if len(outgoing) != 1 {
		t.Fatalf("expected 1 outgoing call from worker, got %#v", outgoing)
	}
	if outgoing[0].To.Name != "helper" {
		t.Fatalf("expected outgoing call to 'helper', got %q", outgoing[0].To.Name)
	}
}

func TestLSPFullProjectDiagnostics(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, "main.tiny")
	mainURI := pathToFileURI(mainPath)

	// Write a tiny.json so scanProjectTinyFiles detects the root correctly
	if err := os.WriteFile(filepath.Join(dir, "tiny.json"), []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	mainText := strings.Join([]string{
		"fn test(x: string) {",
		"}",
		"test(123);",
	}, "\n")

	if err := os.WriteFile(mainPath, []byte(mainText), 0644); err != nil {
		t.Fatal(err)
	}

	lspDocs[mainURI] = mainText
	lspDocs[mainPath] = mainText
	defer delete(lspDocs, mainURI)
	defer delete(lspDocs, mainPath)

	// Redirect stdout to capture the publish diagnostics message
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan bool)
	go func() {
		io.Copy(&buf, r)
		close(done)
	}()

	publishProjectDiagnostics()

	w.Close()
	<-done
	os.Stdout = oldStdout

	output := buf.String()

	if !strings.Contains(output, "textDocument/publishDiagnostics") {
		t.Fatalf("expected project diagnostics publish message, got %q", output)
	}
	if !strings.Contains(output, "cannot pass type 'number' to parameter 'x' of function 'test' (expected 'string')") {
		t.Fatalf("expected parameter type mismatch error in diagnostic output, got %q", output)
	}
}

func TestLSPCrossFileCallHierarchy(t *testing.T) {
	dir := t.TempDir()
	utilsPath := filepath.Join(dir, "utils.tiny")
	mainPath := filepath.Join(dir, "main.tiny")
	utilsURI := pathToFileURI(utilsPath)
	mainURI := pathToFileURI(mainPath)

	utilsText := strings.Join([]string{
		"export fn helper() {",
		"}",
	}, "\n")

	mainText := strings.Join([]string{
		`import "utils.tiny" as Utils;`,
		`fn run() {`,
		`    Utils.helper();`,
		`}`,
	}, "\n")

	if err := os.WriteFile(utilsPath, []byte(utilsText), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte(mainText), 0644); err != nil {
		t.Fatal(err)
	}

	lspDocs[utilsURI] = utilsText
	lspDocs[mainURI] = mainText
	defer delete(lspDocs, utilsURI)
	defer delete(lspDocs, mainURI)

	// 1. Prepare Call Hierarchy for `helper` at the call site `Utils.helper()` in main.tiny (Line 2, Character: 13)
	items := prepareCallHierarchy(mainURI, mainText, Position{Line: 2, Character: 13})
	if len(items) != 1 {
		t.Fatalf("expected 1 call hierarchy item for helper, got %#v", items)
	}
	helperItem := items[0]
	if helperItem.Name != "helper" {
		t.Fatalf("expected item name 'helper', got %q", helperItem.Name)
	}
	if !strings.Contains(helperItem.URI, "utils.tiny") {
		t.Fatalf("expected helper URI to point to utils.tiny, got %q", helperItem.URI)
	}

	// 2. Get incoming calls for `helper`
	incoming := getIncomingCalls(helperItem)
	if len(incoming) != 1 {
		t.Fatalf("expected 1 incoming call to helper, got %#v", incoming)
	}
	if incoming[0].From.Name != "run" {
		t.Fatalf("expected incoming call from 'run', got %q", incoming[0].From.Name)
	}
	if !strings.Contains(incoming[0].From.URI, "main.tiny") {
		t.Fatalf("expected incoming call source to be main.tiny, got %q", incoming[0].From.URI)
	}
}

func TestLSPDefaultParameterFixes(t *testing.T) {
	// 1. Test parameter type inference inside function body
	textTypeInference := strings.Join([]string{
		`fn loadEnv(path = ".env") {`,
		`    const p = path;`,
		`}`,
	}, "\n")
	scope := scopeAtPosition("file:///test1.tiny", textTypeInference, Position{
		Line:      1,
		Character: len("    const p = path;"),
	})
	sym, ok := scope.Resolve("p")
	if !ok {
		t.Fatalf("expected p in scope")
	}
	if sym.Type != "string" {
		t.Fatalf("expected p to have type string, got %q", sym.Type)
	}

	// 2. Test calling function with no args (argument count check passes)
	textDiagnostics := strings.Join([]string{
		`fn loadEnv(path = ".env") {`,
		`}`,
		`loadEnv();`,
	}, "\n")
	diagnostics := semanticDiagnostics("file:///test2.tiny", textDiagnostics)
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics for loadEnv() call, got %#v", diagnostics)
	}

	// 3. Test interface autocomplete for process.start() options
	textAutocomplete := strings.Join([]string{
		`import std "process";`,
		`process.start("cmd", [], {`,
		`    `,
		`})`,
	}, "\n")
	completions := getCompletions("file:///test3.tiny", textAutocomplete, Position{
		Line:      2,
		Character: 4,
	})
	if !completionLabelsContain(completions, "cwd: ") {
		t.Fatalf("expected completions to include cwd: , got %#v", completionLabels(completions))
	}

	// 4. Test interface autocomplete for process.shell() options
	textAutocompleteShell := strings.Join([]string{
		`import std "process";`,
		`process.shell("cmd", {`,
		`    `,
		`})`,
	}, "\n")
	completionsShell := getCompletions("file:///test4.tiny", textAutocompleteShell, Position{
		Line:      2,
		Character: 4,
	})
	if !completionLabelsContain(completionsShell, "cwd: ") {
		t.Fatalf("expected completions to include cwd: , got %#v", completionLabels(completionsShell))
	}

	// 5. Test interface autocomplete for process.start() multiline
	textAutocompleteMultiline := strings.Join([]string{
		`import std "process";`,
		`process.start(`,
		`    "cmd",`,
		`    [],`,
		`    {`,
		`        `,
		`    }`,
		`)`,
	}, "\n")
	completionsMultiline := getCompletions("file:///test5.tiny", textAutocompleteMultiline, Position{
		Line:      5,
		Character: 8,
	})
	if !completionLabelsContain(completionsMultiline, "cwd: ") {
		t.Fatalf("expected completions to include cwd: , got %#v", completionLabels(completionsMultiline))
	}

	// 6. Test interface autocomplete for process.start() multiline unclosed
	textAutocompleteMultilineUnclosed := strings.Join([]string{
		`import std "process";`,
		`process.start(`,
		`    "cmd",`,
		`    [],`,
		`    {`,
		`        `,
	}, "\n")
	completionsMultilineUnclosed := getCompletions("file:///test6.tiny", textAutocompleteMultilineUnclosed, Position{
		Line:      5,
		Character: 8,
	})
	if !completionLabelsContain(completionsMultilineUnclosed, "cwd: ") {
		t.Fatalf("expected completions to include cwd: , got %#v", completionsMultilineUnclosed)
	}

	// 7. Test interface autocomplete for process.start() multiline unclosed CRLF
	textAutocompleteMultilineCRLF := strings.Join([]string{
		`import std "process";` + "\r",
		`process.start(` + "\r",
		`    "cmd",` + "\r",
		`    [],` + "\r",
		`    {` + "\r",
		`        `,
	}, "\n")
	completionsMultilineCRLF := getCompletions("file:///test7.tiny", textAutocompleteMultilineCRLF, Position{
		Line:      5,
		Character: 8,
	})
	if !completionLabelsContain(completionsMultilineCRLF, "cwd: ") {
		t.Fatalf("expected completions to include cwd: , got %#v", completionsMultilineCRLF)
	}
}

func TestLSPAutocompleteSuppressionAndQuotedKeys(t *testing.T) {
	// 1. Inside normal string: verify no autocomplete items
	textString := `const s = "test";`
	completionsString := getCompletions("file:///test_string.tiny", textString, Position{
		Line:      0,
		Character: len("const s = \"test"),
	})
	if len(completionsString) > 0 {
		t.Fatalf("expected no completions inside normal string, got %#v", completionLabels(completionsString))
	}

	textEmptyString := `const s = "";`
	completionsEmptyString := getCompletions("file:///test_empty_string.tiny", textEmptyString, Position{
		Line:      0,
		Character: len("const s = \""),
	})
	if len(completionsEmptyString) > 0 {
		t.Fatalf("expected no completions inside empty string, got %#v", completionLabels(completionsEmptyString))
	}

	// 2. Inside backtick interpolation: verify autocomplete items are present
	textBacktick := strings.Join([]string{
		`const name = "Tiny";`,
		`const s = ` + "`" + `hello ${na}` + "`" + `;`,
	}, "\n")
	completionsBacktick := getCompletions("file:///test_backtick.tiny", textBacktick, Position{
		Line:      1,
		Character: len("const s = `hello ${na"),
	})
	if !completionLabelsContain(completionsBacktick, "name") {
		t.Fatalf("expected completions inside backtick interpolation to contain 'name', got %#v", completionLabels(completionsBacktick))
	}

	// 3. Inside non-interface object: verify no autocomplete items
	textNonInterfaceObj := strings.Join([]string{
		`const x = {`,
		`    `,
		`};`,
	}, "\n")
	completionsNonInterfaceObj := getCompletions("file:///test_non_iface.tiny", textNonInterfaceObj, Position{
		Line:      1,
		Character: 4,
	})
	if len(completionsNonInterfaceObj) > 0 {
		t.Fatalf("expected no completions inside non-interface object literal, got %#v", completionLabels(completionsNonInterfaceObj))
	}

	// 4. Inside interface object with quotes: typing "" suggests "cwd": with TextEdit
	textQuotedQuotes := strings.Join([]string{
		`import std "process";`,
		`process.start("cmd", [], {`,
		`    ""`,
		`})`,
	}, "\n")
	completionsQuotes := getCompletions("file:///test_quotes.tiny", textQuotedQuotes, Position{
		Line:      2,
		Character: len("    \""),
	})
	if !completionLabelsContain(completionsQuotes, "\"cwd\": ") {
		t.Fatalf("expected completions to contain '\"cwd\": ', got %#v", completionLabels(completionsQuotes))
	}
	var cwdItem *CompletionItem
	for _, item := range completionsQuotes {
		if item.Label == "\"cwd\": " {
			cwdItem = &item
			break
		}
	}
	if cwdItem == nil || cwdItem.TextEdit == nil {
		t.Fatalf("expected cwd completion item to have TextEdit, got %#v", cwdItem)
	}
	if cwdItem.TextEdit.Range.Start.Character != 4 || cwdItem.TextEdit.Range.End.Character != 6 {
		t.Fatalf("unexpected TextEdit range: start=%d, end=%d", cwdItem.TextEdit.Range.Start.Character, cwdItem.TextEdit.Range.End.Character)
	}
	if cwdItem.TextEdit.NewText != "\"cwd\": $0" {
		t.Fatalf("unexpected TextEdit newText: %q", cwdItem.TextEdit.NewText)
	}

	// 5. Inside interface object with quoted prefix: typing "c suggests "cwd":
	textQuotedC := strings.Join([]string{
		`import std "process";`,
		`process.start("cmd", [], {`,
		`    "c"`,
		`})`,
	}, "\n")
	completionsQuotedC := getCompletions("file:///test_quoted_c.tiny", textQuotedC, Position{
		Line:      2,
		Character: len("    \"c"),
	})
	if !completionLabelsContain(completionsQuotedC, "\"cwd\": ") {
		t.Fatalf("expected completions to contain '\"cwd\": ', got %#v", completionLabels(completionsQuotedC))
	}

	// 6. Multiline nested object literal autocomplete
	textNested := strings.Join([]string{
		`import std "process";`,
		`process.start("cmd", [], {`,
		`    env: {`,
		`        `,
		`    }`,
		`})`,
	}, "\n")
	completionsNested := getCompletions("file:///test_nested.tiny", textNested, Position{
		Line:      3,
		Character: 8,
	})
	if completionLabelsContain(completionsNested, "cwd: ") {
		t.Fatalf("expected nested env object not to suggest outer ProcessOptions fields, got %#v", completionLabels(completionsNested))
	}
}

func TestLSPHoverDocumentationComments(t *testing.T) {
	text := strings.Join([]string{
		`// This is my test function.`,
		`// It does wonderful things.`,
		`fn myFunc() {}`,
		``,
		`// This is my class description.`,
		`class MyClass {`,
		`    // This is method doc.`,
		`    fn myMethod() {}`,
		`}`,
		``,
		`// This is interface doc.`,
		`interface MyInterface {}`,
		``,
		`// This is enum doc.`,
		`enum MyEnum {`,
		`    Val`,
		`}`,
		``,
		`fn test() {`,
		`    myFunc();`,
		`    const c = MyClass();`,
		`    c.myMethod();`,
		`}`,
	}, "\n")

	// 1. Verify function hover doc
	resFunc := getHover("file:///test_doc.tiny", text, Position{Line: 19, Character: 4})
	hoverFunc, ok := resFunc.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", resFunc)
	}
	if !strings.Contains(hoverFunc.Contents.Value, "This is my test function.  \nIt does wonderful things.") {
		t.Fatalf("unexpected hover content for function: %q", hoverFunc.Contents.Value)
	}

	// 2. Verify class hover doc
	resClass := getHover("file:///test_doc.tiny", text, Position{Line: 20, Character: 14})
	hoverClass, ok := resClass.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", resClass)
	}
	if !strings.Contains(hoverClass.Contents.Value, "This is my class description.") {
		t.Fatalf("unexpected hover content for class: %q", hoverClass.Contents.Value)
	}

	// 3. Verify class method hover doc
	resMethod := getHover("file:///test_doc.tiny", text, Position{Line: 21, Character: 6})
	hoverMethod, ok := resMethod.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", resMethod)
	}
	if !strings.Contains(hoverMethod.Contents.Value, "This is method doc.") {
		t.Fatalf("unexpected hover content for method: %q", hoverMethod.Contents.Value)
	}

	// 4. Verify interface hover doc
	resInterface := getHover("file:///test_doc.tiny", text, Position{Line: 11, Character: 10})
	hoverInterface, ok := resInterface.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", resInterface)
	}
	if !strings.Contains(hoverInterface.Contents.Value, "This is interface doc.") {
		t.Fatalf("unexpected hover content for interface: %q", hoverInterface.Contents.Value)
	}

	// 5. Verify enum hover doc
	resEnum := getHover("file:///test_doc.tiny", text, Position{Line: 14, Character: 5})
	hoverEnum, ok := resEnum.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", resEnum)
	}
	if !strings.Contains(hoverEnum.Contents.Value, "This is enum doc.") {
		t.Fatalf("unexpected hover content for enum: %q", hoverEnum.Contents.Value)
	}
}

func TestLSPLoopVariables(t *testing.T) {
	text := strings.Join([]string{
		`fn test() {`,
		`    for let idx = 0; idx < 10; idx++ {`,
		`        const current = idx;`,
		`    }`,
		`    // outside standard loop`,
		`    for (let idxHint: number = 0; idxHint < 5; idxHint++) {`,
		`        const currentHint = idxHint;`,
		`    }`,
		`    // outside standard loop with hint`,
		`    for item in "hello" {`,
		`        const val = item;`,
		`    }`,
		`    // outside value string loop`,
		`    for x, i in [1, 2, 3] {`,
		`        const valx = x;`,
		`        const indexi = i;`,
		`    }`,
		`    // outside index-value loop`,
		`}`,
	}, "\n")

	uri := "file:///test_loop_vars.tiny"

	// 1. Inside standard for loop let idx = 0
	scopeIdx := scopeAtPosition(uri, text, Position{Line: 2, Character: 8}) // line 3 (index 2)
	if sym, ok := scopeIdx.Resolve("idx"); !ok {
		t.Fatalf("expected idx to be in scope inside standard loop")
	} else if sym.Type != "number" {
		t.Fatalf("expected idx to have type number, got %q", sym.Type)
	}

	// 2. Outside standard loop let idx = 0
	scopeIdxOut := scopeAtPosition(uri, text, Position{Line: 4, Character: 4}) // line 5 (index 4)
	if _, ok := scopeIdxOut.Resolve("idx"); ok {
		t.Fatalf("expected idx to NOT be in scope outside standard loop")
	}

	// 3. Inside standard loop with type hint let idxHint: number
	scopeIdxHint := scopeAtPosition(uri, text, Position{Line: 6, Character: 8}) // line 7 (index 6)
	if sym, ok := scopeIdxHint.Resolve("idxHint"); !ok {
		t.Fatalf("expected idxHint to be in scope inside loop with hint")
	} else if sym.Type != "number" {
		t.Fatalf("expected idxHint to have type number, got %q", sym.Type)
	}

	// 4. Outside standard loop with type hint
	scopeIdxHintOut := scopeAtPosition(uri, text, Position{Line: 8, Character: 4}) // line 9 (index 8)
	if _, ok := scopeIdxHintOut.Resolve("idxHint"); ok {
		t.Fatalf("expected idxHint to NOT be in scope outside loop")
	}

	// 5. Inside string for-in loop (item should be string)
	scopeItem := scopeAtPosition(uri, text, Position{Line: 10, Character: 8}) // line 11 (index 10)
	if sym, ok := scopeItem.Resolve("item"); !ok {
		t.Fatalf("expected item to be in scope inside string loop")
	} else if sym.Type != "string" {
		t.Fatalf("expected item to have type string, got %q", sym.Type)
	}

	// 6. Outside string for-in loop
	scopeItemOut := scopeAtPosition(uri, text, Position{Line: 12, Character: 4}) // line 13 (index 12)
	if _, ok := scopeItemOut.Resolve("item"); ok {
		t.Fatalf("expected item to NOT be in scope outside string loop")
	}

	// 7. Inside index-value loop (x should be any, i should be number)
	scopeX := scopeAtPosition(uri, text, Position{Line: 14, Character: 8}) // line 15 (index 14)
	if sym, ok := scopeX.Resolve("x"); !ok {
		t.Fatalf("expected x to be in scope inside index-val loop")
	} else if sym.Type != "number" {
		t.Fatalf("expected x to have type number, got %q", sym.Type)
	}

	if sym, ok := scopeX.Resolve("i"); !ok {
		t.Fatalf("expected i to be in scope inside index-val loop")
	} else if sym.Type != "number" {
		t.Fatalf("expected i to have type number, got %q", sym.Type)
	}

	// 8. Outside index-value loop
	scopeXOut := scopeAtPosition(uri, text, Position{Line: 17, Character: 4}) // line 18 (index 17)
	if _, ok := scopeXOut.Resolve("x"); ok {
		t.Fatalf("expected x to NOT be in scope outside loop")
	}
	if _, ok := scopeXOut.Resolve("i"); ok {
		t.Fatalf("expected i to NOT be in scope outside loop")
	}
}

func TestLSPTypedArrayVariables(t *testing.T) {
	text := strings.Join([]string{
		`fn test() {`,
		`    let strings: array:string = ["a", "b"];`,
		`    for item in strings {`,
		`        const val = item;`,
		`    }`,
		`}`,
	}, "\n")

	uri := "file:///test_typed_array_vars.tiny"

	// Check scope inside the loop (line 4, index 3 in 0-indexed terms)
	scope := scopeAtPosition(uri, text, Position{Line: 3, Character: 8})
	if sym, ok := scope.Resolve("item"); !ok {
		t.Fatalf("expected item to be in scope")
	} else if sym.Type != "string" {
		t.Fatalf("expected item to have type string, got %q", sym.Type)
	}
}

func TestLSPAnonymousFunctionParameterScoping(t *testing.T) {
	text := strings.Join([]string{
		`const logger = fn(ctx: httpx.Context, next) {`,
		`    ctx.`,
		`}`,
		`const test = fn() {`,
		`    ctx.`,
		`}`,
	}, "\n")

	uri := "file:///test_param_leak.tiny"

	// 1. Inside logger: ctx should be resolved
	scopeInside := scopeAtPosition(uri, text, Position{Line: 1, Character: 4})
	if _, ok := scopeInside.Resolve("ctx"); !ok {
		t.Fatal("expected ctx to be in scope inside logger")
	}

	// 2. Inside test: ctx should NOT be resolved (since it has no ctx parameter)
	scopeOutside := scopeAtPosition(uri, text, Position{Line: 4, Character: 4})
	if _, ok := scopeOutside.Resolve("ctx"); ok {
		t.Fatal("expected ctx to NOT leak into test function body")
	}
}

func TestLSPTypedArrayExtensions(t *testing.T) {
	text := strings.Join([]string{
		`import std "array";`,
		`const arr = ["a", "b"];`,
		`const mixed = ["a", 1];`,
		`const first = arr[0];`,
		`const nested = [["a"]];`,
		`const nestedElem = nested[0][0];`,
		`const arr2 = array.from(arr);`,
		`const arr3 = array.from("hello");`,
		`const gotten = arr.get(0);`,
		`const popped = arr.pop();`,
		`const pushed = arr.push("c");`,
		`const reversed = arr.reverse();`,
		`class User {}`,
		`const users: array:User = [];`,
		`users.push(7);`,
		`users.push();`,
	}, "\n")

	uri := "file:///test_typed_array_ext.tiny"
	lspDocs[uri] = text
	defer delete(lspDocs, uri)

	// Trigger type inference by resolving the scope at the end
	scope := scopeAtPosition(uri, text, Position{Line: 11, Character: 0})

	// 1. Array literal inference
	if sym, ok := scope.Resolve("arr"); !ok {
		t.Fatalf("expected arr to be in scope")
	} else if sym.Type != "array:string" {
		t.Fatalf("expected arr type array:string, got %q", sym.Type)
	}

	if sym, ok := scope.Resolve("mixed"); !ok {
		t.Fatalf("expected mixed to be in scope")
	} else if sym.Type != "array:any" {
		t.Fatalf("expected mixed type array:any, got %q", sym.Type)
	}

	// 2. Index access type inference
	if sym, ok := scope.Resolve("first"); !ok {
		t.Fatalf("expected first to be in scope")
	} else if sym.Type != "string" {
		t.Fatalf("expected first type string, got %q", sym.Type)
	}

	if sym, ok := scope.Resolve("nestedElem"); !ok {
		t.Fatalf("expected nestedElem to be in scope")
	} else if sym.Type != "string" {
		t.Fatalf("expected nestedElem type string, got %q", sym.Type)
	}

	// 3. array.from return type propagation
	if sym, ok := scope.Resolve("arr2"); !ok {
		t.Fatalf("expected arr2 to be in scope")
	} else if sym.Type != "array:string" {
		t.Fatalf("expected arr2 type array:string, got %q", sym.Type)
	}

	if sym, ok := scope.Resolve("arr3"); !ok {
		t.Fatalf("expected arr3 to be in scope")
	} else if sym.Type != "array:string" {
		t.Fatalf("expected arr3 type array:string, got %q", sym.Type)
	}

	// 4. Native method get/pop
	if sym, ok := scope.Resolve("gotten"); !ok {
		t.Fatalf("expected gotten to be in scope")
	} else if sym.Type != "string" {
		t.Fatalf("expected gotten type string, got %q", sym.Type)
	}

	if sym, ok := scope.Resolve("popped"); !ok {
		t.Fatalf("expected popped to be in scope")
	} else if sym.Type != "string" {
		t.Fatalf("expected popped type string, got %q", sym.Type)
	}

	// 5. Native method push/reverse
	if sym, ok := scope.Resolve("pushed"); !ok {
		t.Fatalf("expected pushed to be in scope")
	} else if sym.Type != "array:string" {
		t.Fatalf("expected pushed type array:string, got %q", sym.Type)
	}

	if sym, ok := scope.Resolve("reversed"); !ok {
		t.Fatalf("expected reversed to be in scope")
	} else if sym.Type != "array:string" {
		t.Fatalf("expected reversed type array:string, got %q", sym.Type)
	}

	// 6. Index access receiver resolution for autocomplete
	_, resolvedType, ok := resolveReceiverPath(scope, text, Position{Line: 11, Character: 0}, "arr[0]")
	if !ok {
		t.Fatalf("expected resolveReceiverPath for arr[0] to succeed")
	} else if resolvedType != "string" {
		t.Fatalf("expected receiver type for arr[0] to be string, got %q", resolvedType)
	}

	// 7. Verify no "unknown type" diagnostics for typed arrays
	statements, _ := parseTinyForLSP(uri, text)
	for i, stmt := range statements {
		t.Logf("STMT %d: %T %+v", i, stmt, stmt)
	}

	// and verify argument type check + correct position for wrong arg count
	diagnostics := semanticDiagnostics(uri, text)
	hasTypeError := false
	hasArgCountErrorAtCorrectLine := false

	for _, diag := range diagnostics {
		message, _ := diag["message"].(string)
		t.Logf("DIAGNOSTIC: %q", message)
		if strings.Contains(message, "unknown type:") {
			t.Fatalf("unexpected diagnostic: %q", message)
		}
		if strings.Contains(message, "cannot pass type 'number' to parameter 'value' of function 'array:class:User.push'") {
			hasTypeError = true
		}
		if strings.Contains(message, "wrong argument count for array:class:User.push: expected 1, got 0") {
			// Check line (0-indexed line range in LSP represents the actual line)
			rng, _ := diag["range"].(map[string]any)
			start, _ := rng["start"].(map[string]any)
			var lineVal int
			if val, ok := start["line"].(int); ok {
				lineVal = val
			} else if val, ok := start["line"].(float64); ok {
				lineVal = int(val)
			}
			t.Logf("Found wrong argument count error at line: %v", lineVal)
			// users.push() is on line 15 (0-indexed)
			if lineVal == 15 {
				hasArgCountErrorAtCorrectLine = true
			}
		}
	}

	if !hasTypeError {
		t.Fatalf("expected type checking error for users.push(7), but none was found")
	}
	if !hasArgCountErrorAtCorrectLine {
		t.Fatalf("expected wrong argument count error for users.push() at line 15, but none was found")
	}
}

func TestLSPSemanticDiagnosticKeepsCallSiteRange(t *testing.T) {
	text := strings.Join([]string{
		`fn fib(n: number): number {`,
		`    if n <= 1 {`,
		`        return n`,
		`    }`,
		`    return fib(n - 1) + fib(n - 2)`,
		`}`,
		`fn run() {`,
		`    fib(20, 1)`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_call_range.tiny", text)
	for _, diagnostic := range diagnostics {
		message, _ := diagnostic["message"].(string)
		if !strings.Contains(message, "wrong argument count for fib: expected 1, got 2") {
			continue
		}

		rangeValue, _ := diagnostic["range"].(map[string]any)
		start, _ := rangeValue["start"].(map[string]any)
		end, _ := rangeValue["end"].(map[string]any)
		if intFromAny(start["line"]) != 7 ||
			intFromAny(start["character"]) != 4 ||
			intFromAny(end["line"]) != 7 ||
			intFromAny(end["character"]) != 7 {
			t.Fatalf("expected call-site range for bad fib call, got %#v", diagnostic)
		}
		return
	}

	t.Fatalf("expected wrong argument count diagnostic, got %#v", diagnostics)
}

func TestLSPSemanticDiagnosticHighlightsBadArgument(t *testing.T) {
	text := strings.Join([]string{
		`fn takes(name: string) {}`,
		`fn run() {`,
		`    takes(42)`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_bad_arg_range.tiny", text)
	for _, diagnostic := range diagnostics {
		message, _ := diagnostic["message"].(string)
		if !strings.Contains(message, "cannot pass type 'number' to parameter 'name'") {
			continue
		}
		rangeValue, _ := diagnostic["range"].(map[string]any)
		start, _ := rangeValue["start"].(map[string]any)
		end, _ := rangeValue["end"].(map[string]any)
		if intFromAny(start["line"]) != 2 ||
			intFromAny(start["character"]) != len(`    takes(`) ||
			intFromAny(end["character"]) != len(`    takes(42`) {
			t.Fatalf("expected bad argument range, got %#v", diagnostic)
		}
		return
	}
	t.Fatalf("expected bad argument diagnostic, got %#v", diagnostics)
}

func TestLSPSemanticDiagnosticHighlightsReturnedExpression(t *testing.T) {
	text := strings.Join([]string{
		`fn value(): string {`,
		`    return 42`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_return_range.tiny", text)
	for _, diagnostic := range diagnostics {
		message, _ := diagnostic["message"].(string)
		if !strings.Contains(message, "cannot return type 'number'") {
			continue
		}
		rangeValue, _ := diagnostic["range"].(map[string]any)
		start, _ := rangeValue["start"].(map[string]any)
		end, _ := rangeValue["end"].(map[string]any)
		if intFromAny(start["line"]) != 1 ||
			intFromAny(start["character"]) != len(`    return `) ||
			intFromAny(end["character"]) != len(`    return 42`) {
			t.Fatalf("expected returned expression range, got %#v", diagnostic)
		}
		return
	}
	t.Fatalf("expected return type diagnostic, got %#v", diagnostics)
}

func TestLSPSemanticDiagnosticsRecoverAfterParseError(t *testing.T) {
	text := strings.Join([]string{
		`let =`,
		`fn ok() {`,
		`    missing`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_recovery.tiny", text)
	if !diagnosticsContain(diagnostics, "undefined variable: missing") {
		t.Fatalf("expected recovered semantic diagnostic after parse error, got %#v", diagnostics)
	}
}

func TestLSPUnionInterfaceObjectLiteralCompletions(t *testing.T) {
	// 1. Test union with a primitive and an interface: server(opt: number | ServerOptions)
	text1 := strings.Join([]string{
		`export interface ServerOptions {`,
		`    port: number,`,
		`    host: string`,
		`}`,
		``,
		`export fn server(opt: number | ServerOptions) {}`,
		``,
		`fn test() {`,
		`    server({`,
		`        `,
		`    })`,
		`}`,
	}, "\n")

	completions1 := getCompletions("file:///test1.tiny", text1, Position{
		Line:      9,
		Character: 8,
	})

	if !completionLabelsContain(completions1, "port: ") {
		t.Fatalf("expected completions1 to include 'port: ', got %#v", completionLabels(completions1))
	}
	if !completionLabelsContain(completions1, "host: ") {
		t.Fatalf("expected completions1 to include 'host: ', got %#v", completionLabels(completions1))
	}

	// 2. Test union with multiple interfaces: ServerOptions | ClientOptions
	text2 := strings.Join([]string{
		`export interface ServerOptions {`,
		`    port: number,`,
		`    host: string`,
		`}`,
		`export interface ClientOptions {`,
		`    url: string,`,
		`    timeout: number`,
		`}`,
		``,
		`export fn request(opt: ServerOptions | ClientOptions) {}`,
		``,
		`fn test() {`,
		`    request({`,
		`        `,
		`    })`,
		`}`,
	}, "\n")

	completions2 := getCompletions("file:///test2.tiny", text2, Position{
		Line:      13,
		Character: 8,
	})

	// Should merge fields from both ServerOptions and ClientOptions
	if !completionLabelsContain(completions2, "port: ") {
		t.Fatalf("expected completions2 to include 'port: ', got %#v", completionLabels(completions2))
	}
	if !completionLabelsContain(completions2, "url: ") {
		t.Fatalf("expected completions2 to include 'url: ', got %#v", completionLabels(completions2))
	}

	// 3. Test narrowing when typing a field unique to one interface
	text3 := strings.Join([]string{
		`export interface ServerOptions {`,
		`    port: number,`,
		`    host: string`,
		`}`,
		`export interface ClientOptions {`,
		`    url: string,`,
		`    timeout: number`,
		`}`,
		``,
		`export fn request(opt: ServerOptions | ClientOptions) {}`,
		``,
		`fn test() {`,
		`    request({`,
		`        url: "http://localhost",`,
		`        `,
		`    })`,
		`}`,
	}, "\n")

	completions3 := getCompletions("file:///test3.tiny", text3, Position{
		Line:      14,
		Character: 8,
	})

	// Should narrow to ClientOptions, so only 'timeout' (and not 'port') should be suggested.
	if !completionLabelsContain(completions3, "timeout: ") {
		t.Fatalf("expected completions3 to include 'timeout: ', got %#v", completionLabels(completions3))
	}
	if completionLabelsContain(completions3, "port: ") {
		t.Fatalf("expected completions3 to NOT include 'port: ' due to narrowing, got %#v", completionLabels(completions3))
	}

	// 4. Test narrowing to ServerOptions when typing port
	text4 := strings.Join([]string{
		`export interface ServerOptions {`,
		`    port: number,`,
		`    host: string`,
		`}`,
		`export interface ClientOptions {`,
		`    url: string,`,
		`    timeout: number`,
		`}`,
		``,
		`export fn request(opt: ServerOptions | ClientOptions) {}`,
		``,
		`fn test() {`,
		`    request({`,
		`        port: 80,`,
		`        `,
		`    })`,
		`}`,
	}, "\n")

	completions4 := getCompletions("file:///test4.tiny", text4, Position{
		Line:      14,
		Character: 8,
	})

	// Should narrow to ServerOptions, so only 'host' (and not 'url') should be suggested.
	if !completionLabelsContain(completions4, "host: ") {
		t.Fatalf("expected completions4 to include 'host: ', got %#v", completionLabels(completions4))
	}
	if completionLabelsContain(completions4, "url: ") {
		t.Fatalf("expected completions4 to NOT include 'url: ' due to narrowing, got %#v", completionLabels(completions4))
	}
}

func TestLSPChainingMethodDotCompletion(t *testing.T) {
	text := strings.Join([]string{
		`const path = "a.b.c";`,
		`path.split(".").`,
	}, "\n")

	completions := getCompletions("file:///chaining.tiny", text, Position{
		Line:      1,
		Character: len(`path.split(".").`),
	})

	// Since path.split() returns array:string, it should autocomplete array methods like 'length' or 'push'
	if !completionLabelsContain(completions, "length") {
		t.Fatalf("expected completions to include array method 'length', got %#v", completionLabels(completions))
	}
	if !completionLabelsContain(completions, "push") {
		t.Fatalf("expected completions to include array method 'push', got %#v", completionLabels(completions))
	}
}

func TestLSPGenericInterfaceObjectCompletions(t *testing.T) {
	text := strings.Join([]string{
		`interface Test:T {`,
		`    user: T`,
		`}`,
		``,
		`interface User {`,
		`    name: string,`,
		`    age: number`,
		`}`,
		``,
		`const testing: Test:User = {`,
		`    user: {`,
		`        `,
		`    }`,
		`}`,
		``,
		`testing.`,
		`testing.user.`,
		`const ageValue = testing.user.age`,
	}, "\n")

	testingItems := getCompletions("file:///generic_interface_object.tiny", text, Position{
		Line:      15,
		Character: len("testing."),
	})
	if !completionLabelsContain(testingItems, "user") {
		t.Fatalf("expected testing. completions to include user, got %#v", completionLabels(testingItems))
	}

	userItems := getCompletions("file:///generic_interface_object.tiny", text, Position{
		Line:      16,
		Character: len("testing.user."),
	})
	if !completionLabelsContain(userItems, "name") {
		t.Fatalf("expected testing.user. completions to include name, got %#v", completionLabels(userItems))
	}
	if !completionLabelsContain(userItems, "age") {
		t.Fatalf("expected testing.user. completions to include age, got %#v", completionLabels(userItems))
	}

	nestedItems := getCompletions("file:///generic_interface_object.tiny", text, Position{
		Line:      11,
		Character: len(`        `),
	})
	if !completionLabelsContain(nestedItems, "name: ") {
		t.Fatalf("expected user object completions to include name, got %#v", completionLabels(nestedItems))
	}
	if !completionLabelsContain(nestedItems, "age: ") {
		t.Fatalf("expected user object completions to include age, got %#v", completionLabels(nestedItems))
	}

	diagnostics := semanticDiagnostics("file:///generic_interface_object.tiny", text)
	if diagnosticsContain(diagnostics, "undefined method or property: age") {
		t.Fatalf("expected testing.user.age to pass diagnostics, got %#v", diagnostics)
	}

	diagnosticText := strings.Join([]string{
		`import std "io"`,
		``,
		`interface Test:T {`,
		`    user:T`,
		`}`,
		``,
		`interface User {`,
		`    name: string,`,
		`    age: string`,
		`}`,
		``,
		`const testing: Test:User = {`,
		`    user: {`,
		`        name: "",`,
		`        age: ""`,
		`    }`,
		`}`,
		``,
		`io.println(testing.user.age)`,
	}, "\n")
	callDiagnostics := semanticDiagnostics("file:///generic_interface_object_call.tiny", diagnosticText)
	if diagnosticsContain(callDiagnostics, "undefined method or property: age") {
		t.Fatalf("expected io.println(testing.user.age) to pass diagnostics, got %#v", callDiagnostics)
	}
}

func TestLSPCallableTypeCheckAndNarrowing(t *testing.T) {
	// 1. Call to non-callable variable (string) -> should error
	text1 := strings.Join([]string{
		`const x: string = "hello";`,
		`x();`,
	}, "\n")
	diagnostics1 := semanticDiagnostics("file:///test1.tiny", text1)
	if !diagnosticsContain(diagnostics1, "cannot call non-function type 'string'") {
		t.Fatalf("expected error for calling non-callable string variable, got %#v", diagnostics1)
	}

	// 2. Call to non-narrowed union type (function | null) -> should error
	text2 := strings.Join([]string{
		`const x: function | null = null;`,
		`x();`,
	}, "\n")
	diagnostics2 := semanticDiagnostics("file:///test2.tiny", text2)
	if !diagnosticsContain(diagnostics2, "cannot call non-function type 'function | null'") {
		t.Fatalf("expected error for calling union type containing function directly, got %#v", diagnostics2)
	}

	// 3. Call to narrowed union type (function | null) inside 'if typeof x == "function"' -> should NOT error
	text3 := strings.Join([]string{
		`const x: function | null = null;`,
		`if typeof x == "function" {`,
		`    x();`,
		`}`,
	}, "\n")
	diagnostics3 := semanticDiagnostics("file:///test3.tiny", text3)
	if diagnosticsContain(diagnostics3, "cannot call non-function type") {
		t.Fatalf("expected no call errors inside narrowed typeof function check, got %#v", diagnostics3)
	}
}

func TestLSPUnknownFunctionCallInfersAny(t *testing.T) {
	text := strings.Join([]string{
		`fn get(handler: function) {`,
		`    const item = "value"`,
		`    const result = handler(item)`,
		`    if result {`,
		`        return item`,
		`    }`,
		`}`,
	}, "\n")

	scope := scopeAtPosition("file:///unknown_function_call.tiny", text, Position{Line: 3, Character: 7})
	sym, ok := scope.Resolve("result")
	if !ok {
		t.Fatalf("expected result to be in scope")
	}
	if sym.Type != "any" {
		t.Fatalf("expected result type to be any after calling function-typed handler, got %q", sym.Type)
	}

	diagnostics := semanticDiagnostics("file:///unknown_function_call.tiny", text)
	if diagnosticsContain(diagnostics, "cannot call non-function type") {
		t.Fatalf("expected function-typed handler call to pass diagnostics, got %#v", diagnostics)
	}
}

func TestLSPElseBlockAndNegatedTypeNarrowing(t *testing.T) {
	// 1. Else-block narrowing: x | null is narrowed to non-null in else block
	text1 := strings.Join([]string{
		`const x: function | null = null;`,
		`if x == null {`,
		`    // do nothing`,
		`} else {`,
		`    x();`,
		`}`,
	}, "\n")
	diagnostics1 := semanticDiagnostics("file:///test_else.tiny", text1)
	if diagnosticsContain(diagnostics1, "cannot call non-function type") {
		t.Fatalf("expected no errors inside narrowed else block, got %#v", diagnostics1)
	}

	// 2. Negated check: typeof x != "string" narrows string | number to number
	text2 := strings.Join([]string{
		`const x: string | number = 10;`,
		`if typeof x != "string" {`,
		`    const y: number = x;`,
		`}`,
	}, "\n")
	diagnostics2 := semanticDiagnostics("file:///test_neg.tiny", text2)
	if diagnosticsContain(diagnostics2, "cannot pass type") {
		t.Fatalf("expected x to be narrowed to number, got %#v", diagnostics2)
	}
}

func TestLSPMatchEnumExhaustiveness(t *testing.T) {
	// 1. Missing case -> warning diagnostic
	text1 := strings.Join([]string{
		`enum Status { Active, Suspended }`,
		`const status: Status = Status.Active;`,
		`match status {`,
		`    Status.Active {`,
		`    }`,
		`}`,
	}, "\n")
	diagnostics1 := semanticDiagnostics("file:///test_match1.tiny", text1)
	if !diagnosticsContain(diagnostics1, "match is not exhaustive on 'Status': missing case 'Suspended'") {
		t.Fatalf("expected missing case diagnostic, got %#v", diagnostics1)
	}

	// 2. Default case handles it -> no warning
	text2 := strings.Join([]string{
		`enum Status { Active, Suspended }`,
		`const status: Status = Status.Active;`,
		`match status {`,
		`    Status.Active {`,
		`    }`,
		`    _ {`,
		`    }`,
		`}`,
	}, "\n")
	diagnostics2 := semanticDiagnostics("file:///test_match2.tiny", text2)
	if diagnosticsContain(diagnostics2, "match is not exhaustive") {
		t.Fatalf("expected no warning with default case, got %#v", diagnostics2)
	}
}

func TestLSPImplementMissingMethodsCodeAction(t *testing.T) {
	text := strings.Join([]string{
		`export interface BotConfig {`,
		`    token: string,`,
		`    run: function`,
		`}`,
		`class Bot {`,
		`    embed BotConfig`,
		`}`,
	}, "\n")

	actions := getCodeActions("file:///test_action.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///test_action.tiny"},
		Range: LSPRange{
			Start: Position{Line: 5, Character: 4}, // inside 'class Bot' / 'embed BotConfig'
			End:   Position{Line: 5, Character: 19},
		},
	})

	found := false
	for _, action := range actions {
		if action.Title == "Implement missing methods/fields" {
			found = true
			edits := action.Edit.Changes["file:///test_action.tiny"]
			if len(edits) == 0 {
				t.Fatalf("expected edits in code action")
			}
			newText := edits[0].NewText
			if !strings.Contains(newText, "field token: string =") {
				t.Fatalf("expected newText to implement field token, got: %q", newText)
			}
			if !strings.Contains(newText, "fn run()") {
				t.Fatalf("expected newText to implement method run, got: %q", newText)
			}
		}
	}
	if !found {
		t.Fatalf("expected Implement missing methods/fields code action, got %#v", actions)
	}
}

func TestLSPVariadicParameterTypeChecks(t *testing.T) {
	text := strings.Join([]string{
		`fn myFunc(name: string, ...handlers: function) {`,
		`}`,
		`myFunc("test", "not_a_func");`,
	}, "\n")
	diagnostics := semanticDiagnostics("file:///test_var.tiny", text)
	if !diagnosticsContain(diagnostics, "cannot pass type 'string' to parameter 'handlers'") {
		t.Fatalf("expected error for passing non-function string to variadic parameter, got %#v", diagnostics)
	}
}

func TestLSPCompoundLogicAndNarrowing(t *testing.T) {
	text := strings.Join([]string{
		`const x: string | number | null = null;`,
		`if x and typeof x == "string" {`,
		`    const y: string = x;`,
		`}`,
	}, "\n")
	diagnostics := semanticDiagnostics("file:///test_compound_and.tiny", text)
	if diagnosticsContain(diagnostics, "cannot pass type") {
		t.Fatalf("expected x to be narrowed to string inside compound 'and' block, got %#v", diagnostics)
	}
}

func TestLSPEnumMemberAutoCompleteAndValidation(t *testing.T) {
	// 1. Check autocomplete on enum parameter receiver path is empty
	text := strings.Join([]string{
		`enum TestEnum {`,
		`    tester,`,
		`    other`,
		`}`,
		`fn test(ss: TestEnum) {`,
		`    ss.`,
		`}`,
	}, "\n")

	items := getCompletions("file:///test_enum_ac.tiny", text, Position{Line: 5, Character: 7})
	if completionLabelsContain(items, "tester") || completionLabelsContain(items, "other") {
		t.Fatalf("expected no enum member autocomplete suggestions on parameter variable, got %#v", completionLabels(items))
	}

	// 2. Check hover type feedback on enum members
	textHover := strings.Join([]string{
		`enum StringEnum {`,
		`    tester`,
		`}`,
		`enum NumberEnum {`,
		`    val = 42`,
		`}`,
		`enum IotaEnum {`,
		`    val = iota`,
		`}`,
		`const a = StringEnum.tester;`,
		`const b = NumberEnum.val;`,
		`const c = IotaEnum.val;`,
	}, "\n")

	// Hover tester inside StringEnum definition (Line 1, Character 4)
	resDef1 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 1, Character: 4})
	hDef1, ok := resDef1.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on StringEnum definition tester, got %#v", resDef1)
	}
	if !strings.Contains(hDef1.Contents.Value, "```tiny\nStringEnum.tester: string = \"tester\"\n```") {
		t.Fatalf("expected hover on StringEnum definition to contain Type: string, got %q", hDef1.Contents.Value)
	}

	// Hover val inside NumberEnum definition (Line 4, Character 4)
	resDef2 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 4, Character: 4})
	hDef2, ok := resDef2.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on NumberEnum definition val, got %#v", resDef2)
	}
	if !strings.Contains(hDef2.Contents.Value, "```tiny\nNumberEnum.val: number = 42\n```") {
		t.Fatalf("expected hover on NumberEnum definition to contain Type: number, got %q", hDef2.Contents.Value)
	}

	// Hover val inside IotaEnum definition (Line 7, Character 4)
	resDef3 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 7, Character: 4})
	hDef3, ok := resDef3.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on IotaEnum definition val, got %#v", resDef3)
	}
	if !strings.Contains(hDef3.Contents.Value, "```tiny\nIotaEnum.val: number = 0\n```") {
		t.Fatalf("expected hover on IotaEnum definition to contain Type: number, got %q", hDef3.Contents.Value)
	}

	// Hover StringEnum.tester (Line 9, Character 21)
	res1 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 9, Character: 21})
	h1, ok := res1.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on StringEnum.tester, got %#v", res1)
	}
	if !strings.Contains(h1.Contents.Value, "```tiny\nStringEnum.tester: string = \"tester\"\n```") {
		t.Fatalf("expected hover to contain Type: string, got %q", h1.Contents.Value)
	}

	// Hover NumberEnum.val (Line 10, Character 21)
	res2 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 10, Character: 21})
	h2, ok := res2.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on NumberEnum.val, got %#v", res2)
	}
	if !strings.Contains(h2.Contents.Value, "```tiny\nNumberEnum.val: number = 42\n```") {
		t.Fatalf("expected hover to contain Type: number, got %q", h2.Contents.Value)
	}

	// Hover IotaEnum.val (Line 11, Character 19)
	res3 := getHover("file:///test_enum_hover.tiny", textHover, Position{Line: 11, Character: 19})
	h3, ok := res3.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on IotaEnum.val, got %#v", res3)
	}
	if !strings.Contains(h3.Contents.Value, "```tiny\nIotaEnum.val: number = 0\n```") {
		t.Fatalf("expected hover to contain Type: number, got %q", h3.Contents.Value)
	}

	// 3. Check variable assignments and parameter type checking compatibility
	text2 := strings.Join([]string{
		`enum TestEnum {`,
		`    tester`,
		`}`,
		`fn test(ss: TestEnum) {`,
		`}`,
		`test(TestEnum.tester);`,
		`const s: string = TestEnum.tester;`,
	}, "\n")
	diagnostics2 := semanticDiagnostics("file:///test_enum_val.tiny", text2)
	if diagnosticsContain(diagnostics2, "cannot pass type") || diagnosticsContain(diagnostics2, "cannot assign") {
		t.Fatalf("expected TestEnum.tester to be compatible with TestEnum parameter and string variable, got %#v", diagnostics2)
	}
}

func TestLSPVariadicParameterImprovement(t *testing.T) {
	// 1. Check signature display has array:function
	text := strings.Join([]string{
		`fn test(...handlers: function) {`,
		`}`,
	}, "\n")
	result := getHover("file:///test_var_sig.tiny", text, Position{Line: 0, Character: 3})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "array:function") {
		t.Fatalf("expected hover content to contain array:function, got %q", hover.Contents.Value)
	}

	// 2. Verify that passing correct function type does not trigger error
	text2 := strings.Join([]string{
		`fn test(...handlers: function) {`,
		`}`,
		`test(fn() {});`,
	}, "\n")
	diagnostics2 := semanticDiagnostics("file:///test_var_sig_ok.tiny", text2)
	if diagnosticsContain(diagnostics2, "cannot pass type") {
		t.Fatalf("expected passing function to variadic parameter to succeed, got %#v", diagnostics2)
	}
}

func TestLSPUnreachableCodeDetection(t *testing.T) {
	text := strings.Join([]string{
		`fn test() {`,
		`    return;`,
		`    let x = 1;`,
		`}`,
		`fn testIf(cond: bool) {`,
		`    if cond {`,
		`        return;`,
		`        let y = 2;`,
		`    } else {`,
		`        throw "error";`,
		`        let z = 3;`,
		`    }`,
		`}`,
		`fn testIfElseBothExit(cond: bool) {`,
		`    if cond {`,
		`        return;`,
		`    } else {`,
		`        return;`,
		`    }`,
		`    let a = 4;`,
		`}`,
		`fn testLiteralTrue() {`,
		`    if true {`,
		`        return;`,
		`    }`,
		`    let b = 5;`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_unreachable.tiny", text)
	if !diagnosticsContain(diagnostics, "unreachable code detected") {
		t.Fatalf("expected unreachable code warnings, got %#v", diagnostics)
	}

	count := 0
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "unreachable code detected") {
			count++
		}
	}
	if count != 5 {
		t.Fatalf("expected exactly 5 unreachable code warnings, got %d. Diagnostics: %#v", count, diagnostics)
	}
}

func TestLSPUnreachableCodeAfterReturnValueWithoutSemicolon(t *testing.T) {
	text := strings.Join([]string{
		`import std "io";`,
		`fn test(): string {`,
		`    return ""`,
		``,
		``,
		`    io.println("sdf")`,
		`}`,
		``,
		`test()`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_unreachable_return_value.tiny", text)
	if !diagnosticsContain(diagnostics, "unreachable code detected") {
		t.Fatalf("expected unreachable code warning after return value without semicolon, got %#v", diagnostics)
	}

	var unreachable map[string]any
	for _, diagnostic := range diagnostics {
		message, _ := diagnostic["message"].(string)
		if strings.Contains(message, "unreachable code detected") {
			unreachable = diagnostic
			break
		}
	}
	if unreachable == nil {
		t.Fatalf("expected unreachable diagnostic, got %#v", diagnostics)
	}
	rangeValue, _ := unreachable["range"].(map[string]any)
	start, _ := rangeValue["start"].(map[string]any)
	end, _ := rangeValue["end"].(map[string]any)
	lineText := `    io.println("sdf")`
	if intFromAny(start["line"]) != 5 || intFromAny(start["character"]) != 4 || intFromAny(end["line"]) != 5 || intFromAny(end["character"]) != len(lineText) {
		t.Fatalf("expected unreachable diagnostic to cover full statement line, got %#v", unreachable)
	}

	actions := getCodeActions("file:///test_unreachable_return_value.tiny", text, CodeActionParams{
		TextDocument: TextDocumentIdentifier{URI: "file:///test_unreachable_return_value.tiny"},
		Range: LSPRange{
			Start: Position{Line: 5, Character: strings.Index(lineText, "println")},
			End:   Position{Line: 5, Character: strings.Index(lineText, "println") + len("println")},
		},
		Context: CodeActionContext{Diagnostics: []map[string]any{unreachable}},
	})
	for _, action := range actions {
		if strings.Contains(action.Title, "Create function") {
			t.Fatalf("did not expect create-function quick fix for unreachable member call, got %#v", actions)
		}
	}
}

func TestLSPMissingReturnPathDetection(t *testing.T) {
	text := strings.Join([]string{
		`fn testOk(): number {`,
		`    return 42;`,
		`}`,
		`fn testThrowOk(): string {`,
		`    throw "error";`,
		`}`,
		`fn testMissing(): string {`,
		`    let x = "hello";`,
		`}`,
		`fn testIfOk(cond: bool): number {`,
		`    if cond {`,
		`        return 1;`,
		`    } else {`,
		`        return 2;`,
		`    }`,
		`}`,
		`fn testIfMissing(cond: bool): number {`,
		`    if cond {`,
		`        return 1;`,
		`    }`,
		`}`,
		`fn testWhileTrueOk(): number {`,
		`    while true {`,
		`        return 1;`,
		`    }`,
		`}`,
		`fn testIfTrueOk(): number {`,
		`    if true {`,
		`        return 1;`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_missing_return.tiny", text)
	if !diagnosticsContain(diagnostics, "missing return") {
		t.Fatalf("expected missing return diagnostics, got %#v", diagnostics)
	}

	hasTestMissing := false
	hasTestIfMissing := false
	hasTestOk := false
	hasTestThrowOk := false
	hasTestIfOk := false
	hasTestWhileTrueOk := false
	hasTestIfTrueOk := false

	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "missing return") {
			if strings.Contains(msg, "testMissing") {
				hasTestMissing = true
			}
			if strings.Contains(msg, "testIfMissing") {
				hasTestIfMissing = true
			}
			if strings.Contains(msg, "testOk") {
				hasTestOk = true
			}
			if strings.Contains(msg, "testThrowOk") {
				hasTestThrowOk = true
			}
			if strings.Contains(msg, "testIfOk") {
				hasTestIfOk = true
			}
			if strings.Contains(msg, "testWhileTrueOk") {
				hasTestWhileTrueOk = true
			}
			if strings.Contains(msg, "testIfTrueOk") {
				hasTestIfTrueOk = true
			}
		}
	}

	if !hasTestMissing {
		t.Fatalf("expected function testMissing to have missing return warning")
	}
	if !hasTestIfMissing {
		t.Fatalf("expected function testIfMissing to have missing return warning")
	}
	if hasTestOk || hasTestThrowOk || hasTestIfOk || hasTestWhileTrueOk || hasTestIfTrueOk {
		t.Fatalf("unexpected missing return warning on valid functions. Diagnostics: %#v", diagnostics)
	}
}

func TestLSPGenericsDiagnostics(t *testing.T) {
	text := strings.Join([]string{
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
		`let id: string = identity:string("hello");`,
		`let valStr: string = b.value.toString();`,
		`b.init("hello");`,          // Should error: expected number, got string
		`identity:number("hello");`, // Should error: expected number, got string
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_generics.tiny", text)

	hasInitError := false
	hasIdErrError := false

	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "unused variable:") || strings.Contains(msg, "unused function:") {
			continue
		}

		rng, _ := d["range"].(map[string]any)
		start, _ := rng["start"].(map[string]any)
		lineVal := intFromAny(start["line"])
		t.Logf("Line %d diagnostic: %s", lineVal, msg)

		if lineVal < 12 {
			t.Fatalf("unexpected diagnostic on line %d: %s", lineVal+1, msg)
		}
		if lineVal == 12 {
			if strings.Contains(msg, "cannot pass type 'string' to parameter 'val'") && strings.Contains(msg, "expected 'number'") {
				hasInitError = true
			}
		}
		if lineVal == 13 {
			if strings.Contains(msg, "cannot pass type 'string' to parameter 'x'") && strings.Contains(msg, "expected 'number'") {
				hasIdErrError = true
			}
		}
	}

	if !hasInitError {
		t.Fatalf("expected type checking error for b.init(\"hello\") on line 13")
	}
	if !hasIdErrError {
		t.Fatalf("expected type checking error for identity:number(\"hello\") on line 14")
	}
}

func TestLSPGenericsAutoInferenceAndAutocomplete(t *testing.T) {
	text := strings.Join([]string{
		`class Box:T {`,
		`    field value: T = null`,
		`    fn init(val: T) {`,
		`        this.value = val;`,
		`    }`,
		`}`,
		`class Pair:A:B {`,
		`    field first: A = null`,
		`    field second: B = null`,
		`    fn init(f: A, s: B) {`,
		`        this.first = f;`,
		`        this.second = s;`,
		`    }`,
		`}`,
		`let b = Box(42);`,
		`let p = Pair("hello", true);`,
		`b.`,
		`p.`,
		`let val = b.value;`,
		`let firstVal = p.first;`,
		`let secondVal = p.second;`,
	}, "\n")

	// Verify completions on b. (line 16)
	completionsB := getCompletions("file:///test_inference.tiny", text, Position{
		Line:      16,
		Character: len("b."),
	})

	hasValueB := false
	for _, item := range completionsB {
		if item.Label == "value" {
			hasValueB = true
			if !strings.Contains(item.Detail, "number") {
				t.Fatalf("expected completion for b.value to have type number, got detail: %q", item.Detail)
			}
		}
	}
	if !hasValueB {
		t.Fatalf("expected completions for b. to include 'value'")
	}

	// Verify completions on p. (line 17)
	completionsP := getCompletions("file:///test_inference.tiny", text, Position{
		Line:      17,
		Character: len("p."),
	})

	hasFirstP := false
	hasSecondP := false
	for _, item := range completionsP {
		if item.Label == "first" {
			hasFirstP = true
			if !strings.Contains(item.Detail, "string") {
				t.Fatalf("expected completion for p.first to have type string, got detail: %q", item.Detail)
			}
		}
		if item.Label == "second" {
			hasSecondP = true
			if !strings.Contains(item.Detail, "bool") {
				t.Fatalf("expected completion for p.second to have type bool, got detail: %q", item.Detail)
			}
		}
	}
	if !hasFirstP || !hasSecondP {
		t.Fatalf("expected completions for p. to include 'first' and 'second'")
	}

	// Verify hover on b.value (line 18, character 13)
	hoverValResult := getHover("file:///test_inference.tiny", text, Position{
		Line:      18,
		Character: strings.Index(`let val = b.value;`, "value") + 2,
	})
	hoverVal, ok := hoverValResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on b.value, got %#v", hoverValResult)
	}
	if !strings.Contains(hoverVal.Contents.Value, "number") {
		t.Fatalf("expected hover for b.value to contain 'number', got: %q", hoverVal.Contents.Value)
	}

	// Verify hover on p.first (line 19, character 17)
	hoverFirstResult := getHover("file:///test_inference.tiny", text, Position{
		Line:      19,
		Character: strings.Index(`let firstVal = p.first;`, "first") + 2,
	})
	hoverFirst, ok := hoverFirstResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result on p.first, got %#v", hoverFirstResult)
	}
	if !strings.Contains(hoverFirst.Contents.Value, "string") {
		t.Fatalf("expected hover for p.first to contain 'string', got: %q", hoverFirst.Contents.Value)
	}

	// Verify diagnostics
	diagnostics := semanticDiagnostics("file:///test_inference.tiny", text)
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "unused variable:") || strings.Contains(msg, "unused function:") {
			continue
		}
		t.Fatalf("unexpected warning/diagnostic: %q", msg)
	}
}

func TestLSPValidateSchemaGenericArgumentInference(t *testing.T) {
	text := strings.Join([]string{
		`import std "validate";`,
		`const tags = validate.array(validate.string().required()).default([]);`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///validate_generics_array.tiny", text)
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "unused ") {
			continue
		}
		if strings.Contains(msg, "cannot pass type") || strings.Contains(msg, "cannot assign") {
			t.Fatalf("unexpected validate.array generic diagnostic: %s", msg)
		}
	}
}

func TestLSPValidateBodyAcceptsGenericSchema(t *testing.T) {
	text := strings.Join([]string{
		`import std "validate";`,
		`const createPostSchema = validate.body(`,
		`    validate.object({`,
		`        title: validate.string().trim().nonempty().min(3).required(),`,
		`        content: validate.string().trim().min(10).required(),`,
		`        tags: validate.array(validate.string().required()).default([]),`,
		`        published: validate.bool().default(false)`,
		`    })`,
		`);`,
		`const req = {`,
		`    body: {`,
		`        title: "Hello Tiny",`,
		`        content: "This is my first post.",`,
		`        tags: ["tiny", "lang"]`,
		`    },`,
		`    query: {},`,
		`    params: {}`,
		`};`,
		`const parsed = createPostSchema.safeParse(req);`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///validate_generics_body.tiny", text)
	for _, d := range diagnostics {
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "unused ") {
			continue
		}
		if strings.Contains(msg, "cannot pass type") || strings.Contains(msg, "cannot assign") {
			t.Fatalf("unexpected validate.body generic diagnostic: %s", msg)
		}
	}
}

func TestLSPValidateSchemaChainCompletionAndHover(t *testing.T) {
	text := strings.Join([]string{
		`import std "validate";`,
		`validate.string().`,
		`validate.string().trim().`,
	}, "\n")

	completions1 := getCompletions("file:///validate_chain.tiny", text, Position{
		Line:      1,
		Character: len(`validate.string().`),
	})
	if !completionLabelsContain(completions1, "trim") {
		t.Fatalf("expected validate.string() completions to include trim, got %#v", completionLabels(completions1))
	}
	if !completionLabelsContain(completions1, "nonempty") {
		t.Fatalf("expected validate.string() completions to include nonempty, got %#v", completionLabels(completions1))
	}

	completions2 := getCompletions("file:///validate_chain.tiny", text, Position{
		Line:      2,
		Character: len(`validate.string().trim().`),
	})
	if !completionLabelsContain(completions2, "default") {
		t.Fatalf("expected validate.string().trim() completions to include default, got %#v", completionLabels(completions2))
	}
	if !completionLabelsContain(completions2, "safeParse") {
		t.Fatalf("expected validate.string().trim() completions to include safeParse, got %#v", completionLabels(completions2))
	}

	line1 := strings.Split(text, "\n")[2]
	hoverPos := Position{
		Line:      2,
		Character: strings.Index(line1, "trim") + 1,
	}
	hover := getHover("file:///validate_chain.tiny", text, hoverPos)
	hoverResult, ok := hover.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for trim, got %#v", hover)
	}
	if !strings.Contains(hoverResult.Contents.Value, "trim") {
		t.Fatalf("expected hover for trim to mention trim, got %q", hoverResult.Contents.Value)
	}
}

func TestLSPValidateSchemaVariableInference(t *testing.T) {
	text := strings.Join([]string{
		`import std "validate"`,
		`const createPostSchema = validate.body(`,
		`    validate.object({`,
		`        title: validate.string().trim().nonempty().min(3).required(),`,
		`        content: validate.string().trim().min(10).required(),`,
		`        tags: validate.array(validate.string().required()).default([]),`,
		`        published: validate.bool().default(false)`,
		`    })`,
		`)`,
		`const parsed = createPostSchema.safeParse({ body: {}, query: {}, params: {} })`,
		`const post = parsed.data`,
	}, "\n")

	hoverSchema := getHover("file:///validate_inference.tiny", text, Position{
		Line:      1,
		Character: strings.Index(strings.Split(text, "\n")[1], "createPostSchema") + 2,
	})
	hoverSchemaResult, ok := hoverSchema.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for createPostSchema, got %#v", hoverSchema)
	}
	if !strings.Contains(hoverSchemaResult.Contents.Value, "class:Schema:object") {
		t.Fatalf("expected createPostSchema hover to preserve Schema generic type, got %q", hoverSchemaResult.Contents.Value)
	}

	hoverPost := getHover("file:///validate_inference.tiny", text, Position{
		Line:      10,
		Character: strings.Index(strings.Split(text, "\n")[10], "post") + 1,
	})
	hoverPostResult, ok := hoverPost.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for post, got %#v", hoverPost)
	}
	if strings.Contains(hoverPostResult.Contents.Value, "`T`") {
		t.Fatalf("expected post hover to not leak raw type parameter T, got %q", hoverPostResult.Contents.Value)
	}
}

func TestLSPGenericFunctionTypeParamResolution(t *testing.T) {
	text := strings.Join([]string{
		`fn test:T(testFn: function(T)) {`,
		`    testFn("hello");`,
		`}`,
		`test:string(fn(v) {`,
		`    v.`,
		`});`,
	}, "\n")

	scope := scopeAtPosition("file:///generic_callback.tiny", text, Position{
		Line:      4,
		Character: len(`    v.`),
	})
	if got := expectedInlineFunctionParamTypes(scope, text, Position{Line: 3, Character: len(`test:string(`)}, strings.Index(text, `fn(v)`)); len(got) != 1 || got[0] != "string" {
		t.Fatalf("expected inline function param types [string], got %#v", got)
	}

	vSym, ok := scope.Resolve("v")
	if !ok {
		t.Fatal("expected callback parameter v in scope")
	}
	if vSym.Type != "string" {
		t.Fatalf("v type = %q, want string", vSym.Type)
	}

	items := getCompletions("file:///generic_callback.tiny", text, Position{
		Line:      4,
		Character: len(`    v.`),
	})
	if !completionLabelsContain(items, "split") {
		t.Fatalf("expected v. completions to include string method split, got %#v", completionLabels(items))
	}
}

func TestLSPGenericImplicitTypeInferenceFromArgs(t *testing.T) {
	text := strings.Join([]string{
		`fn test:T(testing: T, testFn: function(T)): T {`,
		`    return`,
		`}`,
		`test("", fn(i) {`,
		`    i.`,
		`});`,
	}, "\n")

	scope := scopeAtPosition("file:///generic_implicit.tiny", text, Position{
		Line:      4,
		Character: len(`    i.`),
	})
	if got := expectedInlineFunctionParamTypes(scope, text, Position{Line: 3, Character: len(`test(`)}, strings.Index(text, `fn(i)`)); len(got) != 1 || got[0] != "string" {
		t.Fatalf("expected inline function param types [string] from implicit inference, got %#v", got)
	}

	vSym, ok := scope.Resolve("i")
	if !ok {
		t.Fatal("expected callback parameter i in scope")
	}
	if vSym.Type != "string" {
		t.Fatalf("i type = %q, want string", vSym.Type)
	}

	items := getCompletions("file:///generic_implicit.tiny", text, Position{
		Line:      4,
		Character: len(`    i.`),
	})
	if !completionLabelsContain(items, "split") {
		t.Fatalf("expected i. completions to include string method split, got %#v", completionLabels(items))
	}
}

func TestLSPNestedCallbackParamTypeResolution(t *testing.T) {
	text := strings.Join([]string{
		`class Conn {`,
		`    fn onMessage(handler: function(Message)) {}`,
		`}`,
		`interface Message {`,
		`    type: string,`,
		`    data: any`,
		`}`,
		`fn onConnection(handler: function(Conn)) {}`,
		`onConnection(fn(conn) {`,
		`    conn.onMessage(fn(msg) {`,
		`        msg.`,
		`    })`,
		`})`,
	}, "\n")

	scope := scopeAtPosition("file:///nested_callback.tiny", text, Position{
		Line:      9,
		Character: len(`        msg.`),
	})

	connSym, ok := scope.Resolve("conn")
	if !ok {
		t.Fatal("expected conn in scope")
	}
	t.Logf("conn type: %q", connSym.Type)

	msgSym, ok := scope.Resolve("msg")
	if !ok {
		t.Fatal("expected callback parameter msg in scope")
	}
	t.Logf("msg type: %q", msgSym.Type)

	fnMsgOffset := strings.Index(text, `fn(msg)`)
	fnByteOffset := bytePositionAtOffset(text, fnMsgOffset)
	inferredTypes := expectedInlineFunctionParamTypes(scope, text, fnByteOffset, fnMsgOffset)
	t.Logf("inferredTypes for fn(msg): %#v", inferredTypes)

	open := findUnclosedCallParen(text[:fnMsgOffset])
	t.Logf("findUnclosedCallParen returned offset %d, char: %q", open, string(text[open]))

	callee := extractCalleeBefore(text, open)
	t.Logf("callee: %q", callee)
}

func TestLSPInterfaceImplements(t *testing.T) {
	// 1. Success case: MyClass implements Reader, defines 'read' method, passes to readAll
	text1 := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		``,
		`export class MyClass implements Reader {`,
		`    fn read(n: number) {}`,
		`}`,
		``,
		`export fn readAll(r: Reader) {}`,
		``,
		`export fn test() {`,
		`    readAll(new MyClass());`,
		`}`,
	}, "\n")

	diagnostics1 := semanticDiagnostics("file:///test1.tiny", text1)
	if len(diagnostics1) > 0 {
		t.Fatalf("expected no diagnostics for implements match, got %#v", diagnostics1)
	}

	// 2. Failure case: MyClass implements Reader but lacks 'read' method
	text2 := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		``,
		`export class MyClass implements Reader {`,
		`    fn other() {}`,
		`}`,
	}, "\n")

	diagnostics2 := semanticDiagnostics("file:///test2.tiny", text2)
	if !diagnosticsContain(diagnostics2, "class 'MyClass' is missing property 'read' from interface 'Reader'") {
		t.Fatalf("expected diagnostic for missing interface property, got %#v", diagnostics2)
	}

	// 3. Object literal case: matches interface type because got is "object"
	text3 := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		``,
		`export fn readAll(r: Reader) {}`,
		``,
		`export fn test() {`,
		`    readAll({`,
		`        read: fn(n) {}`,
		`    });`,
		`}`,
	}, "\n")

	diagnostics3 := semanticDiagnostics("file:///test3.tiny", text3)
	if len(diagnostics3) > 0 {
		t.Fatalf("expected no diagnostics for object literal match, got %#v", diagnostics3)
	}
}

func TestLSPInterpolatedStringUndefinedVariableDiagnostic(t *testing.T) {
	text := strings.Join([]string{
		`import std "io";`,
		`interface Logger {`,
		`    logError: function(string)`,
		`    logInfo: function(string)`,
		`}`,
		`class CustomLogger implements Logger {`,
		`    fn logError() {`,
		"        io.println(`[ERROR]: ${text}`)",
		`    }`,
		`    fn logInfo(text) {`,
		"        io.println(`[INFO]: ${text}`)",
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///interpolated_undefined.tiny", text)
	if !diagnosticsContain(diagnostics, "undefined variable: text") {
		t.Fatalf("expected undefined variable diagnostic inside interpolation, got %#v", diagnostics)
	}
}

func TestLSPClassImplementsFunctionSignatureMismatch(t *testing.T) {
	text := strings.Join([]string{
		`interface Logger {`,
		`    logError: function(string)`,
		`    logInfo: function(string)`,
		`}`,
		`class CustomLogger implements Logger {`,
		`    fn logError() {}`,
		`    fn logInfo(text) {}`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///implements_signature.tiny", text)
	if !diagnosticsContain(diagnostics, "class 'CustomLogger' property 'logError' does not match interface 'Logger'") {
		t.Fatalf("expected implements signature diagnostic, got %#v", diagnostics)
	}
}

func TestLSPClassImplementsExtendedInterface(t *testing.T) {
	text := strings.Join([]string{
		`interface Entity {`,
		`    id: number`,
		`}`,
		`interface User extends Entity {`,
		`    name: string`,
		`}`,
		`class MissingId implements User {`,
		`    field name: string = "Ada"`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///implements_extends.tiny", text)
	if !diagnosticsContain(diagnostics, "class 'MissingId' is missing property 'id' from interface 'User'") {
		t.Fatalf("expected missing inherited interface property diagnostic, got %#v", diagnostics)
	}
}

func TestLSPClassAssignableToExtendedInterfaceParent(t *testing.T) {
	text := strings.Join([]string{
		`interface Entity {`,
		`    id: number`,
		`}`,
		`interface User extends Entity {`,
		`    name: string`,
		`}`,
		`class Person implements User {`,
		`    field id: number = 1`,
		`    field name: string = "Ada"`,
		`}`,
		`fn acceptEntity(entity: Entity) {}`,
		`acceptEntity(Person())`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///implements_extends_assign.tiny", text)
	if len(diagnostics) > 0 {
		t.Fatalf("expected class implementing child interface to satisfy parent interface, got %#v", diagnostics)
	}
}

func TestLSPClassImplementsAutocomplete(t *testing.T) {
	text := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		``,
		`export class MyClass implements Reader {`,
		`    `,
		`}`,
	}, "\n")

	completions := getCompletions("file:///test_implements_ac.tiny", text, Position{
		Line:      5,
		Character: 4,
	})

	if len(completions) == 0 {
		t.Fatalf("expected autocomplete items inside MyClass body, got none")
	}

	if !completionLabelsContain(completions, "fn") {
		t.Fatalf("expected autocomplete items to contain 'fn', got %#v", completionLabels(completions))
	}
}

func TestLSPNestedFunctionReturnDiagnostics(t *testing.T) {
	text := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		``,
		`export fn test(): Reader {`,
		`    return {`,
		`        read: fn(i) {`,
		`            return i + 1`,
		`        },`,
		`        ass: "sdfsdf"`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_nested_return.tiny", text)
	if len(diagnostics) > 0 {
		t.Fatalf("expected no diagnostics for nested anonymous function return, got %#v", diagnostics)
	}
}

func TestLSPObjectLiteralStructuralDiagnostics(t *testing.T) {
	// 1. Missing field
	text1 := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number),`,
		`    ass: string`,
		`}`,
		`export fn test(): Reader {`,
		`    return {`,
		`        read: fn(i) {`,
		`            return i + 1`,
		`        }`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics1 := semanticDiagnostics("file:///test_ol_missing.tiny", text1)
	if !diagnosticsContain(diagnostics1, "object literal is missing property 'ass' from 'Reader'") {
		t.Fatalf("expected diagnostic for missing property, got %#v", diagnostics1)
	}

	// 2. Type mismatch
	text2 := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number),`,
		`    ass: string`,
		`}`,
		`export fn test(): Reader {`,
		`    return {`,
		`        read: fn(i) {`,
		`            return i + 1`,
		`        },`,
		`        ass: 123`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics2 := semanticDiagnostics("file:///test_ol_mismatch.tiny", text2)
	if !diagnosticsContain(diagnostics2, "type mismatch for property 'ass': expected 'string', got 'number'") {
		t.Fatalf("expected diagnostic for type mismatch, got %#v", diagnostics2)
	}
}

func TestLSPObjectVariableStructuralDiagnostics(t *testing.T) {
	text1 := strings.Join([]string{
		`interface Data {`,
		`    test: string`,
		`}`,
		`fn ret(): Data {`,
		`    const data = { text: "x" }`,
		`    return data`,
		`}`,
	}, "\n")

	diagnostics1 := semanticDiagnostics("file:///test_object_var_return_missing.tiny", text1)
	if !diagnosticsContain(diagnostics1, "object literal is missing property 'test' from 'Data'") {
		t.Fatalf("expected diagnostic for object variable missing interface field, got %#v", diagnostics1)
	}

	text2 := strings.Join([]string{
		`interface Data {`,
		`    test: string`,
		`}`,
		`fn ret(): Data {`,
		`    const data = { test: 1 }`,
		`    return data`,
		`}`,
	}, "\n")

	diagnostics2 := semanticDiagnostics("file:///test_object_var_return_mismatch.tiny", text2)
	if !diagnosticsContain(diagnostics2, "type mismatch for property 'test': expected 'string', got 'number'") {
		t.Fatalf("expected diagnostic for object variable field mismatch, got %#v", diagnostics2)
	}
}

func TestLSPObjectLiteralOptionalInterfaceFieldsAreNotRequired(t *testing.T) {
	text := strings.Join([]string{
		`export interface User {`,
		`    id: string,`,
		`    name?: string`,
		`}`,
		`export fn test(): User {`,
		`    return {`,
		`        id: "1"`,
		`    }`,
		`}`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_optional_interface_return.tiny", text)
	if diagnosticsContain(diagnostics, "missing property 'name'") {
		t.Fatalf("did not expect optional field diagnostic, got %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "cannot return type 'object'") || diagnosticsContain(diagnostics, "expected 'User'") {
		t.Fatalf("did not expect object/interface return diagnostic, got %#v", diagnostics)
	}
}

func TestLSPRuntimeNewVMOptionsArePartial(t *testing.T) {
	text := strings.Join([]string{
		`import std "runtime" as runtime`,
		`const vm = runtime.newVM({`,
		`    disableJIT: true,`,
		`    runMainOnLoad: false`,
		`})`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_runtime_newvm_partial.tiny", text)
	for _, field := range []string{"globals", "isolated", "allowedStdlib", "cliArgs"} {
		if diagnosticsContain(diagnostics, "missing property '"+field+"'") {
			t.Fatalf("did not expect missing %s diagnostic, got %#v", field, diagnostics)
		}
	}
}

func TestLSPRuntimeNewVMEmptyQuotedKeyCompletions(t *testing.T) {
	text := strings.Join([]string{
		`import std "runtime" as runtime`,
		`const vm = runtime.newVM({`,
		`    ""`,
		`})`,
	}, "\n")

	items := getCompletions("file:///test_runtime_newvm_empty_key.tiny", text, Position{
		Line:      2,
		Character: len(`    "`),
	})
	for _, label := range []string{`"disableJIT": `, `"runMainOnLoad": `, `"allowedStdlib": `} {
		if !completionLabelsContain(items, label) {
			t.Fatalf("expected runtime.newVM empty quoted key completions to include %q, got %#v", label, completionLabels(items))
		}
	}
	for _, item := range items {
		if item.Label == `"disableJIT": ` && item.FilterText != "disableJIT" {
			t.Fatalf("expected quoted key completion to filter by raw field name, got %#v", item)
		}
	}
}

func TestLSPRuntimeNewVMUnclosedQuotedKeyCompletions(t *testing.T) {
	cases := []struct {
		name string
		text string
		line int
		char int
	}{
		{
			name: "empty object",
			text: strings.Join([]string{
				`import std "runtime" as runtime`,
				`const vm = runtime.newVM({`,
				`    "`,
				`})`,
			}, "\n"),
			line: 2,
			char: len(`    "`),
		},
		{
			name: "after existing fields",
			text: strings.Join([]string{
				`import std "runtime" as runtime`,
				`const vm = runtime.newVM({`,
				`    disableJIT: true,`,
				`    runMainOnLoad: false,`,
				`    "`,
				`})`,
			}, "\n"),
			line: 4,
			char: len(`    "`),
		},
	}

	for _, tc := range cases {
		items := getCompletions("file:///test_runtime_newvm_unclosed_key.tiny", tc.text, Position{
			Line:      tc.line,
			Character: tc.char,
		})
		for _, label := range []string{`"disableJIT": `, `"runMainOnLoad": `, `"allowedStdlib": `} {
			if !completionLabelsContain(items, label) {
				t.Fatalf("%s: expected runtime.newVM unclosed quoted key completions to include %q, got %#v", tc.name, label, completionLabels(items))
			}
		}
	}
}

func TestLSPRuntimeNewVMInlineQuotedKeyCompletions(t *testing.T) {
	line := `const vm = runtime.newVM({""})`
	text := strings.Join([]string{
		`import std "runtime" as runtime`,
		line,
	}, "\n")

	quoteIndex := strings.Index(line, `""`)
	positions := []struct {
		name      string
		character int
	}{
		{name: "between quotes", character: quoteIndex + 1},
		{name: "after closing quote", character: quoteIndex + 2},
	}

	for _, pos := range positions {
		items := getCompletions("file:///test_runtime_newvm_inline_key.tiny", text, Position{
			Line:      1,
			Character: pos.character,
		})
		for _, label := range []string{`"disableJIT": `, `"runMainOnLoad": `, `"allowedStdlib": `} {
			if !completionLabelsContain(items, label) {
				t.Fatalf("%s: expected inline runtime.newVM quoted key completions to include %q, got %#v", pos.name, label, completionLabels(items))
			}
		}
	}
}

func TestLSPFunctionCompletionTriggersParameterHints(t *testing.T) {
	text := strings.Join([]string{
		`fn send(name: string, count: number) {}`,
		`se`,
	}, "\n")

	items := getCompletions("file:///test_function_completion_parameter_hints.tiny", text, Position{
		Line:      1,
		Character: len(`se`),
	})
	item, ok := completionItemByLabel(items, "send")
	if !ok {
		t.Fatalf("expected function completion for send, got %#v", completionLabels(items))
	}
	if item.Command == nil || item.Command.Command != "editor.action.triggerParameterHints" {
		t.Fatalf("expected function completion to trigger parameter hints, got %#v", item)
	}
}

func TestLSPHoverInterfaceShowsFields(t *testing.T) {
	text := strings.Join([]string{
		`interface User {`,
		`    id: string,`,
		`    name?: string`,
		`}`,
		`let user: User = { id: "1" }`,
	}, "\n")

	result := getHover("file:///test_interface_hover_fields.tiny", text, Position{
		Line:      4,
		Character: len(`let user: U`),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for interface type, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "id: string") || !strings.Contains(hover.Contents.Value, "name: string | null") {
		t.Fatalf("expected interface hover to include fields, got %q", hover.Contents.Value)
	}
}

func TestLSPDiagnosticsDeduplicateIdenticalMessages(t *testing.T) {
	analyzer := &astSemanticAnalyzer{uri: "file:///dedupe.tiny", text: "const x = 1"}
	analyzer.addDiagnostic(1, 7, "duplicate diagnostic")
	analyzer.addDiagnostic(1, 7, "duplicate diagnostic")
	analyzer.addError(1, 7, "duplicate error")
	analyzer.addError(1, 7, "duplicate error")

	if len(analyzer.diagnostics) != 2 {
		t.Fatalf("expected duplicate diagnostics to be collapsed, got %#v", analyzer.diagnostics)
	}
}

func TestLSPDeduplicateDiagnosticsKeepsDifferentRanges(t *testing.T) {
	d1 := makeRangeDiagnostic(1, 1, 2, 2, "same")
	d2 := makeRangeDiagnostic(2, 1, 2, 2, "same")
	d3 := makeRangeDiagnostic(1, 1, 2, 2, "same")
	diagnostics := dedupeDiagnostics([]map[string]any{d1, d2, d3})
	if len(diagnostics) != 2 {
		t.Fatalf("expected same-message diagnostics on different ranges to be preserved, got %#v", diagnostics)
	}
}

func TestLSPObjectLiteralMissingFieldsAreCollapsed(t *testing.T) {
	text := strings.Join([]string{
		`interface Config {`,
		`    host: string,`,
		`    port: number,`,
		`    secure: bool`,
		`}`,
		`fn configure(config: Config) {}`,
		`configure({})`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_missing_fields_collapsed.tiny", text)
	count := 0
	for _, diagnostic := range diagnostics {
		msg, _ := diagnostic["message"].(string)
		if strings.Contains(msg, "object literal is missing") {
			count++
			if !strings.Contains(msg, "'host'") || !strings.Contains(msg, "'port'") || !strings.Contains(msg, "'secure'") {
				t.Fatalf("expected collapsed missing fields message, got %q", msg)
			}
			if strings.Contains(msg, "Config | null") {
				t.Fatalf("expected structural type name in diagnostic, got %q", msg)
			}
		}
	}
	if count != 1 {
		t.Fatalf("expected one collapsed missing-fields diagnostic, got %d diagnostics: %#v", count, diagnostics)
	}
}

func TestLSPHttpRequestQuotedObjectKeyCompletions(t *testing.T) {
	text := strings.Join([]string{
		`import std "http" as http`,
		`http.request({`,
		`    ""`,
		`})`,
	}, "\n")

	completions := getCompletions("file:///test_http_request_completion.tiny", text, Position{
		Line:      2,
		Character: len(`    "`),
	})
	if !completionLabelsContain(completions, `"url": `) {
		t.Fatalf("expected http.request object completions to include quoted url, got %#v", completionLabels(completions))
	}
}

func TestLSPHttpMultipartNestedFileCompletionsAndHover(t *testing.T) {
	text := strings.Join([]string{
		`import std "http" as http`,
		`http.post("", {`,
		`    files: [`,
		`        {`,
		`            `,
		`        }`,
		`    ]`,
		`})`,
	}, "\n")

	items := getCompletions("file:///test_http_multipart_nested_file.tiny", text, Position{
		Line:      4,
		Character: len(`            `),
	})
	if !completionLabelsContain(items, "filename: ") {
		t.Fatalf("expected MultipartFile nested completions to include filename, got %#v", completionLabels(items))
	}

	hoverText := strings.Join([]string{
		`import std "http" as http`,
		`http.post("", {`,
		`    files: [`,
		`        {`,
		`            filename: ""`,
		`        }`,
		`    ]`,
		`})`,
	}, "\n")

	filesHoverResult := getHover("file:///test_http_multipart_hover.tiny", hoverText, Position{
		Line:      2,
		Character: len(`    fi`),
	})
	filesHover, ok := filesHoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for MultipartBody.files, got %#v", filesHoverResult)
	}
	if !strings.Contains(filesHover.Contents.Value, "```tiny\nMultipartBody.files: array:interface:MultipartFile | object | null\n```") {
		t.Fatalf("expected files hover to use Tiny declaration markdown, got %q", filesHover.Contents.Value)
	}
	if strings.Contains(filesHover.Contents.Value, "Type: `") {
		t.Fatalf("expected files hover not to use Type line, got %q", filesHover.Contents.Value)
	}

	filenameHoverResult := getHover("file:///test_http_multipart_hover.tiny", hoverText, Position{
		Line:      4,
		Character: len(`            file`),
	})
	filenameHover, ok := filenameHoverResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for MultipartFile.filename, got %#v", filenameHoverResult)
	}
	if !strings.Contains(filenameHover.Contents.Value, "```tiny\nMultipartFile.filename: string | null\n```") {
		t.Fatalf("expected filename hover to use Tiny declaration markdown, got %q", filenameHover.Contents.Value)
	}
}

func TestLSPNestedArrayObjectFieldTypeMismatch(t *testing.T) {
	text := strings.Join([]string{
		`import std "http" as http`,
		`http.post("https://example.com/upload", {`,
		`    multipart: true,`,
		`    form: { username: "tiny" },`,
		`    files: [{ field: "file", path: 4 }]`,
		`})`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_nested_arr.tiny", text)
	if !diagnosticsContain(diagnostics, "type mismatch for property 'path': expected 'string | null', got 'number'") {
		t.Fatalf("expected nested array object field type mismatch diagnostic, got %#v", diagnostics)
	}
}

func TestLSPQuotedObjectKeyCompletionsForStructuralAndNestedTypes(t *testing.T) {
	cases := []struct {
		name      string
		text      string
		line      int
		character int
		want      string
	}{
		{
			name: "double quoted structural key",
			text: strings.Join([]string{
				`fn send(payload: {name: string, age: number}) {}`,
				`send({`,
				`    "`,
				`})`,
			}, "\n"),
			line:      2,
			character: len(`    "`),
			want:      `"name": `,
		},
		{
			name: "single quoted structural key",
			text: strings.Join([]string{
				`fn send(payload: {name: string, age: number}) {}`,
				`send({`,
				`    '`,
				`})`,
			}, "\n"),
			line:      2,
			character: len(`    '`),
			want:      `'name': `,
		},
		{
			name: "double quoted nested array object key",
			text: strings.Join([]string{
				`import std "http" as http`,
				`http.post("", {`,
				`    files: [`,
				`        {`,
				`            "`,
				`        }`,
				`    ]`,
				`})`,
			}, "\n"),
			line:      4,
			character: len(`            "`),
			want:      `"filename": `,
		},
		{
			name: "single quoted nested array object key",
			text: strings.Join([]string{
				`import std "http" as http`,
				`http.post("", {`,
				`    files: [`,
				`        {`,
				`            '`,
				`        }`,
				`    ]`,
				`})`,
			}, "\n"),
			line:      4,
			character: len(`            '`),
			want:      `'filename': `,
		},
	}

	for _, tc := range cases {
		items := getCompletions("file:///test_quoted_object_key_"+tc.name+".tiny", tc.text, Position{
			Line:      tc.line,
			Character: tc.character,
		})
		if !completionLabelsContain(items, tc.want) {
			t.Fatalf("%s: expected quoted object key completions to include %q, got %#v", tc.name, tc.want, completionLabels(items))
		}
	}
}

func TestLSPInlayHintsDisabledForNamespaceTypes(t *testing.T) {
	text := strings.Join([]string{
		`namespace io {`,
		`    export interface Reader {`,
		`        read: function(number)`,
		`    }`,
		`    export fn getReader(): Reader {`,
		`        return {`,
		`            read: fn(i) {}`,
		`        }`,
		`    }`,
		`}`,
		`const r = io.getReader();`,
	}, "\n")

	hints := getInlayHints("file:///test_inlay_ns.tiny", text, LSPRange{
		Start: Position{Line: 10, Character: 0},
		End:   Position{Line: 10, Character: len(`const r = io.getReader();`)},
	})
	if len(hints) != 0 {
		t.Fatalf("expected inlay hints to be disabled, got %#v", hints)
	}
}

func TestLSPObjectLiteralCallbackParameterTypeInference(t *testing.T) {
	text := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number)`,
		`}`,
		`export fn test(): Reader {`,
		`    return {`,
		`        read: fn(i) {`,
		`            return i + 1`,
		`        }`,
		`    }`,
		`}`,
	}, "\n")

	pos := Position{Line: 6, Character: 24}
	innerScope := scopeAtPosition("file:///test_cb_inference.tiny", text, pos)
	sym, ok := innerScope.Resolve("i")
	if !ok {
		t.Fatalf("expected variable i to be defined in scope")
	}
	if sym.Type != "number" {
		t.Fatalf("expected variable i to have type 'number', got %q", sym.Type)
	}
}

func TestLSPObjectLiteralAutocompleteInReturn(t *testing.T) {
	text := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number),`,
		`    ass: string`,
		`}`,
		`export fn test(): Reader {`,
		`    return {`,
		`        `,
		`    }`,
		`}`,
	}, "\n")

	pos := Position{Line: 6, Character: 8}

	completions := getCompletions("file:///test_autocomplete_return.tiny", text, pos)
	labels := completionLabels(completions)
	expected := []string{"read: ", "ass: "}
	for _, exp := range expected {
		if !completionLabelsContain(completions, exp) {
			t.Fatalf("expected completions to contain %q, got %#v", exp, labels)
		}
	}
}

func TestLSPObjectLiteralFieldHoverInReturn(t *testing.T) {
	line := `        read: fn(i) {`
	text := strings.Join([]string{
		`export interface Reader {`,
		`    read: function(number),`,
		`    ass: string`,
		`}`,
		`export fn test(): Reader {`,
		`    return {`,
		line,
		`            return i + 1`,
		`        },`,
		`        ass: "ok"`,
		`    }`,
		`}`,
	}, "\n")

	result := getHover("file:///test_object_field_hover_return.tiny", text, Position{
		Line:      6,
		Character: strings.Index(line, "read") + len("re"),
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for returned object field, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "Reader.read") || !strings.Contains(hover.Contents.Value, "function(number)") {
		t.Fatalf("unexpected hover content: %q", hover.Contents.Value)
	}
}

func TestLSPSemanticTokensFilterStrings(t *testing.T) {
	text := strings.Join([]string{
		"",
		"	const source = \"",
		"	import std \\\"io\\\" as io",
		"	fn greet(name) {",
		"		io.println('Hello, ' + name);",
		"	}",
		"	\";",
		"",
		"	const interpolated = `hello ${name + 42}!`;",
	}, "\n")

	tokens := collectSemanticTokens("file:///test.tiny", text)

	foundName := false
	found42 := false

	for _, tok := range tokens {
		// Tokens inside the multiline double-quoted string (lines 2 to 5) should not be reported
		if tok.Line >= 2 && tok.Line <= 5 {
			t.Errorf("Unexpected semantic token inside string at line %d (Start: %d, Type: %s)", tok.Line, tok.Start, tok.Type)
		}

		// Verify that "name" and "42" inside the template interpolation (line 8) ARE collected
		if tok.Line == 8 {
			if tok.Type == "variable" && strings.Contains(text[lineOffsetForTest(text, 8)+tok.Start:lineOffsetForTest(text, 8)+tok.End], "name") {
				foundName = true
			}
			if tok.Type == "number" && strings.Contains(text[lineOffsetForTest(text, 8)+tok.Start:lineOffsetForTest(text, 8)+tok.End], "42") {
				found42 = true
			}
		}
	}

	if !foundName {
		t.Errorf("expected to find variable 'name' inside interpolation, but it was not found. Tokens: %#v", tokens)
	}
	if !found42 {
		t.Errorf("expected to find number '42' inside interpolation, but it was not found. Tokens: %#v", tokens)
	}
}

func TestLSPSemanticTokensSoftKeywordsContext(t *testing.T) {
	text := strings.Join([]string{
		`const embed = fn(v) { return v }`,
		`embed(1)`,
		`const match = fn(v) { return v }`,
		`match(1)`,
		`match 1 {`,
		`    1 {}`,
		`}`,
		`class Box {`,
		`    embed logger`,
		`}`,
	}, "\n")

	tokens := collectSemanticTokens("file:///soft_keywords.tiny", text)
	tokenTypeAt := func(line int, word string) string {
		lineText := getLine(text, line)
		start := strings.Index(lineText, word)
		if start < 0 {
			t.Fatalf("word %q not found on line %d", word, line)
		}
		end := start + len(word)
		for _, tok := range tokens {
			if tok.Line == line && tok.Start == start && tok.End == end {
				return tok.Type
			}
		}
		return ""
	}

	if got := tokenTypeAt(0, "embed"); got != "variable" {
		t.Fatalf("expected const embed to be variable, got %q", got)
	}
	if got := tokenTypeAt(1, "embed"); got != "function" {
		t.Fatalf("expected embed() to be function, got %q", got)
	}
	if got := tokenTypeAt(2, "match"); got != "variable" {
		t.Fatalf("expected const match to be variable, got %q", got)
	}
	if got := tokenTypeAt(3, "match"); got != "function" {
		t.Fatalf("expected match() to be function, got %q", got)
	}
	if got := tokenTypeAt(4, "match"); got != "keyword" {
		t.Fatalf("expected match statement to be keyword, got %q", got)
	}
	if got := tokenTypeAt(8, "embed"); got != "keyword" {
		t.Fatalf("expected class embed declaration to be keyword, got %q", got)
	}
}

func TestLSPSoftKeywordCallsReportUndefinedWhenUndeclared(t *testing.T) {
	text := strings.Join([]string{
		`import std "io" as io`,
		`embed()`,
		`match()`,
		`field()`,
		`native()`,
		`external()`,
		`private()`,
		`public()`,
		`iota()`,
		`implements()`,
		`extends()`,
	}, "\n")
	diagnostics := semanticDiagnostics("file:///soft_keyword_undefined.tiny", text)
	for _, kw := range []string{"embed", "match", "field", "native", "external", "private", "public", "iota", "implements", "extends"} {
		if !diagnosticsContain(diagnostics, "undefined variable: "+kw) {
			t.Fatalf("expected undefined %s diagnostic, got %#v", kw, diagnostics)
		}
	}
}

func TestLSPCrossFileCallbackParamDottedTypeResolution(t *testing.T) {
	dir := t.TempDir()
	messagePath := filepath.Join(dir, "message.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(messagePath, []byte(strings.Join([]string{
		`export class Message {`,
		`    field content = ""`,
		`    field author: object`,
		`    fn reply(textContent: string) {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "message.tiny" as MessageModule`,
		``,
		`export class Client {`,
		`    fn onMessage(handler: function(MessageModule.Message, Client)) {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	mainText := strings.Join([]string{
		`import "gateway.tiny" as Discord`,
		`import "message.tiny" as Message`,
		``,
		`const bot = Discord.Client()`,
		``,
		`bot.onMessage(fn(msg, client) {`,
		`    msg.content`,
		`    msg.author`,
		`})`,
	}, "\n")

	uri := pathToFileURI(mainPath)
	diagnostics := semanticDiagnostics(uri, mainText)
	if diagnosticsContain(diagnostics, "undefined method or property: content") {
		t.Fatalf("expected msg.content to resolve across files, got diagnostics: %#v", diagnostics)
	}
	if diagnosticsContain(diagnostics, "undefined method or property: author") {
		t.Fatalf("expected msg.author to resolve across files, got diagnostics: %#v", diagnostics)
	}
}

func TestLSPCrossFileCallbackParamCompletions(t *testing.T) {
	dir := t.TempDir()
	messagePath := filepath.Join(dir, "message.tiny")
	gatewayPath := filepath.Join(dir, "gateway.tiny")
	mainPath := filepath.Join(dir, "main.tiny")

	err := os.WriteFile(messagePath, []byte(strings.Join([]string{
		`export class Message {`,
		`    field content = ""`,
		`    field author: object`,
		`    fn reply(textContent: string) {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(gatewayPath, []byte(strings.Join([]string{
		`import "message.tiny" as MessageModule`,
		``,
		`export class Client {`,
		`    fn onMessage(handler: function(MessageModule.Message, Client)) {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	mainText := strings.Join([]string{
		`import "gateway.tiny" as Discord`,
		`import "message.tiny" as Message`,
		``,
		`const bot = Discord.Client()`,
		``,
		`bot.onMessage(fn(msg, client) {`,
		`    msg.`,
		`})`,
	}, "\n")

	uri := pathToFileURI(mainPath)
	items := getCompletions(uri, mainText, Position{
		Line:      6,
		Character: len("    msg."),
	})

	labels := completionLabels(items)
	t.Logf("msg. completions: %#v", labels)

	if !completionLabelsContain(items, "content") {
		t.Fatalf("expected msg. completions to include 'content', got %#v", labels)
	}
	if !completionLabelsContain(items, "author") {
		t.Fatalf("expected msg. completions to include 'author', got %#v", labels)
	}
	if !completionLabelsContain(items, "reply") {
		t.Fatalf("expected msg. completions to include 'reply', got %#v", labels)
	}
}

func TestLSPCrossFileCallbackParamCompletionsRealProject(t *testing.T) {
	dir := t.TempDir()

	err := os.WriteFile(filepath.Join(dir, "constants.tiny"), []byte(strings.Join([]string{
		`export const GATEWAY_URL = "wss://gateway.discord.gg/"`,
		`export enum GatewayOpcode {`,
		`    Dispatch = 0,`,
		`    Heartbeat = 1,`,
		`    Identify = 2,`,
		`    PresenceUpdate = 3,`,
		`    Resume = 6,`,
		`    Reconnect = 7,`,
		`    Hello = 10,`,
		`    HeartbeatAck = 11`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(dir, "rest.tiny"), []byte(strings.Join([]string{
		`import "constants.tiny" as Constants`,
		`export fn request(token: string, method: string, route: string, body: any): any { return null }`,
		`export fn get(token: string, route: string): any { return null }`,
		`export fn post(token: string, route: string, body: any): any { return null }`,
		`export fn patch(token: string, route: string, body: any): any { return null }`,
		`export fn put(token: string, route: string, body: any): any { return null }`,
		`export fn delete(token: string, route: string): any { return null }`,
		`export fn sendMessage(token: string, channelId: string, payload: object): any { return null }`,
		`export fn editMessage(token: string, channelId: string, messageId: string, payload: object): any { return null }`,
		`export fn deleteMessage(token: string, channelId: string, messageId: string): any { return null }`,
		`export fn addReaction(token: string, channelId: string, messageId: string, emoji: string): any { return null }`,
		`export fn removeOwnReaction(token: string, channelId: string, messageId: string, emoji: string): any { return null }`,
		`export fn triggerTyping(token: string, channelId: string): any { return null }`,
		`export fn pinMessage(token: string, channelId: string, messageId: string): any { return null }`,
		`export fn unpinMessage(token: string, channelId: string, messageId: string): any { return null }`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(dir, "message.tiny"), []byte(strings.Join([]string{
		`export class Message {`,
		`    field id = ""`,
		`    field content = ""`,
		`    field author: object`,
		`    field client: any`,
		`    fn init(data: object, client: any) {`,
		`        this.id = data.id`,
		`        this.content = data.content`,
		`        this.author = data.author`,
		`        this.client = client`,
		`    }`,
		`    fn reply(textContent: string) {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	err = os.WriteFile(filepath.Join(dir, "gateway.tiny"), []byte(strings.Join([]string{
		`import "constants.tiny" as Constants`,
		`import "rest.tiny" as Rest`,
		`import "message.tiny" as MessageModule`,
		``,
		`export interface BotConfig {`,
		`    token: string,`,
		`    logging?: bool`,
		`}`,
		``,
		`export interface User {`,
		`    id: string,`,
		`    username: string,`,
		`    bot?: bool`,
		`}`,
		``,
		`export class Client {`,
		`    field token = ""`,
		`    field logging = false`,
		`    field readyCallback = null`,
		`    field messageCallback = null`,
		`    fn init(config: BotConfig) {}`,
		`    fn onReady(handler: function) { this.readyCallback = handler }`,
		`    fn onMessage(handler: function(MessageModule.Message, Client)) { this.messageCallback = handler }`,
		`    fn start() {}`,
		`}`,
	}, "\n")), 0644)
	if err != nil {
		t.Fatal(err)
	}

	mainText := strings.Join([]string{
		`import "gateway.tiny" as Discord`,
		`import "message.tiny" as Message`,
		``,
		`const bot = Discord.Client({`,
		`    token: "test",`,
		`    logging: true`,
		`})`,
		``,
		`bot.onReady(fn(user) {`,
		`    io.println("ready")`,
		`})`,
		``,
		`bot.onMessage(fn(msg, client) {`,
		`    msg.`,
		`})`,
	}, "\n")

	uri := pathToFileURI(filepath.Join(dir, "main.tiny"))
	items := getCompletions(uri, mainText, Position{
		Line:      13,
		Character: len("    msg."),
	})

	labels := completionLabels(items)
	t.Logf("msg. completions: %#v", labels)

	if !completionLabelsContain(items, "content") {
		t.Fatalf("expected msg. completions to include 'content', got %#v", labels)
	}
	if !completionLabelsContain(items, "author") {
		t.Fatalf("expected msg. completions to include 'author', got %#v", labels)
	}
	if !completionLabelsContain(items, "reply") {
		t.Fatalf("expected msg. completions to include 'reply', got %#v", labels)
	}
}

func lineOffsetForTest(text string, lineIndex int) int {
	lines := strings.Split(text, "\n")
	offset := 0
	for i := 0; i < lineIndex; i++ {
		offset += len(lines[i]) + 1
	}
	return offset
}

func TestLSPVariableHoverUsesTinyDeclarationMarkdown(t *testing.T) {
	text := strings.Join([]string{
		`const name = "Ada"`,
		`let count = 1`,
		`name`,
		`count`,
	}, "\n")

	constResult := getHover("file:///test_var_hover_decl.tiny", text, Position{Line: 2, Character: len(`na`)})
	constHover, ok := constResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for const variable, got %#v", constResult)
	}
	if !strings.Contains(constHover.Contents.Value, "```tiny\nconst name: string\n```") {
		t.Fatalf("expected const hover to use Tiny declaration markdown, got %q", constHover.Contents.Value)
	}

	letResult := getHover("file:///test_var_hover_decl.tiny", text, Position{Line: 3, Character: len(`co`)})
	letHover, ok := letResult.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for let variable, got %#v", letResult)
	}
	if !strings.Contains(letHover.Contents.Value, "```tiny\nlet count: number\n```") {
		t.Fatalf("expected let hover to use Tiny declaration markdown, got %q", letHover.Contents.Value)
	}
}

func TestLSPParameterHoverUsesTinyDeclarationMarkdown(t *testing.T) {
	text := strings.Join([]string{
		`fn greet(name: string) {`,
		`    name`,
		`}`,
	}, "\n")

	result := getHover("file:///test_param_hover_decl.tiny", text, Position{Line: 1, Character: len(`    na`)})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover result for parameter, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "```tiny\nname: string\n```") {
		t.Fatalf("expected parameter hover to use Tiny declaration markdown, got %q", hover.Contents.Value)
	}
	if strings.Contains(hover.Contents.Value, "Type: `") {
		t.Fatalf("expected parameter hover not to use Type line, got %q", hover.Contents.Value)
	}
}

func TestLSPStructuralTypeAnnotations(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
		``,
		`let getAge = (person: {name: string, age: number}) => person.age`,
	}, "\n")

	diagnostics := semanticDiagnostics("file:///test_struct.tiny", text)
	if diagnosticsContain(diagnostics, "unknown type") {
		t.Fatalf("should not get 'unknown type' for structural type annotations, got: %#v", diagnostics)
	}
}

func TestLSPStructuralTypeMemberCompletion(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    person.`,
		`}`,
	}, "\n")

	uri := "file:///test_struct_member.tiny"
	items := getCompletions(uri, text, Position{Line: 1, Character: 11})
	labels := completionLabels(items)
	if !completionLabelsContain(items, "name") {
		t.Fatalf("expected person. completions to include 'name', got %#v", labels)
	}
	if !completionLabelsContain(items, "age") {
		t.Fatalf("expected person. completions to include 'age', got %#v", labels)
	}
}

func TestLSPArrowFnParamCompletion(t *testing.T) {
	text := strings.Join([]string{
		`let getAge = (person: {name: string, age: number}) => person.`,
	}, "\n")

	uri := "file:///test_arrow_param.tiny"
	items := getCompletions(uri, text, Position{Line: 0, Character: 61})
	labels := completionLabels(items)
	if !completionLabelsContain(items, "name") {
		t.Fatalf("expected arrow param completions to include 'name', got %#v", labels)
	}
	if !completionLabelsContain(items, "age") {
		t.Fatalf("expected arrow param completions to include 'age', got %#v", labels)
	}
}

func TestLSPArrowFnParamHover(t *testing.T) {
	text := strings.Join([]string{
		`let getAge = (person: {name: string, age: number}) => person.age`,
	}, "\n")

	uri := "file:///test_arrow_hover.tiny"
	result := getHover(uri, text, Position{Line: 0, Character: 55})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for person.age in arrow fn, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "number") {
		t.Fatalf("expected hover to show 'number' type, got %q", hover.Contents.Value)
	}
}

func TestLSPStructuralPropertyHoverUsesTinyDeclarationMarkdown(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
	}, "\n")

	result := getHover("file:///test_struct_property_hover.tiny", text, Position{Line: 2, Character: len(`    io.println(person.ag`)})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for person.age, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "```tiny\nperson.age: number\n```") {
		t.Fatalf("expected structural property hover to use Tiny declaration markdown, got %q", hover.Contents.Value)
	}
	if strings.Contains(hover.Contents.Value, "Type: `") {
		t.Fatalf("expected structural property hover not to use Type line, got %q", hover.Contents.Value)
	}
}

func TestLSPDefinitionOnStructuralTypeFieldAccess(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
	}, "\n")

	nameResult := getDefinition("file:///test_struct_field_def.tiny", text, Position{Line: 1, Character: strings.Index(getLine(text, 1), "name") + 1})
	nameLoc, ok := nameResult.(Location)
	if !ok {
		t.Fatalf("expected definition for person.name, got %#v", nameResult)
	}
	if nameLoc.Range.Start.Line != 0 || nameLoc.Range.Start.Character != strings.Index(getLine(text, 0), "name") {
		t.Fatalf("expected person.name definition to point at structural field, got %#v", nameLoc)
	}

	ageResult := getDefinition("file:///test_struct_field_def.tiny", text, Position{Line: 2, Character: strings.Index(getLine(text, 2), "age") + 1})
	ageLoc, ok := ageResult.(Location)
	if !ok {
		t.Fatalf("expected definition for person.age, got %#v", ageResult)
	}
	if ageLoc.Range.Start.Line != 0 || ageLoc.Range.Start.Character != strings.Index(getLine(text, 0), "age") {
		t.Fatalf("expected person.age definition to point at structural field, got %#v", ageLoc)
	}
}

func TestLSPDefinitionOnInterfaceFieldAccess(t *testing.T) {
	text := strings.Join([]string{
		`interface Person {`,
		`    name: string,`,
		`    age: number`,
		`}`,
		`fn printName(person: Person) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
	}, "\n")

	nameResult := getDefinition("file:///test_interface_field_def.tiny", text, Position{Line: 5, Character: strings.Index(getLine(text, 5), "name") + 1})
	nameLoc, ok := nameResult.(Location)
	if !ok {
		t.Fatalf("expected definition for person.name, got %#v", nameResult)
	}
	if nameLoc.Range.Start.Line != 1 || nameLoc.Range.Start.Character != strings.Index(getLine(text, 1), "name") {
		t.Fatalf("expected person.name definition to point at interface field, got %#v", nameLoc)
	}

	ageResult := getDefinition("file:///test_interface_field_def.tiny", text, Position{Line: 6, Character: strings.Index(getLine(text, 6), "age") + 1})
	ageLoc, ok := ageResult.(Location)
	if !ok {
		t.Fatalf("expected definition for person.age, got %#v", ageResult)
	}
	if ageLoc.Range.Start.Line != 2 || ageLoc.Range.Start.Character != strings.Index(getLine(text, 2), "age") {
		t.Fatalf("expected person.age definition to point at interface field, got %#v", ageLoc)
	}
}

func TestLSPArrowFunctionExpressionBodyReturnInference(t *testing.T) {
	text := `let getAge = (person: {name: any, age: number}) => person.age`

	result := getHover("file:///test_arrow_return_inference.tiny", text, Position{Line: 0, Character: len(`let get`)})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for getAge, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "getAge(person: {age: number, name: any}): number") {
		t.Fatalf("expected arrow function hover to infer number return, got %q", hover.Contents.Value)
	}
}

func TestLSPEntityLiteralCompletionWithStructuralType(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    io.println(person.name)`,
		`}`,
		`printName({})`,
	}, "\n")

	uri := "file:///test_obj_completion.tiny"
	items := getCompletions(uri, text, Position{Line: 3, Character: 11})
	labels := completionLabels(items)
	if !completionLabelsContain(items, "name: ") {
		t.Fatalf("expected {} completions to include 'name: ', got %#v", labels)
	}
	if !completionLabelsContain(items, "age: ") {
		t.Fatalf("expected {} completions to include 'age: ', got %#v", labels)
	}
}

func TestLSPEntityLiteralPartialKeyCompletion(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: {name: string, age: number}) {`,
		`    io.println(person.name)`,
		`}`,
		`printName({ag})`,
	}, "\n")

	uri := "file:///test_obj_partial.tiny"
	items := getCompletions(uri, text, Position{Line: 3, Character: 13})
	labels := completionLabels(items)
	if !completionLabelsContain(items, "age: ") {
		t.Fatalf("expected {ag} completions to include 'age: ', got %#v", labels)
	}
}

func TestLSFScopeDoesNotLeakFunctionParams(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: { name: string, age: number }) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
		`let person = 42`,
		`person.age`,
	}, "\n")

	// Hovering over 'person' outside the function should NOT show the function param type
	hoverPerson := getHover("file:///test_scope_leak.tiny", text, Position{Line: 5, Character: len("person")})
	if hoverPerson == nil {
		t.Fatal("expected hover result for person outside function")
	}
	if h, ok := hoverPerson.(HoverResult); ok {
		if strings.Contains(h.Contents.Value, "{ name: string, age: number }") {
			t.Fatalf("hover on 'person' outside function should NOT show function param type, got %q", h.Contents.Value)
		}
		if !strings.Contains(h.Contents.Value, "number") {
			t.Fatalf("hover on 'person' outside function should show 'number' type from let person = 42, got %q", h.Contents.Value)
		}
	}
}

func TestLSFScopeDoesNotLeakFunctionParamsCompletion(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: { name: string, age: number }) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
		`let person = { fullName: "outer", score: 99 }`,
		`person.`,
	}, "\n")

	items := getCompletions("file:///test_scope_leak.tiny", text, Position{Line: 5, Character: len("person.")})

	for _, item := range items {
		if item.Label == "name" || item.Label == "age" {
			t.Fatalf("completion outside function should NOT include function param fields, got %q", item.Label)
		}
	}
	if !completionLabelsContain(items, "fullName") {
		t.Fatalf("completion outside function should include outer person's fields, got %v", completionLabels(items))
	}
}

func TestLSPDefinitionDoesNotLeakFunctionParam(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(person: { name: string, age: number }) {`,
		`    io.println(person.name)`,
		`    io.println(person.age)`,
		`}`,
		``,
		`person.age`,
	}, "\n")

	// Go-to-definition on 'age' in person.age outside the function should NOT
	// navigate to the function parameter 'person' in printName.
	result := getDefinition("file:///test_def_scope_leak.tiny", text, Position{Line: 5, Character: strings.Index(getLine(text, 5), "age") + 1})

	if result == nil {
		// nil is acceptable — it means no definition found, which is correct
		// because 'person' isn't defined at top level in this test.
		return
	}

	if loc, ok := result.(Location); ok {
		// The function param 'person' is on line 0. If definition goes there, it leaked.
		if loc.Range.Start.Line == 0 {
			t.Fatalf("go-to-definition on 'age' outside function should NOT point to line 0 (function param), got line %d", loc.Range.Start.Line)
		}
	}
}

func TestLSPMultilineMethodChainHover(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(): string {`,
		`    return "hello"`,
		`}`,
		`printName().`,
		`    split("").`,
		`    length()`,
	}, "\n")

	result := getHover("file:///multiline_chain.tiny", text, Position{
		Line:      5,
		Character: 7,
	})
	hover, ok := result.(HoverResult)
	if !ok {
		t.Fatalf("expected hover for length in multiline chain, got %#v", result)
	}
	if !strings.Contains(hover.Contents.Value, "length") && !strings.Contains(hover.Contents.Value, "number") {
		t.Fatalf("expected hover to show length/number info, got %q", hover.Contents.Value)
	}
}

func TestLSPMultilineMethodChainCompletion(t *testing.T) {
	text := strings.Join([]string{
		`fn printName(): string {`,
		`    return "hello"`,
		`}`,
		`printName().`,
		`    split("").`,
		`    `,
	}, "\n")

	items := getCompletions("file:///multiline_chain_comp.tiny", text, Position{
		Line:      5,
		Character: 4,
	})
	hasLength := false
	for _, item := range items {
		if item.Label == "length" {
			hasLength = true
			break
		}
	}
	if !hasLength {
		t.Fatalf("expected completion to include 'length' for string method chain, got %v", completionLabels(items))
	}
}

func symbolFieldNames(fields map[string]SymbolInfo) []string {
	var names []string
	for k := range fields {
		names = append(names, k)
	}
	return names
}

func TestLSPThisFieldGoToDefinition(t *testing.T) {
	text := strings.Join([]string{
		"class Parser {",
		"    field currentToken: any",
		"",
		"    fn parse() {",
		"        this.currentToken",
		"    }",
		"}",
	}, "\n")

	uri := "file:///this_field_def.tiny"
	result := getDefinition(uri, text, Position{Line: 4, Character: 14})
	if result == nil {
		t.Fatalf("expected go-to-definition for this.currentToken to resolve")
	}
	loc, ok := result.(Location)
	if !ok {
		t.Fatalf("expected Location, got %#v", result)
	}
	if loc.URI == "" {
		t.Fatalf("expected definition location with URI")
	}
}

func TestLSPThisFieldGoToDefinitionAcrossFiles(t *testing.T) {
	text := strings.Join([]string{
		"class Token {",
		"    field type: string",
		"    field value: any",
		"}",
		"class Parser {",
		"    field currentToken: Token",
		"",
		"    fn parse() {",
		"        this.currentToken.value",
		"    }",
		"}",
	}, "\n")

	uri := "file:///this_field_cross.tiny"
	scope := scopeAtPosition(uri, text, Position{Line: 9, Character: 14})

	// Check what class:Token resolves to
	classSym, classOk := resolveClassSymbol(scope, "Token")
	t.Logf("resolveClassSymbol('Token'): ok=%v fields=%v", classOk, symbolFieldNames(classSym.Fields))
	if valField, exists := classSym.Fields["value"]; exists {
		t.Logf("  value field: Type=%q Detail=%q", valField.Type, valField.Detail)
	}

	// Resolve via receiver path
	_, typ, ok := resolveReceiverPath(scope, text, Position{Line: 9, Character: 14}, "this.currentToken")
	if !ok {
		t.Fatalf("expected to resolve this.currentToken via receiver path")
	}
	t.Logf("this.currentToken type: %q", typ)

	// Resolve this.currentToken.value
	fieldSym, memberType, memberOk := resolveMemberFromStaticType(scope, typ, "value")
	t.Logf("this.currentToken.value type: %q ok=%v sym=%v", memberType, memberOk, fieldSym)
	if memberType == "null" {
		t.Fatalf("expected this.currentToken.value to resolve to 'any', got 'null'")
	}
	if memberType != "any" {
		t.Fatalf("expected this.currentToken.value type to be 'any', got %q", memberType)
	}
}
