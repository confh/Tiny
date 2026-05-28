package main

import "fmt"

const TinyVersion = "0.1.4"
const BytecodeCacheVersion = 13

func versionCommand() {
	fmt.Printf("Tiny Version: %s\n", TinyVersion)
}
