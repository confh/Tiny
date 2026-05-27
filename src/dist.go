package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	. "language.com/src/bytecode"
	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

func DistCommand(args []string) {
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny dist <file.tiny> -o <output> [--target windows-amd64|linux-amd64] [--plugin <path>]")
	}

	entryFile := args[0]
	target := normalizeTarget("")
	outFile := defaultDistOutputName(entryFile, target)
	extraPlugins := []string{}

	for i := 1; i < len(args); i++ {
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

		default:
			LangError(ErrorRuntime, "unknown dist argument: %s", args[i])
		}
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

	program := LoadProgram(entryFile)

	pluginPaths := collectPluginPathsFromProgram(program, target)

	for _, plugin := range extraPlugins {
		pluginPaths = append(pluginPaths, normalizePluginPathForTarget(plugin, target))
	}

	program = rewritePluginPathsForDist(program, target)

	packProgramToOutput(program, outFile, target)

	for _, pluginPath := range pluginPaths {
		err := copyPluginToDist(pluginPath, distDir)
		if err != nil {
			LangError(ErrorRuntime, "failed to copy plugin %s: %v", pluginPath, err)
		}
	}

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

func packToOutput(entryFile string, outFile string, target string) {
	target = normalizeTarget(target)

	program := LoadProgram(entryFile)
	packProgramToOutput(program, outFile, target)
}

func packProgramToOutput(program Program, outFile string, target string) {
	target = normalizeTarget(target)

	compiler := NewCompiler()
	mainInstructions, functions, classes := compiler.CompileProgram(program)
	mainInstructions = OptimizeBytecode(mainInstructions)

	for name, fn := range functions {
		fn.Instructions = OptimizeBytecode(fn.Instructions)
		functions[name] = fn
	}

	bytecodeBytes := SaveBytecodeToBytes(mainInstructions, functions, classes, false)

	runtimeBytes := getEmbeddedRuntimeForTarget(target)

	err := writePackedExecutable(outFile, runtimeBytes, bytecodeBytes)
	if err != nil {
		LangError(ErrorRuntime, "failed to write packed executable: %v", err)
	}

	if target == "linux-amd64" {
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
