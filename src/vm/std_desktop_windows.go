//go:build windows

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
	"golang.org/x/sys/windows"

	. "language.com/src/tinyerrors"
)

var (
	makcClient *makc.Client
	makcOnce   sync.Once
	makcErr    error
)

func getMakcClient(vm *VM) *makc.Client {
	makcOnce.Do(func() {
		makcClient, makcErr = makc.Open()
	})
	if makcErr != nil {
		vm.runtimeError(ErrorRuntime, "failed to initialize desktop module: %v", makcErr)
		return nil
	}
	return makcClient
}

func getMakcKey(key string) makc.Key {
	switch strings.ToLower(key) {
	case "a":
		return makc.KeyA
	case "b":
		return makc.KeyB
	case "c":
		return makc.KeyC
	case "d":
		return makc.KeyD
	case "e":
		return makc.KeyE
	case "f":
		return makc.KeyF
	case "g":
		return makc.KeyG
	case "h":
		return makc.KeyH
	case "i":
		return makc.KeyI
	case "j":
		return makc.KeyJ
	case "k":
		return makc.KeyK
	case "l":
		return makc.KeyL
	case "m":
		return makc.KeyM
	case "n":
		return makc.KeyN
	case "o":
		return makc.KeyO
	case "p":
		return makc.KeyP
	case "q":
		return makc.KeyQ
	case "r":
		return makc.KeyR
	case "s":
		return makc.KeyS
	case "t":
		return makc.KeyT
	case "u":
		return makc.KeyU
	case "v":
		return makc.KeyV
	case "w":
		return makc.KeyW
	case "x":
		return makc.KeyX
	case "y":
		return makc.KeyY
	case "z":
		return makc.KeyZ
	case "0":
		return makc.Key0
	case "1":
		return makc.Key1
	case "2":
		return makc.Key2
	case "3":
		return makc.Key3
	case "4":
		return makc.Key4
	case "5":
		return makc.Key5
	case "6":
		return makc.Key6
	case "7":
		return makc.Key7
	case "8":
		return makc.Key8
	case "9":
		return makc.Key9
	case "enter", "return":
		return makc.KeyEnter
	case "esc", "escape":
		return makc.KeyEscape
	case "space":
		return makc.KeySpace
	case "backspace":
		return makc.KeyBackspace
	case "tab":
		return makc.KeyTab
	case "shift":
		return makc.KeyLeftShift
	case "ctrl", "control":
		return makc.KeyLeftControl
	case "alt":
		return makc.KeyLeftAlt
	case "win", "command", "super":
		return makc.KeyLeftWindows
	case "up":
		return makc.KeyUp
	case "down":
		return makc.KeyDown
	case "left":
		return makc.KeyLeft
	case "right":
		return makc.KeyRight
	}
	return makc.Key(0)
}

func desktopMoveMouse(vm *VM, args []Value) {
	expectArgs(vm, "desktop.moveMouse", args, 2)
	x := argInt(vm, "desktop.moveMouse", args, 0)
	y := argInt(vm, "desktop.moveMouse", args, 1)

	client := getMakcClient(vm)
	if client == nil {
		return
	}
	client.Mouse.MoveTo(context.Background(), x, y)
	vm.push(NewUndefined())
}

func desktopMoveMouseSmooth(vm *VM, args []Value) {
	expectArgs(vm, "desktop.moveMouseSmooth", args, 2)
	targetX := argInt(vm, "desktop.moveMouseSmooth", args, 0)
	targetY := argInt(vm, "desktop.moveMouseSmooth", args, 1)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	ctx := context.Background()
	pos, _ := client.Mouse.Position(ctx)
	currentX := pos.X
	currentY := pos.Y

	steps := 15
	for i := 1; i <= steps; i++ {
		t := float64(i) / float64(steps)
		x := int(float64(currentX) + t*float64(targetX-currentX))
		y := int(float64(currentY) + t*float64(targetY-currentY))
		client.Mouse.MoveTo(ctx, x, y)
		time.Sleep(10 * time.Millisecond)
	}
	vm.push(NewUndefined())
}

func desktopMouseClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.click", args)
	client := getMakcClient(vm)
	if client == nil {
		return
	}
	client.Mouse.Click(context.Background(), makc.ButtonLeft)
	vm.push(NewUndefined())
}

func desktopMouseRightClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.rightClick", args)
	client := getMakcClient(vm)
	if client == nil {
		return
	}
	client.Mouse.Click(context.Background(), makc.ButtonRight)
	vm.push(NewUndefined())
}

func desktopMouseDoubleClick(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.doubleClick", args)
	client := getMakcClient(vm)
	if client == nil {
		return
	}
	ctx := context.Background()
	client.Mouse.Click(ctx, makc.ButtonLeft)
	time.Sleep(50 * time.Millisecond)
	client.Mouse.Click(ctx, makc.ButtonLeft)
	vm.push(NewUndefined())
}

func desktopMouseMouseDown(vm *VM, args []Value) {
	expectArgs(vm, "desktop.mouseDown", args, 1)
	button := argString(vm, "desktop.mouseDown", args, 0)
	if button != "left" && button != "right" {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left' or 'right' in desktop.mouseDown, got %s", button)
		return
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	mouseEvent := user32.NewProc("mouse_event")
	var flags uintptr = 0x0002 // LEFTDOWN
	if button == "right" {
		flags = 0x0008 // RIGHTDOWN
	}
	mouseEvent.Call(flags, 0, 0, 0, 0)
	vm.push(NewUndefined())
}

func desktopMouseMouseUp(vm *VM, args []Value) {
	expectArgs(vm, "desktop.mouseUp", args, 1)
	button := argString(vm, "desktop.mouseUp", args, 0)
	if button != "left" && button != "right" {
		vm.runtimeError(ErrorRuntime, "expected button to be 'left' or 'right' in desktop.mouseUp, got %s", button)
		return
	}

	user32 := windows.NewLazySystemDLL("user32.dll")
	mouseEvent := user32.NewProc("mouse_event")
	var flags uintptr = 0x0004 // LEFTUP
	if button == "right" {
		flags = 0x0010 // RIGHTUP
	}
	mouseEvent.Call(flags, 0, 0, 0, 0)
	vm.push(NewUndefined())
}

func desktopKeyboardPress(vm *VM, args []Value) {
	expectArgs(vm, "desktop.press", args, 1)
	keyStr := argString(vm, "desktop.press", args, 0)
	client := getMakcClient(vm)
	if client == nil {
		return
	}
	client.Keyboard.Tap(context.Background(), getMakcKey(keyStr))
	vm.push(NewUndefined())
}

func desktopKeyboardHotKey(vm *VM, args []Value) {
	expectArgs(vm, "desktop.hotKey", args, 2)
	k1 := getMakcKey(argString(vm, "desktop.hotKey", args, 0))
	k2 := getMakcKey(argString(vm, "desktop.hotKey", args, 1))

	client := getMakcClient(vm)
	if client == nil {
		return
	}
	ctx := context.Background()
	client.Keyboard.Down(ctx, k1)
	client.Keyboard.Tap(ctx, k2)
	client.Keyboard.Release(ctx, k1)
	vm.push(NewUndefined())
}

func desktopKeyboardType(vm *VM, args []Value) {
	expectArgs(vm, "desktop.type", args, 1)
	text := argString(vm, "desktop.type", args, 0)

	client := getMakcClient(vm)
	if client == nil {
		return
	}

	ctx := context.Background()

	for _, char := range text {
		err := client.Keyboard.TypeText(ctx, string(char))
		if err != nil {
			vm.runtimeError(ErrorRuntime, "error while typing text: %s", err)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	vm.push(NewUndefined())
}

func desktopMousePosition(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.mousePosition", args)
	client := getMakcClient(vm)
	if client == nil {
		return
	}
	pos, _ := client.Mouse.Position(context.Background())
	vm.push(NewNative(ObjectValue{
		"x": NewInt(pos.X),
		"y": NewInt(pos.Y),
	}))
}

func desktopScreenSize(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.screenSize", args)
	user32 := windows.NewLazySystemDLL("user32.dll")
	getSystemMetrics := user32.NewProc("GetSystemMetrics")
	w, _, _ := getSystemMetrics.Call(0)
	h, _, _ := getSystemMetrics.Call(1)

	vm.push(NewNative(ObjectValue{
		"x": NewInt(int(w)),
		"y": NewInt(int(h)),
	}))
}

func desktopScreenShot(vm *VM, args []Value) {
	expectArgs(vm, "desktop.screenshot", args, 1)
	fileName := argString(vm, "desktop.screenshot", args, 0)

	bounds := screenshot.GetDisplayBounds(0)
	img, err := screenshot.CaptureRect(bounds)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error capturing: %s", err)
		return
	}
	file, _ := os.Create(fileName)
	defer file.Close()
	png.Encode(file, img)
	vm.push(NewUndefined())
}

func desktopGetClipboard(vm *VM, args []Value) {
	dontExpectArgs(vm, "desktop.getClipboard", args)
	text, _ := clipboard.ReadAll()
	vm.push(NewNative(text))
}

func desktopSetClipboard(vm *VM, args []Value) {
	expectArgs(vm, "desktop.setClipboard", args, 1)
	text := argString(vm, "desktop.setClipboard", args, 0)
	clipboard.WriteAll(text)
	vm.push(NewUndefined())
}
