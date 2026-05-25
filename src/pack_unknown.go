//go:build !linux && !windows
// +build !linux,!windows

package main

import . "language.com/src/tinyerrors"

func getEmbeddedRuntimeForTarget(target string) []byte {
	LangError(ErrorRuntime, "unsupported target: %s", target)
	return nil
}
