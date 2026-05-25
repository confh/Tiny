<div align="center">
  <img src="tiny.png" alt="Tiny Logo" width="256" height="256">
  <h1>Tiny</h1>
  <p>Tiny is a small, expressive scripting language and bytecode VM written in Go.</p>
  <p>It is designed for the sweet spot between "quick script" and "real little
program": command-line tools, file processors, JSON automation, HTTP services,
small app launchers, native-plugin experiments, and portable packed executables.</p>
  
  </div>



```tiny
import std "io";
import std "json";
import std "fs";

class TodoStore {
    field path = "tasks.json";
    field tasks = [];

    fn init(path = "tasks.json") {
        this.path = path;
        this.load();
    }

    fn load() {
        try {
            this.tasks = json.parse(fs.readFile(this.path));
        } catch err {
            this.tasks = [];
        }
    }

    fn add(title: string) {
        this.tasks.push({
            title: title,
            done: false
        });
        this.save();
    }

    fn save() {
        fs.writeFile(this.path, json.pretty(this.tasks));
    }
}

const store = TodoStore();
store.add("ship something tiny");
io.println(`Saved ${store.tasks.length()} task(s).`);
```

## What Tiny Gives You


| Area             | What you get                                                                                                                                 |
| ---------------- | -------------------------------------------------------------------------------------------------------------------------------------------- |
| Language         | variables, constants, functions, closures, classes, fields, methods, enums, namespaces, imports, loops, match, try/catch/finally             |
| Runtime          | stack-based VM, bytecode compiler, call frames, local slots, class/method dispatch, task objects                                             |
| Data             | strings, numbers, booleans, arrays, objects, buffers, null, undefined                                                                        |
| Tooling          | run source, build bytecode, run bytecode, initialize projects, run project tasks, pack executables, create dist folders                      |
| Standard library | IO, files, JSON, HTTP, TCP, process control, strings, arrays, buffers, regex, path helpers, math, desktop, time, OS/runtime info, app command helpers |
| Distribution     | JSON bytecode files and standalone packed executables for `windows-amd64` and `linux-amd64`                                                  |


## Build Tiny

```bash
./build.sh
```

On Windows:

```bash
.\build.bat
```

Run a file:

```bash
tiny main.tiny
```

Run a project:

```bash
tiny init hello
cd hello
tiny
```

When run without arguments, `tiny` reads `tiny.json` and executes the configured
entry file.

## Command Line

### Run Source

```bash
tiny src/main.tiny arg1 arg2
```

Arguments after the source file are available through `process.args()`.

```tiny
import std "process";
import std "io";

io.println(process.args());
```

Tiny also supports a source-run cache. Disable it when needed:

```bash
tiny src/main.tiny --disable-cache
```

### Run A Project

```bash
tiny
```

Looks for `tiny.json` in the current folder and runs its `entry`.

### Build Bytecode

```bash
tiny build src/main.tiny -o dist/app.tbc
```

The `.tbc` file stores the optimized main instruction stream, function table,
and class table as Tiny bytecode data.

Run bytecode:

```bash
tiny run dist/app.tbc
```

### Pack An Executable

```bash
tiny pack src/main.tiny -o dist/app
```

`tiny pack` compiles your program, embeds the bytecode into a Tiny runtime, and
writes a runnable executable. On Windows, `.exe` is added when appropriate.

Use a target:

```bash
tiny pack src/main.tiny -o dist/app --target linux-amd64
tiny pack src/main.tiny -o dist/app.exe --target windows-amd64
```

Inside a configured project, this works too:

```bash
tiny pack
```

### Create A Dist Folder

```bash
tiny dist src/main.tiny -o dist/app --target windows-amd64
```

`tiny dist` packs the executable and copies native plugins it can discover from
`Plugin.load("path")`. You can add extra plugins manually:

```bash
tiny dist src/main.tiny -o dist/app --plugin plugins/native_tools.dll
```

Supported targets:


| Target          | Output                                               |
| --------------- | ---------------------------------------------------- |
| `windows-amd64` | Windows executable, `.exe` added when missing        |
| `linux-amd64`   | Linux executable, marked executable with `chmod 755` |


### Initialize A Project

```bash
tiny init my-app
```

Creates:

```txt
my-app/
  tiny.json
  README.md
  .gitignore
  src/main.tiny
  plugins/
  dist/
```

Initialize the current folder:

```bash
tiny init .
```

### Run Project Tasks

`tiny task` reads `tiny.json` scripts.

```bash
tiny task
tiny task build
tiny task pack
```

Project configs created by `tiny init` include:

```json
{
  "scripts": {
    "start": "tiny run",
    "build": "tiny build",
    "pack": "tiny pack",
    "dist": "tiny dist"
  }
}
```

## Project Configuration

`tiny.json` is the project manifest.

```json
{
  "name": "hello",
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

The most important fields are:


| Field     | Purpose                                                |
| --------- | ------------------------------------------------------ |
| `name`    | default packed executable name                         |
| `entry`   | source file run by `tiny` and used by project commands |
| `outDir`  | default output directory                               |
| `target`  | default pack/dist target                               |
| `scripts` | named shell commands for `tiny task`                   |


## Language Tour

### Comments

```tiny
// A line comment.
```

### Values

```tiny
let count = 10;
let ratio = 12.5;
let name = "Tiny";
let enabled = true;
let missing = undefined;
let nothing = null;
```

### Variables And Constants

Use `let` for mutable bindings and `const` for bindings that cannot be rebound.

```tiny
let score = 1;
score += 10;
score++;

const version = "0.1.0";
```

Constants protect the binding, not the contents of objects or arrays:

```tiny
const user = { name: "Ada" };
user.name = "Grace"; // allowed
```

### Strings And Interpolation

```tiny
let language = "Tiny";
let message = `Hello from ${language}`;
```

Escape sequences include `\n`, `\r`, `\t`, `\\`, `\"`, and `\0`.

### Arrays

```tiny
let items = ["lexer", "parser"];

items.push("vm");
io.println(items[0]);
io.println(items.length());

items[1] = "compiler";
items.remove(0);
```

Useful array methods:

```tiny
items.length();
items.push(value);
items.pop();
items.get(index);
items.set(index, value);
items.contains(value);
items.join(", ");
items.reverse();
items.map(fn(index, value) { return value; });
items.forEach(fn(index, value) { io.println(value); });
items.filter(fn(index, value) { return true; });
items.clear();
items.remove(index);
```

### Objects

```tiny
let user = {
    name: "Tiny",
    score: 42,
    tags: ["compiler", "vm"]
};

io.println(user.name);
user.score += 1;

user["role"] = "tool";
io.println(user["role"]);
```

Dot property access is checked. Bracket access is dynamic and useful when keys
come from data.

### Operators

```tiny
let a = 10 + 2;
let b = 10 - 2;
let c = 10 * 2;
let d = 10 / 2;
let e = 10 % 3;

let ok = a > b and c != d;
let fallback = ok or false;
let label = ok ? "yes" : "no";
```

Supported assignment forms include:

```tiny
value = 1;
value += 2;
value -= 1;
value++;
value--;

user.score += 10;
```

### Type Hints

Tiny type hints are checked at runtime.

```tiny
let title: string = "Readme";
let retries: number = 3;

fn add(a: number, b: number): number {
    return a + b;
}
```

Common hint names:


| Hint        | Meaning                 |
| ----------- | ----------------------- |
| `any`       | any value               |
| `number`    | integer or float number |
| `string`    | text                    |
| `bool`      | boolean                 |
| `array`     | array                   |
| `object`    | object                  |
| `function`  | callable value          |
| `null`      | null                    |
| `undefined` | undefined               |
| class name  | instance of that class  |


Union-like hints appear in several library signatures:

```tiny
let maybeName: string | null = null;
```

### Functions

```tiny
fn add(a, b) {
    return a + b;
}

io.println(add(2, 3));
```

Default parameters:

```tiny
fn greet(name, prefix = "Hello") {
    return `${prefix}, ${name}`;
}
```

Functions are values:

```tiny
let transform = fn(value) {
    return value * 2;
};

io.println(transform(21));
```

Closures capture local variables:

```tiny
fn makeCounter() {
    let value = 0;

    return fn() {
        value++;
        return value;
    };
}

let next = makeCounter();
io.println(next());
io.println(next());
```

### Control Flow

```tiny
if score >= 90 {
    io.println("excellent");
} else {
    io.println("keep going");
}
```

```tiny
while running {
    tick();
}
```

```tiny
for let i = 0; i < 10; i++ {
    io.println(i);
}
```

```tiny
for task, index in tasks {
    io.println(index, task.title);
}
```

`break` and `continue` work inside loops.

### Match

```tiny
match command {
    "add" {
        addTask();
    }
    "list" {
        listTasks();
    }
    _ {
        io.println("Unknown command");
    }
}
```

### Enums

```tiny
enum Status {
    Open,
    Done,
    Blocked
}

let status = Status.Open;
```

### Classes

Classes group fields and methods. `init` is the constructor hook.

```tiny
class User {
    field name: string = "";
    field score: number = 0;

    fn init(name: string, score: number = 0) {
        this.name = name;
        this.score = score;
    }

    fn rename(name: string) {
        this.name = name;
        return this;
    }

    fn label(): string {
        return `${this.name}: ${this.score}`;
    }
}

let user = User("Ada", 10);
user.rename("Grace");
io.println(user.label());
```

Private members are available inside the class:

```tiny
class TokenStore {
    private field token = "";

    private fn reset() {
        this.token = "";
    }
}
```

Embedded classes let one object delegate method lookup to another object:

```tiny
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
service.log("delegated through embed");
```

### `typeof` And `instanceof`

```tiny
io.println(typeof "hello");   // string
io.println(typeof [1, 2, 3]); // array

if user instanceof User {
    io.println("yes");
}
```

### Errors

```tiny
import std "error";
import std "io";

try {
    throw error.new("ValidationError", "title is required");
} catch err {
    io.println(err.kind);
    io.println(err.message);
} finally {
    io.println("finished");
}
```

Thrown strings, objects, and error values are normalized into Tiny error values
with `kind` and `message`.

### Tasks

```tiny
import std "time";
import std "io";

let task = spawn fn() {
    time.sleep(100);
    return "done";
};

io.println(task.await());
```

`spawn` creates a task from an anonymous function. `await()` waits for the result.

## Imports, Modules, And Exports

### File Imports

```tiny
import "lib/math.tiny";
```

Non-aliased imports are loaded into the current program.

### Aliased Imports

```tiny
import "todo.tiny" as Todo;

const manager = Todo.TaskManager();
manager.add("write docs");
```

Aliased imports create a namespace object.

### Exports

```tiny
export const version = "1.0.0";

export fn createUser(name) {
    return User(name);
}

export class User {
    fn init(name) {
        this.name = name;
    }
}
```

When a namespaced file has explicit exports, only exported declarations are
visible through the namespace.

### Standard Modules

```tiny
import std "io";
import std "json" as JSON;

io.println(JSON.stringify({ ok: true }));
```

## Standard Library

Tiny's standard library is intentionally compact, but it covers the things
scripts usually need.


| Module       | Purpose                                                                                                   |
| ---------    | ---------------------------------------------------------------------------------                         |      
| `io`         | print, println, input, readLine                                                                           |
| `fs`         | open, readFile, writeFile, writeBytes, exists, readDir, mkDir, stat, copy, remove                         |
| `json`       | stringify, pretty, parse, readFile, writeFile                                                             |
| `http`       | HTTP client helpers and an HTTP server object                                                             |
| `net`        | TCP server creation                                                                                       |
| `process`    | args, cwd, env, run, shell, start, exit, process handles                                                  |
| `path`       | join, baseName, dirName, extName, cwd                                                                     |
| `array`      | range, isArray, from                                                                                      |
| `string`     | random, isDigit, newBuilder                                                                               |
| `object`     | get, set, has, delete, keys, values, entries, length                                                      |
| `buffer`     | alloc, fromString, fromArray                                                                              |
| `regex`      | matchString, findString                                                                                   |
| `math`       | numeric conversion, scalar math, trig, matrices, buffer sums                                              |
| `desktop`    | controlling the mouse, controllin the keyboard, taking a screenshot, controlling clipboard                |
| `time`       | sleep, nowMs, nowSec, clock                                                                               |
| `os`         | name, arch                                                                                                |
| `runtime`    | lockThread, unlockThread                                                                                  |
| `error`      | create structured error values                                                                            |
| `app`        | command-style app helper                                                                                  |


### IO

```tiny
import std "io";

io.print("Name: ");
let name = io.readLine();
io.println("Hello", name);
```

### Files And JSON

```tiny
import std "fs";
import std "json";

let config = {
    name: "tiny",
    fast: true
};

fs.writeFile("config.json", json.pretty(config));

let loaded = json.parse(fs.readFile("config.json"));
io.println(loaded.name);
```

### HTTP Client

```tiny
import std "http";
import std "io";

let response = http.get("https://example.com", {
    headers: {
        Accept: "text/plain"
    }
});

io.println(response.status);
io.println(response.body);
```

### HTTP Server

```tiny
import std "http";
import std "json";
import std "io";

let server = http.server(8090);

server.get("/", fn(req) {
    return http.json({
        method: req.method,
        path: req.path,
        query: req.query
    });
});

server.post("/echo", fn(req) {
    return http.text(req.body);
});

io.println("Listening on http://localhost:8090");
server.start();
```

Server object methods:

```tiny
server.get(path, handler);
server.post(path, handler);
server.getJSON(path, value);
server.getPrettyJSON(path, value);
server.start();
server.start(true); // async
server.stop();
```

### Processes

```tiny
import std "process";
import std "io";

io.println(process.args());
io.println(process.cwd());
io.println(process.getEnv("HOME"));

let result = process.run("go", ["version"], {
    stdout: true,
    stderr: true
});

io.println(result.stdout);
```

Long-running processes:

```tiny
let proc = process.start("my-server", [], { stdout: true });
io.println(proc.pid());
proc.interrupt();
proc.wait();
```

### Buffers

```tiny
import std "buffer";
import std "io";

let bytes = buffer.fromString("hello");
io.println(bytes.length());
io.println(bytes.toHex());

bytes.setU8(0, 72);
io.println(bytes.toString());
```

### Math

```tiny
import std "math";

io.println(math.toInt("42"));
io.println(math.sqrt(81));
io.println(math.clamp(120, 0, 100));
```

Matrix helpers:

```tiny
let a = {
    rows: 2,
    cols: 2,
    data: [1, 2, 3, 4]
};

let scaled = math.matScale(a, 10);
```


### Desktop

```tiny
import std "desktop";

io.println(desktop.mousePosition());
desktop.screenshot("screenshot.png");
```

### Regex

```tiny
import std "regex";

io.println(regex.matchString("abc123", "[0-9]+"));
io.println(regex.findString("abc123", "[0-9]+"));
```

### Path

```tiny
import std "path";

io.println(path.join("dist", "app.exe"));
io.println(path.baseName("src/main.tiny"));
io.println(path.extName("src/main.tiny"));
```

### App Commands

The `app` module helps build command-based scripts.

```tiny
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

## Native Types

Some values expose methods directly.


| Type              | Common methods                                                                                                      |
| ----------------- | ------------------------------------------------------------------------------------------------------------------- |
| `array`           | `length`, `push`, `get`, `set`, `pop`, `contains`, `join`, `reverse`, `map`, `forEach`, `filter`, `clear`, `remove` |
| `string`          | `length`, `toUpperCase`, `toLowerCase`, `upper`, `lower`, `split`, `includes`, `trim`, `replace`, `replaceAll`      |
| `buffer`          | `length`, `getU8`, `setU8`, `toHex`, `toString`                                                                     |
| `file`            | `read`, `close`                                                                                                     |
| `process`         | `pid`, `wait`, `kill`, `killTree`, `interrupt`, `isRunning`, `signal`                                               |
| `server`          | `get`, `post`, `getJSON`, `getPrettyJSON`, `start`, `stop`                                                          |
| `stringBuilder`   | `writeString`, `string`                                                                                             |
| `tcpServerObject` | `start`, `onConnection`                                                                                             |


## Native Plugins

Tiny can load native plugins with `Plugin.load`.

```tiny
let plugin = Plugin.load("plugins/my_plugin");
io.println(plugin.hello("Tiny"));
```

The extension is inferred when omitted:


| Platform | Extension |
| -------- | --------- |
| Windows  | `.dll`    |
| Linux    | `.so`     |


The Go helper package in `src/tinyplugin` makes plugin authoring easier:

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

Plugins exchange JSON-compatible values with Tiny.

## Bytecode, VM, And Optimizations

Tiny is not an interpreter walking the AST. It compiles source to bytecode, then
runs that bytecode on a stack-based VM.

```txt
source files
  -> import loader
  -> lexer
  -> parser
  -> compiler
  -> bytecode optimizer
  -> VM
```

The VM uses:

- a value stack
- function call frames
- local slots
- globals
- class and method tables
- try/catch handler stacks
- native method dispatch

The compiler and bytecode optimizer include practical optimizations such as:

- constant folding
- compact bytecode serialization
- optimized bytecode before source runs, bytecode runs, packing, and dist builds
- fast local-slot access paths
- specialized array length/get/push bytecode patterns
- optimized method dispatch paths for common native methods
- source-run bytecode cache in `.tinycache`

For scripts, CLIs, and small services, the result is a language that feels light
while still having a real compilation pipeline.

## Packaging Model

Packed executables are built like this:

1. Load and compile the Tiny program.
2. Optimize main bytecode and function bytecode.
3. Serialize the bytecode.
4. Read the embedded Tiny runtime for the target platform.
5. Append bytecode bytes to the runtime.
6. Append the bytecode size.
7. Append the `TINYAPP1` marker.

At startup, the packed runtime reads its own appended bytecode and runs it.

That means you can hand someone a single executable without shipping `.tiny`
source files.

## Examples

The `examples/` folder is a syntax tour.

```txt
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

Run one:

```bash
cd examples/01-basics
../../tiny
```

Build one:

```bash
../../tiny build src/main.tiny -o dist/basics.tbc
../../tiny run dist/basics.tbc
```

Pack one:

```bash
../../tiny pack src/main.tiny -o dist/basics
```

## Repository Map

```txt
src/
  main.go                 CLI entrypoint
  loader.go               import loader
  compiler.go             AST to bytecode compiler
  pack.go                 executable packing
  dist.go                 dist folder creation
  dist_plugin.go          plugin discovery/copying for dist
  project.go              tiny.json helpers
  task_command.go         tiny task runner
  bytecode/               bytecode serialization
  cmd/tiny_runtime/       packed executable runtime
  tinyerrors/             language error helpers
  tinyplugin/             native plugin helper package
  vm/                     lexer, parser, AST, VM, std modules, native types
```

## Development

Run all tests:

```bash
go test ./src/...
```

Build Tiny (linux):

```bash
./build.sh
```

Build Tiny (windows):

```bash
.\build.bat
```

## Notes

Tiny is experimental and intentionally small. It favors readable implementation,
fast iteration, and useful scripting features over being a full production
language runtime.

Current caveats:

- Type hints are runtime checks, not a full static type system.
- Standard library coverage is useful but intentionally compact.
- Native plugin loading depends on platform and build support.
- Packed runtime targets are currently `windows-amd64` and `linux-amd64`.
- Performance is best judged as a small stack VM built for scripts and tools,
not as a replacement for mature optimizing runtimes.

