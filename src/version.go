package main

import "fmt"

const TinyVersion = "0.1.3"
const BytecodeCacheVersion = 10

func versionCommand() {
	fmt.Printf("Tiny Version: %s\n", TinyVersion)
}
