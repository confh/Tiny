package main

import (
	"os"
	"sort"
	"strings"

	. "language.com/src/vm"
)

func findExprAtPosition(stmts []Stmt, line int, column int) (Expr, bool) {
	var best Expr
	bestScore := -1

	var walkExpr func(Expr)
	walkExpr = func(expr Expr) {
		if expr == nil {
			return
		}
		switch e := expr.(type) {
		case IdentExpr:
			score := exprScore(e.Line, e.Column, line, column)
			if score > bestScore {
				bestScore = score
				best = expr
			}
		case PropertyExpr:
			score := exprScore(e.Line, e.Column, line, column)
			if score > bestScore {
				bestScore = score
				best = expr
			}
			walkExpr(e.Object)
		case CallValueExpr:
			score := exprScore(e.Line, e.Column, line, column)
			if score > bestScore {
				bestScore = score
				best = expr
			}
			walkExpr(e.Callee)
			for _, arg := range e.Args {
				walkExpr(arg)
			}
		case CallExpr:
			score := exprScore(e.Line, e.Column, line, column)
			if score > bestScore {
				bestScore = score
				best = expr
			}
		case MemberCallExpr:
			score := exprScore(e.Line, e.Column, line, column)
			if score > bestScore {
				bestScore = score
				best = expr
			}
			walkExpr(e.Object)
			for _, arg := range e.Args {
				walkExpr(arg)
			}
		case ArrayExpr:
			for _, elem := range e.Elements {
				walkExpr(elem)
			}
		case ObjectExpr:
			for _, field := range e.Fields {
				walkExpr(field.Value)
			}
		case BinaryExpr:
			walkExpr(e.Left)
			walkExpr(e.Right)
		case UnaryExpr:
			walkExpr(e.Right)
		case TernaryExpr:
			walkExpr(e.Condition)
			walkExpr(e.ThenExpr)
			walkExpr(e.ElseExpr)
		case IndexExpr:
			walkExpr(e.Object)
			walkExpr(e.Index)
		case FunctionExpr:
		case StringExpr:
		case NumberExpr:
		case BoolExpr:
		case NullExpr:
		}
	}

	for _, stmt := range stmts {
		walkStmtForExpr(stmt, walkExpr)
	}

	return best, best != nil
}

func walkStmtForExpr(stmt Stmt, walkExpr func(Expr)) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case ExprStmt:
		walkExpr(s.Value)
	case VariableStmt:
		walkExpr(s.Value)
	case AssignStmt:
		walkExpr(s.Value)
	case ReturnStmt:
		walkExpr(s.Value)
	case IfStmt:
		walkExpr(s.Condition)
		walkStmtsForExpr(s.ThenBody, walkExpr)
		walkStmtsForExpr(s.ElseBody, walkExpr)
	case ForStmt:
		walkStmtsForExpr(s.Body, walkExpr)
	case ForInStmt:
		walkStmtsForExpr(s.Body, walkExpr)
	case WhileStmt:
		walkExpr(s.Condition)
		walkStmtsForExpr(s.Body, walkExpr)
	case FunctionStmt:
		walkStmtsForExpr(s.Body, walkExpr)
	case ClassStmt:
		for _, method := range s.Methods {
			walkStmtsForExpr(method.Body, walkExpr)
		}
	case TryCatchStmt:
		walkStmtsForExpr(s.TryBody, walkExpr)
		walkStmtsForExpr(s.CatchBody, walkExpr)
		walkStmtsForExpr(s.FinallyBody, walkExpr)
	case MatchStmt:
		for _, c := range s.Cases {
			walkStmtsForExpr(c.Body, walkExpr)
		}
	}
}

func walkStmtsForExpr(stmts []Stmt, walkExpr func(Expr)) {
	for _, stmt := range stmts {
		walkStmtForExpr(stmt, walkExpr)
	}
}

func exprScore(line, column, targetLine, targetColumn int) int {
	if line == targetLine && column == targetColumn {
		return 1000
	}
	if line == targetLine {
		return 500 - abs(column-targetColumn)
	}
	return 0
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func findIdentAtPosition(stmts []Stmt, line int, column int) (IdentExpr, bool) {
	expr, ok := findExprAtPosition(stmts, line, column)
	if !ok {
		return IdentExpr{}, false
	}
	ident, ok := expr.(IdentExpr)
	return ident, ok
}

func findPropertyExprAtPosition(stmts []Stmt, line int, column int) (PropertyExpr, bool) {
	expr, ok := findExprAtPosition(stmts, line, column)
	if !ok {
		return PropertyExpr{}, false
	}
	prop, ok := expr.(PropertyExpr)
	return prop, ok
}

func findMemberCallExprAtPosition(stmts []Stmt, line int, column int) (MemberCallExpr, bool) {
	expr, ok := findExprAtPosition(stmts, line, column)
	if !ok {
		return MemberCallExpr{}, false
	}
	mce, ok := expr.(MemberCallExpr)
	return mce, ok
}

func findEnclosingFunction(stmts []Stmt, line int) *FunctionStmt {
	var best *FunctionStmt

	for _, stmt := range stmts {
		if fn := findEnclosingInStmt(stmt, line); fn != nil {
			if best == nil || fn.Range.Start.Line > best.Range.Start.Line {
				best = fn
			}
		}
	}
	return best
}

func findEnclosingInStmt(stmt Stmt, line int) *FunctionStmt {
	if stmt == nil {
		return nil
	}
	switch s := stmt.(type) {
	case FunctionStmt:
		if line >= s.Range.Start.Line && line <= s.Range.End.Line {
			for _, inner := range s.Body {
				if fn := findEnclosingInStmt(inner, line); fn != nil {
					return fn
				}
			}
			return &s
		}
	case IfStmt:
		for _, inner := range s.ThenBody {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
		for _, inner := range s.ElseBody {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
	case ForStmt:
		for _, inner := range s.Body {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
	case ForInStmt:
		for _, inner := range s.Body {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
	case WhileStmt:
		for _, inner := range s.Body {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
	case TryCatchStmt:
		for _, inner := range s.TryBody {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
		for _, inner := range s.CatchBody {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
		for _, inner := range s.FinallyBody {
			if fn := findEnclosingInStmt(inner, line); fn != nil {
				return fn
			}
		}
	case MatchStmt:
		for _, c := range s.Cases {
			for _, inner := range c.Body {
				if fn := findEnclosingInStmt(inner, line); fn != nil {
					return fn
				}
			}
		}
	case ClassStmt:
		for _, method := range s.Methods {
			if line >= method.Range.Start.Line && line <= method.Range.End.Line {
				for _, inner := range method.Body {
					if fn := findEnclosingInStmt(inner, line); fn != nil {
						return fn
					}
				}
				m := method
				return &m
			}
		}
	}
	return nil
}

func resolveParamMemberDefinition(uri string, stmts []Stmt, pos Position, receiver string, member string) (any, bool) {
	fn := findEnclosingFunction(stmts, pos.Line+1)
	if fn == nil {
		return nil, false
	}

	for _, param := range fn.Params {
		if param.Name != receiver {
			continue
		}
		return resolveParamFieldLocation(uri, param, member)
	}
	return nil, false
}

func resolveParamFieldLocation(uri string, param Param, member string) (any, bool) {
	th := param.TypeHint

	if len(th.Fields) > 0 {
		if field, ok := th.Fields[member]; ok {
			rng := field.Range
			if rng.Start.Line > 0 && rng.Start.Column > 0 {
				return Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: rng.Start.Line - 1, Character: rng.Start.Column - 1},
						End:   Position{Line: rng.End.Line - 1, Character: rng.End.Column - 1},
					},
				}, true
			}
		}
	}

	if th.Name != "" {
		if loc := resolveTypeNameDefinition(uri, th.Name); loc != nil {
			return *loc, true
		}
	}

	return nil, false
}

func resolveTypeNameDefinition(uri string, typeName string) *Location {
	stmts, _ := parseTinyForLSP(uri, readURIFile(uri))
	return findTypeDefinitionInAST(stmts, typeName)
}

func findTypeDefinitionInAST(stmts []Stmt, typeName string) *Location {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case InterfaceStmt:
			if s.Name == typeName {
				rng := s.Range
				if rng.Start.Line > 0 {
					return &Location{
						URI: fileURIForStmt(s),
						Range: LSPRange{
							Start: Position{Line: rng.Start.Line - 1, Character: rng.Start.Column - 1},
							End:   Position{Line: rng.End.Line - 1, Character: rng.End.Column - 1},
						},
					}
				}
			}
		case ClassStmt:
		if s.Name == typeName && s.Line > 0 {
			return &Location{
				URI: fileURIForStmt(s),
				Range: LSPRange{
					Start: Position{Line: s.Line - 1, Character: s.Column - 1},
					End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(typeName)},
				},
			}
		}
		}
	}
	return nil
}

func readURIFile(uri string) string {
	path := URIToPath(uri)
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(data)
}

func fileURIForStmt(stmt Stmt) string {
	return ""
}

func definitionLocationFromSemanticModel(uri string, text string, word string) *Location {
	model := getSemanticModel(uri, text)
	if model == nil {
		return nil
	}

	for _, stmt := range model.AST {
		switch s := stmt.(type) {
		case FunctionStmt:
			if s.Name == word && s.Line > 0 {
				return &Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: s.Line - 1, Character: s.Column - 1},
						End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(word)},
					},
				}
			}
		case ClassStmt:
			if s.Name == word && s.Line > 0 {
				return &Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: s.Line - 1, Character: s.Column - 1},
						End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(word)},
					},
				}
			}
		case InterfaceStmt:
			if s.Name == word && s.Line > 0 {
				return &Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: s.Line - 1, Character: s.Column - 1},
						End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(word)},
					},
				}
			}
		case VariableStmt:
			if s.Name == word && s.Line > 0 {
				return &Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: s.Line - 1, Character: s.Column - 1},
						End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(word)},
					},
				}
			}
		case EnumStmt:
			if s.Name == word && s.Line > 0 {
				return &Location{
					URI: uri,
					Range: LSPRange{
						Start: Position{Line: s.Line - 1, Character: s.Column - 1},
						End:   Position{Line: s.Line - 1, Character: s.Column - 1 + len(word)},
					},
				}
			}
		}
	}

	return nil
}

func resolveTypeForReceiver(uri string, text string, receiver string, pos Position) (string, bool) {
	model := getSemanticModel(uri, text)
	if model == nil {
		return "", false
	}

	if typ, ok := model.Globals[receiver]; ok && typ != "" {
		return typ, true
	}

	stmts, _ := parseTinyForLSP(uri, text)
	fn := findEnclosingFunction(stmts, pos.Line+1)
	if fn != nil {
		for _, param := range fn.Params {
			if param.Name == receiver {
				th := param.TypeHint
				if th.Name != "" {
					return th.Name, true
				}
				if len(th.Fields) > 0 {
					return th.String(), true
				}
				return "any", false
			}
		}
	}

	return "", false
}

func completionItemsFromSemanticModelMembers(uri string, text string, ownerType string, hasParens bool) []CompletionItem {
	model := getSemanticModel(uri, text)
	if model == nil {
		return nil
	}

	var items []CompletionItem

	if strings.HasPrefix(ownerType, "class:") {
		className := strings.TrimPrefix(ownerType, "class:")
		if cls, ok := model.GetClass(className); ok {
			for _, field := range cls.Fields {
				item := CompletionItem{
					Label:  field.Name,
					Kind:   6,
					Detail: field.TypeHint.String(),
				}
				items = append(items, item)
			}
			for name, sig := range cls.MethodSignatures {
				detail := sig.ReturnType.String()
				if len(sig.Params) > 0 {
					params := make([]string, 0, len(sig.Params))
					for _, p := range sig.Params {
						params = append(params, p.Name+": "+p.TypeHint.String())
					}
					detail = "(" + strings.Join(params, ", ") + ") " + detail
				}
				kind := 3
				if hasParens {
					kind = 2
				}
				item := CompletionItem{
					Label:  name,
					Kind:   kind,
					Detail: detail,
				}
				items = append(items, item)
			}
			return items
		}
	}

	if strings.HasPrefix(ownerType, "interface:") {
		ifaceName := strings.TrimPrefix(ownerType, "interface:")
		if iface, ok := model.GetInterface(ifaceName); ok {
			for name, th := range iface.Fields {
				item := CompletionItem{
					Label:  name,
					Kind:   6,
					Detail: th.String(),
				}
				items = append(items, item)
			}
			return items
		}
	}

	iface, ok := model.GetInterface(ownerType)
	if ok {
		for name, th := range iface.Fields {
			item := CompletionItem{
				Label:  name,
				Kind:   6,
				Detail: th.String(),
			}
			items = append(items, item)
		}
		return items
	}

	cls, ok := model.GetClass(ownerType)
	if ok {
		for _, field := range cls.Fields {
			item := CompletionItem{
				Label:  field.Name,
				Kind:   6,
				Detail: field.TypeHint.String(),
			}
			items = append(items, item)
		}
		for name, sig := range cls.MethodSignatures {
			detail := sig.ReturnType.String()
			if len(sig.Params) > 0 {
				params := make([]string, 0, len(sig.Params))
				for _, p := range sig.Params {
					params = append(params, p.Name+": "+p.TypeHint.String())
				}
				detail = "(" + strings.Join(params, ", ") + ") " + detail
			}
			kind := 3
			if hasParens {
				kind = 2
			}
			item := CompletionItem{
				Label:  name,
				Kind:   kind,
				Detail: detail,
			}
			items = append(items, item)
		}
		return items
	}

	return items
}

func resolveMemberViaSemanticModel(file string, pkg string, receiverType string, member string) (SymbolInfo, string, bool) {
	model := getSemanticModel(file, "")
	if model == nil {
		return SymbolInfo{}, "", false
	}

	mi, ok := model.MemberType(receiverType, member)
	if !ok {
		return SymbolInfo{}, "", false
	}

	kind := SymbolVariable
	if mi.Kind == "method" {
		kind = SymbolFunction
	}

	sym := SymbolInfo{
		Name:   mi.Name,
		Kind:   kind,
		Type:   mi.Type,
		Detail: mi.Name,
	}

	if mi.Kind == "method" {
		params := make([]string, 0, len(mi.Params))
		for _, p := range mi.Params {
			params = append(params, p.Name+": "+p.TypeHint.String())
		}
		sig := mi.Name + "(" + strings.Join(params, ", ") + ")"
		if !mi.ReturnType.IsEmpty() {
			sig += " " + mi.ReturnType.String()
		}
		sym.Detail = sig
	}

	return sym, mi.Type, true
}

func resolveTypeNameDef(uri string, typeName string) *Location {
	stmts, _ := parseTinyForLSP(uri, readURIFile(uri))
	return findTypeDefinitionInAST(stmts, typeName)
}

func sortFields(fields map[string]TypeHint) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func findEnclosingIfStmt(stmts []Stmt, line int) *IfStmt {
	var best *IfStmt

	var walk func(stmts []Stmt)
	walk = func(stmts []Stmt) {
		for _, stmt := range stmts {
			if stmt == nil {
				continue
			}
			switch s := stmt.(type) {
			case IfStmt:
				if line >= s.Line {
					if best == nil || s.Line > best.Line {
						best = &s
					}
					walk(s.ThenBody)
					walk(s.ElseBody)
				}
			case FunctionStmt:
				endLine := s.Line
				if s.Range.End.Line > 0 {
					endLine = s.Range.End.Line
				}
				if line >= s.Line && line <= endLine {
					walk(s.Body)
				}
			case ForStmt:
				if line >= s.Line {
					walk(s.Body)
				}
			case ForInStmt:
				if line >= s.Line {
					walk(s.Body)
				}
			case WhileStmt:
				if line >= s.Line {
					walk(s.Body)
				}
			case TryCatchStmt:
				if line >= s.Line {
					walk(s.TryBody)
					walk(s.CatchBody)
					walk(s.FinallyBody)
				}
			case MatchStmt:
				if line >= s.Line {
					for _, c := range s.Cases {
						walk(c.Body)
					}
				}
			}
		}
	}

	walk(stmts)
	return best
}

func isInIfElseBranch(ifStmt *IfStmt, line int) bool {
	if ifStmt == nil {
		return false
	}
	for _, stmt := range ifStmt.ElseBody {
		if stmt == nil {
			continue
		}
		switch s := stmt.(type) {
		case IfStmt:
			if line >= s.Line {
				return true
			}
		default:
			_ = s
			return true
		}
	}
	return false
}

func applyTypeNarrowingFromAST(scope *Scope, condition Expr, invert bool) {
	if condition == nil {
		return
	}

	switch e := condition.(type) {
	case BinaryExpr:
		switch e.Op {
		case TOKEN_AND:
			if !invert {
				applyTypeNarrowingFromAST(scope, e.Left, false)
				applyTypeNarrowingFromAST(scope, e.Right, false)
			} else {
				applyTypeNarrowingFromAST(scope, e.Left, true)
			}
		case TOKEN_OR:
			if invert {
				applyTypeNarrowingFromAST(scope, e.Left, true)
				applyTypeNarrowingFromAST(scope, e.Right, true)
			}
		case TOKEN_EQ:
			narrowFromEquality(scope, e.Left, e.Right, invert)
		case TOKEN_NEQ:
			narrowFromEquality(scope, e.Left, e.Right, !invert)
		case TOKEN_INSTANCEOF:
			narrowFromInstanceOf(scope, e.Left, e.Right, invert)
		}

	case UnaryExpr:
		if e.Op == TOKEN_BANG {
			applyTypeNarrowingFromAST(scope, e.Right, !invert)
		}

	case IdentExpr:
		narrowFromTruthy(scope, e.Name, invert)

	case CallValueExpr:
		if typeOfExpr, ok := e.Callee.(TypeOfExpr); ok {
			_ = typeOfExpr
		}
	}
}

func narrowFromEquality(scope *Scope, left Expr, right Expr, invert bool) {
	leftIdent, leftIsIdent := left.(IdentExpr)
	rightNull, rightIsNull := right.(NullExpr)

	if leftIsIdent && rightIsNull {
		if invert {
			narrowSymbolRemovingNull(scope, leftIdent.Name)
		} else {
			if sym, ok := scope.Resolve(leftIdent.Name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		}
		return
	}

	rightIdent, rightIsIdent := right.(IdentExpr)
	leftNull, leftIsNull := left.(NullExpr)

	if leftIsNull && rightIsIdent {
		if invert {
			narrowSymbolRemovingNull(scope, rightIdent.Name)
		} else {
			if sym, ok := scope.Resolve(rightIdent.Name); ok {
				sym.Type = "null"
				scope.Define(sym)
			}
		}
		return
	}

	_ = leftIdent
	_ = leftIsIdent
	_ = rightNull
	_ = rightIsNull
	_ = rightIdent
	_ = rightIsIdent
	_ = leftNull
	_ = leftIsNull
}

func narrowFromInstanceOf(scope *Scope, nameExpr Expr, classExpr Expr, invert bool) {
	ident, ok := nameExpr.(IdentExpr)
	if !ok {
		return
	}
	className := exprToStringNode(classExpr)
	if className == "" {
		return
	}
	if invert {
		narrowSymbolRemovingType(scope, ident.Name, "class:"+className)
	} else {
		if sym, ok := scope.Resolve(ident.Name); ok {
			sym.Type = "class:" + className
			scope.Define(sym)
		}
	}
}

func narrowFromTruthy(scope *Scope, name string, invert bool) {
	if invert {
		if sym, ok := scope.Resolve(name); ok {
			sym.Type = "null"
			scope.Define(sym)
		}
	} else {
		narrowSymbolRemovingNull(scope, name)
	}
}

func exprToStringNode(expr Expr) string {
	if expr == nil {
		return ""
	}
	switch e := expr.(type) {
	case IdentExpr:
		return e.Name
	case PropertyExpr:
		obj := exprToStringNode(e.Object)
		if obj == "" {
			return ""
		}
		return obj + "." + e.Name
	}
	return ""
}
