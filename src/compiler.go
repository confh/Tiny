package main

import (
	"maps"
	"path/filepath"
	"strconv"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

type ImportState int

const (
	ImportNotLoaded ImportState = iota
	ImportLoading
	ImportLoaded
)

type BindingKind int

const (
	BindingGlobal BindingKind = iota
	BindingLocal
)

type Binding struct {
	Kind     BindingKind
	Name     string
	Slot     int
	Constant bool
}

type LoopContext struct {
	Start         int
	BreakJumps    []int
	ContinueJumps []int
}

type Compiler struct {
	mainInstructions       []Instruction
	functions              map[string]Function
	classes                map[string]Class
	loopStack              []LoopContext
	anonymousFunctionCount int
	declaredFunctions      map[string]bool

	activeLocks []Expr

	functionIDs    map[string]int
	nextFunctionID int

	importStates map[string]ImportState
	importStack  []string

	isCompilingNamespace bool

	currentFile   string
	currentLine   int
	currentColumn int

	matchTempID int

	currentNamespaceVariables map[string]string
	currentNamespaceClasses   map[string]string
	currentNamespaceFunctions map[string]string
	currentNamespaceEnums     map[string]string

	inMethod bool

	outerBindings   map[string]Binding
	currentCaptures map[string]CapturedVar

	parent   *Compiler
	captured map[string]CapturedVar

	outerScopes []map[string]Binding

	currentInstructions *[]Instruction

	scopes  []map[string]Binding
	scopeID int

	localCount      int
	globalConstants map[string]bool
}

func unwrapExport(stmt Stmt) (Stmt, bool) {
	if exp, ok := stmt.(ExportStmt); ok {
		return exp.Inner, true
	}

	return stmt, false
}

func getNumberLiteral(expr Expr) (int, float64, bool, bool) {
	num, ok := expr.(NumberExpr)
	if !ok {
		return 0, 0, false, false
	}

	return num.Value, 0, false, true
}

func optimizeExpr(expr Expr) Expr {
	switch e := expr.(type) {
	case BinaryExpr:
		left := optimizeExpr(e.Left)
		right := optimizeExpr(e.Right)

		leftInt, leftIsInt := left.(NumberExpr)
		rightInt, rightIsInt := right.(NumberExpr)

		if leftIsInt && rightIsInt {
			switch e.Op {
			case TOKEN_PLUS:
				return NumberExpr{Value: leftInt.Value + rightInt.Value, File: leftInt.File, Column: leftInt.Column, Line: leftInt.Line}
			case TOKEN_MINUS:
				return NumberExpr{Value: leftInt.Value - rightInt.Value, File: leftInt.File, Column: leftInt.Column, Line: leftInt.Line}
			case TOKEN_STAR:
				return NumberExpr{Value: leftInt.Value * rightInt.Value, File: leftInt.File, Column: leftInt.Column, Line: leftInt.Line}
			case TOKEN_SLASH:
				if rightInt.Value == 0 {
					return BinaryExpr{Left: left, Op: e.Op, Right: right}
				}
				return NumberExpr{Value: leftInt.Value / rightInt.Value, File: leftInt.File, Column: leftInt.Column, Line: leftInt.Line}
			case TOKEN_EQ:
				return BoolExpr{Value: leftInt.Value == rightInt.Value}
			case TOKEN_NEQ:
				return BoolExpr{Value: leftInt.Value != rightInt.Value}
			case TOKEN_LT:
				return BoolExpr{Value: leftInt.Value < rightInt.Value}
			case TOKEN_GT:
				return BoolExpr{Value: leftInt.Value > rightInt.Value}
			case TOKEN_LTE:
				return BoolExpr{Value: leftInt.Value <= rightInt.Value}
			case TOKEN_GTE:
				return BoolExpr{Value: leftInt.Value >= rightInt.Value}
			}
		}

		// string == string / string != string
		leftString, leftIsString := left.(StringExpr)
		rightString, rightIsString := right.(StringExpr)

		if leftIsString && rightIsString {
			switch e.Op {
			case TOKEN_PLUS:
				return StringExpr{
					Value: leftString.Value + rightString.Value,
				}
			case TOKEN_EQ:
				return BoolExpr{Value: leftString.Value == rightString.Value}
			case TOKEN_NEQ:
				return BoolExpr{Value: leftString.Value != rightString.Value}
			}
		}

		// bool == bool / bool != bool
		leftBool, leftIsBool := left.(BoolExpr)
		rightBool, rightIsBool := right.(BoolExpr)

		if leftIsBool && rightIsBool {
			switch e.Op {
			case TOKEN_EQ:
				return BoolExpr{Value: leftBool.Value == rightBool.Value}
			case TOKEN_NEQ:
				return BoolExpr{Value: leftBool.Value != rightBool.Value}
			case TOKEN_AND:
				return BoolExpr{Value: leftBool.Value && rightBool.Value}
			case TOKEN_OR:
				return BoolExpr{Value: leftBool.Value || rightBool.Value}
			}
		}

		return BinaryExpr{
			Left:  left,
			Op:    e.Op,
			Right: right,
		}

	case ArrayExpr:
		elements := make([]Expr, len(e.Elements))

		for i, element := range e.Elements {
			elements[i] = optimizeExpr(element)
		}

		return ArrayExpr{Elements: elements}

	case TernaryExpr:
		condition := optimizeExpr(e.Condition)
		thenExpr := optimizeExpr(e.ThenExpr)
		elseExpr := optimizeExpr(e.ElseExpr)

		if b, ok := condition.(BoolExpr); ok {
			if b.Value {
				return thenExpr
			}

			return elseExpr
		}

		return TernaryExpr{
			Condition: condition,
			ThenExpr:  thenExpr,
			ElseExpr:  elseExpr,
		}

	case CallValueExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = optimizeExpr(arg)
		}

		return CallValueExpr{
			Callee: optimizeExpr(e.Callee),
			Args:   args,
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
		}

	case ObjectExpr:
		fields := make([]ObjectField, len(e.Fields))

		for i, field := range e.Fields {
			if field.HasCopy {
				fields[i] = ObjectField{
					Name:    field.Name,
					Value:   nil,
					Copy:    field.Copy,
					HasCopy: true,
				}
			} else {
				fields[i] = ObjectField{
					Name:  field.Name,
					Value: optimizeExpr(field.Value),
				}
			}
		}

		return ObjectExpr{Fields: fields}

	case CallExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = optimizeExpr(arg)
		}

		return CallExpr{
			Name:   e.Name,
			Args:   args,
			Line:   e.Line,
			Column: e.Column,
			File:   e.File,
		}

	case MemberCallExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = optimizeExpr(arg)
		}

		return MemberCallExpr{
			Object: e.Object,
			Method: e.Method,
			Args:   args,
			Line:   e.Line,
			Column: e.Column,
			File:   e.File,
			Safe:   e.Safe,
		}

	case PropertyExpr:
		return PropertyExpr{
			Object: optimizeExpr(e.Object),
			Name:   e.Name,
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
			Safe:   e.Safe,
		}

	case UnaryExpr:
		right := optimizeExpr(e.Right)

		switch e.Op {
		case TOKEN_BANG:
			if boolExpr, ok := right.(BoolExpr); ok {
				return BoolExpr{Value: !boolExpr.Value}
			}

		case TOKEN_MINUS:
			if num, ok := right.(NumberExpr); ok {
				return NumberExpr{
					Value:  -num.Value,
					File:   num.File,
					Line:   num.Line,
					Column: num.Column,
				}
			}
		}

		return UnaryExpr{
			Op:    e.Op,
			Right: right,
		}

	default:
		return expr
	}
}

func NewCompiler() *Compiler {
	c := &Compiler{
		mainInstructions:       []Instruction{},
		functions:              map[string]Function{},
		classes:                map[string]Class{},
		loopStack:              []LoopContext{},
		localCount:             0,
		globalConstants:        map[string]bool{},
		anonymousFunctionCount: 0,
		functionIDs:            map[string]int{},
		declaredFunctions:      map[string]bool{},
		scopes:                 []map[string]Binding{},
		importStates:           map[string]ImportState{},
		importStack:            []string{},
	}

	c.currentInstructions = &c.mainInstructions
	c.beginScope()

	return c
}

func (c *Compiler) predeclareFunctions(statements []Stmt) {
	for _, stmt := range statements {
		switch s := stmt.(type) {
		case FunctionStmt:
			c.declaredFunctions[s.Name] = true
			c.getFunctionID(s.Name)

		case NamespaceStmt:
			for _, nsStmt := range s.Statements {
				if fn, ok := nsStmt.(FunctionStmt); ok {
					fullName := s.Name + "." + fn.Name
					c.declaredFunctions[fullName] = true
					c.getFunctionID(fullName)
				}
			}
		}
	}
}

func (c *Compiler) getFunctionID(name string) int {
	if id, exists := c.functionIDs[name]; exists {
		return id
	}

	id := c.nextFunctionID
	c.nextFunctionID++

	c.functionIDs[name] = id

	return id
}

func (c *Compiler) setLocation(file string, line int, column int) {
	c.currentFile = file
	c.currentLine = line
	c.currentColumn = column
}

func (c *Compiler) fatalError(kind ErrorKind, format string, args ...any) {
	LangErrorAt(kind, c.currentFile, c.currentLine, c.currentColumn, format, args...)
}

func (c *Compiler) newMatchTempName() string {
	name := "__match_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++
	return name
}

func (c *Compiler) beginScope() {
	c.scopes = append(c.scopes, map[string]Binding{})
}

func (c *Compiler) endScope() {
	if len(c.scopes) == 0 {
		c.fatalError(ErrorInternal, "scope stack underflow")
	}

	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *Compiler) currentScope() map[string]Binding {
	if len(c.scopes) == 0 {
		c.beginScope()
	}

	return c.scopes[len(c.scopes)-1]
}

func getParamFlags(params []Param) (bool, bool) {
	hasDefaults := false
	hasTypeHints := false

	for _, param := range params {
		if param.HasDefault {
			hasDefaults = true
		}

		if !param.TypeHint.IsEmpty() {
			hasTypeHints = true
		}
	}

	return hasDefaults, hasTypeHints
}

func (c *Compiler) compileFunctionLiteral(stmt FunctionStmt) {
	compiledName := c.makeAnonymousFunctionName()

	outerBindings := c.collectCapturableBindings()

	oldInstructions := c.currentInstructions
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures

	functionInstructions := []Instruction{}

	c.currentInstructions = &functionInstructions
	c.scopes = []map[string]Binding{}
	c.localCount = 0
	c.inMethod = false
	c.outerBindings = outerBindings
	c.currentCaptures = map[string]CapturedVar{}

	c.beginScope()

	for _, param := range stmt.Params {
		c.declareVariable(param.Name, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, NewUndefined())
	c.emit(OP_RETURN, nil)

	captures := []CapturedVar{}
	for _, capture := range c.currentCaptures {
		captures = append(captures, capture)
	}

	localCount := c.localCount
	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[compiledName] = Function{
		ID:           c.getFunctionID(compiledName),
		Name:         compiledName,
		Params:       stmt.Params,
		ReturnType:   stmt.ReturnType,
		Instructions: functionInstructions,
		LocalCount:   localCount,
		Captures:     captures,
		Async:        stmt.Async,
		HasDefaults:  hasDefaults,
		HasTypeHints: hasTypeHints,
	}

	c.currentInstructions = oldInstructions
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures

	// IMPORTANT: leave closure on stack.
	c.emit(OP_CLOSURE, ClosureInfo{
		Name:     compiledName,
		Captures: captures,
	})
}

func (c *Compiler) compileNestedFunction(stmt FunctionStmt) {
	c.compileFunctionLiteral(stmt)

	if stmt.Name == "" {
		return
	}

	binding := c.declareVariable(stmt.Name, true)

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     stmt.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Constant: true,
		})
	}
}

func (c *Compiler) declareVariable(name string, constant bool) Binding {
	scope := c.currentScope()

	if _, exists := scope[name]; exists {
		LangErrorAt(ErrorName, c.currentFile, c.currentLine, c.currentColumn, "variable already declared in this scope: %s", name)
	}

	if c.isInsideFunction() {
		slot := c.localCount
		c.localCount++

		binding := Binding{
			Kind:     BindingLocal,
			Name:     name,
			Slot:     slot,
			Constant: constant,
		}

		scope[name] = binding
		return binding
	}

	globalName := name

	// Top-level variables keep their real name.
	// Block/global variables get a hidden internal name.
	if len(c.scopes) > 1 {
		globalName = "__scope_" + strconv.Itoa(c.scopeID) + "_" + name
		c.scopeID++
	}

	binding := Binding{
		Kind:     BindingGlobal,
		Name:     globalName,
		Slot:     -1,
		Constant: constant,
	}

	scope[name] = binding
	c.globalConstants[globalName] = constant

	return binding
}

func (c *Compiler) resolveVariable(name string) (Binding, bool) {
	for i := len(c.scopes) - 1; i >= 0; i-- {
		if binding, exists := c.scopes[i][name]; exists {
			return binding, true
		}
	}

	return Binding{}, false
}

func (c *Compiler) compileScopedBlock(body []Stmt) {
	c.beginScope()

	for _, stmt := range body {
		c.compileStatement(stmt)
	}

	c.endScope()
}

func (c *Compiler) CompileProgram(program Program) ([]Instruction, map[string]Function, map[string]Class) {
	c.predeclareFunctions(program.Statements)

	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	return c.mainInstructions, c.functions, c.classes
}

func (c *Compiler) compileNamespace(stmt NamespaceStmt) {
	namespaceStdImports := map[string]string{}
	namespacePluginImports := map[string]string{}
	hasExplicitExports := false

	for _, raw := range stmt.Statements {
		if _, ok := raw.(ExportStmt); ok {
			hasExplicitExports = true
			break
		}
	}

	oldNamespaceFunctions := c.currentNamespaceFunctions
	oldNamespaceVariables := c.currentNamespaceVariables
	oldNamespaceClasses := c.currentNamespaceClasses
	oldNamespaceEnums := c.currentNamespaceEnums
	oldIsCompilingNamespace := c.isCompilingNamespace

	namespaceFunctions := map[string]string{}
	namespaceVariables := map[string]string{}
	namespaceClasses := map[string]string{}
	namespaceEnums := map[string]string{}
	members := map[string]Value{}

	// 1. Nested namespaces first
	for _, raw := range stmt.Statements {
		ns, ok := raw.(NamespaceStmt)
		if !ok {
			continue
		}

		c.compileNamespace(ns)

		members[ns.Name] = NewNative(NamespaceMemberRef{
			GlobalName: ns.Name,
		})
	}

	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		imp, ok := inner.(ImportStmt)
		if !ok || (!imp.Std && !imp.Plugin) {
			continue
		}

		alias := imp.Alias
		if alias == "" {
			alias = imp.Path
		}

		fullName := stmt.Name + "." + alias

		namespaceVariables[alias] = fullName

		if imp.Std {
			namespaceStdImports[alias] = fullName
		}

		if imp.Plugin {
			namespacePluginImports[alias] = fullName
		}
	}

	// 2. Collect functions
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		fn, ok := inner.(FunctionStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + fn.Name

		namespaceFunctions[fn.Name] = fullName

		if !hasExplicitExports || exported {
			members[fn.Name] = NewNative(FunctionValue{Name: fullName})
		}

		c.functions[fullName] = Function{
			ID:     c.getFunctionID(fullName),
			Async:  fn.Async,
			Name:   fullName,
			Params: fn.Params,
		}
	}

	// 3. Collect variables
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		v, ok := inner.(VariableStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + v.Name

		namespaceVariables[v.Name] = fullName

		if !hasExplicitExports || exported {
			members[v.Name] = NewNative(NamespaceMemberRef{GlobalName: fullName})
		}
	}

	// 4. Collect classes
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		classStmt, ok := inner.(ClassStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + classStmt.Name

		namespaceClasses[classStmt.Name] = fullName

		if !hasExplicitExports || exported {
			members[classStmt.Name] = NewNative(Class{Name: fullName})
		}
	}

	// 5. Collect enums
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		enumStmt, ok := inner.(EnumStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + enumStmt.Name

		namespaceEnums[enumStmt.Name] = fullName

		if !hasExplicitExports || exported {
			members[enumStmt.Name] = NewNative(NamespaceMemberRef{
				GlobalName: fullName,
			})
		}
	}

	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		imp, ok := inner.(ImportStmt)
		if !ok || (!imp.Std && !imp.Plugin) {
			continue
		}

		alias := imp.Alias
		if alias == "" {
			alias = imp.Path
		}

		fullName := stmt.Name + "." + alias

		resolvedPath := imp.Path

		if imp.Plugin {
			resolvedPath = c.resolveImportPath(imp.Path)
		}

		c.emit(OP_CONST, resolvedPath)

		if imp.Std {
			c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
				Object:   "Plugin",
				Method:   "std",
				ArgCount: 1,
			})
		} else if imp.Plugin {
			c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
				Object:   "Plugin",
				Method:   "load",
				ArgCount: 1,
			})
		}

		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: true,
		})

		c.globalConstants[fullName] = true
	}

	// IMPORTANT: set namespace maps BEFORE compiling enum/var/function bodies.
	c.currentNamespaceFunctions = namespaceFunctions
	c.currentNamespaceVariables = namespaceVariables
	c.currentNamespaceClasses = namespaceClasses
	c.currentNamespaceEnums = namespaceEnums
	c.isCompilingNamespace = true

	// 6. Compile enums as hidden globals FIRST.
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		enumStmt, ok := inner.(EnumStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + enumStmt.Name

		obj := ObjectValue{}

		for _, member := range enumStmt.Members {
			if _, exists := obj[member]; exists {
				c.fatalError(ErrorName, "duplicate enum member %s.%s", enumStmt.Name, member)
			}

			obj[member] = NewNative(member)
		}

		c.emit(OP_CONST, obj)

		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: true,
		})

		c.globalConstants[fullName] = true
	}

	// 7. Compile variables AFTER enums.
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		v, ok := inner.(VariableStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + v.Name

		c.compileExpr(v.Value)

		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: v.Constant,
			TypeHint: v.TypeHint,
		})

		c.globalConstants[fullName] = v.Constant
	}

	// 8. Compile classes
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		classStmt, ok := inner.(ClassStmt)
		if !ok {
			continue
		}

		namespacedClass := ClassStmt{
			Name:    stmt.Name + "." + classStmt.Name,
			Fields:  classStmt.Fields,
			Methods: classStmt.Methods,
			Embeds:  classStmt.Embeds,
		}

		c.compileClass(namespacedClass)
	}

	// 9. Compile functions
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		fn, ok := inner.(FunctionStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + fn.Name

		namespacedFn := FunctionStmt{
			Name:       fullName,
			Params:     fn.Params,
			ReturnType: fn.ReturnType,
			Body:       fn.Body,
			Private:    fn.Private,
		}

		c.compileFunction(namespacedFn)
	}

	c.currentNamespaceFunctions = oldNamespaceFunctions
	c.currentNamespaceVariables = oldNamespaceVariables
	c.currentNamespaceClasses = oldNamespaceClasses
	c.currentNamespaceEnums = oldNamespaceEnums
	c.isCompilingNamespace = oldIsCompilingNamespace

	// 10. Create namespace object.
	c.emit(OP_CONST, NamespaceValue{
		Name:    stmt.Name,
		Members: members,
	})

	binding := c.declareVariable(stmt.Name, true)

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     stmt.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Constant: true,
		})
	}
}

func (c *Compiler) compileMatchStatement(stmt MatchStmt) {
	// Create block scope so hidden temp does not leak.
	c.beginScope()
	defer c.endScope()

	tempName := c.newMatchTempName()

	// const __match_0 = <value>;
	c.compileExpr(stmt.Value)

	tempBinding := c.declareVariable(tempName, true)

	if tempBinding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     tempName,
			Slot:     tempBinding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     tempBinding.Name,
			Constant: true,
		})
	}

	endJumps := []int{}

	for _, matchCase := range stmt.Cases {
		// load temp
		if tempBinding.Kind == BindingLocal {
			c.emit(OP_LOAD_LOCAL, tempBinding.Slot)
		} else {
			c.emit(OP_LOAD_GLOBAL, tempBinding.Name)
		}

		// load case value
		c.compileExpr(matchCase.Value)

		// compare
		c.emit(OP_EQ, nil)

		// if false, jump to next case
		jumpToNext := c.emitJump(OP_JUMP_IF_FALSE)

		// body
		c.compileScopedBlock(matchCase.Body)

		// after matching body, jump to end
		endJumps = append(endJumps, c.emitJump(OP_JUMP))

		// next case starts here
		c.patchJump(jumpToNext)
	}

	if stmt.Default != nil {
		c.compileScopedBlock(stmt.Default)
	}

	for _, jump := range endJumps {
		c.patchJump(jump)
	}
}

func (c *Compiler) emitStoreBinding(binding Binding, name string, constant bool, typeHint TypeHint) {
	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     name,
			Slot:     binding.Slot,
			Constant: constant,
			TypeHint: typeHint,
		})
		return
	}

	c.emit(OP_STORE_GLOBAL, VariableInfo{
		Name:     binding.Name,
		Constant: constant,
		TypeHint: typeHint,
	})
}

func (c *Compiler) emitLoadBinding(binding Binding) {
	if binding.Kind == BindingLocal {
		c.emit(OP_LOAD_LOCAL, binding.Slot)
		return
	}

	c.emit(OP_LOAD_GLOBAL, binding.Name)
}

func (c *Compiler) emitAssignBinding(binding Binding) {
	if binding.Kind == BindingLocal {
		c.emit(OP_ASSIGN_LOCAL, binding.Slot)
		return
	}

	c.emit(OP_ASSIGN_GLOBAL, binding.Name)
}

func (c *Compiler) compileForInStatement(stmt ForInStmt) {
	c.beginScope()
	defer c.endScope()

	iterName := "__iter_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++

	indexInternalName := "__i_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++

	// const __iter = iterable
	c.compileExpr(stmt.Iterable)

	iterBinding := c.declareVariable(iterName, true)
	c.emitStoreBinding(iterBinding, iterName, true, TypeHint{})

	// let __i = 0
	c.emit(OP_CONST, 0)

	indexBinding := c.declareVariable(indexInternalName, false)
	c.emitStoreBinding(indexBinding, indexInternalName, false, TypeHint{})

	loopStart := len(*c.currentInstructions)

	// condition: __i < len(__iter)
	c.emitLoadBinding(indexBinding)
	c.emitLoadBinding(iterBinding)
	c.emit(OP_LEN, nil)
	c.emit(OP_LT, nil)

	exitJump := c.emitJump(OP_JUMP_IF_FALSE)

	// item/index block scope
	c.beginScope()

	// const item = __iter[__i]
	c.emitLoadBinding(iterBinding)
	c.emitLoadBinding(indexBinding)
	c.emit(OP_INDEX, nil) // use your actual index opcode if different

	itemBinding := c.declareVariable(stmt.ItemName, true)
	c.emitStoreBinding(itemBinding, stmt.ItemName, true, TypeHint{})

	// const index = __i
	if stmt.IndexName != "" {
		c.emitLoadBinding(indexBinding)

		userIndexBinding := c.declareVariable(stmt.IndexName, true)
		c.emitStoreBinding(userIndexBinding, stmt.IndexName, true, TypeHint{})
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.endScope()

	// __i = __i + 1
	c.emitLoadBinding(indexBinding)
	c.emit(OP_CONST, 1)
	c.emit(OP_ADD, nil)
	c.emitAssignBinding(indexBinding)

	c.emit(OP_JUMP, loopStart)

	c.patchJump(exitJump)
}

func (c *Compiler) compileStatement(stmt Stmt) {
	switch s := stmt.(type) {
	case ForInStmt:
		c.compileForInStatement(s)

	case MatchStmt:
		c.compileMatchStatement(s)

	case NamespaceStmt:
		c.compileNamespace(s)

	case VariableStmt:
		c.compileExpr(s.Value)

		binding := c.declareVariable(s.Name, s.Constant)

		c.setLocation(s.File, s.Line, s.Column)

		if binding.Kind == BindingLocal {
			c.emit(OP_STORE_LOCAL, VariableInfo{
				Name:     s.Name,
				Slot:     binding.Slot,
				Constant: s.Constant,
				TypeHint: s.TypeHint,
			})
		} else {
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:     binding.Name,
				Constant: s.Constant,
				TypeHint: s.TypeHint,
			})
		}

	case IncrementStmt:
		if binding, exists := c.resolveVariable(s.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_INC_LOCAL, IncrementInfo{
					Slot:      binding.Slot,
					IntAmount: 1,
					IsFloat:   false,
				})
			} else {
				c.emit(OP_INC_GLOBAL, IncrementInfo{
					Name:      binding.Name,
					IntAmount: 1,
					IsFloat:   false,
				})
			}
			return
		}

		if binding, exists := c.ensureCaptured(s.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_INC_LOCAL, IncrementInfo{
					Slot:      binding.Slot,
					IntAmount: 1,
					IsFloat:   false,
				})
			} else {
				c.emit(OP_INC_GLOBAL, IncrementInfo{
					Name:      binding.Name,
					IntAmount: 1,
					IsFloat:   false,
				})
			}
			return
		}

		if c.currentNamespaceVariables != nil {
			if fullName, exists := c.currentNamespaceVariables[s.Name]; exists {
				c.emit(OP_INC_GLOBAL, IncrementInfo{
					Name:      fullName,
					IntAmount: 1,
					IsFloat:   false,
				})
				return
			}
		}

		c.emit(OP_INC_GLOBAL, s.Name)

	case DecrementStmt:
		if binding, exists := c.resolveVariable(s.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_DEC_LOCAL, binding.Slot)
			} else {
				c.emit(OP_DEC_GLOBAL, binding.Name)
			}
			return
		}

		if binding, exists := c.ensureCaptured(s.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_DEC_LOCAL, binding.Slot)
			} else {
				c.emit(OP_DEC_GLOBAL, binding.Name)
			}
			return
		}

		if c.currentNamespaceVariables != nil {
			if fullName, exists := c.currentNamespaceVariables[s.Name]; exists {
				c.emit(OP_DEC_GLOBAL, fullName)
				return
			}
		}

		c.emit(OP_DEC_GLOBAL, s.Name)

	case AssignStmt:
		if c.outerBindings == nil {
			if c.tryCompileFastIncrement(s.Name, s.Value) {
				return
			}
		} else {
			if _, isOuter := c.outerBindings[s.Name]; !isOuter {
				if c.tryCompileFastIncrement(s.Name, s.Value) {
					return
				}
			}
		}

		c.compileExpr(s.Value)

		if binding, exists := c.resolveVariable(s.Name); exists {
			c.setLocation(s.File, s.Line, s.Column)
			if binding.Kind == BindingLocal {
				c.emit(OP_ASSIGN_LOCAL, binding.Slot)
			} else {
				c.emit(OP_ASSIGN_GLOBAL, binding.Name)
			}
			return
		}

		if c.outerBindings != nil {
			if outer, exists := c.outerBindings[s.Name]; exists {
				capture, already := c.currentCaptures[s.Name]
				if !already {
					slot := c.localCount
					c.localCount++

					capture = CapturedVar{
						Name:      s.Name,
						OuterSlot: outer.Slot,
						InnerSlot: slot,
					}

					c.currentCaptures[s.Name] = capture

					c.currentScope()[s.Name] = Binding{
						Kind:     BindingLocal,
						Name:     s.Name,
						Slot:     slot,
						Constant: outer.Constant,
					}
				}

				c.setLocation(s.File, s.Line, s.Column)

				c.emit(OP_ASSIGN_LOCAL, capture.InnerSlot)
				return
			}
		}

		if c.currentNamespaceVariables != nil {
			if fullName, exists := c.currentNamespaceVariables[s.Name]; exists {
				c.emit(OP_ASSIGN_GLOBAL, fullName)
				return
			}
		}

		if c.isCompilingNamespace {
			LangErrorAt(ErrorName, c.currentFile, c.currentLine, c.currentColumn, "undefined variable in namespace: %s", s.Name)
		}

		c.emit(OP_ASSIGN_GLOBAL, s.Name)

	case IndexAssignStmt:
		c.compileExpr(s.Object)
		c.compileExpr(s.Index)
		c.compileExpr(s.Value)
		c.emit(OP_SET_INDEX, nil)

	case ThrowStmt:
		c.compileExpr(s.Value)
		c.setLocation(s.File, s.Line, s.Column)
		c.emit(OP_THROW, nil)

	case TryCatchStmt:
		c.compileTryCatchStatement(s)

	case BreakStmt:
		c.compileBreakStatement()

	case ContinueStmt:
		c.compileContinueStatement()

	case ExprStmt:
		c.compileExpr(s.Value)
		c.emit(OP_POP, nil)

	case FunctionStmt:
		if c.isCompilingMain() {
			c.compileFunction(s)
		} else {
			c.compileNestedFunction(s)
		}
		c.setLocation(s.File, s.Line, s.Column)

	case ReturnStmt:
		c.setLocation(s.File, s.Line, s.Column)
		if s.HasValue {
			c.compileExpr(s.Value)
		} else {
			c.emit(OP_CONST, NewUndefined())
		}

		for i := len(c.activeLocks) - 1; i >= 0; i-- {
			c.compileExpr(c.activeLocks[i])
			c.emit(OP_UNLOCK_MUTEX, nil)
		}

		c.emit(OP_RETURN, nil)

	case ImportStmt:
		if s.Std {
			c.compileStdImport(s)
			return
		}

		if s.Plugin {
			c.compilePluginImport(s)
			return
		}

		c.fatalError(ErrorInternal, "imports should be resolved before compiling")

	case IfStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileIfStatement(s)

	case LockStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileLockStmt(s)

	case WhileStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileWhileStatement(s)

	case ForStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileForStatement(s)

	case PropertyAssignStmt:
		c.compileExpr(s.Object)
		c.compileExpr(s.Value)
		c.emit(OP_SET_PROPERTY, s.Name)

	case ClassStmt:
		c.compileClass(s)

	case EnumStmt:
		c.compileEnum(s)

	case FieldStmt:
		return

	default:
		c.fatalError(ErrorInternal, "unknown statement")
	}
}

func (c *Compiler) compileEnum(stmt EnumStmt) {
	if len(stmt.Members) == 0 {
		c.fatalError(ErrorSyntax, "enum %s must have at least one member", stmt.Name)
	}

	obj := ObjectValue{}

	for _, member := range stmt.Members {
		if _, exists := obj[member.Name]; exists {
			c.fatalError(ErrorName, "duplicate enum member %s.%s", stmt.Name, member.Name)
		}

		obj[member.Name] = c.evalConstantExpr(member.Value, "enum member must be constant.")
	}

	c.emit(OP_CONST, obj)

	binding := c.declareVariable(stmt.Name, true)

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     stmt.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Constant: true,
		})
	}
}

func (c *Compiler) storeImportedAlias(name string, constant bool) {
	binding := c.declareVariable(name, constant)

	if c.isCompilingNamespace {
		if c.currentNamespaceVariables == nil {
			c.currentNamespaceVariables = map[string]string{}
		}

		c.currentNamespaceVariables[name] = binding.Name
	}

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     name,
			Slot:     binding.Slot,
			Constant: constant,
		})
		return
	}

	c.emit(OP_STORE_GLOBAL, VariableInfo{
		Name:     binding.Name,
		Constant: constant,
	})
}

func (c *Compiler) resolveImportPath(importPath string) string {
	if filepath.IsAbs(importPath) {
		return importPath
	}

	if c.currentFile != "" {
		baseDir := filepath.Dir(c.currentFile)
		return filepath.Join(baseDir, importPath)
	}

	return importPath
}

func (c *Compiler) compileStdImport(stmt ImportStmt) {
	name := stmt.Alias

	if name == "" {
		name = stmt.Path
	}

	c.emit(OP_CONST, stmt.Path)

	c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
		Object:   "Plugin",
		Method:   "std",
		ArgCount: 1,
	})

	c.storeImportedAlias(name, true)
}

func (c *Compiler) compilePluginImport(stmt ImportStmt) {
	name := stmt.Alias

	if name == "" {
		name = stmt.Path
	}

	resolvedPath := c.resolveImportPath(stmt.Path)
	c.emit(OP_CONST, resolvedPath)

	c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
		Object:   "Plugin",
		Method:   "load",
		ArgCount: 1,
	})

	c.storeImportedAlias(name, true)
}

func (c *Compiler) compileTryCatchStatement(stmt TryCatchStmt) {
	info := TryInfo{
		CatchIP: -1,
		Name:    stmt.ErrorName,
		Slot:    -1,
		IsLocal: c.isInsideFunction(),
	}

	setupIndex := c.emitJump(OP_SETUP_TRY)
	(*c.currentInstructions)[setupIndex].Value = info

	// try body
	c.compileScopedBlock(stmt.TryBody)

	// If try succeeds, remove try handler.
	c.emit(OP_POP_TRY, nil)

	// Normal path should skip catch and go to finally.
	jumpOverCatch := c.emitJump(OP_JUMP)

	// catch starts here
	catchStart := len(*c.currentInstructions)

	c.beginScope()

	binding := c.declareVariable(stmt.ErrorName, false)

	if binding.Kind == BindingLocal {
		info.IsLocal = true
		info.Slot = binding.Slot
	} else {
		info.IsLocal = false
		info.Name = binding.Name
	}

	info.CatchIP = catchStart
	(*c.currentInstructions)[setupIndex].Value = info

	for _, bodyStmt := range stmt.CatchBody {
		c.compileStatement(bodyStmt)
	}

	c.endScope()

	// finally starts here
	// Normal try path jumps here.
	c.patchJump(jumpOverCatch)

	if len(stmt.FinallyBody) > 0 {
		c.compileScopedBlock(stmt.FinallyBody)
	}
}

func (c *Compiler) compileForStatement(stmt ForStmt) {
	c.beginScope()
	defer c.endScope()

	if stmt.Init != nil {
		c.compileStatement(stmt.Init)
	}

	loopStart := len(*c.currentInstructions)

	c.loopStack = append(c.loopStack, LoopContext{
		Start: loopStart,
	})

	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	c.compileScopedBlock(stmt.Body)

	updateStart := len(*c.currentInstructions)

	currentLoop := c.loopStack[len(c.loopStack)-1]

	for _, continueJump := range currentLoop.ContinueJumps {
		(*c.currentInstructions)[continueJump].Value = updateStart
	}

	if stmt.Update != nil {
		c.compileStatement(stmt.Update)
	}

	c.emit(OP_JUMP, loopStart)

	c.patchJump(jumpIfFalseIndex)

	currentLoop = c.loopStack[len(c.loopStack)-1]
	c.loopStack = c.loopStack[:len(c.loopStack)-1]

	for _, breakJump := range currentLoop.BreakJumps {
		c.patchJump(breakJump)
	}
}

func (c *Compiler) compileBreakStatement() {
	if len(c.loopStack) == 0 {
		c.fatalError(ErrorSyntax, "break used outside of loop")
	}

	jumpIndex := c.emitJump(OP_JUMP)

	currentLoop := &c.loopStack[len(c.loopStack)-1]
	currentLoop.BreakJumps = append(currentLoop.BreakJumps, jumpIndex)
}

func (c *Compiler) compileContinueStatement() {
	if len(c.loopStack) == 0 {
		c.fatalError(ErrorSyntax, "continue used outside of loop")
	}

	jumpIndex := c.emitJump(OP_JUMP)

	currentLoop := &c.loopStack[len(c.loopStack)-1]
	currentLoop.ContinueJumps = append(currentLoop.ContinueJumps, jumpIndex)
}

func (c *Compiler) ensureCaptured(name string) (Binding, bool) {
	if binding, exists := c.resolveVariable(name); exists {
		return binding, true
	}

	if c.outerBindings == nil {
		return Binding{}, false
	}

	outer, exists := c.outerBindings[name]
	if !exists {
		return Binding{}, false
	}

	capture, already := c.currentCaptures[name]
	if !already {
		slot := c.localCount
		c.localCount++

		capture = CapturedVar{
			Name:      name,
			OuterSlot: outer.Slot,
			InnerSlot: slot,
		}

		c.currentCaptures[name] = capture

		c.currentScope()[name] = Binding{
			Kind:     BindingLocal,
			Name:     name,
			Slot:     slot,
			Constant: outer.Constant,
		}
	}

	return Binding{
		Kind:     BindingLocal,
		Name:     name,
		Slot:     capture.InnerSlot,
		Constant: outer.Constant,
	}, true
}

func (c *Compiler) evalConstantExpr(expr Expr, err string) Value {
	switch e := expr.(type) {
	case StringExpr:
		return NewNative(e.Value)

	case NumberExpr:
		return NewInt(e.Value)

	case BoolExpr:
		return NewNative(e.Value)

	case NullExpr:
		return NewNull()

	case ArrayExpr:
		arr := &ArrayValue{
			Elements: []Value{},
		}

		for _, element := range e.Elements {
			arr.Elements = append(arr.Elements, c.evalConstantExpr(element, err))
		}

		return NewNative(arr)

	case ObjectExpr:
		obj := ObjectValue{}

		for _, pair := range e.Fields {
			obj[pair.Name] = c.evalConstantExpr(pair.Value, err)
		}

		return NewNative(obj)

	default:
		c.fatalError(
			ErrorType,
			"%s",
			err,
		)
		return NewUndefined()
	}
}

func (c *Compiler) compileClass(stmt ClassStmt) {
	if _, exists := c.classes[stmt.Name]; exists {
		c.fatalError(ErrorName, "class already defined: %s", stmt.Name)
	}

	methods := map[string]string{}
	privateMethods := map[string]bool{}
	fields := []ClassField{}

	for _, method := range stmt.Methods {
		compiledName := stmt.Name + "." + method.Name
		methods[method.Name] = compiledName

		if method.Private {
			privateMethods[method.Name] = true
		}

		classMethod := FunctionStmt{
			Name:    method.Name,
			Params:  method.Params,
			Body:    method.Body,
			Private: method.Private,
			Async:   method.Async,
		}

		c.compileMethod(stmt.Name, classMethod)
	}

	for _, field := range stmt.Fields {
		classField := ClassField{
			Constant: field.Constant,
			Name:     field.Name,
			Value:    c.evalConstantExpr(field.Value, "class field default must be constant."),
			TypeHint: field.TypeHint,
			Private:  field.Private,
		}

		fields = append(fields, classField)

		if !CheckTypeHint(c.evalConstantExpr(field.Value, "class field default must be constant."), field.TypeHint) {
			c.fatalError(
				ErrorType,
				"field %s in class '%s' expected %s, got %s",
				field.Name,
				stmt.Name,
				field.TypeHint.Name,
				TypeName(c.evalConstantExpr(field.Value, "class field default must be constant.")),
			)
		}

		c.compileStatement(field)
	}

	c.classes[stmt.Name] = Class{
		Name:           stmt.Name,
		Methods:        methods,
		Embeds:         stmt.Embeds,
		Fields:         fields,
		PrivateMethods: privateMethods,
	}
}

func (c *Compiler) isTrueLiteral(expr Expr) bool {
	switch e := expr.(type) {
	case BoolExpr:
		return e.Value == true
	}

	return false
}

func (c *Compiler) isFalseLiteral(expr Expr) bool {
	switch e := expr.(type) {
	case BoolExpr:
		return e.Value == false
	}

	return false
}

func (c *Compiler) compileWhileStatement(stmt WhileStmt) {
	// while false { ... } => compile nothing
	if c.isFalseLiteral(stmt.Condition) {
		return
	}

	loopStart := len(*c.currentInstructions)

	isInfinite := c.isTrueLiteral(stmt.Condition)

	c.loopStack = append(c.loopStack, LoopContext{
		Start: loopStart,
	})

	jumpIfFalseIndex := -1

	// Normal while condition.
	// For while true, don't emit condition/jump at all.
	if !isInfinite {
		c.compileExpr(stmt.Condition)
		jumpIfFalseIndex = c.emitJump(OP_JUMP_IF_FALSE)
	}

	c.compileScopedBlock(stmt.Body)

	currentLoop := c.loopStack[len(c.loopStack)-1]

	for _, continueJump := range currentLoop.ContinueJumps {
		(*c.currentInstructions)[continueJump].Value = loopStart
	}

	c.emit(OP_JUMP, loopStart)

	if !isInfinite {
		c.patchJump(jumpIfFalseIndex)
	}

	currentLoop = c.loopStack[len(c.loopStack)-1]
	c.loopStack = c.loopStack[:len(c.loopStack)-1]

	for _, breakJump := range currentLoop.BreakJumps {
		c.patchJump(breakJump)
	}
}

func (c *Compiler) compileLockStmt(stmt LockStmt) {
	// lock mutex
	c.compileExpr(stmt.Mutex)
	c.emit(OP_LOCK_MUTEX, nil)

	// register this mutex as active before compiling the block
	c.activeLocks = append(c.activeLocks, stmt.Mutex)

	// run block
	c.setLocation(stmt.File, stmt.Line, stmt.Column)
	c.compileScopedBlock(stmt.Block)

	// unregister it (pop from stack) since the block is done
	c.activeLocks = c.activeLocks[:len(c.activeLocks)-1]

	// unlock mutex after done
	c.compileExpr(stmt.Mutex)
	c.emit(OP_UNLOCK_MUTEX, nil)
}

func (c *Compiler) compileIfStatement(stmt IfStmt) {
	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	c.compileScopedBlock(stmt.ThenBody)

	if len(stmt.ElseBody) > 0 {
		jumpOverElseIndex := c.emitJump(OP_JUMP)

		c.patchJump(jumpIfFalseIndex)

		c.compileScopedBlock(stmt.ElseBody)

		c.patchJump(jumpOverElseIndex)
	} else {
		c.patchJump(jumpIfFalseIndex)
	}
}

func (c *Compiler) compileFunction(stmt FunctionStmt) {
	if existing, exists := c.functions[stmt.Name]; exists && len(existing.Instructions) > 0 {
		c.fatalError(ErrorName, "function already defined: %s", stmt.Name)
	}

	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[stmt.Name] = Function{
		ID:           c.getFunctionID(stmt.Name),
		Name:         stmt.Name,
		Params:       stmt.Params,
		HasDefaults:  hasDefaults,
		HasTypeHints: hasTypeHints,
		Async:        stmt.Async,
	}

	oldInstructions := c.currentInstructions
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures

	functionInstructions := []Instruction{}

	c.currentInstructions = &functionInstructions
	c.scopes = []map[string]Binding{}
	c.localCount = 0
	c.inMethod = false
	c.outerBindings = nil
	c.currentCaptures = nil

	c.beginScope()

	for _, param := range stmt.Params {
		c.declareVariable(param.Name, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, NewUndefined())
	c.emit(OP_RETURN, nil)

	localCount := c.localCount

	c.functions[stmt.Name] = Function{
		ID:           c.getFunctionID(stmt.Name),
		Name:         stmt.Name,
		Params:       stmt.Params,
		Instructions: functionInstructions,
		LocalCount:   localCount,
		HasDefaults:  hasDefaults,
		HasTypeHints: hasTypeHints,
		Async:        stmt.Async,
	}

	c.currentInstructions = oldInstructions
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures
}

func (c *Compiler) makeAnonymousFunctionName() string {
	name := "__anon_" + strconv.Itoa(c.anonymousFunctionCount)
	c.anonymousFunctionCount++
	return name
}

func (c *Compiler) collectCapturableBindings() map[string]Binding {
	result := map[string]Binding{}

	for _, scope := range c.scopes {
		for name, binding := range scope {
			if binding.Kind == BindingLocal {
				result[name] = binding
			}
		}
	}

	if c.outerBindings != nil {
		for name, binding := range c.outerBindings {
			if binding.Kind == BindingLocal {
				result[name] = binding
			}
		}
	}

	return result
}

func (c *Compiler) tryCompileFastIncrement(name string, value Expr) bool {
	bin, ok := value.(BinaryExpr)
	if !ok {
		return false
	}

	leftIdent, ok := bin.Left.(IdentExpr)
	if !ok || leftIdent.Name != name {
		return false
	}

	intAmount, floatAmount, isFloat, ok := getNumberLiteral(bin.Right)
	if !ok {
		return false
	}

	switch bin.Op {
	case TOKEN_PLUS:
		// keep amount

	case TOKEN_MINUS:
		if isFloat {
			floatAmount = -floatAmount
		} else {
			intAmount = -intAmount
		}

	default:
		return false
	}

	c.emitIncrementForName(name, intAmount, floatAmount, isFloat)
	return true
}

func (c *Compiler) emitIncrementForName(name string, intAmount int, floatAmount float64, isFloat bool) {
	info := IncrementInfo{
		IntAmount:   intAmount,
		FloatAmount: floatAmount,
		IsFloat:     isFloat,
	}

	if binding, exists := c.resolveVariable(name); exists {
		if binding.Kind == BindingLocal {
			info.Slot = binding.Slot
			c.emit(OP_INC_LOCAL, info)
		} else {
			info.Name = binding.Name
			c.emit(OP_INC_GLOBAL, info)
		}
		return
	}

	if binding, exists := c.ensureCaptured(name); exists {
		if binding.Kind == BindingLocal {
			info.Slot = binding.Slot
			c.emit(OP_INC_LOCAL, info)
		} else {
			info.Name = binding.Name
			c.emit(OP_INC_GLOBAL, info)
		}
		return
	}

	if c.currentNamespaceVariables != nil {
		if fullName, exists := c.currentNamespaceVariables[name]; exists {
			info.Name = fullName
			c.emit(OP_INC_GLOBAL, info)
			return
		}
	}

	info.Name = name
	c.emit(OP_INC_GLOBAL, info)
}

func flattenStringConcat(expr Expr, parts *[]Expr) bool {
	bin, ok := expr.(BinaryExpr)
	if !ok || bin.Op != TOKEN_PLUS {
		*parts = append(*parts, expr)
		return isProbablyStringExpr(expr)
	}

	leftStringy := flattenStringConcat(bin.Left, parts)
	rightStringy := flattenStringConcat(bin.Right, parts)

	return leftStringy || rightStringy
}

func isProbablyStringExpr(expr Expr) bool {
	switch expr.(type) {
	case StringExpr, InterpolatedStringExpr:
		return true
	default:
		return false
	}
}

func (c *Compiler) compileExpr(expr Expr) {
	expr = optimizeExpr(expr)

	switch e := expr.(type) {
	case InstanceOfExpr:
		c.compileExpr(e.Object)
		c.compileExpr(e.Class)
		c.emit(OP_INSTANCEOF, nil)

	case ObjectInExpr:
		c.compileExpr(e.Object)
		c.compileExpr(e.Key)
		c.setLocation(e.File, e.Line, e.Column)
		c.emit(OP_OBJECT_IN, nil)

	case TernaryExpr:
		c.compileExpr(e.Condition)

		jumpToElse := c.emitJump(OP_JUMP_IF_FALSE)

		c.compileExpr(e.ThenExpr)

		jumpToEnd := c.emitJump(OP_JUMP)

		c.patchJump(jumpToElse)

		c.compileExpr(e.ElseExpr)

		c.patchJump(jumpToEnd)

	case StringExpr:
		c.emit(OP_CONST, e.Value)

	case UnaryExpr:
		c.compileExpr(e.Right)

		switch e.Op {
		case TOKEN_BANG:
			c.emit(OP_NOT, nil)

		case TOKEN_MINUS:
			c.emit(OP_NEGATE, nil)

		default:
			c.fatalError(ErrorInternal, "unknown unary operator: %s", e.Op)
		}

	case InterpolatedStringExpr:
		textParts := []string{}
		exprCount := 0

		textParts = append(textParts, "")

		for _, part := range e.Parts {
			if part.IsExpr {
				c.compileExpr(part.Expr)
				exprCount++
				textParts = append(textParts, "")
			} else {
				textParts[len(textParts)-1] += part.Text
			}
		}

		c.emit(OP_INTERPOLATE, InterpolateInfo{
			Parts:     textParts,
			ExprCount: exprCount,
		})

	case FunctionExpr:
		name := c.makeAnonymousFunctionName()

		outerBindings := c.collectCapturableBindings()

		oldInstructions := c.currentInstructions
		oldScopes := c.scopes
		oldLocalCount := c.localCount
		oldInMethod := c.inMethod
		oldOuterBindings := c.outerBindings
		oldCurrentCaptures := c.currentCaptures

		functionInstructions := []Instruction{}

		c.currentInstructions = &functionInstructions
		c.scopes = []map[string]Binding{}
		c.localCount = 0
		c.inMethod = false
		c.outerBindings = outerBindings
		c.currentCaptures = map[string]CapturedVar{}

		c.beginScope()

		for _, param := range e.Params {
			c.declareVariable(param.Name, false)
		}

		for _, bodyStmt := range e.Body {
			c.compileStatement(bodyStmt)
		}

		c.emit(OP_CONST, NewUndefined())
		c.emit(OP_RETURN, nil)

		captures := []CapturedVar{}
		for _, capture := range c.currentCaptures {
			captures = append(captures, capture)
		}

		localCount := c.localCount

		hasDefaults, hasTypeHints := getParamFlags(e.Params)

		c.functions[name] = Function{
			ID:           c.getFunctionID(name),
			Name:         name,
			Params:       e.Params,
			ReturnType:   e.ReturnType,
			Instructions: functionInstructions,
			LocalCount:   localCount,
			Captures:     captures,
			HasDefaults:  hasDefaults,
			HasTypeHints: hasTypeHints,
			Async:        false,
		}

		c.currentInstructions = oldInstructions
		c.scopes = oldScopes
		c.localCount = oldLocalCount
		c.inMethod = oldInMethod
		c.outerBindings = oldOuterBindings
		c.currentCaptures = oldCurrentCaptures

		c.emit(OP_CLOSURE, ClosureInfo{
			Name:     name,
			Captures: captures,
		})

	case BoolExpr:
		c.emit(OP_CONST, e.Value)

	case NullExpr:
		c.emit(OP_CONST, NullValue{})

	case UndefinedExpr:
		c.emit(OP_CONST, NewUndefined())

	case ObjectExpr:
		names := make([]ObjectFieldsInfo, len(e.Fields))

		for i, field := range e.Fields {
			names[i] = ObjectFieldsInfo{
				Name: field.Name,
			}

			if field.HasCopy {
				names[i].Copy = true
				c.compileExpr(field.Copy)
			} else {
				c.compileExpr(field.Value)
			}
		}

		c.emit(OP_OBJECT, ObjectInfo{
			Names: names,
		})

	case NullishCoalescingExpr:
		c.compileExpr(e.Left)
		c.compileExpr(e.Right)

		c.emit(OP_COALESCE_JUMP, nil)

	case PropertyExpr:
		c.compileExpr(e.Object)

		if e.Safe {
			c.emit(OP_GET_PROPERTY_SAFE, e.Name)
		} else {
			c.emit(OP_GET_PROPERTY, e.Name)
		}

	case TypeOfExpr:
		c.compileExpr(e.Value)
		c.emit(OP_TYPEOF, nil)

	case SpawnExpr:
		c.compileExpr(e.Function)
		c.emit(OP_SPAWN, nil)

	case DeferExpr:
		c.setLocation(e.File, e.Line, e.Column)
		if !c.isInsideFunction() {
			c.fatalError(ErrorName, "cannot use defer outside of a function")
		}
		c.compileExpr(e.Function)
		c.emit(OP_DEFER, nil)

	case AwaitExpr:
		c.setLocation(e.File, e.Line, e.Column)
		c.compileExpr(e.Task)
		c.emit(OP_AWAIT, nil)

	case ArrayExpr:
		for _, element := range e.Elements {
			c.compileExpr(element)
		}

		c.emit(OP_ARRAY, ArrayInfo{
			Count: len(e.Elements),
		})

	case IndexExpr:
		c.compileExpr(e.Object)
		c.compileExpr(e.Index)
		c.emit(OP_INDEX, nil)

	case NumberExpr:
		c.emit(OP_CONST, e.Value)

	case FloatExpr:
		c.emit(OP_CONST, e.Value)

	case IdentExpr:
		// 1. Normal local/global variable resolution first.
		if binding, exists := c.resolveVariable(e.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, binding.Slot)
			} else {
				c.emit(OP_LOAD_GLOBAL, binding.Name)
			}

			return
		}

		// 2. Namespace symbols.
		if c.currentNamespaceEnums != nil {
			if fullName, exists := c.currentNamespaceEnums[e.Name]; exists {
				c.emit(OP_LOAD_GLOBAL, fullName)
				return
			}
		}

		if c.currentNamespaceClasses != nil {
			if fullName, exists := c.currentNamespaceClasses[e.Name]; exists {
				c.emit(OP_CONST, Class{Name: fullName})
				return
			}
		}

		if c.currentNamespaceFunctions != nil {
			if fullName, exists := c.currentNamespaceFunctions[e.Name]; exists {
				c.emit(OP_CONST, FunctionValue{Name: fullName})
				return
			}
		}

		if c.currentNamespaceVariables != nil {
			if fullName, exists := c.currentNamespaceVariables[e.Name]; exists {
				c.emit(OP_LOAD_GLOBAL, fullName)
				return
			}
		}

		// 3. Only capture REAL outer locals.
		if binding, exists := c.ensureCaptured(e.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, binding.Slot)
				return
			}
		}

		// 4. Known global function.
		if _, exists := c.functions[e.Name]; exists {
			c.emit(OP_CONST, FunctionValue{Name: e.Name})
			return
		}

		// 5. Known global class.
		if _, exists := c.classes[e.Name]; exists {
			c.emit(OP_CONST, Class{Name: e.Name})
			return
		}

		// 6. Namespace files should not see random parent globals.
		if c.isCompilingNamespace {
			LangErrorAt(ErrorName, c.currentFile, c.currentLine, c.currentColumn, "undefined variable in namespace: %s", e.Name)
		}

		if c.declaredFunctions[e.Name] {
			c.emit(OP_LOAD_GLOBAL, e.Name)
			return
		}

		// 4. classes
		if _, ok := c.classes[e.Name]; ok {
			c.emit(OP_LOAD_GLOBAL, e.Name)
			return
		}

		// 5. known global variables/imports
		if _, ok := c.globalConstants[e.Name]; ok {
			c.emit(OP_LOAD_GLOBAL, e.Name)
			return
		}

		LangErrorAt(
			ErrorName,
			e.File,
			e.Line,
			e.Column,
			"undefined variable: %s",
			e.Name,
		)

		// 7. Fallback global
		c.emit(OP_LOAD_GLOBAL, e.Name)
		return

	case BinaryExpr:
		if e.Op == TOKEN_PLUS {
			parts := []Expr{}
			hasString := flattenStringConcat(e, &parts)

			if hasString && len(parts) >= 3 {
				for _, part := range parts {
					c.compileExpr(part)
				}

				c.emit(OP_STRING_JOIN, len(parts))
				return
			}
		}

		c.compileExpr(e.Left)
		c.compileExpr(e.Right)

		switch e.Op {
		case TOKEN_PLUS:
			c.emit(OP_ADD, nil)
		case TOKEN_MINUS:
			c.emit(OP_SUB, nil)
		case TOKEN_STAR:
			c.emit(OP_MUL, nil)
		case TOKEN_SLASH:
			c.emit(OP_DIV, nil)

		case TOKEN_EQ:
			c.emit(OP_EQ, nil)
		case TOKEN_NEQ:
			c.emit(OP_NEQ, nil)
		case TOKEN_LT:
			c.emit(OP_LT, nil)
		case TOKEN_GT:
			c.emit(OP_GT, nil)
		case TOKEN_LTE:
			c.emit(OP_LTE, nil)
		case TOKEN_GTE:
			c.emit(OP_GTE, nil)
		case TOKEN_AND:
			c.emit(OP_AND, nil)
		case TOKEN_OR:
			c.emit(OP_OR, nil)
		case TOKEN_PERCENT:
			c.emit(OP_MOD, nil)

		default:
			c.fatalError(ErrorInternal, "unknown binary operator")
		}

	case CallExpr:
		if _, exists := c.classes[e.Name]; exists {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_CALL, CallInfo{
				Name:     e.Name,
				ArgCount: len(e.Args),
			})

			return
		}

		if c.declaredFunctions[e.Name] {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_CALL_DIRECT, DirectCallInfo{
				ID:       c.getFunctionID(e.Name),
				Name:     e.Name,
				ArgCount: len(e.Args),
			})

			return
		}

		if c.currentNamespaceFunctions != nil {
			if fullName, exists := c.currentNamespaceFunctions[e.Name]; exists {
				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(e.File, e.Line, e.Column)

				c.emit(OP_CALL_DIRECT, DirectCallInfo{
					ID:       c.getFunctionID(fullName),
					Name:     fullName,
					ArgCount: len(e.Args),
				})

				return
			}
		}

		c.compileExpr(IdentExpr{
			Name:   e.Name,
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
		})

		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.setLocation(e.File, e.Line, e.Column)

		c.emit(OP_CALL, CallInfo{
			ArgCount: len(e.Args),
		})

	case CallValueExpr:
		if ident, ok := e.Callee.(IdentExpr); ok {
			if c.currentNamespaceClasses != nil {
				if fullName, exists := c.currentNamespaceClasses[ident.Name]; exists {
					for _, arg := range e.Args {
						c.compileExpr(arg)
					}

					c.setLocation(ident.File, ident.Line, ident.Column)

					c.emit(OP_CALL, CallInfo{
						Name:     fullName,
						ArgCount: len(e.Args),
					})

					return
				}
			}

			if c.currentNamespaceFunctions != nil {
				if fullName, exists := c.currentNamespaceFunctions[ident.Name]; exists {
					for _, arg := range e.Args {
						c.compileExpr(arg)
					}

					c.setLocation(ident.File, ident.Line, ident.Column)

					c.emit(OP_CALL_DIRECT, DirectCallInfo{
						ID:       c.getFunctionID(fullName),
						Name:     fullName,
						ArgCount: len(e.Args),
					})

					return
				}
			}

			if _, exists := c.functions[ident.Name]; exists {
				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(ident.File, ident.Line, ident.Column)
				c.emit(OP_CALL_DIRECT, DirectCallInfo{
					ID:       c.getFunctionID(ident.Name),
					Name:     ident.Name,
					ArgCount: len(e.Args),
				})

				return
			}

			if _, exists := c.classes[ident.Name]; exists {
				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(ident.File, ident.Line, ident.Column)

				c.emit(OP_CALL, CallInfo{
					Name:     ident.Name,
					ArgCount: len(e.Args),
				})

				return
			}
		}

		c.compileExpr(e.Callee)

		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.setLocation(e.File, e.Line, e.Column)

		c.emit(OP_CALL_VALUE, CallInfo{
			ArgCount: len(e.Args),
		})

	case MemberCallExpr:
		if ident, ok := e.Object.(IdentExpr); ok && (ident.Name == "Plugin") {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
				Object:   ident.Name,
				Method:   e.Method,
				ArgCount: len(e.Args),
			})

			return
		}

		c.compileExpr(e.Object)

		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.setLocation(e.File, e.Line, e.Column)

		if e.Safe {
			c.emit(OP_METHOD_CALL_SAFE, MethodCallInfo{
				Method:   e.Method,
				ArgCount: len(e.Args),
			})
		} else {
			c.emit(OP_METHOD_CALL, MethodCallInfo{
				Method:   e.Method,
				ArgCount: len(e.Args),
			})
		}

	case ThisExpr:
		if binding, exists := c.resolveVariable("this"); exists {
			c.emit(OP_LOAD_LOCAL, binding.Slot)
			return
		}

		if binding, exists := c.ensureCaptured("this"); exists {
			c.emit(OP_LOAD_LOCAL, binding.Slot)
			return
		}

		c.fatalError(ErrorName, "cannot use this outside of a method")

	default:
		c.fatalError(ErrorInternal, "unknown expression, %T", expr)
	}
}

func (c *Compiler) isCompilingMain() bool {
	return c.currentInstructions == &c.mainInstructions
}

func (c *Compiler) compileMethod(className string, stmt FunctionStmt) {
	name := className + "." + stmt.Name

	oldInstructions := c.currentInstructions
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures

	functionInstructions := []Instruction{}

	c.currentInstructions = &functionInstructions

	globalScope := map[string]Binding{}
	if len(oldScopes) > 0 {
		maps.Copy(globalScope, oldScopes[0])
	}

	c.scopes = []map[string]Binding{globalScope}
	c.localCount = 0
	c.inMethod = true
	c.outerBindings = nil
	c.currentCaptures = nil

	c.beginScope()

	// slot 0 = this
	c.declareVariable("this", false)

	// slot 1+ = real user parameters
	for _, param := range stmt.Params {
		if param.Name == "this" {
			continue
		}

		c.declareVariable(param.Name, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, NewUndefined())
	c.emit(OP_RETURN, nil)

	params := make([]Param, 0, len(stmt.Params)+1)

	params = append(params, Param{
		Name: "this",
	})

	params = append(params, stmt.Params...)

	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[name] = Function{
		ID:           c.getFunctionID(name),
		Name:         name,
		Params:       params,
		Instructions: functionInstructions,
		LocalCount:   c.localCount,
		HasDefaults:  hasDefaults,
		HasTypeHints: hasTypeHints,
		Async:        stmt.Async,
	}

	c.currentInstructions = oldInstructions
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures
}

func (c *Compiler) emit(op OpCode, value any) {
	intVal := 0
	hasInt := false
	if v, ok := value.(int); ok {
		intVal = v
		hasInt = true
	}
	*c.currentInstructions = append(*c.currentInstructions, Instruction{
		Op:     op,
		Value:  value,
		IntArg: intVal,
		IsInt:  hasInt,
		File:   c.currentFile,
		Line:   c.currentLine,
		Column: c.currentColumn,
	})
}

func (c *Compiler) isInsideFunction() bool {
	return c.currentInstructions != &c.mainInstructions
}

func (c *Compiler) emitJump(op OpCode) int {
	c.emit(op, -1)
	return len(*c.currentInstructions) - 1
}

func (c *Compiler) patchJump(index int) {
	target := len(*c.currentInstructions)
	(*c.currentInstructions)[index].Value = target
	(*c.currentInstructions)[index].IntArg = target
}
