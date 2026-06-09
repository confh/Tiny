package main

import (
	"encoding/json"
	"os"

	. "language.com/src/tinyerrors"
)

const tinyLockPath = "tiny.lock"
const tinyLockVersion = 1

type TinyLockFile struct {
	Version      int                             `json:"version"`
	Dependencies map[string]TinyLockedDependency `json:"dependencies"`
}

type TinyLockedDependency struct {
	Source   string `json:"source"`
	Version  string `json:"version"`
	Resolved string `json:"resolved"`
	Owner    string `json:"owner"`
	Repo     string `json:"repo"`
}

func emptyTinyLock() TinyLockFile {
	return TinyLockFile{
		Version:      tinyLockVersion,
		Dependencies: map[string]TinyLockedDependency{},
	}
}

func loadTinyLock() (TinyLockFile, bool) {
	bytes, err := os.ReadFile(tinyLockPath)
	if err != nil {
		return emptyTinyLock(), false
	}

	lock := emptyTinyLock()
	if err := json.Unmarshal(bytes, &lock); err != nil {
		LangError(ErrorRuntime, "failed to parse %s: %v", tinyLockPath, err)
	}

	if lock.Version == 0 {
		lock.Version = tinyLockVersion
	}
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]TinyLockedDependency{}
	}

	return lock, true
}

func writeTinyLock(lock TinyLockFile) {
	if lock.Version == 0 {
		lock.Version = tinyLockVersion
	}
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]TinyLockedDependency{}
	}
	writeJSONFile(tinyLockPath, lock)
}

func lockedDependencyFromConfig(dep TinyDependencyConfig) (TinyLockedDependency, bool) {
	if dep.Source == "" {
		return TinyLockedDependency{}, false
	}

	spec := parseGitHubPackageSource(dep.Source)
	version := dep.Version
	if version == "" {
		version = spec.Ref
	}
	if version == "" {
		return TinyLockedDependency{}, false
	}

	source := canonicalGitHubSource(githubPackageSpec{Owner: spec.Owner, Repo: spec.Repo})
	return TinyLockedDependency{
		Source:   source,
		Version:  version,
		Resolved: source + "@" + version,
		Owner:    spec.Owner,
		Repo:     spec.Repo,
	}, true
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

func applyLockedDependency(dep TinyDependencyConfig, locked TinyLockedDependency) TinyDependencyConfig {
	if locked.Version == "" || !lockedDependencyMatchesConfig(locked, dep) {
		return dep
	}

	dep.Version = locked.Version
	dep.Source = locked.Source
	dep.Path = ""
	return dep
}

func updateTinyLockDependency(lock *TinyLockFile, name string, dep TinyDependencyConfig) bool {
	locked, ok := lockedDependencyFromConfig(dep)
	if !ok {
		return false
	}
	if lock.Dependencies == nil {
		lock.Dependencies = map[string]TinyLockedDependency{}
	}
	if existing, exists := lock.Dependencies[name]; exists && existing == locked {
		return false
	}
	lock.Dependencies[name] = locked
	return true
}
