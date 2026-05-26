package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	. "language.com/src/tinyerrors"
)

type TinyProjectConfig struct {
	Name            string                    `json:"name"`
	Version         string                    `json:"version"`
	Entry           string                    `json:"entry"`
	OutDir          string                    `json:"outDir"`
	Target          string                    `json:"target"`
	Scripts         map[string]string         `json:"scripts"`
	Plugins         []TinyProjectPluginConfig `json:"plugins"`
	CompilerOptions TinyCompilerOptions       `json:"compilerOptions"`
}

type TinyProjectPluginConfig struct {
	Name  string   `json:"name"`
	Path  string   `json:"path"`
	Files []string `json:"files"`
}

type TinyCompilerOptions struct {
	StackTraces bool `json:"stackTraces"`
	Strict      bool `json:"strict"`
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
		Plugins: []TinyProjectPluginConfig{},
		CompilerOptions: TinyCompilerOptions{
			StackTraces: true,
			Strict:      false,
		},
	}
}

func defaultProjectTarget() string {
	if isWindows() {
		return "windows-amd64"
	}

	return "linux-amd64"
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
	path := "tiny.json"

	bytes, err := os.ReadFile(path)
	if err != nil {
		return TinyProjectConfig{}, false
	}

	var config TinyProjectConfig

	err = json.Unmarshal(bytes, &config)
	if err != nil {
		LangError(ErrorRuntime, "failed to parse tiny.json: %v", err)
	}

	return config, true
}
