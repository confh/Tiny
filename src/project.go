package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"

	. "language.com/src/tinyerrors"
)

type TinyProjectConfig struct {
	Name            string                          `json:"name"`
	Version         string                          `json:"version"`
	Entry           string                          `json:"entry"`
	OutDir          string                          `json:"outDir"`
	Target          string                          `json:"target"`
	Scripts         map[string]string               `json:"scripts"`
	Dependencies    map[string]TinyDependencyConfig `json:"dependencies"`
	Ignore          []string                        `json:"ignore"`
	Plugins         []TinyProjectPluginConfig       `json:"plugins"`
	CompilerOptions TinyCompilerOptions             `json:"compilerOptions"`
}

type TinyDependencyConfig struct {
	Source  string `json:"source"`
	Version string `json:"version"`
	Path    string `json:"path"`
}

type TinyProjectPluginConfig struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

type TinyCompilerOptions struct {
	BytecodeCache bool `json:"bytecodeCache"`
	StackTraces   bool `json:"stackTraces,omitempty"`
	Strict        bool `json:"strict,omitempty"`
}

func defaultTinyConfig(projectName string) TinyProjectConfig {
	return TinyProjectConfig{
		Name:    projectName,
		Version: "0.1.0",
		Entry:   "src/main.tiny",
		OutDir:  "dist",
		Target:  defaultProjectTarget(),
		Scripts: map[string]string{
			"start": "tiny run",
			"build": "tiny build",
			"pack":  "tiny pack",
			"dist":  "tiny dist",
		},
		Dependencies: map[string]TinyDependencyConfig{},
		Ignore:       []string{},
		Plugins:      []TinyProjectPluginConfig{},
		CompilerOptions: TinyCompilerOptions{
			BytecodeCache: false,
		},
	}
}

func defaultProjectTarget() string {
	if isWindows() {
		return "windows-" + runtime.GOARCH
	}

	return "linux-" + runtime.GOARCH
}

func isWindows() bool {
	return filepath.Separator == '\\'
}

func writeJSONFile(path string, value any) {
	bytes, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		LangError(ErrorRuntime, "failed to encode %s: %v", path, err)
	}

	err = os.WriteFile(path, bytes, 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write %s: %v", path, err)
	}
}

func loadTinyConfig() (TinyProjectConfig, bool) {
	return loadTinyConfigFrom("tiny.json")
}

func loadTinyConfigFrom(path string) (TinyProjectConfig, bool) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return TinyProjectConfig{}, false
	}

	var config TinyProjectConfig

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		LangError(ErrorRuntime, "failed to parse %s: %v", path, err)
	}

	return config, true
}
