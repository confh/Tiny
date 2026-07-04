package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/bytecode"
	tinycompiler "language.com/src/compiler"
	tinyloader "language.com/src/loader"
	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

func DistCommand(args []string) {
	target := ""
	entryFile := ""
	outFile := ""
	extraPlugins := []string{}
	windowed := false
	iconPath := ""

	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected output path after -o")
			}

			outFile = args[i+1]
			i++

		case "--target":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected target after --target")
			}

			target = normalizeTarget(args[i+1])
			i++

		case "--plugin":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected plugin path after --plugin")
			}

			extraPlugins = append(extraPlugins, args[i+1])
			i++

		case "--windowed":
			windowed = true

		case "--icon":
			if i+1 >= len(args) {
				LangError(ErrorRuntime, "expected icon path after --icon")
			}

			iconPath = args[i+1]
			i++

		default:
			if entryFile != "" {
				LangError(ErrorRuntime, "unknown dist argument: %s", args[i])
			}
			entryFile = args[i]
		}
	}

	if entryFile == "" {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny dist <file.tiny> -o <output> [--target windows-amd64|linux-amd64|linux-arm64|darwin-arm64] [--plugin <path>]")
		}

		entryFile = config.Entry
		if target == "" {
			target = normalizeTarget(config.Target)
		}
	} else {
		if target == "" {
			target = normalizeTarget("")
		}
	}

	if outFile == "" {
		outFile = defaultDistOutputName(entryFile, target)
	}

	outFile = addExtensionForTarget(outFile, target)

	distDir := filepath.Dir(outFile)
	if distDir == "" {
		distDir = "."
	}

	err := os.MkdirAll(distDir, 0755)
	if err != nil {
		LangError(ErrorRuntime, "failed to create dist folder: %v", err)
	}

	program := tinyloader.LoadProgram(entryFile)
	pluginPaths := bundledPluginPaths(program, target, extraPlugins)

	program = rewritePluginPathsForDist(program, target)

	packProgramToOutput(program, outFile, target, windowed, iconPath)

	copyPluginsToOutputDir(pluginPaths, distDir)

	fmt.Println("Dist created:", distDir)
}

func defaultPackOutputName(entryFile string, target string) string {
	base := filepath.Base(entryFile)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]

	if target == "windows-amd64" {
		return name + ".exe"
	}

	return name
}

func defaultDistOutputName(entryFile string, target string) string {
	base := filepath.Base(entryFile)
	ext := filepath.Ext(base)
	name := base[:len(base)-len(ext)]

	if target == "windows-amd64" {
		return filepath.Join("dist", name+".exe")
	}

	return filepath.Join("dist", name)
}

func packToOutput(entryFile string, outFile string, target string, windowed bool, iconPath string) {
	target = normalizeTarget(target)

	program := tinyloader.LoadProgram(entryFile)
	pluginPaths := bundledPluginPaths(program, target, nil)

	if len(pluginPaths) > 0 {
		program = rewritePluginPathsForDist(program, target)
	}

	packProgramToOutput(program, outFile, target, windowed, iconPath)

	if len(pluginPaths) > 0 {
		outDir := filepath.Dir(outFile)
		if outDir == "" {
			outDir = "."
		}

		copyPluginsToOutputDir(pluginPaths, outDir)
	}
}

func bundledPluginPaths(program Program, target string, extraPlugins []string) []string {
	pluginPaths := collectPluginPathsFromProgram(program, target)
	pluginPaths = append(pluginPaths, configuredPluginPaths(target)...)

	for _, plugin := range extraPlugins {
		pluginPaths = append(pluginPaths, normalizePluginPathForTarget(plugin, target))
	}

	return preferExistingPluginBundlePaths(pluginPaths)
}

func preferExistingPluginBundlePaths(pluginPaths []string) []string {
	type candidate struct {
		path   string
		exists bool
	}

	byName := map[string]candidate{}
	order := []string{}

	for _, pluginPath := range pluginPaths {
		if pluginPath == "" {
			continue
		}

		pluginPath = filepath.Clean(pluginPath)
		name := pluginDistFileName(pluginPath)
		if name == "" || name == "." {
			continue
		}

		next := candidate{path: pluginPath, exists: fileExists(pluginPath)}
		current, seen := byName[name]
		if !seen {
			byName[name] = next
			order = append(order, name)
			continue
		}

		if !current.exists && next.exists {
			byName[name] = next
		}
	}

	result := []string{}
	for _, name := range order {
		result = append(result, byName[name].path)
	}

	return result
}

func copyPluginsToOutputDir(pluginPaths []string, outputDir string) {
	for _, pluginPath := range pluginPaths {
		err := copyPluginToDist(pluginPath, outputDir)
		if err != nil {
			LangError(ErrorRuntime, "failed to copy plugin %s: %v", pluginPath, err)
		}
	}
}

func packProgramToOutput(program Program, outFile string, target string, windowed bool, iconPath string) {
	target = normalizeTarget(target)

	compiler := tinycompiler.NewCompiler()
	mainInstructions, functions, classes, interfaces, globalIndex := compiler.CompileProgram(program)
	mainInstructions = OptimizeBytecode(mainInstructions)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	bytecodeBytes := SaveBytecodeToBytes(mainInstructions, functions, classes, interfaces, globalIndex, false, true)

	runtimeBytes := getRuntimeBytesForTarget(target)

	if windowed && target == "windows-amd64" {
		mutableRuntime := make([]byte, len(runtimeBytes))
		copy(mutableRuntime, runtimeBytes)

		PatchPESubsystemToGUI(mutableRuntime)
		runtimeBytes = mutableRuntime
	}

	if iconPath != "" {
		if target != "windows-amd64" {
			LangError(ErrorRuntime, "--icon is currently supported only for windows-amd64 targets")
		}

		patchedRuntime, err := applyWindowsIconToPERuntimeBytes(runtimeBytes, iconPath)
		if err != nil {
			LangError(ErrorRuntime, "failed to apply icon: %v", err)
		}
		runtimeBytes = patchedRuntime
	}

	err := writePackedExecutable(outFile, runtimeBytes, bytecodeBytes)
	if err != nil {
		LangError(ErrorRuntime, "failed to write packed executable: %v", err)
	}

	if strings.HasPrefix(target, "linux-") {
		err = os.Chmod(outFile, 0755)
		if err != nil {
			LangError(ErrorRuntime, "failed to chmod linux executable: %v", err)
		}
	}
}

func copyPluginToDist(pluginPath string, distDir string) error {
	source := filepath.Clean(pluginPath)

	if !fileExists(source) {
		return fmt.Errorf("plugin file does not exist")
	}

	target := filepath.Join(distDir, pluginDistFileName(source))

	return copyFile(source, target)
}

func pluginDistFileName(pluginPath string) string {
	return filepath.Base(filepath.Clean(pluginPath))
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func copyFile(src string, dst string) error {
	err := os.MkdirAll(filepath.Dir(dst), 0755)
	if err != nil {
		return err
	}

	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}

	return out.Close()
}
