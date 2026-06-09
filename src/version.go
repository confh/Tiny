package main

import "fmt"

const TinyVersion = "0.2.0"
const BytecodeCacheVersion = 21

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
