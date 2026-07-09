package compiler

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

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

type VarNodeKey struct {
	File   string
	Line   int
	Column int
}

type Binding struct {
	Kind          BindingKind
	Name          string
	Slot          int
	Constant      bool
	TypeHint      string
	VirtualFields map[string]int
}

type LoopContext struct {
	Start         int
	BreakJumps    []int
	ContinueJumps []int
}

type Compiler struct {
	mainInstructions       []Instruction
	mainDebugInfo          []DebugInfo
	functions              map[string]Function
	nativeFunctions        map[string]string
	externalFunctions      map[string]Function
	interfaces             map[string]Interface
	classes                map[string]Class
	usedFunctions          map[string]bool
	preserveAllFunctions   bool
	loopStack              []LoopContext
	anonymousFunctionCount int
	declaredFunctions      map[string]bool

	activeLocks []Expr

	functionIDs    map[string]int
	nextFunctionID int

	importStates map[string]ImportState
	importStack  []string

	stdImportModules map[string]string

	isCompilingNamespace bool
	currentNamespaceName string

	currentFile   string
	currentLine   int
	currentColumn int

	matchTempID int

	currentNamespaceVariables  map[string]string
	currentNamespaceClasses    map[string]string
	currentNamespaceFunctions  map[string]string
	namespacePrivateMembers    map[string]bool
	currentNamespaceEnums      map[string]string
	currentNamespaceInterfaces map[string]string
	currentTypeImportAliases   map[string]string

	currentReturnType   TypeHint
	currentFunctionName string

	inMethod bool

	outerBindings   map[string]Binding
	currentCaptures map[string]CapturedVar

	parent   *Compiler
	captured map[string]CapturedVar

	outerScopes []map[string]Binding

	currentInstructions *[]Instruction
	currentDebugInfo    *[]DebugInfo

	scopes  []map[string]Binding
	scopeID int

	localCount       int
	globalIndexes    map[string]int
	globalConstants  map[string]bool
	activeTypeParams []string

	virtualObjects map[VarNodeKey]map[string]int

	inlineCandidates map[string]FunctionStmt
	inlineDepth      int

	enumVariants map[string]map[string][]Param
	enumConstants map[string]TinyValue

	jitRegionCount int

	diagnosticMode bool
}

type SemanticModel struct {
	Functions  map[string]Function
	Classes    map[string]Class
	Interfaces map[string]Interface
	Globals    map[string]string
	AST        []Stmt
	Errors     []LangErrorType
	compiler   *Compiler
}

func (m *SemanticModel) InferType(expr Expr) string {
	if m.compiler == nil {
		return "any"
	}
	return m.compiler.inferCompileTimeType(expr)
}

func (m *SemanticModel) ResolveVariable(name string) (Binding, bool) {
	if m.compiler == nil {
		return Binding{}, false
	}
	return m.compiler.resolveVariable(name)
}

type MemberInfo struct {
	Name       string
	Type       string
	Kind       string
	Private    bool
	Constant   bool
	Params     []Param
	ReturnType TypeHint
	Async      bool
}

func (m *SemanticModel) MemberType(ownerType string, memberName string) (MemberInfo, bool) {
	if m == nil {
		return MemberInfo{}, false
	}

	baseType := ownerType
	isArray := false
	if strings.HasPrefix(baseType, "array<") && strings.HasSuffix(baseType, ">") {
		baseType = strings.TrimPrefix(strings.TrimSuffix(baseType, ">"), "array<")
		isArray = true
	}

	if isArray {
		switch memberName {
		case "push", "pop", "shift", "unshift", "splice", "slice", "indexOf", "includes", "reverse", "sort":
			return MemberInfo{Name: memberName, Type: "any", Kind: "method"}, true
		case "length":
			return MemberInfo{Name: "length", Type: "number", Kind: "property"}, true
		}
	}

	switch baseType {
	case "string":
		switch memberName {
		case "length":
			return MemberInfo{Name: "length", Type: "number", Kind: "property"}, true
		case "toUpperCase", "toLowerCase", "trim", "trimStart", "trimEnd":
			return MemberInfo{Name: memberName, Type: "string", Kind: "method"}, true
		case "indexOf", "lastIndexOf", "includes", "startsWith", "endsWith", "search":
			return MemberInfo{Name: memberName, Type: "number", Kind: "method"}, true
		case "replace", "replaceAll", "substring", "slice", "padStart", "padEnd", "repeat", "split", "charAt":
			return MemberInfo{Name: memberName, Type: "string", Kind: "method"}, true
		case "charCodeAt", "codePointAt":
			return MemberInfo{Name: memberName, Type: "number", Kind: "method"}, true
		}
	case "number":
		switch memberName {
		case "toString":
			return MemberInfo{Name: "toString", Type: "string", Kind: "method"}, true
		case "toFixed", "toPrecision", "toExponential":
			return MemberInfo{Name: memberName, Type: "string", Kind: "method"}, true
		}
	case "bool":
		switch memberName {
		case "toString":
			return MemberInfo{Name: "toString", Type: "string", Kind: "method"}, true
		}
	}

	if cls, exists := m.Classes[ownerType]; exists {
		for _, field := range cls.Fields {
			if field.Name == memberName {
				return MemberInfo{
					Name:     field.Name,
					Type:     field.TypeHint.Name,
					Kind:     "property",
					Private:  field.Private,
					Constant: field.Constant,
				}, true
			}
		}
		if sig, exists := cls.MethodSignatures[memberName]; exists {
			return MemberInfo{
				Name:       memberName,
				Type:       sig.ReturnType.Name,
				Kind:       "method",
				Params:     sig.Params,
				ReturnType: sig.ReturnType,
				Async:      sig.Async,
			}, true
		}
	}

	if iface, exists := m.Interfaces[ownerType]; exists {
		if fieldType, exists := iface.Fields[memberName]; exists {
			return MemberInfo{
				Name: memberName,
				Type: fieldType.Name,
				Kind: "property",
			}, true
		}
	}

	for _, ifaceName := range m.Interfaces {
		_ = ifaceName
	}

	return MemberInfo{}, false
}

func (m *SemanticModel) GetFunctionSignature(name string) (Function, bool) {
	if m == nil {
		return Function{}, false
	}
	fn, exists := m.Functions[name]
	return fn, exists
}

func (m *SemanticModel) GetClass(name string) (Class, bool) {
	if m == nil {
		return Class{}, false
	}
	cls, exists := m.Classes[name]
	return cls, exists
}

func (m *SemanticModel) GetInterface(name string) (Interface, bool) {
	if m == nil {
		return Interface{}, false
	}
	iface, exists := m.Interfaces[name]
	return iface, exists
}

func unwrapExport(stmt Stmt) (Stmt, bool) {
	if exp, ok := stmt.(ExportStmt); ok {
		return exp.Inner, true
	}

	return stmt, false
}

func collectDestructuredNames(pattern DestructurePattern) []string {
	switch p := pattern.(type) {
	case ObjectDestructurePattern:
		names := []string{}
		for _, field := range p.Fields {
			if field.HasNested {
				names = append(names, collectDestructuredNames(field.Pattern)...)
			} else if field.AliasIsRenamed {
				names = append(names, field.Alias)
			} else {
				names = append(names, field.Key)
			}
		}
		if p.HasSpread {
			names = append(names, p.Spread)
		}
		return names
	case ArrayDestructurePattern:
		names := []string{}
		for _, elem := range p.Elements {
			if elem.HasNested {
				names = append(names, collectDestructuredNames(elem.Pattern)...)
			} else {
				names = append(names, elem.Name)
			}
		}
		return names
	default:
		return nil
	}
}

func getNumberLiteral(expr Expr) (int, float64, bool, bool) {
	num, ok := expr.(NumberExpr)
	if !ok {
		return 0, 0, false, false
	}

	return num.Value, 0, false, true
}

func (c *Compiler) optimizeExpr(expr Expr) Expr {
	switch e := expr.(type) {
	case BinaryExpr:
		left := c.optimizeExpr(e.Left)
		right := c.optimizeExpr(e.Right)

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
				return FloatExpr{Value: float64(leftInt.Value) / float64(rightInt.Value), File: leftInt.File, Column: leftInt.Column, Line: leftInt.Line}
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
			elements[i] = c.optimizeExpr(element)
		}

		return ArrayExpr{Elements: elements}

	case TernaryExpr:
		condition := c.optimizeExpr(e.Condition)
		thenExpr := c.optimizeExpr(e.ThenExpr)
		elseExpr := c.optimizeExpr(e.ElseExpr)

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
			args[i] = c.optimizeExpr(arg)
		}

		return CallValueExpr{
			Callee: c.optimizeExpr(e.Callee),
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
					Value: c.optimizeExpr(field.Value),
				}
			}
		}

		return ObjectExpr{Fields: fields}

	case CallExpr:
		args := make([]Expr, len(e.Args))

		for i, arg := range e.Args {
			args[i] = c.optimizeExpr(arg)
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

		numLiterals := true

		getFloat := func(expr Expr) float64 {
			if e, ok := expr.(NumberExpr); ok {
				return float64(e.Value)
			} else if e, ok := expr.(FloatExpr); ok {
				return e.Value
			}

			return 0
		}

		for i, arg := range e.Args {
			args[i] = c.optimizeExpr(arg)
			numLiteral := false
			if _, ok := args[i].(NumberExpr); ok {
				numLiteral = true
			} else if _, ok := args[i].(FloatExpr); ok {
				numLiteral = true
			}

			if numLiterals && !numLiteral {
				numLiterals = false
			}
		}

		if numLiterals && !e.Safe && len(e.Args) > 0 {
			module, ok := c.resolveStdImportModuleName(e.Object)
			if ok && module == "math" {
				switch e.Method {
				case "floor":
					if len(e.Args) == 1 {
						return FloatExpr{Value: math.Floor(getFloat(e.Args[0]))}
					}
				case "ceil":
					if len(e.Args) == 1 {
						return FloatExpr{Value: math.Ceil(getFloat(e.Args[0]))}
					}
				case "sqrt":
					if len(e.Args) == 1 {
						return FloatExpr{Value: math.Sqrt(getFloat(e.Args[0]))}
					}
				case "abs":
					if len(e.Args) == 1 {
						return FloatExpr{Value: math.Abs(getFloat(e.Args[0]))}
					}
				case "pow":
					if len(e.Args) == 2 {
						return FloatExpr{Value: math.Pow(getFloat(e.Args[0]), getFloat(e.Args[1]))}
					}
				}
			}
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
			Object: c.optimizeExpr(e.Object),
			Name:   e.Name,
			File:   e.File,
			Line:   e.Line,
			Column: e.Column,
			Safe:   e.Safe,
		}

	case UnaryExpr:
		right := c.optimizeExpr(e.Right)

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
		mainInstructions:        []Instruction{},
		mainDebugInfo:           []DebugInfo{},
		functions:               map[string]Function{},
		interfaces:              map[string]Interface{},
		nativeFunctions:         map[string]string{},
		externalFunctions:       map[string]Function{},
		classes:                 map[string]Class{},
		usedFunctions:           map[string]bool{},
		loopStack:               []LoopContext{},
		localCount:              0,
		globalConstants:         map[string]bool{},
		globalIndexes:           map[string]int{},
		anonymousFunctionCount:  0,
		functionIDs:             map[string]int{},
		declaredFunctions:       map[string]bool{},
		scopes:                  []map[string]Binding{},
		importStates:            map[string]ImportState{},
		importStack:             []string{},
		virtualObjects:          map[VarNodeKey]map[string]int{},
		stdImportModules:        map[string]string{},
		inlineCandidates:        map[string]FunctionStmt{},
		enumVariants:            map[string]map[string][]Param{},
		enumConstants:           map[string]TinyValue{},
		namespacePrivateMembers: map[string]bool{},
	}

	c.currentInstructions = &c.mainInstructions
	c.currentDebugInfo = &c.mainDebugInfo
	c.beginScope()

	return c
}

func (c *Compiler) SetPreserveAllFunctions(preserve bool) {
	c.preserveAllFunctions = preserve
}

func (c *Compiler) SetDiagnosticMode(enabled bool) {
	c.diagnosticMode = enabled
}

func (c *Compiler) predeclareNamespaceFunctions(prefix string, ns NamespaceStmt) {
	for _, nsStmt := range ns.Statements {
		if fn, ok := nsStmt.(FunctionStmt); ok {
			fullName := prefix + "." + ns.Name + "." + fn.Name
			c.declaredFunctions[fullName] = true
			c.getFunctionID(fullName)
			if c.inlineCandidates != nil {
				fn.Name = fullName
				c.inlineCandidates[fullName] = fn
			}
			if c.functionLooksJitCandidate(fn) {
				c.usedFunctions[fullName] = true
			}
		} else if nestedNs, ok := nsStmt.(NamespaceStmt); ok {
			c.predeclareNamespaceFunctions(prefix+"."+ns.Name, nestedNs)
		}
	}
}

func (c *Compiler) predeclareFunctions(statements []Stmt) {
	for _, stmt := range statements {
		switch s := stmt.(type) {
		case ExportStmt:
			if fn, ok := s.Inner.(FunctionStmt); ok {
				c.declaredFunctions[fn.Name] = true
				c.getFunctionID(fn.Name)
				c.usedFunctions[fn.Name] = true
				if c.inlineCandidates != nil {
					c.inlineCandidates[fn.Name] = fn
				}
			} else if fn, ok := s.Inner.(ExternalFnStmt); ok {
				c.declaredFunctions[fn.Name] = true
				c.externalFunctions[fn.Name] = Function{
					ID:         c.getFunctionID(fn.Name),
					Name:       fn.Name,
					Params:     fn.Params,
					ReturnType: fn.ReturnType,
				}
			}

		case FunctionStmt:
			c.declaredFunctions[s.Name] = true
			c.getFunctionID(s.Name)
			if c.inlineCandidates != nil {
				c.inlineCandidates[s.Name] = s
			}
			if c.functionLooksJitCandidate(s) {
				c.usedFunctions[s.Name] = true
			}

		case NativeFnStmt:
			c.declaredFunctions[s.Name] = true
			c.nativeFunctions[s.Name] = s.ReturnType.Name

		case ExternalFnStmt:
			c.declaredFunctions[s.Name] = true
			c.externalFunctions[s.Name] = Function{
				ID:         c.getFunctionID(s.Name),
				Name:       s.Name,
				Params:     s.Params,
				ReturnType: s.ReturnType,
			}

		case NamespaceStmt:
			for _, nsStmt := range s.Statements {
				if fn, ok := nsStmt.(FunctionStmt); ok {
					fullName := s.Name + "." + fn.Name
					c.declaredFunctions[fullName] = true
					c.getFunctionID(fullName)
					if c.inlineCandidates != nil {
						fn.Name = fullName
						c.inlineCandidates[fullName] = fn
					}
					if c.functionLooksJitCandidate(fn) {
						c.usedFunctions[fullName] = true
					}
				} else if nestedNs, ok := nsStmt.(NamespaceStmt); ok {
					c.predeclareNamespaceFunctions(s.Name, nestedNs)
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

	oldActiveTypeParams := c.activeTypeParams
	c.activeTypeParams = append(append([]string{}, oldActiveTypeParams...), stmt.TypeParameters...)
	defer func() {
		c.activeTypeParams = oldActiveTypeParams
	}()

	outerBindings := c.collectCapturableBindings()

	oldInstructions := c.currentInstructions
	oldDebugInfo := c.currentDebugInfo
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures
	oldFile := c.currentFile
	oldLine := c.currentLine
	oldColumn := c.currentColumn

	functionInstructions := []Instruction{}
	functionDebugInfo := []DebugInfo{}

	c.currentInstructions = &functionInstructions
	c.currentDebugInfo = &functionDebugInfo
	c.scopes = []map[string]Binding{}
	c.localCount = 0
	c.setLocation(stmt.File, stmt.Line, stmt.Column)

	c.beginScope()

	for _, param := range stmt.Params {
		binding := c.declareVariable(param.Name, false)
		binding.TypeHint = param.TypeHint.Name
		c.currentScope()[param.Name] = binding
	}

	c.performEscapeAnalysis(stmt.Body)

	c.inMethod = false
	c.outerBindings = outerBindings
	c.currentCaptures = map[string]CapturedVar{}

	oldReturnType := c.currentReturnType
	oldFunctionName := c.currentFunctionName

	c.currentReturnType = stmt.ReturnType
	c.currentFunctionName = compiledName

	defer func() {
		c.currentReturnType = oldReturnType
		c.currentFunctionName = oldFunctionName
	}()

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, NewNull())
	c.emit(OP_RETURN, nil)

	captures := []CapturedVar{}
	for _, capture := range c.currentCaptures {
		captures = append(captures, capture)
	}

	localCount := c.localCount
	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[compiledName] = Function{
		ID:             c.getFunctionID(compiledName),
		Name:           compiledName,
		TypeParameters: stmt.TypeParameters,
		Params:         stmt.Params,
		ReturnType:     stmt.ReturnType,
		Instructions:   functionInstructions,
		DebugInfo:      functionDebugInfo,
		StatementCount: len(stmt.Body),
		LocalCount:     localCount,
		Captures:       captures,
		Async:          stmt.Async,
		HasDefaults:    hasDefaults,
		HasTypeHints:   hasTypeHints,
	}

	c.currentInstructions = oldInstructions
	c.currentDebugInfo = oldDebugInfo
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures
	c.setLocation(oldFile, oldLine, oldColumn)

	c.usedFunctions[compiledName] = true

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

	if binding.Kind == BindingLocal && binding.Slot >= 0 {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     stmt.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	}
}

func (c *Compiler) declareVariable(name string, constant bool) Binding {
	scope := c.currentScope()

	if _, exists := scope[name]; exists {
		LangErrorAt(ErrorName, c.currentFile, c.currentLine, c.currentColumn, "variable already declared in this scope: %s", name)
	}

	if c.isCompilingNamespace && !c.isInsideFunction() && !strings.HasPrefix(name, c.currentNamespaceName+".") {
		name = c.currentNamespaceName + "." + name
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

	if len(c.scopes) > 1 {
		globalName = "__scope_" + strconv.Itoa(c.scopeID) + "_" + name
		c.scopeID++
	}

	if c.globalIndexes == nil {
		c.globalIndexes = map[string]int{}
	}
	if c.interfaces == nil {
		c.interfaces = map[string]Interface{}
	}
	if c.functions == nil {
		c.functions = map[string]Function{}
	}
	if c.functionIDs == nil {
		c.functionIDs = map[string]int{}
	}

	slot, exists := c.globalIndexes[globalName]
	if !exists {
		slot = len(c.globalConstants)
		c.globalIndexes[globalName] = slot
	}

	binding := Binding{
		Kind:     BindingGlobal,
		Name:     globalName,
		Slot:     slot,
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

func (c *Compiler) resolveFullyQualifiedName(expr Expr) (string, bool) {
	switch e := expr.(type) {
	case IdentExpr:
		name := e.Name
		if c.currentNamespaceVariables != nil {
			if fullName, exists := c.currentNamespaceVariables[name]; exists {
				return fullName, true
			}
		}
		if c.currentNamespaceEnums != nil {
			if fullName, exists := c.currentNamespaceEnums[name]; exists {
				return fullName, true
			}
		}
		if c.currentNamespaceClasses != nil {
			if fullName, exists := c.currentNamespaceClasses[name]; exists {
				return fullName, true
			}
		}
		return name, true

	case PropertyExpr:
		parentName, ok := c.resolveFullyQualifiedName(e.Object)
		if !ok {
			return "", false
		}
		return parentName + "." + e.Name, true
	}
	return "", false
}

func (c *Compiler) compileScopedBlock(body []Stmt) {
	c.beginScope()

	for _, stmt := range body {
		c.compileStatement(stmt)
	}

	c.endScope()
}

func uriToPath(uri string) string {
	if strings.HasPrefix(uri, "file:///") {
		path := strings.TrimPrefix(uri, "file:///")
		return filepath.FromSlash(path)
	}
	return uri
}

func getStatementFile(stmt Stmt) string {
	if stmt == nil {
		return ""
	}
	switch s := stmt.(type) {
	case ExportStmt:
		return getStatementFile(s.Inner)
	case NamespaceStmt:
		return s.File
	case FunctionStmt:
		return s.File
	case VariableStmt:
		return s.File
	case DestructureStmt:
		return s.File
	case ClassStmt:
		return s.File
	case EnumStmt:
		return s.File
	case InterfaceStmt:
		return s.File
	case ImportStmt:
		return s.File
	case EmbedStmt:
		return s.File
	case ExternalGlobalStmt:
		return s.File
	case ExternalFnStmt:
		return s.File
	case NativeFnStmt:
		return s.File
	}
	return ""
}

func (c *Compiler) CompileProgram(program Program) ([]Instruction, []DebugInfo, map[string]Function, map[string]Class, map[string]Interface, map[string]int) {
	abortOnCompilerSemanticErrors(c.currentFile, program.Statements)

	c.virtualObjects = map[VarNodeKey]map[string]int{}
	c.predeclareStdImportsForJitRegions(program.Statements)

	// Predeclare user functions before JIT-region outlining. The outliner needs
	// function signatures / inlineCandidates to infer call return types in top-level
	// loops such as:
	//
	//   for ... { final_result = aggregate(logs_data) }
	//
	// Without this, top-level calls are typed as unknown/number and the outer loop
	// never gets outlined into a JIT helper. Helper functions created by the
	// outliner register themselves below.
	c.predeclareFunctions(program.Statements)
	program.Statements = c.outlineJitRegionsInStatements(program.Statements)
	// Register helpers introduced by the outliner too. This keeps function IDs,
	// inlineCandidates, and usedFunctions coherent after main/function region
	// outlining adds __jit_region_* functions to the program.
	c.predeclareFunctions(program.Statements)

	c.performEscapeAnalysis(program.Statements)

	c.compileNativeFunctions(program.Statements)

	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	// remove unused functions. Compiler-generated JIT-region helpers are kept
	// even if an older path forgot to mark them used.
	if !c.preserveAllFunctions {
		for v := range c.functions {
			if strings.HasPrefix(v, "__jit_region_") {
				continue
			}
			if _, ok := c.usedFunctions[v]; !ok {
				delete(c.functions, v)
			}
		}
	}

	c.eraseFunctionGenericsForVM()

	return c.mainInstructions, c.mainDebugInfo, c.functions, c.classes, c.interfaces, c.globalIndexes
}

func (c *Compiler) CompileDiagnostic(program Program) SemanticModel {
	c.diagnosticMode = true
	c.preserveAllFunctions = true

	var collectedErrors []LangErrorType
	SetErrorCollector(&collectedErrors)
	defer ClearErrorCollector()

	defer func() {
		if r := recover(); r != nil {
			switch err := r.(type) {
			case LangErrorType:
				collectedErrors = append(collectedErrors, err)
			case *LangErrorType:
				collectedErrors = append(collectedErrors, *err)
			}
		}
	}()

	abortOnCompilerSemanticErrors(c.currentFile, program.Statements)

	c.virtualObjects = map[VarNodeKey]map[string]int{}
	c.predeclareFunctions(program.Statements)

	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	globals := map[string]string{}
	for name := range c.globalIndexes {
		if binding, ok := c.resolveVariable(name); ok && binding.TypeHint != "" {
			globals[name] = binding.TypeHint
		}
	}

	return SemanticModel{
		Functions:  c.functions,
		Classes:    c.classes,
		Interfaces: c.interfaces,
		Globals:    globals,
		AST:        program.Statements,
		Errors:     collectedErrors,
		compiler:   c,
	}
}

func (c *Compiler) predeclareStdImportsForJitRegions(stmts []Stmt) {
	if c.stdImportModules == nil {
		c.stdImportModules = map[string]string{}
	}
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case ImportStmt:
			if !s.Std {
				continue
			}
			name := s.Alias
			if name == "" {
				name = s.Path
			}
			c.stdImportModules[name] = s.Path
		case NamespaceStmt:
			for _, nested := range s.Statements {
				imp, ok := nested.(ImportStmt)
				if !ok || !imp.Std {
					continue
				}
				name := imp.Alias
				if name == "" {
					name = imp.Path
				}
				c.stdImportModules[s.Name+"."+name] = imp.Path
			}
		}
	}
}

func (c *Compiler) resolveStdImportModuleName(expr Expr) (string, bool) {
	ident, ok := expr.(IdentExpr)
	if !ok {
		return "", false
	}

	// 1. If there is a local binding with the same name, do NOT intrinsic-lower.
	// Example:
	// import std "math" as m
	// fn f() {
	//     let m = {}
	//     m.floor(1) // should be normal method call
	// }
	if binding, exists := c.resolveVariable(ident.Name); exists {
		if binding.Kind == BindingLocal {
			return "", false
		}

		if module, ok := c.stdImportModules[binding.Name]; ok {
			return module, true
		}
	}

	// 2. Global import fallback.
	// This is the missing part for functions.
	// compileFunction resets scopes, so global imports are not visible via resolveVariable().
	if module, ok := c.stdImportModules[ident.Name]; ok {
		return module, true
	}

	// 3. Namespace-local import fallback.
	if c.currentNamespaceVariables != nil {
		if fullName, exists := c.currentNamespaceVariables[ident.Name]; exists {
			if module, ok := c.stdImportModules[fullName]; ok {
				return module, true
			}
		}
	}

	return "", false
}

func (c *Compiler) tryCompileStdIntrinsic(e MemberCallExpr) bool {
	if e.Safe {
		return false
	}

	module, ok := c.resolveStdImportModuleName(e.Object)
	if !ok {
		return false
	}

	if module != "io" && hasSpreadArg(e.Args) {
		return false
	}

	switch module {
	case "io":
		switch e.Method {
		case "println":
			spreadArgs := c.compileCallArgs(e.Args)

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_PRINT, PrintInfo{
				ArgCount:   len(e.Args),
				NewLine:    true,
				SpreadArgs: spreadArgs,
			})

			return true

		case "print":
			spreadArgs := c.compileCallArgs(e.Args)

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_PRINT, PrintInfo{
				ArgCount:   len(e.Args),
				NewLine:    false,
				SpreadArgs: spreadArgs,
			})

			return true
		}
	case "json":
		if e.Method == "stringify" && len(e.Args) == 1 {
			c.compileExpr(e.Args[0])

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_JSON_STRINGIFY, nil)

			return true
		} else if e.Method == "parse" && len(e.Args) == 1 {
			c.compileExpr(e.Args[0])

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_JSON_PARSE, nil)

			return true
		}
	case "math":
		emitUnary := func(op OpCode) bool {
			if len(e.Args) != 1 {
				return false
			}

			c.compileExpr(e.Args[0])
			c.setLocation(e.File, e.Line, e.Column)
			c.emit(op, nil)
			return true
		}

		emitBinary := func(op OpCode) bool {
			if len(e.Args) != 2 {
				return false
			}

			c.compileExpr(e.Args[0])
			c.compileExpr(e.Args[1])
			c.setLocation(e.File, e.Line, e.Column)
			c.emit(op, nil)
			return true
		}

		switch e.Method {
		case "floor":
			return emitUnary(OP_MATH_FLOOR)
		case "ceil":
			return emitUnary(OP_MATH_CEIL)
		case "sqrt":
			return emitUnary(OP_MATH_SQRT)
		case "abs":
			return emitUnary(OP_MATH_ABS)
		case "pow":
			return emitBinary(OP_MATH_POW)
		}
	}

	return false
}

func (c *Compiler) compileNamespace(stmt NamespaceStmt) {
	namespaceStdImports := map[string]string{}
	namespacePluginImports := map[string]string{}
	hasExplicitExports := false
	qualify := func(name string) string {
		return stmt.Name + "." + name
	}

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
	oldNamespaceInterfaces := c.currentNamespaceInterfaces
	oldTypeImportAliases := c.currentTypeImportAliases
	oldIsCompilingNamespace := c.isCompilingNamespace
	oldNamespaceName := c.currentNamespaceName

	namespaceFunctions := map[string]string{}
	namespaceVariables := map[string]string{}
	namespaceClasses := cloneStringMap(oldNamespaceClasses)
	namespaceEnums := map[string]string{}
	namespaceInterfaces := cloneStringMap(oldNamespaceInterfaces)
	namespaceTypeImportAliases := cloneStringMap(oldTypeImportAliases)
	members := map[string]TinyValue{}

	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		if ns, ok := inner.(NamespaceStmt); ok {
			namespaceTypeImportAliases[ns.Name] = qualify(ns.Name)
		}

		if imp, ok := inner.(ImportStmt); ok {
			namespaceTypeImportAliases[imp.Alias] = imp.TypeNamespace
			if imp.TypeOnly {
				continue
			}
		}

		imp, ok := inner.(ImportStmt)
		if !ok || (!imp.Std && !imp.Plugin) {
			continue
		}

		alias := imp.Alias
		if alias == "" {
			alias = imp.Path
		}

		fullName := qualify(alias)

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

		var name string
		var isNative bool

		if fn, ok := inner.(FunctionStmt); ok {
			name = fn.Name
		} else if fn, ok := inner.(NativeFnStmt); ok {
			name = fn.Name
			isNative = true
		} else if fn, ok := inner.(ExternalFnStmt); ok {
			name = fn.Name
		} else {
			continue
		}

		fullName := qualify(name)

		namespaceFunctions[name] = fullName
		if (hasExplicitExports && !exported) || (func() bool {
			fn, ok := inner.(FunctionStmt)
			return ok && fn.Private
		})() {
			c.namespacePrivateMembers[fullName] = true
		}

		if !hasExplicitExports || exported {
			if _, ok := inner.(ExternalFnStmt); ok {
				members[name] = NewNative(NamespaceMemberRef{GlobalName: fullName})
			} else {
				members[name] = NewNative(FunctionValue{Name: fullName})
			}
		}

		if isNative {
			fn, _ := inner.(NativeFnStmt)
			c.declaredFunctions[fullName] = true
			c.nativeFunctions[fullName] = fn.ReturnType.Name
		} else if fn, ok := inner.(ExternalFnStmt); ok {
			c.declaredFunctions[fullName] = true
			c.externalFunctions[fullName] = Function{
				ID:         c.getFunctionID(fullName),
				Name:       fullName,
				Params:     fn.Params,
				ReturnType: fn.ReturnType,
			}
		} else {
			fn, _ := inner.(FunctionStmt)
			c.functions[fullName] = Function{
				ID:             c.getFunctionID(fullName),
				Async:          fn.Async,
				Name:           fullName,
				StatementCount: len(fn.Body),
				Params:         fn.Params,
			}
		}
	}

	// 3. Collect variables
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		var name string
		if v, ok := inner.(VariableStmt); ok {
			name = v.Name
		} else if s, ok := inner.(EmbedStmt); ok {
			name = s.Name
		} else if s, ok := inner.(ExternalGlobalStmt); ok {
			name = s.Name
		} else if destructureStmt, ok := inner.(DestructureStmt); ok {
			for _, name := range collectDestructuredNames(destructureStmt.Target) {
				fullName := qualify(name)
				namespaceVariables[name] = fullName
				if !hasExplicitExports || exported {
					members[name] = NewNative(NamespaceMemberRef{GlobalName: fullName})
				}
			}
			continue
		} else {
			continue
		}

		fullName := qualify(name)

		namespaceVariables[name] = fullName
		if hasExplicitExports && !exported {
			c.namespacePrivateMembers[fullName] = true
		}

		if !hasExplicitExports || exported {
			members[name] = NewNative(NamespaceMemberRef{GlobalName: fullName})
		}
	}

	// 4. Collect classes
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		classStmt, ok := inner.(ClassStmt)
		if !ok {
			continue
		}

		fullName := qualify(classStmt.Name)

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

		fullName := qualify(enumStmt.Name)

		namespaceEnums[enumStmt.Name] = fullName

		if !hasExplicitExports || exported {
			members[enumStmt.Name] = NewNative(NamespaceMemberRef{
				GlobalName: fullName,
			})
		}
	}

	// 5. Collect interfaces
	for _, raw := range stmt.Statements {
		inner, exported := unwrapExport(raw)

		interfaceStmt, ok := inner.(InterfaceStmt)
		if !ok {
			continue
		}

		fullName := qualify(interfaceStmt.Name)

		namespaceInterfaces[interfaceStmt.Name] = fullName

		if !hasExplicitExports || exported {
			members[interfaceStmt.Name] = NewNative(NamespaceMemberRef{
				GlobalName: fullName,
			})
		}
	}

	c.currentNamespaceClasses = namespaceClasses
	c.currentNamespaceInterfaces = namespaceInterfaces
	c.currentTypeImportAliases = namespaceTypeImportAliases

	// 1. Nested namespaces after this namespace's type names are known, so
	// imported child modules can use sibling parent types like Client.
	for _, raw := range stmt.Statements {
		ns, ok := raw.(NamespaceStmt)
		if !ok {
			continue
		}

		originalName := ns.Name
		ns.Name = stmt.Name + "." + ns.Name

		c.compileNamespace(ns)

		namespaceVariables[originalName] = ns.Name

		members[originalName] = NewNative(NamespaceMemberRef{
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

		fullName := qualify(alias)

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

		binding := c.declareVariable(fullName, true)
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: true,
			Slot:     binding.Slot,
		})

		c.globalConstants[fullName] = true

		if imp.Std {
			if c.stdImportModules == nil {
				c.stdImportModules = map[string]string{}
			}

			c.stdImportModules[binding.Name] = imp.Path
			c.stdImportModules[fullName] = imp.Path
		}
	}

	c.currentNamespaceFunctions = namespaceFunctions
	c.currentNamespaceVariables = namespaceVariables
	c.currentNamespaceClasses = namespaceClasses
	c.currentNamespaceEnums = namespaceEnums
	c.currentNamespaceInterfaces = namespaceInterfaces
	c.currentTypeImportAliases = namespaceTypeImportAliases
	c.isCompilingNamespace = true
	c.currentNamespaceName = stmt.Name

	// 6. Compile enums as hidden globals FIRST.
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		enumStmt, ok := inner.(EnumStmt)
		if !ok {
			continue
		}

		fullName := qualify(enumStmt.Name)

		obj := ObjectValue{}
		variants := map[string][]Param{}

		for _, member := range enumStmt.Members {
			if _, exists := obj[member.Name]; exists {
				c.fatalError(ErrorName, "duplicate enum member %s.%s", enumStmt.Name, member.Name)
			}

			val := c.evalConstantExpr(member.Value, "enum member must be constant.")
			obj[member.Name] = val
			c.enumConstants[fullName+"."+member.Name] = val

			if len(member.VariantParams) > 0 {
				variants[member.Name] = member.VariantParams
			}
		}

		if len(variants) > 0 {
			c.enumVariants[fullName] = variants
		}

		c.emit(OP_CONST, obj)

		binding := c.declareVariable(fullName, true)
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     fullName,
			Constant: true,
			Slot:     binding.Slot,
		})

		c.globalConstants[fullName] = true
	}

	// 7. Compile variables and embeds AFTER enums.
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		if v, ok := inner.(VariableStmt); ok {
			fullName := qualify(v.Name)

			c.compileExpr(v.Value)

			binding := c.declareVariable(fullName, v.Constant)
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:     fullName,
				Constant: v.Constant,
				TypeHint: v.TypeHint,
				Slot:     binding.Slot,
			})

			c.globalConstants[fullName] = v.Constant

		} else if embedStmt, ok := inner.(EmbedStmt); ok {
			fullName := qualify(embedStmt.Name)

			namespacedEmbed := EmbedStmt{
				Kind:         embedStmt.Kind,
				Name:         fullName,
				EmbeddedPath: embedStmt.EmbeddedPath,
				Constant:     embedStmt.Constant,
				TypeHint:     embedStmt.TypeHint,
				File:         embedStmt.File,
				Line:         embedStmt.Line,
				Column:       embedStmt.Column,
			}

			c.compileEmbedStatement(namespacedEmbed)
		} else if externalGlobal, ok := inner.(ExternalGlobalStmt); ok {
			fullName := qualify(externalGlobal.Name)
			c.setLocation(externalGlobal.File, externalGlobal.Line, externalGlobal.Column)
			binding := c.declareVariable(fullName, true)
			binding.TypeHint = externalGlobal.Type.Name
			c.currentScope()[fullName] = binding
			c.globalConstants[fullName] = true
		} else if destructureStmt, ok := inner.(DestructureStmt); ok {
			c.compileDestructureStmt(destructureStmt)
		}
	}

	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		externalFn, ok := inner.(ExternalFnStmt)
		if !ok {
			continue
		}

		fullName := qualify(externalFn.Name)
		c.setLocation(externalFn.File, externalFn.Line, externalFn.Column)
		binding := c.declareVariable(fullName, false)
		binding.TypeHint = externalFn.ReturnType.Name
		c.currentScope()[fullName] = binding
	}

	// 8. Compile interfaces before classes/functions so namespace-local return
	// hints can be checked while compiling method and function bodies.
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		interfaceStmt, ok := inner.(InterfaceStmt)
		if !ok {
			continue
		}

		fullName := qualify(interfaceStmt.Name)

		namespacedInterface := interfaceStmt
		namespacedInterface.Name = fullName
		namespacedInterface.Extends = qualifyNamespaceInterfaceNames(interfaceStmt.Extends, namespaceInterfaces)
		namespacedInterface.Fields = c.qualifyNamespaceTypeHintMap(interfaceStmt.Fields)

		c.compileInterfaceStatement(namespacedInterface)
	}

	// 9. Compile classes
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		classStmt, ok := inner.(ClassStmt)
		if !ok {
			continue
		}

		fullName := qualify(classStmt.Name)

		namespacedClass := classStmt
		namespacedClass.Name = fullName
		namespacedClass.Implements = qualifyNamespaceInterfaceNames(classStmt.Implements, namespaceInterfaces)
		namespacedClass.Embeds = qualifyNamespaceInterfaceNames(classStmt.Embeds, namespaceClasses)
		namespacedClass.Fields = c.qualifyNamespaceClassFields(classStmt.Fields)
		namespacedClass.Methods = c.qualifyNamespaceMethods(classStmt.Methods)

		c.compileClass(namespacedClass)
	}

	// 10. Compile functions
	for _, raw := range stmt.Statements {
		inner, _ := unwrapExport(raw)

		fn, ok := inner.(FunctionStmt)
		if !ok {
			continue
		}

		fullName := qualify(fn.Name)

		namespacedFn := fn
		namespacedFn.Name = fullName
		namespacedFn.Params = c.qualifyNamespaceParams(fn.Params)
		namespacedFn.ReturnType = c.qualifyNamespaceTypeHint(fn.ReturnType)

		c.compileFunction(namespacedFn)
	}

	c.currentNamespaceFunctions = oldNamespaceFunctions
	c.currentNamespaceVariables = oldNamespaceVariables
	c.currentNamespaceClasses = oldNamespaceClasses
	c.currentNamespaceEnums = oldNamespaceEnums
	c.currentNamespaceInterfaces = oldNamespaceInterfaces
	c.currentTypeImportAliases = oldTypeImportAliases
	c.isCompilingNamespace = oldIsCompilingNamespace
	c.currentNamespaceName = oldNamespaceName

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
			Slot:     binding.Slot,
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
			Name:     tempName,
			Slot:     tempBinding.Slot,
			Constant: true,
		})
	}

	endJumps := []int{}

	for _, matchCase := range stmt.Cases {
		// If there's a bind name, create a new scope for it
		if matchCase.BindName != "" {
			c.beginScope()
		}

		if c.compileEnumMatchCase(matchCase, tempBinding, &endJumps) {
			if matchCase.BindName != "" {
				c.endScope()
			}
			continue
		}

		if len(matchCase.Values) > 1 {
			// Union pattern: match against ANY of the alternatives
			alternativeJumps := []int{}

			for _, caseValue := range matchCase.Values {
				// load temp
				if tempBinding.Kind == BindingLocal {
					c.emit(OP_LOAD_LOCAL, tempBinding.Slot)
				} else {
					c.emit(OP_LOAD_GLOBAL, VariableInfo{
						Name: tempBinding.Name,
						Slot: tempBinding.Slot,
					})
				}

				// load case value
				c.compileExpr(caseValue)

				// compare
				c.emit(OP_EQ, nil)

				// if true, we matched - jump to guard/body
				alternativeJumps = append(alternativeJumps, c.emitJump(OP_JUMP_IF_TRUE))
			}

			// None of the alternatives matched, jump to next case
			jumpToNext := c.emitJump(OP_JUMP)

			// Patch all the "matched" jumps to land here
			for _, j := range alternativeJumps {
				c.patchJump(j)
			}

			// Now check guard if present
			if matchCase.Guard != nil {
				if matchCase.BindName != "" {
					c.compileMatchBindName(matchCase.BindName, tempBinding)
				}

				c.compileExpr(matchCase.Guard)
				guardFailJump := c.emitJump(OP_JUMP_IF_FALSE)

				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))

				c.patchJump(guardFailJump)
			} else {
				if matchCase.BindName != "" {
					c.compileMatchBindName(matchCase.BindName, tempBinding)
				}

				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))
			}

			c.patchJump(jumpToNext)
		} else if matchCase.BindName != "" && matchCase.Values[0] == nil {
			// Bind-only pattern: always matches, just binds the value
			c.compileMatchBindName(matchCase.BindName, tempBinding)

			if matchCase.Guard != nil {
				c.compileExpr(matchCase.Guard)
				guardFailJump := c.emitJump(OP_JUMP_IF_FALSE)

				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))

				c.patchJump(guardFailJump)
			} else {
				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))
			}
		} else {
			// Single value pattern
			// load temp
			if tempBinding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, tempBinding.Slot)
			} else {
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: tempBinding.Name,
					Slot: tempBinding.Slot,
				})
			}

			// load case value
			c.compileExpr(matchCase.Values[0])

			// compare
			c.emit(OP_EQ, nil)

			// if false, jump to next case
			jumpToNext := c.emitJump(OP_JUMP_IF_FALSE)

			// Check guard if present
			if matchCase.Guard != nil {
				if matchCase.BindName != "" {
					c.compileMatchBindName(matchCase.BindName, tempBinding)
				}

				c.compileExpr(matchCase.Guard)
				guardFailJump := c.emitJump(OP_JUMP_IF_FALSE)

				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))

				c.patchJump(guardFailJump)
				c.patchJump(jumpToNext)
			} else {
				if matchCase.BindName != "" {
					c.compileMatchBindName(matchCase.BindName, tempBinding)
				}

				c.compileScopedBlock(matchCase.Body)
				endJumps = append(endJumps, c.emitJump(OP_JUMP))

				c.patchJump(jumpToNext)
			}
		}

		// End bind name scope
		if matchCase.BindName != "" {
			c.endScope()
		}
	}

	if stmt.Default != nil {
		c.compileScopedBlock(stmt.Default)
	}

	for _, jump := range endJumps {
		c.patchJump(jump)
	}
}

func (c *Compiler) compileMatchBindName(name string, tempBinding Binding) {
	binding := c.declareVariable(name, true)

	if tempBinding.Kind == BindingLocal {
		c.emit(OP_LOAD_LOCAL, tempBinding.Slot)
	} else {
		c.emit(OP_LOAD_GLOBAL, VariableInfo{
			Name: tempBinding.Name,
			Slot: tempBinding.Slot,
		})
	}

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     name,
			Slot:     binding.Slot,
			Constant: true,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Slot:     binding.Slot,
			Constant: true,
		})
	}
}

func (c *Compiler) compileEnumMatchCase(matchCase MatchCase, tempBinding Binding, endJumps *[]int) bool {
	if len(matchCase.Values) != 1 {
		return false
	}

	call, ok := matchCase.Value.(MemberCallExpr)
	if !ok {
		return false
	}

	enumName, ok := c.resolveFullyQualifiedName(call.Object)
	if !ok || !c.isEnumVariant(enumName, call.Method) {
		return false
	}

	variantParams := c.enumVariants[enumName][call.Method]
	if len(call.Args) != len(variantParams) {
		c.fatalError(ErrorSyntax, "enum pattern %s.%s expects %d payload value(s), got %d", enumName, call.Method, len(variantParams), len(call.Args))
	}

	c.beginScope()
	defer c.endScope()

	failJumps := []int{}

	loadTemp := func() {
		if tempBinding.Kind == BindingLocal {
			c.emit(OP_LOAD_LOCAL, tempBinding.Slot)
		} else {
			c.emit(OP_LOAD_GLOBAL, VariableInfo{Name: tempBinding.Name, Slot: tempBinding.Slot})
		}
	}

	loadTemp()
	c.emit(OP_TYPEOF, nil)
	c.emit(OP_CONST, "object")
	c.emit(OP_EQ, nil)
	failJumps = append(failJumps, c.emitJump(OP_JUMP_IF_FALSE))

	loadTemp()
	c.emit(OP_GET_PROPERTY, "_enum")
	c.emit(OP_CONST, enumName)
	c.emit(OP_EQ, nil)
	failJumps = append(failJumps, c.emitJump(OP_JUMP_IF_FALSE))

	loadTemp()
	c.emit(OP_GET_PROPERTY, "_variant")
	c.emit(OP_CONST, call.Method)
	c.emit(OP_EQ, nil)
	failJumps = append(failJumps, c.emitJump(OP_JUMP_IF_FALSE))

	for i, arg := range call.Args {
		fieldName := fmt.Sprintf("_%d", i)
		if ident, ok := arg.(IdentExpr); ok {
			binding := c.declareVariable(ident.Name, true)

			loadTemp()
			c.emit(OP_GET_PROPERTY, fieldName)

			if binding.Kind == BindingLocal {
				c.emit(OP_STORE_LOCAL, VariableInfo{Name: ident.Name, Slot: binding.Slot, Constant: true})
			} else {
				c.emit(OP_STORE_GLOBAL, VariableInfo{Name: binding.Name, Slot: binding.Slot, Constant: true})
			}
			continue
		}

		loadTemp()
		c.emit(OP_GET_PROPERTY, fieldName)
		c.compileExpr(arg)
		c.emit(OP_EQ, nil)
		failJumps = append(failJumps, c.emitJump(OP_JUMP_IF_FALSE))
	}

	if matchCase.BindName != "" {
		c.compileMatchBindName(matchCase.BindName, tempBinding)
	}

	if matchCase.Guard != nil {
		c.compileExpr(matchCase.Guard)
		guardFailJump := c.emitJump(OP_JUMP_IF_FALSE)

		c.compileScopedBlock(matchCase.Body)
		*endJumps = append(*endJumps, c.emitJump(OP_JUMP))

		c.patchJump(guardFailJump)
	} else {
		c.compileScopedBlock(matchCase.Body)
		*endJumps = append(*endJumps, c.emitJump(OP_JUMP))
	}

	for _, jump := range failJumps {
		c.patchJump(jump)
	}

	return true
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
		Slot:     binding.Slot,
	})
}

func (c *Compiler) emitLoadBinding(binding Binding) {
	if binding.Kind == BindingLocal {
		c.emit(OP_LOAD_LOCAL, binding.Slot)
		return
	}

	c.emit(OP_LOAD_GLOBAL, VariableInfo{
		Name: binding.Name,
		Slot: binding.Slot,
	})
}

func (c *Compiler) emitAssignBinding(binding Binding) {
	if binding.Kind == BindingLocal {
		c.emit(OP_ASSIGN_LOCAL, binding.Slot)
		return
	}

	c.emit(OP_ASSIGN_GLOBAL, VariableInfo{
		Name: binding.Name,
		Slot: binding.Slot,
	})
}

func (c *Compiler) compileDestructureStmt(stmt DestructureStmt) {
	c.setLocation(stmt.File, stmt.Line, stmt.Column)

	c.compileExpr(stmt.Value)

	tempName := "__destructure_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++

	tempBinding := c.declareVariable(tempName, true)
	c.emitStoreBinding(tempBinding, tempName, true, TypeHint{})

	switch target := stmt.Target.(type) {
	case ObjectDestructurePattern:
		for _, field := range target.Fields {
			c.setLocation(stmt.File, stmt.Line, stmt.Column)
			c.emitLoadBinding(tempBinding)

			c.emit(OP_GET_PROPERTY, field.Key)

			if field.HasNested {
				c.compileDestructureNestedField(stmt, field, tempBinding)
				continue
			}

			if field.AliasIsRenamed {
				binding := c.declareVariable(field.Alias, stmt.Constant)
				c.emitStoreBinding(binding, field.Alias, stmt.Constant, TypeHint{})
			} else {
				binding := c.declareVariable(field.Key, stmt.Constant)
				c.emitStoreBinding(binding, field.Key, stmt.Constant, TypeHint{})
			}

			if field.HasDefault {
				_ = field.Default
			}
		}

	case ArrayDestructurePattern:
		for i, elem := range target.Elements {
			c.setLocation(stmt.File, stmt.Line, stmt.Column)
			c.emitLoadBinding(tempBinding)

			c.emit(OP_CONST, i)
			c.emit(OP_INDEX, nil)

			if elem.IsSpread {
				_ = elem
				continue
			}

			if elem.HasNested {
				c.compileDestructureNestedArrayElem(stmt, elem, tempBinding, i)
				continue
			}

			binding := c.declareVariable(elem.Name, stmt.Constant)
			c.emitStoreBinding(binding, elem.Name, stmt.Constant, TypeHint{})
		}
	}
}

func (c *Compiler) compileDestructureNestedField(stmt DestructureStmt, field ObjectDestructureField, tempBinding Binding) {
	nestedTempName := "__destructure_nested_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++

	nestedTempBinding := c.declareVariable(nestedTempName, true)
	c.emitStoreBinding(nestedTempBinding, nestedTempName, true, TypeHint{})

	switch nested := field.Pattern.(type) {
	case ObjectDestructurePattern:
		for _, nestedField := range nested.Fields {
			c.emitLoadBinding(nestedTempBinding)
			c.emit(OP_GET_PROPERTY, nestedField.Key)

			if nestedField.HasNested {
				c.compileDestructureNestedField(stmt, nestedField, nestedTempBinding)
				continue
			}

			name := nestedField.Key
			if nestedField.AliasIsRenamed {
				name = nestedField.Alias
			}

			binding := c.declareVariable(name, stmt.Constant)
			c.emitStoreBinding(binding, name, stmt.Constant, TypeHint{})
		}

	case ArrayDestructurePattern:
		for i, elem := range nested.Elements {
			c.emitLoadBinding(nestedTempBinding)
			c.emit(OP_CONST, i)
			c.emit(OP_INDEX, nil)

			if elem.HasNested {
				c.compileDestructureNestedArrayElem(stmt, elem, nestedTempBinding, i)
				continue
			}

			binding := c.declareVariable(elem.Name, stmt.Constant)
			c.emitStoreBinding(binding, elem.Name, stmt.Constant, TypeHint{})
		}
	}
}

func (c *Compiler) compileDestructureNestedArrayElem(stmt DestructureStmt, elem ArrayDestructureElement, tempBinding Binding, index int) {
	nestedTempName := "__destructure_nested_" + strconv.Itoa(c.matchTempID)
	c.matchTempID++

	nestedTempBinding := c.declareVariable(nestedTempName, true)
	c.emitStoreBinding(nestedTempBinding, nestedTempName, true, TypeHint{})

	switch nested := elem.Pattern.(type) {
	case ObjectDestructurePattern:
		for _, field := range nested.Fields {
			c.emitLoadBinding(nestedTempBinding)
			c.emit(OP_GET_PROPERTY, field.Key)

			name := field.Key
			if field.AliasIsRenamed {
				name = field.Alias
			}

			binding := c.declareVariable(name, stmt.Constant)
			c.emitStoreBinding(binding, name, stmt.Constant, TypeHint{})
		}

	case ArrayDestructurePattern:
		for i, nestedElem := range nested.Elements {
			c.emitLoadBinding(nestedTempBinding)
			c.emit(OP_CONST, i)
			c.emit(OP_INDEX, nil)

			binding := c.declareVariable(nestedElem.Name, stmt.Constant)
			c.emitStoreBinding(binding, nestedElem.Name, stmt.Constant, TypeHint{})
		}
	}
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

	c.loopStack = append(c.loopStack, LoopContext{
		Start: loopStart,
	})

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
	c.emit(OP_INDEX, nil)

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

	updateStart := len(*c.currentInstructions)

	currentLoop := c.loopStack[len(c.loopStack)-1]

	for _, continueJump := range currentLoop.ContinueJumps {
		(*c.currentInstructions)[continueJump].Value = updateStart
	}

	c.endScope()

	// __i = __i + 1
	c.emitLoadBinding(indexBinding)
	c.emit(OP_CONST, 1)
	c.emit(OP_ADD, nil)
	c.emitAssignBinding(indexBinding)

	c.emit(OP_JUMP, loopStart)

	c.patchJump(exitJump)

	currentLoop = c.loopStack[len(c.loopStack)-1]
	c.loopStack = c.loopStack[:len(c.loopStack)-1]

	for _, breakJump := range currentLoop.BreakJumps {
		c.patchJump(breakJump)
	}
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
		key := VarNodeKey{File: s.File, Line: s.Line, Column: s.Column}
		if fields, isVirtual := c.virtualObjects[key]; isVirtual {
			obj := s.Value.(ObjectExpr)
			for _, field := range obj.Fields {
				c.compileExpr(field.Value)
				slot := fields[field.Name]
				c.emit(OP_STORE_LOCAL, VariableInfo{
					Name:     s.Name + "." + field.Name,
					Slot:     slot,
					Constant: s.Constant,
				})
			}
			c.currentScope()[s.Name] = Binding{
				Kind:          BindingLocal,
				Name:          s.Name,
				Slot:          -1,
				Constant:      s.Constant,
				VirtualFields: fields,
			}
			return
		}

		c.compileExpr(s.Value)

		binding := c.declareVariable(s.Name, s.Constant)
		binding.TypeHint = s.TypeHint.Name

		c.currentScope()[s.Name] = binding

		c.setLocation(s.File, s.Line, s.Column)

		if binding.Kind == BindingLocal && binding.Slot >= 0 {
			c.emit(OP_STORE_LOCAL, VariableInfo{
				Name:          s.Name,
				Slot:          binding.Slot,
				Constant:      s.Constant,
				TypeHint:      c.eraseTypeHint(s.TypeHint),
				Uninitialized: isImplicitNullInitializer(s.Value),
			})
		} else {
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:          binding.Name,
				Constant:      s.Constant,
				TypeHint:      c.eraseTypeHint(s.TypeHint),
				Slot:          binding.Slot,
				Uninitialized: isImplicitNullInitializer(s.Value),
			})
		}

	case DestructureStmt:
		c.compileDestructureStmt(s)

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
				c.emit(OP_ASSIGN_GLOBAL, VariableInfo{
					Name: binding.Name,
					Slot: binding.Slot,
				})
			}
			return
		}

		if c.outerBindings != nil {
			if outer, exists := c.outerBindings[s.Name]; exists {
				if outer.Kind == BindingGlobal {
					c.setLocation(s.File, s.Line, s.Column)
					c.emit(OP_ASSIGN_GLOBAL, VariableInfo{
						Name: outer.Name,
						Slot: outer.Slot,
					})
					return
				}

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
				c.emit(OP_ASSIGN_GLOBAL, VariableInfo{
					Name: fullName,
					Slot: c.globalIndexes[fullName],
				})
				return
			}
		}

		if c.isCompilingNamespace {
			LangErrorAt(
				ErrorName,
				s.File,
				s.Line,
				s.Column,
				"undefined variable in namespace: %s",
				s.Name,
			)
		}

		c.emit(OP_ASSIGN_GLOBAL, VariableInfo{
			Name: s.Name,
			Slot: c.globalIndexes[s.Name],
		})

	case IndexAssignStmt:
		if strLit, ok := s.Index.(StringExpr); ok {
			c.compileExpr(s.Object)
			c.compileExpr(s.Value)
			c.emit(OP_SET_PROPERTY, strLit.Value)
		} else {
			c.compileExpr(s.Object)
			c.compileExpr(s.Index)
			c.compileExpr(s.Value)
			c.emit(OP_SET_INDEX, nil)
		}

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
		c.setLocation(s.File, s.Line, s.Column)
		if c.isCompilingMain() {
			c.compileFunction(s)
		} else {
			c.compileNestedFunction(s)
		}

	case NativeFnStmt:

	case ExternalFnStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.declareVariable(s.Name, false)

	case ExternalGlobalStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.declareVariable(s.Name, true)

	case ReturnStmt:
		c.setLocation(s.File, s.Line, s.Column)

		if !c.currentReturnType.IsEmpty() && c.currentReturnType.Name != "any" {
			returnedType := "null"
			if s.HasValue {
				returnedType = c.inferCompileTimeType(s.Value)
			}

			if returnedType != "any" {
				if !c.compareCompileTimeTypes(returnedType, c.currentReturnType.Name) {
					c.fatalError(ErrorType, "cannot return %s from function '%s' (expected %s)", returnedType, c.currentFunctionName, c.currentReturnType.Name)
				}
			}
		}

		if s.HasValue {
			c.compileExpr(s.Value)
		} else {
			c.emit(OP_CONST, NewNull())
		}

		for i := len(c.activeLocks) - 1; i >= 0; i-- {
			c.compileExpr(c.activeLocks[i])
			c.emit(OP_UNLOCK_MUTEX, nil)
		}

		c.emit(OP_RETURN, nil)

	case ImportStmt:
		if s.TypeOnly {
			return
		}

		if s.Std {
			c.compileStdImport(s)
			return
		}

		if s.Plugin {
			c.compilePluginImport(s)
			return
		}

		if c.diagnosticMode {
			c.storeImportedAlias(s.Alias, true)
			return
		}

		c.fatalError(ErrorInternal, "imports should be resolved before compiling")

	case IfStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileIfStatement(s)

	case InterfaceStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileInterfaceStatement(s)

	case EmbedStmt:
		c.setLocation(s.File, s.Line, s.Column)
		c.compileEmbedStatement(s)

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
		if ident, ok := s.Object.(IdentExpr); ok {
			if binding, exists := c.resolveVariable(ident.Name); exists && binding.VirtualFields != nil {
				if slot, ok := binding.VirtualFields[s.Name]; ok {
					c.compileExpr(s.Value)
					c.emit(OP_ASSIGN_LOCAL, slot)
					return
				}
			}
		}

		c.compileExpr(s.Object)
		c.compileExpr(s.Value)
		c.emit(OP_SET_PROPERTY, s.Name)

	case ClassStmt:
		c.compileClass(s)

	case EnumStmt:
		c.compileEnum(s)

	case FieldStmt:
		return

	case ExportStmt:
		c.compileStatement(s.Inner)

	default:
		c.fatalError(ErrorInternal, "unknown statement: %T", stmt)
	}
}

func (c *Compiler) compileEnum(stmt EnumStmt) {
	if len(stmt.Members) == 0 {
		c.setLocation(stmt.File, stmt.Line, stmt.Column)
		c.fatalError(ErrorSyntax, "enum %s must have at least one member", stmt.Name)
	}

	obj := ObjectValue{}

	variants := map[string][]Param{}

	for _, member := range stmt.Members {
		if _, exists := obj[member.Name]; exists {
			c.fatalError(ErrorName, "duplicate enum member %s.%s", stmt.Name, member.Name)
		}

		val := c.evalConstantExpr(member.Value, "enum member must be constant.")
		obj[member.Name] = val
		c.enumConstants[stmt.Name+"."+member.Name] = val

		if len(member.VariantParams) > 0 {
			variants[member.Name] = member.VariantParams
		}
	}

	if len(variants) > 0 {
		c.enumVariants[stmt.Name] = variants
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
			Slot:     binding.Slot,
		})
	}
}

func (c *Compiler) isEnumVariant(enumName, variantName string) bool {
	if variants, ok := c.enumVariants[enumName]; ok {
		_, exists := variants[variantName]
		return exists
	}
	return false
}

func (c *Compiler) compileEnumVariantConstruction(enumName string, variantName string, args []Expr, file string, line int, column int) {
	c.setLocation(file, line, column)

	c.emit(OP_CONST, enumName)
	c.emit(OP_CONST, variantName)

	for _, arg := range args {
		c.compileExpr(arg)
	}

	names := make([]ObjectFieldsInfo, 2+len(args))
	names[0] = ObjectFieldsInfo{Name: "_enum", Copy: false}
	names[1] = ObjectFieldsInfo{Name: "_variant", Copy: false}
	for i := range args {
		names[2+i] = ObjectFieldsInfo{Name: fmt.Sprintf("_%d", i), Copy: false}
	}

	c.emit(OP_OBJECT, ObjectInfo{Names: names})
}

func (c *Compiler) storeImportedAlias(name string, constant bool) Binding {
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
		return binding
	}

	c.emit(OP_STORE_GLOBAL, VariableInfo{
		Name:     binding.Name,
		Constant: constant,
		Slot:     binding.Slot,
	})

	return binding
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

	binding := c.storeImportedAlias(name, true)

	if c.stdImportModules == nil {
		c.stdImportModules = map[string]string{}
	}

	// Actual global/internal name
	c.stdImportModules[binding.Name] = stmt.Path

	// Source alias name, needed inside functions where global scope is not in c.scopes
	c.stdImportModules[name] = stmt.Path
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
		info.Slot = binding.Slot // <-- ADD THIS LINE! [22]
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

	if outer.Kind == BindingGlobal {
		return outer, true
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

func (c *Compiler) evalConstantExpr(expr Expr, err string) TinyValue {
	switch e := expr.(type) {
	case StringExpr:
		return NewNative(e.Value)

	case NumberExpr:
		return NewInt(e.Value)

	case FloatExpr:
		return NewNative(e.Value)

	case BoolExpr:
		return NewNative(e.Value)

	case NullExpr:
		return NewNull()

	case ArrayExpr:
		arr := &ArrayValue{
			Elements: []TinyValue{},
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
		return NewNull()
	}
}

func (c *Compiler) compileClass(stmt ClassStmt) {
	if _, exists := c.classes[stmt.Name]; exists {
		c.fatalError(ErrorName, "class already defined: %s", stmt.Name)
	}

	if interfaceName, exists := c.interfaces[stmt.Name]; exists {
		c.fatalError(ErrorName, "class %s has the same name as interface %s", stmt.Name, interfaceName.Name)
	}

	oldActiveTypeParams := c.activeTypeParams
	c.activeTypeParams = append([]string{}, stmt.TypeParameters...)
	defer func() {
		c.activeTypeParams = oldActiveTypeParams
	}()

	methods := map[string]string{}
	methodSignatures := map[string]MethodSignature{}
	privateMethods := map[string]bool{}
	fields := []ClassField{}

	for _, method := range stmt.Methods {
		compiledName := stmt.Name + "." + method.Name
		methods[method.Name] = compiledName
		methodSignatures[method.Name] = MethodSignature{
			Params:     method.Params,
			ReturnType: method.ReturnType,
			Async:      method.Async,
		}

		if method.Private {
			privateMethods[method.Name] = true
		}

		c.usedFunctions[compiledName] = true

		classMethod := FunctionStmt{
			Name:           method.Name,
			TypeParameters: method.TypeParameters,
			Params:         method.Params,
			ReturnType:     method.ReturnType,
			Body:           method.Body,
			Private:        method.Private,
			Async:          method.Async,
		}

		c.compileMethod(stmt.Name, classMethod)
	}

	for _, field := range stmt.Fields {
		fieldValue := c.evalConstantExpr(field.Value, "class field default must be constant.")
		classField := ClassField{
			Constant: field.Constant,
			Name:     field.Name,
			Value:    fieldValue,
			TypeHint: c.eraseTypeHint(field.TypeHint),
			Private:  field.Private,
		}

		fields = append(fields, classField)

		isGenericParam := false
		for _, tp := range stmt.TypeParameters {
			if field.TypeHint.Name == tp || strings.Contains(field.TypeHint.Name, tp+"|") || strings.Contains(field.TypeHint.Name, "|"+tp) || strings.Contains(field.TypeHint.Name, tp+":") || strings.Contains(field.TypeHint.Name, ":"+tp) {
				isGenericParam = true
				break
			}
		}

		if !isGenericParam && !isImplicitNullInitializer(field.Value) {
			if ok, _ := CheckTypeHint(fieldValue, field.TypeHint, c.interfaces); !ok {
				c.fatalError(
					ErrorType,
					"field %s in class '%s' expected %s, got %s",
					field.Name,
					stmt.Name,
					field.TypeHint.Name,
					TypeName(fieldValue),
				)
			}
		}

		c.compileStatement(field)
	}

	c.classes[stmt.Name] = Class{
		Name:             stmt.Name,
		TypeParameters:   stmt.TypeParameters,
		Implements:       stmt.Implements,
		Methods:          methods,
		MethodSignatures: methodSignatures,
		Embeds:           stmt.Embeds,
		Fields:           fields,
		PrivateMethods:   privateMethods,
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

func (c *Compiler) compileEmbedStatement(stmt EmbedStmt) {
	if stmt.Kind == EmbedFolder {
		assets := ObjectValue{}

		err := filepath.Walk(stmt.EmbeddedPath, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return err
			}

			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}

			relPath, _ := filepath.Rel(stmt.EmbeddedPath, path)
			key := filepath.ToSlash(relPath)

			ext := strings.ToLower(filepath.Ext(path))

			if ext == ".html" || ext == ".css" || ext == ".js" || ext == ".json" || ext == ".svg" || ext == ".txt" {
				assets[key] = NewNative(string(content))
			} else {
				assets[key] = NewNative(&BufferValue{Bytes: content})
			}

			return nil
		})

		if err != nil {
			c.fatalError(ErrorImport, "could not embed folder '%s': %v", filepath.Base(stmt.EmbeddedPath), err)
		}

		c.emit(OP_CONST, assets)

	} else {
		content, err := os.ReadFile(stmt.EmbeddedPath)
		if err != nil {
			c.fatalError(ErrorImport, "could not embed file '%s': %s", filepath.Base(stmt.EmbeddedPath), err)
		}

		if stmt.Kind == EmbedText {
			c.emit(OP_CONST, string(content))
		} else {
			c.emit(OP_CONST, &BufferValue{
				Bytes: content,
			})
		}
	}

	// 2. The variable binding and storing logic stays 100% the same! [compiler.go]
	binding := c.declareVariable(stmt.Name, stmt.Constant)

	c.setLocation(stmt.File, stmt.Line, stmt.Column)

	if binding.Kind == BindingLocal {
		c.emit(OP_STORE_LOCAL, VariableInfo{
			Name:     stmt.Name,
			Slot:     binding.Slot,
			Constant: stmt.Constant,
			TypeHint: stmt.TypeHint,
		})
	} else {
		c.emit(OP_STORE_GLOBAL, VariableInfo{
			Name:     binding.Name,
			Constant: stmt.Constant,
			TypeHint: stmt.TypeHint,
			Slot:     binding.Slot,
		})
	}
}

func (c *Compiler) compileInterfaceStatement(stmt InterfaceStmt) {
	if _, exists := c.interfaces[stmt.Name]; exists {
		c.fatalError(ErrorName, "interface already defined: %s", stmt.Name)
	}

	if className, exists := c.classes[stmt.Name]; exists {
		c.fatalError(ErrorName, "interface %s has the same name as class %s", stmt.Name, className.Name)
	}

	c.interfaces[stmt.Name] = Interface{
		Name:           stmt.Name,
		TypeParameters: stmt.TypeParameters,
		Extends:        stmt.Extends,
		Fields:         stmt.Fields,
	}

	c.emit(OP_CONST, NewNative(InterfaceValue{Name: stmt.Name}))
	binding := c.declareVariable(stmt.Name, true)
	c.emit(OP_STORE_GLOBAL, VariableInfo{
		Name:     binding.Name,
		Constant: true,
		Slot:     binding.Slot,
	})
}

func qualifyNamespaceInterfaceNames(names []string, namespaceInterfaces map[string]string) []string {
	if len(names) == 0 {
		return nil
	}
	qualified := make([]string, len(names))
	for i, name := range names {
		base := name
		suffix := ""
		if colon := strings.Index(name, ":"); colon >= 0 {
			base = name[:colon]
			suffix = name[colon:]
		}
		if fullName, exists := namespaceInterfaces[base]; exists {
			qualified[i] = fullName + suffix
			continue
		}
		qualified[i] = name
	}
	return qualified
}

func cloneStringMap(src map[string]string) map[string]string {
	dst := map[string]string{}
	for key, value := range src {
		dst[key] = value
	}
	return dst
}

func (c *Compiler) qualifyNamespaceTypeHint(hint TypeHint) TypeHint {
	if hint.IsEmpty() {
		return hint
	}

	hint.Name = c.qualifyNamespaceTypeName(hint.Name)
	for i, typ := range hint.Types {
		hint.Types[i] = c.qualifyNamespaceTypeName(typ)
	}
	return hint
}

func (c *Compiler) qualifyNamespaceTypeName(name string) string {
	if name == "" {
		return name
	}
	if strings.HasPrefix(name, "function(") {
		return name
	}
	if strings.Contains(name, "|") {
		parts := strings.Split(name, "|")
		for i, part := range parts {
			trimmed := strings.TrimSpace(part)
			parts[i] = strings.Replace(part, trimmed, c.qualifyNamespaceTypeName(trimmed), 1)
		}
		return strings.Join(parts, "|")
	}
	if strings.HasPrefix(name, "array:") {
		return "array:" + c.qualifyNamespaceTypeName(strings.TrimPrefix(name, "array:"))
	}
	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")
		for i, part := range parts {
			parts[i] = c.qualifyNamespaceTypeName(part)
		}
		return strings.Join(parts, ":")
	}
	if strings.Contains(name, ".") {
		dot := strings.Index(name, ".")
		alias := name[:dot]
		baseName := name[dot+1:]
		if fullNamespace, exists := c.currentTypeImportAliases[alias]; exists {
			return fullNamespace + "." + baseName
		}
	}
	if fullName, exists := c.currentNamespaceClasses[name]; exists {
		return fullName
	}
	if fullName, exists := c.currentNamespaceInterfaces[name]; exists {
		return fullName
	}
	return name
}

func (c *Compiler) qualifyNamespaceParams(params []Param) []Param {
	out := append([]Param(nil), params...)
	for i := range out {
		out[i].TypeHint = c.qualifyNamespaceTypeHint(out[i].TypeHint)
	}
	return out
}

func (c *Compiler) qualifyNamespaceClassFields(fields []FieldStmt) []FieldStmt {
	out := append([]FieldStmt(nil), fields...)
	for i := range out {
		out[i].TypeHint = c.qualifyNamespaceTypeHint(out[i].TypeHint)
	}
	return out
}

func (c *Compiler) qualifyNamespaceMethods(methods []FunctionStmt) []FunctionStmt {
	out := append([]FunctionStmt(nil), methods...)
	for i := range out {
		out[i].Params = c.qualifyNamespaceParams(out[i].Params)
		out[i].ReturnType = c.qualifyNamespaceTypeHint(out[i].ReturnType)
	}
	return out
}

func (c *Compiler) qualifyNamespaceTypeHintMap(fields map[string]TypeHint) map[string]TypeHint {
	if fields == nil {
		return nil
	}
	out := map[string]TypeHint{}
	for name, hint := range fields {
		out[name] = c.qualifyNamespaceTypeHint(hint)
	}
	return out
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

func (c *Compiler) compileLogicalAnd(left Expr, right Expr) {
	c.compileExpr(left)
	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	c.compileExpr(right)
	c.emit(OP_NOT, nil)
	c.emit(OP_NOT, nil)
	jumpOverFalseIndex := c.emitJump(OP_JUMP)

	c.patchJump(jumpIfFalseIndex)
	c.emit(OP_CONST, NewNative(false))
	c.patchJump(jumpOverFalseIndex)
}

func (c *Compiler) compileLogicalOr(left Expr, right Expr) {
	c.compileExpr(left)
	jumpIfTrueIndex := c.emitJump(OP_JUMP_IF_TRUE)

	c.compileExpr(right)
	c.emit(OP_NOT, nil)
	c.emit(OP_NOT, nil)
	jumpOverTrueIndex := c.emitJump(OP_JUMP)

	c.patchJump(jumpIfTrueIndex)
	c.emit(OP_CONST, NewNative(true))
	c.patchJump(jumpOverTrueIndex)
}

func isImplicitNullInitializer(expr Expr) bool {
	_, ok := expr.(NullExpr)
	return ok
}

func (c *Compiler) compileFunction(stmt FunctionStmt) {
	if existing, exists := c.functions[stmt.Name]; exists && len(existing.Instructions) > 0 {
		c.fatalError(ErrorName, "function already defined: %s", stmt.Name)
	}

	if _, exists := c.nativeFunctions[stmt.Name]; exists {
		c.fatalError(ErrorName, "function already defined: %s", stmt.Name)
	}

	oldActiveTypeParams := c.activeTypeParams
	c.activeTypeParams = append(append([]string{}, oldActiveTypeParams...), stmt.TypeParameters...)
	defer func() {
		c.activeTypeParams = oldActiveTypeParams
	}()

	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[stmt.Name] = Function{
		ID:             c.getFunctionID(stmt.Name),
		Name:           stmt.Name,
		TypeParameters: stmt.TypeParameters,
		Params:         stmt.Params,
		StatementCount: len(stmt.Body),
		HasDefaults:    hasDefaults,
		HasTypeHints:   hasTypeHints,
		Async:          stmt.Async,
		ReturnType:     stmt.ReturnType,
	}

	oldInstructions := c.currentInstructions
	oldDebugInfo := c.currentDebugInfo
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures

	functionInstructions := []Instruction{}
	functionDebugInfo := []DebugInfo{}

	c.currentInstructions = &functionInstructions
	c.currentDebugInfo = &functionDebugInfo
	c.scopes = []map[string]Binding{}
	c.localCount = 0

	c.beginScope()

	for _, param := range stmt.Params {
		binding := c.declareVariable(param.Name, false)
		binding.TypeHint = param.TypeHint.Name
		c.currentScope()[param.Name] = binding
	}

	c.performEscapeAnalysis(stmt.Body)

	c.inMethod = false
	c.outerBindings = nil
	c.currentCaptures = nil

	oldReturnType := c.currentReturnType
	oldFunctionName := c.currentFunctionName

	c.currentReturnType = stmt.ReturnType
	c.currentFunctionName = stmt.Name

	defer func() {
		c.currentReturnType = oldReturnType
		c.currentFunctionName = oldFunctionName
	}()

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	if !stmt.ReturnType.IsEmpty() && stmt.ReturnType.Name != "any" && stmt.ReturnType.Name != "null" {
		if !alwaysReturnsOrThrowsBlock(stmt.Body) {
			c.setLocation(stmt.File, stmt.Line, stmt.Column)
			c.fatalError(ErrorType, "missing return: function '%s' expects return type '%s'", stmt.Name, stmt.ReturnType.Name)
		}
	}

	c.emit(OP_CONST, NewNull())
	c.emit(OP_RETURN, nil)

	localCount := c.localCount

	c.functions[stmt.Name] = Function{
		ID:             c.getFunctionID(stmt.Name),
		Name:           stmt.Name,
		TypeParameters: stmt.TypeParameters,
		Params:         stmt.Params,
		Instructions:   functionInstructions,
		DebugInfo:      functionDebugInfo,
		StatementCount: len(stmt.Body),
		LocalCount:     localCount,
		HasDefaults:    hasDefaults,
		HasTypeHints:   hasTypeHints,
		ReturnType:     stmt.ReturnType,
		Async:          stmt.Async,
	}

	c.currentInstructions = oldInstructions
	c.currentDebugInfo = oldDebugInfo
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
			result[name] = binding
		}
	}

	if c.outerBindings != nil {
		for name, binding := range c.outerBindings {
			if _, exists := result[name]; exists {
				continue
			}

			if binding.Kind == BindingGlobal {
				result[name] = binding
				continue
			}

			captured, ok := c.ensureCaptured(name)
			if ok {
				result[name] = captured
				continue
			}

			result[name] = binding
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
			info.Slot = binding.Slot
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

func jitRegionDebugf(format string, args ...any) {
	if os.Getenv("TINY_JIT_REGION_DEBUG") != "1" {
		return
	}
	fmt.Fprintf(os.Stderr, "[JIT REGION] "+format+"\n", args...)
}

func jitRegionStdMemberCallType(e MemberCallExpr, stdImportModules map[string]string) (string, bool) {
	if e.Safe || stdImportModules == nil {
		return "", false
	}

	ident, ok := e.Object.(IdentExpr)
	if !ok {
		return "", false
	}

	switch stdImportModules[ident.Name] {
	case "math":
		switch e.Method {
		case "floor", "ceil", "sqrt", "abs":
			return "number", len(e.Args) == 1
		case "pow":
			return "number", len(e.Args) == 2
		}

	case "strings":
		switch e.Method {
		case "isDigit":
			return "bool", len(e.Args) == 1
		case "random":
			return "string", len(e.Args) == 1
		}
	}

	return "", false
}

func jitRegionStdMemberCallArgType(e MemberCallExpr, argIndex int, stdImportModules map[string]string) string {
	ident, ok := e.Object.(IdentExpr)
	if !ok || argIndex < 0 {
		return ""
	}

	switch stdImportModules[ident.Name] {
	case "math":
		return "number"
	case "strings":
		switch e.Method {
		case "isDigit":
			return "string"
		case "random":
			return "number"
		}
	}

	return ""
}

func jitRegionNumericStdMemberCall(e MemberCallExpr, stdImportModules map[string]string) bool {
	typ, ok := jitRegionStdMemberCallType(e, stdImportModules)
	return ok && typ == "number"
}

func jitRegionSupportedMethodCall(e MemberCallExpr) bool {
	if e.Safe {
		return false
	}
	switch e.Method {
	case "length":
		return len(e.Args) == 0
	case "get", "push":
		return len(e.Args) == 1
	default:
		return false
	}
}

type jitRegionCandidate struct {
	start        int
	end          int
	helper       FunctionStmt
	replacements []Stmt
}

func (c *Compiler) registerOutlinedJitFunctionForInference(fn FunctionStmt) {
	if c.inlineCandidates != nil {
		c.inlineCandidates[fn.Name] = fn
	}
	if c.declaredFunctions != nil {
		c.declaredFunctions[fn.Name] = true
	}
	if strings.HasPrefix(fn.Name, "__jit_region_") && c.usedFunctions != nil {
		c.usedFunctions[fn.Name] = true
	}
}

func (c *Compiler) registerOutlinedJitFunctionsForInference(helpers []Stmt, rewritten FunctionStmt) {
	for _, helper := range helpers {
		if fn, ok := helper.(FunctionStmt); ok {
			c.registerOutlinedJitFunctionForInference(fn)
		}
	}
	c.registerOutlinedJitFunctionForInference(rewritten)
}

func (c *Compiler) outlineJitRegionsInStatements(stmts []Stmt) []Stmt {
	outlined := make([]Stmt, 0, len(stmts))

	// Track simple top-level value types so main-code loops can be outlined too.
	// Previously only FunctionStmt bodies were visited, so a hot top-level loop like:
	//
	//   for let i = 0; i < 5000; i++ { final_result = aggregate(logs_data) }
	//
	// stayed in the interpreter and paid the VM -> Wazero call boundary thousands of
	// times. This map is intentionally conservative: if a type cannot be proven as a
	// JIT value type, the loop is simply left alone.
	mainKnownTypes := map[string]string{}

	for i := 0; i < len(stmts); i++ {
		stmt := stmts[i]
		switch s := stmt.(type) {
		case FunctionStmt:
			helpers, rewritten := c.outlineJitRegionsInFunction(s)
			// Important: main-region outlining later in this same pass needs the
			// rewritten function body and any helper return types. Without this,
			// `let logs_data = generate_logs()` still sees the old unoutlined
			// generate_logs body and cannot infer array, so the top-level timed
			// loop rejects `logs_data` as an unknown live-in.
			c.registerOutlinedJitFunctionsForInference(helpers, rewritten)
			outlined = append(outlined, helpers...)
			outlined = append(outlined, rewritten)
			continue

		case ExportStmt:
			fn, ok := s.Inner.(FunctionStmt)
			if !ok {
				outlined = append(outlined, stmt)
				continue
			}
			helpers, rewritten := c.outlineJitRegionsInFunction(fn)
			c.registerOutlinedJitFunctionsForInference(helpers, rewritten)
			outlined = append(outlined, helpers...)
			s.Inner = rewritten
			outlined = append(outlined, s)
			continue
		}

		if _, ok := stmt.(WhileStmt); ok {
			jitRegionDebugf("candidate fn=<main> kind=while index=%d known=%v", i, mainKnownTypes)
			mainFn := FunctionStmt{Name: "main", Body: stmts}
			if candidate, ok := c.tryBuildJitRegionCandidate(mainFn, stmts, i, mainKnownTypes); ok {
				jitRegionDebugf("outline fn=<main> helper=%s start=%d end=%d", candidate.helper.Name, candidate.start, candidate.end)
				if setupCount := i - candidate.start; setupCount > 0 {
					outlined = outlined[:len(outlined)-setupCount]
				}
				c.registerOutlinedJitFunctionForInference(candidate.helper)
				outlined = append(outlined, candidate.helper)
				outlined = append(outlined, candidate.replacements...)
				for name, typ := range jitRegionDeclaredScalarTypesKnown(stmts[candidate.start:candidate.end], mainKnownTypes, c.stdImportModules) {
					mainKnownTypes[name] = typ
				}
				i = candidate.end - 1
				continue
			}
			if candidate, ok := c.tryBuildJitMainSingleLoopCandidate(mainFn, stmts, i, mainKnownTypes); ok {
				jitRegionDebugf("outline-loose fn=<main> helper=%s start=%d end=%d", candidate.helper.Name, candidate.start, candidate.end)
				c.registerOutlinedJitFunctionForInference(candidate.helper)
				outlined = append(outlined, candidate.helper)
				outlined = append(outlined, candidate.replacements...)
				i = candidate.end - 1
				continue
			}
		}

		if _, ok := stmt.(ForStmt); ok {
			jitRegionDebugf("candidate fn=<main> kind=for index=%d known=%v", i, mainKnownTypes)
			mainFn := FunctionStmt{Name: "main", Body: stmts}
			if candidate, ok := c.tryBuildJitRegionCandidate(mainFn, stmts, i, mainKnownTypes); ok {
				jitRegionDebugf("outline fn=<main> helper=%s start=%d end=%d", candidate.helper.Name, candidate.start, candidate.end)
				if setupCount := i - candidate.start; setupCount > 0 {
					outlined = outlined[:len(outlined)-setupCount]
				}
				c.registerOutlinedJitFunctionForInference(candidate.helper)
				outlined = append(outlined, candidate.helper)
				outlined = append(outlined, candidate.replacements...)
				for name, typ := range jitRegionDeclaredScalarTypesKnown(stmts[candidate.start:candidate.end], mainKnownTypes, c.stdImportModules) {
					mainKnownTypes[name] = typ
				}
				i = candidate.end - 1
				continue
			}
			if candidate, ok := c.tryBuildJitMainSingleLoopCandidate(mainFn, stmts, i, mainKnownTypes); ok {
				jitRegionDebugf("outline-loose fn=<main> helper=%s start=%d end=%d", candidate.helper.Name, candidate.start, candidate.end)
				c.registerOutlinedJitFunctionForInference(candidate.helper)
				outlined = append(outlined, candidate.helper)
				outlined = append(outlined, candidate.replacements...)
				i = candidate.end - 1
				continue
			}
		}

		outlined = append(outlined, stmt)
		if variable, ok := stmt.(VariableStmt); ok {
			if typ, ok := c.inferJitMainVariableType(variable, mainKnownTypes); ok {
				mainKnownTypes[variable.Name] = typ
				jitRegionDebugf("main-known name=%s type=%s", variable.Name, typ)
			} else {
				jitRegionDebugf("main-known-miss name=%s known=%v", variable.Name, mainKnownTypes)
			}
		}
	}
	return outlined
}

func (c *Compiler) outlineJitRegionsInFunction(fn FunctionStmt) ([]Stmt, FunctionStmt) {
	jitRegionDebugf("visit fn=%s body=%d async=%v typeParams=%d", fn.Name, len(fn.Body), fn.Async, len(fn.TypeParameters))
	if fn.Async || len(fn.TypeParameters) > 0 || strings.HasPrefix(fn.Name, "__jit_region_") {
		jitRegionDebugf("skip fn=%s reason=async/generic/helper", fn.Name)
		return nil, fn
	}
	for _, stmt := range fn.Body {
		if ret, ok := stmt.(ReturnStmt); ok && ret.HasValue {
			if _, ok := ret.Value.(ObjectExpr); ok {
				jitRegionDebugf("skip fn=%s reason=direct-object-return", fn.Name)
				return nil, fn
			}
		}
	}
	for _, param := range fn.Params {
		if param.HasDefault || param.Variadic {
			jitRegionDebugf("skip fn=%s reason=default-or-variadic-param param=%s", fn.Name, param.Name)
			return nil, fn
		}
	}

	knownTypes := map[string]string{}
	for _, param := range fn.Params {
		if isJitRegionValueType(param.TypeHint.Name) {
			knownTypes[param.Name] = param.TypeHint.Name
		}
	}

	helpers := []Stmt{}
	body := make([]Stmt, 0, len(fn.Body))
	for i := 0; i < len(fn.Body); i++ {
		stmt := fn.Body[i]
		if _, ok := stmt.(WhileStmt); ok {
			jitRegionDebugf("candidate fn=%s kind=while index=%d known=%v", fn.Name, i, knownTypes)
			if candidate, ok := c.tryBuildJitRegionCandidate(fn, fn.Body, i, knownTypes); ok {
				jitRegionDebugf("outline fn=%s helper=%s start=%d end=%d", fn.Name, candidate.helper.Name, candidate.start, candidate.end)
				helpers = append(helpers, candidate.helper)
				if setupCount := i - candidate.start; setupCount > 0 {
					body = body[:len(body)-setupCount]
				}
				body = append(body, candidate.replacements...)
				for name, typ := range jitRegionDeclaredScalarTypesKnown(fn.Body[candidate.start:candidate.end], knownTypes, c.stdImportModules) {
					knownTypes[name] = typ
				}
				i = candidate.end - 1
				continue
			}
		}
		if _, ok := stmt.(ForStmt); ok {
			jitRegionDebugf("candidate fn=%s kind=for index=%d known=%v", fn.Name, i, knownTypes)
			if candidate, ok := c.tryBuildJitRegionCandidate(fn, fn.Body, i, knownTypes); ok {
				jitRegionDebugf("outline fn=%s helper=%s start=%d end=%d", fn.Name, candidate.helper.Name, candidate.start, candidate.end)
				helpers = append(helpers, candidate.helper)
				if setupCount := i - candidate.start; setupCount > 0 {
					body = body[:len(body)-setupCount]
				}
				body = append(body, candidate.replacements...)
				for name, typ := range jitRegionDeclaredScalarTypesKnown(fn.Body[candidate.start:candidate.end], knownTypes, c.stdImportModules) {
					knownTypes[name] = typ
				}
				i = candidate.end - 1
				continue
			}
		}

		body = append(body, stmt)
		if variable, ok := stmt.(VariableStmt); ok {
			if typ, ok := inferJitRegionVariableTypeKnown(variable, knownTypes, c.stdImportModules); ok {
				knownTypes[variable.Name] = typ
			}
		}
	}

	fn.Body = body
	return helpers, fn
}

func (c *Compiler) tryBuildJitRegionCandidate(fn FunctionStmt, body []Stmt, loopIndex int, knownTypes map[string]string) (jitRegionCandidate, bool) {
	reject := func(reason string, args ...any) (jitRegionCandidate, bool) {
		jitRegionDebugf("reject setup fn=%s loop=%d reason="+reason, append([]any{fn.Name, loopIndex}, args...)...)
		return jitRegionCandidate{}, false
	}
	start := loopIndex
	for start > 0 {
		variable, ok := body[start-1].(VariableStmt)
		if !ok || variable.Constant || !jitRegionExprSafe(variable.Value, c.stdImportModules) {
			break
		}
		if _, ok := inferJitRegionVariableTypeKnown(variable, knownTypes, c.stdImportModules); !ok {
			break
		}
		start--
	}
	if start == loopIndex {
		jitRegionDebugf("setup fallback fn=%s loop=%d reason=no-setup-vars", fn.Name, loopIndex)
		return c.tryBuildJitLoopOnlyRegionCandidate(fn, body, loopIndex, knownTypes)
	}

	region := body[start : loopIndex+1]
	if !jitRegionStatementsSafe(region, true, c.stdImportModules) {
		return reject("region-not-safe")
	}

	setupTypes := jitRegionDeclaredScalarTypesKnown(region[:loopIndex-start], knownTypes, c.stdImportModules)
	setupNames := map[string]bool{}
	for name := range setupTypes {
		setupNames[name] = true
	}
	regionDeclaredNames := jitRegionDeclaredNames(region)

	afterUses := map[string]bool{}
	for _, stmt := range body[loopIndex+1:] {
		collectJitRegionStmtUses(stmt, afterUses, c.stdImportModules)
	}

	escapingSetup := []string{}
	for name := range setupNames {
		if afterUses[name] {
			escapingSetup = append(escapingSetup, name)
		}
	}
	if len(escapingSetup) == 0 {
		return reject("escaping-setup-count=%d escaping=%v setup=%v afterUses=%v", len(escapingSetup), escapingSetup, setupNames, afterUses)
	}
	sort.Strings(escapingSetup)
	for _, liveOut := range escapingSetup {
		liveOutType := setupTypes[liveOut]
		if !isJitRegionValueType(liveOutType) {
			return reject("liveout-not-supported name=%s type=%s", liveOut, liveOutType)
		}
	}

	used := map[string]bool{}
	for _, stmt := range region {
		collectJitRegionStmtUses(stmt, used, c.stdImportModules)
	}
	liveIns := []string{}
	regionKnownTypes := maps.Clone(knownTypes)
	for name, typ := range setupTypes {
		regionKnownTypes[name] = typ
	}

	for name := range used {
		if regionDeclaredNames[name] {
			continue
		}

		typ, ok := knownTypes[name]
		if !ok || !isJitRegionValueType(typ) {
			if inferred, inferredOK := inferJitRegionLiveInType(name, region, regionKnownTypes, c.stdImportModules); inferredOK {
				typ = inferred
				knownTypes[name] = inferred
				regionKnownTypes[name] = inferred
				ok = true
			}
		}

		if !ok || !isJitRegionValueType(typ) {
			return reject("livein-unknown name=%s known=%v", name, knownTypes)
		}

		liveIns = append(liveIns, name)
	}
	sort.Strings(liveIns)

	params := make([]Param, 0, len(liveIns))
	args := make([]Expr, 0, len(liveIns))
	for _, name := range liveIns {
		params = append(params, Param{Name: name, TypeHint: TypeHint{Name: knownTypes[name]}})
		args = append(args, IdentExpr{Name: name})
	}

	helperName := c.nextJitRegionName(fn.Name)
	jitRegionDebugf("make helper=%s fn=%s liveOuts=%v liveIns=%v params=%d", helperName, fn.Name, escapingSetup, liveIns, len(liveIns))
	if c.usedFunctions != nil {
		c.usedFunctions[helperName] = true
	}
	if c.declaredFunctions != nil {
		c.declaredFunctions[helperName] = true
	}
	helperBody := append([]Stmt{}, region...)
	returnType := TypeHint{Name: setupTypes[escapingSetup[0]]}
	replacements := []Stmt{}
	if len(escapingSetup) == 1 {
		liveOut := escapingSetup[0]
		liveOutType := setupTypes[liveOut]
		helperBody = append(helperBody, ReturnStmt{
			Value:    IdentExpr{Name: liveOut},
			HasValue: true,
		})
		replacements = append(replacements, VariableStmt{
			Name:     liveOut,
			Value:    CallExpr{Name: helperName, Args: args},
			Constant: false,
			TypeHint: TypeHint{Name: liveOutType},
		})
	} else {
		fields := make([]ObjectField, 0, len(escapingSetup))
		for _, liveOut := range escapingSetup {
			fields = append(fields, ObjectField{
				Name:  liveOut,
				Value: IdentExpr{Name: liveOut},
			})
		}
		helperBody = append(helperBody, ReturnStmt{
			Value:    ObjectExpr{Fields: fields},
			HasValue: true,
		})
		returnType = TypeHint{Name: "object"}

		resultName := fmt.Sprintf("__jit_region_result_%d", c.jitRegionCount)
		replacements = append(replacements, VariableStmt{
			Name:     resultName,
			Value:    CallExpr{Name: helperName, Args: args},
			Constant: false,
		})
		for _, liveOut := range escapingSetup {
			replacements = append(replacements, VariableStmt{
				Name: liveOut,
				Value: PropertyExpr{
					Object: IdentExpr{Name: resultName},
					Name:   liveOut,
				},
				Constant: false,
				TypeHint: TypeHint{Name: setupTypes[liveOut]},
			})
		}
	}

	helper := FunctionStmt{
		Name:       helperName,
		Params:     params,
		ReturnType: returnType,
		Body:       helperBody,
		File:       fn.File,
		Line:       fn.Line,
		Column:     fn.Column,
	}

	return jitRegionCandidate{
		start:        start,
		end:          loopIndex + 1,
		helper:       helper,
		replacements: replacements,
	}, true
}

func (c *Compiler) tryBuildJitMainSingleLoopCandidate(fn FunctionStmt, body []Stmt, loopIndex int, knownTypes map[string]string) (jitRegionCandidate, bool) {
	reject := func(reason string, args ...any) (jitRegionCandidate, bool) {
		jitRegionDebugf("reject main-single-loop fn=%s loop=%d reason="+reason, append([]any{fn.Name, loopIndex}, args...)...)
		return jitRegionCandidate{}, false
	}

	region := body[loopIndex : loopIndex+1]
	if !jitRegionStatementsSafe(region, true, c.stdImportModules) {
		return reject("region-not-safe")
	}

	regionDeclaredNames := jitRegionDeclaredNames(region)
	assigned := map[string]bool{}
	for _, stmt := range region {
		collectJitRegionAssignedNames(stmt, assigned)
	}

	liveOuts := make([]string, 0, 1)
	for name := range assigned {
		if regionDeclaredNames[name] {
			continue
		}
		typ, ok := knownTypes[name]
		if !ok || !isJitRegionValueType(typ) {
			if inferred, inferredOK := inferJitRegionLiveInType(name, region, maps.Clone(knownTypes), c.stdImportModules); inferredOK {
				typ = inferred
				knownTypes[name] = inferred
				ok = true
			}
		}
		if ok && isJitRegionValueType(typ) {
			liveOuts = append(liveOuts, name)
		}
	}
	if len(liveOuts) != 1 {
		return reject("liveout-count=%d liveouts=%v assigned=%v declared=%v known=%v", len(liveOuts), liveOuts, assigned, regionDeclaredNames, knownTypes)
	}
	sort.Strings(liveOuts)
	liveOut := liveOuts[0]
	liveOutType := knownTypes[liveOut]
	if !isJitRegionValueType(liveOutType) {
		return reject("liveout-type-unknown name=%s type=%s known=%v", liveOut, liveOutType, knownTypes)
	}

	used := map[string]bool{}
	for _, stmt := range region {
		collectJitRegionStmtUses(stmt, used, c.stdImportModules)
	}

	liveIns := []string{}
	regionKnownTypes := maps.Clone(knownTypes)
	for name := range used {
		if regionDeclaredNames[name] {
			continue
		}
		typ, ok := knownTypes[name]
		if !ok || !isJitRegionValueType(typ) {
			if inferred, inferredOK := inferJitRegionLiveInType(name, region, regionKnownTypes, c.stdImportModules); inferredOK {
				typ = inferred
				knownTypes[name] = inferred
				regionKnownTypes[name] = inferred
				ok = true
			}
		}
		if !ok || !isJitRegionValueType(typ) {
			return reject("livein-unknown name=%s used=%v known=%v", name, used, knownTypes)
		}
		liveIns = append(liveIns, name)
	}

	if !regionDeclaredNames[liveOut] {
		found := false
		for _, name := range liveIns {
			if name == liveOut {
				found = true
				break
			}
		}
		if !found {
			liveIns = append(liveIns, liveOut)
		}
	}
	sort.Strings(liveIns)

	params := make([]Param, 0, len(liveIns))
	args := make([]Expr, 0, len(liveIns))
	for _, name := range liveIns {
		typ := knownTypes[name]
		if !isJitRegionValueType(typ) {
			return reject("param-type-unknown name=%s type=%s known=%v", name, typ, knownTypes)
		}
		params = append(params, Param{Name: name, TypeHint: TypeHint{Name: typ}})
		args = append(args, IdentExpr{Name: name})
	}

	helperName := c.nextJitRegionName(fn.Name)
	jitRegionDebugf("make-main-single helper=%s liveOut=%s type=%s liveIns=%v known=%v", helperName, liveOut, liveOutType, liveIns, knownTypes)
	if c.usedFunctions != nil {
		c.usedFunctions[helperName] = true
	}
	if c.declaredFunctions != nil {
		c.declaredFunctions[helperName] = true
	}

	helperBody := append([]Stmt{}, region...)
	helperBody = append(helperBody, ReturnStmt{Value: IdentExpr{Name: liveOut}, HasValue: true})

	helper := FunctionStmt{
		Name:       helperName,
		Params:     params,
		ReturnType: TypeHint{Name: liveOutType},
		Body:       helperBody,
		File:       fn.File,
		Line:       fn.Line,
		Column:     fn.Column,
	}

	return jitRegionCandidate{
		start:        loopIndex,
		end:          loopIndex + 1,
		helper:       helper,
		replacements: []Stmt{AssignStmt{Name: liveOut, Value: CallExpr{Name: helperName, Args: args}}},
	}, true
}

func (c *Compiler) tryBuildJitLoopOnlyRegionCandidate(fn FunctionStmt, body []Stmt, loopIndex int, knownTypes map[string]string) (jitRegionCandidate, bool) {
	reject := func(reason string, args ...any) (jitRegionCandidate, bool) {
		jitRegionDebugf("reject loop-only fn=%s loop=%d reason="+reason, append([]any{fn.Name, loopIndex}, args...)...)
		return jitRegionCandidate{}, false
	}
	region := body[loopIndex : loopIndex+1]
	if !jitRegionStatementsSafe(region, true, c.stdImportModules) {
		return reject("region-not-safe")
	}

	regionDeclaredNames := jitRegionDeclaredNames(region)

	afterUses := map[string]bool{}
	for _, stmt := range body[loopIndex+1:] {
		collectJitRegionStmtUses(stmt, afterUses, c.stdImportModules)
	}

	assigned := map[string]bool{}
	for _, stmt := range region {
		collectJitRegionAssignedNames(stmt, assigned)
	}

	liveOuts := []string{}
	for name := range assigned {
		if regionDeclaredNames[name] {
			continue
		}
		if !afterUses[name] {
			continue
		}
		typ, ok := knownTypes[name]
		if !ok || !isJitRegionValueType(typ) {
			return reject("liveout-unknown name=%s known=%v", name, knownTypes)
		}
		liveOuts = append(liveOuts, name)
	}
	if len(liveOuts) != 1 {
		return reject("liveout-count=%d liveouts=%v assigned=%v afterUses=%v declared=%v known=%v", len(liveOuts), liveOuts, assigned, afterUses, regionDeclaredNames, knownTypes)
	}
	sort.Strings(liveOuts)
	liveOut := liveOuts[0]
	liveOutType := knownTypes[liveOut]
	if !isJitRegionValueType(liveOutType) {
		if inferred, ok := inferJitRegionLiveInType(liveOut, region, knownTypes, c.stdImportModules); ok {
			liveOutType = inferred
			knownTypes[liveOut] = liveOutType
		} else {
			return reject("liveout-unknown name=%s known=%v", liveOut, knownTypes)
		}
	}

	used := map[string]bool{}
	for _, stmt := range region {
		collectJitRegionStmtUses(stmt, used, c.stdImportModules)
	}

	liveIns := []string{}
	regionKnownTypes := maps.Clone(knownTypes)
	for name := range used {
		if regionDeclaredNames[name] {
			continue
		}

		typ, ok := knownTypes[name]
		if !ok || !isJitRegionValueType(typ) {
			if inferred, inferredOK := inferJitRegionLiveInType(name, region, regionKnownTypes, c.stdImportModules); inferredOK {
				typ = inferred
				knownTypes[name] = inferred
				regionKnownTypes[name] = inferred
				ok = true
			}
		}

		if !ok || !isJitRegionValueType(typ) {
			return reject("livein-unknown name=%s known=%v", name, knownTypes)
		}

		liveIns = append(liveIns, name)
	}

	// If the loop assigns an outer variable without reading it first, it still has
	// to exist inside the generated helper. Passing it as a parameter preserves the
	// binding and avoids compiling an assignment to an undeclared local. The value
	// may be unused by the loop, but the extra argument is tiny compared with the
	// cost of leaving the whole outer loop in the interpreter.
	if !regionDeclaredNames[liveOut] {
		foundLiveOutParam := false
		for _, name := range liveIns {
			if name == liveOut {
				foundLiveOutParam = true
				break
			}
		}
		if !foundLiveOutParam {
			liveIns = append(liveIns, liveOut)
		}
	}
	sort.Strings(liveIns)

	params := make([]Param, 0, len(liveIns))
	args := make([]Expr, 0, len(liveIns))
	for _, name := range liveIns {
		params = append(params, Param{Name: name, TypeHint: TypeHint{Name: knownTypes[name]}})
		args = append(args, IdentExpr{Name: name})
	}

	helperName := c.nextJitRegionName(fn.Name)
	jitRegionDebugf("make helper=%s fn=%s liveOut=%s liveIns=%v params=%d", helperName, fn.Name, liveOut, liveIns, len(liveIns))
	if c.usedFunctions != nil {
		c.usedFunctions[helperName] = true
	}
	if c.declaredFunctions != nil {
		c.declaredFunctions[helperName] = true
	}

	helperBody := append([]Stmt{}, region...)
	helperBody = append(helperBody, ReturnStmt{
		Value:    IdentExpr{Name: liveOut},
		HasValue: true,
	})

	replacement := AssignStmt{
		Name:  liveOut,
		Value: CallExpr{Name: helperName, Args: args},
	}

	helper := FunctionStmt{
		Name:       helperName,
		Params:     params,
		ReturnType: TypeHint{Name: liveOutType},
		Body:       helperBody,
		File:       fn.File,
		Line:       fn.Line,
		Column:     fn.Column,
	}

	return jitRegionCandidate{
		start:        loopIndex,
		end:          loopIndex + 1,
		helper:       helper,
		replacements: []Stmt{replacement},
	}, true
}

func (c *Compiler) nextJitRegionName(parent string) string {
	c.jitRegionCount++
	cleanParent := strings.NewReplacer(".", "_", ":", "_", "-", "_").Replace(parent)
	return fmt.Sprintf("__jit_region_%s_%d", cleanParent, c.jitRegionCount)
}

func inferJitRegionVariableType(stmt VariableStmt) (string, bool) {
	if isJitRegionValueType(stmt.TypeHint.Name) {
		return stmt.TypeHint.Name, true
	}
	return inferJitRegionLiteralType(stmt.Value)
}

func inferJitRegionVariableTypeKnown(stmt VariableStmt, knownTypes map[string]string, stdImportModules map[string]string) (string, bool) {
	if isJitRegionValueType(stmt.TypeHint.Name) {
		return stmt.TypeHint.Name, true
	}
	if typ, ok := inferJitRegionLiteralType(stmt.Value); ok {
		return typ, true
	}
	return inferJitRegionExprType(stmt.Value, knownTypes, stdImportModules)
}

// inferJitMainVariableType is the top-level equivalent of
// inferJitRegionVariableTypeKnown, but it can use the compiler's predeclared
// function table / inlineCandidates to infer direct-call return types. This is
// what lets `let logs_data = generate_logs()` become `array` instead of the old
// bogus generic `number` fallback used by region expression inference.
func (c *Compiler) inferJitMainVariableType(stmt VariableStmt, knownTypes map[string]string) (string, bool) {
	if isJitRegionValueType(stmt.TypeHint.Name) {
		return stmt.TypeHint.Name, true
	}
	if typ, ok := inferJitRegionLiteralType(stmt.Value); ok {
		return typ, true
	}
	return c.inferJitMainExprType(stmt.Value, knownTypes)
}

func (c *Compiler) inferJitMainExprType(expr Expr, knownTypes map[string]string) (string, bool) {
	switch e := expr.(type) {
	case CallExpr:
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, c.stdImportModules) {
				return "", false
			}
		}
		if typ, ok := c.inferJitDirectCallReturnType(e.Name, e.Args, knownTypes); ok {
			jitRegionDebugf("infer-call-return name=%s source=call-expr type=%s", e.Name, typ)
			return typ, true
		}
		return "", false
	case CallValueExpr:
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, c.stdImportModules) {
				return "", false
			}
		}
		name, ok := c.resolveJitDirectCalleeName(e.Callee)
		if !ok {
			return "", false
		}
		if typ, ok := c.inferJitDirectCallReturnType(name, e.Args, knownTypes); ok {
			jitRegionDebugf("infer-call-return name=%s source=call-value type=%s", name, typ)
			return typ, true
		}
		return "", false
	default:
		return inferJitRegionExprType(expr, knownTypes, c.stdImportModules)
	}
}

func (c *Compiler) resolveJitDirectCalleeName(callee Expr) (string, bool) {
	name, ok := c.resolveFullyQualifiedName(callee)
	if !ok || name == "" {
		return "", false
	}
	return name, true
}

func (c *Compiler) inferJitDirectCallReturnType(name string, args []Expr, callerKnownTypes map[string]string) (string, bool) {
	if fn, ok := c.inlineCandidates[name]; ok {
		if isJitRegionValueType(fn.ReturnType.Name) && fn.ReturnType.Name != "any" {
			return fn.ReturnType.Name, true
		}
		return c.inferJitFunctionReturnType(fn, args, callerKnownTypes)
	}
	if fn, ok := c.functions[name]; ok {
		if isJitRegionValueType(fn.ReturnType.Name) && fn.ReturnType.Name != "any" {
			return fn.ReturnType.Name, true
		}
	}
	return "", false
}

func (c *Compiler) inferJitFunctionReturnType(fn FunctionStmt, args []Expr, callerKnownTypes map[string]string) (string, bool) {
	if fn.Async || len(fn.TypeParameters) > 0 || len(args) != len(fn.Params) {
		return "", false
	}

	known := map[string]string{}
	for i, param := range fn.Params {
		if isJitRegionValueType(param.TypeHint.Name) {
			known[param.Name] = param.TypeHint.Name
			continue
		}
		if typ, ok := c.inferJitMainExprType(args[i], callerKnownTypes); ok && isJitRegionValueType(typ) {
			known[param.Name] = typ
		}
	}

	var scanStmt func(Stmt) (string, bool)
	scanStmt = func(stmt Stmt) (string, bool) {
		switch s := stmt.(type) {
		case VariableStmt:
			if typ, ok := c.inferJitMainVariableTypeWithKnown(s, known); ok {
				known[s.Name] = typ
			}
		case AssignStmt:
			if typ, ok := c.inferJitMainExprTypeWithKnown(s.Value, known); ok {
				known[s.Name] = typ
			}
		case ReturnStmt:
			if !s.HasValue {
				return "null", true
			}
			return c.inferJitMainExprTypeWithKnown(s.Value, known)
		case IfStmt:
			// Only infer from branches when both branches return the same proven type.
			thenType, thenOK := scanStmtListFirstReturn(c, s.ThenBody, maps.Clone(known))
			elseType, elseOK := scanStmtListFirstReturn(c, s.ElseBody, maps.Clone(known))
			if thenOK && elseOK && thenType == elseType {
				return thenType, true
			}
		case ForStmt, WhileStmt, ForInStmt:
			// Loops can mutate locals, but for return-type inference we only need the
			// simple common case: variables declared before the loop and returned after.
			return "", false
		}
		return "", false
	}

	for _, stmt := range fn.Body {
		if typ, ok := scanStmt(stmt); ok {
			if isJitRegionValueType(typ) {
				return typ, true
			}
			return "", false
		}
	}
	return "", false
}

func scanStmtListFirstReturn(c *Compiler, body []Stmt, known map[string]string) (string, bool) {
	for _, stmt := range body {
		switch s := stmt.(type) {
		case VariableStmt:
			if typ, ok := c.inferJitMainVariableTypeWithKnown(s, known); ok {
				known[s.Name] = typ
			}
		case AssignStmt:
			if typ, ok := c.inferJitMainExprTypeWithKnown(s.Value, known); ok {
				known[s.Name] = typ
			}
		case ReturnStmt:
			if !s.HasValue {
				return "null", true
			}
			return c.inferJitMainExprTypeWithKnown(s.Value, known)
		}
	}
	return "", false
}

func (c *Compiler) inferJitMainVariableTypeWithKnown(stmt VariableStmt, knownTypes map[string]string) (string, bool) {
	if isJitRegionValueType(stmt.TypeHint.Name) {
		return stmt.TypeHint.Name, true
	}
	if typ, ok := inferJitRegionLiteralType(stmt.Value); ok {
		return typ, true
	}
	return c.inferJitMainExprTypeWithKnown(stmt.Value, knownTypes)
}

func (c *Compiler) inferJitMainExprTypeWithKnown(expr Expr, knownTypes map[string]string) (string, bool) {
	switch e := expr.(type) {
	case CallExpr:
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, c.stdImportModules) {
				return "", false
			}
		}
		return c.inferJitDirectCallReturnType(e.Name, e.Args, knownTypes)
	case CallValueExpr:
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, c.stdImportModules) {
				return "", false
			}
		}
		name, ok := c.resolveJitDirectCalleeName(e.Callee)
		if !ok {
			return "", false
		}
		return c.inferJitDirectCallReturnType(name, e.Args, knownTypes)
	default:
		return inferJitRegionExprType(expr, knownTypes, c.stdImportModules)
	}
}

func inferJitRegionExprType(expr Expr, knownTypes map[string]string, stdImportModules map[string]string) (string, bool) {
	switch e := expr.(type) {
	case NumberExpr, FloatExpr:
		return "number", true
	case StringExpr:
		return "string", true
	case BoolExpr:
		return "bool", true
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy {
				return "", false
			}
			if !jitRegionExprSafe(field.Value, stdImportModules) {
				return "", false
			}
		}
		return "object", true
	case ArrayExpr:
		elemType := ""
		for _, elem := range e.Elements {
			typ, ok := inferJitRegionExprType(elem, knownTypes, stdImportModules)
			if !ok {
				if !jitRegionExprSafe(elem, stdImportModules) {
					return "", false
				}
				elemType = ""
				continue
			}
			if elemType == "" {
				elemType = typ
			} else if elemType != typ {
				elemType = ""
			}
		}
		if elemType != "" {
			return jitRegionArrayTypeFromElement(elemType), true
		}
		return "array", true
	case IdentExpr:
		typ, ok := knownTypes[e.Name]
		if ok && isJitRegionValueType(typ) {
			return typ, true
		}
		return "", false
	case UnaryExpr:
		if e.Op == TOKEN_MINUS {
			typ, ok := inferJitRegionExprType(e.Right, knownTypes, stdImportModules)
			return typ, ok && typ == "number"
		}
		if e.Op == TOKEN_TILDE {
			typ, ok := inferJitRegionExprType(e.Right, knownTypes, stdImportModules)
			return typ, ok && typ == "number"
		}
		if e.Op == TOKEN_BANG {
			typ, ok := inferJitRegionExprType(e.Right, knownTypes, stdImportModules)
			if ok && typ == "bool" {
				return "bool", true
			}
		}
		return "", false
	case BinaryExpr:
		left, leftOK := inferJitRegionExprType(e.Left, knownTypes, stdImportModules)
		right, rightOK := inferJitRegionExprType(e.Right, knownTypes, stdImportModules)
		if !leftOK || !rightOK {
			return "", false
		}
		switch e.Op {
		case TOKEN_PLUS, TOKEN_MINUS, TOKEN_STAR, TOKEN_SLASH, TOKEN_PERCENT:
			if e.Op == TOKEN_PLUS && (left == "string" || right == "string") {
				return "string", true
			}
			if left == "number" && right == "number" {
				return "number", true
			}
		case TOKEN_LT, TOKEN_LTE, TOKEN_GT, TOKEN_GTE, TOKEN_EQ, TOKEN_NEQ:
			if left == right {
				return "bool", true
			}
		case TOKEN_AND, TOKEN_OR:
			if left == "bool" && right == "bool" {
				return "bool", true
			}
		case TOKEN_AMP, TOKEN_PIPE, TOKEN_CARET, TOKEN_LSHIFT, TOKEN_RSHIFT:
			if left == "number" && right == "number" {
				return "number", true
			}
		}
		return "", false
	case TernaryExpr:
		thenType, thenOK := inferJitRegionExprType(e.ThenExpr, knownTypes, stdImportModules)
		elseType, elseOK := inferJitRegionExprType(e.ElseExpr, knownTypes, stdImportModules)
		if thenOK && elseOK && thenType == elseType {
			return thenType, true
		}
	case CallExpr:
		// Do not invent a numeric return type for direct calls here. This helper has
		// no access to the compiler's function table, so guessing "number" turns
		// values like `generate_logs()` into bogus number-typed live-ins. Compiler
		// methods such as inferJitMainExprType / inferJitDirectCallReturnType handle
		// real direct-call return inference when function metadata is available.
		return "", false
	case CallValueExpr:
		// Function-valued calls are not knowable here. Keep them conservative.
		return "", false
	case MemberCallExpr:
		if retType, ok := jitRegionStdMemberCallType(e, stdImportModules); ok {
			for argIndex, arg := range e.Args {
				expected := jitRegionStdMemberCallArgType(e, argIndex, stdImportModules)
				if expected == "" {
					if !jitRegionExprSafe(arg, stdImportModules) {
						return "", false
					}
					continue
				}
				typ, ok := inferJitRegionExprType(arg, knownTypes, stdImportModules)
				if !ok || typ != expected {
					return "", false
				}
			}
			return retType, true
		}
		if !jitRegionSupportedMethodCall(e) {
			return "", false
		}

		objectType, objectOK := inferJitRegionExprType(e.Object, knownTypes, stdImportModules)
		if objectOK && objectType != "array" && !strings.HasPrefix(objectType, "array:") {
			return "", false
		}

		switch e.Method {
		case "length":
			return "number", true
		case "get":
			if len(e.Args) != 1 {
				return "", false
			}
			idxType, ok := inferJitRegionExprType(e.Args[0], knownTypes, stdImportModules)
			if !ok || idxType != "number" {
				return "", false
			}
			if elemType, ok := jitRegionArrayElementType(objectType); ok {
				return elemType, true
			}
			return "number", true
		case "push":
			if len(e.Args) != 1 || !jitRegionExprSafe(e.Args[0], stdImportModules) {
				return "", false
			}
			return "array", true
		default:
			return "", false
		}
	case PropertyExpr:
		return "number", true
	case IndexExpr:
		objectType, objectOK := inferJitRegionExprType(e.Object, knownTypes, stdImportModules)
		if !objectOK {
			return "", false
		}
		indexType, indexOK := inferJitRegionExprType(e.Index, knownTypes, stdImportModules)
		if !indexOK || indexType != "number" {
			return "", false
		}
		if elemType, ok := jitRegionArrayElementType(objectType); ok {
			return elemType, true
		}
		if objectType == "array" || objectType == "object" {
			return "number", true
		}
		return "", false
	}
	return "", false
}

func inferJitRegionLiteralType(expr Expr) (string, bool) {
	switch expr.(type) {
	case NumberExpr, FloatExpr:
		return "number", true
	case StringExpr:
		return "string", true
	case BoolExpr:
		return "bool", true
	default:
		return "", false
	}
}

func isJitRegionScalarType(name string) bool {
	return name == "number" || name == "bool"
}

func isJitRegionValueType(name string) bool {
	return isJitRegionScalarType(name) || name == "string" || name == "object" || name == "array" || strings.HasPrefix(name, "array:")
}

func jitRegionArrayTypeFromElement(elemType string) string {
	switch elemType {
	case "number", "string", "bool":
		return "array:" + elemType
	default:
		return "array"
	}
}

func jitRegionArrayElementType(arrayType string) (string, bool) {
	if !strings.HasPrefix(arrayType, "array:") {
		return "", false
	}
	elemType := strings.TrimPrefix(arrayType, "array:")
	if elemType == "" || !isJitRegionValueType(elemType) {
		return "", false
	}
	return elemType, true
}

func mergeJitRegionRequiredTypes(existing string, next string) (string, bool) {
	if next == "" {
		return existing, true
	}
	if existing == "" || existing == next {
		return next, true
	}
	if existing == "array" && strings.HasPrefix(next, "array:") {
		return next, true
	}
	if next == "array" && strings.HasPrefix(existing, "array:") {
		return existing, true
	}
	return existing, false
}

func jitRegionDeclaredScalarTypes(stmts []Stmt) map[string]string {
	return jitRegionDeclaredScalarTypesKnown(stmts, nil, nil)
}

func jitRegionDeclaredScalarTypesKnown(stmts []Stmt, knownTypes map[string]string, stdImportModules map[string]string) map[string]string {
	types := map[string]string{}
	regionTypes := map[string]string{}
	if knownTypes != nil {
		regionTypes = maps.Clone(knownTypes)
	}
	for _, stmt := range stmts {
		variable, ok := stmt.(VariableStmt)
		if !ok {
			continue
		}
		if typ, ok := inferJitRegionVariableTypeKnown(variable, regionTypes, stdImportModules); ok {
			types[variable.Name] = typ
			regionTypes[variable.Name] = typ
		}
	}
	return types
}

func jitRegionDeclaredNames(stmts []Stmt) map[string]bool {
	declared := map[string]bool{}
	for _, stmt := range stmts {
		collectJitRegionDeclaredNames(stmt, declared)
	}
	return declared
}

func collectJitRegionDeclaredNames(stmt Stmt, declared map[string]bool) {
	switch s := stmt.(type) {
	case VariableStmt:
		declared[s.Name] = true
	case ForStmt:
		if s.Init != nil {
			collectJitRegionDeclaredNames(s.Init, declared)
		}
		for _, nested := range s.Body {
			collectJitRegionDeclaredNames(nested, declared)
		}
	case WhileStmt:
		for _, nested := range s.Body {
			collectJitRegionDeclaredNames(nested, declared)
		}
	case IfStmt:
		for _, nested := range s.ThenBody {
			collectJitRegionDeclaredNames(nested, declared)
		}
		for _, nested := range s.ElseBody {
			collectJitRegionDeclaredNames(nested, declared)
		}
	}
}

func jitRegionStatementsSafe(stmts []Stmt, allowLoop bool, stdImportModules map[string]string) bool {
	for _, stmt := range stmts {
		if !jitRegionStatementSafe(stmt, allowLoop, stdImportModules) {
			return false
		}
	}
	return true
}

func jitRegionStatementSafe(stmt Stmt, allowLoop bool, stdImportModules map[string]string) bool {
	switch s := stmt.(type) {
	case VariableStmt:
		return !s.Constant && jitRegionExprSafe(s.Value, stdImportModules)
	case AssignStmt:
		return jitRegionExprSafe(s.Value, stdImportModules)
	case PropertyAssignStmt:
		return jitRegionExprSafe(s.Object, stdImportModules) && jitRegionExprSafe(s.Value, stdImportModules)
	case IndexAssignStmt:
		return jitRegionExprSafe(s.Object, stdImportModules) && jitRegionExprSafe(s.Index, stdImportModules) && jitRegionExprSafe(s.Value, stdImportModules)
	case IncrementStmt, DecrementStmt, BreakStmt, ContinueStmt:
		return true
	case ExprStmt:
		return jitRegionExprSafe(s.Value, stdImportModules)
	case IfStmt:
		return jitRegionExprSafe(s.Condition, stdImportModules) &&
			jitRegionStatementsSafe(s.ThenBody, false, stdImportModules) &&
			jitRegionStatementsSafe(s.ElseBody, false, stdImportModules)
	case WhileStmt:
		return allowLoop &&
			jitRegionExprSafe(s.Condition, stdImportModules) &&
			jitRegionStatementsSafe(s.Body, allowLoop, stdImportModules)
	case ForStmt:
		return allowLoop &&
			(s.Init == nil || jitRegionStatementSafe(s.Init, false, stdImportModules)) &&
			jitRegionExprSafe(s.Condition, stdImportModules) &&
			(s.Update == nil || jitRegionStatementSafe(s.Update, false, stdImportModules)) &&
			jitRegionStatementsSafe(s.Body, allowLoop, stdImportModules)
	default:
		return false
	}
}

func jitRegionExprSafe(expr Expr, stdImportModules map[string]string) bool {
	switch e := expr.(type) {
	case nil:
		return true
	case NumberExpr, FloatExpr, StringExpr, BoolExpr, NullExpr, IdentExpr:
		return true
	case UnaryExpr:
		return jitRegionExprSafe(e.Right, stdImportModules)
	case BinaryExpr:
		return jitRegionExprSafe(e.Left, stdImportModules) && jitRegionExprSafe(e.Right, stdImportModules)
	case TernaryExpr:
		return jitRegionExprSafe(e.Condition, stdImportModules) && jitRegionExprSafe(e.ThenExpr, stdImportModules) && jitRegionExprSafe(e.ElseExpr, stdImportModules)
	case NullishCoalescingExpr:
		return jitRegionExprSafe(e.Left, stdImportModules) && jitRegionExprSafe(e.Right, stdImportModules)
	case CallExpr:
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, stdImportModules) {
				return false
			}
		}
		return true
	case CallValueExpr:
		if _, ok := e.Callee.(IdentExpr); !ok {
			return false
		}
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, stdImportModules) {
				return false
			}
		}
		return true
	case MemberCallExpr:
		if _, ok := jitRegionStdMemberCallType(e, stdImportModules); ok {
			for _, arg := range e.Args {
				if !jitRegionExprSafe(arg, stdImportModules) {
					return false
				}
			}
			return true
		}
		if !jitRegionSupportedMethodCall(e) {
			return false
		}
		if !jitRegionExprSafe(e.Object, stdImportModules) {
			return false
		}
		for _, arg := range e.Args {
			if !jitRegionExprSafe(arg, stdImportModules) {
				return false
			}
		}
		return true
	case PropertyExpr:
		return jitRegionExprSafe(e.Object, stdImportModules)
	case IndexExpr:
		return jitRegionExprSafe(e.Object, stdImportModules) && jitRegionExprSafe(e.Index, stdImportModules)
	case ArrayExpr:
		for _, elem := range e.Elements {
			if !jitRegionExprSafe(elem, stdImportModules) {
				return false
			}
		}
		return true
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy || !jitRegionExprSafe(field.Value, stdImportModules) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func inferJitRegionLiveInType(name string, stmts []Stmt, knownTypes map[string]string, stdImportModules map[string]string) (string, bool) {
	required := ""
	seen := false

	merge := func(typ string) bool {
		if typ == "" {
			return true
		}
		if !isJitRegionValueType(typ) {
			return false
		}
		merged, ok := mergeJitRegionRequiredTypes(required, typ)
		if !ok {
			return false
		}
		seen = true
		required = merged
		return true
	}

	var exprContains func(Expr) bool
	exprContains = func(expr Expr) bool {
		switch e := expr.(type) {
		case nil:
			return false
		case IdentExpr:
			return e.Name == name
		case UnaryExpr:
			return exprContains(e.Right)
		case BinaryExpr:
			return exprContains(e.Left) || exprContains(e.Right)
		case TernaryExpr:
			return exprContains(e.Condition) || exprContains(e.ThenExpr) || exprContains(e.ElseExpr)
		case NullishCoalescingExpr:
			return exprContains(e.Left) || exprContains(e.Right)
		case PropertyExpr:
			return exprContains(e.Object)
		case IndexExpr:
			return exprContains(e.Object) || exprContains(e.Index)
		case ArrayExpr:
			for _, elem := range e.Elements {
				if exprContains(elem) {
					return true
				}
			}
			return false
		case ObjectExpr:
			for _, field := range e.Fields {
				if field.HasCopy {
					if exprContains(field.Copy) {
						return true
					}
					continue
				}
				if exprContains(field.Value) {
					return true
				}
			}
			return false
		case CallValueExpr:
			for _, arg := range e.Args {
				if exprContains(arg) {
					return true
				}
			}
			return false
		case MemberCallExpr:
			if _, ok := jitRegionStdMemberCallType(e, stdImportModules); !ok {
				return exprContains(e.Object)
			}
			for _, arg := range e.Args {
				if exprContains(arg) {
					return true
				}
			}
			return false
		default:
			return false
		}
	}

	var exprTypeHint func(Expr) string
	exprTypeHint = func(expr Expr) string {
		switch e := expr.(type) {
		case NumberExpr, FloatExpr:
			return "number"
		case StringExpr:
			return "string"
		case BoolExpr:
			return "bool"
		case IdentExpr:
			return knownTypes[e.Name]
		case CallValueExpr:
			return ""
		case MemberCallExpr:
			if typ, ok := jitRegionStdMemberCallType(e, stdImportModules); ok {
				return typ
			}
			if jitRegionSupportedMethodCall(e) {
				switch e.Method {
				case "length":
					return "number"
				case "get":
					if elemType, ok := jitRegionArrayElementType(exprTypeHint(e.Object)); ok {
						return elemType
					}
					return "number"
				case "push":
					return "array"
				}
			}
			return ""
		case PropertyExpr:
			return "number"
		case IndexExpr:
			if elemType, ok := jitRegionArrayElementType(exprTypeHint(e.Object)); ok {
				return elemType
			}
			return "number"
		default:
			return ""
		}
	}

	var walkExpr func(Expr, string) bool
	walkExpr = func(expr Expr, expected string) bool {
		switch e := expr.(type) {
		case nil:
			return true
		case IdentExpr:
			if e.Name == name {
				return merge(expected)
			}
			return true
		case NumberExpr, FloatExpr, StringExpr, BoolExpr, NullExpr:
			return true
		case UnaryExpr:
			switch e.Op {
			case TOKEN_MINUS:
				return walkExpr(e.Right, "number")
			case TOKEN_BANG:
				return walkExpr(e.Right, "bool")
			default:
				return walkExpr(e.Right, expected)
			}
		case BinaryExpr:
			switch e.Op {
			case TOKEN_PLUS:
				if expected == "string" {
					return walkExpr(e.Left, "") && walkExpr(e.Right, "")
				}
				return walkExpr(e.Left, "number") && walkExpr(e.Right, "number")
			case TOKEN_MINUS, TOKEN_STAR, TOKEN_SLASH, TOKEN_PERCENT:
				return walkExpr(e.Left, "number") && walkExpr(e.Right, "number")
			case TOKEN_LT, TOKEN_LTE, TOKEN_GT, TOKEN_GTE:
				return walkExpr(e.Left, "number") && walkExpr(e.Right, "number")
			case TOKEN_AND, TOKEN_OR:
				return walkExpr(e.Left, "bool") && walkExpr(e.Right, "bool")
			case TOKEN_EQ, TOKEN_NEQ:
				leftHint := exprTypeHint(e.Left)
				rightHint := exprTypeHint(e.Right)
				if exprContains(e.Left) && rightHint != "" {
					if !walkExpr(e.Left, rightHint) {
						return false
					}
				} else if !walkExpr(e.Left, expected) {
					return false
				}
				if exprContains(e.Right) && leftHint != "" {
					return walkExpr(e.Right, leftHint)
				}
				return walkExpr(e.Right, expected)
			default:
				return walkExpr(e.Left, expected) && walkExpr(e.Right, expected)
			}
		case TernaryExpr:
			return walkExpr(e.Condition, "bool") && walkExpr(e.ThenExpr, expected) && walkExpr(e.ElseExpr, expected)
		case NullishCoalescingExpr:
			return walkExpr(e.Left, expected) && walkExpr(e.Right, expected)
		case CallExpr:
			// A direct-call argument is not inherently numeric. The previous logic forced
			// every live-in used as a call argument to number, which produced helpers like
			// __jit_region_main_2(logs_data: number) for aggregate(logs_data). Let known
			// types drive the parameter instead; unknown call-argument live-ins should
			// reject outlining rather than generate a wrong signature.
			for _, arg := range e.Args {
				if !walkExpr(arg, "") {
					return false
				}
			}
			return true
		case CallValueExpr:
			if _, ok := e.Callee.(IdentExpr); !ok {
				return false
			}
			for _, arg := range e.Args {
				if !walkExpr(arg, "") {
					return false
				}
			}
			return true
		case MemberCallExpr:
			if _, ok := jitRegionStdMemberCallType(e, stdImportModules); ok {
				for argIndex, arg := range e.Args {
					if !walkExpr(arg, jitRegionStdMemberCallArgType(e, argIndex, stdImportModules)) {
						return false
					}
				}
				return true
			}
			if !jitRegionSupportedMethodCall(e) {
				return false
			}
			objectExpected := "array"
			switch e.Method {
			case "get":
				if expected != "" && expected != "array" {
					objectExpected = jitRegionArrayTypeFromElement(expected)
				}
			case "length", "push":
				objectExpected = "array"
			}
			if !walkExpr(e.Object, objectExpected) {
				return false
			}
			for argIndex, arg := range e.Args {
				argExpected := ""
				if e.Method == "get" && argIndex == 0 {
					argExpected = "number"
				}
				if !walkExpr(arg, argExpected) {
					return false
				}
			}
			return true
		case PropertyExpr:
			return walkExpr(e.Object, "object")
		case IndexExpr:
			objectExpected := "array"
			if expected != "" && expected != "array" {
				objectExpected = jitRegionArrayTypeFromElement(expected)
			}
			return walkExpr(e.Object, objectExpected) && walkExpr(e.Index, "number")
		case ArrayExpr:
			for _, elem := range e.Elements {
				if !walkExpr(elem, "") {
					return false
				}
			}
			return true
		case ObjectExpr:
			for _, field := range e.Fields {
				if field.HasCopy {
					if !walkExpr(field.Copy, "") {
						return false
					}
					continue
				}
				if !walkExpr(field.Value, "") {
					return false
				}
			}
			return true
		default:
			return false
		}
	}

	var walkStmt func(Stmt) bool
	walkStmt = func(stmt Stmt) bool {
		switch s := stmt.(type) {
		case VariableStmt:
			expected := ""
			if typ, ok := inferJitRegionVariableType(s); ok {
				expected = typ
			}
			return walkExpr(s.Value, expected)
		case AssignStmt:
			return walkExpr(s.Value, knownTypes[s.Name])
		case PropertyAssignStmt:
			return walkExpr(s.Object, "object") && walkExpr(s.Value, "")
		case IndexAssignStmt:
			return walkExpr(s.Object, "array") && walkExpr(s.Index, "number") && walkExpr(s.Value, "")
		case ExprStmt:
			return walkExpr(s.Value, "")
		case IfStmt:
			if !walkExpr(s.Condition, "bool") {
				return false
			}
			for _, nested := range s.ThenBody {
				if !walkStmt(nested) {
					return false
				}
			}
			for _, nested := range s.ElseBody {
				if !walkStmt(nested) {
					return false
				}
			}
			return true
		case WhileStmt:
			if !walkExpr(s.Condition, "bool") {
				return false
			}
			for _, nested := range s.Body {
				if !walkStmt(nested) {
					return false
				}
			}
			return true
		case ForStmt:
			if s.Init != nil && !walkStmt(s.Init) {
				return false
			}
			if !walkExpr(s.Condition, "bool") {
				return false
			}
			if s.Update != nil && !walkStmt(s.Update) {
				return false
			}
			for _, nested := range s.Body {
				if !walkStmt(nested) {
					return false
				}
			}
			return true
		case ReturnStmt:
			return walkExpr(s.Value, "")
		case IncrementStmt, DecrementStmt, BreakStmt, ContinueStmt:
			return true
		default:
			return true
		}
	}

	for _, stmt := range stmts {
		if !walkStmt(stmt) {
			return "", false
		}
	}

	if !seen || required == "" {
		return "", false
	}
	return required, true
}

func collectJitRegionAssignedNames(stmt Stmt, assigned map[string]bool) {
	switch s := stmt.(type) {
	case AssignStmt:
		assigned[s.Name] = true
	case IncrementStmt:
		assigned[s.Name] = true
	case DecrementStmt:
		assigned[s.Name] = true
	case PropertyAssignStmt:
		if ident, ok := s.Object.(IdentExpr); ok {
			assigned[ident.Name] = true
		}
	case IndexAssignStmt:
		if ident, ok := s.Object.(IdentExpr); ok {
			assigned[ident.Name] = true
		}
	case IfStmt:
		for _, nested := range s.ThenBody {
			collectJitRegionAssignedNames(nested, assigned)
		}
		for _, nested := range s.ElseBody {
			collectJitRegionAssignedNames(nested, assigned)
		}
	case WhileStmt:
		for _, nested := range s.Body {
			collectJitRegionAssignedNames(nested, assigned)
		}
	case ForStmt:
		if s.Update != nil {
			collectJitRegionAssignedNames(s.Update, assigned)
		}
		for _, nested := range s.Body {
			collectJitRegionAssignedNames(nested, assigned)
		}
	}
}

func collectJitRegionStmtUses(stmt Stmt, uses map[string]bool, stdImportModules map[string]string) {
	switch s := stmt.(type) {
	case VariableStmt:
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	case AssignStmt:
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	case ExprStmt:
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	case IfStmt:
		collectJitRegionExprUses(s.Condition, uses, stdImportModules)
		for _, nested := range s.ThenBody {
			collectJitRegionStmtUses(nested, uses, stdImportModules)
		}
		for _, nested := range s.ElseBody {
			collectJitRegionStmtUses(nested, uses, stdImportModules)
		}
	case WhileStmt:
		collectJitRegionExprUses(s.Condition, uses, stdImportModules)
		for _, nested := range s.Body {
			collectJitRegionStmtUses(nested, uses, stdImportModules)
		}
	case ForStmt:
		if s.Init != nil {
			collectJitRegionStmtUses(s.Init, uses, stdImportModules)
		}
		collectJitRegionExprUses(s.Condition, uses, stdImportModules)
		if s.Update != nil {
			collectJitRegionStmtUses(s.Update, uses, stdImportModules)
		}
		for _, nested := range s.Body {
			collectJitRegionStmtUses(nested, uses, stdImportModules)
		}
	case ReturnStmt:
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	case PropertyAssignStmt:
		collectJitRegionExprUses(s.Object, uses, stdImportModules)
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	case IndexAssignStmt:
		collectJitRegionExprUses(s.Object, uses, stdImportModules)
		collectJitRegionExprUses(s.Index, uses, stdImportModules)
		collectJitRegionExprUses(s.Value, uses, stdImportModules)
	}
}

func collectJitRegionExprUses(expr Expr, uses map[string]bool, stdImportModules map[string]string) {
	switch e := expr.(type) {
	case nil:
		return
	case IdentExpr:
		uses[e.Name] = true
	case UnaryExpr:
		collectJitRegionExprUses(e.Right, uses, stdImportModules)
	case BinaryExpr:
		collectJitRegionExprUses(e.Left, uses, stdImportModules)
		collectJitRegionExprUses(e.Right, uses, stdImportModules)
	case TernaryExpr:
		collectJitRegionExprUses(e.Condition, uses, stdImportModules)
		collectJitRegionExprUses(e.ThenExpr, uses, stdImportModules)
		collectJitRegionExprUses(e.ElseExpr, uses, stdImportModules)
	case NullishCoalescingExpr:
		collectJitRegionExprUses(e.Left, uses, stdImportModules)
		collectJitRegionExprUses(e.Right, uses, stdImportModules)
	case CallExpr:
		for _, arg := range e.Args {
			collectJitRegionExprUses(arg, uses, stdImportModules)
		}
	case CallValueExpr:
		if _, ok := e.Callee.(IdentExpr); !ok {
			collectJitRegionExprUses(e.Callee, uses, stdImportModules)
		}
		for _, arg := range e.Args {
			collectJitRegionExprUses(arg, uses, stdImportModules)
		}
	case MemberCallExpr:
		if _, ok := jitRegionStdMemberCallType(e, stdImportModules); !ok {
			collectJitRegionExprUses(e.Object, uses, stdImportModules)
		}
		for _, arg := range e.Args {
			collectJitRegionExprUses(arg, uses, stdImportModules)
		}
	case PropertyExpr:
		collectJitRegionExprUses(e.Object, uses, stdImportModules)
	case IndexExpr:
		collectJitRegionExprUses(e.Object, uses, stdImportModules)
		collectJitRegionExprUses(e.Index, uses, stdImportModules)
	case ArrayExpr:
		for _, elem := range e.Elements {
			collectJitRegionExprUses(elem, uses, stdImportModules)
		}
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy {
				uses[field.Copy.Name] = true
			}
			collectJitRegionExprUses(field.Value, uses, stdImportModules)
		}
	case InterpolatedStringExpr:
		for _, part := range e.Parts {
			if part.IsExpr {
				collectJitRegionExprUses(part.Expr, uses, stdImportModules)
			}
		}
	case TypeOfExpr:
		collectJitRegionExprUses(e.Value, uses, stdImportModules)
	case InstanceOfExpr:
		collectJitRegionExprUses(e.Object, uses, stdImportModules)
		collectJitRegionExprUses(e.Class, uses, stdImportModules)
	case ObjectInExpr:
		collectJitRegionExprUses(e.Key, uses, stdImportModules)
		collectJitRegionExprUses(e.Object, uses, stdImportModules)
	case InstantiatedExpr:
		collectJitRegionExprUses(e.Object, uses, stdImportModules)
	case SpawnExpr:
		collectJitRegionExprUses(e.Function, uses, stdImportModules)
		for _, arg := range e.Args {
			collectJitRegionExprUses(arg, uses, stdImportModules)
		}
	case AwaitExpr:
		collectJitRegionExprUses(e.Task, uses, stdImportModules)
	case DeferExpr:
		collectJitRegionExprUses(e.Function, uses, stdImportModules)
	case SpreadExpr:
		collectJitRegionExprUses(e.Value, uses, stdImportModules)
	}
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

const maxInlineDepth = 8
const maxInlineBodyStatements = 2

func (c *Compiler) functionLooksJitCandidate(stmt FunctionStmt) bool {
	if stmt.Async || len(stmt.TypeParameters) > 0 {
		return false
	}
	if len(stmt.Params) > 0 && stmt.Params[len(stmt.Params)-1].Variadic {
		return false
	}
	if c.stmtListHasLoopOrComplexControl(stmt.Body) || c.functionStmtCallsName(stmt, stmt.Name) {
		return true
	}
	return len(stmt.Body) >= 8
}

func (c *Compiler) stmtListHasLoopOrComplexControl(stmts []Stmt) bool {
	for _, stmt := range stmts {
		switch s := stmt.(type) {
		case WhileStmt, ForStmt, ForInStmt:
			return true
		case TryCatchStmt, LockStmt:
			return true
		case IfStmt:
			if c.stmtListHasLoopOrComplexControl(s.ThenBody) || c.stmtListHasLoopOrComplexControl(s.ElseBody) {
				return true
			}
		}
	}
	return false
}

func (c *Compiler) functionStmtCallsName(stmt FunctionStmt, name string) bool {
	for _, bodyStmt := range stmt.Body {
		if c.stmtCallsName(bodyStmt, name) {
			return true
		}
	}
	return false
}

func (c *Compiler) stmtCallsName(stmt Stmt, name string) bool {
	switch s := stmt.(type) {
	case ReturnStmt:
		return s.HasValue && c.exprCallsName(s.Value, name)
	case ExprStmt:
		return c.exprCallsName(s.Value, name)
	case VariableStmt:
		return c.exprCallsName(s.Value, name)
	case AssignStmt:
		return c.exprCallsName(s.Value, name)
	case PropertyAssignStmt:
		return c.exprCallsName(s.Object, name) || c.exprCallsName(s.Value, name)
	case IndexAssignStmt:
		return c.exprCallsName(s.Object, name) || c.exprCallsName(s.Index, name) || c.exprCallsName(s.Value, name)
	case ThrowStmt:
		return c.exprCallsName(s.Value, name)
	case IfStmt:
		if c.exprCallsName(s.Condition, name) {
			return true
		}
		for _, inner := range s.ThenBody {
			if c.stmtCallsName(inner, name) {
				return true
			}
		}
		for _, inner := range s.ElseBody {
			if c.stmtCallsName(inner, name) {
				return true
			}
		}
	}
	return false
}

func (c *Compiler) exprCallsName(expr Expr, name string) bool {
	switch e := expr.(type) {
	case CallExpr:
		if e.Name == name {
			return true
		}
		for _, arg := range e.Args {
			if c.exprCallsName(arg, name) {
				return true
			}
		}
	case CallValueExpr:
		if c.exprCallsName(e.Callee, name) {
			return true
		}
		for _, arg := range e.Args {
			if c.exprCallsName(arg, name) {
				return true
			}
		}
	case MemberCallExpr:
		if c.exprCallsName(e.Object, name) {
			return true
		}
		for _, arg := range e.Args {
			if c.exprCallsName(arg, name) {
				return true
			}
		}
	case BinaryExpr:
		return c.exprCallsName(e.Left, name) || c.exprCallsName(e.Right, name)
	case UnaryExpr:
		return c.exprCallsName(e.Right, name)
	case TernaryExpr:
		return c.exprCallsName(e.Condition, name) || c.exprCallsName(e.ThenExpr, name) || c.exprCallsName(e.ElseExpr, name)
	case NullishCoalescingExpr:
		return c.exprCallsName(e.Left, name) || c.exprCallsName(e.Right, name)
	case PropertyExpr:
		return c.exprCallsName(e.Object, name)
	case IndexExpr:
		return c.exprCallsName(e.Object, name) || c.exprCallsName(e.Index, name)
	case ArrayExpr:
		for _, element := range e.Elements {
			if c.exprCallsName(element, name) {
				return true
			}
		}
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy {
				if c.exprCallsName(field.Copy, name) {
					return true
				}
			} else if c.exprCallsName(field.Value, name) {
				return true
			}
		}
	case TypeOfExpr:
		return c.exprCallsName(e.Value, name)
	case InstanceOfExpr:
		return c.exprCallsName(e.Object, name) || c.exprCallsName(e.Class, name)
	case ObjectInExpr:
		return c.exprCallsName(e.Object, name) || c.exprCallsName(e.Key, name)
	case InterpolatedStringExpr:
		for _, part := range e.Parts {
			if part.IsExpr && c.exprCallsName(part.Expr, name) {
				return true
			}
		}
	}
	return false
}

func (c *Compiler) tryCompileInlineCall(name string, args []Expr, file string, line int, column int) bool {
	if c.inlineDepth >= maxInlineDepth {
		return false
	}
	stmt, ok := c.inlineCandidates[name]
	if !ok {
		return false
	}
	inlineExpr, ok := c.inlineReturnExpr(name, stmt, args)
	if !ok {
		return false
	}
	c.checkCompileTimeArguments(name, args, stmt.Params, line, column)

	c.inlineDepth++

	c.compileExpr(inlineExpr)
	c.inlineDepth--
	return true
}

func (c *Compiler) inlineReturnExpr(name string, stmt FunctionStmt, args []Expr) (Expr, bool) {
	if len(args) != len(stmt.Params) || len(args) == 0 && len(stmt.Params) != 0 {
		return nil, false
	}
	if stmt.Async || stmt.Private || len(stmt.TypeParameters) > 0 || c.functionLooksJitCandidate(stmt) {
		return nil, false
	}
	for _, param := range stmt.Params {
		if param.HasDefault || param.Variadic {
			return nil, false
		}
	}
	if len(stmt.Body) == 0 || len(stmt.Body) > maxInlineBodyStatements {
		return nil, false
	}
	ret, ok := stmt.Body[len(stmt.Body)-1].(ReturnStmt)
	if !ok || !ret.HasValue {
		return nil, false
	}
	if len(stmt.Body) != 1 {
		return nil, false
	}
	if !c.exprIsInlineSafe(ret.Value) {
		return nil, false
	}

	params := map[string]Expr{}
	usage := map[string]int{}
	for i, param := range stmt.Params {
		params[param.Name] = args[i]
		usage[param.Name] = 0
	}
	c.countInlineParamUses(ret.Value, usage)
	for name, count := range usage {
		if count > 1 && !c.exprSafeToDuplicate(params[name]) {
			return nil, false
		}
	}
	return c.substituteInlineParams(ret.Value, params), true
}

func (c *Compiler) exprIsInlineSafe(expr Expr) bool {
	switch e := expr.(type) {
	case NumberExpr, FloatExpr, StringExpr, BoolExpr, NullExpr, IdentExpr, ThisExpr:
		return true
	case UnaryExpr:
		return c.exprIsInlineSafe(e.Right)
	case BinaryExpr:
		return c.exprIsInlineSafe(e.Left) && c.exprIsInlineSafe(e.Right)
	case TernaryExpr:
		return c.exprIsInlineSafe(e.Condition) && c.exprIsInlineSafe(e.ThenExpr) && c.exprIsInlineSafe(e.ElseExpr)
	case NullishCoalescingExpr:
		return c.exprIsInlineSafe(e.Left) && c.exprIsInlineSafe(e.Right)
	case TypeOfExpr:
		return c.exprIsInlineSafe(e.Value)
	case PropertyExpr:
		return !e.Safe && c.exprIsInlineSafe(e.Object)
	case IndexExpr:
		return c.exprIsInlineSafe(e.Object) && c.exprIsInlineSafe(e.Index)
	case ArrayExpr:
		for _, element := range e.Elements {
			if !c.exprIsInlineSafe(element) {
				return false
			}
		}
		return true
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy || !c.exprIsInlineSafe(field.Value) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

func (c *Compiler) exprSafeToDuplicate(expr Expr) bool {
	switch e := expr.(type) {
	case NumberExpr, FloatExpr, StringExpr, BoolExpr, NullExpr, IdentExpr, ThisExpr:
		return true
	case UnaryExpr:
		return c.exprSafeToDuplicate(e.Right)
	case BinaryExpr:
		return c.exprSafeToDuplicate(e.Left) && c.exprSafeToDuplicate(e.Right)
	default:
		return false
	}
}

func (c *Compiler) countInlineParamUses(expr Expr, usage map[string]int) {
	switch e := expr.(type) {
	case IdentExpr:
		if _, ok := usage[e.Name]; ok {
			usage[e.Name]++
		}
	case UnaryExpr:
		c.countInlineParamUses(e.Right, usage)
	case BinaryExpr:
		c.countInlineParamUses(e.Left, usage)
		c.countInlineParamUses(e.Right, usage)
	case TernaryExpr:
		c.countInlineParamUses(e.Condition, usage)
		c.countInlineParamUses(e.ThenExpr, usage)
		c.countInlineParamUses(e.ElseExpr, usage)
	case NullishCoalescingExpr:
		c.countInlineParamUses(e.Left, usage)
		c.countInlineParamUses(e.Right, usage)
	case TypeOfExpr:
		c.countInlineParamUses(e.Value, usage)
	case PropertyExpr:
		c.countInlineParamUses(e.Object, usage)
	case IndexExpr:
		c.countInlineParamUses(e.Object, usage)
		c.countInlineParamUses(e.Index, usage)
	case ArrayExpr:
		for _, element := range e.Elements {
			c.countInlineParamUses(element, usage)
		}
	case ObjectExpr:
		for _, field := range e.Fields {
			if field.HasCopy {
				c.countInlineParamUses(field.Copy, usage)
			} else {
				c.countInlineParamUses(field.Value, usage)
			}
		}
	case InterpolatedStringExpr:
		for _, part := range e.Parts {
			if part.IsExpr {
				c.countInlineParamUses(part.Expr, usage)
			}
		}
	}
}

func (c *Compiler) substituteInlineParams(expr Expr, params map[string]Expr) Expr {
	switch e := expr.(type) {
	case IdentExpr:
		if repl, ok := params[e.Name]; ok {
			return repl
		}
		return e
	case UnaryExpr:
		e.Right = c.substituteInlineParams(e.Right, params)
		return e
	case BinaryExpr:
		e.Left = c.substituteInlineParams(e.Left, params)
		e.Right = c.substituteInlineParams(e.Right, params)
		return e
	case TernaryExpr:
		e.Condition = c.substituteInlineParams(e.Condition, params)
		e.ThenExpr = c.substituteInlineParams(e.ThenExpr, params)
		e.ElseExpr = c.substituteInlineParams(e.ElseExpr, params)
		return e
	case NullishCoalescingExpr:
		e.Left = c.substituteInlineParams(e.Left, params)
		e.Right = c.substituteInlineParams(e.Right, params)
		return e
	case TypeOfExpr:
		e.Value = c.substituteInlineParams(e.Value, params)
		return e
	case PropertyExpr:
		e.Object = c.substituteInlineParams(e.Object, params)
		return e
	case IndexExpr:
		e.Object = c.substituteInlineParams(e.Object, params)
		e.Index = c.substituteInlineParams(e.Index, params)
		return e
	case ArrayExpr:
		for i := range e.Elements {
			e.Elements[i] = c.substituteInlineParams(e.Elements[i], params)
		}
		return e
	case ObjectExpr:
		for i := range e.Fields {
			if e.Fields[i].HasCopy {
				e.Fields[i].Copy = c.substituteInlineParams(e.Fields[i].Copy, params).(IdentExpr)
			} else {
				e.Fields[i].Value = c.substituteInlineParams(e.Fields[i].Value, params)
			}
		}
		return e
	case InterpolatedStringExpr:
		for i := range e.Parts {
			if e.Parts[i].IsExpr {
				e.Parts[i].Expr = c.substituteInlineParams(e.Parts[i].Expr, params)
			}
		}
		return e
	default:
		return e
	}
}

func (c *Compiler) compileExpr(expr Expr) {
	expr = c.optimizeExpr(expr)

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

	case InstantiatedExpr:
		c.compileExpr(e.Object)

	case StringExpr:
		c.emit(OP_CONST, e.Value)

	case UnaryExpr:
		c.compileExpr(e.Right)

		switch e.Op {
		case TOKEN_BANG:
			c.emit(OP_NOT, nil)

		case TOKEN_MINUS:
			c.emit(OP_NEGATE, nil)

		case TOKEN_TILDE:
			c.emit(OP_NOT_BIT, nil)

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
		oldDebugInfo := c.currentDebugInfo
		oldScopes := c.scopes
		oldLocalCount := c.localCount
		oldInMethod := c.inMethod
		oldOuterBindings := c.outerBindings
		oldCurrentCaptures := c.currentCaptures
		oldReturnType := c.currentReturnType
		oldFunctionName := c.currentFunctionName
		oldFile := c.currentFile
		oldLine := c.currentLine
		oldColumn := c.currentColumn

		functionInstructions := []Instruction{}
		functionDebugInfo := []DebugInfo{}

		c.currentInstructions = &functionInstructions
		c.currentDebugInfo = &functionDebugInfo
		c.scopes = []map[string]Binding{}
		c.localCount = 0
		c.inMethod = false
		c.outerBindings = outerBindings
		c.currentCaptures = map[string]CapturedVar{}
		c.currentReturnType = e.ReturnType
		c.currentFunctionName = name
		c.setLocation(e.File, e.Line, e.Column)

		c.beginScope()

		for _, param := range e.Params {
			c.declareVariable(param.Name, false)
		}

		for _, bodyStmt := range e.Body {
			c.compileStatement(bodyStmt)
		}

		c.emit(OP_CONST, NewNull())
		c.emit(OP_RETURN, nil)

		captures := []CapturedVar{}
		for _, capture := range c.currentCaptures {
			captures = append(captures, capture)
		}

		localCount := c.localCount

		hasDefaults, hasTypeHints := getParamFlags(e.Params)

		c.functions[name] = Function{
			ID:             c.getFunctionID(name),
			Name:           name,
			Params:         e.Params,
			ReturnType:     e.ReturnType,
			Instructions:   functionInstructions,
			DebugInfo:      functionDebugInfo,
			StatementCount: len(e.Body),
			LocalCount:     localCount,
			Captures:       captures,
			HasDefaults:    hasDefaults,
			HasTypeHints:   hasTypeHints,
			Async:          false,
		}

		c.currentInstructions = oldInstructions
		c.currentDebugInfo = oldDebugInfo
		c.scopes = oldScopes
		c.localCount = oldLocalCount
		c.inMethod = oldInMethod
		c.outerBindings = oldOuterBindings
		c.currentCaptures = oldCurrentCaptures
		c.currentReturnType = oldReturnType
		c.currentFunctionName = oldFunctionName
		c.setLocation(oldFile, oldLine, oldColumn)

		c.usedFunctions[name] = true

		c.emit(OP_CLOSURE, ClosureInfo{
			Name:     name,
			Captures: captures,
		})

	case BoolExpr:
		c.emit(OP_CONST, e.Value)

	case NullExpr:
		c.emit(OP_CONST, NullValue{})

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
		if fullName, ok := c.resolveFullyQualifiedName(e); ok {
			if val, exists := c.enumConstants[fullName]; exists {
				c.emit(OP_CONST, val)
				return
			}
		}

		if ident, ok := e.Object.(IdentExpr); ok {
			if binding, exists := c.resolveVariable(ident.Name); exists && binding.VirtualFields != nil {
				if slot, exists := binding.VirtualFields[e.Name]; exists {
					c.emit(OP_LOAD_LOCAL, slot)
					return
				}
			}
		}

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
		for _, arg := range e.Args {
			c.compileExpr(arg)
		}
		c.compileExpr(e.Function)
		c.emit(OP_SPAWN, len(e.Args))

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
		if strLit, ok := e.Index.(StringExpr); ok {
			c.compileExpr(e.Object)
			c.emit(OP_GET_PROPERTY_SAFE, strLit.Value)
		} else {
			c.compileExpr(e.Object)
			c.compileExpr(e.Index)
			c.emit(OP_INDEX, nil)
		}

	case NumberExpr:
		c.emit(OP_CONST, e.Value)

	case FloatExpr:
		c.emit(OP_CONST, e.Value)

	case IdentExpr:
		c.setLocation(e.File, e.Line, e.Column)
		// 1. Normal local/global variable resolution first.
		if binding, exists := c.resolveVariable(e.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, binding.Slot)
			} else {
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: binding.Name,
					Slot: binding.Slot,
				})
			}
			return
		}

		// 2. Namespace symbols.
		if c.currentNamespaceEnums != nil {
			if fullName, exists := c.currentNamespaceEnums[e.Name]; exists {
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: fullName,
					Slot: c.globalIndexes[fullName],
				})
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
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: fullName,
					Slot: c.globalIndexes[fullName],
				})
				return
			}
		}

		// 3. Only capture REAL outer locals.
		if binding, exists := c.ensureCaptured(e.Name); exists {
			if binding.Kind == BindingLocal {
				c.emit(OP_LOAD_LOCAL, binding.Slot)
				return
			} else {
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: binding.Name,
					Slot: binding.Slot,
				})
				return
			}
		}
		// 4. Known global function.
		if _, exists := c.functions[e.Name]; exists {
			c.usedFunctions[e.Name] = true
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
			LangErrorAt(
				ErrorName,
				e.File,
				e.Line,
				e.Column,
				"undefined variable in namespace: %s",
				e.Name,
			)
		}

		if c.declaredFunctions[e.Name] {
			if _, ok := c.externalFunctions[e.Name]; ok {
				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: e.Name,
					Slot: c.globalIndexes[e.Name],
				})
				return
			}
			c.usedFunctions[e.Name] = true
			c.emit(OP_CONST, FunctionValue{Name: e.Name})
			return
		}

		// 4. classes
		if _, ok := c.classes[e.Name]; ok {
			c.emit(OP_LOAD_GLOBAL, VariableInfo{
				Name: e.Name,
				Slot: c.globalIndexes[e.Name],
			})
			return
		}

		// 5. known global variables/imports
		if _, ok := c.globalConstants[e.Name]; ok {
			c.emit(OP_LOAD_GLOBAL, VariableInfo{
				Name: e.Name,
				Slot: c.globalIndexes[e.Name],
			})
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
		c.emit(OP_LOAD_GLOBAL, VariableInfo{
			Name: e.Name,
			Slot: c.globalIndexes[e.Name],
		})
		return

	case BinaryExpr:
		if e.Op == TOKEN_AND {
			c.compileLogicalAnd(e.Left, e.Right)
			return
		}
		if e.Op == TOKEN_OR {
			c.compileLogicalOr(e.Left, e.Right)
			return
		}

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

		case TOKEN_AMP:
			c.emit(OP_AND_BIT, nil)
		case TOKEN_PIPE:
			c.emit(OP_OR_BIT, nil)
		case TOKEN_CARET:
			c.emit(OP_XOR, nil)
		case TOKEN_LSHIFT:
			c.emit(OP_LSHIFT, nil)
		case TOKEN_RSHIFT:
			c.emit(OP_RSHIFT, nil)

		default:
			c.fatalError(ErrorInternal, "unknown binary operator")
		}

	case CallExpr:
		if hasSpreadArg(e.Args) {
			c.compileExpr(IdentExpr{
				Name:   e.Name,
				File:   e.File,
				Line:   e.Line,
				Column: e.Column,
			})

			spreadArgs := c.compileCallArgs(e.Args)

			c.setLocation(e.File, e.Line, e.Column)
			c.emit(OP_CALL_VALUE_SPREAD, SpreadCallInfo{SpreadArgs: spreadArgs})
			return
		}

		if cls, exists := c.classes[e.Name]; exists {
			inferredArgs := []TypeHint{}
			if len(cls.TypeParameters) > 0 {
				initMethodName := e.Name + ".init"
				if fn, exists := c.functions[initMethodName]; exists {
					subst := map[string]string{}
					for _, tp := range cls.TypeParameters {
						subst[tp] = "any"
					}

					params := fn.Params
					if len(params) > 0 && params[0].Name == "this" {
						params = params[1:]
					}

					for i, arg := range e.Args {
						if i >= len(params) {
							break
						}
						param := params[i]
						argType := c.inferCompileTimeType(arg)
						for _, tp := range cls.TypeParameters {
							if res, ok := c.inferTypeParamInCompiler(param.TypeHint.Name, argType, tp); ok {
								subst[tp] = res
							}
						}
					}
					for _, tp := range cls.TypeParameters {
						inferredArgs = append(inferredArgs, TypeHint{Name: subst[tp]})
					}
				}
			}
			c.checkCompileTimeClassArguments(e.Name, e.Args, inferredArgs, e.Line, e.Column)

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
			if fn, ok := c.externalFunctions[e.Name]; ok {
				c.checkCompileTimeArguments(e.Name, e.Args, fn.Params, e.Line, e.Column)

				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: e.Name,
					Slot: c.globalIndexes[e.Name],
				})

				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(e.File, e.Line, e.Column)
				c.emit(OP_CALL_VALUE, CallInfo{
					ArgCount: len(e.Args),
				})
				return
			}

			if c.tryCompileInlineCall(e.Name, e.Args, e.File, e.Line, e.Column) {
				return
			}

			fn := c.functions[e.Name]

			c.checkCompileTimeArguments(e.Name, e.Args, fn.Params, e.Line, e.Column)

			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.usedFunctions[e.Name] = true

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_CALL_DIRECT, DirectCallInfo{
				ID:       c.getFunctionID(e.Name),
				Name:     e.Name,
				ArgCount: len(e.Args),
			})

			return
		}

		if retType, isNative := c.nativeFunctions[e.Name]; isNative {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.setLocation(e.File, e.Line, e.Column)

			sanitizedName := strings.ReplaceAll(e.Name, ".", "_")
			c.emit(OP_NATIVE_CALL, NativeCallInfo{
				Name:       sanitizedName,
				ArgCount:   len(e.Args),
				ReturnType: retType,
			})

			return
		}

		if c.currentNamespaceFunctions != nil {
			if fullName, exists := c.currentNamespaceFunctions[e.Name]; exists {
				if c.tryCompileInlineCall(fullName, e.Args, e.File, e.Line, e.Column) {
					return
				}

				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				if retType, isNative := c.nativeFunctions[fullName]; isNative {
					c.setLocation(e.File, e.Line, e.Column)

					sanitizedName := strings.ReplaceAll(fullName, ".", "_")
					c.emit(OP_NATIVE_CALL, NativeCallInfo{
						Name:       sanitizedName,
						ArgCount:   len(e.Args),
						ReturnType: retType,
					})

					return
				}

				c.usedFunctions[fullName] = true

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
		if hasSpreadArg(e.Args) {
			c.compileExpr(e.Callee)

			spreadArgs := c.compileCallArgs(e.Args)

			c.setLocation(e.File, e.Line, e.Column)

			c.emit(OP_CALL_VALUE_SPREAD, SpreadCallInfo{SpreadArgs: spreadArgs})
			return
		}

		if instantiated, ok := e.Callee.(InstantiatedExpr); ok {
			if fullName, ok := c.resolveFullyQualifiedName(instantiated.Object); ok {
				if _, exists := c.classes[fullName]; exists {
					c.checkCompileTimeClassArguments(fullName, e.Args, instantiated.TypeArgs, e.Line, e.Column)
					for _, arg := range e.Args {
						c.compileExpr(arg)
					}
					c.setLocation(e.File, e.Line, e.Column)
					c.emit(OP_CALL, CallInfo{
						Name:     fullName,
						ArgCount: len(e.Args),
					})
					return
				}

				if _, exists := c.functions[fullName]; exists {
					c.checkCompileTimeFunctionArguments(fullName, e.Args, instantiated.TypeArgs, e.Line, e.Column)
					c.usedFunctions[fullName] = true
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
		}

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

			if retType, isNative := c.nativeFunctions[ident.Name]; isNative {
				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(ident.File, ident.Line, ident.Column)

				sanitizedName := strings.ReplaceAll(ident.Name, ".", "_")
				c.emit(OP_NATIVE_CALL, NativeCallInfo{
					Name:       sanitizedName,
					ArgCount:   len(e.Args),
					ReturnType: retType,
				})

				return
			}

			if c.currentNamespaceFunctions != nil {
				if fullName, exists := c.currentNamespaceFunctions[ident.Name]; exists {
					if c.tryCompileInlineCall(fullName, e.Args, ident.File, ident.Line, ident.Column) {
						return
					}

					for _, arg := range e.Args {
						c.compileExpr(arg)
					}

					if retType, isNative := c.nativeFunctions[fullName]; isNative {
						c.setLocation(ident.File, ident.Line, ident.Column)

						sanitizedName := strings.ReplaceAll(fullName, ".", "_")
						c.emit(OP_NATIVE_CALL, NativeCallInfo{
							Name:       sanitizedName,
							ArgCount:   len(e.Args),
							ReturnType: retType,
						})

						return
					}

					c.setLocation(ident.File, ident.Line, ident.Column)

					c.usedFunctions[fullName] = true

					c.emit(OP_CALL_DIRECT, DirectCallInfo{
						ID:       c.getFunctionID(fullName),
						Name:     fullName,
						ArgCount: len(e.Args),
					})

					return
				}
			}

			if fn, exists := c.functions[ident.Name]; exists {
				if c.tryCompileInlineCall(ident.Name, e.Args, ident.File, ident.Line, ident.Column) {
					return
				}

				c.checkCompileTimeArguments(ident.Name, e.Args, fn.Params, e.Line, e.Column)

				c.usedFunctions[ident.Name] = true

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

			if fn, exists := c.externalFunctions[ident.Name]; exists {
				c.checkCompileTimeArguments(ident.Name, e.Args, fn.Params, e.Line, e.Column)

				c.emit(OP_LOAD_GLOBAL, VariableInfo{
					Name: ident.Name,
					Slot: c.globalIndexes[ident.Name],
				})

				for _, arg := range e.Args {
					c.compileExpr(arg)
				}

				c.setLocation(ident.File, ident.Line, ident.Column)
				c.emit(OP_CALL_VALUE, CallInfo{
					ArgCount: len(e.Args),
				})
				return
			}

			if cls, exists := c.classes[ident.Name]; exists {
				inferredArgs := []TypeHint{}
				if len(cls.TypeParameters) > 0 {
					initMethodName := ident.Name + ".init"
					if fn, exists := c.functions[initMethodName]; exists {
						subst := map[string]string{}
						for _, tp := range cls.TypeParameters {
							subst[tp] = "any"
						}

						params := fn.Params
						if len(params) > 0 && params[0].Name == "this" {
							params = params[1:]
						}

						for i, arg := range e.Args {
							if i >= len(params) {
								break
							}
							param := params[i]
							argType := c.inferCompileTimeType(arg)
							for _, tp := range cls.TypeParameters {
								if res, ok := c.inferTypeParamInCompiler(param.TypeHint.Name, argType, tp); ok {
									subst[tp] = res
								}
							}
						}
						for _, tp := range cls.TypeParameters {
							inferredArgs = append(inferredArgs, TypeHint{Name: subst[tp]})
						}
					}
				}
				c.checkCompileTimeClassArguments(ident.Name, e.Args, inferredArgs, e.Line, e.Column)

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
		if c.tryCompileStdIntrinsic(e) {
			return
		}

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

		if !e.Safe && e.Method == "length" && len(e.Args) == 0 {
			c.compileExpr(e.Object)
			c.setLocation(e.File, e.Line, e.Column)
			c.emit(OP_LEN, nil)
			return
		}

		// Check for enum variant construction: Enum.Variant(args)
		if ident, ok := e.Object.(IdentExpr); ok {
			if enumName, ok := c.resolveFullyQualifiedName(ident); ok && c.isEnumVariant(enumName, e.Method) {
				c.compileEnumVariantConstruction(enumName, e.Method, e.Args, e.File, e.Line, e.Column)
				return
			}
		}

		c.compileExpr(e.Object)

		spreadArgs := c.compileCallArgs(e.Args)

		if objName, ok := c.resolveFullyQualifiedName(e.Object); ok {
			funcName := objName + "." + e.Method
			if c.namespacePrivateMembers[funcName] && c.currentNamespaceName != objName {
				LangErrorAt(ErrorRuntime, e.File, e.Line, e.Column, "cannot access private namespace member: %s", funcName)
			}
			if _, exists := c.functions[funcName]; exists {
				c.usedFunctions[funcName] = true
			}
		}

		c.setLocation(e.File, e.Line, e.Column)

		if e.Safe {
			c.emit(OP_METHOD_CALL_SAFE, MethodCallInfo{
				Method:     e.Method,
				ArgCount:   len(e.Args),
				SpreadArgs: spreadArgs,
			})
		} else {
			c.emit(OP_METHOD_CALL, MethodCallInfo{
				Method:     e.Method,
				ArgCount:   len(e.Args),
				SpreadArgs: spreadArgs,
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

func hasSpreadArg(args []Expr) bool {
	for _, arg := range args {
		if _, ok := arg.(SpreadExpr); ok {
			return true
		}
	}

	return false
}

func (c *Compiler) compileCallArgs(args []Expr) []bool {
	hasSpread := hasSpreadArg(args)
	var spreadArgs []bool
	if hasSpread {
		spreadArgs = make([]bool, len(args))
	}

	for i, arg := range args {
		if spread, ok := arg.(SpreadExpr); ok {
			c.compileExpr(spread.Value)
			spreadArgs[i] = true
		} else {
			c.compileExpr(arg)
		}
	}

	return spreadArgs
}

func (c *Compiler) isCompilingMain() bool {
	return c.currentInstructions == &c.mainInstructions
}

func (c *Compiler) compileMethod(className string, stmt FunctionStmt) {
	name := className + "." + stmt.Name

	oldActiveTypeParams := c.activeTypeParams
	c.activeTypeParams = append(append([]string{}, oldActiveTypeParams...), stmt.TypeParameters...)
	defer func() {
		c.activeTypeParams = oldActiveTypeParams
	}()

	oldInstructions := c.currentInstructions
	oldDebugInfo := c.currentDebugInfo
	oldScopes := c.scopes
	oldLocalCount := c.localCount
	oldInMethod := c.inMethod
	oldOuterBindings := c.outerBindings
	oldCurrentCaptures := c.currentCaptures

	functionInstructions := []Instruction{}
	functionDebugInfo := []DebugInfo{}

	c.currentInstructions = &functionInstructions
	c.currentDebugInfo = &functionDebugInfo

	globalScope := map[string]Binding{}
	if len(oldScopes) > 0 {
		maps.Copy(globalScope, oldScopes[0])
	}

	c.scopes = []map[string]Binding{globalScope}
	c.localCount = 0
	c.inMethod = true
	c.outerBindings = nil
	c.currentCaptures = nil

	oldReturnType := c.currentReturnType
	oldFunctionName := c.currentFunctionName

	c.currentReturnType = stmt.ReturnType
	c.currentFunctionName = stmt.Name

	defer func() {
		c.currentReturnType = oldReturnType
		c.currentFunctionName = oldFunctionName
	}()

	c.beginScope()

	// slot 0 = this
	thisBinding := c.declareVariable("this", false)
	thisBinding.TypeHint = className
	c.currentScope()["this"] = thisBinding

	// slot 1+ = real user parameters
	for _, param := range stmt.Params {
		if param.Name == "this" {
			continue
		}

		binding := c.declareVariable(param.Name, false)
		binding.TypeHint = param.TypeHint.Name
		c.currentScope()[param.Name] = binding
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, NewNull())
	c.emit(OP_RETURN, nil)

	params := make([]Param, 0, len(stmt.Params)+1)

	params = append(params, Param{
		Name: "this",
	})

	params = append(params, stmt.Params...)

	hasDefaults, hasTypeHints := getParamFlags(stmt.Params)

	c.functions[name] = Function{
		ID:             c.getFunctionID(name),
		Name:           name,
		TypeParameters: stmt.TypeParameters,
		Params:         params,
		ReturnType:     stmt.ReturnType,
		Instructions:   functionInstructions,
		DebugInfo:      functionDebugInfo,
		StatementCount: len(stmt.Body),
		LocalCount:     c.localCount,
		HasDefaults:    hasDefaults,
		HasTypeHints:   hasTypeHints,
		Async:          stmt.Async,
	}

	c.currentInstructions = oldInstructions
	c.currentDebugInfo = oldDebugInfo
	c.scopes = oldScopes
	c.localCount = oldLocalCount
	c.inMethod = oldInMethod
	c.outerBindings = oldOuterBindings
	c.currentCaptures = oldCurrentCaptures
}

func (c *Compiler) emit(op OpCode, value any) {
	if c.diagnosticMode {
		return
	}
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
	})
	*c.currentDebugInfo = append(*c.currentDebugInfo, DebugInfo{
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
	if c.diagnosticMode {
		return 0
	}
	return len(*c.currentInstructions) - 1
}

func (c *Compiler) patchJump(index int) {
	if c.diagnosticMode {
		return
	}
	target := len(*c.currentInstructions)
	(*c.currentInstructions)[index].Value = target
	(*c.currentInstructions)[index].IntArg = target
}

func (c *Compiler) inferCompileTimeType(expr Expr) string {
	switch e := expr.(type) {
	case StringExpr, InterpolatedStringExpr:
		return "string"
	case NumberExpr, FloatExpr:
		return "number"
	case BoolExpr:
		return "bool"
	case NullExpr:
		return "null"
	case ArrayExpr:
		if len(e.Elements) == 0 {
			return "array:any"
		}
		firstType := c.inferCompileTimeType(e.Elements[0])
		allSame := true
		for _, elem := range e.Elements {
			if c.inferCompileTimeType(elem) != firstType {
				allSame = false
				break
			}
		}
		if allSame && firstType != "any" {
			return "array:" + firstType
		}
		return "array"
	case ObjectExpr:
		if len(e.Fields) == 0 {
			return "object"
		}
		parts := []string{}
		for _, f := range e.Fields {
			if f.HasCopy {
				continue
			}
			fieldType := c.inferCompileTimeType(f.Value)
			if fieldType == "any" {
				return "object"
			}
			parts = append(parts, f.Name+": "+fieldType)
		}
		if len(parts) == 0 {
			return "object"
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case IdentExpr:
		if binding, exists := c.resolveVariable(e.Name); exists {
			if binding.TypeHint != "" {
				return binding.TypeHint
			}
		}
		return "any"
	case InstantiatedExpr:
		if name, ok := c.resolveFullyQualifiedName(e.Object); ok {
			formattedArgs := []string{}
			for _, arg := range e.TypeArgs {
				formattedArgs = append(formattedArgs, arg.Name)
			}
			return name + ":" + strings.Join(formattedArgs, ":")
		}
		return "any"
	case CallValueExpr:
		if instantiated, ok := e.Callee.(InstantiatedExpr); ok {
			if name, ok := c.resolveFullyQualifiedName(instantiated.Object); ok {
				if _, isClass := c.classes[name]; isClass {
					formattedArgs := []string{}
					for _, arg := range instantiated.TypeArgs {
						formattedArgs = append(formattedArgs, arg.Name)
					}
					return name + ":" + strings.Join(formattedArgs, ":")
				}
				if fn, exists := c.functions[name]; exists {
					subst := map[string]string{}
					for i, pName := range fn.TypeParameters {
						if i < len(instantiated.TypeArgs) {
							subst[pName] = instantiated.TypeArgs[i].Name
						}
					}
					return c.substituteTypeHintName(fn.ReturnType.Name, subst)
				}
			}
		}
		if ident, ok := e.Callee.(IdentExpr); ok {
			if name, ok := c.resolveFullyQualifiedName(ident); ok {
				if cls, isClass := c.classes[name]; isClass {
					if len(cls.TypeParameters) > 0 {
						initMethodName := name + ".init"
						if fn, exists := c.functions[initMethodName]; exists {
							subst := map[string]string{}
							for _, tp := range cls.TypeParameters {
								subst[tp] = "any"
							}

							params := fn.Params
							if len(params) > 0 && params[0].Name == "this" {
								params = params[1:]
							}

							for i, arg := range e.Args {
								if i >= len(params) {
									break
								}
								param := params[i]
								argType := c.inferCompileTimeType(arg)
								for _, tp := range cls.TypeParameters {
									if res, ok := c.inferTypeParamInCompiler(param.TypeHint.Name, argType, tp); ok {
										subst[tp] = res
									}
								}
							}

							formattedArgs := []string{}
							for _, tp := range cls.TypeParameters {
								formattedArgs = append(formattedArgs, subst[tp])
							}
							return name + ":" + strings.Join(formattedArgs, ":")
						}
					}
					return name
				}
				if fn, exists := c.functions[name]; exists {
					return fn.ReturnType.Name
				}
			}
		}
		return "any"
	case CallExpr:
		if cls, exists := c.classes[e.Name]; exists {
			if len(cls.TypeParameters) > 0 {
				initMethodName := e.Name + ".init"
				if fn, exists := c.functions[initMethodName]; exists {
					subst := map[string]string{}
					for _, tp := range cls.TypeParameters {
						subst[tp] = "any"
					}

					params := fn.Params
					if len(params) > 0 && params[0].Name == "this" {
						params = params[1:]
					}

					for i, arg := range e.Args {
						if i >= len(params) {
							break
						}
						param := params[i]
						argType := c.inferCompileTimeType(arg)
						for _, tp := range cls.TypeParameters {
							if res, ok := c.inferTypeParamInCompiler(param.TypeHint.Name, argType, tp); ok {
								subst[tp] = res
							}
						}
					}

					formattedArgs := []string{}
					for _, tp := range cls.TypeParameters {
						formattedArgs = append(formattedArgs, subst[tp])
					}
					return e.Name + ":" + strings.Join(formattedArgs, ":")
				}
			}
			return e.Name
		}
		if fn, exists := c.functions[e.Name]; exists {
			return fn.ReturnType.Name
		}
		return "any"
	default:
		return "any"
	}
}

func (c *Compiler) isEnumType(name string) bool {
	switch name {
	case "string", "number", "bool", "any", "null", "function", "error", "buffer", "array", "object":
		return false
	}
	if strings.HasPrefix(name, "array:") {
		return false
	}
	if _, exists := c.classes[name]; exists {
		return false
	}
	for key := range c.classes {
		if strings.HasSuffix(key, "."+name) {
			return false
		}
	}
	if _, exists := c.interfaces[name]; exists {
		return false
	}
	for key := range c.interfaces {
		if strings.HasSuffix(key, "."+name) {
			return false
		}
	}
	return true
}

func (c *Compiler) resolveTypeStringNamespaces(t string) string {
	var sb strings.Builder
	runes := []rune(t)
	n := len(runes)
	i := 0
	for i < n {
		r := runes[i]
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			start := i
			for i < n {
				ch := runes[i]
				if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_' {
					i++
				} else {
					break
				}
			}
			id := string(runes[start:i])
			if i < n && runes[i] == '.' {
				i++ // skip '.'
				if i < n {
					r2 := runes[i]
					if (r2 >= 'a' && r2 <= 'z') || (r2 >= 'A' && r2 <= 'Z') || (r2 >= '0' && r2 <= '9') || r2 == '_' {
						startSuffix := i
						for i < n {
							ch2 := runes[i]
							if (ch2 >= 'a' && ch2 <= 'z') || (ch2 >= 'A' && ch2 <= 'Z') || (ch2 >= '0' && ch2 <= '9') || ch2 == '_' {
								i++
							} else {
								break
							}
						}
						suffix := string(runes[startSuffix:i])
						prefix := id
						if c.currentNamespaceVariables != nil {
							if resolvedPrefix, exists := c.currentNamespaceVariables[prefix]; exists {
								prefix = resolvedPrefix
							}
						}
						if module, ok := c.stdImportModules[prefix]; ok {
							prefix = module
						}
						sb.WriteString(prefix + "." + suffix)
						continue
					}
				}
				sb.WriteString(id + ".")
				continue
			} else {
				sb.WriteString(id)
				continue
			}
		} else {
			sb.WriteRune(r)
			i++
		}
	}
	return sb.String()
}

func (c *Compiler) compareCompileTimeTypes(got string, expected string) bool {
	expected = c.resolveTypeStringNamespaces(expected)
	got = c.resolveTypeStringNamespaces(got)

	if got == "array" {
		got = "array:any"
	}
	if expected == "array" {
		expected = "array:any"
	}

	if expected == "any" || got == "any" {
		return true
	}
	if expected == "object" && strings.HasPrefix(got, "{") {
		return true
	}
	if strings.HasPrefix(got, "{") && typeContainsObject(expected) {
		return true
	}
	if strings.HasPrefix(expected, "{") && strings.HasPrefix(got, "{") {
		return c.compareStructuralTypesCompileTime(got, expected)
	}
	if strings.HasPrefix(got, "{") && !strings.HasPrefix(expected, "{") {
		if c.hasInterfaceHint(expected) || c.hasInterfaceHint(strings.Split(expected, ":")[0]) {
			ifaceFields := c.getInterfaceFieldsForCompare(expected)
			if ifaceFields != nil {
				gotFields := parseStructuralTypeString(got)
				for name, expectedType := range ifaceFields {
					gotType, ok := gotFields[name]
					if !ok {
						if typeAllowsNull(expectedType) {
							continue
						}
						return false
					}
					if !c.compareCompileTimeTypes(gotType, expectedType) {
						return false
					}
				}
				return true
			}
		}
		return false
	}
	if strings.HasPrefix(expected, "{") && (got == "object" || got == "any") {
		return true
	}
	if strings.HasPrefix(expected, "function(") && got == "function" {
		return true
	}
	if expected == "function" && strings.HasPrefix(got, "function(") {
		return true
	}

	// Handle generic types: e.g. Box:number and Box:any or Box:number and Box:number
	if strings.Contains(got, ":") && strings.Contains(expected, ":") && !strings.HasPrefix(got, "array:") && !strings.HasPrefix(expected, "array:") {
		gotParts := strings.Split(got, ":")
		expectedParts := strings.Split(expected, ":")

		if gotParts[0] != expectedParts[0] {
			return false
		}

		if len(gotParts) != len(expectedParts) {
			return false
		}

		for i := 1; i < len(gotParts); i++ {
			if !c.compareCompileTimeTypes(gotParts[i], expectedParts[i]) {
				return false
			}
		}
		return true
	}

	// Raw generic type compatibility
	if strings.Contains(got, ":") && !strings.Contains(expected, ":") && !strings.HasPrefix(got, "array:") {
		gotBase := strings.Split(got, ":")[0]
		if gotBase == expected {
			return true
		}
	}
	if !strings.Contains(got, ":") && strings.Contains(expected, ":") && !strings.HasPrefix(expected, "array:") {
		expectedBase := strings.Split(expected, ":")[0]
		if got == expectedBase {
			return true
		}
	}

	expectedParts := strings.Split(expected, "|")
	for _, part := range expectedParts {
		part = strings.TrimSpace(part)
		if part == "array" {
			part = "array:any"
		}
		if got == part {
			return true
		}

		if c.classImplementsInterface(got, part) {
			return true
		}

		if c.isEnumType(part) {
			if got == "string" || got == "number" || got == part {
				return true
			}
		}

		if c.isEnumType(got) {
			if part == "string" || part == "number" || part == got {
				return true
			}
		}

		if strings.HasPrefix(got, "array:") && strings.HasPrefix(part, "array:") {
			gotElem := strings.TrimPrefix(got, "array:")
			partElem := strings.TrimPrefix(part, "array:")
			if c.compareCompileTimeTypes(gotElem, partElem) {
				return true
			}
		}

		if part == "object" && (strings.HasPrefix(got, "{") || strings.HasPrefix(got, "class:") || strings.HasPrefix(got, "interface:") || got == "object" || c.hasInterfaceHint(got)) {
			return true
		}

		if got == "object" {
			if c.hasInterfaceHint(part) {
				return true
			}

			// Handle generic interface: Box:number is compatible with object
			if strings.Contains(part, ":") {
				base := strings.Split(part, ":")[0]
				if c.hasInterfaceHint(base) {
					return true
				}
			}
		}
	}
	return false
}

func (c *Compiler) compareStructuralTypesCompileTime(got string, expected string) bool {
	gotFields := parseStructuralTypeString(got)
	expectedFields := parseStructuralTypeString(expected)
	for name, expectedType := range expectedFields {
		gotType, ok := gotFields[name]
		if !ok {
			return false
		}
		if !c.compareCompileTimeTypes(gotType, expectedType) {
			return false
		}
	}
	return true
}

func parseStructuralTypeString(s string) map[string]string {
	fields := map[string]string{}
	s = strings.TrimPrefix(s, "{")
	s = strings.TrimSuffix(s, "}")
	if s == "" {
		return fields
	}
	parts := SplitTopLevelTypeList(s, ',')
	for _, part := range parts {
		kv := splitTopLevelCompiler(strings.TrimSpace(part), ':', 2)
		if len(kv) == 2 {
			fields[strings.TrimSpace(strings.TrimSuffix(kv[0], "?"))] = strings.TrimSpace(kv[1])
		}
	}
	return fields
}

func splitTopLevelCompiler(s string, sep rune, n int) []string {
	parts := SplitTopLevelTypeList(s, sep)
	if n <= 0 || len(parts) <= n {
		return parts
	}
	merged := append([]string{}, parts[:n-1]...)
	merged = append(merged, strings.Join(parts[n-1:], string(sep)))
	return merged
}

func typeAllowsNull(typeName string) bool {
	for _, part := range SplitTopLevelTypeList(typeName, '|') {
		if strings.TrimSpace(part) == "null" {
			return true
		}
	}
	return false
}

func typeContainsObject(typeName string) bool {
	for _, part := range SplitTopLevelTypeList(typeName, '|') {
		if strings.TrimSpace(part) == "object" {
			return true
		}
	}
	return false
}

func (c *Compiler) hasInterfaceHint(name string) bool {
	if _, exists := c.interfaces[name]; exists {
		return true
	}

	if fullName, exists := c.currentNamespaceInterfaces[name]; exists {
		if _, exists := c.interfaces[fullName]; exists {
			return true
		}
	}

	if HasStandardInterfaceHint(name) {
		return true
	}

	if dot := strings.LastIndex(name, "."); dot >= 0 {
		shortName := name[dot+1:]
		if _, exists := c.interfaces[shortName]; exists {
			return true
		}
	}

	if !strings.Contains(name, ".") {
		for key := range c.interfaces {
			if strings.HasSuffix(key, "."+name) {
				return true
			}
		}
	}

	return false
}

func (c *Compiler) getInterfaceFieldsForCompare(name string) map[string]string {
	iface, ok := c.resolveInterfaceForCompare(name)
	if !ok {
		return nil
	}
	fields := map[string]string{}
	for fname, ftype := range iface.Fields {
		fields[fname] = ftype.Name
	}
	return fields
}

func (c *Compiler) resolveInterfaceForCompare(name string) (Interface, bool) {
	baseName := name
	typeArgs := []string{}
	if strings.Contains(name, ":") {
		parts := SplitTopLevelTypeList(name, ':')
		baseName = parts[0]
		if len(parts) > 1 {
			typeArgs = parts[1:]
		}
	}

	resolveBase := func(candidate string) (Interface, bool) {
		if iface, exists := c.interfaces[candidate]; exists {
			return iface, true
		}
		if fullName, exists := c.currentNamespaceInterfaces[candidate]; exists {
			if iface, exists := c.interfaces[fullName]; exists {
				return iface, true
			}
		}
		if iface, exists := GetStandardInterfaceHint(candidate); exists {
			return iface, true
		}
		if dot := strings.LastIndex(candidate, "."); dot >= 0 {
			shortName := candidate[dot+1:]
			if iface, exists := c.interfaces[shortName]; exists {
				return iface, true
			}
		}
		if !strings.Contains(candidate, ".") {
			for key, iface := range c.interfaces {
				if strings.HasSuffix(key, "."+candidate) {
					return iface, true
				}
			}
		}
		return Interface{}, false
	}

	iface, ok := resolveBase(baseName)
	if !ok {
		return Interface{}, false
	}
	iface = mergeInterfaceExtendsForCompare(iface, resolveBase, map[string]bool{})

	if len(typeArgs) > 0 && len(iface.TypeParameters) > 0 {
		subst := map[string]string{}
		for i, tp := range iface.TypeParameters {
			if i < len(typeArgs) {
				subst[tp] = typeArgs[i]
			}
		}
		fields := map[string]TypeHint{}
		for fname, ftype := range iface.Fields {
			fields[fname] = TypeHintFromString(substituteTypeHintForCompare(ftype.Name, subst))
		}
		iface.Fields = fields
	}

	return iface, true
}

func mergeInterfaceExtendsForCompare(iface Interface, resolveBase func(string) (Interface, bool), visiting map[string]bool) Interface {
	if visiting[iface.Name] {
		return iface
	}
	visiting[iface.Name] = true
	defer delete(visiting, iface.Name)

	fields := map[string]TypeHint{}
	for _, parentName := range iface.Extends {
		parentBase := parentName
		if strings.Contains(parentBase, ":") {
			parentBase = SplitTopLevelTypeList(parentBase, ':')[0]
		}
		parent, ok := resolveBase(parentBase)
		if !ok || visiting[parent.Name] {
			continue
		}
		parent = mergeInterfaceExtendsForCompare(parent, resolveBase, visiting)
		for fname, ftype := range parent.Fields {
			fields[fname] = ftype
		}
	}
	for fname, ftype := range iface.Fields {
		fields[fname] = ftype
	}
	iface.Fields = fields
	return iface
}

func substituteTypeHintForCompare(typeName string, subst map[string]string) string {
	if val, exists := subst[typeName]; exists {
		return val
	}
	if strings.Contains(typeName, "|") {
		parts := SplitTopLevelTypeList(typeName, '|')
		for i, part := range parts {
			parts[i] = substituteTypeHintForCompare(strings.TrimSpace(part), subst)
		}
		return strings.Join(parts, " | ")
	}
	if strings.Contains(typeName, ":") {
		parts := SplitTopLevelTypeList(typeName, ':')
		for i, part := range parts {
			parts[i] = substituteTypeHintForCompare(strings.TrimSpace(part), subst)
		}
		return strings.Join(parts, ":")
	}
	return typeName
}

func (c *Compiler) checkCompileTimeArguments(fnName string, args []Expr, params []Param, line int, col int) {
	minArgs := 0
	hasVariadic := false
	for _, p := range params {
		if p.Variadic {
			hasVariadic = true
			continue
		}
		if !p.HasDefault {
			minArgs++
		}
	}

	if !hasVariadic && len(args) > len(params) {
		c.setLocation(c.currentFile, line, col)
		c.fatalError(ErrorType, "wrong argument count for %s: expected at most %d, got %d", fnName, len(params), len(args))
	} else if len(args) < minArgs {
		c.setLocation(c.currentFile, line, col)
		c.fatalError(ErrorType, "wrong argument count for %s: expected at least %d, got %d", fnName, minArgs, len(args))
	}

	for i, arg := range args {
		if i >= len(params) {
			break
		}

		param := params[i]
		if param.TypeHint.IsEmpty() || param.TypeHint.Name == "any" {
			continue
		}

		argType := c.inferCompileTimeType(arg)
		if argType == "any" {
			continue
		}

		if !c.compareCompileTimeTypes(argType, param.TypeHint.Name) {
			c.setLocation(c.currentFile, line, col)
			c.fatalError(ErrorType, "cannot pass %s to parameter '%s' of function '%s' (expected %s)", argType, param.Name, fnName, param.TypeHint.Name)
		}
	}
}

func (c *Compiler) checkCompileTimeClassArguments(className string, args []Expr, typeArgs []TypeHint, line int, col int) {
	cls, exists := c.classes[className]
	if !exists {
		return
	}

	subst := map[string]string{}
	for i, paramName := range cls.TypeParameters {
		if i < len(typeArgs) {
			subst[paramName] = typeArgs[i].Name
		}
	}

	initMethodName := className + ".init"
	if fn, exists := c.functions[initMethodName]; exists {
		params := fn.Params
		if len(params) > 0 && params[0].Name == "this" {
			params = params[1:]
		}

		substitutedParams := make([]Param, len(params))
		for i, p := range params {
			substitutedParams[i] = p
			substitutedParams[i].TypeHint = TypeHint{
				Name:  c.substituteTypeHintName(p.TypeHint.Name, subst),
				Types: p.TypeHint.Types,
			}
		}

		c.checkCompileTimeArguments("class "+className+" constructor", args, substitutedParams, line, col)
	} else {
		if len(args) > 0 {
			c.setLocation(c.currentFile, line, col)
			c.fatalError(ErrorType, "class %s constructor expects 0 arguments, got %d", className, len(args))
		}
	}
}

func (c *Compiler) checkCompileTimeFunctionArguments(fnName string, args []Expr, typeArgs []TypeHint, line int, col int) {
	fn, exists := c.functions[fnName]
	if !exists {
		return
	}

	subst := map[string]string{}
	for i, paramName := range fn.TypeParameters {
		if i < len(typeArgs) {
			subst[paramName] = typeArgs[i].Name
		}
	}

	substitutedParams := make([]Param, len(fn.Params))
	for i, p := range fn.Params {
		substitutedParams[i] = p
		substitutedParams[i].TypeHint = TypeHint{
			Name:  c.substituteTypeHintName(p.TypeHint.Name, subst),
			Types: p.TypeHint.Types,
		}
	}

	c.checkCompileTimeArguments(fnName, args, substitutedParams, line, col)
}

func (c *Compiler) substituteTypeHintName(typeName string, subst map[string]string) string {
	if val, exists := subst[typeName]; exists {
		return val
	}
	if strings.Contains(typeName, "|") {
		parts := strings.Split(typeName, "|")
		for i, part := range parts {
			parts[i] = c.substituteTypeHintName(strings.TrimSpace(part), subst)
		}
		return strings.Join(parts, "|")
	}
	if strings.Contains(typeName, ":") {
		parts := strings.Split(typeName, ":")
		for i, part := range parts {
			parts[i] = c.substituteTypeHintName(part, subst)
		}
		return strings.Join(parts, ":")
	}
	return typeName
}

func (c *Compiler) inferTypeParamInCompiler(paramType string, argType string, tp string) (string, bool) {
	paramType = strings.TrimSpace(paramType)
	argType = strings.TrimSpace(argType)

	if paramType == tp {
		return argType, true
	}

	if strings.HasPrefix(paramType, "array:") && strings.HasPrefix(argType, "array:") {
		return c.inferTypeParamInCompiler(strings.TrimPrefix(paramType, "array:"), strings.TrimPrefix(argType, "array:"), tp)
	}

	if strings.Contains(paramType, ":") && strings.Contains(argType, ":") {
		pParts := strings.Split(paramType, ":")
		aParts := strings.Split(argType, ":")
		if len(pParts) == len(aParts) && pParts[0] == aParts[0] {
			for i := 1; i < len(pParts); i++ {
				if res, ok := c.inferTypeParamInCompiler(pParts[i], aParts[i], tp); ok {
					return res, true
				}
			}
		}
	}

	return "", false
}

func (c *Compiler) eraseTypeHint(hint TypeHint) TypeHint {
	return c.eraseTypeHintWithParams(hint, c.activeTypeParams)
}

func (c *Compiler) eraseTypeHintWithParams(hint TypeHint, tps []string) TypeHint {
	if hint.IsEmpty() {
		return hint
	}

	erasedName := c.eraseTypeHintNameWithParams(hint.Name, tps)
	erasedTypes := make([]string, len(hint.Types))
	for i, t := range hint.Types {
		erasedTypes[i] = c.eraseTypeHintNameWithParams(t, tps)
	}

	return TypeHint{
		Name:  erasedName,
		Types: erasedTypes,
	}
}

func (c *Compiler) eraseTypeHintNameWithParams(name string, tps []string) string {
	if name == "" {
		return ""
	}
	for _, tp := range tps {
		if name == tp {
			return "any"
		}
	}
	if strings.Contains(name, "|") {
		parts := strings.Split(name, "|")
		for i, part := range parts {
			parts[i] = c.eraseTypeHintNameWithParams(strings.TrimSpace(part), tps)
		}
		return strings.Join(parts, "|")
	}
	if strings.HasPrefix(name, "array:") {
		elemType := strings.TrimPrefix(name, "array:")
		return "array:" + c.eraseTypeHintNameWithParams(elemType, tps)
	}
	if strings.Contains(name, ":") {
		parts := strings.Split(name, ":")
		for i, part := range parts {
			parts[i] = c.eraseTypeHintNameWithParams(part, tps)
		}
		return strings.Join(parts, ":")
	}
	return name
}

func (c *Compiler) eraseFunctionGenericsForVM() {
	for name, fn := range c.functions {
		var tps []string
		if dot := strings.Index(name, "."); dot >= 0 {
			className := name[:dot]
			if class, exists := c.classes[className]; exists {
				tps = append(tps, class.TypeParameters...)
			}
		}
		tps = append(tps, fn.TypeParameters...)

		erasedParams := make([]Param, len(fn.Params))
		for i, p := range fn.Params {
			erasedParams[i] = p
			erasedParams[i].TypeHint = c.eraseTypeHintWithParams(p.TypeHint, tps)
		}
		fn.Params = erasedParams
		fn.ReturnType = c.eraseTypeHintWithParams(fn.ReturnType, tps)

		c.functions[name] = fn
	}
}

func (c *Compiler) collectNativeFnStmts(stmts []Stmt, prefix string, out *[]NativeFnStmt) {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)

		switch s := stmt.(type) {
		case NativeFnStmt:
			fullName := s.Name
			if prefix != "" {
				fullName = prefix + "." + s.Name
			}
			s.Name = fullName
			*out = append(*out, s)

		case NamespaceStmt:
			nextPrefix := s.Name
			if prefix != "" {
				nextPrefix = prefix + "." + s.Name
			}
			c.collectNativeFnStmts(s.Statements, nextPrefix, out)
		}
	}
}

func (c *Compiler) compileNativeFunctions(statements []Stmt) {
	nativeFns := []NativeFnStmt{}
	c.collectNativeFnStmts(statements, "", &nativeFns)

	if len(nativeFns) == 0 {
		return
	}

	var goSource strings.Builder
	goSource.WriteString("package main\n")
	goSource.WriteString("import \"C\"\n")

	imports := map[string]bool{}

	for i := range nativeFns {
		lines := strings.Split(nativeFns[i].GoCode, "\n")
		cleanBody := []string{}
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "import ") {
				imp := strings.TrimSpace(strings.TrimPrefix(trimmed, "import "))
				imports[imp] = true
			} else {
				cleanBody = append(cleanBody, line)
			}
		}
		nativeFns[i].GoCode = strings.Join(cleanBody, "\n")
	}

	goSource.WriteString("import (\n")
	for _, imp := range sortedNativeImports(imports) {
		goSource.WriteString("    ")
		goSource.WriteString(imp)
		goSource.WriteString("\n")
	}
	goSource.WriteString(")\n\n")

	stringRegex := regexp.MustCompile(`"(?:\\.|[^"\\])*"|` + "`" + `[^` + "`" + `]*` + "`")

	for i, fn := range nativeFns {
		var stashedStrings []string
		placeholderPrefix := "__TINY_STR_STASH_"

		stashedCode := stringRegex.ReplaceAllStringFunc(nativeFns[i].GoCode, func(matched string) string {
			placeholder := fmt.Sprintf("%s%d__", placeholderPrefix, len(stashedStrings))
			stashedStrings = append(stashedStrings, matched)
			return placeholder
		})

		for _, targetFn := range nativeFns {
			parts := strings.Split(targetFn.Name, ".")
			shortName := parts[len(parts)-1]
			sanitizedName := strings.ReplaceAll(targetFn.Name, ".", "_")

			re := regexp.MustCompile(`\b` + regexp.QuoteMeta(shortName) + `\b`)
			stashedCode = re.ReplaceAllString(stashedCode, sanitizedName)
		}

		for idx, original := range stashedStrings {
			placeholder := fmt.Sprintf("%s%d__", placeholderPrefix, idx)
			stashedCode = strings.Replace(stashedCode, placeholder, original, 1)
		}

		nativeFns[i].GoCode = stashedCode

		sanitizedName := strings.ReplaceAll(fn.Name, ".", "_")

		goSource.WriteString("//export ")
		goSource.WriteString(sanitizedName)
		goSource.WriteString("\n")

		params := []string{}
		for _, param := range fn.Params {
			if strings.Contains(param.TypeHint.Name, "|") {
				LangErrorAt(ErrorSyntax, fn.File, fn.Line, fn.Column, "native functions cannot have union types")
			}
			typ := ""
			switch param.TypeHint.Name {
			case "string":
				typ = "string"
			case "bool":
				typ = "bool"
			case "number":
				typ = "float64"
			case "array":
				typ = "[]float64"
			default:
				LangErrorAt(ErrorSyntax, fn.File, fn.Line, fn.Column, "invalid parameter '%s' type for native function '%s': only 'string', 'array', 'bool' or 'number' are allowed.", param.Name, fn.Name)
			}
			params = append(params, param.Name+" "+typ)
		}

		if strings.Contains(fn.ReturnType.Name, "|") {
			LangErrorAt(ErrorSyntax, fn.File, fn.Line, fn.Column, "native functions cannot have union types")
		}

		retType := ""
		switch fn.ReturnType.Name {
		case "string":
			retType = "string"
		case "bool":
			retType = "bool"
		case "array":
			retType = "[]float64"
		case "null":
			retType = ""
		case "number":
			retType = "float64"
		default:
			LangErrorAt(ErrorSyntax, fn.File, fn.Line, fn.Column, "invalid return type for native function '%s': only 'string', 'array', 'bool', 'null', or 'number' are allowed.", fn.Name)
		}

		signature := fmt.Sprintf("func %s(%s) %s", sanitizedName, strings.Join(params, ", "), retType)
		goSource.WriteString(signature)
		goSource.WriteString(" {\n")
		goSource.WriteString(stashedCode)
		goSource.WriteString("\n")
		goSource.WriteString("}\n\n")
	}

	goSource.WriteString("func main() {}\n")

	hashBytes := sha256.Sum256([]byte(goSource.String()))
	hashStr := hex.EncodeToString(hashBytes[:])

	cacheDir := filepath.Join(os.TempDir(), "tiny_compiler_cache")
	err := os.MkdirAll(cacheDir, 0755)
	if err != nil {
		c.fatalError(ErrorInternal, "failed to create compiler cache directory: %v", err)
	}

	cachedWasmFile := filepath.Join(cacheDir, "tiny_native_"+hashStr+".wasm")

	wasmBytes, err := os.ReadFile(cachedWasmFile)
	if err == nil {
		wasmInstr := Instruction{
			Op:    OP_LOAD_WASM,
			Value: wasmBytes,
		}
		c.mainInstructions = append([]Instruction{wasmInstr}, c.mainInstructions...)
		return
	}

	tmpGoFile := filepath.Join(os.TempDir(), "tiny_native_source.go")
	err = os.WriteFile(tmpGoFile, []byte(goSource.String()), 0644)
	if err != nil {
		c.fatalError(ErrorInternal, "failed to write temporary Go source: %v", err)
	}
	defer os.Remove(tmpGoFile)

	cmd := exec.Command("tinygo", "build", "-target=wasi", "-scheduler=none", "-no-debug", "-o", cachedWasmFile, tmpGoFile)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	err = cmd.Run()
	if err != nil {
		os.Remove(cachedWasmFile)
		c.fatalError(ErrorInternal, "failed to compile native functions with TinyGo:\n%s\n%v", stderr.String(), err)
	}

	wasmBytes, err = os.ReadFile(cachedWasmFile)
	if err != nil {
		c.fatalError(ErrorInternal, "failed to read compiled Wasm binary: %v", err)
	}
	wasmInstr := Instruction{
		Op:    OP_LOAD_WASM,
		Value: wasmBytes,
	}

	c.mainInstructions = append([]Instruction{wasmInstr}, c.mainInstructions...)
}

func sortedNativeImports(imports map[string]bool) []string {
	names := make([]string, 0, len(imports))
	for name := range imports {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (c *Compiler) doesVariableEscape(name string, fieldsSet map[string]bool, statements []Stmt) bool {
	escaped := false

	var scanExpr func(e Expr)
	var scanStmt func(s Stmt)

	scanExpr = func(e Expr) {
		if e == nil || escaped {
			return
		}
		switch expr := e.(type) {
		case IdentExpr:
			if expr.Name == name {
				escaped = true
			}
		case PropertyExpr:
			if ident, ok := expr.Object.(IdentExpr); ok && ident.Name == name {
				// Accessing property does not escape, UNLESS it doesn't exist in the literal fields
				if !fieldsSet[expr.Name] {
					escaped = true
				}
			} else {
				scanExpr(expr.Object)
			}
		case BinaryExpr:
			scanExpr(expr.Left)
			scanExpr(expr.Right)
		case CallExpr:
			for _, arg := range expr.Args {
				scanExpr(arg)
			}
		case CallValueExpr:
			scanExpr(expr.Callee)
			for _, arg := range expr.Args {
				scanExpr(arg)
			}
		case MemberCallExpr:
			if ident, ok := expr.Object.(IdentExpr); ok && ident.Name == name {
				// Calling method on object. For now we consider it escaping
				// unless we can prove the method doesn't capture 'this'.
				// To be safe, we say it escapes.
				escaped = true
			} else {
				scanExpr(expr.Object)
			}
			for _, arg := range expr.Args {
				scanExpr(arg)
			}
		case ObjectExpr:
			for _, f := range expr.Fields {
				scanExpr(f.Value)
			}
		case ArrayExpr:
			for _, item := range expr.Elements {
				scanExpr(item)
			}
		case TernaryExpr:
			scanExpr(expr.Condition)
			scanExpr(expr.ThenExpr)
			scanExpr(expr.ElseExpr)
		case UnaryExpr:
			scanExpr(expr.Right)
		case InterpolatedStringExpr:
			for _, part := range expr.Parts {
				if part.IsExpr {
					scanExpr(part.Expr)
				}
			}
		case IndexExpr:
			if ident, ok := expr.Object.(IdentExpr); ok && ident.Name == name {
				// obj[index] is like a property access, but if index is not a string literal,
				// it's harder to virtualize. For now, let's say it escapes.
				escaped = true
			} else {
				scanExpr(expr.Object)
			}
			scanExpr(expr.Index)
		}
	}

	scanStmt = func(s Stmt) {
		if s == nil || escaped {
			return
		}
		switch stmt := s.(type) {
		case VariableStmt:
			scanExpr(stmt.Value)
		case AssignStmt:
			if stmt.Name == name {
				escaped = true
			}
			scanExpr(stmt.Value)
		case PropertyAssignStmt:
			if ident, ok := stmt.Object.(IdentExpr); ok && ident.Name == name {
				// assignment to property is fine, UNLESS it doesn't exist in the literal fields
				if !fieldsSet[stmt.Name] {
					escaped = true
				}
			} else {
				scanExpr(stmt.Object)
			}
			scanExpr(stmt.Value)
		case ExprStmt:
			scanExpr(stmt.Value)
		case ReturnStmt:
			if stmt.HasValue {
				scanExpr(stmt.Value)
			}
		case IfStmt:
			scanExpr(stmt.Condition)
			for _, inner := range stmt.ThenBody {
				scanStmt(inner)
			}
			for _, inner := range stmt.ElseBody {
				scanStmt(inner)
			}
		case WhileStmt:
			scanExpr(stmt.Condition)
			for _, inner := range stmt.Body {
				scanStmt(inner)
			}
		case ForStmt:
			scanStmt(stmt.Init)
			scanExpr(stmt.Condition)
			scanStmt(stmt.Update)
			for _, inner := range stmt.Body {
				scanStmt(inner)
			}
		case ForInStmt:
			if stmt.ItemName == name || stmt.IndexName == name {
				escaped = true
			}
			scanExpr(stmt.Iterable)
			for _, inner := range stmt.Body {
				scanStmt(inner)
			}
		case TryCatchStmt:
			for _, inner := range stmt.TryBody {
				scanStmt(inner)
			}
			if stmt.ErrorName == name {
				escaped = true
			}
			for _, inner := range stmt.CatchBody {
				scanStmt(inner)
			}
			for _, inner := range stmt.FinallyBody {
				scanStmt(inner)
			}
		case MatchStmt:
			scanExpr(stmt.Value)
			for _, c := range stmt.Cases {
				scanExpr(c.Value)
				for _, inner := range c.Body {
					scanStmt(inner)
				}
			}
			for _, inner := range stmt.Default {
				scanStmt(inner)
			}
		case NamespaceStmt:
			for _, inner := range stmt.Statements {
				scanStmt(inner)
			}
		case ExportStmt:
			scanStmt(stmt.Inner)
		}
	}

	for _, s := range statements {
		scanStmt(s)
	}

	return escaped
}

func (c *Compiler) performEscapeAnalysis(statements []Stmt) {
	for _, stmt := range statements {
		inner, _ := unwrapExport(stmt)
		if v, ok := inner.(VariableStmt); ok && c.isInsideFunction() {
			if obj, ok := v.Value.(ObjectExpr); ok {
				fieldsSet := map[string]bool{}
				for _, field := range obj.Fields {
					fieldsSet[field.Name] = true
				}
				escaping := c.doesVariableEscape(v.Name, fieldsSet, statements)
				if !escaping {
					fields := map[string]int{}
					for _, field := range obj.Fields {
						slot := c.localCount
						c.localCount++
						fields[field.Name] = slot
					}
					key := VarNodeKey{File: v.File, Line: v.Line, Column: v.Column}
					c.virtualObjects[key] = fields
				}
			}
		}

		// Recurse into blocks
		switch s := inner.(type) {
		case IfStmt:
			c.performEscapeAnalysis(s.ThenBody)
			c.performEscapeAnalysis(s.ElseBody)
		case WhileStmt:
			c.performEscapeAnalysis(s.Body)
		case ForStmt:
			c.performEscapeAnalysis(s.Body)
		case ForInStmt:
			c.performEscapeAnalysis(s.Body)
		case TryCatchStmt:
			c.performEscapeAnalysis(s.TryBody)
			c.performEscapeAnalysis(s.CatchBody)
			c.performEscapeAnalysis(s.FinallyBody)
		case MatchStmt:
			for _, mc := range s.Cases {
				c.performEscapeAnalysis(mc.Body)
			}
			c.performEscapeAnalysis(s.Default)
		case NamespaceStmt:
			c.performEscapeAnalysis(s.Statements)
		case FunctionStmt:
			c.performEscapeAnalysis(s.Body)
		}
	}
}

func (c *Compiler) classImplementsInterface(gotClass string, expectedInterface string) bool {
	if strings.Contains(gotClass, ":") {
		gotClass = strings.Split(gotClass, ":")[0]
	}
	if strings.Contains(expectedInterface, ":") {
		expectedInterface = strings.Split(expectedInterface, ":")[0]
	}

	cls, exists := c.classes[gotClass]
	if !exists {
		return false
	}

	for _, imp := range cls.Implements {
		if strings.Contains(imp, ":") {
			imp = strings.Split(imp, ":")[0]
		}
		if imp == expectedInterface {
			return true
		}
		if strings.HasSuffix(imp, "."+expectedInterface) || strings.HasSuffix(expectedInterface, "."+imp) {
			return true
		}
		if c.interfaceExtendsInterface(imp, expectedInterface, map[string]bool{}) {
			return true
		}
	}
	return false
}

func (c *Compiler) interfaceExtendsInterface(gotInterface string, expectedInterface string, visiting map[string]bool) bool {
	gotInterface = baseTypeName(gotInterface)
	expectedInterface = baseTypeName(expectedInterface)
	if gotInterface == expectedInterface || strings.HasSuffix(gotInterface, "."+expectedInterface) || strings.HasSuffix(expectedInterface, "."+gotInterface) {
		return true
	}
	if visiting[gotInterface] {
		return false
	}
	visiting[gotInterface] = true
	defer delete(visiting, gotInterface)

	iface, ok := c.interfaces[gotInterface]
	if !ok {
		for name, candidate := range c.interfaces {
			if strings.HasSuffix(name, "."+gotInterface) {
				iface = candidate
				ok = true
				break
			}
		}
	}
	if !ok {
		return false
	}
	for _, parent := range iface.Extends {
		parent = baseTypeName(parent)
		if parent == expectedInterface || strings.HasSuffix(parent, "."+expectedInterface) || strings.HasSuffix(expectedInterface, "."+parent) {
			return true
		}
		if c.interfaceExtendsInterface(parent, expectedInterface, visiting) {
			return true
		}
	}
	return false
}

func baseTypeName(name string) string {
	if strings.Contains(name, ":") {
		name = strings.Split(name, ":")[0]
	}
	return name
}

func alwaysReturnsOrThrows(stmt Stmt) bool {
	if stmt == nil {
		return false
	}
	switch s := stmt.(type) {
	case ReturnStmt, ThrowStmt:
		return true
	case IfStmt:
		if len(s.ElseBody) > 0 && alwaysReturnsOrThrowsBlock(s.ThenBody) && alwaysReturnsOrThrowsBlock(s.ElseBody) {
			return true
		}
		return false
	case TryCatchStmt:
		if alwaysReturnsOrThrowsBlock(s.FinallyBody) {
			return true
		}
		return alwaysReturnsOrThrowsBlock(s.TryBody) && alwaysReturnsOrThrowsBlock(s.CatchBody)
	case MatchStmt:
		if len(s.Default) == 0 || !alwaysReturnsOrThrowsBlock(s.Default) {
			return false
		}
		for _, c := range s.Cases {
			if !alwaysReturnsOrThrowsBlock(c.Body) {
				return false
			}
		}
		return true
	}
	return false
}

func alwaysReturnsOrThrowsBlock(stmts []Stmt) bool {
	for _, raw := range stmts {
		stmt, _ := unwrapExport(raw)
		if alwaysReturnsOrThrows(stmt) {
			return true
		}
	}
	return false
}
