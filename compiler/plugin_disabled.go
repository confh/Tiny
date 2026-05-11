//go:build !windows && !(linux && cgo)
// +build !windows
// +build !linux !cgo

package main

func (vm *VM) callPluginModule(method string, argCount int) {
	langError(ErrorRuntime, "native plugins are not supported on this build")
}

func (vm *VM) callNativePlugin(plugin *NativePluginValue, method string, args []Value) {
	langError(ErrorRuntime, "native plugins are not supported on this build")
}
