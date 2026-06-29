//go:build !windows && !linux

package vm

import (
	webview "github.com/abemedia/go-webview"
)

func (webView *NativeWebViewValue) setFrameless(frameless bool) {}
func (webView *NativeWebViewValue) refreshWindowChrome()        {}
func (webView *NativeWebViewValue) hide()                       {}
func (webView *NativeWebViewValue) show()                       {}
func (webView *NativeWebViewValue) startDrag()                  {}
func (webView *NativeWebViewValue) startResize(edge string)     {}
func (webView *NativeWebViewValue) minimize()                   {}
func (webView *NativeWebViewValue) maximize()                   {}
func (webView *NativeWebViewValue) restore()                    {}
func (webView *NativeWebViewValue) toggleMaximize()             {}

func (webView *NativeWebViewValue) close() {
	if webView != nil && webView.w != nil {
		webView.w.Terminate()
	}
}

func createPlatformWebView(webView *NativeWebViewValue, debug bool, hidden bool, frameless bool) PlatformWebView {
	return wrapWebView(webview.New(debug))
}

func (webView *NativeWebViewValue) center(w, h int) {
	webView.w.SetSize(w, h, HintNone)
}

func (webView *NativeWebViewValue) smoothResize(w, h int) {
	webView.w.SetSize(w, h, HintNone)
}
