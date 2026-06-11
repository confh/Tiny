package main

import "fmt"

const TinyVersion = "0.2.3"
const BytecodeCacheVersion = 22

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
