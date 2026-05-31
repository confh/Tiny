package vm

import (
	webview "github.com/jchv/go-webview-selector"
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

	if len(args) > 0 {
		debug = argBool(vm, "ui.new", args, 0)
	}

	webv := &NativeWebViewValue{
		w: webview.New(debug),
	}
	vm.push(NewNative(webv))
}
