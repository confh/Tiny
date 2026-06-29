//go:build !windows

package main

import "fmt"

func applyWindowsIconToPERuntimeBytes(runtimeBytes []byte, iconPath string) ([]byte, error) {
	return nil, fmt.Errorf("--icon can only be applied by tiny running on Windows")
}
