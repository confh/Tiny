package main

import (
	"fmt"
	"os"
	"path/filepath"

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

		case "version", "ver", "v":
			versionCommand()
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

	disableCache := false
	filteredArgs := []string{}
	for _, arg := range args {
		if arg == "--disable-cache" {
			disableCache = true
		} else {
			filteredArgs = append(filteredArgs, arg)
		}
	}

	if len(filteredArgs) >= 1 {
		entryFile = filteredArgs[0]
		cliArgs = filteredArgs[1:]
	}

	if len(filteredArgs) == 0 {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny run <file.tiny> or create tiny.json with tiny init")
		}

		entryFile = config.Entry
	} else {
		entryFile = filteredArgs[0]
	}

	sourceBytes, err := os.ReadFile(entryFile)
	if err != nil {
		panic(err)
	}

	sourceText := string(sourceBytes)

	hash, err := hashTinyProject(entryFile, sourceText)
	if err != nil {
		compileAndRun(entryFile, cliArgs)
		return
	}

	cachePath, err := tinyCachePath(entryFile, hash)
	if !disableCache && err == nil && fileExists(cachePath) {
		runBytecodeFile(cachePath)
		return
	}

	if !disableCache {
		deleteTinyCacheContent(entryFile)
		saveBytecodeFile(entryFile, cachePath, true)
		runBytecodeFile(cachePath)
	} else {
		compileAndRun(entryFile, cliArgs)
	}
}

func deleteTinyCacheContent(entryFile string) {
	abs, err := filepath.Abs(entryFile)
	if err != nil {
		panic(err)
	}

	dir := filepath.Dir(abs)

	cacheDir := filepath.Join(dir, ".tinycache")

	files, err := os.ReadDir(cacheDir)
	if err != nil {
		panic(err)
	}

	for _, file := range files {
		filePath := filepath.Join(cacheDir, file.Name())

		err = os.Remove(filePath)
		if err != nil {
			panic(err)
		}
	}
}

func runBytecodeFile(path string) {
	mainBytecode, functions, classes := LoadBytecode(path)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	vm := NewVM(mainBytecode, functions, classes)
	vm.SetCLIArgs(getScriptArgs())
	vm.Run()
}

func saveBytecodeFile(entryFile string, outFile string, cache bool) {
	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	SaveBytecode(outFile, mainBytecode, functions, classes, cache)
}

func compileAndRun(entryFile string, cliArgs []string) {
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

	saveBytecodeFile(entryFile, outFile, false)

	fmt.Println("Built", outFile)
}

func runBytecodeCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny run <file.tbc>")
	}

	runBytecodeFile(args[0])
}
