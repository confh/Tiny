//go:build !windows

package vm

import (
	webview "github.com/abemedia/go-webview"
	_ "github.com/abemedia/go-webview/embedded"
)

type platformWebViewWrapper struct {
	webview.WebView
}

func (w platformWebViewWrapper) SetSize(width, height int, hint Hint) {
	w.WebView.SetSize(width, height, webview.Hint(hint))
}

func wrapWebView(w webview.WebView) PlatformWebView {
	if w == nil {
		return nil
	}
	return platformWebViewWrapper{w}
}
