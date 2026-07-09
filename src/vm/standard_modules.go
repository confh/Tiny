package vm

import (
	"sync"

	. "language.com/src/tinyerrors"
)

func (vm *VM) callStandardModule(module string, method string, args []TinyValue) {
	if vm.allowedStdlib != nil && !vm.allowedStdlib[module] {
		vm.fatalError(ErrorRuntime, "standard module '%s' is not allowed in this VM", module)
	}

	switch module {
	case "array":
		vm.callStdArray(method, args)

	case "math":
		vm.callStdMath(method, args)

	case "strings":
		vm.callStdString(method, args)

	case "json":
		vm.callStdJson(method, args)

	case "fs":
		vm.callStdFs(method, args)

	case "app":
		vm.callStdApp(method, args)

	case "buffer":
		vm.callStdBuffer(method, args)

	case "regex":
		vm.callStdRegex(method, args)

	case "io":
		vm.callStdIO(method, args)

	case "process":
		vm.callStdProcess(method, args)

	case "time":
		vm.callStdTime(method, args)

	case "error":
		vm.callStdError(method, args)

	case "http":
		vm.callStdHttp(method, args)

	case "os":
		vm.callStdOS(method, args)

	case "runtime":
		vm.callStdRuntime(method, args)

	case "net":
		vm.callStdNet(method, args)

	case "path":
		vm.callStdPath(method, args)

	case "object":
		vm.callStdObject(method, args)

	case "observer":
		vm.callStdObserver(method, args)

	case "desktop":
		vm.callStdDesktop(method, args)

	case "sync":
		vm.callStdSync(method, args)

	case "tests":
		vm.callStdTest(method, args)

	case "ui":
		vm.callStdUi(method, args)

	case "websocket":
		vm.callStdWebsocket(method, args)

	case "tray":
		vm.callStdTray(method, args)

	case "validate":
		vm.callStdValidate(method, args)

	case "url":
		vm.callStdUrl(method, args)

	case "crypto":
		vm.callStdCrypto(method, args)

	case "sqlite":
		vm.callStdSqlite(method, args)

	case "wasm":
		vm.callStdWasm(method, args)

	default:
		vm.fatalError(ErrorName, "unknown standard module: %s", module)
	}
}

var stdModuleMethods map[string]map[string]StdModuleFunc
var stdModuleMethodsOnce sync.Once

func EnsureStdModuleMethods() {
	stdModuleMethodsOnce.Do(func() {
		stdModuleMethods = map[string]map[string]StdModuleFunc{
			"app":       stdAppMethods,
			"array":     stdArrayMethods,
			"buffer":    stdBufferMethods,
			"crypto":    stdCryptoMethods,
			"desktop":   stdDesktopMethods,
			"error":     stdErrorMethods,
			"fs":        stdFsMethods,
			"http":      stdHttpMethods,
			"io":        stdIOMethods,
			"json":      stdJsonMethods,
			"math":      stdMathMethods,
			"net":       stdNetMethods,
			"object":    stdObjectMethods,
			"observer":  stdObserverMethods,
			"os":        stdOSMethods,
			"path":      stdPathMethods,
			"process":   stdProcessMethods,
			"regex":     stdRegexMethods,
			"runtime":   stdRuntimeMethods,
			"sqlite":    stdSqliteMethods,
			"strings":   stdStringMethods,
			"sync":      stdSyncMethods,
			"tests":     stdTestMethods,
			"time":      stdTimeMethods,
			"tray":      stdTrayMethods,
			"ui":        stdUiMethods,
			"url":       stdUrlMethods,
			"validate":  stdValidateMethods,
			"wasm":      stdWasmMethods,
			"websocket": stdWebsocketMethods,
		}
	})
}
