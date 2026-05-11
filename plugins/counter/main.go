package main

/*
#include <stdlib.h>
*/
import "C"

import (
	"unsafe"

	"language.com/compiler/tinyplugin"
)

//export TinyPluginCall
func TinyPluginCall(methodC *C.char, argsC *C.char) *C.char {
	method := C.GoString(methodC)
	argsJSON := C.GoString(argsC)

	result := tinyplugin.HandleCall(method, argsJSON)

	return C.CString(result)
}

//export TinyPluginFree
func TinyPluginFree(ptr *C.char) {
	C.free(unsafe.Pointer(ptr))
}

func main() {}
