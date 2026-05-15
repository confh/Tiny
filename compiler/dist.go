package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
)

func distCommand(args []string) {
	if len(args) < 1 {
		langError(ErrorRuntime, "usage: tiny dist <file.tiny> -o <output>")
	}

	entryFile := args[0]
	outFile := defaultPackedOutputName()
	extraPlugins := []string{}

	for i := 1; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outFile = args[i+1]
			i++
			continue
		}

		if args[i] == "--plugin" && i+1 < len(args) {
			extraPlugins = append(extraPlugins, args[i+1])
			i++
			continue
		}
	}

	outFile = addExeExtensionIfNeeded(outFile)

	distDir := filepath.Dir(outFile)

	err := os.MkdirAll(distDir, 0755)
	if err != nil {
		langError(ErrorRuntime, "failed to create dist folder: %v", err)
	}

	program := LoadProgram(entryFile)

	pluginPaths := collectPluginPathsFromProgram(program)

	for _, plugin := range extraPlugins {
		pluginPaths = append(pluginPaths, normalizePluginPath(plugin))
	}

	packCommand([]string{entryFile, "-o", outFile})

	for _, pluginPath := range pluginPaths {
		err := copyPluginToDist(pluginPath, distDir)
		if err != nil {
			langError(ErrorRuntime, "failed to copy plugin %s: %v", pluginPath, err)
		}
	}

	fmt.Println("Dist created:", distDir)
}

func addExeExtensionIfNeeded(path string) string {
	if runtime.GOOS == "windows" && filepath.Ext(path) == "" {
		return path + ".exe"
	}

	return path
}

func dirExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func copyDir(src string, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)

		if info.IsDir() {
			return os.MkdirAll(target, info.Mode())
		}

		return cpFile(path, target)
	})
}

func cpFile(src string, dst string) error {
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
