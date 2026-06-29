# 12 Embed

This example demonstrates the `embedtext` and `embedbytes` keywords, which allow you to embed external files directly into your Tiny program at compile time.

- `embedtext` embeds a file as a constant string.
- `embedbytes` embeds a file as a constant buffer (binary data).
- `embedfolder` embeds a directory a constant object.

This is useful for bundling assets, configuration, or other static data within the executable.

```bash
cd examples/12-embed
../../tiny
```
