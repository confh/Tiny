package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type Frame struct {
	function     Function
	ip           int
	locals       map[string]Value
	constants    map[string]bool
	instructions []Instruction

	returnOverride    Value
	hasReturnOverride bool
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	functions        map[string]Function
	classes          map[string]Class

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
	}
}

func (vm *VM) Run() {
	for {
		instructions := vm.currentInstructions()
		ip := vm.currentIP()

		if ip < 0 || ip >= len(instructions) {
			langError(ErrorInternal, "instruction pointer out of range")
		}

		instr := instructions[ip]
		vm.incrementIP()

		switch instr.Op {
		case OP_CONST:
			vm.push(instr.Value)

		case OP_SET_PROPERTY:
			name := instr.Value.(string)

			value := vm.pop()
			objectValue := vm.pop()

			object, ok := objectValue.(ObjectValue)
			if !ok {
				langError(ErrorType, "expected object, got %s", typeName(objectValue))
			}

			object[name] = value

		case OP_LOAD_GLOBAL:
			name := instr.Value.(string)

			value, ok := vm.globals[name]
			if !ok {
				langError(ErrorName, "undefined global variable: %s", name)
			}

			vm.push(value)

		case OP_STORE_GLOBAL:
			info := instr.Value.(VariableInfo)
			value := vm.pop()

			vm.globals[info.Name] = value
			vm.globalConstants[info.Name] = info.Constant

		case OP_LOAD_LOCAL:
			name := instr.Value.(string)

			frame := vm.currentFrame()

			value, ok := frame.locals[name]
			if !ok {
				langError(ErrorName, "undefined local variable: %s", name)
			}

			vm.push(value)

		case OP_STORE_LOCAL:
			info := instr.Value.(VariableInfo)
			value := vm.pop()

			frame := vm.currentFrame()
			frame.locals[info.Name] = value
			frame.constants[info.Name] = info.Constant

		case OP_ASSIGN_GLOBAL:
			name := instr.Value.(string)
			value := vm.pop()

			_, exists := vm.globals[name]
			if !exists {
				langError(ErrorName, "cannot assign to undefined variable: %s", name)
			}

			if vm.globalConstants[name] {
				langError(ErrorConst, "cannot assign to constant: %s", name)
			}

			vm.globals[name] = value

		case OP_ASSIGN_LOCAL:
			name := instr.Value.(string)
			value := vm.pop()

			frame := vm.currentFrame()

			_, exists := frame.locals[name]
			if !exists {
				langError(ErrorName, "cannot assign to undefined local variable: %s", name)
			}

			if frame.constants[name] {
				langError(ErrorConst, "cannot assign to constant: %s", name)
			}

			frame.locals[name] = value

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
			} else {
				langError(ErrorType, "cannot add %s and %s", typeName(left), typeName(right))
			}

		case OP_SUB:
			right := vm.pop()
			left := vm.pop()

			if isNumber(left) && isNumber(right) {
				if _, ok := left.(float64); ok {
					vm.push(asFloat(left) - asFloat(right))
				} else if _, ok := right.(float64); ok {
					vm.push(asFloat(left) - asFloat(right))
				} else {
					vm.push(left.(int) - right.(int))
				}
			} else {
				langError(ErrorType, "cannot subtract %s and %s", typeName(left), typeName(right))
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
				langError(ErrorType, "cannot multiply %s and %s", typeName(left), typeName(right))
			}

		case OP_DIV:
			right := vm.pop()
			left := vm.pop()

			if !isNumber(left) || !isNumber(right) {
				langError(ErrorType, "cannot divide %s and %s", typeName(left), typeName(right))
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
				langError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
			}

			vm.push(asFloat(left) < asFloat(right))

		case OP_GT:
			right := vm.pop()
			left := vm.pop()

			if !isNumber(left) || !isNumber(right) {
				langError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
			}

			vm.push(asFloat(left) > asFloat(right))

		case OP_LTE:
			right := vm.pop()
			left := vm.pop()

			if !isNumber(left) || !isNumber(right) {
				langError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
			}

			vm.push(asFloat(left) <= asFloat(right))

		case OP_GTE:
			right := vm.pop()
			left := vm.pop()

			if !isNumber(left) || !isNumber(right) {
				langError(ErrorType, "cannot compare %s and %s", typeName(left), typeName(right))
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
			vm.callFunction(info.Name, info.ArgCount)

		case OP_BUILTIN_CALL:
			info := instr.Value.(BuiltinCallInfo)
			vm.callBuiltin(info.Object, info.Method, info.ArgCount)

		case OP_ARRAY:
			info := instr.Value.(ArrayInfo)

			elements := make([]Value, info.Count)

			for i := info.Count - 1; i >= 0; i-- {
				elements[i] = vm.pop()
			}

			vm.push(ArrayValue(elements))

		case OP_INDEX:
			index := asInt(vm.pop())
			arrayValue := vm.pop()

			array, ok := arrayValue.(ArrayValue)
			if !ok {
				langError(ErrorSyntax, "expected array, got %T", arrayValue)
			}

			if index < 0 || index >= len(array) {
				langError(ErrorInternal, "array index out of range: %d", index)
			}

			vm.push(array[index])

		case OP_RETURN:
			returnValue := vm.pop()

			if len(vm.frames) == 0 {
				langError(ErrorRuntime, "return used outside of function")
			}

			frame := vm.frames[len(vm.frames)-1]
			vm.frames = vm.frames[:len(vm.frames)-1]

			if frame.hasReturnOverride {
				vm.push(frame.returnOverride)
			} else {
				vm.push(returnValue)
			}

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

		case OP_GET_PROPERTY:
			name := instr.Value.(string)
			objectValue := vm.pop()

			object, ok := objectValue.(ObjectValue)
			if !ok {
				langError(ErrorType, "expected object, got %s", typeName(objectValue))
			}

			value, exists := object[name]
			if !exists {
				langError(ErrorName, "object has no property: %s", name)
			}

			vm.push(value)

		case OP_HALT:
			return

		default:
			langError(ErrorInternal, "unknown opcode: %s", instr.Op)
		}
	}
}

func (vm *VM) callServerMethod(server *NativeServerValue, method string, args []Value) {
	switch method {
	case "getPrettyJSON":
		if len(args) != 2 {
			langError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0])
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.Routes[path] = string(bytes)

		vm.push(0)
	case "getJSON":
		if len(args) != 2 {
			langError(ErrorRuntime, "server.getJSON expects 2 arguments")
		}

		path := asString(args[0])
		jsonValue := valueToJSONCompatible(args[1])

		bytes, err := json.Marshal(jsonValue)
		if err != nil {
			langError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}

		server.Routes[path] = string(bytes)

		vm.push(0)
	case "get":
		if len(args) != 2 {
			langError(ErrorRuntime, "server.get expects 2 arguments")
		}

		path := asString(args[0])
		response := valueToString(args[1])

		server.Routes[path] = response

		vm.push(0)

	case "start":
		if len(args) != 0 {
			langError(ErrorRuntime, "server.start expects 0 arguments")
		}

		mux := http.NewServeMux()

		for path, response := range server.Routes {
			text := response

			mux.HandleFunc(path, func(w http.ResponseWriter, r *http.Request) {
				trimmed := strings.TrimSpace(text)
				isJSON := (len(trimmed) > 0 && ((trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') || (trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']')))

				if isJSON {
					w.Header().Set("Content-Type", "application/json")
					fmt.Fprint(w, trimmed)
				} else {
					fmt.Fprint(w, text)
				}
			})
		}

		addr := ":" + strconv.Itoa(server.Port)

		err := http.ListenAndServe(addr, mux)
		if err != nil {
			langError(ErrorRuntime, "server failed: %v", err)
		}

		vm.push(0)

	default:
		langError(ErrorName, "unknown server method: %s", method)
	}
}

func (vm *VM) callMethod(method string, argCount int) {
	args := vm.popArgs(argCount)

	objectValue := vm.pop()

	if server, ok := objectValue.(*NativeServerValue); ok {
		vm.callServerMethod(server, method, args)
		return
	}

	object, ok := objectValue.(ObjectValue)
	if !ok {
		langError(ErrorType, "expected object, got %s", typeName(objectValue))
	}

	methodValue, exists := object[method]
	if !exists {
		langError(ErrorName, "object has no method: %s", method)
	}

	fnValue, ok := methodValue.(FunctionValue)
	if !ok {
		langError(ErrorType, "property %s is not callable", method)
	}

	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		langError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	expectedArgs := len(fn.Params)

	if expectedArgs != argCount {
		langError(
			ErrorRuntime,
			"method %s expects %d arguments, got %d",
			method,
			expectedArgs,
			argCount,
		)
	}

	locals := map[string]Value{}
	constants := map[string]bool{}

	locals["this"] = object
	constants["this"] = true

	for i, arg := range args {
		locals[fn.Params[i]] = arg
		constants[fn.Params[i]] = false
	}

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    map[string]bool{},
		instructions: fn.Instructions,
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) setIP(value int) {
	if len(vm.frames) == 0 {
		vm.ip = value
		return
	}

	vm.frames[len(vm.frames)-1].ip = value
}

func (vm *VM) callFunction(name string, argCount int) {
	fn, ok := vm.functions[name]
	if !ok {
		if class, exists := vm.classes[name]; exists {
			vm.callClass(class, argCount)
			return
		}

		langError(ErrorName, "undefined function or class: %s", name)
	}

	if len(fn.Params) != argCount {
		langError(ErrorRuntime, "function %s expects %d arguments, got %d",
			name,
			len(fn.Params),
			argCount)
	}

	locals := map[string]Value{}

	for i := argCount - 1; i >= 0; i-- {
		paramName := fn.Params[i]
		locals[paramName] = vm.pop()
	}

	frame := Frame{
		function:     fn,
		ip:           0,
		locals:       locals,
		constants:    map[string]bool{},
		instructions: fn.Instructions,
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callClass(class Class, argCount int) {
	args := vm.popArgs(argCount)

	instance := ObjectValue{}

	for methodName, functionName := range class.Methods {
		instance[methodName] = FunctionValue{Name: functionName}
	}

	initName, hasInit := class.Methods["init"]
	if !hasInit {
		if argCount != 0 {
			langError(ErrorRuntime, "class %s expects 0 arguments", class.Name)
		}

		vm.push(instance)
		return
	}

	fn, ok := vm.functions[initName]
	if !ok {
		langError(ErrorName, "missing init function for class: %s", class.Name)
	}

	if len(fn.Params) != argCount {
		langError(
			ErrorRuntime,
			"class %s constructor expects %d arguments, got %d",
			class.Name,
			len(fn.Params),
			argCount,
		)
	}

	locals := map[string]Value{}
	constants := map[string]bool{}

	locals["this"] = instance
	constants["this"] = true

	for i, arg := range args {
		locals[fn.Params[i]] = arg
		constants[fn.Params[i]] = false
	}

	frame := Frame{
		function:          fn,
		ip:                0,
		locals:            locals,
		constants:         constants,
		instructions:      fn.Instructions,
		returnOverride:    instance,
		hasReturnOverride: true,
	}

	vm.frames = append(vm.frames, frame)
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
		langError(ErrorInternal, "no current function frame")
	}

	return &vm.frames[len(vm.frames)-1]
}

func (vm *VM) popArgs(argCount int) []Value {
	args := make([]Value, argCount)

	for i := argCount - 1; i >= 0; i-- {
		args[i] = vm.pop()
	}

	return args
}

func (vm *VM) push(value Value) {
	vm.stack = append(vm.stack, value)
}

func (vm *VM) pop() Value {
	if len(vm.stack) == 0 {
		langError(ErrorInternal, "stack underflow")
	}

	value := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]

	return value
}
