# 15 Runtime VM

This example demonstrates the `runtime` standard library for creating and controlling Tiny VMs from Tiny code.

### Features shown
- Creating isolated VMs with `runtime.newVM`.
- Configuring `allowedStdlib`, `cliArgs`, `globals`, `disableJIT`, and `runMainOnLoad`.
- Loading source directly with `loadSource`.
- Compiling source to bytecode with `runtime.compileSource`.
- Compiling a file to bytecode with `runtime.compileFile`.
- Saving bytecode as `.tbc` with `fs.writeBytes`.
- Loading bytecode with `loadBytecode`.
- Calling exported functions with `call`.
- Injecting globals with `setGlobal`.
- Exposing host callbacks with `exposeFunction`.
- Reusing VMs with `reset`.
- Inspecting VMs with `info`.
- Using `memoryStats`, `gc`, `isPacked`, `lockThread`, `unlockThread`, `onFatal`, and `clearFatalHandler`.

```bash
cd examples/15-runtime-vm
../../tiny
```

The example writes `dist/runtime-child.tbc` at runtime.
