package vm

import (
	"github.com/go-vgo/robotgo"
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

func desktopMoveMouse(vm *VM, args []Value) {
	expectArgs(vm, "desktop.moveMouse", args, 2)

	x := argInt(vm, "dekstop.moveMouse", args, 0)
	y := argInt(vm, "dekstop.moveMouse", args, 1)

	robotgo.Move(x, y)

	vm.push(UndefinedValue{})
}

func desktopMoveMouseSmooth(vm *VM, args []Value) {
	expectArgs(vm, "desktop.moveMouseSmooth", args, 2)

	x := argInt(vm, "dekstop.moveMouseSmooth", args, 0)
	y := argInt(vm, "dekstop.moveMouseSmooth", args, 1)

	robotgo.MoveSmooth(x, y)

	vm.push(UndefinedValue{})
}

func desktopMouseClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.click", args)

	err := robotgo.Click()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while clicking: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopMouseRightClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.rightClick", args)

	err := robotgo.Click("right")
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while right clicking: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopMouseDoubleClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.doubleClick", args)

	err := robotgo.MultiClick("left", 2)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while double clicking: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopMouseMouseDown(vm *VM, args []Value) {
	expectArgs(vm, "desktop.mouseDown", args, 1)

	button := argString(vm, "desktop.mouseDown", args, 0)
	if button != "left" && button != "right" {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left' or 'right' in desktop.mouseDown, got %s", button)
	}

	err := robotgo.MouseDown(button)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while performing mouse down: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopMouseMouseUp(vm *VM, args []Value) {
	expectArgs(vm, "desktop.mouseUp", args, 1)

	button := argString(vm, "desktop.mouseUp", args, 0)
	if button != "left" && button != "right" {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left' or 'right' in desktop.mouseUp, got %s", button)
	}

	err := robotgo.MouseUp(button)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while performing mouse up: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopKeyboardPress(vm *VM, args []Value) {
	expectArgs(vm, "desktop.press", args, 1)

	button := argString(vm, "desktop.press", args, 0)

	err := robotgo.KeyPress(button)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while pressing key: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopKeyboardHotKey(vm *VM, args []Value) {
	expectArgs(vm, "desktop.hotKey", args, 2)

	button1 := argString(vm, "desktop.hotKey", args, 0)
	button2 := argString(vm, "desktop.hotKey", args, 1)

	err := robotgo.KeyTap(button1, button2)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while sending hotkey: %s", err)
	}

	vm.push(UndefinedValue{})
}

func desktopKeyboardType(vm *VM, args []Value) {
	expectArgs(vm, "desktop.type", args, 1)

	text := argString(vm, "desktop.type", args, 0)

	robotgo.Type(text)

	vm.push(UndefinedValue{})
}

func desktopMousePosition(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.mousePosition", args)

	x, y := robotgo.Location()

	vm.push(ObjectValue{
		"x": x,
		"y": y,
	})
}

func desktopScreenSize(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.screenSize", args)

	x, y := robotgo.GetScreenSize()

	vm.push(ObjectValue{
		"x": x,
		"y": y,
	})
}

func desktopScreenShot(vm *VM, args []Value) {
	expectArgs(vm, "desktop.screenshot", args, 1)

	fileName := argString(vm, "desktop.screenshot", args, 0)

	img, err := robotgo.CaptureImg()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while capturing the screen: %s", err)
	}

	robotgo.Save(img, fileName)

	vm.push(UndefinedValue{})
}

func desktopGetClipboard(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.getClipboard", args)

	text, err := robotgo.ReadAll()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while reading clipboard: %s", err)
	}

	vm.push(text)
}

func desktopSetClipboard(vm *VM, args []Value) {
	expectArgs(vm, "desktop.setClipboard", args, 1)

	text := argString(vm, "desktop.setClipboard", args, 0)

	err := robotgo.WriteAll(text)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while writing to clipboard: %s", err)
	}

	vm.push(UndefinedValue{})
}
