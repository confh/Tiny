package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func packCommand(args []string) {
	if len(args) < 1 {
		langError(ErrorRuntime, "usage: tiny pack <file.tiny> -o <app.exe>")
	}

	entryFile := args[0]
	outFile := defaultPackedOutputName()

	for i := 1; i < len(args); i++ {
		if args[i] == "-o" && i+1 < len(args) {
			outFile = args[i+1]
			i++
		}
	}

	absOutFile, err := filepath.Abs(outFile)
	if err != nil {
		langError(ErrorRuntime, "failed to resolve output path: %v", err)
	}

	tempDir, err := os.MkdirTemp("", "tiny-pack-*")
	if err != nil {
		langError(ErrorRuntime, "failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	bytecodePath := filepath.Join(tempDir, "app.tbc")

	program := LoadProgram(entryFile)

	compiler := NewCompiler()
	mainBytecode, functions, classes := compiler.CompileProgram(program)

	SaveBytecode(bytecodePath, mainBytecode, functions, classes)

	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		langError(ErrorRuntime, "failed to find compiler source directory")
	}

	sourceDir := filepath.Dir(currentFile)

	copyRuntimeFiles(tempDir, sourceDir)
	writePackedMain(tempDir)

	cmd := exec.Command("go", "build", "-ldflags=-s -w", "-o", absOutFile, ".")
	cmd.Dir = tempDir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	err = cmd.Run()
	if err != nil {
		langError(ErrorRuntime, "failed to build packed executable: %v", err)
	}

	fmt.Println("Packed", absOutFile)
}

func defaultPackedOutputName() string {
	if runtime.GOOS == "windows" {
		return "app.exe"
	}

	return "app"
}

func copyRuntimeFiles(tempDir string, sourceDir string) {
	files, err := os.ReadDir(sourceDir)
	if err != nil {
		langError(ErrorRuntime, "failed to read source directory: %v", err)
	}

	for _, file := range files {
		name := file.Name()

		if file.IsDir() {
			continue
		}

		if !strings.HasSuffix(name, ".go") {
			continue
		}

		if name == "main.go" {
			continue
		}

		src := filepath.Join(sourceDir, name)
		dst := filepath.Join(tempDir, name)

		copyFile(src, dst)
	}

	copyGoModFiles(tempDir, sourceDir)
}

func copyFile(src string, dst string) {
	data, err := os.ReadFile(src)
	if err != nil {
		langError(ErrorRuntime, "failed to read %s: %v", src, err)
	}

	err = os.WriteFile(dst, data, 0644)
	if err != nil {
		langError(ErrorRuntime, "failed to write %s: %v", dst, err)
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

func writePackedMain(tempDir string) {
	code := `package main

import _ "embed"

//go:embed app.tbc
var embeddedBytecode []byte

func main() {
	defer handleLangError()

	mainBytecode, functions, classes := LoadBytecodeFromBytes(embeddedBytecode)

	vm := NewVM(mainBytecode, functions, classes)
	vm.Run()
}
`

	err := os.WriteFile(filepath.Join(tempDir, "main.go"), []byte(code), 0644)
	if err != nil {
		langError(ErrorRuntime, "failed to write packed main.go: %v", err)
	}
}
