package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "language.com/src/tinyerrors"
)

type libraryImportPath struct {
	Owner string
	Repo  string
	Rest  string
}

type installedLibraryInfo struct {
	Owner    string
	Repo     string
	Versions []string
}

var installedLibraryImportCacheRoot string
var installedLibraryImportCache []string

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

func libraryGlobalRoot(owner string, repo string, version string) string {
	return filepath.Join(tinyGlobalDepsDir(), owner, repo, sanitizeLibraryVersion(version))
}

func sanitizeLibraryVersion(version string) string {
	version = strings.TrimSpace(version)
	if version == "" {
		return "default"
	}

	version = strings.ReplaceAll(version, "\\", "_")
	version = strings.ReplaceAll(version, "/", "_")
	version = strings.ReplaceAll(version, ":", "_")
	return version
}

func parseLibraryImportPath(path string) (libraryImportPath, bool) {
	path = strings.TrimSpace(filepath.ToSlash(path))
	parts := strings.Split(path, "/")
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return libraryImportPath{}, false
	}

	return libraryImportPath{
		Owner: parts[0],
		Repo:  parts[1],
		Rest:  strings.Join(parts[2:], "/"),
	}, true
}

func dependencyVersionForLibrary(owner string, repo string) string {
	config, ok := loadTinyConfig()
	if !ok {
		return ""
	}

	for _, dep := range config.Dependencies {
		if dep.Source == "" {
			continue
		}
		spec := parseGitHubPackageSource(dep.Source)
		if spec.Owner == owner && spec.Repo == repo {
			if dep.Version != "" {
				return dep.Version
			}
			return spec.Ref
		}
	}

	return ""
}

func resolveLibraryImportPath(importPath string) string {
	lib, ok := parseLibraryImportPath(importPath)
	if !ok {
		return importPath
	}

	version := dependencyVersionForLibrary(lib.Owner, lib.Repo)
	root := ""

	if version != "" {
		root = libraryGlobalRoot(lib.Owner, lib.Repo, version)
	} else {
		root = firstInstalledLibraryRoot(lib.Owner, lib.Repo)
		if root == "" {
			root = libraryGlobalRoot(lib.Owner, lib.Repo, "")
		}
	}

	if lib.Rest == "" {
		config, ok := loadTinyConfigFrom(filepath.Join(root, "tiny.json"))
		if ok && config.Entry != "" {
			return filepath.Clean(filepath.Join(root, config.Entry))
		}
		return filepath.Clean(filepath.Join(root, "main.tiny"))
	}

	return filepath.Clean(filepath.Join(root, filepath.FromSlash(lib.Rest)))
}

func firstInstalledLibraryRoot(owner string, repo string) string {
	base := filepath.Join(tinyGlobalDepsDir(), owner, repo)
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}

	names := []string{}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}

	if len(names) == 0 {
		return ""
	}

	sort.Strings(names)
	return filepath.Join(base, names[0])
}

func scanInstalledLibraries() []installedLibraryInfo {
	root := tinyGlobalDepsDir()
	owners, err := os.ReadDir(root)
	if err != nil {
		return []installedLibraryInfo{}
	}

	result := []installedLibraryInfo{}
	for _, owner := range owners {
		if !owner.IsDir() {
			continue
		}

		repos, err := os.ReadDir(filepath.Join(root, owner.Name()))
		if err != nil {
			continue
		}

		for _, repo := range repos {
			if !repo.IsDir() {
				continue
			}

			versionEntries, err := os.ReadDir(filepath.Join(root, owner.Name(), repo.Name()))
			if err != nil {
				continue
			}

			versions := []string{}
			for _, version := range versionEntries {
				if version.IsDir() {
					versions = append(versions, version.Name())
				}
			}
			sort.Strings(versions)

			result = append(result, installedLibraryInfo{
				Owner:    owner.Name(),
				Repo:     repo.Name(),
				Versions: versions,
			})
		}
	}

	sort.Slice(result, func(i int, j int) bool {
		left := result[i].Owner + "/" + result[i].Repo
		right := result[j].Owner + "/" + result[j].Repo
		return left < right
	})
	return result
}

func installedLibraryImportCompletions() []string {
	root := tinyGlobalDepsDir()
	if installedLibraryImportCacheRoot == root && installedLibraryImportCache != nil {
		return append([]string{}, installedLibraryImportCache...)
	}

	result := []string{}
	for _, lib := range scanInstalledLibraries() {
		result = append(result, lib.Owner+"/"+lib.Repo)
	}

	sort.Strings(result)
	installedLibraryImportCacheRoot = root
	installedLibraryImportCache = append([]string{}, result...)
	return result
}

func invalidateInstalledLibraryImportCache() {
	installedLibraryImportCacheRoot = ""
	installedLibraryImportCache = nil
}

func removeInstalledLibrary(owner string, repo string, version string) error {
	root := filepath.Clean(tinyGlobalDepsDir())
	target := ""

	if version == "" {
		target = filepath.Join(root, owner, repo)
	} else {
		target = libraryGlobalRoot(owner, repo, version)
	}

	target = filepath.Clean(target)
	if target == root || !strings.HasPrefix(target, root+string(filepath.Separator)) {
		return os.ErrInvalid
	}

	err := os.RemoveAll(target)
	if err == nil {
		invalidateInstalledLibraryImportCache()
		if version != "" {
			pruneEmptyLibraryParents(root, owner, repo)
		}
	}
	return err
}

func pruneEmptyLibraryParents(root string, owner string, repo string) {
	repoDir := filepath.Join(root, owner, repo)
	if entries, err := os.ReadDir(repoDir); err == nil && len(entries) == 0 {
		_ = os.Remove(repoDir)
	}

	ownerDir := filepath.Join(root, owner)
	if entries, err := os.ReadDir(ownerDir); err == nil && len(entries) == 0 {
		_ = os.Remove(ownerDir)
	}
}
