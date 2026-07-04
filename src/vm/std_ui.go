package vm

import (
	"errors"

	"github.com/ncruces/zenity"
	. "language.com/src/tinyerrors"
)

func (v *NativeWebViewValue) TinyTypeName() string {
	return "ui.WebView"
}

var stdUiMetadata = StdModuleInfo{
	Name: "ui",
}

var stdUiMethods map[string]StdModuleFunc

func init() {
	stdUiMethods = map[string]StdModuleFunc{
		"new":                uiNew,
		"openFileDialog":     uiOpenFileDialog,
		"saveFileDialog":     uiSaveFileDialog,
		"selectFolderDialog": uiSelectFolderDialog,
	}
	registerStdModule(stdUiMetadata)
}

func (vm *VM) callStdUi(method string, args []TinyValue) {
	fn, ok := stdUiMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown ui function: %s", method)
		return
	}

	fn(vm, args)
}

func uiNew(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "ui.new", args, 0, 1)

	debug := false
	hidden := false
	frameless := false
	width := 800
	height := 600

	if len(args) > 0 {
		if _, ok := args[0].Value.(bool); ok {
			debug = argBool(vm, "ui.new", args, 0)
		} else if options, ok := vm.valueAsObjectForRead(args[0]); ok {
			debug = objectBool(options, "debug", false)
			hidden = objectBool(options, "hidden", false)
			frameless = objectBool(options, "frameless", false)
			width = objectInt(vm, options, "width", 800)
			height = objectInt(vm, options, "height", 600)
		} else {
			vm.runtimeError(ErrorType, "ui.new expects a bool or object")
			return
		}
	}

	webv := &NativeWebViewValue{
		hidden:           true,
		userWantedHidden: hidden,
		width:            width,
		height:           height,
	}

	webv.w = createPlatformWebView(webv, debug, true, frameless)

	webv.applyExecutableIcon()
	if frameless {
		webv.setFrameless(true)
	}
	if hidden {
		webv.hide()
	}
	vm.push(NewNative(webv))
}

func uiOpenFileDialog(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "ui.openFileDialog", args, 0, 2)
	var options []zenity.Option
	if len(args) > 0 && !isNullish(args[0]) {
		title := argString(vm, "ui.openFileDialog", args, 0)
		options = append(options, zenity.Title(title))
	}
	if len(args) > 1 && !isNullish(args[1]) {
		filter := argString(vm, "ui.openFileDialog", args, 1)
		options = append(options, zenity.FileFilter{
			Name:     filter,
			Patterns: []string{filter},
		})
	}
	path, err := zenity.SelectFile(options...)
	if err != nil {
		if errors.Is(err, zenity.ErrCanceled) {
			vm.push(NewNull())
			return
		}
		vm.runtimeError(ErrorRuntime, "failed to open file dialog: %v", err)
		return
	}
	vm.push(NewNative(path))
}

func uiSaveFileDialog(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "ui.saveFileDialog", args, 0, 2)
	var options []zenity.Option
	if len(args) > 0 && !isNullish(args[0]) {
		title := argString(vm, "ui.saveFileDialog", args, 0)
		options = append(options, zenity.Title(title))
	}
	if len(args) > 1 && !isNullish(args[1]) {
		defaultName := argString(vm, "ui.saveFileDialog", args, 1)
		options = append(options, zenity.Filename(defaultName))
	}
	path, err := zenity.SelectFileSave(options...)
	if err != nil {
		if errors.Is(err, zenity.ErrCanceled) {
			vm.push(NewNull())
			return
		}
		vm.runtimeError(ErrorRuntime, "failed to open save file dialog: %v", err)
		return
	}
	vm.push(NewNative(path))
}

func uiSelectFolderDialog(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "ui.selectFolderDialog", args, 0, 1)
	var options []zenity.Option
	if len(args) > 0 && !isNullish(args[0]) {
		title := argString(vm, "ui.selectFolderDialog", args, 0)
		options = append(options, zenity.Title(title))
	}
	path, err := zenity.SelectFile(append(options, zenity.Directory())...)
	if err != nil {
		if errors.Is(err, zenity.ErrCanceled) {
			vm.push(NewNull())
			return
		}
		vm.runtimeError(ErrorRuntime, "failed to open directory dialog: %v", err)
		return
	}
	vm.push(NewNative(path))
}
