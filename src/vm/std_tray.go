//go:build !darwin
// +build !darwin

package vm

import (
	"github.com/gogpu/systray"
	. "language.com/src/tinyerrors"
)

var stdTrayMetadata = StdModuleInfo{
	Name: "tray",
}

var stdTrayMethods map[string]StdModuleFunc

func init() {
	stdTrayMethods = map[string]StdModuleFunc{
		"new": trayNew,
	}
	registerStdModule(stdTrayMetadata)
}

func (vm *VM) callStdTray(method string, args []TinyValue) {
	fn, ok := stdTrayMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown tray function: %s", method)
		return
	}

	fn(vm, args)
}

func trayNew(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "tray.new", args)

	vm.push(NewNative(&NativeTrayValue{
		Tray: systray.New(),
		Menu: systray.NewMenu(),
	}))
}
