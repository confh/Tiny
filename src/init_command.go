package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
)

func initCommand(args []string) {
	targetDir := "."

	if len(args) > 0 {
		targetDir = args[0]
	}

	targetDir = filepath.Clean(targetDir)

	err := os.MkdirAll(targetDir, 0755)
	if err != nil {
		LangError(ErrorRuntime, "failed to create project folder: %v", err)
	}

	projectName := filepath.Base(targetDir)

	if projectName == "." || projectName == string(filepath.Separator) || projectName == "" {
		wd, err := os.Getwd()
		if err != nil {
			LangError(ErrorRuntime, "failed to get current directory: %v", err)
		}

		projectName = filepath.Base(wd)
	}

	tinyJSONPath := filepath.Join(targetDir, "tiny.json")

	if fileExists(tinyJSONPath) {
		LangError(ErrorRuntime, "tiny.json already exists")
	}

	mkdir(filepath.Join(targetDir, "src"))
	mkdir(filepath.Join(targetDir, "plugins"))
	mkdir(filepath.Join(targetDir, "dist"))

	config := defaultTinyConfig(projectName)

	writeJSONFile(tinyJSONPath, config)

	writeTextFile(
		filepath.Join(targetDir, "src", "main.tiny"),
		`import std "io";

io.println("Hello from Tiny!");
`,
	)

	writeTextFile(
		filepath.Join(targetDir, "README.md"),
		fmt.Sprintf(`# %s

A Tiny project.

## Commands

`+"```bash"+`
tiny run
tiny build
tiny pack
tiny dist
`+"```"+`
`, projectName),
	)

	writeTextFile(
		filepath.Join(targetDir, ".gitignore"),
		`dist/
*.tbc
`,
	)

	fmt.Println("Created Tiny project:", displayPath(targetDir))
	fmt.Println("")
	fmt.Println("Next:")
	fmt.Println("  cd " + displayPath(targetDir))
	fmt.Println("  tiny")
}

func mkdir(path string) {
	err := os.MkdirAll(path, 0755)
	if err != nil {
		LangError(ErrorRuntime, "failed to create folder %s: %v", path, err)
	}
}

func writeTextFile(path string, content string) {
	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write %s: %v", path, err)
	}
}

func displayPath(path string) string {
	if path == "." {
		return "."
	}

	return strings.ReplaceAll(path, "\\", "/")
}
