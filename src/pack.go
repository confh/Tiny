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

//go:embed embedded/tiny_runtime_windows_amd64.exe
var embeddedRuntimeWindowsAMD64 []byte

//go:embed embedded/tiny_runtime_linux_amd64
var embeddedRuntimeLinuxAMD64 []byte

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

func getEmbeddedRuntimeForTarget(target string) []byte {
	switch target {
	case "windows-amd64":
		return embeddedRuntimeWindowsAMD64

	case "linux-amd64":
		return embeddedRuntimeLinuxAMD64

	default:
		LangError(ErrorRuntime, "unsupported target: %s", target)
		return nil
	}
}

func writePackedGoMod(tempDir string, projectRoot string) {
	code := fmt.Sprintf(`module tiny_packed_app

go 1.22

require language.com v0.0.0

replace language.com => %s
`, filepath.ToSlash(projectRoot))

	err := os.WriteFile(filepath.Join(tempDir, "go.mod"), []byte(code), 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write packed go.mod: %v", err)
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
	if len(args) < 1 {
		LangError(ErrorRuntime, "usage: tiny pack <file.tiny> -o <output> [--target windows-amd64|linux-amd64]")
	}

	entryFile := args[0]
	target := normalizeTarget("")
	outFile := defaultPackOutputName(entryFile, target)

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

func writePackedExecutable(outFile string, runtimeBytes []byte, bytecodeBytes []byte) error {
	err := os.MkdirAll(filepath.Dir(outFile), 0755)
	if err != nil && filepath.Dir(outFile) != "." {
		return err
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

	_, err = f.Write([]byte("TINYAPP1"))
	if err != nil {
		return err
	}

	return nil
}

func copyFile(src string, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		LangError(ErrorRuntime, "failed to read %s: %v", src, err)
	}

	err = os.WriteFile(dst, data, 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write %s: %v", dst, err)
	}
}

func copyGoModFiles(tempDir string, sourceDir string) {
	dir := sourceDir

	for {
		goModPath := filepath.Join(dir, "go.mod")

		if _, err := os.Stat(goModPath); err == nil {
			copyFile(goModPath, filepath.Join(tempDir, "go.mod"))

			goSumPath := filepath.Join(dir, "go.sum")
			if _, err := os.Stat(goSumPath); err == nil {
				copyFile(goSumPath, filepath.Join(tempDir, "go.sum"))
			}

			return
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}

		dir = parent
	}
}
