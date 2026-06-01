package vm

import (
	webview "github.com/abemedia/go-webview"

	_ "github.com/abemedia/go-webview/embedded"
	. "language.com/src/tinyerrors"
)

var webviewMethods map[string]NativeModuleFunc[*NativeWebViewValue]

func init() {
	webviewMethods = map[string]NativeModuleFunc[*NativeWebViewValue]{
		"setTitle": webviewSetTitle,
		"setSize":  webviewSetSize,
		"setHtml":  webviewSetHtml,
		"navigate": webviewNavigate,
		"run":      webviewRun,
		"callback": webviewCallback,
	}
}

func (vm *VM) callNativeWebviewMethod(webView *NativeWebViewValue, method string, args []TinyValue) {
	fn, ok := webviewMethods[method]
	if !ok {
		vm.fatalError(ErrorName, "unknown webview method: %s", method)
		return
	}
	fn(vm, webView, args)
}

func webviewSetTitle(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.setTitle", args, 1)

	title := argString(vm, "webview.setTitle", args, 0)

	webView.w.SetTitle(title)

	vm.push(NewNull())
}

func webviewSetSize(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.setSize", args, 2)

	w := argInt(vm, "webview.setSize", args, 0)
	h := argInt(vm, "webview.setSize", args, 1)

	webView.w.SetSize(w, h, webview.HintNone)

	vm.push(NewNull())
}

func webviewSetHtml(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.setHtml", args, 1)

	html := argString(vm, "webview.setHtml", args, 0)

	webView.w.SetHtml(html)

	vm.push(NewNull())
}

func webviewNavigate(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.navigate", args, 1)

	url := argString(vm, "webview.navigate", args, 0)

	webView.w.Navigate(url)

	vm.push(NewNull())
}

func webviewRun(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.run", args)

	defer webView.w.Destroy()
	webView.w.Run()

	vm.push(NewNull())
}

func webviewCallback(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.callback", args, 2)

	name := argString(vm, "webview.callback", args, 0)
	callback := argFn(vm, "webview.callback", args, 1)

	webView.w.Bind(name, func(arg string) string {
		result := vm.callFunctionValue(callback, []TinyValue{NewNative(arg)})

		return valueToString(result)
	})

	vm.push(NewNull())
}
