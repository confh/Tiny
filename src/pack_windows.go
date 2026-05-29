//go:build windows
// +build windows

package main

import _ "embed"

//go:embed embedded/tiny_runtime_windows_amd64.exe
var embeddedRuntimeWindowsAMD64 []byte

func getEmbeddedRuntimeForTarget() []byte {
	return embeddedRuntimeWindowsAMD64
}
