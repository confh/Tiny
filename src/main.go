package main

import (
	"fmt"
	"os"

	. "language.com/src/bytecode"
	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

func getScriptArgs() []string {
	args := os.Args

	if len(args) < 2 {
		return []string{}
	}

	if len(args) >= 3 {
		return args[2:]
	}

	return []string{}
}

func main() {
	defer HandleLangError()

	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "build":
			buildCommand(os.Args[2:])
			return

		case "run":
			runBytecodeCommand(os.Args[2:])
			return

		case "pack":
			packCommand(os.Args[2:])
			return

		case "dist":
			DistCommand(os.Args[2:])
			return

		case "init":
			initCommand(os.Args[2:])
			return

		case "task":
			taskCommand(os.Args[2:])
			return

		case "lsp":
			runLSP()
			return
		}
	}

	runSourceCommand(os.Args[1:])
}

func runSourceCommand(args []string) {
	var entryFile string
	cliArgs := []string{}

	if len(args) >= 1 {
		entryFile = args[0]
		cliArgs = args[1:]
	}

	if len(args) == 0 {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny run <file.tiny> or create tiny.json with tiny init")
		}

		entryFile = config.Entry
	} else {
		entryFile = args[0]
	}

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	vm := NewVM(mainBytecode, functions, classes)
	vm.SetCLIArgs(cliArgs)
	vm.Run()
}

func buildCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny build <file.tiny> -o <file.tbc>")
	}

	entryFile := args[0]
	outFile := "out.tbc"

	for i := 1; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outFile = args[i+1]
			i++
		}
	}

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	SaveBytecode(outFile, mainBytecode, functions, classes)

	fmt.Println("Built", outFile)
}

func runBytecodeCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny run <file.tbc>")
	}

	mainBytecode, functions, classes := LoadBytecode(args[0])

	vm := NewVM(mainBytecode, functions, classes)
	vm.SetCLIArgs(getScriptArgs())
	vm.Run()
}
