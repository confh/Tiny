//go:build !windows

package vm

func (webView *NativeWebViewValue) applyExecutableIcon() {}

func (webView *NativeWebViewValue) destroyIconHandles() {}
