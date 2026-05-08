package main

type LoopContext struct {
	Start         int
	BreakJumps    []int
	ContinueJumps []int
}

type Compiler struct {
	mainInstructions []Instruction
	functions        map[string]Function
	classes          map[string]Class
	loopStack        []LoopContext

	currentInstructions *[]Instruction

	locals          map[string]bool
	localConstants  map[string]bool
	globalConstants map[string]bool
}

func optimizeExpr(expr Expr) Expr {
	switch e := expr.(type) {
	case BinaryExpr:
		left := optimizeExpr(e.Left)
		right := optimizeExpr(e.Right)

		// int + int
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

	default:
		return expr
	}
}

func NewCompiler() *Compiler {
	c := &Compiler{
		mainInstructions: []Instruction{},
		functions:        map[string]Function{},
		classes:          map[string]Class{},
		loopStack:        []LoopContext{},
		locals:           map[string]bool{},
		localConstants:   map[string]bool{},
		globalConstants:  map[string]bool{},
	}

	c.currentInstructions = &c.mainInstructions

	return c
}

func (c *Compiler) CompileProgram(program Program) ([]Instruction, map[string]Function, map[string]Class) {
	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	return c.mainInstructions, c.functions, c.classes
}

func (c *Compiler) compileStatement(stmt Stmt) {
	switch s := stmt.(type) {
	case VariableStmt:
		c.compileExpr(s.Value)

		if c.isInsideFunction() {
			c.locals[s.Name] = true
			c.localConstants[s.Name] = s.Constant
			c.emit(OP_STORE_LOCAL, VariableInfo{
				Name:     s.Name,
				Constant: s.Constant,
			})
		} else {
			c.globalConstants[s.Name] = s.Constant
			c.emit(OP_STORE_GLOBAL, VariableInfo{
				Name:     s.Name,
				Constant: s.Constant,
			})
		}

	case AssignStmt:
		c.compileExpr(s.Value)

		if c.isInsideFunction() && c.locals[s.Name] {
			c.emit(OP_ASSIGN_LOCAL, s.Name)
		} else {
			c.emit(OP_ASSIGN_GLOBAL, s.Name)
		}

	case BreakStmt:
		c.compileBreakStatement()

	case ContinueStmt:
		c.compileContinueStatement()

	case ExprStmt:
		c.compileExpr(s.Value)
		c.emit(OP_POP, nil)

	case FunctionStmt:
		c.compileFunction(s)

	case ReturnStmt:
		if s.HasValue {
			c.compileExpr(s.Value)
		} else {
			c.emit(OP_CONST, UndefinedValue{})
		}

		c.emit(OP_RETURN, nil)

	case ImportStmt:
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

func (c *Compiler) compileForStatement(stmt ForStmt) {
	if stmt.Init != nil {
		c.compileStatement(stmt.Init)
	}

	loopStart := len(*c.currentInstructions)

	c.loopStack = append(c.loopStack, LoopContext{
		Start: loopStart,
	})

	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

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

func (c *Compiler) compileClass(stmt ClassStmt) {
	if _, exists := c.classes[stmt.Name]; exists {
		langError(ErrorName, "class already defined: %s", stmt.Name)
	}

	methods := map[string]string{}

	for _, method := range stmt.Methods {
		compiledName := stmt.Name + "." + method.Name

		methods[method.Name] = compiledName

		classMethod := FunctionStmt{
			Name:   compiledName,
			Params: method.Params,
			Body:   method.Body,
		}

		c.compileFunction(classMethod)
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

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

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

	for _, bodyStmt := range stmt.ThenBody {
		c.compileStatement(bodyStmt)
	}

	if len(stmt.ElseBody) > 0 {
		jumpOverElseIndex := c.emitJump(OP_JUMP)

		c.patchJump(jumpIfFalseIndex)

		for _, bodyStmt := range stmt.ElseBody {
			c.compileStatement(bodyStmt)
		}

		c.patchJump(jumpOverElseIndex)
	} else {
		c.patchJump(jumpIfFalseIndex)
	}
}

func (c *Compiler) compileFunction(stmt FunctionStmt) {
	if _, exists := c.functions[stmt.Name]; exists {
		langError(ErrorName, "function already defined: %s", stmt.Name)
	}

	oldInstructions := c.currentInstructions
	oldLocals := c.locals
	oldLocalConstants := c.localConstants

	functionInstructions := []Instruction{}
	c.currentInstructions = &functionInstructions
	c.locals = map[string]bool{}
	c.localConstants = map[string]bool{}

	for _, param := range stmt.Params {
		c.locals[param] = true
		c.localConstants[param] = false
	}

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_CONST, 0)
	c.emit(OP_RETURN, nil)

	c.functions[stmt.Name] = Function{
		Name:         stmt.Name,
		Params:       stmt.Params,
		Instructions: functionInstructions,
	}

	c.currentInstructions = oldInstructions
	c.locals = oldLocals
	c.localConstants = oldLocalConstants
}

func (c *Compiler) compileExpr(expr Expr) {
	expr = optimizeExpr(expr)

	switch e := expr.(type) {
	case StringExpr:
		c.emit(OP_CONST, e.Value)

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
		c.compileExpr(e.Array)
		c.compileExpr(e.Index)
		c.emit(OP_INDEX, nil)

	case NumberExpr:
		c.emit(OP_CONST, e.Value)

	case FloatExpr:
		c.emit(OP_CONST, e.Value)

	case IdentExpr:
		if c.locals[e.Name] {
			c.emit(OP_LOAD_LOCAL, e.Name)
		} else if _, exists := c.functions[e.Name]; exists {
			c.emit(OP_CONST, FunctionValue{Name: e.Name})
		} else {
			c.emit(OP_LOAD_GLOBAL, e.Name)
		}

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

		default:
			langError(ErrorInternal, "unknown binary operator")
		}

	case CallExpr:
		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.emit(OP_CALL, CallInfo{
			Name:     e.Name,
			ArgCount: len(e.Args),
		})

	case MemberCallExpr:
		// Keep core.println(), core.input(), etc. as builtins.
		if e.Object == "Core" || e.Object == "Math" {
			for _, arg := range e.Args {
				c.compileExpr(arg)
			}

			c.emit(OP_BUILTIN_CALL, BuiltinCallInfo{
				Object:   e.Object,
				Method:   e.Method,
				ArgCount: len(e.Args),
			})

			return
		}

		// Object method call: user.greet()
		c.compileExpr(IdentExpr{Name: e.Object})

		for _, arg := range e.Args {
			c.compileExpr(arg)
		}

		c.emit(OP_METHOD_CALL, MethodCallInfo{
			Method:   e.Method,
			ArgCount: len(e.Args),
		})

	case ThisExpr:
		if !c.isInsideFunction() {
			langError(ErrorSyntax, "cannot use this outside of a function")
		}

		c.emit(OP_LOAD_LOCAL, "this")

	default:
		langError(ErrorInternal, "unknown expression")
	}
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
