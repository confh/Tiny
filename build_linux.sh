#!/bin/bash

echo "Building Tiny Linux runtime..."
export GOOS=linux
export GOARCH=amd64
go build -ldflags "-s -w" -o src/embedded/tiny_runtime_linux_amd64 ./src/cmd/tiny_runtime || exit 1
export GOARCH=arm64
go build -ldflags "-s -w" -o src/embedded/tiny_runtime_linux_arm64 ./src/cmd/tiny_runtime || exit 1

echo "Building Tiny Windows runtime..."
export GOOS=windows
export GOARCH=amd64
go build -ldflags "-s -w" -o src/embedded/tiny_runtime_windows_amd64.exe ./src/cmd/tiny_runtime || exit 1

echo "Building Tiny Darwin runtime..."
export GOOS=darwin
export GOARCH=arm64
go build -ldflags "-s -w" -o src/embedded/tiny_runtime_darwin_arm64 ./src/cmd/tiny_runtime || exit 1

echo "Building Tiny compiler..."
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=$(go env GOARCH)
go build -ldflags "-s -w" -o tiny_linux ./src || exit 1

echo "Done."
