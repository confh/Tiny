package main

import (
	"encoding/binary"
	"os"

	. "language.com/src/bytecode"
	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

var magic = []byte("TINYAPP1")

func main() {
	defer HandleLangError()

	bytecode := readAppendedBytecode()

	mainBytecode, functions, classes := LoadBytecodeFromBytes(bytecode)

	vm := NewVM(mainBytecode, functions, classes)
	vm.SetCLIArgs(os.Args[1:])
	vm.Run()
}

func readAppendedBytecode() []byte {
	exePath, err := os.Executable()
	if err != nil {
		LangError(ErrorRuntime, "failed to get executable path: %v", err)
	}

	data, err := os.ReadFile(exePath)
	if err != nil {
		LangError(ErrorRuntime, "failed to read executable: %v", err)
	}

	minSize := len(magic) + 8
	if len(data) < minSize {
		LangError(ErrorRuntime, "no embedded Tiny bytecode found")
	}

	magicStart := len(data) - len(magic)

	if string(data[magicStart:]) != string(magic) {
		LangError(ErrorRuntime, "no embedded Tiny bytecode found")
	}

	sizeStart := magicStart - 8
	size := binary.LittleEndian.Uint64(data[sizeStart:magicStart])

	bytecodeStart := sizeStart - int(size)

	if bytecodeStart < 0 {
		LangError(ErrorRuntime, "invalid embedded Tiny bytecode size")
	}

	return data[bytecodeStart:sizeStart]
}
