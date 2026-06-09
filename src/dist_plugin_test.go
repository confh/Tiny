package main

import (
	"os"
	"path/filepath"
	"testing"

	. "language.com/src/vm"
)

func TestCopyPluginToDistUsesFileNameForAbsolutePath(t *testing.T) {
	tempDir := t.TempDir()
	source := filepath.Join(tempDir, "test.dll")
	distDir := filepath.Join(tempDir, "dist")

	if err := os.WriteFile(source, []byte("plugin bytes"), 0644); err != nil {
		t.Fatalf("write source plugin: %v", err)
	}

	if err := copyPluginToDist(source, distDir); err != nil {
		t.Fatalf("copy plugin: %v", err)
	}

	copied := filepath.Join(distDir, "test.dll")
	bytes, err := os.ReadFile(copied)
	if err != nil {
		t.Fatalf("read copied plugin: %v", err)
	}

	if string(bytes) != "plugin bytes" {
		t.Fatalf("copied plugin content mismatch: %q", string(bytes))
	}
}

func TestRewritePluginPathsForDistUsesBundledFileName(t *testing.T) {
	program := Program{
		Statements: []Stmt{
			ImportStmt{
				Path:   filepath.Join("plugins", "native"),
				Plugin: true,
				Alias:  "Native",
			},
			VariableStmt{
				Name: "dynamic",
				Value: MemberCallExpr{
					Object: IdentExpr{Name: "Plugin"},
					Method: "load",
					Args: []Expr{
						StringExpr{Value: filepath.Join("plugins", "dynamic")},
					},
				},
			},
		},
	}

	rewritten := rewritePluginPathsForDist(program, "windows-amd64")

	importStmt, ok := rewritten.Statements[0].(ImportStmt)
	if !ok {
		t.Fatalf("statement 0 type = %T, want ImportStmt", rewritten.Statements[0])
	}
	if importStmt.Path != "native.dll" {
		t.Fatalf("plugin import path = %q, want native.dll", importStmt.Path)
	}

	varStmt, ok := rewritten.Statements[1].(VariableStmt)
	if !ok {
		t.Fatalf("statement 1 type = %T, want VariableStmt", rewritten.Statements[1])
	}
	call, ok := varStmt.Value.(MemberCallExpr)
	if !ok {
		t.Fatalf("variable value type = %T, want MemberCallExpr", varStmt.Value)
	}
	arg, ok := call.Args[0].(StringExpr)
	if !ok {
		t.Fatalf("Plugin.load arg type = %T, want StringExpr", call.Args[0])
	}
	if arg.Value != "dynamic.dll" {
		t.Fatalf("Plugin.load path = %q, want dynamic.dll", arg.Value)
	}
}

func TestBundledPluginPathsPreferInstalledDependencyPlugin(t *testing.T) {
	tempDir := t.TempDir()
	withWorkingDir(t, tempDir)

	tinyHome := filepath.Join(tempDir, "tiny-home")
	t.Setenv("TINY_HOME", tinyHome)

	depRoot := libraryGlobalRoot("owner", "dep", "v1")
	depPlugin := filepath.Join(depRoot, "native", "dep.dll")
	if err := os.MkdirAll(filepath.Dir(depPlugin), 0755); err != nil {
		t.Fatalf("create dependency plugin dir: %v", err)
	}
	if err := os.WriteFile(depPlugin, []byte("dependency plugin"), 0644); err != nil {
		t.Fatalf("write dependency plugin: %v", err)
	}

	writeConfigForTest(t, filepath.Join(depRoot, "tiny.json"), TinyProjectConfig{
		Entry: "main.tiny",
		Plugins: []TinyProjectPluginConfig{
			{Name: "Dep", Path: filepath.Join("native", "dep")},
		},
	})
	writeConfigForTest(t, filepath.Join(tempDir, "tiny.json"), TinyProjectConfig{
		Entry: "src/main.tiny",
		Dependencies: map[string]TinyDependencyConfig{
			"dep": {
				Source:  "github:owner/dep",
				Version: "v1",
			},
		},
	})

	program := Program{
		Statements: []Stmt{
			NamespaceStmt{
				Name: "Dep",
				Statements: []Stmt{
					VariableStmt{
						Name: "dynamic",
						Value: MemberCallExpr{
							Object: IdentExpr{Name: "Plugin"},
							Method: "load",
							Args: []Expr{
								StringExpr{Value: filepath.Join("native", "dep")},
							},
						},
					},
				},
			},
		},
	}

	paths := bundledPluginPaths(program, "windows-amd64", nil)
	if len(paths) != 1 {
		t.Fatalf("bundled plugin paths = %#v, want one path", paths)
	}
	if filepath.Clean(paths[0]) != filepath.Clean(depPlugin) {
		t.Fatalf("bundled plugin path = %q, want %q", paths[0], depPlugin)
	}
}
