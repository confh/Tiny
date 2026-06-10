package main

import (
	"os"
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type Loader struct {
	states       map[string]ImportState
	stack        []string
	cache        map[string][]Stmt
	dependencies map[string]TinyDependencyConfig
}

func (l *Loader) loadFile(path string) []Stmt {
	absPath, err := filepath.Abs(path)
	if err != nil {
		LangError(ErrorImport, "%v", err)
	}

	absPath = filepath.Clean(absPath)

	state := l.states[absPath]

	if state == ImportLoading {
		cycle := l.formatImportCycle(absPath)
		LangError(ErrorImport, "circular import detected: %s", cycle)
	}

	if state == ImportLoaded {
		return l.cache[absPath]
	}

	l.states[absPath] = ImportLoading
	l.stack = append(l.stack, absPath)

	bytes, err := os.ReadFile(absPath)
	if err != nil {
		LangError(ErrorImport, "failed to read file %s: %v", path, err)
	}

	lexer := NewLexer(string(bytes), absPath)
	parser := NewParser(lexer)
	program := parser.ParseProgram()

	var result []Stmt
	dir := filepath.Dir(absPath)

	for _, stmt := range program.Statements {
		switch s := stmt.(type) {
		case ImportStmt:
			if s.Std {
				result = append(result, s)
				continue
			}

			if s.Library {
				importPath := resolveLibraryImportPath(s.Path, absPath)
				importedStatements := l.loadFile(importPath)

				alias := s.Alias
				if alias == "" {
					alias = defaultLibraryAlias(s.Path)
				}

				result = append(result, NamespaceStmt{
					Name:       alias,
					Statements: importedStatements,
				})
				continue
			}

			if s.Plugin {
				if !filepath.IsAbs(s.Path) {
					s.Path = filepath.Clean(filepath.Join(dir, s.Path))
				}

				result = append(result, s)
				continue
			}

			importPath := l.resolveSourceImportPath(dir, s.Path)
			importedStatements := l.loadFile(importPath)

			if s.Alias != "" {
				result = append(result, NamespaceStmt{
					Name:       s.Alias,
					Statements: importedStatements,
				})
				continue
			}

			result = append(result, importedStatements...)
		default:
			result = append(result, stmt)
		}
	}

	l.stack = l.stack[:len(l.stack)-1]
	l.states[absPath] = ImportLoaded
	l.cache[absPath] = result

	return result
}

func (l *Loader) resolveSourceImportPath(baseDir string, importPath string) string {
	if filepath.IsAbs(importPath) {
		return importPath
	}

	localPath := filepath.Clean(filepath.Join(baseDir, importPath))
	if fileExists(localPath) {
		return localPath
	}

	parts := strings.FieldsFunc(importPath, func(r rune) bool {
		return r == '/' || r == '\\'
	})

	if len(parts) == 0 {
		return localPath
	}

	dep, exists := l.dependencies[parts[0]]
	if !exists {
		return localPath
	}

	root := filepath.Join(".tinydeps", parts[0])
	if dep.Path != "" {
		root = dep.Path
	} else if dep.Source != "" {
		spec := parseGitHubPackageSource(dep.Source)
		version := dep.Version
		if version == "" {
			version = spec.Ref
		}
		if version == "" {
			if lock, ok := loadTinyLock(); ok {
				if locked, exists := lock.Dependencies[parts[0]]; exists && lockedDependencyMatchesConfig(locked, dep) {
					version = locked.Version
				}
			}
		}
		root = libraryGlobalRoot(spec.Owner, spec.Repo, version)
	}

	if len(parts) == 1 {
		depConfig, ok := loadTinyConfigFrom(filepath.Join(root, "tiny.json"))
		if ok && depConfig.Entry != "" {
			return filepath.Clean(filepath.Join(root, depConfig.Entry))
		}
	}

	rest := filepath.Join(parts[1:]...)
	return filepath.Clean(filepath.Join(root, rest))
}

func defaultLibraryAlias(path string) string {
	lib, ok := parseLibraryImportPath(path)
	if !ok {
		return filepath.Base(path)
	}

	return lib.Repo
}

func (l *Loader) formatImportCycle(repeatedPath string) string {
	parts := []string{}

	start := 0

	for i, path := range l.stack {
		if path == repeatedPath {
			start = i
			break
		}
	}

	for _, path := range l.stack[start:] {
		parts = append(parts, filepath.Base(path))
	}

	parts = append(parts, filepath.Base(repeatedPath))

	return strings.Join(parts, " -> ")
}

func LoadProgram(path string) Program {
	config, _ := loadTinyConfig()

	loader := &Loader{
		states:       map[string]ImportState{},
		stack:        []string{},
		cache:        map[string][]Stmt{},
		dependencies: config.Dependencies,
	}

	statements := loader.loadFile(path)

	return Program{Statements: statements}
}
