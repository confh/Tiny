//go:build !linux && !windows && !darwin
// +build !linux,!windows,!darwin

package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callPluginModule(method string, argCount int) {
	LangError(ErrorRuntime, "native plugins are not supported on this build")
}

func (vm *VM) callNativePlugin(plugin *NativePluginValue, method string, args []TinyValue) {
	LangError(ErrorRuntime, "native plugins are not supported on this build")
}
