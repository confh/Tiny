package main

import "fmt"

const TinyVersion = "0.2.7"
const BytecodeCacheVersion = 27

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
