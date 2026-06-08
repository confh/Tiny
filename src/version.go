package main

import "fmt"

const TinyVersion = "0.1.8"
const BytecodeCacheVersion = 19

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
