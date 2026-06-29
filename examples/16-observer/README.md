# 16 Observer

This example demonstrates the `observer` standard library by starting an inspectable Tiny process with live status, messages, events, commands, exposed functions, editable globals, and a shutdown hook.

### Features shown
- Starting the observer HTTP endpoint with `observer.start`.
- Publishing status with `observer.status`.
- Recording timeline events with `observer.event`.
- Sending log-style messages with `observer.message`.
- Registering remote commands with `observer.command`.
- Exposing callable functions with `observer.expose`.
- Handling remote shutdown with `observer.onShutdown`.
- Running background work with `spawn`.

```bash
cd examples/16-observer
../../tiny
```

Then open the Observer UI from the repository root:

```bash
cd ../../observer
../tiny src/main.tiny http://127.0.0.1:4040 tiny
```

The observer password is `tiny`.
