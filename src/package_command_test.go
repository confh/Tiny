package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	tinyloader "language.com/src/loader"
	. "language.com/src/vm"
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

func TestConfiguredPluginPathsFilterNativePluginEntriesByTarget(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Plugins: []TinyProjectPluginConfig{
			{Name: "tiny_sqlite.dll", Path: filepath.Join("plugins", "tiny_sqlite.dll")},
			{Name: "tiny_sqlite.so", Path: filepath.Join("plugins", "tiny_sqlite.so")},
			{Name: "tiny_sqlite.dylib", Path: filepath.Join("plugins", "tiny_sqlite.dylib")},
			{Name: "config", Path: filepath.Join("plugins", "tiny_sqlite"), Files: []string{
				filepath.Join("plugins", "helper.dll"),
				filepath.Join("plugins", "helper.so"),
				filepath.Join("plugins", "metadata.dat"),
			}},
		},
	})

	linuxPaths := configuredPluginPaths("linux-amd64")
	if !stringSliceContains(linuxPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.so"))) {
		t.Fatalf("expected linux plugin paths to include .so, got %#v", linuxPaths)
	}
	if stringSliceContains(linuxPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.dll"))) ||
		stringSliceContains(linuxPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.dylib"))) ||
		stringSliceContains(linuxPaths, filepath.Clean(filepath.Join("plugins", "helper.dll"))) {
		t.Fatalf("expected linux plugin paths to exclude non-linux native plugins, got %#v", linuxPaths)
	}
	if !stringSliceContains(linuxPaths, filepath.Clean(filepath.Join("plugins", "metadata.dat"))) {
		t.Fatalf("expected linux plugin paths to keep non-native support files, got %#v", linuxPaths)
	}

	windowsPaths := configuredPluginPaths("windows-amd64")
	if !stringSliceContains(windowsPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.dll"))) {
		t.Fatalf("expected windows plugin paths to include .dll, got %#v", windowsPaths)
	}
	if stringSliceContains(windowsPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.so"))) ||
		stringSliceContains(windowsPaths, filepath.Clean(filepath.Join("plugins", "tiny_sqlite.dylib"))) ||
		stringSliceContains(windowsPaths, filepath.Clean(filepath.Join("plugins", "helper.so"))) {
		t.Fatalf("expected windows plugin paths to exclude non-windows native plugins, got %#v", windowsPaths)
	}
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
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

	program := tinyloader.LoadProgram(filepath.Join("src", "main.tiny"))
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

	program := tinyloader.LoadProgram(filepath.Join("src", "main.tiny"))
	if len(program.Statements) != 1 {
		t.Fatalf("expected dependency namespace statement, got %#v", program.Statements)
	}
}

func TestInstallPackagesCommandUsesTinyLockAndCachedDependency(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")
	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{Entry: "main.tiny"})
	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {Source: "github:owner/dep"},
		},
	})
	writeTinyLock(TinyLockFile{
		Version: tinyLockVersion,
		Dependencies: map[string]TinyLockedDependency{
			"dep": {
				Source:   "github:owner/dep",
				Version:  "v1",
				Resolved: "github:owner/dep@v1",
				Owner:    "owner",
				Repo:     "dep",
			},
		},
	})

	installPackagesCommand(nil)

	lock, ok := loadTinyLock()
	if !ok {
		t.Fatalf("tiny.lock missing")
	}
	if got := lock.Dependencies["dep"].Version; got != "v1" {
		t.Fatalf("locked dependency version = %q, want v1", got)
	}
}

func TestInstalledDependencyCacheChecksPluginTarget(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	plainRoot := libraryGlobalRoot("owner", "plain", "v1")
	writeConfigForTest(t, filepath.Join(plainRoot, "tiny.json"), TinyProjectConfig{})
	if !installedDependencyExists("owner", "plain", "v1", "windows-amd64") {
		t.Fatalf("expected plain cached dependency to be reusable without target metadata")
	}

	pluginRoot := libraryGlobalRoot("owner", "plugin", "v1")
	writeConfigForTest(t, filepath.Join(pluginRoot, "tiny.json"), TinyProjectConfig{
		Plugins: []TinyProjectPluginConfig{{Name: "native", Path: "native/plugin"}},
	})
	if installedDependencyExists("owner", "plugin", "v1", "windows-amd64") {
		t.Fatalf("expected plugin dependency without target metadata not to be reused")
	}

	writeInstalledDependencyMetadata(pluginRoot, "linux-amd64")
	if installedDependencyExists("owner", "plugin", "v1", "windows-amd64") {
		t.Fatalf("expected plugin dependency for another target not to be reused")
	}
	if !installedDependencyExists("owner", "plugin", "v1", "linux-amd64") {
		t.Fatalf("expected plugin dependency for matching target to be reused")
	}
}

func TestCopyDirectoryWithIgnoreSkipsPackagePaths(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	files := map[string]string{
		"tiny.json":              "{}",
		"src/main.tiny":          "main",
		"docs/readme.md":         "docs",
		"examples/demo.tiny":     "demo",
		"README.md":              "readme",
		"notes.txt":              "notes",
		"assets/screenshot.png":  "png",
		"assets/runtime.keep":    "keep",
		"nested/tests/test.tiny": "test",
	}

	for name, content := range files {
		path := filepath.Join(src, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatalf("create source dir: %v", err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatalf("write source file %s: %v", name, err)
		}
	}

	err := copyDirectoryWithIgnore(src, dst, []string{
		"docs",
		"examples/**",
		"*.md",
		"assets/*.png",
		"**/tests",
	})
	if err != nil {
		t.Fatalf("copy with ignore: %v", err)
	}

	for _, name := range []string{"tiny.json", "src/main.tiny", "notes.txt", "assets/runtime.keep"} {
		if !fileExists(filepath.Join(dst, filepath.FromSlash(name))) {
			t.Fatalf("expected copied file %s", name)
		}
	}

	for _, name := range []string{"docs/readme.md", "examples/demo.tiny", "README.md", "assets/screenshot.png", "nested/tests/test.tiny"} {
		if fileExists(filepath.Join(dst, filepath.FromSlash(name))) {
			t.Fatalf("expected ignored file %s not to be copied", name)
		}
	}
}

func TestAddPackageCommandWritesTinyLock(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v1")
	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{Entry: "main.tiny"})
	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{})

	addPackageCommand([]string{"dep", "github:owner/dep@v1"})

	config, ok := loadTinyConfig()
	if !ok {
		t.Fatalf("tiny.json missing")
	}
	if got := config.Dependencies["dep"].Version; got != "v1" {
		t.Fatalf("tiny.json dependency version = %q, want v1", got)
	}

	lock, ok := loadTinyLock()
	if !ok {
		t.Fatalf("tiny.lock missing")
	}
	locked := lock.Dependencies["dep"]
	if locked.Source != "github:owner/dep" || locked.Version != "v1" || locked.Resolved != "github:owner/dep@v1" {
		t.Fatalf("unexpected lock entry: %#v", locked)
	}
}

func TestLoaderUsesTinyLockVersionForUnpinnedDependency(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	depRoot := libraryGlobalRoot("owner", "dep", "v2")
	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{Entry: "main.tiny"})
	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {Source: "github:owner/dep"},
		},
	})
	writeTinyLock(TinyLockFile{
		Version: tinyLockVersion,
		Dependencies: map[string]TinyLockedDependency{
			"dep": {
				Source:   "github:owner/dep",
				Version:  "v2",
				Resolved: "github:owner/dep@v2",
				Owner:    "owner",
				Repo:     "dep",
			},
		},
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

	program := tinyloader.LoadProgram(filepath.Join("src", "main.tiny"))
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
	writeTinyLock(TinyLockFile{
		Version: tinyLockVersion,
		Dependencies: map[string]TinyLockedDependency{
			"dep": {
				Source:   "github:owner/dep",
				Version:  "v1",
				Resolved: "github:owner/dep@v1",
				Owner:    "owner",
				Repo:     "dep",
			},
			"keep": {
				Source:   "github:owner/keep",
				Version:  "v1",
				Resolved: "github:owner/keep@v1",
				Owner:    "owner",
				Repo:     "keep",
			},
		},
	})

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

	lock, ok := loadTinyLock()
	if !ok {
		t.Fatalf("tiny.lock missing")
	}
	if _, exists := lock.Dependencies["dep"]; exists {
		t.Fatalf("removed dependency still present in lock: %#v", lock.Dependencies)
	}
	if _, exists := lock.Dependencies["keep"]; !exists {
		t.Fatalf("unrelated lock dependency was removed: %#v", lock.Dependencies)
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

func TestRecursiveDependencyInstallation(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
	t.Setenv("TINY_HOME", filepath.Join(dir, "tiny-home"))

	alphaRoot := libraryGlobalRoot("owner", "alpha", "v1")
	betaRoot := libraryGlobalRoot("owner", "beta", "v2")
	gammaRoot := libraryGlobalRoot("owner", "gamma", "v3")

	writeConfigForTest(t, filepath.Join(dir, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"alpha": {Source: "github:owner/alpha@v1"},
		},
	})

	writeConfigForTest(t, filepath.Join(alphaRoot, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"beta": {Source: "github:owner/beta@v2"},
		},
	})

	writeConfigForTest(t, filepath.Join(betaRoot, "tiny.json"), TinyProjectConfig{
		Dependencies: map[string]TinyDependencyConfig{
			"gamma": {Source: "github:owner/gamma@v3"},
		},
	})

	writeConfigForTest(t, filepath.Join(gammaRoot, "tiny.json"), TinyProjectConfig{})

	installPackagesCommand(nil)

	if !installedDependencyExists("owner", "alpha", "v1", defaultProjectTarget()) {
		t.Fatalf("expected alpha to be cached and checked")
	}
	if !installedDependencyExists("owner", "beta", "v2", defaultProjectTarget()) {
		t.Fatalf("expected beta to be recursively installed/cached")
	}
	if !installedDependencyExists("owner", "gamma", "v3", defaultProjectTarget()) {
		t.Fatalf("expected gamma to be recursively installed/cached")
	}
}

func TestLoaderResolvesLibraryLocalPathAndSubpathImport(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir)
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
				Path:   "../TinyHttpx", // relative to tiny.json dir (projRoot)
			},
		},
	})

	// Write files in local dependency
	writeConfigForTest(t, filepath.Join(libRoot, "tiny.json"), TinyProjectConfig{
		Entry: "src/httpx.tiny",
	})
	if err := os.WriteFile(filepath.Join(libRoot, "src", "httpx.tiny"), []byte(`export const value = "main_httpx";`), 0644); err != nil {
		t.Fatalf("write main httpx: %v", err)
	}
	if err := os.WriteFile(filepath.Join(libRoot, "src", "httpxs.tiny"), []byte(`export const value = "sub_httpxs";`), 0644); err != nil {
		t.Fatalf("write sub httpxs: %v", err)
	}

	// Write files in myproject
	// 1. Importing the library itself (should resolve to its Entry file)
	mainTinyContent := `
import lib "confh/TinyHttpx" as httpx;
import lib "confh/TinyHttpx/src/httpxs.tiny" as httpxs;
export const v1 = httpx.value;
export const v2 = httpxs.value;
`
	mainPath := filepath.Join(projRoot, "src", "main.tiny")
	if err := os.WriteFile(mainPath, []byte(mainTinyContent), 0644); err != nil {
		t.Fatalf("write main.tiny: %v", err)
	}

	program := tinyloader.LoadProgram(mainPath)
	// Statements should contain the namespaces for the imports
	if len(program.Statements) < 2 {
		t.Fatalf("expected at least 2 namespace statements for imports, got %#v", program.Statements)
	}

	// Verify namespace statement content or names
	foundHttpx := false
	foundHttpxs := false
	for _, stmt := range program.Statements {
		if ns, ok := stmt.(NamespaceStmt); ok {
			if ns.Name == "httpx" {
				foundHttpx = true
			} else if ns.Name == "httpxs" {
				foundHttpxs = true
			}
		}
	}
	if !foundHttpx || !foundHttpxs {
		t.Fatalf("expected namespaces 'httpx' and 'httpxs' to be resolved, got foundHttpx=%v, foundHttpxs=%v", foundHttpx, foundHttpxs)
	}
}
