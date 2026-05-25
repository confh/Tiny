package main

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	. "language.com/src/tinyerrors"

	_ "embed"
)

func normalizePluginPathForTarget(path string, target string) string {
	ext := filepath.Ext(path)

	if ext != "" {
		return path
	}

	switch target {
	case "windows-amd64":
		return path + ".dll"

	case "linux-amd64":
		return path + ".so"

	default:
		return path
	}
}

func normalizeTarget(target string) string {
	if target == "" {
		if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
			return "windows-amd64"
		}

		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			return "linux-amd64"
		}

		LangError(ErrorRuntime, "unsupported default target: %s-%s", runtime.GOOS, runtime.GOARCH)
	}

	return target
}

func packCommand(args []string) {
	entryFile := ""
	outFile := ""
	target := normalizeTarget("")

	if len(args) == 0 {
		config, ok := loadTinyConfig()
		if !ok {
			LangError(ErrorRuntime, "usage: tiny pack <file.tiny> -o <output>")
		}

		entryFile = config.Entry

		name := config.Name
		if name == "" {
			name = "app"
		}

		outFile = filepath.Join(config.OutDir, name)
		target = config.Target
	} else {
		entryFile = args[0]
		outFile = defaultPackOutputName(entryFile, target)
	}

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

		default:
			LangError(ErrorRuntime, "unknown pack argument: %s", args[i])
		}
	}

	outFile = addExtensionForTarget(outFile, target)

	packToOutput(entryFile, outFile, target)

	fmt.Println("Packed:", outFile)
}

func addExtensionForTarget(path string, target string) string {
	if target == "windows-amd64" && filepath.Ext(path) == "" {
		return path + ".exe"
	}

	return path
}

var tinyPackMagic = []byte("TINYAPP1")

func writePackedExecutable(outFile string, runtimeBytes []byte, bytecodeBytes []byte) error {
	dir := filepath.Dir(outFile)

	if dir != "." && dir != "" {
		err := os.MkdirAll(dir, 0755)
		if err != nil {
			return err
		}
	}

	f, err := os.Create(outFile)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(runtimeBytes)
	if err != nil {
		return err
	}

	_, err = f.Write(bytecodeBytes)
	if err != nil {
		return err
	}

	sizeBytes := make([]byte, 8)
	binary.LittleEndian.PutUint64(sizeBytes, uint64(len(bytecodeBytes)))

	_, err = f.Write(sizeBytes)
	if err != nil {
		return err
	}

	_, err = f.Write(tinyPackMagic)
	if err != nil {
		return err
	}

	return nil
}
