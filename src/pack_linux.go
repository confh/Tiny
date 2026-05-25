//go:build linux
// +build linux

package main

import _ "embed"

//go:embed embedded/tiny_runtime_linux_amd64
var embeddedRuntimeLinuxAMD64 []byte

func getEmbeddedRuntimeForTarget(target string) []byte {
	return embeddedRuntimeLinuxAMD64
}
