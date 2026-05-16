package main

import (
	"path/filepath"
	"runtime"

	. "language.com/src/vm"
)

func collectPluginPathsFromProgram(program Program, target string) []string {
	seen := map[string]bool{}
	var result []string

	add := func(path string) {
		if path == "" {
			return
		}

		path = normalizePluginPathForTarget(path, target)

		if !seen[path] {
			seen[path] = true
			result = append(result, path)
		}
	}

	var scanExpr func(expr Expr)
	var scanStmt func(stmt Stmt)

	scanExpr = func(expr Expr) {
		switch e := expr.(type) {
		case MemberCallExpr:
			// Plugin.load("path")
			if ident, ok := e.Object.(IdentExpr); ok && ident.Name == "Plugin" && e.Method == "load" {
				if len(e.Args) == 1 {
					if str, ok := e.Args[0].(StringExpr); ok {
						add(str.Value)
					}
				}
			}

			scanExpr(e.Object)

			for _, arg := range e.Args {
				scanExpr(arg)
			}

		case CallValueExpr:
			scanExpr(e.Callee)

			for _, arg := range e.Args {
				scanExpr(arg)
			}

		case BinaryExpr:
			scanExpr(e.Left)
			scanExpr(e.Right)

		case UnaryExpr:
			scanExpr(e.Right)

		case PropertyExpr:
			scanExpr(e.Object)

		case IndexExpr:
			scanExpr(e.Object)
			scanExpr(e.Index)

		case ArrayExpr:
			for _, item := range e.Elements {
				scanExpr(item)
			}

		case ObjectExpr:
			for _, item := range e.Fields {
				scanExpr(item.Value)
			}

		case FunctionExpr:
			for _, bodyStmt := range e.Body {
				scanStmt(bodyStmt)
			}

		case InterpolatedStringExpr:
			for _, part := range e.Parts {
				if part.Expr != nil {
					scanExpr(part.Expr)
				}
			}
		}
	}

	scanStmt = func(stmt Stmt) {
		switch s := stmt.(type) {
		case VariableStmt:
			scanExpr(s.Value)

		case ExprStmt:
			scanExpr(s.Value)

		case ReturnStmt:
			scanExpr(s.Value)

		case AssignStmt:
			scanExpr(s.Value)

		case IfStmt:
			scanExpr(s.Condition)

			for _, bodyStmt := range s.ThenBody {
				scanStmt(bodyStmt)
			}

			for _, bodyStmt := range s.ElseBody {
				scanStmt(bodyStmt)
			}

		case WhileStmt:
			scanExpr(s.Condition)

			for _, bodyStmt := range s.Body {
				scanStmt(bodyStmt)
			}

		case ForStmt:
			if s.Init != nil {
				scanStmt(s.Init)
			}

			if s.Condition != nil {
				scanExpr(s.Condition)
			}

			if s.Update != nil {
				scanStmt(s.Update)
			}

			for _, bodyStmt := range s.Body {
				scanStmt(bodyStmt)
			}

		case FunctionStmt:
			for _, bodyStmt := range s.Body {
				scanStmt(bodyStmt)
			}

		case ClassStmt:
			for _, method := range s.Methods {
				for _, bodyStmt := range method.Body {
					scanStmt(bodyStmt)
				}
			}

		case TryCatchStmt:
			for _, bodyStmt := range s.TryBody {
				scanStmt(bodyStmt)
			}

			for _, bodyStmt := range s.CatchBody {
				scanStmt(bodyStmt)
			}

		case NamespaceStmt:
			for _, bodyStmt := range s.Statements {
				scanStmt(bodyStmt)
			}
		}
	}

	for _, stmt := range program.Statements {
		scanStmt(stmt)
	}

	return result
}

func normalizePluginPath(path string) string {
	ext := filepath.Ext(path)

	if ext != "" {
		return path
	}

	switch runtime.GOOS {
	case "windows":
		return path + ".dll"
	case "linux":
		return path + ".so"
	case "darwin":
		return path + ".dylib"
	default:
		return path
	}
}
