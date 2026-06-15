package vm

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/x/term"
	. "language.com/src/tinyerrors"
)

var stdIOMetadata = StdModuleInfo{
	Name: "io",
}

var stdIOMethods map[string]StdModuleFunc

func init() {
	stdIOMethods = map[string]StdModuleFunc{
		"println":  stdIOPrintln,
		"print":    stdIOPrint,
		"input":    stdIOInput,
		"readLine": stdIOReadLine,
		"readKey":  stdIOReadKey,
		"clear":    stdIOClearScreen,
	}
	registerStdModule(stdIOMetadata)
}

func (vm *VM) callStdIO(method string, args []TinyValue) {
	fn, ok := stdIOMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown io function: %s", method)
		return
	}
	fn(vm, args)
}

func stdIOPrintln(vm *VM, args []TinyValue) {
	for i, arg := range args {
		if i > 0 {
			fmt.Print(" ")
		}
		fmt.Print(valueToString(arg, true))
	}
	fmt.Println()
	vm.push(NewNull())
}

func stdIOPrint(vm *VM, args []TinyValue) {
	for _, arg := range args {
		fmt.Print(valueToString(arg, true))
	}
	vm.push(NewNull())
}

func stdIOInput(vm *VM, args []TinyValue) {
	expectArgs(vm, "io.input", args, 1)

	prompt := argString(vm, "io.input", args, 0)
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	vm.push(NewNative(input))
}

func stdIOReadLine(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "io.readLine", args)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	vm.push(NewNative(line))
}

func stdIOReadKey(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "io.readKey", args)

	fd := uintptr(os.Stdin.Fd())

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}

	defer term.Restore(fd, oldState)

	b := make([]byte, 1)
	_, err = os.Stdin.Read(b)
	if err != nil {
		fmt.Println("Error reading key:", err)
		return
	}

	vm.push(NewNative(string(b[0])))
}

func stdIOClearScreen(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "io.readKey", args)

	switch v := runtime.GOOS; v {
	case "windows":
		cmd := exec.Command("cmd", "/c", "cls")
		cmd.Stdout = os.Stdout
		cmd.Run()

	case "linux":
		cmd := exec.Command("clear")
		cmd.Stdout = os.Stdout
		cmd.Run()
	}

	vm.push(NewNull())
}
