package vm

import (
	"fmt"
	"strconv"
	"time"

	. "language.com/src/tinyerrors"
)

var webviewMethods map[string]NativeModuleFunc[*NativeWebViewValue]

func init() {
	webviewMethods = map[string]NativeModuleFunc[*NativeWebViewValue]{
		"setTitle":       webviewSetTitle,
		"setSize":        webviewSetSize,
		"setHtml":        webviewSetHtml,
		"navigate":       webviewNavigate,
		"run":            webviewRun,
		"callback":       webviewCallback,
		"setFrameless":   webviewSetFrameless,
		"hide":           webviewHide,
		"show":           webviewShow,
		"startDrag":      webviewStartDrag,
		"startResize":    webviewStartResize,
		"minimize":       webviewMinimize,
		"maximize":       webviewMaximize,
		"restore":        webviewRestore,
		"toggleMaximize": webviewToggleMaximize,
		"close":          webviewClose,
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

	webView.width = w
	webView.height = h

	webView.smoothResize(w, h)

	vm.push(NewNull())
}

func webviewSetHtml(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.setHtml", args, 1)

	html := argString(vm, "webview.setHtml", args, 0)

	webView.w.SetHtml(html)
	webView.refreshWindowChrome()
	webView.showAfterContentReady()

	vm.push(NewNull())
}

func webviewNavigate(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.navigate", args, 1)

	url := argString(vm, "webview.navigate", args, 0)

	webView.w.Navigate(url)
	webView.showAfterContentReady()

	vm.push(NewNull())
}

func (webView *NativeWebViewValue) showAfterContentReady() {
	if webView.userWantedHidden {
		return
	}
	go func() {
		time.Sleep(350 * time.Millisecond)
		webView.show()
	}()
}

func webviewRun(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.run", args)

	defer func() {
		webView.destroyIconHandles()
		webView.w.Destroy()
	}()

	webView.w.Run()

	vm.push(NewNull())
}

func webviewCallback(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.callback", args, 2)

	name := argString(vm, "webview.callback", args, 0)
	callback := argFn(vm, "webview.callback", args, 1)

	webView.w.Bind(name, func(arg string) (resultText string) {
		defer func() {
			if r := recover(); r != nil {
				resultText = `{"status":500,"body":` + strconv.Quote(fmt.Sprint(r)) + `}`
			}
		}()

		result := vm.callFunctionValue(callback, []TinyValue{NewNative(arg)})

		return valueToString(result)
	})

	vm.push(NewNull())
}

func webviewSetFrameless(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.setFrameless", args, 1)
	webView.setFrameless(argBool(vm, "webview.setFrameless", args, 0))
	vm.push(NewNull())
}

func webviewHide(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.hide", args)
	webView.hide()
	vm.push(NewNull())
}

func webviewShow(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.show", args)
	webView.show()
	vm.push(NewNull())
}

func webviewStartDrag(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.startDrag", args)
	webView.startDrag()
	vm.push(NewNull())
}

func webviewStartResize(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	expectArgs(vm, "webview.startResize", args, 1)
	webView.startResize(argString(vm, "webview.startResize", args, 0))
	vm.push(NewNull())
}

func webviewMinimize(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.minimize", args)
	webView.minimize()
	vm.push(NewNull())
}

func webviewMaximize(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.maximize", args)
	webView.maximize()
	vm.push(NewNull())
}

func webviewRestore(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.restore", args)
	webView.restore()
	vm.push(NewNull())
}

func webviewToggleMaximize(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.toggleMaximize", args)
	webView.toggleMaximize()
	vm.push(NewNull())
}

func webviewClose(vm *VM, webView *NativeWebViewValue, args []TinyValue) {
	dontExpectArgs(vm, "webview.close", args)
	webView.close()
	vm.push(NewNull())
}
