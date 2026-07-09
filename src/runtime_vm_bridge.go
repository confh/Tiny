package main

import (
	. "language.com/src/bytecode"
	tinycompiler "language.com/src/compiler"
	tinyloader "language.com/src/loader"
	. "language.com/src/vm"
)

func init() {
	SetRuntimeBytecodeLoader(func(data []byte) RuntimeBytecodeProgram {
		mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex := LoadBytecodeFromBytes(data)
		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}
		return RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			MainDebugInfo:    mainDebugInfo,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})

	SetRuntimeSourceLoader(func(source string, file string) RuntimeBytecodeProgram {
		lexer := NewLexer(source, file)
		parser := NewParser(lexer)
		program := parser.ParseProgram()

		compiler := tinycompiler.NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			MainDebugInfo:    mainDebugInfo,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})

	SetCompileSourceFunc(func(source string, file string) []byte {
		lexer := NewLexer(source, file)
		parser := NewParser(lexer)
		program := parser.ParseProgram()

		compiler := tinycompiler.NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return SaveBytecodeToBytes(mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex, false, false)
	})

	SetCompileFileFunc(func(path string) []byte {
		program := tinyloader.LoadProgram(path)

		compiler := tinycompiler.NewCompiler()
		compiler.SetPreserveAllFunctions(true)
		mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return SaveBytecodeToBytes(mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex, false, false)
	})
}
