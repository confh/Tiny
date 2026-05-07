package main

import (
	"time"
)

type Frame struct {
	function     Function
	ip           int
	locals       map[string]Value
	constants    map[string]bool
	instructions []Instruction
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	functions        map[string]Function

	ip int

	stack           []Value
	globals         map[string]Value
	globalConstants map[string]bool

	frames []Frame
}

func NewVM(mainInstructions []Instruction, functions map[string]Function) *VM {
	return &VM{
		start:            time.Now().UnixMilli(),
		mainInstructions: mainInstructions,
		functions:        functions,
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
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left + right)

		case OP_SUB:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left - right)

		case OP_MUL:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left * right)

		case OP_DIV:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left / right)

		case OP_EQ:
			right := vm.pop()
			left := vm.pop()
			vm.push(valuesEqual(left, right))

		case OP_NEQ:
			right := vm.pop()
			left := vm.pop()
			vm.push(!valuesEqual(left, right))

		case OP_LT:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left < right)

		case OP_GT:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left > right)

		case OP_LTE:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left <= right)

		case OP_GTE:
			right := asInt(vm.pop())
			left := asInt(vm.pop())
			vm.push(left >= right)

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
				langError(ErrorInternal, "return used outside of function")
			}

			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.push(returnValue)

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
		langError(ErrorName, "undefined function: %s", name)
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
