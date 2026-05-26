package main

import "fmt"

const TinyVersion = "0.1.2"
const BytecodeCacheVersion = 4

func versionCommand() {
	fmt.Printf("Tiny Version: %s\n", TinyVersion)
}
