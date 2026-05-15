package main

import (
	"strconv"
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

type LocalInfo struct {
	Slot     int
	Constant bool
}

type Compiler struct {
	mainInstructions       []Instruction
	functions              map[string]Function
	classes                map[string]Class
	loopStack              []LoopContext
	anonymousFunctionCount int

	currentNamespaceVariables map[string]string
	currentNamespaceClasses   map[string]string
	currentNamespaceFunctions map[string]string

	inMethod bool

	outerBindings   map[string]Binding
	currentCaptures map[string]CapturedVar

	parent   *Compiler
	captured map[string]CapturedVar

	outerScopes []map[string]Binding

	currentInstructions *[]Instruction

	scopes  []map[string]Binding
	scopeID int

	locals          map[string]LocalInfo
	localCount      int
	globalConstants map[string]bool
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
				return NumberExpr{Value: leftInt.Value + rightInt.Value}
			case TOKEN_MINUS:
				return NumberExpr{Value: leftInt.Value - rightInt.Value}
			case TOKEN_STAR:
				return NumberExpr{Value: leftInt.Value * rightInt.Value}
			case TOKEN_SLASH:
				if rightInt.Value == 0 {
					return BinaryExpr{Left: left, Op: e.Op, Right: right}
				}

				return NumberExpr{Value: leftInt.Value / rightInt.Value}
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

	case CallValueExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = optimizeExpr(arg)
		}

		return CallValueExpr{
			Callee: optimizeExpr(e.Callee),
			Args:   args,
		}

	case ObjectExpr:
		fields := make([]ObjectField, len(e.Fields))

		for i, field := range e.Fields {
			fields[i] = ObjectField{
				Name:  field.Name,
				Value: optimizeExpr(field.Value),
			}
		}

		return ObjectExpr{Fields: fields}

	case CallExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = optimizeExpr(arg)
		}

		return CallExpr{
			Name: e.Name,
			Args: args,
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
		}

	case PropertyExpr:
		return PropertyExpr{
			Object: optimizeExpr(e.Object),
			Name:   e.Name,
		}

	case UnaryExpr:
		right := optimizeExpr(e.Right)

		if e.Op == TOKEN_BANG {
			if boolExpr, ok := right.(BoolExpr); ok {
				return BoolExpr{Value: !boolExpr.Value}
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
		locals:                 map[string]LocalInfo{},
		localCount:             0,
		globalConstants:        map[string]bool{},
		anonymousFunctionCount: 0,
		scopes:                 []map[string]Binding{},
	}

	c.currentInstructions = &c.mainInstructions
	c.beginScope()

	return c
}

// func (c *Compiler) compileAnonymousFunction(expr FunctionExpr) FunctionValue {
// 	// later
// }

func (c *Compiler) collectCurrentLocalBindings() map[string]Binding {
	result := map[string]Binding{}

	for _, scope := range c.scopes {
		for name, binding := range scope {
			if binding.Kind == BindingLocal {
				result[name] = binding
			}
		}
	}

	return result
}

func (c *Compiler) beginScope() {
	c.scopes = append(c.scopes, map[string]Binding{})
}

func (c *Compiler) endScope() {
	if len(c.scopes) == 0 {
		langError(ErrorInternal, "scope stack underflow")
	}

	c.scopes = c.scopes[:len(c.scopes)-1]
}

func (c *Compiler) currentScope() map[string]Binding {
	if len(c.scopes) == 0 {
		c.beginScope()
	}

	return c.scopes[len(c.scopes)-1]
}

func (c *Compiler) compileNestedFunction(stmt FunctionStmt) {
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
		c.declareVariable(param, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, UndefinedValue{})
	c.emit(OP_RETURN, nil)

	captures := []CapturedVar{}

	for _, capture := range c.currentCaptures {
		captures = append(captures, capture)
	}

	localCount := c.localCount

	c.functions[compiledName] = Function{
		Name:         compiledName,
		Params:       stmt.Params,
		Instructions: functionInstructions,
		LocalCount:   localCount,
		Captures:     captures,
	}

	c.currentInstructions = oldInstructions
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures

	// Create closure value.
	c.emit(OP_CLOSURE, ClosureInfo{
		Name:     compiledName,
		Captures: captures,
	})

	// Store it as local const function name.
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
		langError(ErrorName, "variable already declared in this scope: %s", name)
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
	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	return c.mainInstructions, c.functions, c.classes
}

func (c *Compiler) compileNamespace(stmt NamespaceStmt) {
	oldNamespaceFunctions := c.currentNamespaceFunctions
	oldNamespaceVariables := c.currentNamespaceVariables
	oldNamespaceClasses := c.currentNamespaceClasses

	namespaceFunctions := map[string]string{}
	namespaceClasses := map[string]string{}
	namespaceVariables := map[string]string{}
	members := map[string]Value{}

	// 1. Compile nested namespaces first.
	for _, raw := range stmt.Statements {
		ns, ok := raw.(NamespaceStmt)
		if !ok {
			continue
		}

		c.compileNamespace(ns)

		// expose nested namespace as a member too
		members[ns.Name] = NamespaceMemberRef{
			GlobalName: ns.Name,
		}
	}

	// 2. Collect functions.
	for _, raw := range stmt.Statements {
		fn, ok := raw.(FunctionStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + fn.Name
		namespaceFunctions[fn.Name] = fullName
		members[fn.Name] = FunctionValue{Name: fullName}

		c.functions[fullName] = Function{
			Name:   fullName,
			Params: fn.Params,
		}
	}

	// 3. Collect variables.
	for _, raw := range stmt.Statements {
		v, ok := raw.(VariableStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + v.Name
		namespaceVariables[v.Name] = fullName

		members[v.Name] = NamespaceMemberRef{
			GlobalName: fullName,
		}
	}

	c.currentNamespaceFunctions = namespaceFunctions
	c.currentNamespaceVariables = namespaceVariables
	c.currentNamespaceClasses = namespaceClasses

	// 4. Compile variables as hidden globals.
	for _, raw := range stmt.Statements {
		v, ok := raw.(VariableStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + v.Name

		c.compileExpr(v.Value)

		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: v.Constant,
		})

		c.globalConstants[fullName] = v.Constant
	}

	// Collect classes.
	for _, raw := range stmt.Statements {
		classStmt, ok := raw.(ClassStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + classStmt.Name
		namespaceClasses[classStmt.Name] = fullName
		members[classStmt.Name] = Class{Name: fullName}
	}

	// 5. Compile functions.
	for _, raw := range stmt.Statements {
		fn, ok := raw.(FunctionStmt)
		if !ok {
			continue
		}

		fullName := stmt.Name + "." + fn.Name

		namespacedFn := FunctionStmt{
			Name:   fullName,
			Params: fn.Params,
			Body:   fn.Body,
		}

		c.compileFunction(namespacedFn)
	}

	// Compile classes with namespaced names.
	for _, raw := range stmt.Statements {
		classStmt, ok := raw.(ClassStmt)
		if !ok {
			continue
		}

		namespacedClass := ClassStmt{
			Name:    stmt.Name + "." + classStmt.Name,
			Methods: classStmt.Methods,
		}

		c.compileClass(namespacedClass)
	}

	c.currentNamespaceFunctions = oldNamespaceFunctions
	c.currentNamespaceVariables = oldNamespaceVariables
	c.currentNamespaceClasses = oldNamespaceClasses

	// 6. Create namespace object.
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

func (c *Compiler) compileStatement(stmt Stmt) {
	switch s := stmt.(type) {
	case NamespaceStmt:
		c.compileNamespace(s)
	case VariableStmt:
		c.compileExpr(s.Value)

		binding := c.declareVariable(s.Name, s.Constant)

		if binding.Kind == BindingLocal {
			c.emit(OP_STORE_LOCAL, VariableInfo{
				Name:     s.Name,
				Slot:     binding.Slot,
				Constant: s.Constant,
			})
		} else {
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:     binding.Name,
				Constant: s.Constant,
			})
		}

	case AssignStmt:
		c.compileExpr(s.Value)

		if binding, exists := c.resolveVariable(s.Name); exists {
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

		c.emit(OP_ASSIGN_GLOBAL, s.Name)

	case IndexAssignStmt:
		c.compileExpr(s.Object)
		c.compileExpr(s.Index)
		c.compileExpr(s.Value)
		c.emit(OP_SET_INDEX, nil)

	case ThrowStmt:
		c.compileExpr(s.Value)
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

	case ReturnStmt:
		if s.HasValue {
			c.compileExpr(s.Value)
		} else {
			c.emit(OP_CONST, UndefinedValue{})
		}

		c.emit(OP_RETURN, nil)

	case ImportStmt:
		if s.Std {
			c.compileStdImport(s)
			return
		}

		langError(ErrorInternal, "imports should be resolved before compiling")

	case IfStmt:
		c.compileIfStatement(s)

	case WhileStmt:
		c.compileWhileStatement(s)

	case ForStmt:
		c.compileForStatement(s)

	case PropertyAssignStmt:
		c.compileExpr(s.Object)
		c.compileExpr(s.Value)
		c.emit(OP_SET_PROPERTY, s.Name)

	case ClassStmt:
		c.compileClass(s)

	default:
		langError(ErrorInternal, "unknown statement")
	}
}

func (c *Compiler) compileStdImport(stmt ImportStmt) {
	name := stmt.Alias

	if name == "" {
		name = stmt.Path
	}

	// Same as:
	// const name = Plugin.std("module");
	c.emit(OP_CONST, stmt.Path)

	c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
		Object:   "Plugin",
		Method:   "std",
		ArgCount: 1,
	})

	binding := c.declareVariable(name, true)

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     name,
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

func (c *Compiler) compileTryCatchStatement(stmt TryCatchStmt) {
	info := TryInfo{
		CatchIP: -1,
		Name:    stmt.ErrorName,
		Slot:    -1,
		IsLocal: c.isInsideFunction(),
	}

	setupIndex := c.emitJump(OP_SETUP_TRY)
	(*c.currentInstructions)[setupIndex].Value = info

	c.compileScopedBlock(stmt.TryBody)

	c.emit(OP_POP_TRY, nil)

	jumpOverCatch := c.emitJump(OP_JUMP)

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

	c.patchJump(jumpOverCatch)
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
		langError(ErrorSyntax, "break used outside of loop")
	}

	jumpIndex := c.emitJump(OP_JUMP)

	currentLoop := &c.loopStack[len(c.loopStack)-1]
	currentLoop.BreakJumps = append(currentLoop.BreakJumps, jumpIndex)
}

func (c *Compiler) compileContinueStatement() {
	if len(c.loopStack) == 0 {
		langError(ErrorSyntax, "continue used outside of loop")
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

func (c *Compiler) compileClass(stmt ClassStmt) {

	if _, exists := c.classes[stmt.Name]; exists {
		langError(ErrorName, "class already defined: %s", stmt.Name)
	}

	methods := map[string]string{}

	for _, method := range stmt.Methods {
		compiledName := stmt.Name + "." + method.Name
		methods[method.Name] = compiledName

		classMethod := FunctionStmt{
			Name:   method.Name,
			Params: method.Params,
			Body:   method.Body,
		}

		c.compileMethod(stmt.Name, classMethod)
	}

	c.classes[stmt.Name] = Class{
		Name:    stmt.Name,
		Methods: methods,
	}
}

func (c *Compiler) compileWhileStatement(stmt WhileStmt) {
	loopStart := len(*c.currentInstructions)

	c.loopStack = append(c.loopStack, LoopContext{
		Start: loopStart,
	})

	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	c.compileScopedBlock(stmt.Body)

	currentLoop := c.loopStack[len(c.loopStack)-1]

	for _, continueJump := range currentLoop.ContinueJumps {
		(*c.currentInstructions)[continueJump].Value = loopStart
	}

	c.emit(OP_JUMP, loopStart)

	c.patchJump(jumpIfFalseIndex)

	currentLoop = c.loopStack[len(c.loopStack)-1]
	c.loopStack = c.loopStack[:len(c.loopStack)-1]

	for _, breakJump := range currentLoop.BreakJumps {
		c.patchJump(breakJump)
	}
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
		langError(ErrorName, "function already defined: %s", stmt.Name)
	}

	// Predeclare function so recursion works.
	c.functions[stmt.Name] = Function{
		Name:   stmt.Name,
		Params: stmt.Params,
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
		c.declareVariable(param, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, UndefinedValue{})
	c.emit(OP_RETURN, nil)

	localCount := c.localCount

	c.functions[stmt.Name] = Function{
		Name:         stmt.Name,
		Params:       stmt.Params,
		Instructions: functionInstructions,
		LocalCount:   localCount,
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
		for name := range c.outerBindings {
			binding, ok := c.ensureCaptured(name)
			if ok {
				result[name] = binding
			}
		}
	}

	return result
}

func (c *Compiler) compileExpr(expr Expr) {
	expr = optimizeExpr(expr)

	switch e := expr.(type) {
	case StringExpr:
		c.emit(OP_CONST, e.Value)

	case UnaryExpr:
		c.compileExpr(e.Right)

		switch e.Op {
		case TOKEN_BANG:
			c.emit(OP_NOT, nil)

		default:
			langError(ErrorInternal, "unknown unary operator: %s", e.Op)
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
			c.declareVariable(param, false)
		}

		for _, bodyStmt := range e.Body {
			c.compileStatement(bodyStmt)
		}

		c.emit(OP_CONST, UndefinedValue{})
		c.emit(OP_RETURN, nil)

		captures := []CapturedVar{}
		for _, capture := range c.currentCaptures {
			captures = append(captures, capture)
		}

		localCount := c.localCount

		c.functions[name] = Function{
			Name:         name,
			Params:       e.Params,
			Instructions: functionInstructions,
			LocalCount:   localCount,
			Captures:     captures,
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
		c.emit(OP_CONST, UndefinedValue{})

	case ObjectExpr:
		names := make([]string, len(e.Fields))

		for i, field := range e.Fields {
			names[i] = field.Name
			c.compileExpr(field.Value)
		}

		c.emit(OP_OBJECT, ObjectInfo{
			Names: names,
		})

	case PropertyExpr:
		c.compileExpr(e.Object)
		c.emit(OP_GET_PROPERTY, e.Name)

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
		if c.currentNamespaceClasses != nil {
			if fullName, exists := c.currentNamespaceClasses[e.Name]; exists {
				c.emit(OP_CONST, Class{Name: fullName})
				return
			}
		}

		if binding, exists := c.resolveVariable(e.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, binding.Slot)
			} else {
				c.emit(OP_LOAD_GLOBAL, binding.Name)
			}

			return
		}

		if binding, exists := c.ensureCaptured(e.Name); exists {
			c.emit(OP_LOAD_LOCAL, binding.Slot)
			return
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

		if _, exists := c.functions[e.Name]; exists {
			c.emit(OP_CONST, FunctionValue{Name: e.Name})
			return
		}

		c.emit(OP_LOAD_GLOBAL, e.Name)

	case BinaryExpr:
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
			langError(ErrorInternal, "unknown binary operator")
		}

	case CallExpr:
		if _, exists := c.functions[e.Name]; exists {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.emit(OP_CALL, CallInfo{
				Name:     e.Name,
				ArgCount: len(e.Args),
			})

			return
		}

		if _, exists := c.classes[e.Name]; exists {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.emit(OP_CALL, CallInfo{
				Name:     e.Name,
				ArgCount: len(e.Args),
			})

			return
		}

		// Otherwise treat it as a function value variable.
		c.compileExpr(IdentExpr{Name: e.Name})

		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

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

					c.emit(OP_CALL, CallInfo{
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

				c.emit(OP_CALL, CallInfo{
					Name:     ident.Name,
					ArgCount: len(e.Args),
				})

				return
			}

			if _, exists := c.classes[ident.Name]; exists {
				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

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

		c.emit(OP_CALL_VALUE, CallInfo{
			ArgCount: len(e.Args),
		})

	case MemberCallExpr:
		if ident, ok := e.Object.(IdentExpr); ok && (ident.Name == "Core" || ident.Name == "Plugin") {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

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

		c.emit(OP_METHOD_CALL, MethodCallInfo{
			Method:   e.Method,
			ArgCount: len(e.Args),
		})

	case ThisExpr:
		if binding, exists := c.resolveVariable("this"); exists {
			c.emit(OP_LOAD_LOCAL, binding.Slot)
			return
		}

		if binding, exists := c.ensureCaptured("this"); exists {
			c.emit(OP_LOAD_LOCAL, binding.Slot)
			return
		}

		langError(ErrorName, "cannot use this outside of a method")

	default:
		langError(ErrorInternal, "unknown expression")
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
	c.scopes = []map[string]Binding{}
	c.localCount = 0
	c.inMethod = true
	c.outerBindings = nil
	c.currentCaptures = nil

	c.beginScope()

	// slot 0 = this
	c.declareVariable("this", false)

	// slot 1+ = real user parameters
	for _, param := range stmt.Params {
		if param == "this" {
			continue
		}

		c.declareVariable(param, false)
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, UndefinedValue{})
	c.emit(OP_RETURN, nil)

	params := []string{"this"}

	for _, param := range stmt.Params {
		if param == "this" {
			continue
		}

		params = append(params, param)
	}

	c.functions[name] = Function{
		Name:         name,
		Params:       params,
		Instructions: functionInstructions,
		LocalCount:   c.localCount,
	}

	c.currentInstructions = oldInstructions
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures
}

func (c *Compiler) emit(op OpCode, value any) {
	*c.currentInstructions = append(*c.currentInstructions, Instruction{
		Op:    op,
		Value: value,
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
	(*c.currentInstructions)[index].Value = len(*c.currentInstructions)
}
