package main

import (
	"fmt"
	"os"
)

func main() {
	defer handleLangError()

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
		}
	}

	runSourceCommand(os.Args[1:])
}

func runSourceCommand(args []string) {
	entryFile := "main.tiny"

	if len(args) >= 1 {
		entryFile = args[0]
	}

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	vm := NewVM(mainBytecode, functions, classes)
	vm.Run()
}

func buildCommand(args []string) {
	if len(args) < 1 {
		langError(ErrorRuntime, "usage: tiny build <file.tiny> -o <file.tbc>")
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

	SaveBytecode(outFile, mainBytecode, functions, classes)

	fmt.Println("Built", outFile)
}

func runBytecodeCommand(args []string) {
	if len(args) < 1 {
		langError(ErrorRuntime, "usage: tiny run <file.tbc>")
	}

	mainBytecode, functions, classes := LoadBytecode(args[0])

	vm := NewVM(mainBytecode, functions, classes)
	vm.Run()
}
