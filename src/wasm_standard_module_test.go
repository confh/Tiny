package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWasmModuleExposed(t *testing.T) {
	// 1. Create a temp directory for compiling the WASM module
	tmpDir, err := os.MkdirTemp("", "tiny_wasm_test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 2. Write the Go test code
	goCode := `
package main

import "C"

//go:wasmimport env host_func
func host_func(val float64) float64

//export add
func add(a, b float64) float64 {
	return a + b
}

//export call_host
func call_host(a float64) float64 {
	return host_func(a) + 100.0
}

func main() {}
`
	goFile := filepath.Join(tmpDir, "main.go")
	err = os.WriteFile(goFile, []byte(goCode), 0644)
	if err != nil {
		t.Fatalf("failed to write Go source: %v", err)
	}

	// 3. Compile Go code to WASM using TinyGo
	wasmFile := filepath.Join(tmpDir, "test_module.wasm")
	cmd := exec.Command("tinygo", "build", "-target=wasi", "-scheduler=none", "-no-debug", "-o", wasmFile, goFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		t.Fatalf("failed to compile WASM with TinyGo: %s\n%v", stderr.String(), err)
	}

	// Get the absolute path to the test script before changing CWD
	scriptPath, err := filepath.Abs(fixturePath("wasm_test.tiny"))
	if err != nil {
		t.Fatalf("failed to get absolute path: %v", err)
	}

	// Copy the compiled WASM binary to the repository testdata directory so it can be run manually
	repoWasmPath, err := filepath.Abs(fixturePath("test_module.wasm"))
	if err == nil {
		wasmBytes, err := os.ReadFile(wasmFile)
		if err == nil {
			_ = os.WriteFile(repoWasmPath, wasmBytes, 0644)
		}
	}

	// 4. Change current working directory to tmpDir so the Tiny script can read "test_module.wasm" relatively
	oldCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get cwd: %v", err)
	}
	err = os.Chdir(tmpDir)
	if err != nil {
		t.Fatalf("failed to chdir to temp dir: %v", err)
	}
	defer os.Chdir(oldCwd)

	// 5. Run the Tiny test script using runTinyFile
	out := requireTinySuccess(t, runTinyFile(t, scriptPath))

	// 6. Assert output
	want := strings.Join([]string{
		"30.75",
		"200",
		"1",
		"65",
		"66",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}
