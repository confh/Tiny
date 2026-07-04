package loader

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type ImportState int

const (
	ImportNotLoaded ImportState = iota
	ImportLoading
	ImportLoaded
)

type TinyProjectConfig struct {
	Entry        string                          `json:"entry"`
	Dependencies map[string]TinyDependencyConfig `json:"dependencies"`
}

type TinyDependencyConfig struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type TinyLockFile struct {
	Version      int                             `json:"version"`
	Dependencies map[string]TinyLockedDependency `json:"dependencies"`
}

type TinyLockedDependency struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	Owner   string `json:"owner"`
	Repo    string `json:"repo"`
}

type githubPackageSpec struct {
	Owner string
	Repo  string
	Ref   string
}

type libraryImportPath struct {
	Owner string
	Repo  string
	Rest  string
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func loadTinyConfig() (TinyProjectConfig, bool) {
	return loadTinyConfigFrom("tiny.json")
}

func findProjectRoot(startPath string) string {
	if startPath == "" {
		return ""
	}
	abs, err := filepath.Abs(startPath)
	if err != nil {
		abs = startPath
	}
	dir := abs
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	} else if err != nil && filepath.Ext(dir) != "" {
		dir = filepath.Dir(dir)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "tiny.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func loadTinyConfigFrom(path string) (TinyProjectConfig, bool) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return TinyProjectConfig{}, false
	}
	var config TinyProjectConfig
	if err := json.Unmarshal(bytes, &config); err != nil {
		LangError(ErrorRuntime, "failed to parse %s: %v", path, err)
	}
	if config.Dependencies == nil {
		config.Dependencies = map[string]TinyDependencyConfig{}
	}
	return config, true
}

func loadTinyLock() (TinyLockFile, bool) {
	bytes, err := os.ReadFile("tiny.lock")
	if err != nil {
		return TinyLockFile{Version: 1, Dependencies: map[string]TinyLockedDependency{}}, false
	}
	lock := TinyLockFile{Version: 1, Dependencies: map[string]TinyLockedDependency{}}
	if err := json.Unmarshal(bytes, &lock); err != nil {
		LangError(ErrorRuntime, "failed to parse tiny.lock: %v", err)
	}
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]TinyLockedDependency{}
	}
	return lock, true
}

func parseGitHubPackageSource(source string) githubPackageSpec {
	source = strings.TrimSpace(source)
	source = strings.TrimPrefix(source, "github:")
	source = strings.TrimPrefix(source, "https://github.com/")
	source = strings.TrimSuffix(source, ".git")

	ref := ""
	if at := strings.LastIndex(source, "@"); at >= 0 {
		ref = source[at+1:]
		source = source[:at]
	}

	parts := strings.Split(strings.Trim(source, "/"), "/")
	spec := githubPackageSpec{Ref: ref}
	if len(parts) > 0 {
		spec.Owner = parts[0]
	}
	if len(parts) > 1 {
		spec.Repo = parts[1]
	}
	return spec
}

func canonicalGitHubSource(spec githubPackageSpec) string {
	if spec.Owner == "" || spec.Repo == "" {
		return ""
	}
	return "github:" + spec.Owner + "/" + spec.Repo
}

func lockedDependencyMatchesConfig(locked TinyLockedDependency, dep TinyDependencyConfig) bool {
	if dep.Source == "" || locked.Version == "" {
		return false
	}
	spec := parseGitHubPackageSource(dep.Source)
	if locked.Owner != "" && (locked.Owner != spec.Owner || locked.Repo != spec.Repo) {
		return false
	}
	if locked.Source != "" && locked.Source != canonicalGitHubSource(githubPackageSpec{Owner: spec.Owner, Repo: spec.Repo}) {
		return false
	}
	requestedVersion := dep.Version
	if requestedVersion == "" {
		requestedVersion = spec.Ref
	}
	return requestedVersion == "" || requestedVersion == locked.Version
}

func tinyGlobalDepsDir() string {
	if tinyHome := strings.TrimSpace(os.Getenv("TINY_HOME")); tinyHome != "" {
		return filepath.Join(tinyHome, "deps")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		LangError(ErrorRuntime, "failed to locate user home directory")
	}
	return filepath.Join(home, ".tiny", "deps")
}

func sanitizeLibraryVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "default"
	}
	version = strings.NewReplacer("\\", "_", "/", "_", ":", "_").Replace(version)
	return version
}

func libraryGlobalRoot(owner string, repo string, version string) string {
	return filepath.Join(tinyGlobalDepsDir(), owner, repo, sanitizeLibraryVersion(version))
}

func parseLibraryImportPath(path string) (libraryImportPath, bool) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return libraryImportPath{}, false
	}
	return libraryImportPath{Owner: parts[0], Repo: parts[1], Rest: strings.Join(parts[2:], "/")}, true
}

func resolveLibraryImportPath(importPath string, currentFilePath string) string {
	lib, ok := parseLibraryImportPath(importPath)
	if !ok {
		return importPath
	}
	version := ""
	projectRoot := findProjectRoot(currentFilePath)
	configPath := "tiny.json"
	if projectRoot != "" {
		configPath = filepath.Join(projectRoot, "tiny.json")
	}
	if config, ok := loadTinyConfigFrom(configPath); ok {
		for _, dep := range config.Dependencies {
			spec := parseGitHubPackageSource(dep.Source)
			if spec.Owner != lib.Owner || spec.Repo != lib.Repo {
				continue
			}
			if dep.Path != "" {
				root := dep.Path
				if !filepath.IsAbs(root) {
					base := projectRoot
					if base == "" {
						base = "."
					}
					root = filepath.Clean(filepath.Join(base, root))
				}
				if lib.Rest == "" {
					if depConfig, ok := loadTinyConfigFrom(filepath.Join(root, "tiny.json")); ok && depConfig.Entry != "" {
						return filepath.Clean(filepath.Join(root, depConfig.Entry))
					}
					return filepath.Clean(filepath.Join(root, "main.tiny"))
				}
				return filepath.Clean(filepath.Join(root, filepath.FromSlash(lib.Rest)))
			}
			version = dep.Version
			if version == "" {
				version = spec.Ref
			}
			if version == "" {
				if lock, ok := loadTinyLock(); ok {
					for _, locked := range lock.Dependencies {
						if locked.Owner == lib.Owner && locked.Repo == lib.Repo && lockedDependencyMatchesConfig(locked, dep) {
							version = locked.Version
							break
						}
					}
				}
			}
			break
		}
	}
	root := libraryGlobalRoot(lib.Owner, lib.Repo, version)
	if lib.Rest == "" {
		if config, ok := loadTinyConfigFrom(filepath.Join(root, "tiny.json")); ok && config.Entry != "" {
			return filepath.Clean(filepath.Join(root, config.Entry))
		}
		return filepath.Clean(filepath.Join(root, "main.tiny"))
	}
	return filepath.Clean(filepath.Join(root, filepath.FromSlash(lib.Rest)))
}

func unwrapExport(stmt Stmt) (Stmt, bool) {
	if exp, ok := stmt.(ExportStmt); ok {
		return exp.Inner, true
	}
	return stmt, false
}
