package main

import "fmt"

const TinyVersion = "0.2.5"
const BytecodeCacheVersion = 25

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
