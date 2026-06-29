//go:build windows

package vm

import (
	"os"
	"syscall"
	"unsafe"
)

func (webView *NativeWebViewValue) applyExecutableIcon() {
	if webView == nil || webView.w == nil {
		return
	}

	exePath, err := os.Executable()
	if err != nil || exePath == "" {
		return
	}

	pathPtr, err := syscall.UTF16PtrFromString(exePath)
	if err != nil {
		return
	}

	shell32 := syscall.NewLazyDLL("shell32.dll")
	extractIconEx := shell32.NewProc("ExtractIconExW")
	if err := extractIconEx.Find(); err != nil {
		return
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	sendMessage := user32.NewProc("SendMessageW")
	if err := sendMessage.Find(); err != nil {
		return
	}

	var largeIcon uintptr
	var smallIcon uintptr
	count, _, _ := extractIconEx.Call(
		uintptr(unsafe.Pointer(pathPtr)),
		0,
		uintptr(unsafe.Pointer(&largeIcon)),
		uintptr(unsafe.Pointer(&smallIcon)),
		1,
	)
	if count == 0 {
		return
	}

	hwnd := uintptr(webView.w.Window())
	if hwnd == 0 {
		webView.iconBig = largeIcon
		webView.iconSmall = smallIcon
		webView.destroyIconHandles()
		return
	}

	const (
		wmSetIcon  = 0x0080
		iconSmall  = 0
		iconBig    = 1
		iconSmall2 = 2
	)

	if smallIcon != 0 {
		sendMessage.Call(hwnd, wmSetIcon, iconSmall, smallIcon)
		sendMessage.Call(hwnd, wmSetIcon, iconSmall2, smallIcon)
		webView.iconSmall = smallIcon
	}
	if largeIcon != 0 {
		sendMessage.Call(hwnd, wmSetIcon, iconBig, largeIcon)
		webView.iconBig = largeIcon
	}
}

func (webView *NativeWebViewValue) destroyIconHandles() {
	if webView == nil {
		return
	}

	user32 := syscall.NewLazyDLL("user32.dll")
	destroyIcon := user32.NewProc("DestroyIcon")
	if err := destroyIcon.Find(); err != nil {
		webView.iconSmall = 0
		webView.iconBig = 0
		return
	}

	if webView.iconSmall != 0 {
		destroyIcon.Call(webView.iconSmall)
		webView.iconSmall = 0
	}
	if webView.iconBig != 0 {
		destroyIcon.Call(webView.iconBig)
		webView.iconBig = 0
	}
}
