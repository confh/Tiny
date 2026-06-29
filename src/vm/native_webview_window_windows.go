//go:build windows

package vm

import (
	"os"
	"sync"
	"syscall"
	"unsafe"

	webview2 "github.com/jchv/go-webview2"
)

const (
	gwlStyle    = ^uintptr(15)
	gwlpWndProc = ^uintptr(3)

	wsCaption     = 0x00c00000
	wsThickFrame  = 0x00040000
	wsSysMenu     = 0x00080000
	wsMinimizeBox = 0x00020000
	wsMaximizeBox = 0x00010000

	swpNoSize       = 0x0001
	swpNoMove       = 0x0002
	swpNoZOrder     = 0x0004
	swpFrameChanged = 0x0020

	swHide          = 0
	swNormal        = 1
	swShowMinimized = 2
	swMaximize      = 3
	swRestore       = 9

	wmDestroy         = 0x0002
	wmNCCalcSize      = 0x0083
	wmNCLButtonDown   = 0x00a1
	wmNCLButtonUp     = 0x00a2
	wmNCLButtonDblClk = 0x00a3
	wmNCHitTest       = 0x0084
	wmGetMinMaxInfo   = 0x0024
	wmSysCommand      = 0x0112
	wmLButtonUp       = 0x0202

	htClient      = 1
	htCaption     = 2
	htMinButton   = 8
	htMaxButton   = 9
	htLeft        = 10
	htRight       = 11
	htTop         = 12
	htTopLeft     = 13
	htTopRight    = 14
	htBottom      = 15
	htBottomLeft  = 16
	htBottomRight = 17
	htClose       = 20

	scMinimize = 0xf020
	scMaximize = 0xf030
	scRestore  = 0xf120
	scClose    = 0xf060
	scDragMove = 0xf012
	scSize     = 0xf000

	gwlExStyle  = ^uintptr(19)
	wsExLayered = 0x00080000
	lwaAlpha    = 0x00000002

	spiGetWorkArea = 0x0030

	wmSize       = 0x0005
	gwChild      = 5
	wmClose      = 0x0010
	wmEraseBkgnd = 0x0014

	whCbt         = 5
	hcbtCreateWnd = 3
)

var (
	user32Window                   = syscall.NewLazyDLL("user32.dll")
	procGetWindowLongPtrW          = user32Window.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW          = user32Window.NewProc("SetWindowLongPtrW")
	procSetWindowPos               = user32Window.NewProc("SetWindowPos")
	procShowWindow                 = user32Window.NewProc("ShowWindow")
	procSendMessageWindow          = user32Window.NewProc("SendMessageW")
	procPostMessageWindow          = user32Window.NewProc("PostMessageW")
	procIsZoomed                   = user32Window.NewProc("IsZoomed")
	procSetForeground              = user32Window.NewProc("SetForegroundWindow")
	procSetFocus                   = user32Window.NewProc("SetFocus")
	procReleaseCapture             = user32Window.NewProc("ReleaseCapture")
	procSystemParameters           = user32Window.NewProc("SystemParametersInfoW")
	procCallWindowProcW            = user32Window.NewProc("CallWindowProcW")
	procGetCursorPos               = user32Window.NewProc("GetCursorPos")
	procWindowFromPoint            = user32Window.NewProc("WindowFromPoint")
	procSetLayeredWindowAttributes = user32Window.NewProc("SetLayeredWindowAttributes")
	procCreateWindowExW            = user32Window.NewProc("CreateWindowExW")
	procDestroyWindow              = user32Window.NewProc("DestroyWindow")
	procGetWindow                  = user32Window.NewProc("GetWindow")
	procGetAncestor                = user32Window.NewProc("GetAncestor")
	procPostQuitMessage            = user32Window.NewProc("PostQuitMessage")
	procRegisterClassExW           = user32Window.NewProc("RegisterClassExW")
	procDefWindowProcW             = user32Window.NewProc("DefWindowProcW")
	procGetWindowRect              = user32Window.NewProc("GetWindowRect")
	procGetClientRect              = user32Window.NewProc("GetClientRect")
	procFillRect                   = user32Window.NewProc("FillRect")

	procSetWindowsHookExW   = user32Window.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32Window.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32Window.NewProc("CallNextHookEx")

	kernel32               = syscall.NewLazyDLL("kernel32.dll")
	procGetCurrentThreadId = kernel32.NewProc("GetCurrentThreadId")

	gdi32                     = syscall.NewLazyDLL("gdi32.dll")
	procCreateSolidBrush      = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject          = gdi32.NewProc("DeleteObject")
	dwmapi                    = syscall.NewLazyDLL("dwmapi.dll")
	procDwmSetWindowAttribute = dwmapi.NewProc("DwmSetWindowAttribute")

	winmm               = syscall.NewLazyDLL("winmm.dll")
	procTimeBeginPeriod = winmm.NewProc("timeBeginPeriod")
	procTimeEndPeriod   = winmm.NewProc("timeEndPeriod")

	windowSubclassMu sync.Mutex
	windowSubclass   = map[uintptr]*NativeWebViewValue{}
	windowOldProc    = map[uintptr]uintptr{}
	windowIsChild    = map[uintptr]bool{}
	windowProc       = syscall.NewCallback(webviewWindowProc)

	hHook                 uintptr
	activeWebViewCreating *NativeWebViewValue
)

type windowRect struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

type windowPoint struct {
	X int32
	Y int32
}

type minMaxInfo struct {
	Reserved     windowPoint
	MaxSize      windowPoint
	MaxPosition  windowPoint
	MinTrackSize windowPoint
	MaxTrackSize windowPoint
}

type windowPos struct {
	Hwnd            uintptr
	HwndInsertAfter uintptr
	X               int32
	Y               int32
	Cx              int32
	Cy              int32
	Flags           uint32
}

type createStructW struct {
	LpCreateParams uintptr
	HInstance      uintptr
	HMenu          uintptr
	HwndParent     uintptr
	Cy             int32
	Cx             int32
	Y              int32
	X              int32
	Style          int32
	LpszName       *uint16
	LpszClass      *uint16
	DwExStyle      uint32
}

type cbtCreateWnd struct {
	Lpcs            *createStructW
	HwndInsertAfter uintptr
}

func cbtHookProc(code, wparam, lparam uintptr) uintptr {
	if code == hcbtCreateWnd && activeWebViewCreating != nil {
		hwnd := wparam

		windowSubclassMu.Lock()
		alreadyCreated := false
		for _, v := range windowSubclass {
			if v == activeWebViewCreating {
				alreadyCreated = true
				break
			}
		}
		windowSubclassMu.Unlock()

		if !alreadyCreated {
			cbt := (*cbtCreateWnd)(unsafe.Pointer(lparam))

			cbt.Lpcs.DwExStyle |= uint32(wsExLayered)

			oldProc, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, windowProc)
			if oldProc != 0 {
				windowSubclassMu.Lock()
				windowSubclass[hwnd] = activeWebViewCreating
				windowOldProc[hwnd] = oldProc
				windowSubclassMu.Unlock()

				procSetLayeredWindowAttributes.Call(hwnd, 0, 0, lwaAlpha)
			}
		}
	}
	res, _, _ := procCallNextHookEx.Call(hHook, code, wparam, lparam)
	return res
}

func (webView *NativeWebViewValue) windowHandle() uintptr {
	if webView == nil || webView.w == nil {
		return 0
	}
	return uintptr(webView.w.Window())
}

func (webView *NativeWebViewValue) dispatchWindow(fn func(hwnd uintptr)) {
	hwnd := webView.windowHandle()
	if hwnd != 0 {
		webView.w.Dispatch(func() {
			fn(hwnd)
		})
	}
}

func callOldWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	windowSubclassMu.Lock()
	oldProc := windowOldProc[hwnd]
	windowSubclassMu.Unlock()
	if oldProc != 0 {
		result, _, _ := procCallWindowProcW.Call(oldProc, hwnd, msg, wparam, lparam)
		return result
	}
	return 0
}

func webviewWindowProc(hwnd, msg, wparam, lparam uintptr) uintptr {
	if msg == wmDestroy {
		windowSubclassMu.Lock()
		oldProc := windowOldProc[hwnd]
		delete(windowSubclass, hwnd)
		delete(windowOldProc, hwnd)
		delete(windowIsChild, hwnd)
		remaining := len(windowSubclass)
		windowSubclassMu.Unlock()

		if remaining == 0 {
			procPostQuitMessage.Call(0)
		}

		if oldProc != 0 {
			procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, oldProc)
			res, _, _ := procCallWindowProcW.Call(oldProc, hwnd, msg, wparam, lparam)
			return res
		}
		return 0
	}

	windowSubclassMu.Lock()
	webView := windowSubclass[hwnd]
	windowSubclassMu.Unlock()

	switch msg {
	case wmNCCalcSize:
		if wparam == 1 && webView != nil && webView.frameless {
			return 0
		}
	case wmNCHitTest:
		if webView != nil && webView.frameless {
			if hit := framelessResizeHitTest(resizeOwnerWindow(hwnd), lparam); hit != htClient {
				return hit
			}
			if hit := framelessCaptionHitTest(resizeOwnerWindow(hwnd), lparam); hit != htClient {
				return hit
			}
		}
	case wmNCLButtonUp:
		if webView != nil && webView.frameless {
			switch wparam {
			case htMinButton:
				procShowWindow.Call(resizeOwnerWindow(hwnd), swShowMinimized)
				return 0
			case htMaxButton:
				target := resizeOwnerWindow(hwnd)
				zoomed, _, _ := procIsZoomed.Call(target)
				if zoomed != 0 {
					procShowWindow.Call(target, swRestore)
				} else {
					procShowWindow.Call(target, swMaximize)
				}
				return 0
			case htClose:
				procPostMessageWindow.Call(resizeOwnerWindow(hwnd), wmClose, 0, 0)
				return 0
			}
		}
	case wmNCLButtonDblClk:
		if webView != nil && webView.frameless && wparam == htCaption {
			target := resizeOwnerWindow(hwnd)
			zoomed, _, _ := procIsZoomed.Call(target)
			if zoomed != 0 {
				procShowWindow.Call(target, swRestore)
			} else {
				procShowWindow.Call(target, swMaximize)
			}
			return 0
		}
	case wmGetMinMaxInfo:
		if webView != nil && webView.frameless {
			var workArea windowRect
			ok, _, _ := procSystemParameters.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&workArea)), 0)
			if ok != 0 {
				info := (*minMaxInfo)(unsafe.Pointer(lparam))
				info.MaxPosition.X = workArea.Left
				info.MaxPosition.Y = workArea.Top
				info.MaxSize.X = workArea.Right - workArea.Left
				info.MaxSize.Y = workArea.Bottom - workArea.Top
				return 0
			}
		}
	case wmEraseBkgnd:
		hdc := wparam
		var rect windowRect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
		hBrush, _, _ := procCreateSolidBrush.Call(0x0b0909)
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&rect)), hBrush)
		procDeleteObject.Call(hBrush)
		return 1
	}
	return callOldWindowProc(hwnd, msg, wparam, lparam)
}

func resizeOwnerWindow(hwnd uintptr) uintptr {
	windowSubclassMu.Lock()
	isChild := windowIsChild[hwnd]
	windowSubclassMu.Unlock()
	if !isChild {
		return hwnd
	}
	root, _, _ := procGetAncestor.Call(hwnd, 2)
	if root != 0 {
		return root
	}
	return hwnd
}

func framelessResizeHitTest(hwnd, lparam uintptr) uintptr {
	const resizeBorder = int32(8)

	x := int32(int16(uint16(lparam & 0xffff)))
	y := int32(int16(uint16((lparam >> 16) & 0xffff)))

	var rect windowRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ok == 0 {
		return htClient
	}

	onLeft := x >= rect.Left && x < rect.Left+resizeBorder
	onRight := x <= rect.Right && x > rect.Right-resizeBorder
	onTop := y >= rect.Top && y < rect.Top+resizeBorder
	onBottom := y <= rect.Bottom && y > rect.Bottom-resizeBorder

	switch {
	case onTop && onLeft:
		return htTopLeft
	case onTop && onRight:
		return htTopRight
	case onBottom && onLeft:
		return htBottomLeft
	case onBottom && onRight:
		return htBottomRight
	case onLeft:
		return htLeft
	case onRight:
		return htRight
	case onTop:
		return htTop
	case onBottom:
		return htBottom
	default:
		return htClient
	}
}

func framelessCaptionHitTest(hwnd, lparam uintptr) uintptr {
	const titlebarHeight = int32(48)
	const buttonWidth = int32(46)
	const buttonCount = int32(3)

	x := int32(int16(uint16(lparam & 0xffff)))
	y := int32(int16(uint16((lparam >> 16) & 0xffff)))

	var rect windowRect
	if ok, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect))); ok == 0 {
		return htClient
	}

	if y < rect.Top || y >= rect.Top+titlebarHeight || x < rect.Left || x >= rect.Right {
		return htClient
	}

	buttonsLeft := rect.Right - buttonWidth*buttonCount
	if x >= buttonsLeft {
		buttonIndex := (x - buttonsLeft) / buttonWidth
		switch buttonIndex {
		case 0:
			return htMinButton
		case 1:
			return htMaxButton
		case 2:
			return htClose
		}
	}

	return htCaption
}

func (webView *NativeWebViewValue) installWindowSubclass(hwnd uintptr) {
	windowSubclassMu.Lock()
	_, installed := windowSubclass[hwnd]
	windowSubclassMu.Unlock()
	if installed {
		return
	}
	oldProc, _, _ := procSetWindowLongPtrW.Call(hwnd, gwlpWndProc, windowProc)
	if oldProc == 0 {
		return
	}
	windowSubclassMu.Lock()
	windowSubclass[hwnd] = webView
	windowOldProc[hwnd] = oldProc
	windowSubclassMu.Unlock()
}

func (webView *NativeWebViewValue) installChildWindowSubclass(hwnd uintptr) {
	child, _, _ := procGetWindow.Call(hwnd, gwChild)
	if child == 0 {
		return
	}
	windowSubclassMu.Lock()
	_, installed := windowSubclass[child]
	windowSubclassMu.Unlock()
	if installed {
		return
	}
	oldProc, _, _ := procSetWindowLongPtrW.Call(child, gwlpWndProc, windowProc)
	if oldProc == 0 {
		return
	}
	windowSubclassMu.Lock()
	windowSubclass[child] = webView
	windowOldProc[child] = oldProc
	windowIsChild[child] = true
	windowSubclassMu.Unlock()
}

func (webView *NativeWebViewValue) setFrameless(frameless bool) {
	webView.frameless = frameless
	webView.dispatchWindow(func(hwnd uintptr) {
		if frameless {
			exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
			procSetWindowLongPtrW.Call(hwnd, gwlExStyle, exStyle|wsExLayered)

			style, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlStyle)
			style |= wsCaption | wsThickFrame | wsSysMenu | wsMinimizeBox | wsMaximizeBox
			webView.installWindowSubclass(hwnd)
			webView.installChildWindowSubclass(hwnd)
			procSetWindowLongPtrW.Call(hwnd, gwlStyle, style)
			procSetWindowPos.Call(hwnd, 0, 0, 0, 0, 0, swpNoMove|swpNoSize|swpNoZOrder|swpFrameChanged)
		}
	})
}

func (webView *NativeWebViewValue) refreshWindowChrome() {
	webView.dispatchWindow(func(hwnd uintptr) {
		webView.installWindowSubclass(hwnd)
		webView.installChildWindowSubclass(hwnd)
	})
}

func (webView *NativeWebViewValue) hide() {
	webView.hidden = true
	webView.dispatchWindow(func(hwnd uintptr) {
		procShowWindow.Call(hwnd, swHide)
	})
}

func (webView *NativeWebViewValue) show() {
	webView.hidden = false
	webView.dispatchWindow(func(hwnd uintptr) {
		exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)

		exStyle &= ^uintptr(0x00000080)
		exStyle |= 0x00040000

		if webView.frameless {
			exStyle |= wsExLayered
		} else {
			exStyle &= ^uintptr(wsExLayered)
		}
		procSetWindowLongPtrW.Call(hwnd, gwlExStyle, exStyle)

		if webView.frameless {
			var preference uint32 = 2
			procDwmSetWindowAttribute.Call(
				hwnd,
				33,
				uintptr(unsafe.Pointer(&preference)),
				4,
			)

			var borderColor uint32 = 0x0b0909
			procDwmSetWindowAttribute.Call(
				hwnd,
				34,
				uintptr(unsafe.Pointer(&borderColor)),
				4,
			)
		}

		procSetLayeredWindowAttributes.Call(hwnd, 0, 255, lwaAlpha)

		w := webView.width
		h := webView.height
		if w == 0 {
			w = 800
		}
		if h == 0 {
			h = 600
		}

		var workArea windowRect
		ok, _, _ := procSystemParameters.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&workArea)), 0)
		if ok != 0 {
			screenWidth := workArea.Right - workArea.Left
			screenHeight := workArea.Bottom - workArea.Top
			x := workArea.Left + (screenWidth-int32(w))/2
			y := workArea.Top + (screenHeight-int32(h))/2
			procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder|swpFrameChanged)
		}

		procShowWindow.Call(hwnd, swNormal)
		procSetForeground.Call(hwnd)
		procSetFocus.Call(hwnd)

		if webView.w != nil {
			webView.w.SetSize(w, h, HintNone)
		}

	})
}

func (webView *NativeWebViewValue) center(w, h int) {
	webView.width = w
	webView.height = h
	webView.dispatchWindow(func(hwnd uintptr) {
		var workArea windowRect
		ok, _, _ := procSystemParameters.Call(spiGetWorkArea, 0, uintptr(unsafe.Pointer(&workArea)), 0)
		if ok != 0 {
			screenWidth := workArea.Right - workArea.Left
			screenHeight := workArea.Bottom - workArea.Top
			x := workArea.Left + (screenWidth-int32(w))/2
			y := workArea.Top + (screenHeight-int32(h))/2
			procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder)
		}
	})
}

func (webView *NativeWebViewValue) smoothResize(targetW, targetH int) {
	webView.center(targetW, targetH)
}

func (webView *NativeWebViewValue) startDrag() {
	webView.dispatchWindow(func(hwnd uintptr) {
		procReleaseCapture.Call()
		procSendMessageWindow.Call(hwnd, wmNCLButtonDown, htCaption, 0)
	})
}

func (webView *NativeWebViewValue) startResize(edge string) {
	hit := uintptr(0)
	switch edge {
	case "left":
		hit = htLeft
	case "right":
		hit = htRight
	case "top":
		hit = htTop
	case "top-left":
		hit = htTopLeft
	case "top-right":
		hit = htTopRight
	case "bottom":
		hit = htBottom
	case "bottom-left":
		hit = htBottomLeft
	case "bottom-right":
		hit = htBottomRight
	default:
		return
	}
	webView.dispatchWindow(func(hwnd uintptr) {
		procReleaseCapture.Call()
		procSendMessageWindow.Call(hwnd, wmNCLButtonDown, hit, 0)
	})
}

func (webView *NativeWebViewValue) minimize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		procShowWindow.Call(hwnd, swShowMinimized)
	})
}

func (webView *NativeWebViewValue) maximize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		procShowWindow.Call(hwnd, swMaximize)
	})
}

func (webView *NativeWebViewValue) restore() {
	webView.dispatchWindow(func(hwnd uintptr) {
		procShowWindow.Call(hwnd, swRestore)
	})
}

func (webView *NativeWebViewValue) toggleMaximize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		zoomed, _, _ := procIsZoomed.Call(hwnd)
		if zoomed != 0 {
			procShowWindow.Call(hwnd, swRestore)
			return
		}
		procShowWindow.Call(hwnd, swMaximize)
	})
}

func (webView *NativeWebViewValue) close() {
	webView.dispatchWindow(func(hwnd uintptr) {
		procPostMessageWindow.Call(hwnd, wmClose, 0, 0)
	})
}

func createPlatformWebView(webView *NativeWebViewValue, debug bool, hidden bool, frameless bool) PlatformWebView {
	webView.frameless = frameless

	os.Setenv("WEBVIEW2_DEFAULT_BACKGROUND_COLOR", "FF09090B")

	activeWebViewCreating = webView

	threadId, _, _ := procGetCurrentThreadId.Call()
	hHook, _, _ = procSetWindowsHookExW.Call(
		whCbt,
		syscall.NewCallback(cbtHookProc),
		0,
		threadId,
	)

	w := webview2.NewWithOptions(webview2.WebViewOptions{
		Debug: debug,
		WindowOptions: webview2.WindowOptions{
			Width:  uint(webView.width),
			Height: uint(webView.height),
			Hidden: hidden,
		},
	})

	if hHook != 0 {
		procUnhookWindowsHookEx.Call(hHook)
		hHook = 0
	}
	activeWebViewCreating = nil

	if w == nil {
		return nil
	}

	hwnd := uintptr(w.Window())
	if hwnd != 0 {
		if frameless {
			exStyle, _, _ := procGetWindowLongPtrW.Call(hwnd, gwlExStyle)
			procSetWindowLongPtrW.Call(hwnd, gwlExStyle, exStyle|wsExLayered)

			var preference uint32 = 2
			procDwmSetWindowAttribute.Call(
				hwnd,
				33,
				uintptr(unsafe.Pointer(&preference)),
				4,
			)

			var borderColor uint32 = 0x0b0909
			procDwmSetWindowAttribute.Call(
				hwnd,
				34,
				uintptr(unsafe.Pointer(&borderColor)),
				4,
			)
		}
	}

	return wrapWebView(w)
}

type platformWebViewWrapper struct {
	webview2.WebView
}

func (w platformWebViewWrapper) SetSize(width, height int, hint Hint) {
	w.WebView.SetSize(width, height, webview2.Hint(hint))
}

func wrapWebView(w webview2.WebView) PlatformWebView {
	if w == nil {
		return nil
	}
	return platformWebViewWrapper{w}
}
