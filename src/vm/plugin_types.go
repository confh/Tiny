package vm

import "unsafe"

type NativePluginValue struct {
	Path      string
	IsMsgPack bool

	Call uintptr
	Free uintptr

	Handle unsafe.Pointer
}
