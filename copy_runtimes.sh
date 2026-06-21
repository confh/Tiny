#!/bin/bash

RUNTIMES_DIR="$HOME/.tiny/runtimes"
mkdir -p "$RUNTIMES_DIR"

echo "Copying runtimes to $RUNTIMES_DIR..."

if [ -f "src/embedded/tiny_runtime_windows_amd64.exe" ]; then
    cp "src/embedded/tiny_runtime_windows_amd64.exe" "$RUNTIMES_DIR/tiny_runtime_windows_amd64.exe"
fi
if [ -f "src/embedded/tiny_runtime_linux_amd64" ]; then
    cp "src/embedded/tiny_runtime_linux_amd64" "$RUNTIMES_DIR/tiny_runtime_linux_amd64"
    chmod +x "$RUNTIMES_DIR/tiny_runtime_linux_amd64"
fi
if [ -f "src/embedded/tiny_runtime_linux_arm64" ]; then
    cp "src/embedded/tiny_runtime_linux_arm64" "$RUNTIMES_DIR/tiny_runtime_linux_arm64"
    chmod +x "$RUNTIMES_DIR/tiny_runtime_linux_arm64"
fi
if [ -f "src/embedded/tiny_runtime_darwin_arm64" ]; then
    cp "src/embedded/tiny_runtime_darwin_arm64" "$RUNTIMES_DIR/tiny_runtime_darwin_arm64"
    chmod +x "$RUNTIMES_DIR/tiny_runtime_darwin_arm64"
fi
echo "Done."
