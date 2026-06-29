package vm

import (
	. "language.com/src/tinyerrors"
)

func (v *NativeWebViewValue) TinyTypeName() string {
	return "ui.WebView"
}

var stdUiMetadata = StdModuleInfo{
	Name: "ui",
}

var stdUiMethods map[string]StdModuleFunc

func init() {
	stdUiMethods = map[string]StdModuleFunc{
		"new": uiNew,
	}
	registerStdModule(stdUiMetadata)
}

func (vm *VM) callStdUi(method string, args []TinyValue) {
	fn, ok := stdUiMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown ui function: %s", method)
		return
	}

	fn(vm, args)
}

func uiNew(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "ui.new", args, 0, 1)

	debug := false
	hidden := false
	frameless := false
	width := 800
	height := 600

	if len(args) > 0 {
		if _, ok := args[0].Value.(bool); ok {
			debug = argBool(vm, "ui.new", args, 0)
		} else if options, ok := vm.valueAsObjectForRead(args[0]); ok {
			debug = objectBool(options, "debug", false)
			hidden = objectBool(options, "hidden", false)
			frameless = objectBool(options, "frameless", false)
			width = objectInt(vm, options, "width", 800)
			height = objectInt(vm, options, "height", 600)
		} else {
			vm.runtimeError(ErrorType, "ui.new expects a bool or object")
			return
		}
	}

	webv := &NativeWebViewValue{
		hidden:           true,
		userWantedHidden: hidden,
		width:            width,
		height:           height,
	}

	webv.w = createPlatformWebView(webv, debug, true, frameless)

	webv.applyExecutableIcon()
	if frameless {
		webv.setFrameless(true)
	}
	if hidden {
		webv.hide()
	}
	vm.push(NewNative(webv))
}
