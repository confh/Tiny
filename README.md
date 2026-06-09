<div align="center">
  <img src="examples/tiny.png" alt="Tiny Logo" width="200">
  <h1>Tiny Programming Language</h1>
  <p><b>A high-performance, concurrent bytecode virtual machine and language written in Go.</b></p>
  <p>Tiny combines the development speed of dynamic coding with a robust, multi-threaded runtime engine.</p>

  <p>
    <img src="https://img.shields.io/badge/Language-Tiny-blue.svg">
    <img src="https://img.shields.io/badge/Built%20With-Go-00ADD8.svg">
    <img src="https://img.shields.io/badge/VS%20Code-Extension-007ACC.svg">
    <img src="https://img.shields.io/badge/License-MIT-green.svg">
  </p>
</div>

***

Tiny is a concurrent programming language and runtime system. It compiles source files into compact, stack-based bytecode instructions (`.tbc`) which run on a highly optimized, register-less virtual machine written in Go.

The runtime engine features direct OS-level parallel threading, native WebAssembly compilation for inline Go extensions, zero-copy matrix linear algebra, a built-in Language Server (LSP), and standalone binary distribution packaging.

Read the full documentation at [tiny-lang-docs.github.io](https://tiny-lang-docs.github.io/), or check out the [examples](https://github.com/confh/Tiny/tree/master/examples) to see Tiny in action.

---

## Installation

Precompiled binaries are available on the release page:

- Windows: `tiny_windows_amd64.exe`
- Linux: `tiny_linux_amd64`
- macOS (Apple Silicon): `tiny_darwin_arm64`

To install:

1. Download the binary for your operating system.
2. Rename the file to `tiny` (or `tiny.exe` on Windows).
3. Move the binary into a directory in your system path (for example, `~/.tiny/bin` on Unix or `%USERPROFILE%\.tiny` on Windows).
4. Add that directory to your system `PATH` environment variable.

For compilation from source instructions, see the online documentation.

***

<p align="center">
  <img src="examples/showcase.gif" alt="Tiny Showcase">
</p>

***

## Language Specifications

### Dynamic Typing with Optional Hints

Tiny is dynamically typed by default. You can write untyped code for rapid prototyping, or apply optional static type hints to variables, parameters, and function returns.

```js
import std "io";

// Untyped variable
let data = "untyped string";

// Explicitly typed variable
const port: number = 8080;

// Typed function parameters and return type
fn calculatePayout(base: number, multiplier: number): number {
    return base * multiplier;
}

io.println(calculatePayout(100, 1.5));
```

### Structural Interfaces and Shape Validation

Tiny uses structural typing (shape-based validation). Objects are validated against interfaces at runtime based entirely on their properties and methods, rather than explicit inheritance declarations.

```js
import std "io";

interface Task {
    title: string
    done: bool
}

fn printTask(t: Task) {
    let status = t.done ? "Completed" : "Pending";
    io.println(`${t.title} - Status: ${status}`);
}

// Valid structural matches
printTask({ title: "Write Compiler Tests", done: true });
printTask({ title: "Optimize VM Dispatcher", done: false, priority: 1 });
```

### Class Composition and Embedding

Tiny does not use deep class inheritance hierarchies. Instead, it supports composition via the `embed` keyword. When a class embeds another class instance, any methods and fields from the embedded instance are delegated automatically if they are not defined on the parent class.

```js
import std "io";
import std "json";

class Logger {
    field messages = []

    fn log(message: string) {
        this.messages.push(message);
        io.println(`Log: ${message}`);
    }

    fn dump() {
        return this.messages;
    }
}

class SessionManager {
    field active = true
    embed logger

    fn init() {
        this.active = true;
        this.logger = Logger();
        // Call to embedded class method
        this.log("Session manager initialized");
    }

    fn close() {
        this.active = false;
        this.log("Session closed");
    }
}

let session = SessionManager();
session.close();

// Directly calls the embedded Logger.dump method
io.println(json.pretty(session.dump()));
```

### Pattern Matching

The `match` block provides a clean syntax for branch dispatching, supporting literal values, variable variables, enums, and a default fallback case (`_`).

```js
import std "io";

enum Status {
    Idle,
    Running,
    Failed
}

let current = Status.Running;

match current {
    "Idle" {
        io.println("System is idling");
    }
    "Running" {
        io.println("System is actively running");
    }
    _ {
        io.println("System encountered an error or unknown state");
    }
}
```

### Scoped Cleanups with Defer

The `defer` statement schedules a function call to execute immediately before the current surrounding function scope exits. This ensures that resource cleanups, file closures, and synchronization locks are released regardless of early returns or errors.

```js
import std "fs";
import std "io";

fn processFile(path: string) {
    io.println("Opening file stream...");
    let file = fs.open(path);

    defer fn() {
        io.println("Running defer block: closing file stream.");
        // Cleanup logic runs here
    }

    io.println("Processing file data...");
}

processFile("README.md");
```

### Loop Unpacking

The `for` loop supports unpacking both elements and their indices directly when iterating over collections.

```js
import std "io";

const names = ["Alice", "Bob", "Charlie"];

for name, index in names {
    io.println(`Index ${index}: ${name}`);
}
```

***

## Concurrency Model

### Parallel Thread Execution

Tiny executes parallel operations by using OS-level multi-threading backed by the Go scheduler. The `spawn` keyword starts a new execution routine running on an independent, isolated VM state space containing its own call frame stack.

Unlike event-loop models or runtimes with a Global Interpreter Lock (GIL), Tiny runs tasks concurrently across all available CPU cores.

```js
import std "io";
import std "time";

let worker = spawn () fn() {
    time.sleep(1000);
    return "Worker thread complete";
};

io.println("Main thread proceeding...");
let result = await worker;
io.println(result);
```

### Thread Safety and Mutex Locking

Shared memory operations can be coordinated using mutexes and native `lock` blocks. The compiler guarantees that the mutex is automatically released when execution leaves the lock block scope, preventing common deadlock mistakes.

```js
import std "io";
import std "sync";

let counter = 0;
const m = sync.mutex();

fn increment() {
    lock m {
        counter = counter + 1;
    }
}
```

***

## Inline Go Extensions (WebAssembly)

For performance-critical code segments, Tiny allows you to write Go logic directly in the source file using the `native fn` keyword. During compilation, the compiler extracts these blocks and compiles them to WebAssembly bytecode via TinyGo, loading them at runtime for near-native execution speed.

```js
import std "io";
import std "time";

native fn calculateSha256(input: string): string {
    go {
        import "crypto/sha256"
        import "encoding/hex"

        h := sha256.Sum256([]byte(input))
        return hex.EncodeToString(h[:])
    }
}

native fn computeFibonacci(n: number): number {
    go {
        if n < 2 {
            return n
        }
        return computeFibonacci(n - 1) + computeFibonacci(n - 2)
    }
}

const text = "Tiny runtime speed";
io.println(`SHA256: ${calculateSha256(text)}`);

const start = time.nowMs();
const fibResult = computeFibonacci(30);
const duration = time.nowMs() - start;

io.println(`Fibonacci(30) = ${fibResult} (calculated in ${duration}ms)`);
```

> **Note**: Utilizing `native fn` Go extensions requires Go and TinyGo installations on the host system.

***

## Standard Library Reference

Tiny includes a CGO-free standard library designed for networking, UI creation, scripting, and calculations.

### `http` (High-Throughput Web Services)

The `http` module contains a fully concurrent web server and client. The server uses a multiplexed routing engine that can process more than 45,000 requests per second.

```js
import std "http";
import std "io";

let server = http.server(8080);

server.get("/", fn(req) {
    return http.json({
        status: "online",
        system: "Tiny VM"
    });
});

server.get("/users/:id", fn(req) {
    const userId: string = req.params["id"];
    return http.json({
        id: userId,
        query: req.query
    });
});

io.println("Web server listening on port 8080");
server.start();
```

### `ui` (WebView Desktop Applications)

The `ui` module provides a Webview container to construct lightweight desktop applications using HTML, CSS, and JavaScript, while binding underlying system logic to Tiny functions.

```js
import std "ui";
import std "io";

// Embed index.html and its directory assets
embeddir "./ui" const assets

let clicks = 0;
const win = ui.new(true); // Argument enables developer tools

win.setTitle("Tiny Desktop UI");
win.setSize(500, 400);

// Bind a function accessible from JavaScript
win.callback("registerClick", fn(args) {
    clicks = clicks + 1;
    io.println(`Clicks registered: ${clicks}`);
    return clicks;
});

// Load embedded HTML content
win.setHtml(assets["index.html"]);
win.run();
```

Inside `ui/index.html`, invoke the bound function using standard JavaScript promises:

```html
<script>
    async function handleClick() {
        const count = await window.registerClick("");
        document.getElementById("counter").innerText = count;
    }
</script>
```

### `desktop` (OS Automation)

The `desktop` module wraps native OS level interfaces for automating keyboard inputs, mouse actions, screen parsing, and clipboard interactions.

```js
import std "desktop";
import std "time";

// Move mouse smoothly to coordinates (800, 600) over 1 second
desktop.moveMouseSmooth(800, 600);
desktop.click();

// Simulate keyboard input
desktop.type("Tiny Automation");
desktop.hotKey("ctrl", "a");

// Screenshot and Clipboard operations
let clipboardText = desktop.getClipboard();
desktop.setClipboard("New Clipboard Content");
```

### `math` (Linear Algebra and Matrix Operations)

The `math` module leverages Go's Gonum package for high-speed matrix computations. It utilizes unsafe zero-copy casting of raw `Buffer` binary payloads to perform memory-efficient linear algebra calculations.

```js
import std "io";
import std "math";

// Define two 2x2 matrices using flat data arrays inside a Buffer
const matrixA = {
    rows: 2,
    cols: 2,
    data: math.toFloat([1.0, 2.0, 3.0, 4.0]) // Floats packed into a buffer
};

const matrixB = {
    rows: 2,
    cols: 2,
    data: math.toFloat([5.0, 6.0, 7.0, 8.0])
};

// Perform high-speed matrix multiplication
let result = math.matMul(matrixA, matrixB);

io.println(`Product rows: ${result.rows}, cols: ${result.cols}`);
```

### `process` and `fs` (System and Files)

Run external commands, check execution status, pipes outputs, and manipulate files directly.

```js
import std "fs";
import std "io";
import std "process";

// Direct file access
if fs.exists("config.json") {
    let content = fs.readFile("config.json");
    io.println(content);
}

// Spawn and wait for a shell command, capturing standard output
let res = process.run("git", ["status"], { stdout: true });
if res.success {
    io.println("Git Status Output:");
    io.println(res.stdout);
}
```

***

## Tooling and Ecosystem

### Project Configuration (`tiny.json`)

Manage scripts, dependencies, targets, and compilation rules using a project-level `tiny.json` file.

```json
{
  "name": "my-tiny-project",
  "version": "1.0.0",
  "entry": "src/main.tiny",
  "outDir": "dist",
  "target": "windows-amd64",
  "scripts": {
    "start": "tiny run",
    "build": "tiny build",
    "pack": "tiny pack"
  },
  "dependencies": {
    "TinyDotEnv": {
      "source": "github:confh/TinyDotEnv",
      "version": "main"
    }
  },
  "plugins": [],
  "compilerOptions": {
    "stackTraces": true,
    "strict": true
  }
}
```

### Command Line Interface (CLI)

The `tiny` binary acts as a compiler, package manager, and execution runtime:

- **`tiny run <file>`**: Compiles and runs a `.tiny` or `.tbc` file. Direct execution of `.tiny` files utilizes `.tinycache` tracking to skip compilation if source files have not been modified.
- **`tiny build <file> -o <out>`**: Compiles a source file into a distribution bytecode file (`.tbc`).
- **`tiny pack <file> -o <binary>`**: Compiles and bundles the source bytecode alongside the VM runtime interpreter into a single, standalone native executable (\~9MB).
- **`tiny dist <file> -o <dir>`**: Packages the application alongside compiled external library plugins (`.dll`/`.so`).
- **`tiny init`**: Generates a default `tiny.json` config in the current directory.
- **`tiny add <owner/repo>`**: Downloads and registers a third-party library from GitHub into the global dependency cache.
- **`tiny install`**: Installs all libraries specified in the `tiny.json` manifest.
- **`tiny remove <owner/repo>`**: Deletes a library from the project dependencies.
- **`tiny task <name>`**: Runs a task script defined in the `scripts` section of `tiny.json`.
- **`tiny lsp`**: Starts the language server.

### [Built-in Language Server (LSP)](https://marketplace.visualstudio.com/items?itemName=Confis.tiny)

Tiny features a native Language Server Protocol implementation. Run `tiny lsp` to interface with editor plugins. The LSP provides:

- Semantic diagnostics and syntax error reporting.
- Auto-completion for variables, objects, standard library, and custom modules.
- Jump-to-definition and reference tracking.
- Automated code formatting.
- Type narrowing (refining variable types following flow control structures like `if` checks).

<p align="center">
  <img src="examples/extension.png" alt="VS Code Extension" width="500">
</p>

### Static Asset Embedding and XOR Obfuscation

You can embed static strings and binary files directly into compiled bytecode using `embedstr` and `embedbin`. Assets are compiled into the `.tbc` stream and automatically obfuscated with an XOR key. This prevents sensitive configurations, keys, or binary assets from being extracted using simple static analysis tools (like the Unix `strings` utility).

```js
import std "io";

embedstr "./api_key.txt" const apiKey
embedbin "./logo.png" const logoBytes

io.println(`API Key securely loaded: ${apiKey}`);
```

***

<div align="center">
  <p>Tiny Language © 2026 | MIT Licensed</p>
  <p>
    <a href="https://github.com/confh/Tiny/issues">Report an Issue</a> • 
    <a href="https://github.com/confh/Tiny/blob/main/LICENSE">License</a>
  </p>
</div>
