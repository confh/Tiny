package main

import "fmt"

const TinyVersion = "0.2.6"
const BytecodeCacheVersion = 26

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
