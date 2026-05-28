package vm

import "unsafe"

type NativePluginValue struct {
	Path string

	Handle unsafe.Pointer

	Call uintptr
	Free uintptr
}
