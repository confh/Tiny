package main

import "fmt"

const TinyVersion = "0.2.4"
const BytecodeCacheVersion = 23

func versionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
