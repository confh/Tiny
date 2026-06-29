package vm

import "unsafe"

type Hint int

const (
	HintNone  Hint = 0
	HintMin   Hint = 1
	HintMax   Hint = 2
	HintFixed Hint = 3
)

type PlatformWebView interface {
	SetTitle(title string)
	SetSize(width, height int, hint Hint)
	SetHtml(html string)
	Navigate(url string)
	Run()
	Dispatch(f func())
	Destroy()
	Terminate()
	Bind(name string, f interface{}) error
	Window() unsafe.Pointer
}
