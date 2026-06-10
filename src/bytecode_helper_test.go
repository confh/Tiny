package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBytecodeCacheLibraryInvalidation(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	// Create project root
	projRoot := filepath.Join(dir, "myproject")
	if err := os.MkdirAll(filepath.Join(projRoot, "src"), 0755); err != nil {
		t.Fatalf("create projRoot src dir: %v", err)
	}

	// Create local library dependency root
	libRoot := filepath.Join(dir, "TinyHttpx")
	if err := os.MkdirAll(filepath.Join(libRoot, "src"), 0755); err != nil {
		t.Fatalf("create libRoot src dir: %v", err)
	}

	// Write tiny.json in project root referencing local path dependency
	writeConfigForTest(t, filepath.Join(projRoot, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"TinyHttpx": {
				Source: "github:confh/TinyHttpx",
				Path:   "../TinyHttpx",
			},
		},
	})

	// Write files in local dependency
	writeConfigForTest(t, filepath.Join(libRoot, "tiny.json"), TinyProjectConfig{
		Entry: "src/httpx.tiny",
	})

	libFilePath := filepath.Join(libRoot, "src", "httpx.tiny")
	if err := os.WriteFile(libFilePath, []byte(`export const value = "version 1";`), 0644); err != nil {
		t.Fatalf("write library file: %v", err)
	}

	// Write main.tiny in project
	mainPath := filepath.Join(projRoot, "src", "main.tiny")
	mainTinyContent := `import lib "confh/TinyHttpx" as httpx;`
	if err := os.WriteFile(mainPath, []byte(mainTinyContent), 0644); err != nil {
		t.Fatalf("write main.tiny: %v", err)
	}

	// Hash the project
	hash1, err := hashTinyProject(mainPath, mainTinyContent)
	if err != nil {
		t.Fatalf("hash1 error: %v", err)
	}

	// Update the library file
	if err := os.WriteFile(libFilePath, []byte(`export const value = "version 2";`), 0644); err != nil {
		t.Fatalf("update library file: %v", err)
	}

	// Hash the project again
	hash2, err := hashTinyProject(mainPath, mainTinyContent)
	if err != nil {
		t.Fatalf("hash2 error: %v", err)
	}

	// The hash MUST be different because the library file content changed!
	if hash1 == hash2 {
		t.Fatalf("expected hash to be different after library update, but got same hash %q", hash1)
	}
}
