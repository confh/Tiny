# 13 Native Functions

This example demonstrates the `native fn` feature, which allows you to write high-performance Go code directly inside your Tiny scripts. These functions are compiled to WebAssembly using TinyGo and run at near-native speeds.

> **Note:** To use native functions, you **MUST** have [Go](https://go.dev/) and [TinyGo](https://tinygo.org/) installed and available in your system's PATH.

### Features shown:
- Direct recursion within Go blocks.
- Importing standard Go packages (like `crypto/sha256`).
- Automatic WebAssembly compilation and caching.

```bash
cd examples/13-native-functions
../../tiny
```
