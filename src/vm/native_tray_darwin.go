//go:build darwin
// +build darwin

package vm

import . "language.com/src/tinyerrors"

func (vm *VM) callNativeTrayMethod(tray *NativeTrayValue, method string, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "tray is not supported in the Darwin runtime")
}
