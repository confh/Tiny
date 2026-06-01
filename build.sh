#!/bin/bash

echo "Building Tiny Linux runtime..."
export GOOS=linux
export GOARCH=amd64
go build -ldflags "-s -w" -o src/embedded/tiny_runtime_linux_amd64 ./src/cmd/tiny_runtime || exit 1

echo "Building Tiny compiler..."
export CGO_ENABLED=0
export GOOS=linux
export GOARCH=amd64
go build -ldflags "-s -w" -o tiny_linux ./src || exit 1

echo "Done."