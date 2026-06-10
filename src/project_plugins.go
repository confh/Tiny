package main

import (
	"path/filepath"
)

func configuredPluginPaths(target string) []string {
	seen := map[string]bool{}
	result := []string{}

	add := func(path string) {
		if path == "" {
			return
		}

		path = filepath.Clean(path)
		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}

	config, ok := loadTinyConfig()
	if !ok {
		return result
	}

	addPluginConfigPaths(".", config, target, add)

	for name, dep := range config.Dependencies {
		depRoot := dep.Path
		if depRoot == "" && dep.Source != "" {
			spec := parseGitHubPackageSource(dep.Source)
			version := dep.Version
			if version == "" {
				version = spec.Ref
			}
			depRoot = libraryGlobalRoot(spec.Owner, spec.Repo, version)
		}

		if depRoot == "" {
			depRoot = filepath.Join(".tinydeps", name)
		}

		depConfig, ok := loadTinyConfigFrom(filepath.Join(depRoot, "tiny.json"))
		if !ok {
			continue
		}

		addPluginConfigPaths(depRoot, depConfig, target, add)
	}

	return result
}

func configuredPluginSearchPaths(target string) []string {
	seen := map[string]bool{}
	result := []string{}

	for _, pluginPath := range configuredPluginPaths(target) {
		dir := filepath.Dir(pluginPath)
		if dir == "." || dir == "" {
			continue
		}

		dir = filepath.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			result = append(result, dir)
		}
	}

	return result
}

func addPluginConfigPaths(baseDir string, config TinyProjectConfig, target string, add func(string)) {
	for _, plugin := range config.Plugins {
		if plugin.Path != "" && pluginPathAppliesToTarget(plugin.Path, target) {
			add(filepath.Join(baseDir, normalizePluginPathForTarget(plugin.Path, target)))
		}

		for _, file := range plugin.Files {
			if file != "" && pluginPathAppliesToTarget(file, target) {
				add(filepath.Join(baseDir, file))
			}
		}
	}
}
