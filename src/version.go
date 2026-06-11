package main

import "fmt"

const TinyVersion = "0.2.2"
const BytecodeCacheVersion = 21

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
