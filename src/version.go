package main

import "fmt"

const TinyVersion = "0.1.9"
const BytecodeCacheVersion = 20

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
