# 14 WebView

This example shows how to build a desktop GUI application using the `ui` module and `embeddir`. 

- **Desktop Window:** Uses `ui.new()` to create a native WebView window.
- **Asset Embedding:** Uses `embeddir` to bundle the entire `ui/` folder (HTML and CSS) into the executable.
- **JS-to-Tiny Callbacks:** Uses `w.callback()` to expose Tiny functions to JavaScript.

```bash
cd examples/14-webview
../../tiny
```

### Building a Windowed Executable (Windows)
To create a standalone EXE that doesn't spawn a console window:
```bash
../../tiny dist --windowed
```
