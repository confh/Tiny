package version

import "fmt"

const TinyVersion = "0.3.1"
const BytecodeCacheVersion = 30

func VersionCommand() {
	fmt.Printf("Tiny Version: %s\nBytecode Version: %d\n", TinyVersion, BytecodeCacheVersion)
}
