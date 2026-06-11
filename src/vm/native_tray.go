//go:build !darwin
// +build !darwin

package vm

import (
	. "language.com/src/tinyerrors"
)

var trayMethods map[string]NativeModuleFunc[*NativeTrayValue]

func init() {
	trayMethods = map[string]NativeModuleFunc[*NativeTrayValue]{
		"setIcon":          traySetIcon,
		"setDarkModeIcon":  traySetDarkModeIcon,
		"setTemplateIcon":  traySetTemplateIcon,
		"setTooltip":       traySetTooltip,
		"setToolTip":       traySetTooltip,
		"add":              trayAdd,
		"addWithIcon":      trayAddWithIcon,
		"addSeparator":     trayAddSeparator,
		"addCheckbox":      trayAddCheckbox,
		"onClick":          trayOnClick,
		"onDoubleClick":    trayOnDoubleClick,
		"onRightClick":     trayOnRightClick,
		"notify":           trayNotify,
		"showNotification": trayNotify,
		"show":             trayShow,
		"hide":             trayHide,
		"run":              trayRun,
		"remove":           trayRemove,
		"quit":             trayRemove,
		"bounds":           trayBounds,
	}
}

func (vm *VM) callNativeTrayMethod(tray *NativeTrayValue, method string, args []TinyValue) {
	fn, ok := trayMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown tray method: %s", method)
		return
	}
	fn(vm, tray, args)
}

func traySetIcon(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.setIcon", args, 1)

	bytes := argBuffer(vm, "tray.setIcon", args, 0)

	tray.Tray.SetIcon(bytes.Bytes)

	vm.push(NewNull())
}

func traySetDarkModeIcon(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.setDarkModeIcon", args, 1)

	bytes := argBuffer(vm, "tray.setDarkModeIcon", args, 0)

	tray.Tray.SetDarkModeIcon(bytes.Bytes)

	vm.push(NewNull())
}

func traySetTemplateIcon(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.setTemplateIcon", args, 1)

	bytes := argBuffer(vm, "tray.setTemplateIcon", args, 0)

	tray.Tray.SetTemplateIcon(bytes.Bytes)

	vm.push(NewNull())
}

func traySetTooltip(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.setTooltip", args, 1)

	tip := argString(vm, "tray.setTooltip", args, 0)

	tray.Tray.SetTooltip(tip)

	vm.push(NewNull())
}

func trayAdd(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.add", args, 2)

	label := argString(vm, "tray.add", args, 0)
	fn := argFn(vm, "tray.add", args, 1)

	tray.Menu.Add(label, func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayAddWithIcon(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.addWithIcon", args, 3)

	label := argString(vm, "tray.addWithIcon", args, 0)
	bytes := argBuffer(vm, "tray.addWithIcon", args, 1)
	fn := argFn(vm, "tray.addWithIcon", args, 2)

	tray.Menu.AddWithIcon(label, bytes.Bytes, func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayAddSeparator(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.addSeparator", args)

	tray.Menu.AddSeparator()

	vm.push(NewNull())
}

func trayAddCheckbox(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.addCheckbox", args, 3)

	label := argString(vm, "tray.addCheckbox", args, 0)
	checked := argBool(vm, "tray.addCheckbox", args, 1)
	fn := argFn(vm, "tray.addCheckbox", args, 2)

	tray.Menu.AddCheckbox(label, checked, func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayOnClick(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.onClick", args, 1)

	fn := argFn(vm, "tray.onClick", args, 0)

	tray.Tray.OnClick(func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayOnDoubleClick(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.onDoubleClick", args, 1)

	fn := argFn(vm, "tray.onDoubleClick", args, 0)

	tray.Tray.OnDoubleClick(func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayOnRightClick(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.onRightClick", args, 1)

	fn := argFn(vm, "tray.onRightClick", args, 0)

	tray.Tray.OnRightClick(func() {
		vm.callFunctionValue(fn, []TinyValue{})
	})

	vm.push(NewNull())
}

func trayNotify(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	expectArgs(vm, "tray.notify", args, 2)

	title := argString(vm, "tray.notify", args, 0)
	message := argString(vm, "tray.notify", args, 1)

	tray.Tray.ShowNotification(title, message)

	vm.push(NewNull())
}

func trayShow(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.show", args)

	tray.Tray.SetMenu(tray.Menu)
	tray.Tray.Show()

	vm.push(NewNull())
}

func trayHide(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.hide", args)

	tray.Tray.Hide()

	vm.push(NewNull())
}

func trayRun(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.run", args)

	if err := tray.Tray.Run(); err != nil {
		vm.runtimeError(ErrorRuntime, "tray.run failed: %v", err)
		return
	}

	vm.push(NewNull())
}

func trayRemove(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.remove", args)

	tray.Tray.Remove()

	vm.push(NewNull())
}

func trayBounds(vm *VM, tray *NativeTrayValue, args []TinyValue) {
	dontExpectArgs(vm, "tray.bounds", args)

	x, y, w, h := tray.Tray.Bounds()

	vm.push(NewNative(ObjectValue{
		"x":      NewInt(x),
		"y":      NewInt(y),
		"width":  NewInt(w),
		"height": NewInt(h),
	}))
}
