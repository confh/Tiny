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

	if len(os.Args) < 2 {
		if _, ok := loadTinyConfig(); ok {
			runSourceCommand(nil)
		} else {
			helpCommand(nil)
		}
		return
	}

	if len(os.Args) >= 2 {
		switch os.Args[1] {
		case "help", "--help", "-h":
			helpCommand(os.Args[2:])
			return

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

		case "add":
			addPackageCommand(os.Args[2:])
			return

		case "install":
			installPackagesCommand(os.Args[2:])
			return

		case "remove", "rm":
			removePackageCommand(os.Args[2:])
			return

		case "deps", "list":
			listDownloadedDependenciesCommand(os.Args[2:])
			return

		case "task":
			taskCommand(os.Args[2:])
			return

		case "version", "ver", "v":
			versionCommand()
			return

		case "update":
			updateCommand()
			return

		case "lsp":
			runLSP()
			return
		}
	}

	runSourceCommand(os.Args[1:])
}

func helpCommand(args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "build":
			fmt.Println("usage: tiny build <file.tiny> -o <file.tbc>")
			fmt.Println("Compiles a Tiny source file into bytecode.")
		case "run":
			fmt.Println("usage: tiny run <file.tbc>")
			fmt.Println("Runs a compiled Tiny bytecode file.")
		case "pack":
			fmt.Println("usage: tiny pack <file.tiny> -o <output>")
			fmt.Println("Builds a packed executable from a Tiny source file.")
		case "dist":
			fmt.Println("usage: tiny dist <file.tiny> -o <output> [--target windows-amd64|linux-amd64|linux-arm64] [--plugin <path>]")
			fmt.Println("Builds a distributable executable for a target platform.")
		case "init":
			fmt.Println("usage: tiny init")
			fmt.Println("Creates a tiny.json project file.")
		case "add":
			fmt.Println("usage: tiny add <github:owner/repo[@ref]>")
			fmt.Println("usage: tiny add <name> <github:owner/repo[@ref]>")
			fmt.Println("Adds and installs a dependency.")
		case "install":
			fmt.Println("usage: tiny install [--target <target>]")
			fmt.Println("Installs dependencies from tiny.json.")
		case "remove", "rm":
			fmt.Println("usage: tiny remove <name|owner/repo> [--global|--project-only]")
			fmt.Println("Removes a dependency from tiny.json and downloaded libraries.")
		case "deps", "list":
			fmt.Println("usage: tiny deps")
			fmt.Println("Lists downloaded dependencies.")
		case "task":
			fmt.Println("usage: tiny task [name] [args...]")
			fmt.Println("Runs a script from tiny.json, or lists scripts when no name is given.")
		case "version", "ver", "v":
			fmt.Println("usage: tiny version")
			fmt.Println("Prints the Tiny and bytecode versions.")
		case "update":
			fmt.Println("usage: tiny update")
			fmt.Println("Updates Tiny.")
		case "lsp":
			fmt.Println("usage: tiny lsp")
			fmt.Println("Starts the Tiny language server.")
		default:
			fmt.Printf("unknown help topic: %s\n\n", args[0])
			printGeneralHelp()
		}
		return
	}

	printGeneralHelp()
}

func printGeneralHelp() {
	fmt.Println("Tiny")
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  tiny <file.tiny> [args...]")
	fmt.Println("  tiny <command> [args...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  build      Compile a Tiny source file to bytecode")
	fmt.Println("  run        Run a compiled bytecode file")
	fmt.Println("  pack       Build a packed executable")
	fmt.Println("  dist       Build a distributable executable")
	fmt.Println("  init       Create a tiny.json project file")
	fmt.Println("  add        Add and install a dependency")
	fmt.Println("  install    Install dependencies")
	fmt.Println("  remove     Remove a dependency")
	fmt.Println("  deps       List downloaded dependencies")
	fmt.Println("  task       Run or list project tasks")
	fmt.Println("  version    Print version information")
	fmt.Println("  update     Update Tiny")
	fmt.Println("  lsp        Start the language server")
	fmt.Println("  help       Show help")
	fmt.Println()
	fmt.Println("Run 'tiny help <command>' for command-specific help.")
}

func runSourceCommand(args []string) {
	var entryFile string
	cliArgs := []string{}

	disableCache := false
	disableJIT := false
	filteredArgs := []string{}

	if os.Getenv("TINY_DISABLE_CACHE") == "1" {
		disableCache = true
	}
	if os.Getenv("TINY_DISABLE_JIT") == "1" {
		disableJIT = true
	}

	for _, arg := range args {
		filteredArgs = append(filteredArgs, arg)
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

		disableCache = !config.CompilerOptions.BytecodeCache

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
		compileAndRun(entryFile, cliArgs, disableJIT)
		return
	}

	cachePath, err := tinyCachePath(entryFile, hash)
	if !disableCache && err == nil && fileExists(cachePath) {
		runBytecodeFile(cachePath, disableJIT)
		return
	}

	if !disableCache {
		deleteTinyCacheContent(entryFile)
		saveBytecodeFile(entryFile, cachePath, true)
		runBytecodeFile(cachePath, disableJIT)
	} else {
		compileAndRun(entryFile, cliArgs, disableJIT)
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

func runBytecodeFile(path string, disableJit bool) {
	mainBytecode, functions, classes, interfaces, _ := LoadBytecode(path)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	vm := NewVM(VMInfo{
		MainInstructions: mainBytecode,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
		JITDisabled:      disableJit,
	})
	SetPluginSearchPaths(configuredPluginSearchPaths(normalizeTarget("")))
	vm.SetCLIArgs(getScriptArgs())
	vm.Run()
}

func saveBytecodeFile(entryFile string, outFile string, cache bool) {
	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	SaveBytecode(outFile, mainBytecode, functions, classes, interfaces, globalIndex, cache)
}

func compileAndRun(entryFile string, cliArgs []string, disableJit bool) {
	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes, interfaces, _ := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	vm := NewVM(VMInfo{
		MainInstructions: mainBytecode,
		Functions:        functions,
		Classes:          classes,
		Interfaces:       interfaces,
		Packed:           false,
		JITDisabled:      disableJit,
	})
	SetPluginSearchPaths(configuredPluginSearchPaths(normalizeTarget("")))
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

	runBytecodeFile(args[0], false)
}
