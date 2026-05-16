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

var tinyPackMagic = []byte("TINYAPP1")

func readAppendedBytecode() []byte {
	exePath, err := os.Executable()
	if err != nil {
		LangError(ErrorRuntime, "failed to get executable path: %v", err)
	}

	data, err := os.ReadFile(exePath)
	if err != nil {
		LangError(ErrorRuntime, "failed to read executable: %v", err)
	}

	minSize := len(tinyPackMagic) + 8

	if len(data) < minSize {
		LangError(ErrorRuntime, "no embedded Tiny bytecode found: executable too small")
	}

	magicStart := len(data) - len(tinyPackMagic)
	magicBytes := data[magicStart:]

	if string(magicBytes) != string(tinyPackMagic) {
		LangError(ErrorRuntime, "no embedded Tiny bytecode found: missing TINYAPP1 marker")
	}

	sizeStart := magicStart - 8

	if sizeStart < 0 {
		LangError(ErrorRuntime, "invalid embedded Tiny bytecode: missing size")
	}

	bytecodeSize := binary.LittleEndian.Uint64(data[sizeStart:magicStart])

	if bytecodeSize == 0 {
		LangError(ErrorRuntime, "invalid embedded Tiny bytecode: size is 0")
	}

	if bytecodeSize > uint64(sizeStart) {
		LangError(
			ErrorRuntime,
			"invalid embedded Tiny bytecode: size %d is larger than available data %d",
			bytecodeSize,
			sizeStart,
		)
	}

	bytecodeStart := sizeStart - int(bytecodeSize)

	if bytecodeStart < 0 || bytecodeStart > sizeStart {
		LangError(
			ErrorRuntime,
			"invalid embedded Tiny bytecode range: start=%d sizeStart=%d size=%d",
			bytecodeStart,
			sizeStart,
			bytecodeSize,
		)
	}

	bytecode := data[bytecodeStart:sizeStart]

	if len(bytecode) == 0 {
		LangError(ErrorRuntime, "embedded Tiny bytecode is empty")
	}

	return bytecode
}
