package main

import "fmt"

const TinyVersion = "0.2.9"
const BytecodeCacheVersion = 29

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
