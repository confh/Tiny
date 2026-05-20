package vm

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	. "language.com/src/tinyerrors"
)

var stdIOMetadata = StdModuleInfo{
	Name: "io",
	Methods: map[string]StdMethodInfo{
		"print": {
			Name: "print",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: true},
			},
			Returns:     "bool",
			Description: "Prints a value.",
		},
		"println": {
			Name: "println",
			Args: []StdArg{
				{Name: "value", Type: "any", Optional: true},
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
	},
}

var stdIOMethods = map[string]StdModuleFunc{
	"println":  stdIOPrintln,
	"print":    stdIOPrint,
	"input":    stdIOInput,
	"readLine": stdIOReadLine,
}

func init() {
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
	vm.push(UndefinedValue{})
}

func stdIOPrint(vm *VM, args []Value) {
	for _, arg := range args {
		fmt.Print(valueToString(arg))
	}
	vm.push(UndefinedValue{})
}

func stdIOInput(vm *VM, args []Value) {
	expectArgs(vm, "io.input", args, 1)

	prompt := argString(vm, "io.input", args, 0)
	reader := bufio.NewReader(os.Stdin)
	fmt.Print(prompt)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	vm.push(input)
}

func stdIOReadLine(vm *VM, args []Value) {
	dontExpectArgs(vm, "io.readLine", args)

	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimRight(line, "\r\n")
	vm.push(line)
}
