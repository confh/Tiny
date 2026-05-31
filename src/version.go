package main

import "fmt"

const TinyVersion = "0.1.6"
const BytecodeCacheVersion = 16

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
