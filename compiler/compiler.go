package main

type Compiler struct {
	mainInstructions []Instruction
	functions        map[string]Function

	currentInstructions *[]Instruction

	locals          map[string]bool
	localConstants  map[string]bool
	globalConstants map[string]bool
}

func NewCompiler() *Compiler {
	c := &Compiler{
		mainInstructions: []Instruction{},
		functions:        map[string]Function{},
		locals:           map[string]bool{},
		localConstants:   map[string]bool{},
		globalConstants:  map[string]bool{},
	}

	c.currentInstructions = &c.mainInstructions

	return c
}

func (c *Compiler) CompileProgram(program Program) ([]Instruction, map[string]Function) {
	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}

	c.emit(OP_HALT, nil)

	return c.mainInstructions, c.functions
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

	case PropertyAssignStmt:
		c.compileExpr(s.Object)
		c.compileExpr(s.Value)
		c.emit(OP_SET_PROPERTY, s.Name)

	default:
		langError(ErrorInternal, "unknown statement")
	}
}

func (c *Compiler) compileWhileStatement(stmt WhileStmt) {
	loopStart := len(*c.currentInstructions)

	c.compileExpr(stmt.Condition)

	jumpIfFalseIndex := c.emitJump(OP_JUMP_IF_FALSE)

	for _, bodyStmt := range stmt.Body {
		c.compileStatement(bodyStmt)
	}

	c.emit(OP_JUMP, loopStart)

	c.patchJump(jumpIfFalseIndex)
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
		if e.Object == "core" {
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
