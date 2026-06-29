package main

import (
	. "language.com/src/bytecode"
	. "language.com/src/vm"
)

func init() {
	SetRuntimeBytecodeLoader(func(data []byte) RuntimeBytecodeProgram {
		mainInstructions, functions, classes, interfaces, globalIndex := LoadBytecodeFromBytes(data)
		return RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
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

		compiler := NewCompiler()
		compiler.preserveAllFunctions = true
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
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

		compiler := NewCompiler()
		compiler.preserveAllFunctions = true
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	})

	SetCompileFileFunc(func(path string) []byte {
		program := LoadProgram(path)

		compiler := NewCompiler()
		compiler.preserveAllFunctions = true
		mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

		mainInstructions = OptimizeBytecode(mainInstructions)
		for name, fn := range functions {
			fn.Instructions = OptimizeBytecode(fn.Instructions)
			functions[name] = fn
		}

		return SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, false)
	})
}
