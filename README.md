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

Tiny is a concurrent programming language and runtime system. It compiles source files into compact, stack-based bytecode instructions (`.tbc`) which run on a highly optimized virtual machine using slot-based local storage.

The runtime engine features a multi-tiered execution model: an efficient interpreter for general logic and a Just-In-Time (JIT) compiler for performance-critical code. Key features include direct OS-level parallel threading, host-mirrored packed arrays, a chainable schema validation library, native WebAssembly extensions, and a built-in Language Server (LSP).

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

Tiny is dynamically typed by default. You can write untyped code for rapid prototyping, or apply optional static type hints to variables, parameters, and function returns. The type system supports unions, generics, and inline structural types.

```ts
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

### Arrow Functions

Arrow functions provide concise single-expression and multi-parameter syntax. Typed parameters are supported.

```ts
import std "io";

const double = x => x * 2;
const add = (a: number, b: number) => a + b;
const greet = (name: string) => `Hello, ${name}`;

io.println(greet("Tiny"));
```

### Structural Interfaces and Shape Validation

Tiny uses structural typing (shape-based validation). Objects are validated against interfaces at runtime based on their properties and methods. The JIT engine optimizes these checks by tracking object shapes and utilizing linear memory field offsets.

Classes can explicitly declare which interfaces they implement using the `implements` keyword. The compiler verifies at build time that the class satisfies every field and method the interface requires.

```ts
import std "io";

interface Greeter {
    greet: function(string)
}

class Human implements Greeter {
    fn greet(name: string): string {
        return `Hello, ${name}`
    }
}

fn welcome(g: Greeter) {
    io.println(g.greet("world"));
}

welcome(Human());
```

Interfaces can extend other interfaces:

```ts
interface Base {
    id: number
}

interface User extends Base {
    name: string
}
```

Function parameters and return types support inline structural types directly:

```ts
fn process(input: { name: string, age: number }): { ok: bool } {
    return { ok: true }
}
```

The binary `in` expression tests whether a key exists in an object at runtime:

```ts
import std "io";

const config = { host: "localhost", port: 8080 };

if "host" in config {
    io.println(config["host"]);
}
```

### Destructuring Assignment

Tiny supports object and array destructuring for both `let` and `const` declarations. This includes support for nested patterns, default values, and property renaming.

```ts
import std "io";

const user = {
    name: "Alice",
    age: 30,
    address: { city: "NYC", zip: "10001" }
};

// Object destructuring with renaming and nesting
let { name, address: { city } } = user;
io.println(`${name} lives in ${city}`);

// Array destructuring
let coordinates = [10.5, 20.8, 30.0];
let [x, y] = coordinates;
io.println(`X: ${x}, Y: ${y}`);
```

### Class Composition and Embedding

Tiny emphasizes composition over deep inheritance. The `embed` keyword allows a class to delegate behavior to another class instance. If a method or field is missing on the parent, it is automatically resolved from the embedded instance.

```ts
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

The `match` block provides branch dispatching with support for literal values, variables, enums, union patterns, and guards. It is the primary way to extract data from enum variants.

```ts
import std "io";

enum Result {
    Ok(value),
    Error(message)
}

fn process(res: Result) {
    match res {
        Result.Ok(val) if val > 0 {
            io.println(`Success: ${val}`);
        }
        Result.Ok(val) {
            io.println("Success with zero or negative value");
        }
        Result.Error(msg) {
            io.println(`Error: ${msg}`);
        }
        _ {
            io.println("Unknown state");
        }
    }
}

process(Result.Ok(42));
```

### Scoped Cleanups with Defer

The `defer` statement schedules a function call to execute immediately before the current surrounding function scope exits, regardless of early returns or thrown errors.

```ts
import std "fs";
import std "io";

fn processFile(path: string) {
    io.println("Opening file stream...");
    let file = fs.open(path);

    defer fn() {
        io.println("Running defer block: closing file stream.");
        file.close();
    }

    io.println("Processing file data...");
}

processFile("README.md");
```

### Modules and External Declarations

Tiny supports standard modules, local file modules, GitHub-backed library imports, plugin imports, and explicit exports. Runtime child VMs can also bind host-provided constants and functions through `external` declarations.

```ts
import std "io";
import "math_helpers.tiny" as Math;
import lib "confh/TinyJWT" as Jwt;

export external const hostName: string
export external fn hostLog(message: string): string

io.println(hostName);
hostLog("called from Tiny");
```

### Asset Embedding

Compile-time embeds let source files, packaged tools, and desktop apps carry static assets without relying on loose runtime files.

```ts
import std "io";

embedtext "./data.json" const dataText
embedbytes "./data.json" const dataBytes
embedfolder "./ui" const assets

io.println(dataText);
io.println(assets["index.html"]);
```

***

## Concurrency Model

### Parallel Thread Execution

Tiny executes parallel operations using OS-level multi-threading. The `spawn` keyword starts a new execution routine on an isolated VM state space. Unlike event-loop models, Tiny runs tasks concurrently across all available CPU cores.

```ts
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

Shared state can be coordinated using mutexes and native `lock` blocks. The compiler guarantees that the mutex is automatically released when execution leaves the block, preventing deadlocks.

```ts
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

## Just-In-Time (JIT) Compilation

Tiny includes a multi-function JIT compilation engine that translates hot bytecode paths into native WebAssembly.

### Region Outlining
The compiler automatically identifies hot loops in top-level code and function bodies, outlining them into specialized JIT regions. This ensures that even scripts and timed benchmarks run at native speed without manual function encapsulation.

### Packed Object Arrays
For arrays containing objects of uniform shape, the JIT implements host-memory mirroring. It utilizes field-column pointer tables to access object properties directly in linear memory, bypassing the host-call overhead typically associated with VM-to-Native interop. **Packed arrays now support dynamic growth and Wasm-side optimization.**

### JIT-Safe Best Practices
The JIT automatically selects eligible functions. For maximum performance:
- **Avoid Closures with Captures**: Functions that close over mutable outer variables are executed by the interpreter.
- **Stay Synchronous**: `async` functions are not currently eligible for JIT compilation.
- **Type Hints**: Provide explicit hints (e.g., `: number`) to help the JIT generate specialized machine code.
- **Efficient Strings**: **String join operations are now JIT-accelerated.** For large builds, prefer `stringBuilder` from the standard library.

```ts
// Highly JIT-optimized: typed, synchronous, no captures, uses loops
fn computeSum(n: number, initial = 0): number {
    let total = initial;
    for let i = 0; i < n; i++ {
        total += i;
    }
    return total;
}
```

***

## Inline Go Extensions (WebAssembly)

For logic requiring specific Go packages, Tiny allows writing Go code directly in the source file using `native fn`. These blocks are compiled to WebAssembly via TinyGo and loaded at runtime.

```ts
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

const text = "Tiny runtime speed";
io.println(`SHA256: ${calculateSha256(text)}`);
```

***

## Standard Library Reference

Tiny ships with modules for command-line tools, servers, desktop apps, automation, testing, networking, data validation, and runtime embedding.

### `validate` (Schema Validation)
A chainable API for defining and enforcing data schemas. Supports objects, arrays, unions, transformations, and runtime interface validation.

```ts
import std "validate";
import std "io";

const userSchema = validate.object({
    username: validate.string().trim().nonempty().min(3).required(),
    age: validate.number().int().positive().default(18),
    tags: validate.array(validate.string()).default([])
});

const result = userSchema.safeParse({ username: "  alice  " });
if result.success {
    io.println(result.data.username); // "alice"
}

interface User {
    id: number
    name: string
}

// Runtime interface validation
const user = { id: 1, name: "Alice" };
if validate.interfaceOf(user, User) {
    io.println("valid user");
}

const user2 = { id: "1", name: "Alice" };
if !validate.interfaceOf(user2, User) {
    io.println("invalid user");
}
```

---

### `url` (URL Encoding & Decoding)
Encode & Decode URL

### `io`, `json`, `fs`, `path`, and `os`
Core scripting modules for console IO, JSON parsing/formatting, file and directory operations, path helpers, and operating-system metadata.

### `time` (Timers and Measurement)
Support for execution delays, performance measurement, managed timers, and timestamp parsing.

```ts
import std "io";
import std "time";

// Managed interval timer
let timer = time.interval(1000, fn() {
    io.println("Tick");
});

time.sleep(5000);
timer.cancel();

// Parse RFC3339 timestamp to Unix seconds
let ts = time.parseUnix("2025-01-15T10:30:00Z", time.TimeUnit.Seconds);
```

---

### `array` (Native Operations)
Native operations for array manipulation, including `find`, `filter`, `map`, `reduce`, `sort`, `flat`, and `findIndex`.

### `strings`, Native Strings, and `buffer`
String helpers cover case conversion, trimming, replacement, splitting, containment checks, slicing, and string builders. The `buffer` module and native buffer values support byte-oriented data, hex conversion, indexed `u8` access, and string conversion.

---

### `http` (High-Throughput Web Services)
Fully concurrent web server and client. The server supports route-based multiplexing and optimized JSON serialization. The client supports multipart/form-data file uploads.

```ts
import std "http";
import std "io";

let server = http.server(8080);

server.get("/users/:id", fn(req: http.RequestObject) {
    return http.json({
        id: req.params["id"],
        query: req.query
    });
});

io.println("Web server listening on port 8080");
server.start();
```

Multipart file uploads:

```ts
const res = http.post("https://example.com/upload", {
    multipart: true,
    form: { username: "tiny" },
    files: [{ field: "file", path: "./photo.png" }]
});
```

### `websocket`, `net`, and `process`
Network modules include WebSocket clients/servers and TCP servers/connections. The process module exposes CLI args, working directory helpers, environment variables, foreground/background process execution, and signals.

---

### `ui` (WebView Desktop Applications)
Lightweight desktop containers using HTML/CSS/JS with direct bindings to Tiny functions. Includes native OS file and folder dialogs.

```ts
import std "ui";

const win = ui.new(true);
win.setTitle("Tiny UI");
win.setSize(500, 400);

win.callback("registerClick", fn(arg) {
    return "Click registered";
});

win.setHtml("<h1>Hello Tiny</h1>");
win.run();
```

Native file dialogs:

```ts
const path = ui.openFileDialog("Select a file", "*.tiny");
if path {
    const content = fs.readFile(path);
}
```

---

### `desktop` (OS Automation)
Wraps native interfaces for automating keyboard, mouse, and clipboard interactions.

```ts
import std "desktop";

desktop.moveMouseSmooth(800, 600);
desktop.click();
desktop.type("Tiny Automation");
```

### `app`, `tray`, `observer`, `sync`, `runtime`, `sqlite`, `crypto`, and `tests`
Tiny includes app-command wiring, native tray support, live process telemetry, mutexes, runtime memory/GC/fatal-handler tools, child VM creation, source/bytecode compilation at runtime, embedded SQLite database access, cryptographic helpers, and a small test assertion module. The desktop application interface for process telemetry can be downloaded from the [Observer Tool Release](https://github.com/confh/Tiny/releases/tag/observer-tool).

***

## Tooling and Ecosystem

### Command Line Interface (CLI)
- **`tiny <file.tiny/file.tbc>`**: Compiles and runs a source file directly or runs compiled bytecode if the file ends with **.tbc**.
- **`tiny`**: Runs the `entry` from `tiny.json` when a project config exists.
- **`tiny build <file> -o <file.tbc>`**: Compiles source to bytecode.
- **`tiny run <file.tbc>`**: Runs compiled bytecode.
- **`tiny watch <file>`** or **`tiny --watch <file>`**: Restarts when the entry or imported files change.
- **`tiny pack <file> -o <binary>`**: Bundles bytecode and the VM runtime into a single standalone native executable (~13MB).
- **`tiny dist <file> -o <dir>`**: Packages the application with plugins, assets, and target-specific output.
- **`tiny init [dir]`**: Creates a project with `tiny.json`, `src/main.tiny`, `plugins`, and `dist`.
- **`tiny add/install/remove/deps`**: Manages GitHub-backed Tiny dependencies and lock metadata.
- **`tiny task [name]`**: Runs scripts from `tiny.json`.
- **`tiny fmt <file.tiny>`**: Formats a source file in place using the built-in document formatter.
- **`tiny update`**: Updates the Tiny binary from the latest GitHub release.
- **`tiny lsp`**: Starts the Language Server.

Windows packs can use `--icon <icon.ico>` to set the executable icon. The icon must be an `.ico` file and currently applies only to `windows-amd64` targets.

```bash
tiny pack src/main.tiny -o dist/observer.exe --windowed --icon assets/observer.ico
tiny dist src/main.tiny -o dist/observer.exe --windowed --icon assets/observer.ico
```

### Project Configuration and Package Management
Projects are described by `tiny.json`, including the entry file, output directory, target, scripts, dependencies, ignored package paths, native plugin assets, and compiler options such as bytecode caching. Dependencies can pin a GitHub ref and are recorded in `tiny.lock`.

```json
{
  "entry": "src/main.tiny",
  "target": "windows-amd64",
  "scripts": {
    "start": "tiny",
    "dist": "tiny dist"
  },
  "dependencies": {
    "jwt": {
      "source": "github:confh/TinyJWT",
      "version": "v1.0.0"
    }
  }
}
```

### [Built-in Language Server (LSP)](https://marketplace.visualstudio.com/items?itemName=Confis.tiny)
Run `tiny lsp` for integration with editors like VS Code. Features include:
- **Organize Imports**: Automatic sorting and unused import removal.
- **Semantic Recovery**: Diagnostics that persist even during syntax errors.
- **Type Narrowing**: Flow-based type inference (e.g., after `null` checks).
- **Refactoring Safety**: Correct symbol resolution for object keys and variable identifiers.
