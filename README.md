<div align="center">
  <img src="examples/tiny.png" alt="Tiny Logo" width="200">
  <h1>Tiny Programming Language</h1>
  <p><b>Small. Fast. Expressive.</b></p>
  <p>Tiny is a lightweight scripting language and stack-based bytecode VM written in Go.</p>

  <p>
    <img src="https://img.shields.io/badge/Language-Tiny-blue.svg">
    <img src="https://img.shields.io/badge/Built%20With-Go-00ADD8.svg">
    <img src="https://img.shields.io/badge/VS%20Code-Extension-007ACC.svg">
    <img src="https://img.shields.io/badge/License-MIT-green.svg">
  </p>
</div>

---

Tiny sits in the "sweet spot" between a quick bash script and a complex Go program. It’s perfect for CLI tools, JSON automation, HTTP services, and native-plugin experiments.

<p align="center">
  <img src="examples/showcase.gif" alt="Tiny Showcase">
</p>

## Features

* Dynamically typed with optional static type hints
* Compiles to bytecode (.tbc) and runs on a custom VM
* Case-sensitive syntax
* In-place bytecode optimizations (fusing common loop and variable operations)
* Built-in Language Server (LSP) for VS Code (supporting syntax warnings, autocomplete, jump-to-definition, and renaming)
* Single-binary packaging (`tiny pack` bundles your script and the VM into a standalone executable)
* Native Go-based standard library, including:
  * `io` (console I/O)
  * `fs` (file system operations)
  * `json` (high-performance parsing and stringifying)
  * `http` (built-in client and server)
  * `math` (math operations and matrix multiplication)
  * `desktop` (CGO-free mouse, keyboard, and clipboard automation)
  * `process`, `regex`, `time`, `net`, `sync`

## VS Code Support

Tiny has a built-in Language Server (LSP) to provide a modern development workflow. You can install the official VS Code extension for syntax highlighting, autocomplete, and diagnostics.

<p align="center">
  <img src="examples/extension.png" alt="VS Code Extension" width="500">
</p>

You can download the extension here: [Tiny for Visual Studio Code](https://github.com/confh/TinyVsCode/releases/latest)

## Quick Start (Language Tour)

### Classes and Methods
```js
import std "io";

class Greeter {
    field prefix = "Hello";
    fn init(p) { this.prefix = p; }
    fn greet(name) {
        return `${this.prefix}, ${name}!`;
    }
}

let g = Greeter("Welcome");
io.println(g.greet("Tiny"));
```

### JSON and File IO
```js
import std "io";
import std "fs";
import std "json";

let data = { user: "David", score: 100 };
fs.writeFile("save.json", json.pretty(data));

let loaded = json.parse(fs.readFile("save.json"));
io.println(`User: ${loaded.user}`);
```

### Async Tasks
```js
import std "io";
import std "time";

let task = spawn fn() {
    time.sleep(1000);
    return "Result from background!";
};

io.println("Doing other things...");
io.println(await task);
```

## Installation & Setup

### Pre-built Binaries
You can download the pre-compiled executable for your OS from the [Releases Page](https://github.com/confh/Tiny/releases/latest).

1. Move the binary into a folder named `.tiny` in your home directory (e.g., `C:\Users\YourName\.tiny\tiny.exe` or `~/.tiny/tiny`).
2. Add the `.tiny` folder path to your system's `PATH` environment variable.
3. Grab the official VS Code extension from the marketplace for LSP support.

### Building from Source
If you prefer to build from source, clone the repository and build:

```bash
git clone https://github.com/confh/Tiny.git tiny
cd tiny

# On Linux/macOS
./build.sh

# On Windows
.\build.bat
```

## How It Works (Performance & Design)

Tiny compiles source files into a custom binary bytecode instruction stream (`.tbc`) before running them. The VM uses several optimizations to keep things fast:

* **Fast Local Access:** Local variables are resolved by the compiler and indexed as flat numeric slots inside the call frames.
* **Instruction Fusing:** The optimizer passes over the bytecode and fuses common sequences (like `OP_LOAD_LOCAL` followed by `OP_INC` and `OP_ASSIGN`) into single, optimized opcodes like `OP_INC_LOCAL`.
* **Constant Folding:** Static math expressions (like `1 + 2 * 3`) are evaluated by the compiler during codegen rather than at runtime.
* **Go GC Integration:** Tiny values are directly backed by Go's concurrent garbage collector, so memory is handled automatically.

## Distribution & Bundling

Tiny has built-in tools to package and distribute your code:

### Standalone Binaries (`tiny pack`)
You can bundle your compiled bytecode and the Tiny interpreter into a single standalone binary using the pack command:

```bash
tiny pack src/main.tiny -o mytool
```

<p align="center">
  <img src="examples/packing.gif" alt="Tiny Packing">
</p>

### Distribution Folder (`tiny dist`)
If your project uses **Native Plugins** (DLLs/SOs), `tiny dist` is the answer. It packs the executable *and* automatically gathers all linked plugins into a clean `dist/` folder.
```bash
tiny dist src/main.tiny -o release/app
```

### Native HTTP Server
You can write and execute lightweight HTTP services natively:

```javascript
import std "io";
import std "http";

let server = http.server("8080");
server.get("/hello", fn(req, res) {
    res.write("Hello from Tiny!");
});
```

<p align="center">
  <img src="examples/http_server.gif" alt="HTTP Server">
</p>

---

*Tiny is an open-source project licensed under the MIT License.*