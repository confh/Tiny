package vm

import (
	. "language.com/src/tinyerrors"
)

var stdDesktopMetadata = StdModuleInfo{
	Name: "desktop",
}

var stdDesktopMethods map[string]StdModuleFunc

func init() {
	stdDesktopMethods = map[string]StdModuleFunc{
		"moveMouse":       desktopMoveMouse,
		"moveMouseSmooth": desktopMoveMouseSmooth,
		"click":           desktopMouseClick,
		"rightClick":      desktopMouseRightClick,
		"doubleClick":     desktopMouseDoubleClick,
		"mouseDown":       desktopMouseMouseDown,
		"mouseUp":         desktopMouseMouseUp,
		"press":           desktopKeyboardPress,
		"hotKey":          desktopKeyboardHotKey,
		"type":            desktopKeyboardType,
		"mousePosition":   desktopMousePosition,
		"screenSize":      desktopScreenSize,
		"screenshot":      desktopScreenShot,
		"getClipboard":    desktopGetClipboard,
		"setClipboard":    desktopSetClipboard,
	}
	registerStdModule(stdDesktopMetadata)
}

func (vm *VM) callStdDesktop(method string, args []TinyValue) {
	fn, ok := stdDesktopMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown desktop function: %s", method)
		return
	}

	fn(vm, args)
}
