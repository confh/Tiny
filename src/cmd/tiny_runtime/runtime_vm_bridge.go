package main

import (
	. "language.com/src/bytecode"
	. "language.com/src/vm"
)

func init() {
	SetRuntimeBytecodeLoader(func(data []byte) RuntimeBytecodeProgram {
		mainInstructions, mainDebugInfo, functions, classes, interfaces, globalIndex := LoadBytecodeFromBytes(data)
		return RuntimeBytecodeProgram{
			MainInstructions: mainInstructions,
			MainDebugInfo:    mainDebugInfo,
			Functions:        functions,
			Classes:          classes,
			Interfaces:       interfaces,
			GlobalIndex:      globalIndex,
		}
	})
}
