# Tiny Language

A small interpreted programming language written in Go.

Tiny is a simple scripting language with variables, functions, objects, classes, imports, built-in modules, and a stack-based bytecode VM. It is mainly built for learning how programming languages work, while still being usable enough to write small scripts and experiments.

> The language name is temporary. Rename this README if your language has a final name.

---

## Features

- `let` variables
- `const` constants
- Numbers, floats, strings, booleans, `null`, and `undefined`
- Functions
- `return` and `return;`
- `if / else`
- `while` loops
- Objects/maps
- Property access and assignment
- Methods with `this`
- Basic classes
- Imports
- String interpolation with backticks
- Core built-ins
- Math built-ins
- Native server object
- Function callbacks for routes
- Custom language errors

---

## Example

```tiny
const version = 0.1;

class User {
    fn init(name, age) {
        this.name = name;
        this.age = age;
    }

    fn greet() {
        core.println(`Hello ${this.name}!`);
    }

    fn birthday() {
        this.age = this.age + 1;
    }
}

let user = User("confis", 18);

user.greet();
core.println("Age:", user.age);

user.birthday();
core.println("New age:", user.age);

core.println("Version:", version);
```

Output:

```txt
Hello confis!
Age: 18
New age: 19
Version: 0.1
```

---

## Variables

Use `let` for variables that can be reassigned:

```tiny
let name = "confis";
name = "alex";

core.println(name);
```

Use `const` for variables that cannot be reassigned:

```tiny
const name = "confis";

name = "alex"; // ConstError
```

Note: if a `const` contains an object, the variable itself cannot be reassigned, but object properties may still be changed.

```tiny
const user = {
    name: "confis"
};

user.name = "alex"; // allowed
user = {};          // not allowed
```

---

## Types

Currently supported value types include:

```tiny
let number = 123;
let float = 0.1;
let text = "hello";
let active = true;
let nothing = null;
let missing = undefined;

let user = {
    name: "confis",
    age: 18
};
```

---

## Strings

Normal strings use double quotes:

```tiny
let name = "confis";
core.println(name);
```

Interpolated strings use backticks:

```tiny
let name = "confis";
let age = 18;

core.println(`Hello ${name}, you are ${age} years old.`);
```

---

## Functions

```tiny
fn add(a, b) {
    return a + b;
}

core.println(add(10, 5));
```

Functions can return nothing:

```tiny
fn check(value) {
    if value == 0 {
        return;
    }

    core.println(value);
}
```

Functions can also be passed around as values:

```tiny
fn greet() {
    return "Hello";
}

let fnRef = greet;
core.println(fnRef());
```

---

## Conditions

```tiny
let age = 18;

if age >= 18 {
    core.println("Adult");
} else {
    core.println("Not adult");
}
```

Boolean operators:

```tiny
let age = 18;
let hasCard = true;

if age >= 18 and hasCard {
    core.println("Allowed");
}

if age < 18 or hasCard == false {
    core.println("Blocked");
}
```

---

## Loops

```tiny
let i = 0;

while i < 5 {
    core.println(i);
    i = i + 1;
}
```

---

## Objects

```tiny
let user = {
    name: "confis",
    age: 18
};

core.println(user.name);

user.name = "alex";
user.age = user.age + 1;

core.println(user.name);
core.println(user.age);
```

---

## Methods and `this`

Objects can store functions as methods.

```tiny
fn greet() {
    core.println("Hello", this.name);
}

fn rename(newName) {
    this.name = newName;
}

let user = {
    name: "confis",
    greet: greet,
    rename: rename
};

user.greet();

user.rename("alex");
user.greet();
```

Output:

```txt
Hello confis
Hello alex
```

---

## Classes

Classes are a cleaner way to create objects with methods.

```tiny
class User {
    fn init(name, age) {
        this.name = name;
        this.age = age;
    }

    fn greet() {
        core.println("Hello", this.name);
    }

    fn birthday() {
        this.age = this.age + 1;
    }
}

let user = User("confis", 18);

user.greet();
user.birthday();

core.println(user.age);
```

Output:

```txt
Hello confis
19
```

The `init` method is used as the constructor.

---

## Imports

You can import another file:

```tiny
import "math.tiny";

core.println(add(10, 5));
```

Example `math.tiny`:

```tiny
fn add(a, b) {
    return a + b;
}
```

Imported files are loaded before the main file runs.

---

## Built-in Modules

### `core`

Common runtime and system functions.

```tiny
core.print("Hello");
core.println("Hello");
core.input("Enter your name: ");
core.clock();
core.halt();
core.exit(0);
```

### `math`

Basic number conversions and math helpers.

```tiny
let x = math.toFloat(10);
let y = math.toInt(3.9);

core.println(x);
core.println(y);
```

Suggested future math functions:

```tiny
math.floor(3.9);
math.ceil(3.1);
math.round(3.5);
math.abs(-10);
math.min(1, 2);
math.max(1, 2);
```

---

## HTTP Server Example

Tiny can create a simple native Go-backed server object.

```tiny
fn home(req) {
    return `It's been ${math.toFloat(core.clock()) / 1000.0} seconds since this server started.`;
}

fn api(req) {
    return core.toJSON({
        path: req.path,
        method: req.method
    });
}

let server = core.server(3000);

server.get("/", home);
server.get("/api", api);
server.get("/static", "Static response");

core.println("Server running on http://localhost:3000");

server.start();
```

Then open:

```txt
http://localhost:3000/
http://localhost:3000/api
http://localhost:3000/static
```

---

## Running

Put your code in `main.tiny`, then run:

```bash
go run .
```

Or run a specific file:

```bash
go run . main.tiny
```

Debug bytecode output:

```bash
go run . --debug main.tiny
```

---

## Building

Build the interpreter:

```bash
go build -o tiny .
```

Run:

```bash
./tiny main.tiny
```

On Windows:

```bash
tiny.exe main.tiny
```

---

## Project Structure

Suggested Go file layout:

```txt
.
├── main.go
├── token.go
├── lexer.go
├── ast.go
├── parser.go
├── loader.go
├── opcode.go
├── compiler.go
├── value.go
├── vm.go
├── builtins.go
├── errors.go
└── main.tiny
```

All Go files can stay in the same package:

```go
package main
```

---

## Current Limitations

This language is still experimental.

Current limitations may include:

- No inheritance
- No private fields
- No static class methods
- No module namespaces yet
- No type checker
- Runtime type errors are still possible
- No optimizer
- No native binary compilation
- Bytecode is currently run in memory by the VM
- Performance is not comparable to mature languages like Python or JavaScript yet

---

## Roadmap Ideas

Possible future additions:

- Arrays
- Better standard library modules
- `string` module
- `json` module
- `fs` module
- `http` client module
- `break` and `continue`
- `for` loops
- Better error line/column reporting
- Bytecode file output
- Local variable slots for performance
- Constant pool
- Integer opcodes
- Real module system
- Anonymous functions
- Better server callbacks
- Package manager