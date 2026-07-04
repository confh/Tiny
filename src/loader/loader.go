package loader

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type Loader struct {
	states           map[string]ImportState
	stack            []string
	cache            map[string][]Stmt
	dependencies     map[string]TinyDependencyConfig
	files            map[string]bool
	namespaceAliases map[string]string
}

func (l *Loader) loadFile(path string) []Stmt {
	absPath, err := filepath.Abs(path)
	if err != nil {
		LangError(ErrorImport, "%v", err)
	}

	absPath = filepath.Clean(absPath)
	if l.files != nil {
		l.files[absPath] = true
	}

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
			if s.TypeOnly {
				if s.Alias == "" {
					LangErrorAt(ErrorImport, s.File, s.Line, s.Column, "import type requires an alias")
				}
				absImportPath, err := filepath.Abs(importPath)
				if err != nil {
					LangError(ErrorImport, "%v", err)
				}
				absImportPath = filepath.Clean(absImportPath)
				s.Path = absImportPath
				s.TypeNamespace = l.namespaceAliases[absImportPath]
				if s.TypeNamespace == "" {
					s.TypeNamespace = s.Alias
				}
				result = append(result, s)
				continue
			}

			absImportPath, err := filepath.Abs(importPath)
			if err == nil && s.Alias != "" {
				l.namespaceAliases[filepath.Clean(absImportPath)] = s.Alias
			}
			importedStatements := l.loadFile(importPath)

			if s.Alias != "" {
				result = append(result, NamespaceStmt{
					Name:       s.Alias,
					Statements: importedStatements,
				})
				continue
			}

			result = append(result, exportedStatementsForImport(importedStatements)...)
		default:
			result = append(result, stmt)
		}
	}

	l.stack = l.stack[:len(l.stack)-1]
	l.states[absPath] = ImportLoaded
	l.cache[absPath] = result

	return result
}

func exportedStatementsForImport(statements []Stmt) []Stmt {
	result := []Stmt{}
	for _, stmt := range statements {
		if _, exported := unwrapExport(stmt); exported {
			result = append(result, stmt)
		}
	}
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
	program, _ := LoadProgramWithFiles(path)
	return program
}

func LoadProgramWithFiles(path string) (Program, []string) {
	config, _ := loadTinyConfig()

	loader := &Loader{
		states:           map[string]ImportState{},
		stack:            []string{},
		cache:            map[string][]Stmt{},
		dependencies:     config.Dependencies,
		files:            map[string]bool{},
		namespaceAliases: map[string]string{},
	}

	statements := loader.loadFile(path)

	files := make([]string, 0, len(loader.files))
	for file := range loader.files {
		files = append(files, file)
	}
	sort.Strings(files)

	return Program{Statements: statements}, files
}
