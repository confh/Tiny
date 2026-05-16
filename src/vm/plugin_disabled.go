//go:build !windows && !(linux && cgo)
// +build !windows
// +build !linux !cgo

package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callPluginModule(method string, argCount int) {
	LangError(ErrorRuntime, "native plugins are not supported on this build")
}

func (vm *VM) callNativePlugin(plugin *NativePluginValue, method string, args []Value) {
	LangError(ErrorRuntime, "native plugins are not supported on this build")
}
