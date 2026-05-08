package main

import (
	"flag"
	"fmt"
)

func main() {
	defer handleLangError()

	debug := flag.Bool("debug", false, "show bytecode debug output")

	flag.Parse()

	entryFile := "main.tiny"

	if flag.NArg() >= 1 {
		entryFile = flag.Arg(0)
	}

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	if *debug {
		fmt.Println("Main bytecode:")
		for i, instr := range mainBytecode {
			fmt.Printf("%03d: %-13s %v\n", i, instr.Op, instr.Value)
		}

		fmt.Println("\nFunctions:")
		for name, fn := range functions {
			fmt.Println("fn", name, fn.Params)
			for i, instr := range fn.Instructions {
				fmt.Printf("  %03d: %-13s %v\n", i, instr.Op, instr.Value)
			}
		}

		fmt.Println("\nOutput:")
	}

	vm := NewVM(mainBytecode, functions, classes)
	vm.Run()
}
