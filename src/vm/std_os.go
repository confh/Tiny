package vm

import (
	"os"
	"runtime"

	"github.com/mackerelio/go-osstat/uptime"
	. "language.com/src/tinyerrors"
)

var stdOSMetadata = StdModuleInfo{
	Name: "os",
}

var stdOSMethods map[string]StdModuleFunc

func init() {
	stdOSMethods = map[string]StdModuleFunc{
		"name":      osName,
		"arch":      osArch,
		"tempDir":   osTempDir,
		"homeDir":   osHomeDir,
		"configDir": osConfigDir,
		"cpus":      osCpus,
		"hostName":  osHostname,
		"uptime":    osUptime,
	}
	registerStdModule(stdOSMetadata)
}

func (vm *VM) callStdOS(method string, args []TinyValue) {
	fn, ok := stdOSMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown os function: %s", method)
		return
	}
	fn(vm, args)
}

func osName(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.name", args)

	vm.push(NewNative(runtime.GOOS))
}

func osArch(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.arch", args)

	vm.push(NewNative(runtime.GOARCH))
}

func osTempDir(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.tempDir", args)

	vm.push(NewNative(os.TempDir()))
}

func osHomeDir(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.homeDir", args)

	dir, err := os.UserHomeDir()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while getting home directory: %s", err)
	}

	vm.push(NewNative(dir))
}

func osConfigDir(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.configDir", args)

	dir, err := os.UserConfigDir()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while getting config directory: %s", err)
	}

	vm.push(NewNative(dir))
}

func osCpus(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.cpus", args)

	vm.push(NewInt(runtime.NumCPU()))
}

func osHostname(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.hostName", args)

	name, err := os.Hostname()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while getting host name: %s", err)
	}

	vm.push(NewNative(name))
}

func osUptime(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "os.uptime", args)

	sysUptime, err := uptime.Get()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error while retrieving uptime: %s", err)
	}

	vm.push(NewInt(int(sysUptime.Milliseconds())))
}
