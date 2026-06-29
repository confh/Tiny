//go:build linux

package vm

import (
	"bufio"
	"os"
	"runtime"
	"strings"
	"syscall"

	webview "github.com/abemedia/go-webview"
	"github.com/ebitengine/purego"
)

var (
	libgtk3 uintptr
)

var (
	gtk_window_set_decorated   func(window uintptr, setting int)
	gtk_window_begin_move_drag func(window uintptr, button int, root_x int, root_y int, timestamp uint32)
	gtk_window_iconify         func(window uintptr)
	gtk_window_maximize        func(window uintptr)
	gtk_window_unmaximize      func(window uintptr)
	gtk_window_close           func(window uintptr)
	gtk_window_is_maximized    func(window uintptr) int

	gdk_display_get_default      func() uintptr
	gdk_display_get_default_seat func(display uintptr) uintptr
	gdk_seat_get_pointer         func(seat uintptr) uintptr
	gdk_device_get_position      func(device uintptr, screen *uintptr, x *int32, y *int32)
)

func SilenceGarbageLogs() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}

	origStderr, err := syscall.Dup(syscall.Stderr)
	if err != nil {
		return
	}

	err = syscall.Dup3(int(w.Fd()), syscall.Stderr, 0)
	if err != nil {
		return
	}

	go func() {
		reader := bufio.NewReader(r)
		realStderrFile := os.NewFile(uintptr(origStderr), "/dev/stderr")

		wasSpam := false

		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				break
			}

			isSpam := strings.Contains(line, "signal 10") ||
				strings.Contains(line, "libEGL warning") ||
				strings.Contains(line, "MESA-LOADER") ||
				strings.Contains(line, "ZINK") ||
				strings.Contains(line, "failed to create dri2 screen")

			isEmpty := len(strings.TrimSpace(line)) == 0

			if isSpam {
				wasSpam = true
				continue
			}

			if isEmpty && wasSpam {
				continue
			}

			wasSpam = false

			if realStderrFile != nil {
				realStderrFile.WriteString(line)
			}
		}
	}()
}

func init() {
	if runtime.GOOS != "linux" {
		return
	}

	SilenceGarbageLogs()

	var err error
	for _, name := range []string{"libgtk-3.so.0", "libgtk-3.so", "libgtk-3"} {
		libgtk3, err = purego.Dlopen(name, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil {
			break
		}
	}
	if err != nil {
		println("Error loading libgtk3 for custom frame bindings:", err.Error())
		return
	}

	purego.RegisterLibFunc(&gtk_window_set_decorated, libgtk3, "gtk_window_set_decorated")
	purego.RegisterLibFunc(&gtk_window_begin_move_drag, libgtk3, "gtk_window_begin_move_drag")
	purego.RegisterLibFunc(&gtk_window_iconify, libgtk3, "gtk_window_iconify")
	purego.RegisterLibFunc(&gtk_window_maximize, libgtk3, "gtk_window_maximize")
	purego.RegisterLibFunc(&gtk_window_unmaximize, libgtk3, "gtk_window_unmaximize")
	purego.RegisterLibFunc(&gtk_window_close, libgtk3, "gtk_window_close")
	purego.RegisterLibFunc(&gtk_window_is_maximized, libgtk3, "gtk_window_is_maximized")

	purego.RegisterLibFunc(&gdk_display_get_default, libgtk3, "gdk_display_get_default")
	purego.RegisterLibFunc(&gdk_display_get_default_seat, libgtk3, "gdk_display_get_default_seat")
	purego.RegisterLibFunc(&gdk_seat_get_pointer, libgtk3, "gdk_seat_get_pointer")
	purego.RegisterLibFunc(&gdk_device_get_position, libgtk3, "gdk_device_get_position")
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
		fn(hwnd)
	}
}

func (webView *NativeWebViewValue) setFrameless(frameless bool) {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_set_decorated != nil {
			if frameless {
				gtk_window_set_decorated(hwnd, 0)
			} else {
				gtk_window_set_decorated(hwnd, 1)
			}
		}
	})
}

func (webView *NativeWebViewValue) startDrag() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_begin_move_drag == nil || gdk_display_get_default == nil {
			return
		}

		go func() {
			display := gdk_display_get_default()
			seat := gdk_display_get_default_seat(display)
			pointer := gdk_seat_get_pointer(seat)

			var screen uintptr
			var x, y int32
			gdk_device_get_position(pointer, &screen, &x, &y)

			gtk_window_begin_move_drag(hwnd, 1, int(x), int(y), 0)
		}()
	})
}

func (webView *NativeWebViewValue) startResize(edge string) {}

func (webView *NativeWebViewValue) toggleMaximize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_is_maximized == nil || gtk_window_unmaximize == nil || gtk_window_maximize == nil {
			return
		}
		if gtk_window_is_maximized(hwnd) != 0 {
			gtk_window_unmaximize(hwnd)
		} else {
			gtk_window_maximize(hwnd)
		}
	})
}

func (webView *NativeWebViewValue) minimize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_iconify != nil {
			gtk_window_iconify(hwnd)
		}
	})
}

func (webView *NativeWebViewValue) maximize() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_maximize != nil {
			gtk_window_maximize(hwnd)
		}
	})
}

func (webView *NativeWebViewValue) restore() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_unmaximize != nil {
			gtk_window_unmaximize(hwnd)
		}
	})
}

func (webView *NativeWebViewValue) close() {
	webView.dispatchWindow(func(hwnd uintptr) {
		if gtk_window_close != nil {
			gtk_window_close(hwnd)
		}
	})
}

func (webView *NativeWebViewValue) refreshWindowChrome() {}
func (webView *NativeWebViewValue) hide()                {}
func (webView *NativeWebViewValue) show()                {}

func createPlatformWebView(webView *NativeWebViewValue, debug bool, hidden bool, frameless bool) PlatformWebView {
	return wrapWebView(webview.New(debug))
}

func (webView *NativeWebViewValue) center(w, h int) {
	webView.w.SetSize(w, h, HintNone)
}

func (webView *NativeWebViewValue) smoothResize(w, h int) {
	webView.w.SetSize(w, h, HintNone)
}
