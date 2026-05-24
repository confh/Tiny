package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	if item.InsertText != "greet($0);" || item.InsertTextFormat != 2 {
		t.Fatalf("expected callable snippet insert text, got %#v", item)
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
	if item.InsertText != "Todo.TaskManager($0);" || item.InsertTextFormat != 2 {
		t.Fatalf("expected namespaced constructor snippet, got %#v", item)
	}
	if len(item.AdditionalTextEdits) != 1 || item.AdditionalTextEdits[0].NewText != "import \"todo.tiny\" as Todo;\n" {
		t.Fatalf("expected todo import edit, got %#v", item)
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
