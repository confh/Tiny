package main

import "fmt"

const TinyVersion = "0.1.5"
const BytecodeCacheVersion = 14

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
