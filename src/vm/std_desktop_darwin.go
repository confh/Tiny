//go:build darwin

package vm

import (
	"context"
	"image/png"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aiwaki/makc"
	"github.com/atotto/clipboard"
	"github.com/kbinani/screenshot"

	. "language.com/src/tinyerrors"
)

var (
	makcClient *makc.Client
	makcOnce   sync.Once
	makcErr    error
)

func getMakcClient(vm *VM) *makc.Client {
	makcOnce.Do(func() {
		// macOS needs Accessibility permission for mouse/keyboard injection.
		makcClient, makcErr = makc.Open(makc.WithMouseInjection(makc.MouseInjectionAuto))
	})

	if makcErr != nil {
		vm.runtimeError(ErrorRuntime, "failed to initialize desktop module: %v", makcErr)
		return nil
	}

	return makcClient
}

func getMakcKey(key string) (makc.Key, bool) {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "a":
		return makc.KeyA, true
	case "b":
		return makc.KeyB, true
	case "c":
		return makc.KeyC, true
	case "d":
		return makc.KeyD, true
	case "e":
		return makc.KeyE, true
	case "f":
		return makc.KeyF, true
	case "g":
		return makc.KeyG, true
	case "h":
		return makc.KeyH, true
	case "i":
		return makc.KeyI, true
	case "j":
		return makc.KeyJ, true
	case "k":
		return makc.KeyK, true
	case "l":
		return makc.KeyL, true
	case "m":
		return makc.KeyM, true
	case "n":
		return makc.KeyN, true
	case "o":
		return makc.KeyO, true
	case "p":
		return makc.KeyP, true
	case "q":
		return makc.KeyQ, true
	case "r":
		return makc.KeyR, true
	case "s":
		return makc.KeyS, true
	case "t":
		return makc.KeyT, true
	case "u":
		return makc.KeyU, true
	case "v":
		return makc.KeyV, true
	case "w":
		return makc.KeyW, true
	case "x":
		return makc.KeyX, true
	case "y":
		return makc.KeyY, true
	case "z":
		return makc.KeyZ, true
	case "0":
		return makc.Key0, true
	case "1":
		return makc.Key1, true
	case "2":
		return makc.Key2, true
	case "3":
		return makc.Key3, true
	case "4":
		return makc.Key4, true
	case "5":
		return makc.Key5, true
	case "6":
		return makc.Key6, true
	case "7":
		return makc.Key7, true
	case "8":
		return makc.Key8, true
	case "9":
		return makc.Key9, true
	case "enter", "return":
		return makc.KeyEnter, true
	case "esc", "escape":
		return makc.KeyEscape, true
	case "space":
		return makc.KeySpace, true
	case "backspace":
		return makc.KeyBackspace, true
	case "tab":
		return makc.KeyTab, true
	case "shift":
		return makc.KeyLeftShift, true
	case "rightshift", "shiftright":
		return makc.KeyRightShift, true
	case "ctrl", "control":
		return makc.KeyLeftControl, true
	case "rightctrl", "rightcontrol", "controlright":
		return makc.KeyRightControl, true
	case "alt", "option":
		return makc.KeyLeftAlt, true
	case "rightalt", "rightoption", "altright":
		return makc.KeyRightAlt, true
	case "cmd", "command", "win", "super", "meta":
		return makc.KeyLeftWindows, true
	case "rightcmd", "rightcommand", "rightwin", "rightsuper", "rightmeta":
		return makc.KeyRightWindows, true
	case "up":
		return makc.KeyUp, true
	case "down":
		return makc.KeyDown, true
	case "left":
		return makc.KeyLeft, true
	case "right":
		return makc.KeyRight, true
	case "home":
		return makc.KeyHome, true
	case "end":
		return makc.KeyEnd, true
	case "pageup", "pgup":
		return makc.KeyPageUp, true
	case "pagedown", "pgdown":
		return makc.KeyPageDown, true
	case "insert", "ins":
		return makc.KeyInsert, true
	case "delete", "del":
		return makc.KeyDelete, true
	case "capslock":
		return makc.KeyCapsLock, true
	case "f1":
		return makc.KeyF1, true
	case "f2":
		return makc.KeyF2, true
	case "f3":
		return makc.KeyF3, true
	case "f4":
		return makc.KeyF4, true
	case "f5":
		return makc.KeyF5, true
	case "f6":
		return makc.KeyF6, true
	case "f7":
		return makc.KeyF7, true
	case "f8":
		return makc.KeyF8, true
	case "f9":
		return makc.KeyF9, true
	case "f10":
		return makc.KeyF10, true
	case "f11":
		return makc.KeyF11, true
	case "f12":
		return makc.KeyF12, true
	case "-", "minus":
		return makc.KeyMinus, true
	case "=", "equals":
		return makc.KeyEquals, true
	case "[", "leftbracket":
		return makc.KeyLeftSquareBracket, true
	case "]", "rightbracket":
		return makc.KeyRightSquareBracket, true
	case "\\", "backslash":
		return makc.KeyBackslash, true
	case ";", "semicolon":
		return makc.KeySemicolon, true
	case "'", "quote", "singlequote":
		return makc.KeySingleQuote, true
	case ",", "comma":
		return makc.KeyComma, true
	case ".", "dot", "period":
		return makc.KeyDot, true
	case "/", "slash":
		return makc.KeySlash, true
	case "`", "backquote", "grave":
		return makc.KeyBackQuote, true
	}

	return makc.KeyUnknown, false
}

func getMakcButton(button string) (makc.MouseButton, bool) {
	switch strings.ToLower(strings.TrimSpace(button)) {
	case "left":
		return makc.ButtonLeft, true
	case "right":
		return makc.ButtonRight, true
	case "middle":
		return makc.ButtonMiddle, true
	}

	return 0, false
}

func desktopMoveMouse(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.moveMouse", args, 2)
	x := argInt(vm, "desktop.moveMouse", args, 0)
	y := argInt(vm, "desktop.moveMouse", args, 1)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.MoveTo(context.Background(), x, y); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.moveMouse failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMoveMouseSmooth(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.moveMouseSmooth", args, 2)
	targetX := argInt(vm, "desktop.moveMouseSmooth", args, 0)
	targetY := argInt(vm, "desktop.moveMouseSmooth", args, 1)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	ctx := context.Background()
	pos, err := client.Mouse.Position(ctx)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.moveMouseSmooth failed to get mouse position: %v", err)
		return
	}

	currentX := pos.X
	currentY := pos.Y
	steps := 15

	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(float64(currentX) + t*float64(targetX-currentX))
		y := int(float64(currentY) + t*float64(targetY-currentY))

		if err := client.Mouse.MoveTo(ctx, x, y); err != nil {
			vm.runtimeError(ErrorRuntime, "desktop.moveMouseSmooth failed: %v", err)
			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	vm.push(NewNull())
}

func desktopMouseClick(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.click", args)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.Click(context.Background(), makc.ButtonLeft); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.click failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMouseRightClick(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.rightClick", args)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.Click(context.Background(), makc.ButtonRight); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.rightClick failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMouseDoubleClick(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.doubleClick", args)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.DoubleClick(context.Background(), makc.ButtonLeft, 25*time.Millisecond, 50*time.Millisecond); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.doubleClick failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMouseMouseDown(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.mouseDown", args, 1)
	button := argString(vm, "desktop.mouseDown", args, 0)

	btn, ok := getMakcButton(button)
	if !ok {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left', 'right', or 'middle' in desktop.mouseDown, got %s", button)
		return
	}

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.Press(context.Background(), btn); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.mouseDown failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMouseMouseUp(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.mouseUp", args, 1)
	button := argString(vm, "desktop.mouseUp", args, 0)

	btn, ok := getMakcButton(button)
	if !ok {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left', 'right', or 'middle' in desktop.mouseUp, got %s", button)
		return
	}

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Mouse.Release(context.Background(), btn); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.mouseUp failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopKeyboardPress(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.press", args, 1)
	keyStr := argString(vm, "desktop.press", args, 0)

	key, ok := getMakcKey(keyStr)
	if !ok {
		vm.runtimeError(ErrorRuntime, "unknown key in desktop.press: %s", keyStr)
		return
	}

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Keyboard.Tap(context.Background(), key); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.press failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopKeyboardHotKey(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.hotKey", args, 2)

	keyStr1 := argString(vm, "desktop.hotKey", args, 0)
	keyStr2 := argString(vm, "desktop.hotKey", args, 1)

	k1, ok := getMakcKey(keyStr1)
	if !ok {
		vm.runtimeError(ErrorRuntime, "unknown first key in desktop.hotKey: %s", keyStr1)
		return
	}

	k2, ok := getMakcKey(keyStr2)
	if !ok {
		vm.runtimeError(ErrorRuntime, "unknown second key in desktop.hotKey: %s", keyStr2)
		return
	}

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Keyboard.Combo(context.Background(), k1, k2); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.hotKey failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopKeyboardType(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.type", args, 1)
	text := argString(vm, "desktop.type", args, 0)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	if err := client.Keyboard.TypeText(context.Background(), text); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.type failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopMousePosition(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.mousePosition", args)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	pos, err := client.Mouse.Position(context.Background())
	if err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.mousePosition failed: %v", err)
		return
	}

	vm.push(NewNative(ObjectValue{
		"x": NewInt(pos.X),
		"y": NewInt(pos.Y),
	}))
}

func desktopScreenSize(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.screenSize", args)

	client := getMakcClient(vm)
	if client != nil {
		if size, err := client.Mouse.ScreenSize(context.Background()); err == nil {
			vm.push(NewNative(ObjectValue{
				"x": NewInt(size.X),
				"y": NewInt(size.Y),
			}))
			return
		}
	}

	// Fallback for cases where mouse backend cannot report screen size.
	bounds := screenshot.GetDisplayBounds(0)
	if bounds.Empty() {
		vm.runtimeError(ErrorRuntime, "desktop.screenSize failed: no display found")
		return
	}

	vm.push(NewNative(ObjectValue{
		"x": NewInt(bounds.Dx()),
		"y": NewInt(bounds.Dy()),
	}))
}

func desktopScreenShot(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.screenshot", args, 1)
	fileName := argString(vm, "desktop.screenshot", args, 0)

	bounds := screenshot.GetDisplayBounds(0)
	if bounds.Empty() {
		vm.runtimeError(ErrorRuntime, "desktop.screenshot failed: no display found")
		return
	}

	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.screenshot failed to capture screen: %v", err)
		return
	}

	file, err := os.Create(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.screenshot failed to create file %s: %v", fileName, err)
		return
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.screenshot failed to save PNG: %v", err)
		return
	}

	vm.push(NewNull())
}

func desktopGetClipboard(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "desktop.getClipboard", args)

	text, err := clipboard.ReadAll()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.getClipboard failed: %v", err)
		return
	}

	vm.push(NewNative(text))
}

func desktopSetClipboard(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.setClipboard", args, 1)
	text := argString(vm, "desktop.setClipboard", args, 0)

	if err := clipboard.WriteAll(text); err != nil {
		vm.runtimeError(ErrorRuntime, "desktop.setClipboard failed: %v", err)
		return
	}

	vm.push(NewNull())
}
