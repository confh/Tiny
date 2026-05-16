package vm

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	. "language.com/src/tinyerrors"
)

type TryHandler struct {
	CatchIP int
	Name    string
	Slot    int
	IsLocal bool

	FrameDepth int
}

type Frame struct {
	function     Function
	ip           int
	locals       []*Cell
	constants    []bool
	instructions []Instruction
	localTypes   []TypeHint

	returnOverride    Value
	hasReturnOverride bool
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	functions        map[string]Function
	classes          map[string]Class

	lastInstruction      Instruction
	lastInstructionIndex int
	lastFunctionName     string

	globalTypes map[string]TypeHint

	cliArgs []string

	tryHandlers []TryHandler

	mu sync.Mutex

	ip int

	stack           []Value
	globals         map[string]Value
	globalConstants map[string]bool

	frames []Frame
}

func NewVM(mainInstructions []Instruction, functions map[string]Function, classes map[string]Class) *VM {
	return &VM{
		start:            time.Now().UnixMilli(),
		mainInstructions: mainInstructions,
		functions:        functions,
		classes:          classes,
		globals:          map[string]Value{},
		globalConstants:  map[string]bool{},
		mu:               sync.Mutex{},
		cliArgs:          []string{},
		globalTypes:      map[string]TypeHint{},
	}
}

func (vm *VM) SetCLIArgs(args []string) {
	vm.cliArgs = args
}

func (vm *VM) CloneForTask() *VM {
	return &VM{
		mainInstructions: vm.mainInstructions,
		functions:        vm.functions,
		classes:          vm.classes,

		stack:       []Value{},
		frames:      []Frame{},
		tryHandlers: []TryHandler{},

		globals:         vm.globals,
		globalConstants: vm.globalConstants,

		cliArgs: vm.cliArgs,
	}
}

func (vm *VM) callClassWithArgs(class Class, args []Value) {
	object := ObjectValue{}

	for methodName, functionName := range class.Methods {
		object[methodName] = FunctionValue{
			Name: functionName,
		}
	}

	if initName, exists := class.Methods["init"]; exists {
		fn, ok := vm.functions[initName]
		if !ok {
			LangError(ErrorName, "undefined init function: %s", initName)
		}

		expectedArgs := len(fn.Params) - 1

		if expectedArgs != len(args) {
			LangError(
				ErrorRuntime,
				"class %s constructor expects %d arguments, got %d",
				class.Name,
				expectedArgs,
				len(args),
			)
		}

		locals := make([]*Cell, fn.LocalCount)
		constants := make([]bool, fn.LocalCount)

		locals[0] = &Cell{Value: object}
		constants[0] = true

		for i, arg := range args {
			locals[i+1] = &Cell{Value: arg}
			constants[i+1] = false
		}

		for i := range locals {
			if locals[i] == nil {
				locals[i] = &Cell{Value: UndefinedValue{}}
			}
		}

		frameDepthBefore := len(vm.frames)
		localTypes := make([]TypeHint, fn.LocalCount)

		frame := Frame{
			function:     fn,
			ip:           0,
			locals:       locals,
			constants:    constants,
			instructions: fn.Instructions,
			localTypes:   localTypes,
		}

		vm.frames = append(vm.frames, frame)

		for len(vm.frames) > frameDepthBefore {
			if vm.step() {
				LangError(ErrorRuntime, "program halted while running constructor")
			}
		}

		if len(vm.stack) > 0 {
			vm.pop()
		}
	}

	vm.push(object)
}

func (vm *VM) callClassByName(name string, args []Value) {
	class, exists := vm.classes[name]
	if !exists {
		LangError(ErrorName, "undefined class: %s", name)
	}

	vm.callClassWithArgs(class, args)
}

func (vm *VM) step() bool {
	instructions := vm.currentInstructions()
	ip := vm.currentIP()

	if ip < 0 || ip >= len(instructions) {
		LangError(ErrorInternal, "instruction pointer out of range")
	}

	instr := instructions[ip]

	vm.lastInstruction = instr
	vm.lastInstructionIndex = ip // use whatever variable stores current instruction index

	if len(vm.frames) > 0 {
		vm.lastFunctionName = vm.frames[len(vm.frames)-1].function.Name
	} else {
		vm.lastFunctionName = "<main>"
	}

	vm.incrementIP()

	switch instr.Op {
	case OP_CLOSURE:
		info := instr.Value.(ClosureInfo)

		captures := map[int]*Cell{}

		if len(info.Captures) > 0 {
			if len(vm.frames) == 0 {
				LangError(ErrorInternal, "closure has captures but no current function frame")
			}

			frame := vm.currentFrame()

			for _, capture := range info.Captures {
				if capture.OuterSlot < 0 || capture.OuterSlot >= len(frame.locals) {
					LangError(
						ErrorInternal,
						"capture slot out of range: function=%s outerSlot=%d locals=%d",
						frame.function.Name,
						capture.OuterSlot,
						len(frame.locals),
					)
				}

				if frame.locals[capture.OuterSlot] == nil {
					LangError(
						ErrorInternal,
						"captured local is nil: function=%s outerSlot=%d",
						frame.function.Name,
						capture.OuterSlot,
					)
				}

				captures[capture.InnerSlot] = frame.locals[capture.OuterSlot]
			}
		}

		vm.push(FunctionValue{
			Name:     info.Name,
			Captures: captures,
		})

	case OP_CONST:
		vm.push(instr.Value)

	case OP_SET_PROPERTY:
		name := instr.Value.(string)

		value := vm.pop()
		objectValue := vm.pop()

		object, ok := objectValue.(ObjectValue)
		if !ok {
			LangError(ErrorType, "expected object, got %s", typeName(objectValue))
		}

		object[name] = value

	case OP_LOAD_GLOBAL:
		name := instr.Value.(string)

		value, ok := vm.globals[name]
		if !ok {
			LangError(ErrorName, "undefined global variable: %s", name)
		}

		vm.push(value)

	case OP_SETUP_TRY:
		info := instr.Value.(TryInfo)

		vm.tryHandlers = append(vm.tryHandlers, TryHandler{
			CatchIP:    info.CatchIP,
			Name:       info.Name,
			Slot:       info.Slot,
			IsLocal:    info.IsLocal,
			FrameDepth: len(vm.frames),
		})

	case OP_POP_TRY:
		if len(vm.tryHandlers) == 0 {
			LangError(ErrorInternal, "try handler stack underflow")
		}

		vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

	case OP_STORE_GLOBAL:
		info := instr.Value.(VariableInfo)
		value := vm.pop()

		if !checkTypeHint(value, info.TypeHint) {
			LangError(
				ErrorType,
				"variable %s expected %s, got %s",
				info.Name,
				info.TypeHint.Name,
				typeName(value),
			)
		}

		vm.globals[info.Name] = value
		vm.globalConstants[info.Name] = info.Constant
		vm.globalTypes[info.Name] = info.TypeHint

	case OP_LOAD_LOCAL:
		slot := instr.Value.(int)
		frame := vm.currentFrame()

		if slot < 0 || slot >= len(frame.locals) {
			LangError(
				ErrorInternal,
				"local slot out of range: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.locals[slot] == nil {
			LangError(
				ErrorInternal,
				"local slot is nil: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		vm.push(frame.locals[slot].Value)

	case OP_STORE_LOCAL:
		info := instr.Value.(VariableInfo)
		value := vm.pop()

		if !checkTypeHint(value, info.TypeHint) {
			LangError(
				ErrorType,
				"variable %s expected %s, got %s",
				info.Name,
				info.TypeHint.Name,
				typeName(value),
			)
		}

		frame := vm.currentFrame()

		frame.locals[info.Slot] = &Cell{Value: value}
		frame.constants[info.Slot] = info.Constant
		frame.localTypes[info.Slot] = info.TypeHint

	case OP_ASSIGN_GLOBAL:
		name := instr.Value.(string)
		value := vm.pop()

		if vm.globalConstants[name] {
			LangError(ErrorConst, "cannot assign to constant global")
		}

		hint := vm.globalTypes[name]

		if !checkTypeHint(value, hint) {
			LangError(
				ErrorType,
				"global %s expected %s, got %s",
				name,
				hint.Name,
				typeName(value),
			)
		}

		vm.globals[name] = value

	case OP_ASSIGN_LOCAL:
		slot := instr.Value.(int)
		value := vm.pop()

		frame := vm.currentFrame()

		if slot < 0 || slot >= len(frame.locals) {
			LangError(
				ErrorInternal,
				"local slot out of range: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.locals[slot] == nil {
			LangError(
				ErrorInternal,
				"local slot is nil during assignment: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.constants[slot] {
			LangError(ErrorConst, "cannot assign to constant local")
		}

		frame.locals[slot].Value = value

	case OP_ADD:
		right := vm.pop()
		left := vm.pop()

		if isNumber(left) && isNumber(right) {
			if _, ok := left.(float64); ok {
				vm.push(asFloat(left) + asFloat(right))
			} else if _, ok := right.(float64); ok {
				vm.push(asFloat(left) + asFloat(right))
			} else {
				vm.push(left.(int) + right.(int))
			}
		} else if isString(left) && isString(right) {
			vm.push(left.(string) + right.(string))
		} else {
			LangError(ErrorType, "cannot add %s and %s", typeName(left), typeName(right))
		}

	case OP_SUB:
		right := vm.pop()
		left := vm.pop()

		if isNumber(left) && isNumber(right) {
			if _, ok := left.(float64); ok {
				vm.push(asFloat(left) - asFloat(right))
			} else if _, ok := right.(float64); ok {
				vm.push(asFloat(left) - asFloat(right))
			} else if _, ok := right.(int64); ok {
				vm.push(int(left.(int64)) - int(right.(int64)))
			} else {
				vm.push(left.(int) - right.(int))
			}
		} else {
			LangError(ErrorType, "cannot subtract %s and %s", typeName(left), typeName(right))
		}

	case OP_MUL:
		right := vm.pop()
		left := vm.pop()

		if isNumber(left) && isNumber(right) {
			if _, ok := left.(float64); ok {
				vm.push(asFloat(left) * asFloat(right))
			} else if _, ok := right.(float64); ok {
				vm.push(asFloat(left) * asFloat(right))
			} else {
				vm.push(left.(int) * right.(int))
			}
		} else {
			LangError(ErrorType, "cannot multiply %s and %s", typeName(left), typeName(right))
		}

	case OP_DIV:
		right := vm.pop()
		left := vm.pop()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot divide %s and %s", typeName(left), typeName(right))
		}

		vm.push(asFloat(left) / asFloat(right))

	case OP_EQ:
		right := vm.pop()
		left := vm.pop()
		vm.push(valuesEqual(left, right))

	case OP_NEQ:
		right := vm.pop()
		left := vm.pop()
		vm.push(!valuesEqual(left, right))

	case OP_LT:
		right := vm.pop()
		left := vm.pop()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
		}

		vm.push(asFloat(left) < asFloat(right))

	case OP_GT:
		right := vm.pop()
		left := vm.pop()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
		}

		vm.push(asFloat(left) > asFloat(right))

	case OP_LTE:
		right := vm.pop()
		left := vm.pop()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
		}

		vm.push(asFloat(left) <= asFloat(right))

	case OP_GTE:
		right := vm.pop()
		left := vm.pop()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
		}

		vm.push(asFloat(left) >= asFloat(right))

	case OP_AND:
		right := vm.pop()
		left := vm.pop()
		vm.push(isTruthy(left) && isTruthy(right))

	case OP_OR:
		right := vm.pop()
		left := vm.pop()
		vm.push(isTruthy(left) || isTruthy(right))

	case OP_JUMP:
		target := instr.Value.(int)
		vm.setIP(target)

	case OP_JUMP_IF_FALSE:
		target := instr.Value.(int)
		condition := vm.pop()

		if !isTruthy(condition) {
			vm.setIP(target)
		}

	case OP_METHOD_CALL:
		info := instr.Value.(MethodCallInfo)

		vm.callMethod(info.Method, info.ArgCount)

	case OP_CALL:
		info := instr.Value.(CallInfo)

		if class, exists := vm.classes[info.Name]; exists {
			vm.callClass(class, info.ArgCount)
			return false
		}

		vm.callFunction(info.Name, info.ArgCount)

	case OP_CALL_VALUE:
		info := instr.Value.(CallInfo)

		args := vm.popArgs(info.ArgCount)

		callee := vm.pop()

		switch v := callee.(type) {
		case FunctionValue:
			result := vm.callFunctionValue(v, args)
			vm.push(result)

		case *FunctionValue:
			result := vm.callFunctionValue(*v, args)
			vm.push(result)

		case Class:
			vm.callClassByName(v.Name, args)

		case *Class:
			vm.callClassByName(v.Name, args)

		default:
			LangError(ErrorType, "expected function or class, got %s", typeName(callee))
		}

	case OP_BUILTIN_CALL:
		info := instr.Value.(BuiltinCallInfo)
		vm.callBuiltin(info.Object, info.Method, info.ArgCount)

	case OP_MOD:
		right := vm.pop()
		left := vm.pop()

		switch l := left.(type) {
		case int:
			r := asInt(right)

			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(l % r)

		case int64:
			r := int64(asInt(right))

			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(l % r)

		default:
			LangError(ErrorType, "cannot modulo %s and %s", typeName(left), typeName(right))
		}

	case OP_ARRAY:
		info := instr.Value.(ArrayInfo)

		elements := make([]Value, info.Count)

		for i := info.Count - 1; i >= 0; i-- {
			elements[i] = vm.pop()
		}

		vm.push(&ArrayValue{Elements: elements})

	case OP_INDEX:
		indexValue := vm.pop()
		objectValue := vm.pop()

		switch obj := objectValue.(type) {
		case *ArrayValue:
			index := asInt(indexValue)

			if index < 0 || index >= len(obj.Elements) {
				LangError(ErrorRuntime, "array index out of range: %d", index)
			}

			vm.push(obj.Elements[index])

		case ObjectValue:
			key := asString(indexValue)

			value, exists := obj[key]
			if !exists {
				LangError(ErrorName, "object has no key: %s", key)
			}

			vm.push(value)

		default:
			LangError(ErrorType, "cannot index %s", typeName(objectValue))
		}

	case OP_SET_INDEX:
		value := vm.pop()
		indexValue := vm.pop()
		objectValue := vm.pop()

		switch obj := objectValue.(type) {
		case *ArrayValue:
			index := asInt(indexValue)

			if index < 0 || index >= len(obj.Elements) {
				LangError(ErrorRuntime, "array index out of range: %d", index)
			}

			obj.Elements[index] = value

		case ObjectValue:
			key := asString(indexValue)
			obj[key] = value

		default:
			LangError(ErrorType, "cannot index assign %s", typeName(objectValue))
		}

	case OP_RETURN:
		var returnValue Value

		if len(vm.stack) == 0 {
			returnValue = UndefinedValue{}
		} else {
			returnValue = vm.pop()
		}

		returningDepth := len(vm.frames)
		vm.removeTryHandlersAtOrAbove(returningDepth)

		frame := vm.frames[len(vm.frames)-1]

		if !checkTypeHint(returnValue, frame.function.ReturnType) {
			LangError(
				ErrorType,
				"function %s should return %s, got %s",
				frame.function.Name,
				frame.function.ReturnType.Name,
				typeName(returnValue),
			)
		}

		if len(vm.frames) == 0 {
			LangError(ErrorRuntime, "return used outside of function")
		}

		vm.frames = vm.frames[:len(vm.frames)-1]

		if frame.hasReturnOverride {
			vm.push(frame.returnOverride)
		} else {
			vm.push(returnValue)
		}

	case OP_THROW:
		value := vm.pop()
		vm.throwValue(value)

	case OP_POP:
		vm.pop()

	case OP_INTERPOLATE:
		info := instr.Value.(InterpolateInfo)

		values := make([]Value, info.ExprCount)

		for i := info.ExprCount - 1; i >= 0; i-- {
			values[i] = vm.pop()
		}

		result := ""

		for i := 0; i < info.ExprCount; i++ {
			result += info.Parts[i]
			result += valueToString(values[i])
		}

		result += info.Parts[len(info.Parts)-1]

		vm.push(result)

	case OP_OBJECT:
		info := instr.Value.(ObjectInfo)

		values := make([]Value, len(info.Names))

		for i := len(info.Names) - 1; i >= 0; i-- {
			values[i] = vm.pop()
		}

		object := ObjectValue{}

		for i, name := range info.Names {
			object[name] = values[i]
		}

		vm.push(object)

	case OP_NOT:
		value := vm.pop()
		vm.push(!isTruthy(value))

	case OP_GET_PROPERTY:
		name := instr.Value.(string)
		objectValue := vm.pop()

		if ns, ok := objectValue.(NamespaceValue); ok {
			value, exists := ns.Members[name]
			if !exists {
				LangError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
			}

			if ref, ok := value.(NamespaceMemberRef); ok {
				actual, exists := vm.globals[ref.GlobalName]
				if !exists {
					LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
				}

				vm.push(actual)
				break
			}

			if ref, ok := value.(*NamespaceMemberRef); ok {
				actual, exists := vm.globals[ref.GlobalName]
				if !exists {
					LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
				}

				vm.push(actual)
				break
			}

			vm.push(value)
			break
		}

		if ns, ok := objectValue.(*NamespaceValue); ok {
			value, exists := ns.Members[name]
			if !exists {
				LangError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
			}

			if ref, ok := value.(NamespaceMemberRef); ok {
				actual, exists := vm.globals[ref.GlobalName]
				if !exists {
					LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
				}

				vm.push(actual)
				break
			}

			if ref, ok := value.(*NamespaceMemberRef); ok {
				actual, exists := vm.globals[ref.GlobalName]
				if !exists {
					LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
				}

				vm.push(actual)
				break
			}

			vm.push(value)
			break
		}

		object, ok := objectValue.(ObjectValue)
		if !ok {
			LangError(ErrorType, "expected object, got %s", typeName(objectValue))
		}

		value, exists := object[name]
		if !exists {
			LangError(ErrorName, "object has no property: %s", name)
		}

		vm.push(value)

	case OP_HALT:
		return true

	default:
		LangError(ErrorInternal, "unknown opcode: %d", instr.Op)
	}

	return false
}

func (vm *VM) removeTryHandlersAtOrAbove(depth int) {
	filtered := vm.tryHandlers[:0]

	for _, handler := range vm.tryHandlers {
		if handler.FrameDepth < depth {
			filtered = append(filtered, handler)
		}
	}

	vm.tryHandlers = filtered
}

func (vm *VM) throwValue(value Value) {
	errorObject := makeErrorObject(value)

	if len(vm.tryHandlers) == 0 {
		message := valueToString(errorObject["message"])
		kind := valueToString(errorObject["kind"])

		panic(LangErrorType{
			Kind:    ErrorKind(kind),
			Message: message,
		})
	}

	handler := vm.tryHandlers[len(vm.tryHandlers)-1]
	vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

	for len(vm.frames) > handler.FrameDepth {
		vm.frames = vm.frames[:len(vm.frames)-1]
	}

	if handler.IsLocal {
		if handler.FrameDepth == 0 {
			LangError(ErrorInternal, "local catch handler has no frame")
		}

		frame := &vm.frames[handler.FrameDepth-1]

		if handler.Slot < 0 || handler.Slot >= len(frame.locals) {
			LangError(ErrorInternal, "catch local slot out of range")
		}

		frame.locals[handler.Slot].Value = errorObject
		frame.constants[handler.Slot] = false
	} else {
		vm.globals[handler.Name] = errorObject
		vm.globalConstants[handler.Name] = false
	}

	if handler.FrameDepth == 0 {
		vm.ip = handler.CatchIP
	} else {
		vm.frames[handler.FrameDepth-1].ip = handler.CatchIP
	}
}

func makeErrorObject(value Value) ObjectValue {
	switch err := value.(type) {
	case ErrorValue:
		return ObjectValue{
			"kind":    err.Kind,
			"message": err.Message,
		}

	case *ErrorValue:
		return ObjectValue{
			"kind":    err.Kind,
			"message": err.Message,
		}

	case ObjectValue:
		return err

	case string:
		return ObjectValue{
			"kind":    "Error",
			"message": err,
		}

	default:
		return ObjectValue{
			"kind":    "Error",
			"message": valueToString(value),
		}
	}
}

func (vm *VM) callFunctionValueWithArgs(fnValue FunctionValue, args []Value) {
	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		LangError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	if len(args) != len(fn.Params) {
		LangError(
			ErrorRuntime,
			"function %s expects %d arguments, got %d",
			fnValue.Name,
			len(fn.Params),
			len(args),
		)
	}

	locals := make([]*Cell, fn.LocalCount)
	constants := make([]bool, fn.LocalCount)

	for i, arg := range args {
		locals[i] = &Cell{Value: arg}
	}

	for slot, cell := range fnValue.Captures {
		if slot < 0 || slot >= len(locals) {
			LangError(ErrorInternal, "closure capture slot out of range")
		}

		locals[slot] = cell
	}

	for i := range locals {
		if locals[i] == nil {
			locals[i] = &Cell{Value: UndefinedValue{}}
		}
	}

	localTypes := make([]TypeHint, fn.LocalCount)

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    constants,
		instructions: fn.Instructions,
		localTypes:   localTypes,
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callFunctionValue(fnValue FunctionValue, args []Value) Value {
	frameDepthBefore := len(vm.frames)

	vm.callFunctionValueWithArgs(fnValue, args)

	for len(vm.frames) > frameDepthBefore {
		if vm.step() {
			LangError(ErrorRuntime, "program halted while running function value")
		}
	}

	if len(vm.stack) == 0 {
		LangError(ErrorInternal, "function %s returned no value", fnValue.Name)
	}

	return vm.pop()
}

func (vm *VM) Run() {
	for {
		if vm.step() {
			return
		}
	}
}

func (vm *VM) callServerMethod(server *NativeServerValue, method string, args []Value) {
	switch method {
	case "getPrettyJSON":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0])
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			LangError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.Routes[path] = string(bytes)

		vm.push(0)
	case "getJSON":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0])
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			LangError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.Routes[path] = string(bytes)

		vm.push(0)
	case "get":
		if len(args) != 2 {
			LangError(ErrorRuntime, "server.get expects 2 arguments")
		}

		path := asString(args[0])
		handler := args[1]

		switch handler.(type) {
		case string:
			server.Routes[path] = handler
		case FunctionValue:
			server.Routes[path] = handler
		default:
			LangError(ErrorType, "server.get expects string or function as second argument")
		}

		vm.push(0)

	case "start":
		if len(args) != 0 {
			LangError(ErrorRuntime, "server.start expects 0 arguments")
		}

		mux := http.NewServeMux()

		for path, handler := range server.Routes {
			routeHandler := handler

			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				switch h := routeHandler.(type) {
				case string:
					writeServerResponse(w, h)

				case FunctionValue:
					reqObj := ObjectValue{
						"path":   r.URL.Path,
						"method": r.Method,
					}

					vm.mu.Lock()
					defer vm.mu.Unlock()

					result := vm.callFunctionValue(h, []Value{reqObj})
					writeServerResponse(w, valueToString(result))

				default:
					LangError(ErrorType, "invalid route handler: %s", typeName(routeHandler))
				}
			})
		}

		addr := ":" + strconv.Itoa(server.Port)

		err := http.ListenAndServe(addr, mux)
		if err != nil {
			LangError(ErrorRuntime, "server failed: %v", err)
		}

		vm.push(0)

	default:
		LangError(ErrorName, "unknown server method: %s", method)
	}
}

func writeServerResponse(w http.ResponseWriter, text string) {
	trimmed := strings.TrimSpace(text)

	isJSON := len(trimmed) > 0 &&
		((trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') ||
			(trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']'))

	if isJSON {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, trimmed)
		return
	}

	fmt.Fprint(w, text)
}

func (vm *VM) callNamespaceMethod(ns NamespaceValue, method string, args []Value) {
	value, exists := ns.Members[method]
	if !exists {
		LangError(ErrorName, "namespace %s has no member: %s", ns.Name, method)
	}

	switch v := value.(type) {
	case FunctionValue:
		result := vm.callFunctionValue(v, args)
		vm.push(result)

	case *FunctionValue:
		result := vm.callFunctionValue(*v, args)
		vm.push(result)

	case Class:
		vm.callClassByName(v.Name, args)

	case *Class:
		vm.callClassByName(v.Name, args)

	default:
		LangError(ErrorType, "namespace member %s is not callable", method)
	}
}

func (vm *VM) callMethod(method string, argCount int) {
	args := vm.popArgs(argCount)

	objectValue := vm.pop()

	if ns, ok := objectValue.(NamespaceValue); ok {
		vm.callNamespaceMethod(ns, method, args)
		return
	}

	if ns, ok := objectValue.(*NamespaceValue); ok {
		vm.callNamespaceMethod(*ns, method, args)
		return
	}

	if plugin, ok := objectValue.(*NativePluginValue); ok {
		vm.callNativePlugin(plugin, method, args)
		return
	}

	if app, ok := objectValue.(*NativeAppValue); ok {
		vm.callNativeAppMethod(app, method, args)
		return
	}

	if std, ok := objectValue.(*StandardModuleValue); ok {
		vm.callStandardModule(std.Name, method, args)
		return
	}

	if server, ok := objectValue.(*NativeServerValue); ok {
		vm.callServerMethod(server, method, args)
		return
	}

	object, ok := objectValue.(ObjectValue)
	if !ok {
		LangError(ErrorType, "expected object, got %s", typeName(objectValue))
	}

	methodValue, exists := object[method]
	if !exists {
		LangError(ErrorName, "object has no method: %s", method)
	}

	fnValue, ok := methodValue.(FunctionValue)
	if !ok {
		LangError(ErrorType, "property %s is not callable", method)
	}

	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		LangError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	expectedArgs := len(fn.Params) - 1

	if expectedArgs != argCount {
		LangError(
			ErrorRuntime,
			"method %s expects %d arguments, got %d",
			method,
			expectedArgs,
			argCount,
		)
	}

	locals := make([]*Cell, fn.LocalCount)
	constants := make([]bool, fn.LocalCount)

	locals[0] = &Cell{Value: object}
	constants[0] = true

	for i, arg := range args {
		locals[i+1] = &Cell{Value: arg}
		constants[i+1] = false
	}

	for i := range locals {
		if locals[i] == nil {
			locals[i] = &Cell{Value: UndefinedValue{}}
		}
	}

	localTypes := make([]TypeHint, fn.LocalCount)

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    constants,
		instructions: fn.Instructions,
		localTypes:   localTypes,
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) runNativeApp(app *NativeAppValue) {
	if len(vm.cliArgs) == 0 {
		fmt.Println("Available commands:")

		for name := range app.Commands {
			fmt.Println("  " + name)
		}

		return
	}

	commandName := vm.cliArgs[0]
	commandArgs := vm.cliArgs[1:]

	fn, exists := app.Commands[commandName]
	if !exists {
		LangError(ErrorRuntime, "unknown command: %s", commandName)
	}

	tinyArgs := &ArrayValue{
		Elements: make([]Value, len(commandArgs)),
	}

	for i, arg := range commandArgs {
		tinyArgs.Elements[i] = arg
	}

	vm.callFunctionValue(fn, []Value{tinyArgs})
}

func (vm *VM) callNativeAppMethod(app *NativeAppValue, method string, args []Value) {
	switch method {
	case "command":
		if len(args) != 2 {
			LangError(ErrorRuntime, "app.command expects 2 arguments")
		}

		name := asString(args[0])

		fn, ok := args[1].(FunctionValue)
		if !ok {
			LangError(ErrorType, "app.command expects function callback")
		}

		app.Commands[name] = fn
		vm.push(app)

	case "run":
		if len(args) != 0 {
			LangError(ErrorRuntime, "app.run expects 0 arguments")
		}

		vm.runNativeApp(app)
		vm.push(0)

	default:
		LangError(ErrorName, "unknown app method: %s", method)
	}
}

func (vm *VM) setIP(value int) {
	if len(vm.frames) == 0 {
		vm.ip = value
		return
	}

	vm.frames[len(vm.frames)-1].ip = value
}

func (vm *VM) callFunction(name string, argCount int) {
	fn, exists := vm.functions[name]
	if !exists {
		LangError(ErrorName, "undefined function: %s", name)
	}

	args := vm.popArgs(argCount)

	if len(args) != len(fn.Params) {
		LangError(
			ErrorRuntime,
			"function %s expects %d arguments, got %d",
			name,
			len(fn.Params),
			len(args),
		)
	}

	locals := make([]*Cell, fn.LocalCount)
	constants := make([]bool, fn.LocalCount)

	for i, arg := range args {
		locals[i] = &Cell{Value: arg}
	}

	// Very important: fill empty local slots.
	for i := range locals {
		if locals[i] == nil {
			locals[i] = &Cell{Value: UndefinedValue{}}
		}
	}

	localTypes := make([]TypeHint, fn.LocalCount)

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    constants,
		instructions: fn.Instructions,
		localTypes:   localTypes,
	}

	if len(args) != len(fn.Params) {
		LangError(ErrorRuntime, "function %s expects %d arguments, got %d", fn.Name, len(fn.Params), len(args))
	}

	for i, arg := range args {
		param := fn.Params[i]

		if !checkTypeHint(arg, param.TypeHint) {
			LangError(
				ErrorType,
				"function %s parameter %s expected %s, got %s",
				fn.Name,
				param.Name,
				param.TypeHint.Name,
				typeName(arg),
			)
		}
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callClass(class Class, argCount int) {
	args := vm.popArgs(argCount)
	vm.callClassWithArgs(class, args)
}

func (vm *VM) currentInstructions() []Instruction {
	if len(vm.frames) == 0 {
		return vm.mainInstructions
	}

	return vm.frames[len(vm.frames)-1].instructions
}

func (vm *VM) currentIP() int {
	if len(vm.frames) == 0 {
		return vm.ip
	}

	return vm.frames[len(vm.frames)-1].ip
}

func (vm *VM) incrementIP() {
	if len(vm.frames) == 0 {
		vm.ip++
		return
	}

	vm.frames[len(vm.frames)-1].ip++
}

func (vm *VM) currentFrame() *Frame {
	if len(vm.frames) == 0 {
		LangError(ErrorInternal, "no current function frame")
	}

	return &vm.frames[len(vm.frames)-1]
}

func (vm *VM) popArgs(count int) []Value {
	if len(vm.stack) < count {
		LangError(
			ErrorInternal,
			"not enough values for args: need=%d have=%d at function=%s ip=%d op=%v value=%#v stack=%#v",
			count,
			len(vm.stack),
			vm.lastFunctionName,
			vm.lastInstructionIndex,
			vm.lastInstruction.Op,
			vm.lastInstruction.Value,
			vm.stack,
		)
	}

	args := make([]Value, count)

	for i := count - 1; i >= 0; i-- {
		args[i] = vm.pop()
	}

	return args
}

func (vm *VM) push(value Value) {
	vm.stack = append(vm.stack, value)
}

func (vm *VM) pop() Value {
	if len(vm.stack) == 0 {
		LangError(
			ErrorInternal,
			"stack underflow at function=%s ip=%d op=%v value=%#v",
			vm.lastFunctionName,
			vm.lastInstructionIndex,
			vm.lastInstruction.Op,
			vm.lastInstruction.Value,
		)
	}

	value := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return value
}
