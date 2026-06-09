package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()

	oldDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("get working dir: %v", err)
	}

	if err := os.Chdir(dir); err != nil {
		t.Fatalf("change working dir: %v", err)
	}

	t.Cleanup(func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Fatalf("restore working dir: %v", err)
		}
	})
}

func writeConfigForTest(t *testing.T, path string, config TinyProjectConfig) {
	t.Helper()

	bytes, err := json.Marshal(config)
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("create config dir: %v", err)
	}

	if err := os.WriteFile(path, bytes, 0644); err != nil {
		t.Fatalf("write config: %v", err)
	}
}

func TestParseGitHubPackageSource(t *testing.T) {
	spec := parseGitHubPackageSource("https://github.com/tiny-lang/sample.git@v1.2.3")

	if spec.Owner != "tiny-lang" || spec.Repo != "sample" || spec.Ref != "v1.2.3" {
		t.Fatalf("unexpected spec: %#v", spec)
	}
}

func TestConfiguredPluginPathsIncludeProjectAndDependencyConfigs(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Target: "windows-amd64",
		Plugins: []TinyProjectPluginConfig{
			{Name: "local", Path: filepath.Join("plugins", "local")},
		},
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {
				Source:  "github:owner/dep",
				Version: "v1",
			},
		},
	})

	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{
		Plugins: []TinyProjectPluginConfig{
			{Name: "native", Path: filepath.Join("native", "dep"), Files: []string{filepath.Join("native", "dep.dat")}},
		},
	})

	paths := configuredPluginPaths("windows-amd64")
	want := map[string]bool{
		filepath.Clean(filepath.Join("plugins", "local.dll")):       false,
		filepath.Clean(filepath.Join(depRoot, "native", "dep.dll")): false,
		filepath.Clean(filepath.Join(depRoot, "native", "dep.dat")): false,
	}

	for _, path := range paths {
		if _, exists := want[path]; exists {
			want[path] = true
		}
	}

	for path, found := range want {
		if !found {
			t.Fatalf("configured plugin path %s not found in %#v", path, paths)
		}
	}
}

func TestLoaderResolvesDependencyEntryImport(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {
				Source:  "github:owner/dep",
				Version: "v1",
			},
		},
	})

	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{
		Entry: "main.tiny",
	})

	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "src", "main.tiny"), []byte(`import library "owner/dep" as Dep;`), 0644); err != nil {
		t.Fatalf("write main source: %v", err)
	}

	if err := os.WriteFile(filepath.Join(depRoot, "main.tiny"), []byte(`export const value = "ok";`), 0644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}

	program := LoadProgram(filepath.Join("src", "main.tiny"))
	if len(program.Statements) != 1 {
		t.Fatalf("expected dependency namespace statement, got %#v", program.Statements)
	}
}

func TestLoaderResolvesLibraryFileImport(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {
				Source:  "github:owner/dep",
				Version: "v1",
			},
		},
	})

	if err := os.MkdirAll(filepath.Join(dir, "src"), 0755); err != nil {
		t.Fatalf("create src dir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(depRoot, "pkg"), 0755); err != nil {
		t.Fatalf("create dependency pkg dir: %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "src", "main.tiny"), []byte(`import library "owner/dep/pkg/tools.tiny" as Tools;`), 0644); err != nil {
		t.Fatalf("write main source: %v", err)
	}

	if err := os.WriteFile(filepath.Join(depRoot, "pkg", "tools.tiny"), []byte(`export const value = "ok";`), 0644); err != nil {
		t.Fatalf("write dependency source: %v", err)
	}

	program := LoadProgram(filepath.Join("src", "main.tiny"))
	if len(program.Statements) != 1 {
		t.Fatalf("expected dependency namespace statement, got %#v", program.Statements)
	}
}

func TestRemovePackageCommandRemovesConfigAndGlobalDependency(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")
	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {Source: "github:owner/dep", Version: "v1"},
			"keep": {
				Source:  "github:owner/keep",
				Version: "v1",
			},
		},
	})
	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{Entry: "main.tiny"})

	removePackageCommand([]string{"dep"})

	config, ok := loadTinyConfig()
	if !ok {
		t.Fatalf("tiny.json missing")
	}
	if _, exists := config.Dependencies["dep"]; exists {
		t.Fatalf("removed dependency still present: %#v", config.Dependencies)
	}
	if _, exists := config.Dependencies["keep"]; !exists {
		t.Fatalf("unrelated dependency was removed: %#v", config.Dependencies)
	}
	if fileExists(filepath.Join(depRoot, "tiny.json")) {
		t.Fatalf("global dependency folder was not removed")
	}
}

func TestListDownloadedDependenciesUsesGlobalCache(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))
	invalidateInstalledLibraryImportCache()

	writeConfigForTest(t, filepath.Join(libraryGlobalRoot("owner", "alpha", "v1"), "tiny.json"), TinyProjectConfig{})
	writeConfigForTest(t, filepath.Join(libraryGlobalRoot("owner", "alpha", "v2"), "tiny.json"), TinyProjectConfig{})
	writeConfigForTest(t, filepath.Join(libraryGlobalRoot("team", "beta", "main"), "tiny.json"), TinyProjectConfig{})

	libs := scanInstalledLibraries()
	if len(libs) != 2 {
		t.Fatalf("downloaded libraries = %#v, want 2", libs)
	}
	if libs[0].Owner != "owner" || libs[0].Repo != "alpha" || len(libs[0].Versions) != 2 {
		t.Fatalf("unexpected first downloaded library: %#v", libs[0])
	}
	if libs[1].Owner != "team" || libs[1].Repo != "beta" || len(libs[1].Versions) != 1 {
		t.Fatalf("unexpected second downloaded library: %#v", libs[1])
	}
}
