//go:build !windows && !linux && !darwin

package vm

import (
	. "language.com/src/tinyerrors"
)

func desktopMoveMouse(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMoveMouseSmooth(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMouseClick(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMouseRightClick(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMouseDoubleClick(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMouseMouseDown(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMouseMouseUp(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopKeyboardPress(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopKeyboardHotKey(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopKeyboardType(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopMousePosition(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopScreenSize(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopScreenShot(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopGetClipboard(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}

func desktopSetClipboard(vm *VM, args []TinyValue) {
	vm.runtimeError(ErrorRuntime, "desktop stdlib is not supported on this platform")
}
