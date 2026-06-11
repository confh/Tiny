//go:build !darwin
// +build !darwin

package vm

import "github.com/gogpu/systray"

type NativeTrayValue struct {
	Tray *systray.SystemTray
	Menu *systray.Menu
}
