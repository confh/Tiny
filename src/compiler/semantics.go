package compiler

import (
	"path/filepath"
	"strings"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type semanticScope struct {
	parent *semanticScope
	names  map[string]bool
}

func newSemanticScope(parent *semanticScope) *semanticScope {
	return &semanticScope{parent: parent, names: map[string]bool{}}
}

func (s *semanticScope) define(name string) {
	if name != "" {
		s.names[name] = true
	}
}

func (s *semanticScope) has(name string) bool {
	for cur := s; cur != nil; cur = cur.parent {
		if cur.names[name] {
			return true
		}
	}
	return false
}

type semanticNamespace struct {
	exported map[string]bool
	all      map[string]bool
}

type compilerSemanticAnalyzer struct {
	file       string
	scope      *semanticScope
	functions  map[string]bool
	classes    map[string]bool
	enums      map[string]bool
	namespaces map[string]semanticNamespace
}

func abortOnCompilerSemanticErrors(file string, statements []Stmt) {
	a := &compilerSemanticAnalyzer{
		file:       file,
		scope:      newSemanticScope(nil),
		functions:  map[string]bool{},
		classes:    map[string]bool{},
		enums:      map[string]bool{},
		namespaces: map[string]semanticNamespace{},
	}
	a.seedBuiltins()
	a.predeclare(statements, "")
	a.visitStatements(statements)
}

func (a *compilerSemanticAnalyzer) seedBuiltins() {
	for _, name := range []string{
		"Plugin", "true", "false", "null",
	} {
		a.scope.define(name)
	}
}

func (a *compilerSemanticAnalyzer) predeclare(statements []Stmt, namespace string) semanticNamespace {
	ns := semanticNamespace{exported: map[string]bool{}, all: map[string]bool{}}
	hasExplicitExports := false
	for _, stmt := range statements {
		if _, ok := stmt.(ExportStmt); ok {
			hasExplicitExports = true
			break
		}
	}

	for _, raw := range statements {
		stmt, exported := unwrapExport(raw)
		public := !hasExplicitExports || exported
		name := semanticDeclaredName(stmt)
		if name != "" {
			ns.all[name] = true
			if public {
				ns.exported[name] = true
			}
			if namespace == "" {
				a.scope.define(name)
			}
			fullName := name
			if namespace != "" {
				fullName = namespace + "." + name
			}
			switch stmt.(type) {
			case FunctionStmt, ExternalFnStmt, NativeFnStmt:
				a.functions[fullName] = true
			case ClassStmt:
				a.classes[fullName] = true
			case EnumStmt:
				a.enums[fullName] = true
			}
		}

		if destructureStmt, ok := stmt.(DestructureStmt); ok {
			for _, destructuredName := range collectDestructuredNames(destructureStmt.Target) {
				ns.all[destructuredName] = true
				if public {
					ns.exported[destructuredName] = true
				}
				if namespace == "" {
					a.scope.define(destructuredName)
				}
			}
		}

		if importStmt, ok := stmt.(ImportStmt); ok && namespace == "" {
			alias := importStmt.Alias
			if alias == "" {
				if importStmt.Std {
					alias = importStmt.Path
				} else {
					alias = strings.TrimSuffix(filepath.Base(importStmt.Path), filepath.Ext(importStmt.Path))
				}
			}
			a.scope.define(alias)
		}

		if nested, ok := stmt.(NamespaceStmt); ok {
			nestedFullName := nested.Name
			if namespace != "" {
				nestedFullName = namespace + "." + nested.Name
			}
			a.namespaces[nestedFullName] = a.predeclare(nested.Statements, nestedFullName)
			ns.all[nested.Name] = true
			if public {
				ns.exported[nested.Name] = true
			}
			if namespace == "" {
				a.scope.define(nested.Name)
			}
		}
	}

	return ns
}

func semanticDeclaredName(stmt Stmt) string {
	switch s := stmt.(type) {
	case VariableStmt:
		return s.Name
	case FunctionStmt:
		return s.Name
	case ClassStmt:
		return s.Name
	case InterfaceStmt:
		return s.Name
	case EnumStmt:
		return s.Name
	case NativeFnStmt:
		return s.Name
	case ExternalFnStmt:
		return s.Name
	case ExternalGlobalStmt:
		return s.Name
	case EmbedStmt:
		return s.Name
	case NamespaceStmt:
		return s.Name
	default:
		return ""
	}
}

func (a *compilerSemanticAnalyzer) pushScope() {
	a.scope = newSemanticScope(a.scope)
}

func (a *compilerSemanticAnalyzer) popScope() {
	if a.scope.parent != nil {
		a.scope = a.scope.parent
	}
}

func (a *compilerSemanticAnalyzer) visitStatements(statements []Stmt) {
	for _, raw := range statements {
		stmt, _ := unwrapExport(raw)
		a.visitStatement(stmt)
	}
}

func (a *compilerSemanticAnalyzer) visitStatement(stmt Stmt) {
	switch s := stmt.(type) {
	case ImportStmt, InterfaceStmt, ExternalFnStmt, ExternalGlobalStmt:
		return
	case VariableStmt:
		if s.Value != nil {
			a.visitExpr(s.Value)
		}
		a.scope.define(s.Name)
	case DestructureStmt:
		a.visitExpr(s.Value)
		for _, name := range collectDestructuredNames(s.Target) {
			a.scope.define(name)
		}
	case FunctionStmt:
		a.scope.define(s.Name)
		a.visitFunctionBody(s.Params, s.Body)
	case ClassStmt:
		a.scope.define(s.Name)
		for _, field := range s.Fields {
			if field.Value != nil {
				a.visitExpr(field.Value)
			}
		}
		for _, method := range s.Methods {
			a.visitFunctionBody(method.Params, method.Body)
		}
	case NamespaceStmt:
		a.scope.define(s.Name)
	case ExprStmt:
		a.visitExpr(s.Value)
	case ReturnStmt:
		if s.HasValue {
			a.visitExpr(s.Value)
		}
	case IfStmt:
		a.visitExpr(s.Condition)
		a.pushScope()
		a.visitStatements(s.ThenBody)
		a.popScope()
		a.pushScope()
		a.visitStatements(s.ElseBody)
		a.popScope()
	case WhileStmt:
		a.visitExpr(s.Condition)
		a.pushScope()
		a.visitStatements(s.Body)
		a.popScope()
	case ForStmt:
		a.pushScope()
		if s.Init != nil {
			a.visitStatement(s.Init)
		}
		if s.Condition != nil {
			a.visitExpr(s.Condition)
		}
		if s.Update != nil {
			a.visitStatement(s.Update)
		}
		a.visitStatements(s.Body)
		a.popScope()
	case ForInStmt:
		a.visitExpr(s.Iterable)
		a.pushScope()
		a.scope.define(s.ItemName)
		a.scope.define(s.IndexName)
		a.visitStatements(s.Body)
		a.popScope()
	case TryCatchStmt:
		a.pushScope()
		a.visitStatements(s.TryBody)
		a.popScope()
		a.pushScope()
		if s.ErrorName != "" {
			a.scope.define(s.ErrorName)
		}
		a.visitStatements(s.CatchBody)
		a.popScope()
		a.pushScope()
		a.visitStatements(s.FinallyBody)
		a.popScope()
	case ThrowStmt:
		a.visitExpr(s.Value)
	case LockStmt:
		a.visitExpr(s.Mutex)
		a.visitStatements(s.Block)
	}
}

func (a *compilerSemanticAnalyzer) visitFunctionBody(params []Param, body []Stmt) {
	a.pushScope()
	a.scope.define("this")
	for _, param := range params {
		a.scope.define(param.Name)
	}
	a.visitStatements(body)
	a.popScope()
}

func (a *compilerSemanticAnalyzer) visitExpr(expr Expr) {
	switch e := expr.(type) {
	case IdentExpr:
		if !a.scope.has(e.Name) && !a.functions[e.Name] && !a.classes[e.Name] && !a.enums[e.Name] {
			LangErrorAt(ErrorName, e.File, e.Line, e.Column, "undefined variable: %s", e.Name)
		}
	case BinaryExpr:
		a.visitExpr(e.Left)
		a.visitExpr(e.Right)
	case UnaryExpr:
		a.visitExpr(e.Right)
	case CallExpr:
		if !a.scope.has(e.Name) && !a.functions[e.Name] && !a.classes[e.Name] {
			LangErrorAt(ErrorName, e.File, e.Line, e.Column, "undefined variable: %s", e.Name)
		}
		for _, arg := range e.Args {
			a.visitExpr(arg)
		}
	case CallValueExpr:
		a.visitExpr(e.Callee)
		for _, arg := range e.Args {
			a.visitExpr(arg)
		}
	case MemberCallExpr:
		a.checkMemberExport(e.Object, e.Method, e.File, e.Line, e.Column)
		a.visitExpr(e.Object)
		for _, arg := range e.Args {
			a.visitExpr(arg)
		}
	case PropertyExpr:
		a.checkMemberExport(e.Object, e.Name, e.File, e.Line, e.Column)
		a.visitExpr(e.Object)
	case IndexExpr:
		a.visitExpr(e.Object)
		a.visitExpr(e.Index)
	case ArrayExpr:
		for _, elem := range e.Elements {
			a.visitExpr(elem)
		}
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy {
				a.visitExpr(field.Copy)
			}
			if field.Value != nil {
				a.visitExpr(field.Value)
			}
		}
	case TernaryExpr:
		a.visitExpr(e.Condition)
		a.visitExpr(e.ThenExpr)
		a.visitExpr(e.ElseExpr)
	case NullishCoalescingExpr:
		a.visitExpr(e.Left)
		a.visitExpr(e.Right)
	case FunctionExpr:
		a.visitFunctionBody(e.Params, e.Body)
	case InterpolatedStringExpr:
		for _, part := range e.Parts {
			if part.IsExpr {
				a.visitExpr(part.Expr)
			}
		}
	case AwaitExpr:
		a.visitExpr(e.Task)
	case SpawnExpr:
		for _, arg := range e.Args {
			a.visitExpr(arg)
		}
		a.visitExpr(e.Function)
	case SpreadExpr:
		a.visitExpr(e.Value)
	}
}

func (a *compilerSemanticAnalyzer) checkMemberExport(object Expr, member string, file string, line int, column int) {
	ident, ok := object.(IdentExpr)
	if !ok {
		return
	}
	ns, ok := a.namespaces[ident.Name]
	if !ok {
		return
	}
	if ns.all[member] && !ns.exported[member] {
		LangErrorAt(ErrorName, file, line, column, "undefined export: %s.%s", ident.Name, member)
	}
}
