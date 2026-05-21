package main

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var tinyFileImportRegex = regexp.MustCompile(`import\s+"([^"]+)"(?:\s+as\s+[A-Za-z_][A-Za-z0-9_]*)?\s*;?`)

func collectImportedSourceText(entryPath string) (string, error) {
	visited := map[string]bool{}

	var builder strings.Builder

	err := collectImportedSourceTextInto(entryPath, visited, &builder)
	if err != nil {
		return "", err
	}

	return builder.String(), nil
}

func collectImportedSourceTextInto(path string, visited map[string]bool, builder *strings.Builder) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}

	if visited[abs] {
		return nil
	}

	visited[abs] = true

	bytes, err := os.ReadFile(abs)
	if err != nil {
		return err
	}

	text := string(bytes)

	builder.WriteString("\n--- FILE: ")
	builder.WriteString(abs)
	builder.WriteString(" ---\n")
	builder.WriteString(text)
	builder.WriteString("\n")

	imports := tinyFileImportRegex.FindAllStringSubmatch(text, -1)

	dir := filepath.Dir(abs)

	for _, match := range imports {
		importPath := match[1]

		resolved := importPath
		if !filepath.IsAbs(importPath) {
			resolved = filepath.Join(dir, importPath)
		}

		err := collectImportedSourceTextInto(resolved, visited, builder)
		if err != nil {
			return err
		}
	}

	return nil
}

func hashTinyProject(entryPath string, sourceText string) (string, error) {
	importedText, err := collectImportedSourceText(entryPath)
	if err != nil {
		return "", err
	}

	input := strings.Join([]string{
		"TinyVersion=" + TinyVersion,
		"BytecodeCacheVersion=1",
		"EntryPath=" + entryPath,
		"SourceText=" + sourceText,
		"ImportedText=" + importedText,
	}, "\n")

	sum := sha256.Sum256([]byte(input))
	return hex.EncodeToString(sum[:]), nil
}

func tinyCachePath(entryPath string, hash string) (string, error) {
	abs, err := filepath.Abs(entryPath)
	if err != nil {
		return "", err
	}

	dir := filepath.Dir(abs)
	base := strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))

	cacheDir := filepath.Join(dir, ".tinycache")

	err = os.MkdirAll(cacheDir, 0755)
	if err != nil {
		return "", err
	}

	shortHash := hash
	if len(shortHash) > 12 {
		shortHash = shortHash[:12]
	}

	return filepath.Join(cacheDir, base+"_"+shortHash+".tbc"), nil
}
