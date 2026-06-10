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
		"parts":      "array:any",
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
		"parts":  "array:any",
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

func TestLSPInlayHintsInferVariableAndParameterNames(t *testing.T) {
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

	labels := []string{}
	for _, hint := range hints {
		labels = append(labels, hint.Label)
	}

	for _, want := range []string{": string", "name:", "excited:"} {
		found := false
		for _, label := range labels {
			if label == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("expected inlay hint %q in %#v", want, labels)
		}
	}
}

func TestLSPInlayHintsParameterNamesForMultilineMemberCall(t *testing.T) {
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

	labelsByLine := map[string]int{}
	for _, hint := range hints {
		labelsByLine[hint.Label] = hint.Position.Line
	}

	if labelsByLine["payload:"] != 6 {
		t.Fatalf("payload hint line = %d, want 6; hints %#v", labelsByLine["payload:"], hints)
	}
	if labelsByLine["ttl:"] != 8 {
		t.Fatalf("ttl hint line = %d, want 8; hints %#v", labelsByLine["ttl:"], hints)
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
	if parts.Type != "array:any" {
		t.Fatalf("parts type = %q, want array:any", parts.Type)
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
			if len(edits) == 0 || edits[0].NewText != ": array:any" {
				t.Fatalf("unexpected type hint action edits: %#v", edits)
			}
			return
		}
	}
	t.Fatalf("expected Add inferred type hint action, got %#v", actions)
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

func TestLSPFileAutoImportCompletion(t *testing.T) {
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
