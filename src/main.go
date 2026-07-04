package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	goruntime "runtime"
	"strconv"
	"time"

	. "language.com/src/bytecode"
	tinycompiler "language.com/src/compiler"
	tinyloader "language.com/src/loader"
	. "language.com/src/tinyerrors"
	. "language.com/src/version"
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

func formatCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny fmt <file.tiny>")
	}

	file := args[0]

	fileContent, err := os.ReadFile(file)
	if err != nil {
		LangError(ErrorRuntime, "error while reading file: %s", err)
	}

	formatted := formatTinyDocument(string(fileContent))

	err = os.WriteFile(file, []byte(formatted), 0644)
	if err != nil {
		LangError(ErrorRuntime, "error while writing file: %s", err)
	}

	fmt.Println("Formatted", file)
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

		case "fmt":
			formatCommand(os.Args[2:])
			return

		case "run":
			runBytecodeCommand(os.Args[2:])
			return

		case "watch", "--watch":
			watchCommand(os.Args[2:])
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
			VersionCommand()
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
		case "watch", "--watch":
			fmt.Println("usage: tiny --watch [file.tiny] [args...]")
			fmt.Println("Runs a Tiny source file and restarts when it or its imported files change.")
		case "pack":
			fmt.Println("usage: tiny pack <file.tiny> -o <output> [--target windows-amd64|linux-amd64|linux-arm64|darwin-arm64] [--windowed] [--icon <icon.ico>]")
			fmt.Println("Builds a packed executable from a Tiny source file.")
		case "dist":
			fmt.Println("usage: tiny dist <file.tiny> -o <output> [--target windows-amd64|linux-amd64|linux-arm64|darwin-arm64] [--plugin <path>] [--windowed] [--icon <icon.ico>]")
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
	fmt.Println("  watch      Restart a source program when it or its imports change")
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

	if filepath.Ext(entryFile) == ".tbc" {
		runBytecodeFile(entryFile, disableJIT)
		return
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

func saveBytecodeFile(entryFile string, outFile string, cache bool, preserveAllFunctions ...bool) {
	program := tinyloader.LoadProgram(entryFile)

	compiler := tinycompiler.NewCompiler()
	if len(preserveAllFunctions) > 0 && preserveAllFunctions[0] {
		compiler.SetPreserveAllFunctions(true)
	}
	mainBytecode, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)

	mainBytecode = OptimizeBytecode(mainBytecode)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	SaveBytecode(outFile, mainBytecode, functions, classes, interfaces, globalIndex, cache, !cache)
}

func compileAndRun(entryFile string, cliArgs []string, disableJit bool) {
	program := tinyloader.LoadProgram(entryFile)

	compiler := tinycompiler.NewCompiler()
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
	preserveAll := false

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected output path after -o")
			}
			outFile = args[i+1]
			i++
		case "--preserve-all":
			preserveAll = true
		}
	}

	saveBytecodeFile(entryFile, outFile, false, preserveAll)

	fmt.Println("Built", outFile)
}

func runBytecodeCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny run <file.tbc>")
	}

	runBytecodeFile(args[0], false)
}

type watchFileState struct {
	modTime time.Time
	exists  bool
}

type watchChild struct {
	cmd  *exec.Cmd
	done chan error
}

func watchCommand(args []string) {
	entryFile := ""
	cliArgs := []string{}

	if len(args) > 0 {
		entryFile = args[0]
		cliArgs = args[1:]
	} else {
		config, ok := loadTinyConfig()
		if !ok || config.Entry == "" {
			LangError(ErrorRuntime, "usage: tiny --watch [file.tiny] [args...]")
		}
		entryFile = config.Entry
	}

	exe, err := os.Executable()
	if err != nil {
		LangError(ErrorRuntime, "failed to locate tiny executable: %v", err)
	}

	childArgs := append([]string{entryFile}, cliArgs...)
	childEnv := append(os.Environ(), "TINY_DISABLE_CACHE=1")

	fmt.Printf("[watch] watching %s\n", entryFile)

	files := watchedFilesForEntry(entryFile)
	state := snapshotWatchedFiles(files)
	child := startWatchChild(exe, childArgs, childEnv)
	childDone := child.done

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt)
	defer signal.Stop(signals)

	for {
		select {
		case <-signals:
			stopWatchChild(child)
			fmt.Println("[watch] stopped")
			return

		case err := <-childDone:
			if err != nil {
				fmt.Printf("[watch] process exited: %v\n", err)
			} else {
				fmt.Println("[watch] process exited")
			}
			child = nil
			childDone = nil

		case <-ticker.C:
			nextState := snapshotWatchedFiles(files)
			if changedFile, changed := changedWatchedFile(state, nextState); changed {
				switch v := runtime.GOOS; v {
				case "windows":
					cmd := exec.Command("cmd", "/c", "cls")
					cmd.Stdout = os.Stdout
					cmd.Run()

				case "linux":
					cmd := exec.Command("clear")
					cmd.Stdout = os.Stdout
					cmd.Run()
				}
				cwd, err := os.Getwd()
				if err != nil {
					log.Fatalf("failed to get cwd: %v", err)
				}
				relPath, err := filepath.Rel(cwd, changedFile)
				if err != nil {
					relPath = changedFile
				}
				fmt.Printf("[watch] change detected: %s\n", relPath)
				stopWatchChild(child)
				files = watchedFilesForEntry(entryFile)
				state = snapshotWatchedFiles(files)
				child = startWatchChild(exe, childArgs, childEnv)
				childDone = child.done
			}
		}
	}
}

func watchedFilesForEntry(entryFile string) []string {
	files := []string{}

	func() {
		defer func() {
			if recover() != nil {
				files = []string{}
			}
		}()

		_, files = tinyloader.LoadProgramWithFiles(entryFile)
	}()

	if len(files) == 0 {
		abs, err := filepath.Abs(entryFile)
		if err == nil {
			files = append(files, filepath.Clean(abs))
		} else {
			files = append(files, entryFile)
		}
	}

	return files
}

func snapshotWatchedFiles(files []string) map[string]watchFileState {
	state := map[string]watchFileState{}
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			state[file] = watchFileState{exists: false}
			continue
		}
		state[file] = watchFileState{exists: true, modTime: info.ModTime()}
	}
	return state
}

func changedWatchedFile(previous map[string]watchFileState, next map[string]watchFileState) (string, bool) {
	for file, previousState := range previous {
		nextState, exists := next[file]
		if !exists || previousState.exists != nextState.exists || !previousState.modTime.Equal(nextState.modTime) {
			return file, true
		}
	}

	for file := range next {
		if _, exists := previous[file]; !exists {
			return file, true
		}
	}

	return "", false
}

func startWatchChild(exe string, args []string, env []string) *watchChild {
	cmd := exec.Command(exe, args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Env = env

	child := &watchChild{
		cmd:  cmd,
		done: make(chan error, 1),
	}

	fmt.Printf("[watch] starting tiny %s\n", filepath.Base(args[0]))
	if err := cmd.Start(); err != nil {
		fmt.Printf("[watch] failed to start: %v\n", err)
		child.done <- err
		return child
	}

	go func() {
		child.done <- cmd.Wait()
	}()

	return child
}

func stopWatchChild(child *watchChild) {
	if child == nil || child.cmd == nil || child.cmd.Process == nil {
		return
	}

	if goruntime.GOOS == "windows" {
		_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(child.cmd.Process.Pid)).Run()
	} else {
		_ = child.cmd.Process.Kill()
	}

	select {
	case <-child.done:
	case <-time.After(2 * time.Second):
	}
}
