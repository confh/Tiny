package vm

import (
	. "language.com/src/tinyerrors"
)

var stdDesktopMetadata = StdModuleInfo{
	Name: "desktop",
	Methods: map[string]StdMethodInfo{
		"moveMouse": {
			Name: "moveMouse",
			Args: []StdArg{
				{Name: "x", Type: "number"},
				{Name: "y", Type: "number"},
			},
			Returns:     "undefined",
			Description: "Moves the mouse pointer to the given screen coordinates (x, y).",
		},
		"moveMouseSmooth": {
			Name: "moveMouseSmooth",
			Args: []StdArg{
				{Name: "x", Type: "number"},
				{Name: "y", Type: "number"},
			},
			Returns:     "undefined",
			Description: "Smoothly moves the mouse pointer to the given (x, y) coordinates.",
		},
		"click": {
			Name:        "click",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Performs a left mouse button click at the current mouse position.",
		},
		"rightClick": {
			Name:        "rightClick",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Performs a right mouse button click at the current mouse position.",
		},
		"doubleClick": {
			Name:        "doubleClick",
			Args:        []StdArg{},
			Returns:     "undefined",
			Description: "Performs a double left mouse button click at the current mouse position.",
		},
		"mouseDown": {
			Name: "mouseDown",
			Args: []StdArg{
				{Name: "button", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Presses down the specified mouse button ('left' or 'right').",
		},
		"mouseUp": {
			Name: "mouseUp",
			Args: []StdArg{
				{Name: "button", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Releases the specified mouse button ('left' or 'right').",
		},
		"press": {
			Name: "press",
			Args: []StdArg{
				{Name: "key", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Presses the specified keyboard key.",
		},
		"hotKey": {
			Name: "hotKey",
			Args: []StdArg{
				{Name: "key1", Type: "string"},
				{Name: "key2", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Sends a hotkey combination (e.g., Ctrl+C) using two keys.",
		},
		"type": {
			Name: "type",
			Args: []StdArg{
				{Name: "text", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Types the given text using the keyboard.",
		},
		"mousePosition": {
			Name:        "mousePosition",
			Args:        []StdArg{},
			Returns:     "object",
			Description: "Returns the current mouse cursor position as an object with x and y fields.",
		},
		"screenSize": {
			Name:        "screenSize",
			Args:        []StdArg{},
			Returns:     "object",
			Description: "Returns the width and height of the primary screen as an object with x and y fields.",
		},
		"screenshot": {
			Name: "screenshot",
			Args: []StdArg{
				{Name: "filename", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Saves a screenshot of the screen to the specified file.",
		},
		"getClipboard": {
			Name:        "getClipboard",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Returns the current text stored in the system clipboard.",
		},
		"setClipboard": {
			Name: "setClipboard",
			Args: []StdArg{
				{Name: "text", Type: "string"},
			},
			Returns:     "undefined",
			Description: "Sets the system clipboard to the provided text.",
		},
	},
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

func (vm *VM) callStdDesktop(method string, args []Value) {
	fn, ok := stdDesktopMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown desktop function: %s", method)
		return
	}

	fn(vm, args)
}
