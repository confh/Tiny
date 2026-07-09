package vm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	. "language.com/src/tinyerrors"

	"github.com/aiwaki/makc"
)

type registeredHotkey struct {
	id       int
	vm       *VM
	ctrl     bool
	shift    bool
	alt      bool
	win      bool
	key      makc.Key
	callback FunctionValue
}

var (
	hotkeyMutex          sync.Mutex
	activeHotkeys        = make(map[int]*registeredHotkey)
	nextHotkeyID         = 1
	globalHotkeyClient   *makc.Client
	globalHotkeyListener *makc.Listener
	globalHotkeyCancel   context.CancelFunc
)

func initKeyMap() {
	// No-op - we use makc.ParseKey directly at runtime
}

func startGlobalListener() error {
	if globalHotkeyListener != nil {
		return nil
	}

	var err error
	globalHotkeyClient, err = makc.Open()
	if err != nil {
		return fmt.Errorf("failed to open makc client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	globalHotkeyCancel = cancel

	globalHotkeyListener, err = globalHotkeyClient.Listen(ctx, makc.ListenOptions{})
	if err != nil {
		cancel()
		globalHotkeyClient = nil
		return fmt.Errorf("failed to start makc listener: %w", err)
	}

	go listenerLoop(globalHotkeyListener.Events)
	return nil
}

func stopGlobalListener() {
	if globalHotkeyListener == nil {
		return
	}
	globalHotkeyCancel()
	globalHotkeyListener.Close()
	globalHotkeyListener = nil
	globalHotkeyClient = nil
	globalHotkeyCancel = nil
}

func listenerLoop(events <-chan makc.InputEvent) {
	ctrlPressed := false
	shiftPressed := false
	altPressed := false
	winPressed := false

	for ev := range events {
		if ev.Kind != makc.InputEventKey {
			continue
		}

		key := uint16(ev.Keyboard.Key)
		state := ev.Keyboard.State
		isDown := (state == makc.Down)

		// 1. Update modifier state
		isCtrl := (key == 17 || key == 162 || key == 163)
		isShift := (key == 16 || key == 160 || key == 161)
		isAlt := (key == 18 || key == 164 || key == 165)
		isWin := (key == 91 || key == 92)

		if isCtrl {
			ctrlPressed = isDown
		} else if isShift {
			shiftPressed = isDown
		} else if isAlt {
			altPressed = isDown
		} else if isWin {
			winPressed = isDown
		}

		// 2. Check if a non-modifier key is pressed
		if isDown && !isCtrl && !isShift && !isAlt && !isWin {
			hotkeyMutex.Lock()
			var matches []*registeredHotkey
			for _, hk := range activeHotkeys {
				if hk.key == ev.Keyboard.Key &&
					hk.ctrl == ctrlPressed &&
					hk.shift == shiftPressed &&
					hk.alt == altPressed &&
					hk.win == winPressed {
					matches = append(matches, hk)
				}
			}
			hotkeyMutex.Unlock()

			// Trigger matches asynchronously
			for _, hk := range matches {
				taskVM := hk.vm.taskPool.Get()
				go func(vm *VM, cb FunctionValue) {
					defer hk.vm.taskPool.Put(vm)
					defer func() {
						if r := recover(); r != nil {
							fmt.Fprintf(os.Stderr, "uncaught async error in hotkey callback: %v\n", r)
						}
					}()
					vm.callFunctionValue(cb, []TinyValue{})
				}(taskVM, hk.callback)
			}
		}
	}
}

func desktopRegisterHotKey(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.registerHotKey", args, 3)

	// 1. Parse modifiers array
	rawMods := argArray(vm, "desktop.registerHotKey", args, 0)
	var ctrl, shift, alt, win bool
	for _, rawMod := range rawMods.Elements {
		modStr, ok := rawMod.Value.(string)
		if !ok {
			vm.runtimeError(ErrorType, "desktop.registerHotKey modifier must be string, got %s", TypeName(rawMod))
			return
		}
		switch strings.ToLower(modStr) {
		case "ctrl", "control":
			ctrl = true
		case "shift":
			shift = true
		case "alt", "option":
			alt = true
		case "win", "super", "meta", "cmd", "command":
			win = true
		default:
			vm.runtimeError(ErrorRuntime, "unknown modifier: %s", modStr)
			return
		}
	}

	// 2. Parse key
	keyStr := strings.ToLower(argString(vm, "desktop.registerHotKey", args, 1))
	// Map some standard alias names for makc compatibility
	switch keyStr {
	case "cmd", "command", "meta", "super", "win":
		keyStr = "leftwindows"
	case "ctrl":
		keyStr = "control"
	case "option":
		keyStr = "alt"
	}

	targetKey, err := makc.ParseKey(keyStr)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "unsupported hotkey key: %s (error: %v)", keyStr, err)
		return
	}

	// 3. Parse callback function
	callback := argFn(vm, "desktop.registerHotKey", args, 2)

	hotkeyMutex.Lock()
	defer hotkeyMutex.Unlock()

	// 4. Start the listener if not already running
	err = startGlobalListener()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "%v", err)
		return
	}

	id := nextHotkeyID
	nextHotkeyID++
	activeHotkeys[id] = &registeredHotkey{
		id:       id,
		vm:       vm,
		ctrl:     ctrl,
		shift:    shift,
		alt:      alt,
		win:      win,
		key:      targetKey,
		callback: callback,
	}

	vm.push(NewInt(id))
}

func desktopUnregisterHotKey(vm *VM, args []TinyValue) {
	expectArgs(vm, "desktop.unregisterHotKey", args, 1)
	id := argInt(vm, "desktop.unregisterHotKey", args, 0)

	hotkeyMutex.Lock()
	_, exists := activeHotkeys[id]
	if exists {
		delete(activeHotkeys, id)
		if len(activeHotkeys) == 0 {
			stopGlobalListener()
		}
	}
	hotkeyMutex.Unlock()

	vm.push(NewNative(exists))
}

func CleanupHotKeysForVM(targetVM *VM) {
	hotkeyMutex.Lock()
	defer hotkeyMutex.Unlock()
	for id, reg := range activeHotkeys {
		if reg.vm == targetVM {
			delete(activeHotkeys, id)
		}
	}
	if len(activeHotkeys) == 0 {
		stopGlobalListener()
	}
}
