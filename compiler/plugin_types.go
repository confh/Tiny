package main

import "unsafe"

type NativePluginValue struct {
	Path string

	// Windows uses uintptr for syscall.SyscallN.
	// Linux/macOS can convert C function pointers to uintptr too.
	Handle unsafe.Pointer

	Call uintptr
	Free uintptr
}
