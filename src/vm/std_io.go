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
	Methods: map[string]StdMethodInfo{
		"print": {
			Name: "print",
			Args: []StdArg{
				{Name: "value", Type: "any", Variadic: true},
			},
			Returns:     "bool",
			Description: "Prints a value.",
		},
		"println": {
			Name: "println",
			Args: []StdArg{
				{Name: "value", Type: "any", Variadic: true},
			},
			Returns:     "bool",
			Description: "Prints a value with a newline.",
		},
		"input": {
			Name: "input",
			Args: []StdArg{
				{Name: "prompt", Type: "string", Optional: true},
			},
			Returns:     "string",
			Description: "Reads input from the terminal.",
		},
		"readLine": {
			Name:        "readLine",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Reads one line of input from the terminal.",
		},
		"readKey": {
			Name:        "readKey",
			Args:        []StdArg{},
			Returns:     "string",
			Description: "Reads a single key press from the terminal.",
		},
		"clear": {
			Name:        "clear",
			Args:        []StdArg{},
			Returns:     "null",
			Description: "Clears the terminal screen.",
		},
	},
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

func (vm *VM) callStdIO(method string, args []Value) {
	fn, ok := stdIOMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown io function: %s", method)
		return
	}
	fn(vm, args)
}

func stdIOPrintln(vm *VM, args []Value) {
	for i, arg := range args {
		if i > 0 {
			fmt.Print(" ")
		}
		fmt.Print(valueToString(arg))
	}
	fmt.Println()
	vm.push(NewNull())
}

func stdIOPrint(vm *VM, args []Value) {
	for _, arg := range args {
		fmt.Print(valueToString(arg))
	}
	vm.push(NewNull())
}

func stdIOInput(vm *VM, args []Value) {
	expectArgs(vm, "io.input", args, 1)

	prompt := argString(vm, "io.input", args, 0)
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	vm.push(NewNative(input))
}

func stdIOReadLine(vm *VM, args []Value) {
	dontExpectArgs(vm, "io.readLine", args)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	vm.push(NewNative(line))
}

func stdIOReadKey(vm *VM, args []Value) {
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

func stdIOClearScreen(vm *VM, args []Value) {
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
