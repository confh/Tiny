package main

import "fmt"

const TinyVersion = "0.1.7"
const BytecodeCacheVersion = 18

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
