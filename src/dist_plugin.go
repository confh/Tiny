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

		case ImportStmt:
			if s.Plugin {
				add(s.Path)
			}
		}
	}

	for _, stmt := range program.Statements {
		scanStmt(stmt)
	}

	return result
}

func rewritePluginPathsForDist(program Program, target string) Program {
	statements := make([]Stmt, len(program.Statements))
	for i, stmt := range program.Statements {
		statements[i] = rewritePluginStmtForDist(stmt, target)
	}

	return Program{Statements: statements}
}

func rewritePluginStmtForDist(stmt Stmt, target string) Stmt {
	switch s := stmt.(type) {
	case ImportStmt:
		if s.Plugin {
			s.Path = pluginDistFileName(normalizePluginPathForTarget(s.Path, target))
		}
		return s

	case ExportStmt:
		s.Inner = rewritePluginStmtForDist(s.Inner, target)
		return s

	case VariableStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case ExprStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case ReturnStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case AssignStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case PropertyAssignStmt:
		s.Object = rewritePluginExprForDist(s.Object, target)
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case IndexAssignStmt:
		s.Object = rewritePluginExprForDist(s.Object, target)
		s.Index = rewritePluginExprForDist(s.Index, target)
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case ThrowStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		return s

	case IfStmt:
		s.Condition = rewritePluginExprForDist(s.Condition, target)
		s.ThenBody = rewritePluginStmtListForDist(s.ThenBody, target)
		s.ElseBody = rewritePluginStmtListForDist(s.ElseBody, target)
		return s

	case WhileStmt:
		s.Condition = rewritePluginExprForDist(s.Condition, target)
		s.Body = rewritePluginStmtListForDist(s.Body, target)
		return s

	case ForStmt:
		if s.Init != nil {
			s.Init = rewritePluginStmtForDist(s.Init, target)
		}
		if s.Condition != nil {
			s.Condition = rewritePluginExprForDist(s.Condition, target)
		}
		if s.Update != nil {
			s.Update = rewritePluginStmtForDist(s.Update, target)
		}
		s.Body = rewritePluginStmtListForDist(s.Body, target)
		return s

	case ForInStmt:
		s.Iterable = rewritePluginExprForDist(s.Iterable, target)
		s.Body = rewritePluginStmtListForDist(s.Body, target)
		return s

	case MatchStmt:
		s.Value = rewritePluginExprForDist(s.Value, target)
		for i := range s.Cases {
			s.Cases[i].Value = rewritePluginExprForDist(s.Cases[i].Value, target)
			s.Cases[i].Body = rewritePluginStmtListForDist(s.Cases[i].Body, target)
		}
		s.Default = rewritePluginStmtListForDist(s.Default, target)
		return s

	case FunctionStmt:
		s.Body = rewritePluginStmtListForDist(s.Body, target)
		return s

	case ClassStmt:
		for i := range s.Fields {
			s.Fields[i].Value = rewritePluginExprForDist(s.Fields[i].Value, target)
		}
		for i := range s.Methods {
			s.Methods[i].Body = rewritePluginStmtListForDist(s.Methods[i].Body, target)
		}
		return s

	case TryCatchStmt:
		s.TryBody = rewritePluginStmtListForDist(s.TryBody, target)
		s.CatchBody = rewritePluginStmtListForDist(s.CatchBody, target)
		s.FinallyBody = rewritePluginStmtListForDist(s.FinallyBody, target)
		return s

	case NamespaceStmt:
		s.Statements = rewritePluginStmtListForDist(s.Statements, target)
		return s
	}

	return stmt
}

func rewritePluginStmtListForDist(statements []Stmt, target string) []Stmt {
	result := make([]Stmt, len(statements))
	for i, stmt := range statements {
		result[i] = rewritePluginStmtForDist(stmt, target)
	}
	return result
}

func rewritePluginExprForDist(expr Expr, target string) Expr {
	switch e := expr.(type) {
	case MemberCallExpr:
		if ident, ok := e.Object.(IdentExpr); ok && ident.Name == "Plugin" && e.Method == "load" && len(e.Args) == 1 {
			if str, ok := e.Args[0].(StringExpr); ok {
				e.Args[0] = StringExpr{Value: pluginDistFileName(normalizePluginPathForTarget(str.Value, target))}
				return e
			}
		}

		e.Object = rewritePluginExprForDist(e.Object, target)
		for i, arg := range e.Args {
			e.Args[i] = rewritePluginExprForDist(arg, target)
		}
		return e

	case CallValueExpr:
		e.Callee = rewritePluginExprForDist(e.Callee, target)
		for i, arg := range e.Args {
			e.Args[i] = rewritePluginExprForDist(arg, target)
		}
		return e

	case CallExpr:
		for i, arg := range e.Args {
			e.Args[i] = rewritePluginExprForDist(arg, target)
		}
		return e

	case BinaryExpr:
		e.Left = rewritePluginExprForDist(e.Left, target)
		e.Right = rewritePluginExprForDist(e.Right, target)
		return e

	case UnaryExpr:
		e.Right = rewritePluginExprForDist(e.Right, target)
		return e

	case TernaryExpr:
		e.Condition = rewritePluginExprForDist(e.Condition, target)
		e.ThenExpr = rewritePluginExprForDist(e.ThenExpr, target)
		e.ElseExpr = rewritePluginExprForDist(e.ElseExpr, target)
		return e

	case PropertyExpr:
		e.Object = rewritePluginExprForDist(e.Object, target)
		return e

	case IndexExpr:
		e.Object = rewritePluginExprForDist(e.Object, target)
		e.Index = rewritePluginExprForDist(e.Index, target)
		return e

	case ObjectInExpr:
		e.Key = rewritePluginExprForDist(e.Key, target)
		e.Object = rewritePluginExprForDist(e.Object, target)
		return e

	case ArrayExpr:
		for i, item := range e.Elements {
			e.Elements[i] = rewritePluginExprForDist(item, target)
		}
		return e

	case ObjectExpr:
		for i := range e.Fields {
			e.Fields[i].Value = rewritePluginExprForDist(e.Fields[i].Value, target)
		}
		return e

	case FunctionExpr:
		e.Body = rewritePluginStmtListForDist(e.Body, target)
		return e

	case InterpolatedStringExpr:
		for i := range e.Parts {
			if e.Parts[i].Expr != nil {
				e.Parts[i].Expr = rewritePluginExprForDist(e.Parts[i].Expr, target)
			}
		}
		return e

	case TypeOfExpr:
		e.Value = rewritePluginExprForDist(e.Value, target)
		return e

	case SpawnExpr:
		e.Function = rewritePluginExprForDist(e.Function, target)
		return e

	case InstanceOfExpr:
		e.Object = rewritePluginExprForDist(e.Object, target)
		e.Class = rewritePluginExprForDist(e.Class, target)
		return e
	}

	return expr
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
