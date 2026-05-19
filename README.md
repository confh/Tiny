# Tiny Language

Tiny is a small scripting language and bytecode VM written in Go. It is built as a learning-friendly language runtime, but it has enough practical features to write command-line tools, file processors, HTTP services, and small automation scripts.

Tiny includes:

- A lexer, parser, AST, compiler, JSON bytecode format, and stack-based VM
- Variables, constants, functions, closures, classes, methods, namespaces, imports, enums, loops, and error handling
- Runtime type hints for variables, parameters, and returns
- Standard modules for IO, files, JSON, HTTP, processes, strings, arrays, buffers, regex, time, OS info, runtime thread control, app commands, and errors
- Native plugin loading on supported builds
- Bytecode build output and packed executable generation for Windows and Linux AMD64

## Quick Start

Build the Tiny executable:

```bash
go build -ldflags "-w -s" -o tiny ./src
```

On Windows this creates `tiny.exe` if you choose that output name:

```bash
go build -ldflags "-w -s" -o tiny.exe ./src
```

Run a Tiny source file directly:

```bash
tiny main.tiny
```

Create and run a configured Tiny project:

```bash
tiny init hello-tiny
cd hello-tiny
tiny
```

in the same folder

```bash
tiny init .
tiny
```

When `tiny` is run with no arguments, it looks for `tiny.json` in the current directory and runs the configured `entry` file.

## CLI Commands

### Run Source

```bash
tiny path/to/main.tiny arg1 arg2
```

Runs a `.tiny` source file directly. Arguments after the file are available through `process.args()`.

```js
import std "process";
import std "io";

io.println(process.args());
```

### Run From `tiny.json`

```bash
tiny
```

Loads `tiny.json`, reads the `entry` field, compiles the source in memory, and runs it.

### Build Bytecode

```bash
tiny build src/main.tiny -o dist/app.tbc
```

Compiles source to Tiny bytecode. The bytecode file is JSON and contains:

- Bytecode version
- Main instruction stream
- Function table
- Class table

Run bytecode with:

```bash
tiny run dist/app.tbc
```

### Pack Executable

```bash
tiny pack src/main.tiny -o dist/app
```

Compiles the program, appends the bytecode to an embedded Tiny runtime, and writes a runnable executable. On Windows, `.exe` is added when needed.

With a project config, `tiny pack` can use `tiny.json`:

```bash
tiny pack
```

### Create Distribution Folder

```bash
tiny dist src/main.tiny -o dist/app --target windows-amd64
```

Creates a packed executable and copies statically discoverable native plugins used through `Plugin.load("path")`.

Supported runtime targets:

- `windows-amd64`
- `linux-amd64`

### Initialize Project

```bash
tiny init my-project
```

Creates:

```txt
my-project/
  tiny.json
  README.md
  .gitignore
  src/main.tiny
  plugins/
  dist/
```

## Project Configuration

`tiny.json` describes a Tiny project:

```json
{
  "name": "my-project",
  "version": "0.1.0",
  "entry": "src/main.tiny",
  "outDir": "dist",
  "target": "windows-amd64",
  "scripts": {
    "start": "tiny run",
    "build": "tiny build",
    "pack": "tiny pack",
    "dist": "tiny dist"
  },
  "plugins": [],
  "compilerOptions": {
    "stackTraces": true,
    "strict": false
  }
}
```

Current fields are parsed and preserved by tooling. The actively used fields are `name`, `entry`, `outDir`, and `target`.

## Language Tour

### Comments

Tiny supports line comments:

```js
// This is a comment.
```

### Variables And Constants

Use `let` for mutable variables and `const` for bindings that cannot be reassigned.

```js
let name = "jake";
name = "alex";

const version = "0.1.0";
```

Constants protect the binding, not the contents of referenced objects:

```js
const user = { name: "confis" };

user.name = "alex"; // allowed
// user = {};       // ConstError
```

### Primitive Values

```js
let count = 10;
let ratio = 12.5;
let text = "hello";
let active = true;
let nothing = null;
let missing = undefined;
```

### Strings

Normal strings use double quotes:

```js
let message = "hello";
```

Backtick strings support interpolation:

```js
let name = "Tiny";
let version = 1;

io.println(`Welcome to ${name} v${version}`);
```

Supported escape sequences in quoted strings include `\n`, `\r`, `\t`, `\\`, `\"`, and `\0`.

### Arrays

```js
let items = ["compiler", "runtime"];

items.push("plugins");
io.println(items.get(0));
io.println(items.length());
```

Indexing is also supported:

```js
io.println(items[1]);
items[1] = "vm";
```

### Objects

```js
let user = {
    name: "confis",
    score: 10
};

io.println(user.name);
user.score = user.score + 5;

user["role"] = "maintainer";
io.println(user["role"]);
```

Property access with `.` requires the property to exist. Bracket indexing on objects returns `undefined` for missing keys and creates that key with `undefined`.

### Operators

Arithmetic:

```js
let a = 10 + 2;
let b = 10 - 2;
let c = 10 * 2;
let d = 10 / 2;
let e = 10 % 3;
```

Comparison:

```js
a == b;
a != b;
a < b;
a > b;
a <= b;
a >= b;
```

Boolean logic:

```js
if ready and count > 0 {
    io.println("go");
}

if failed or retry {
    io.println("try again");
}
```

Unary operators:

```js
let negative = -10;
let opposite = !ready;
```

Compound assignment and increments are supported for variables and properties:

```js
count += 1;
count -= 1;
count++;
count--;

user.score += 10;
```

### Type Hints

Tiny checks type hints at runtime.

```js
let name: string = "Tiny";
let total: number = 10;

fn add(a: number, b: number): number {
    return a + b;
}
```

Supported built-in hint names include:

- `any`
- `number`
- `string`
- `bool`
- `array`
- `object`
- `function`
- `null`
- `undefined`

Class names can also be used through runtime type-name checks.

### Functions

```js
fn add(a, b) {
    return a + b;
}

io.println(add(2, 3));
```

Functions can return no explicit value:

```js
fn log(message) {
    io.println(message);
    return;
}
```

Functions are values:

```js
fn greet(name) {
    return `Hello ${name}`;
}

let callback = greet;
io.println(callback("Tiny"));
```

Anonymous functions are supported:

```js
let double = fn(value) {
    return value * 2;
};

io.println(double(21));
```

### Default Parameters

Default parameter values are supported for constant literal values.

```js
fn greet(name, prefix = "Hello") {
    return `${prefix}, ${name}`;
}

io.println(greet("Tiny"));
io.println(greet("Tiny", "Welcome"));
```

Required parameters cannot appear after default parameters.

### Closures

Nested functions and anonymous functions can capture local variables.

```js
fn makeCounter() {
    let value = 0;

    return fn() {
        value = value + 1;
        return value;
    };
}

let next = makeCounter();
io.println(next());
io.println(next());
```

### Control Flow

```js
if score >= 90 {
    io.println("great");
} else {
    io.println("keep going");
}
```

### Ternary Expressions

```js
let label = score >= 60 ? "pass" : "fail";
```

### While Loops

```js
let i = 0;

while i < 3 {
    io.println(i);
    i = i + 1;
}
```

### For Loops

```js
for let i = 0; i < 3; i = i + 1 {
    io.println(i);
}
```

### For-In Loops

```js
let names = ["lexer", "parser", "vm"];

for name, index in names {
    io.println(index, name);
}
```

The first loop variable is the item. The optional second variable is the index.

### Break And Continue

`break` and `continue` work in classic `while` and `for` loops:

```js
let i = 0;

while i < 10 {
    i = i + 1;

    if i == 3 {
        continue;
    }

    if i == 6 {
        break;
    }

    io.println(i);
}
```

### Match

```js
match status {
    "open" {
        io.println("still open");
    }
    "done" {
        io.println("complete");
    }
    _ {
        io.println("unknown");
    }
}
```

### Enums

Enums compile to constant objects whose members map to their own string names.

```js
enum Status {
    Open,
    Done,
    Blocked
}

io.println(Status.Open);
```

### Classes And Methods

Classes create objects with a `__class` marker and method values. `init` is the constructor.

```js
class User {
    fn init(name, score) {
        this.name = name;
        this.score = score;
    }

    fn rename(name) {
        this.name = name;
        return this;
    }

    fn label() {
        return `${this.name}: ${this.score}`;
    }
}

let user = User("confis", 42);
user.rename("Tiny user");

io.println(user.label());
```

### Embedded Classes

Classes can declare embedded fields. Method lookup can fall through into embedded objects.

```js
class Logger {
    fn log(message) {
        io.println(message);
    }
}

class Service {
    embed logger;

    fn init() {
        this.logger = Logger();
    }
}

let service = Service();
service.log("called through embedded logger");
```

### `instanceof`

```js
if user instanceof User {
    io.println("it is a user");
}
```

Embedded objects are considered during `instanceof` checks.

### `this`

`this` is available inside methods and points to the receiver object.

### `typeof`

```js
io.println(typeof "hello");     // string
io.println(typeof [1, 2, 3]);   // array
io.println(typeof undefined);   // undefined
```

### Errors, Try/Catch, Finally, And Throw

```js
import std "error";
import std "io";

try {
    throw error.new("ValidationError", "name is required");
} catch err {
    io.println(err.kind);
    io.println(err.message);
} finally {
    io.println("finished");
}
```

Thrown strings, error values, and objects are normalized into an error object with `kind` and `message`.

### Tasks

Tiny can spawn anonymous functions as tasks:

```js
import std "time";
import std "io";

let task = spawn fn() {
    time.sleep(100);
    return "done";
};

io.println(task.await());
```

`spawn` expects an anonymous function expression.

## Imports And Namespaces

### File Imports

```js
import "math.tiny";
```

Imported files are loaded before the importing file. Non-aliased imports are flattened into the current program.

### Aliased Imports

```js
import "lib/report.tiny" as Report;

io.println(Report.render(data));
```

Aliased imports create a namespace object.

### Standard Module Imports

```js
import std "io";
import std "json" as JSON;

io.println(JSON.stringify({ ok: true }));
```

### Exports

If a namespaced file has no explicit exports, Tiny exposes its functions, variables, classes, and enums. If any explicit export exists, only exported declarations are exposed.

```js
export fn publicName() {
    return "visible";
}

fn privateName() {
    return "hidden";
}
```

## Standard Library

Standard modules are imported with `import std "module";`.

### `io`

```js
io.print(value);
io.println(value1, value2);
let name = io.input("Name: ");
let line = io.readLine();
```

### `array`

```js
let numbers = array.range(1, 5);
array.isArray(numbers);
```

Array methods:

```js
items.length();
items.push(value);
items.pop();
items.get(index);
items.set(index, value);
items.contains(value);
items.join(", ");
items.reverse(); // mutates the array
items.map(fn(index, value) { return value; });
items.forEach(fn(index, value) { io.println(value); });
items.filter(fn(index, value) { return true; });
items.clear();
```

### `string`

```js
string.random(12);
string.isDigit("7");
let builder = string.newBuilder();
```

String methods:

```js
text.length();
text.toUpperCase();
text.toLowerCase();
text.split(",");
text.includes("needle");
text.trim();
text.replace("old", "new");
text.replaceAll("old", "new");
text.toString();
```

String builder methods:

```js
builder.writeString("hello");
builder.writeString(" world");
builder.string();
```

### `math`

```js
math.toFloat(10);
math.toInt(3.9);
```

### `json`

```js
let text = json.stringify({ ok: true });
let pretty = json.pretty({ ok: true });
let value = json.parse("{\"ok\":true}");
```

### `fs`

```js
fs.exists("path.txt");
fs.readFile("path.txt");
fs.writeFile("path.txt", "hello");
fs.writeBytes("path.bin", buffer.fromArray([1, 2, 3]));
fs.readDir(".");

let file = fs.open("path.txt");
let chunk = file.read(64);
file.close();
```

### `buffer`

```js
let bytes = buffer.fromString("hello");
let raw = buffer.fromArray([72, 105]);

bytes.length();
bytes.getU8(0);
bytes.setU8(0, 104);
bytes.toString();
bytes.toHex();
```

### `regex`

```js
regex.matchString("abc123", "[0-9]+");
regex.findString("abc123", "[0-9]+");
```

### `http`

HTTP client:

```js
let response = http.get("https://example.com", {
    headers: {
        Accept: "text/plain"
    }
});

io.println(response.status);
io.println(response.body);
```

```js
let response = http.post("https://example.com/api", {
    name: "Tiny"
}, {
    headers: {
        Authorization: "Bearer token"
    }
});
```

HTTP server:

```js
import std "http";
import std "json";
import std "io";

let server = http.server(8090);

server.get("/", fn(req) {
    return json.stringify({
        path: req.path,
        method: req.method,
        query: req.query
    });
});

server.post("/echo", fn(req) {
    return req.body;
});

io.println("Listening on http://localhost:8090");
server.start();
```

Server methods:

```js
server.get(path, stringOrFunction);
server.post(path, stringOrFunction);
server.getJSON(path, value);
server.getPrettyJSON(path, value);
server.start();
server.start(true); // async
server.stop();
```

### `process`

```js
process.args();
process.cwd();
process.getEnv("HOME");
process.run("go", ["version"], { stdout: true, stderr: true });
process.shell("echo hello", { stdout: true });
process.start("long-running-command", [], { stdout: true });
process.exit(0);
process.close();
process.halt();
```

Process handles from `process.start` support:

```js
proc.pid();
proc.wait();
proc.kill();
proc.killTree();
proc.interrupt();
proc.isRunning();
```

### `time`

```js
time.sleep(100); // milliseconds
time.nowMs();
time.nowSec();
time.clock(); // milliseconds since VM start
```

### `os`

```js
os.name();
os.arch();
```

### `runtime`

```js
runtime.lockThread();
runtime.unlockThread();
```

### `error`

```js
let err = error.new("ValidationError", "invalid input");
throw err;
```

### `app`

The `app` module helps build command-based scripts.

```js
import std "app";
import std "io";

let cli = app.new("tools");

cli.command("hello", fn(args) {
    io.println("hello", args.join(" "));
});

cli.run();
```

Run:

```bash
tiny tools.tiny hello Tiny
```

## Native Plugins

Tiny can load native plugins through the built-in `Plugin` object on supported builds.

```js
let plugin = Plugin.load("plugins/example");
io.println(plugin.someMethod("argument"));
```

The extension is inferred when omitted:

- Windows: `.dll`
- Linux: `.so`
- macOS path normalization exists in helper code, but native plugin loading is implemented for Windows and Linux cgo builds

Plugins export:

- `TinyPluginCall`
- `TinyPluginFree`

The `src/tinyplugin` Go package provides helpers for plugin authors:

```go
package main

import "C"
import "language.com/src/tinyplugin"

func init() {
    tinyplugin.Register("hello", func(args tinyplugin.Args) (any, error) {
        return "hello " + args.String(0), nil
    })
}
```

Plugin calls pass JSON-compatible Tiny values into the plugin and convert JSON-compatible plugin results back into Tiny values.

## Bytecode And Runtime Architecture

Tiny source is processed in this order:

1. `Loader` resolves file imports, detects circular imports, and flattens or namespaces imported statements.
2. `Lexer` turns source into tokens.
3. `Parser` creates the AST.
4. `Compiler` lowers AST statements and expressions into bytecode instructions.
5. `VM` executes bytecode using a stack, globals table, call frames, local slots, and try-handler stack.

The compiler performs small optimizations such as constant folding and string join lowering.

Packed executables are created by:

1. Compiling Tiny source to bytecode bytes
2. Taking an embedded Tiny runtime executable
3. Appending bytecode bytes
4. Appending bytecode size
5. Appending the `TINYAPP1` marker

The runtime executable reads its own appended bytecode and runs it.

## Error Reporting

Tiny reports language errors with a kind and message:

- `SyntaxError`
- `NameError`
- `TypeError`
- `RuntimeError`
- `ConstError`
- `ImportError`
- `InternalError`
- `Error`

When location information is available, errors include file, line, and column. Runtime thrown errors include a stack trace.

## Examples

The `examples/` folder is a numbered syntax tour. The examples avoid app-specific themes and focus directly on how Tiny code works.

### `examples/01-basics`

Basic values and expressions: `let`, `const`, type hints, strings, interpolation, arrays, objects, arithmetic, comparisons, booleans, `null`, `undefined`, and `typeof`.

```bash
cd examples/01-basics
../../tiny
```

### `examples/02-control-flow`

Control flow: `if`, `else`, `while`, classic `for`, `for in`, `break`, `continue`, ternaries, `match`, and enums.

```bash
cd examples/02-control-flow
../../tiny
```

### `examples/03-functions`

Functions: named functions, return values, default parameters, function values, anonymous functions, callbacks, and closures.

```bash
cd examples/03-functions
../../tiny
```

### `examples/04-arrays-objects`

Arrays and objects: array methods, object properties, bracket access, nested objects, and callback methods such as `map` and `filter`.

```bash
cd examples/04-arrays-objects
../../tiny
```

### `examples/05-modules`

Modules: file imports, aliased imports, namespaces, explicit exports, and private helper functions.

```bash
cd examples/05-modules
../../tiny
```

### `examples/06-classes`

Classes: constructors, methods, `this`, method chaining, embedded objects, and `instanceof`.

```bash
cd examples/06-classes
../../tiny
```

### `examples/07-errors`

Errors: `try`, `catch`, `throw`, and the `error` standard module.

```bash
cd examples/07-errors
../../tiny
```

### `examples/08-files-json`

Files and JSON: read a JSON file, parse it, update the object, and write a new file.

```bash
cd examples/08-files-json
../../tiny
```

### `examples/09-cli-args`

Command-line arguments: `process.args()` and a tiny command dispatcher.

```bash
cd examples/09-cli-args
../../tiny src/main.tiny upper hello tiny
../../tiny src/main.tiny join one two three
```

### `examples/10-http-server`

HTTP server basics: route callbacks, request objects, query values, and JSON responses.

```bash
cd examples/10-http-server
../../tiny
```

Open:

- `http://localhost:8090/`
- `http://localhost:8090/echo?text=hello`

## Current Notes And Limitations

Tiny is experimental and intentionally small. Some behavior is runtime-checked rather than statically checked.

Current limitations and caveats:

- Type hints are runtime checks, not a full static type system.
- Property access with `.` errors for missing object properties.
- Missing object keys accessed with `[]` become `undefined`.
- Standard library coverage is practical but small.
- Native plugin loading depends on platform/build support.
- Packed executables currently target embedded Windows AMD64 and Linux AMD64 runtimes.
- Some low-level process helpers are still rough. For example, environment mutation helpers exist internally but currently do not return a Tiny value.
- Performance is suitable for small scripts and experiments, not comparable to mature production language runtimes.

## Repository Layout

```txt
.
  src/
    main.go                 CLI entrypoint
    compiler.go             AST to bytecode compiler
    loader.go               import loader
    project.go              tiny.json config helpers
    pack.go                 executable packing
    dist.go                 dist command
    dist_plugin.go          plugin discovery for dist
    bytecode/               bytecode serialization
    cmd/tiny_runtime/       packed executable runtime
    tinyerrors/             language error helpers
    tinyplugin/             helper package for native plugins
    vm/                     lexer, parser, AST, VM, std modules
  examples/
    01-basics/
    02-control-flow/
    03-functions/
    04-arrays-objects/
    05-modules/
    06-classes/
    07-errors/
    08-files-json/
    09-cli-args/
    10-http-server/
```

## Development Checks

Compile all Go packages:

```bash
go test ./src/...
```

Build Tiny:

```bash
go build -o tiny ./src
```

Build an example to bytecode:

```bash
cd examples/01-basics
../../tiny build src/main.tiny -o dist/01-basics.tbc
```

Run bytecode:

```bash
../../tiny run dist/01-basics.tbc
```
