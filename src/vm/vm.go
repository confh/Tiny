package vm

import (
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
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
	methodClass  string

	returnOverride    Value
	hasReturnOverride bool
	hasEscapedLocals  bool
}

type VM struct {
	start            int64
	mainInstructions []Instruction
	functions        map[string]Function
	classes          map[string]Class
	framePool        []Frame
	functionList     []Function

	top int

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

func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}

	return string(buf[i:])
}

func bigIntToString(n int64) string {
	if n == 0 {
		return "0"
	}

	var buf [20]byte
	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + (n % 10))
		n /= 10
	}

	return string(buf[i:])
}

func FloatToString(val float64) string {
	var sb strings.Builder

	if val < 0 {
		sb.WriteByte('-')
		val = -val
	}

	// 2. Extract integer part
	intPart := int64(val)
	// Extract remaining fractional part
	fracPart := val - float64(intPart)

	// 3. Convert integer part to string
	sb.WriteString(bigIntToString(intPart))

	// 4. If precision is 0, we are done
	// if precision <= 0 {
	// 	return sb.String()
	// }

	sb.WriteByte('.')

	// 5. Convert fractional part
	for i := 0; i < 6; i++ {
		fracPart *= 10
		digit := int64(fracPart)
		sb.WriteByte(byte('0' + digit))
		fracPart -= float64(digit)
	}

	return sb.String()
}

func isClass(value ObjectValue) bool {
	_, exists := value["__class"]

	return exists
}

func NewVM(mainInstructions []Instruction, functions map[string]Function, classes map[string]Class) *VM {
	mainInstructions, functions, functionList := normalizeFunctionIDs(mainInstructions, functions)

	return &VM{
		start:            time.Now().UnixMilli(),
		mainInstructions: mainInstructions,
		functions:        functions,
		functionList:     functionList,
		classes:          classes,
		globals:          map[string]Value{},
		globalConstants:  map[string]bool{},
		mu:               sync.Mutex{},
		cliArgs:          []string{},
		globalTypes:      map[string]TypeHint{},
		top:              0,
		stack:            make([]Value, 1024),
		framePool:        make([]Frame, 0, 1024),
	}
}

func normalizeFunctionIDs(
	mainInstructions []Instruction,
	functions map[string]Function,
) ([]Instruction, map[string]Function, []Function) {
	names := make([]string, 0, len(functions))

	for name := range functions {
		names = append(names, name)
	}

	sort.Strings(names)

	ids := map[string]int{}
	functionList := make([]Function, len(names))

	for id, name := range names {
		ids[name] = id

		fn := functions[name]
		fn.ID = id
		functions[name] = fn
	}

	remapDirectCallIDs(mainInstructions, ids)

	for name, fn := range functions {
		remapDirectCallIDs(fn.Instructions, ids)

		id := ids[name]
		fn.ID = id
		functions[name] = fn
		functionList[id] = fn
	}

	return mainInstructions, functions, functionList
}

func remapDirectCallIDs(instructions []Instruction, ids map[string]int) {
	for i := range instructions {
		if instructions[i].Op != OP_CALL_DIRECT {
			continue
		}

		info, ok := instructions[i].Value.(DirectCallInfo)
		if !ok {
			continue
		}

		id, exists := ids[info.Name]
		if !exists {
			continue
		}

		info.ID = id
		instructions[i].Value = info
	}
}

func methodOwnerClass(functionName string) string {
	dot := strings.LastIndex(functionName, ".")
	if dot == -1 {
		return ""
	}

	return functionName[:dot]
}

func (vm *VM) currentMethodClass() string {
	if len(vm.frames) == 0 {
		return ""
	}

	frame := vm.frames[len(vm.frames)-1]
	return frame.methodClass
}

func (vm *VM) SetCLIArgs(args []string) {
	vm.cliArgs = args
}

func (vm *VM) getFrame(fn Function) Frame {
	var frame Frame

	if len(vm.framePool) > 0 {
		last := len(vm.framePool) - 1
		frame = vm.framePool[last]
		vm.framePool = vm.framePool[:last]
	}

	if cap(frame.locals) < fn.LocalCount {
		frame.locals = make([]*Cell, fn.LocalCount)
	} else {
		frame.locals = frame.locals[:fn.LocalCount]
	}

	if cap(frame.constants) < fn.LocalCount {
		frame.constants = make([]bool, fn.LocalCount)
	} else {
		frame.constants = frame.constants[:fn.LocalCount]
	}

	if cap(frame.localTypes) < fn.LocalCount {
		frame.localTypes = make([]TypeHint, fn.LocalCount)
	} else {
		frame.localTypes = frame.localTypes[:fn.LocalCount]
	}

	for i := 0; i < fn.LocalCount; i++ {
		if frame.locals[i] == nil {
			frame.locals[i] = &Cell{}
		}

		setCellValue(frame.locals[i], UndefinedValue{})
		frame.constants[i] = false
		frame.localTypes[i] = TypeHint{}
	}

	frame.function = fn
	frame.ip = 0
	frame.instructions = fn.Instructions
	frame.methodClass = ""
	frame.returnOverride = nil
	frame.hasReturnOverride = false
	frame.hasEscapedLocals = false

	return frame
}

func (vm *VM) releaseFrame(frame Frame) {
	if frame.hasEscapedLocals {
		return
	}

	// Keep pool from growing forever.
	if len(vm.framePool) >= 1024 {
		return
	}

	// Clear references so big objects can be GC'd.
	for i := range frame.locals {
		if frame.locals[i] != nil {
			setCellValue(frame.locals[i], nil)
		}
	}

	frame.function = Function{}
	frame.instructions = nil
	frame.ip = 0

	vm.framePool = append(vm.framePool, frame)
}

func (vm *VM) CloneForTask() *VM {
	return &VM{
		mainInstructions: vm.mainInstructions,
		functions:        vm.functions,
		classes:          vm.classes,
		functionList:     vm.functionList,

		stack:       make([]Value, 256),
		framePool:   make([]Frame, 0, 256),
		frames:      []Frame{},
		tryHandlers: []TryHandler{},

		globals:         vm.globals,
		globalConstants: vm.globalConstants,

		cliArgs: vm.cliArgs,
	}
}

func cloneValue(value Value) Value {
	switch v := value.(type) {
	case ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range v {
			copyObj[key] = cloneValue(val)
		}

		return copyObj

	case *ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range *v {
			copyObj[key] = cloneValue(val)
		}

		return copyObj

	case *ArrayValue:
		copyArr := &ArrayValue{
			Elements: make([]Value, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return copyArr

	case ArrayValue:
		copyArr := ArrayValue{
			Elements: make([]Value, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return copyArr

	case *BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return &BufferValue{
			Bytes: bytes,
		}

	case BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return BufferValue{
			Bytes: bytes,
		}

	default:
		return value
	}
}

func cellValue(cell *Cell) Value {
	if cell.IsInt {
		return cell.Int
	}

	return cell.Value
}

func setCellValue(cell *Cell, value Value) {
	if intValue, ok := value.(int); ok {
		cell.Int = intValue
		cell.Value = nil
		cell.IsInt = true
		return
	}

	cell.Value = value
	cell.Int = 0
	cell.IsInt = false
}

func frameLocalValue(frame *Frame, slot int, op string) Value {
	if slot < 0 || slot >= len(frame.locals) {
		LangError(ErrorInternal, "local slot out of range in %s", op)
	}

	cell := frame.locals[slot]
	if cell == nil {
		LangError(ErrorInternal, "local cell is nil in %s", op)
	}

	return cellValue(cell)
}

func propertyValue(vm *VM, objectValue Value, name string) Value {
	if object, ok := objectValue.(ObjectValue); ok {
		if !vm.canAccessField(object, name) {
			LangError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := object[name]
		if !exists {
			LangError(ErrorName, "object has no property: %s", name)
		}

		return value
	}

	if ns, ok := objectValue.(NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			LangError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	if ns, ok := objectValue.(*NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			LangError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	LangError(ErrorType, "expected object, got %s", TypeName(objectValue))
	return nil
}

func resolveNamespaceValue(vm *VM, value Value) Value {
	if ref, ok := value.(NamespaceMemberRef); ok {
		actual, exists := vm.globals[ref.GlobalName]
		if !exists {
			LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
		}
		return actual
	}

	if ref, ok := value.(*NamespaceMemberRef); ok {
		actual, exists := vm.globals[ref.GlobalName]
		if !exists {
			LangError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
		}
		return actual
	}

	return value
}

func multiplyByInt(value Value, factor int) Value {
	switch v := value.(type) {
	case int:
		return v * factor
	case int64:
		return v * int64(factor)
	case float64:
		return v * float64(factor)
	case float32:
		return v * float32(factor)
	default:
		LangError(ErrorType, "cannot multiply %s and number", TypeName(value))
		return nil
	}
}

func addValues(left Value, right Value) Value {
	switch l := left.(type) {
	case int:
		switch r := right.(type) {
		case int:
			return l + r
		case float64:
			return float64(l) + r
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case float64:
		switch r := right.(type) {
		case int:
			return l + float64(r)
		case float64:
			return l + r
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case string:
		switch r := right.(type) {
		case string:
			return l + r
		case float64:
			return l + FloatToString(r)
		case int:
			return l + intToString(r)
		case int64:
			return l + bigIntToString(r)
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	default:
		LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
	}

	return nil
}

func (vm *VM) canAccessField(object ObjectValue, field string) bool {
	className, isClass := object["__class"]
	if !isClass {
		return true
	}
	privateFields := object["__privateFields"].(map[string]bool)
	if _, fieldIsPrivate := privateFields[field]; fieldIsPrivate {
		return vm.currentMethodClass() == className
	}

	return true
}

func (vm *VM) canAccessMethod(object ObjectValue, method string) bool {
	className, isClass := object["__class"]
	if !isClass {
		return true
	}
	privateMethods := object["__privateMethods"].(map[string]bool)
	if _, methodIsPrivate := privateMethods[method]; methodIsPrivate {
		return vm.currentMethodClass() == className
	}

	return true
}

func (vm *VM) callClassWithArgs(class Class, args []Value) {
	object := ObjectValue{
		"__class":          class.Name,
		"__constFields":    map[string]bool{},
		"__privateFields":  map[string]bool{},
		"__privateMethods": map[string]bool{},
	}

	constFields, _ := object["__constFields"].(map[string]bool)
	privateFields, _ := object["__privateFields"].(map[string]bool)
	privateMethods, _ := object["__privateMethods"].(map[string]bool)

	for methodName, functionName := range class.Methods {
		object[methodName] = FunctionValue{
			Name: functionName,
		}

		if class.PrivateMethods[methodName] {
			privateMethods[methodName] = true
		}
	}

	for _, field := range class.Fields {
		object[field.Name] = cloneValue(field.Value)
		if field.Constant {
			constFields[field.Name] = true
		}

		if field.Private {
			privateFields[field.Name] = true
		}
	}

	if initName, exists := class.Methods["init"]; exists {
		fn, ok := vm.functions[initName]
		if !ok {
			LangError(ErrorName, "undefined init function: %s", initName)
		}

		expected := len(fn.Params) - 1

		if fn.HasDefaults {
			args = vm.applyDefaultArgs(fn, args, 1, "class "+class.Name+" constructor")
		} else if len(args) != expected {
			vm.runtimeError(
				ErrorRuntime,
				"class %s constructor expects %d arguments, got %d",
				class.Name,
				expected,
				len(args),
			)
		}

		frameDepthBefore := len(vm.frames)

		frame := vm.getFrame(fn)

		setCellValue(frame.locals[0], object)
		frame.constants[0] = true

		for i, arg := range args {
			setCellValue(frame.locals[i+1], arg)
			frame.constants[i+1] = false
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

func (vm *VM) stackTrace() string {
	if len(vm.frames) == 0 {
		return "  at <main>"
	}

	lines := []string{}

	for i := len(vm.frames) - 1; i >= 0; i-- {
		frame := vm.frames[i]

		name := frame.function.Name
		if name == "" {
			name = "<anonymous>"
		}

		location := ""

		ip := frame.ip - 1
		if ip >= 0 && ip < len(frame.instructions) {
			instr := frame.instructions[ip]

			if instr.File != "" && instr.Line > 0 {
				location = fmt.Sprintf(" (%s:%d", instr.File, instr.Line)

				if instr.Column > 0 {
					location += fmt.Sprintf(":%d", instr.Column)
				}

				location += ")"
			}
		}

		lines = append(lines, "  at "+name+location)
	}

	return strings.Join(lines, "\n")
}

func (vm *VM) fatalError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)

	trace := vm.stackTrace()

	panic(LangErrorType{
		Kind:    kind,
		Message: message + "\n\nStack trace:\n" + trace,
	})
}

func (vm *VM) runtimeError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)

	errObj := ObjectValue{
		"kind":    string(kind),
		"message": message,
	}

	vm.throwValue(errObj)
}

func (vm *VM) isInstanceOf(value Value, className string) bool {
	object, ok := value.(ObjectValue)
	if !ok {
		return false
	}

	return vm.objectIsOrEmbedsClass(object, className)
}

func (vm *VM) objectIsOrEmbedsClass(object ObjectValue, className string) bool {
	currentClassValue, ok := object["__class"]
	if ok {
		currentClassName, ok := currentClassValue.(string)
		if ok && currentClassName == className {
			return true
		}

		if ok {
			class, exists := vm.classes[currentClassName]
			if exists {
				for _, fieldName := range class.Embeds {
					fieldValue, exists := object[fieldName]
					if !exists {
						continue
					}

					embeddedObject, ok := fieldValue.(ObjectValue)
					if !ok {
						continue
					}

					if vm.objectIsOrEmbedsClass(embeddedObject, className) {
						return true
					}
				}
			}
		}
	}

	return false
}

func (vm *VM) callFunctionDirectFromStack(fn Function, argCount int, callableName string) {
	expected := len(fn.Params)
	isVariadic := expected > 0 && fn.Params[expected-1].Variadic

	// Defaults are easier through the old path, except variadic should not use defaults.
	if fn.HasDefaults && !isVariadic {
		args := vm.popArgs(argCount)
		args = vm.applyDefaultArgs(fn, args, 0, callableName)
		vm.callFunctionDirect(fn, args)
		return
	}

	if isVariadic {
		minArgs := expected - 1

		if argCount < minArgs {
			vm.runtimeError(
				ErrorRuntime,
				"%s expects at least %d arguments, got %d",
				callableName,
				minArgs,
				argCount,
			)
			return
		}
	} else if argCount != expected {
		vm.runtimeError(
			ErrorRuntime,
			"%s expects %d arguments, got %d",
			callableName,
			expected,
			argCount,
		)
		return
	}

	if vm.top < argCount {
		vm.handleUnderflow()
		return
	}

	frame := vm.getFrame(fn)

	start := vm.top - argCount

	if isVariadic {
		fixedCount := expected - 1

		// Fixed params before ...args
		for i := 0; i < fixedCount; i++ {
			arg := vm.stack[start+i]
			param := fn.Params[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() && !CheckTypeHint(arg, param.TypeHint) {
				LangError(
					ErrorType,
					"function %s parameter %s expected %s, got %s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
				)
			}

			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
			frame.localTypes[i] = param.TypeHint

			vm.stack[start+i] = nil
		}

		// Rest param gets remaining args as array
		restParam := fn.Params[fixedCount]
		rest := &ArrayValue{
			Elements: []Value{},
		}

		for i := fixedCount; i < argCount; i++ {
			arg := vm.stack[start+i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() && !CheckTypeHint(arg, restParam.TypeHint) {
				LangError(
					ErrorType,
					"function %s rest parameter %s expected %s, got %s",
					fn.Name,
					restParam.Name,
					restParam.TypeHint.String(),
					TypeName(arg),
				)
			}

			rest.Elements = append(rest.Elements, arg)
			vm.stack[start+i] = nil
		}

		setCellValue(frame.locals[fixedCount], rest)
		frame.constants[fixedCount] = false
		frame.localTypes[fixedCount] = TypeHint{Name: "array"}

		vm.top = start
		vm.frames = append(vm.frames, frame)
		return
	}

	if fn.HasTypeHints {
		for i := 0; i < argCount; i++ {
			arg := vm.stack[start+i]
			param := fn.Params[i]

			if !param.TypeHint.IsEmpty() && !CheckTypeHint(arg, param.TypeHint) {
				LangError(
					ErrorType,
					"function %s parameter %s expected %s, got %s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
				)
			}

			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
			frame.localTypes[i] = param.TypeHint

			vm.stack[start+i] = nil
		}
	} else {
		for i := 0; i < argCount; i++ {
			setCellValue(frame.locals[i], vm.stack[start+i])
			frame.constants[i] = false

			vm.stack[start+i] = nil
		}
	}

	vm.top = start
	vm.frames = append(vm.frames, frame)
}

func isNullish(value Value) bool {
	switch value.(type) {
	case NullValue, UndefinedValue:
		return true
	default:
		return false
	}
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
	case OP_JUMP_LOCAL_GE_LOCAL:
		info := instr.Value.(JumpLocalGELocalInfo)

		frame := &vm.frames[len(vm.frames)-1]

		leftCell := frame.locals[info.LeftSlot]
		rightCell := frame.locals[info.RightSlot]

		if leftCell == nil || rightCell == nil {
			LangError(ErrorInternal, "nil local in OP_JUMP_LOCAL_GE_LOCAL")
		}

		if leftCell.IsInt && rightCell.IsInt {
			if leftCell.Int >= rightCell.Int {
				frame.ip = info.Target
			}
			break
		}

		left := cellValue(leftCell)
		right := cellValue(rightCell)

		shouldJump := false

		switch l := left.(type) {
		case int:
			switch r := right.(type) {
			case int:
				shouldJump = l >= r
			case int64:
				shouldJump = int64(l) >= r
			case float64:
				shouldJump = float64(l) >= r
			default:
				LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		case int64:
			switch r := right.(type) {
			case int:
				shouldJump = l >= int64(r)
			case int64:
				shouldJump = l >= r
			case float64:
				shouldJump = float64(l) >= r
			default:
				LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		case float64:
			shouldJump = l >= asFloat64(right)

		default:
			LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
		info := instr.Value.(JumpModLocalLocalNotZeroInfo)

		frame := &vm.frames[len(vm.frames)-1]

		leftCell := frame.locals[info.LeftSlot]
		rightCell := frame.locals[info.RightSlot]

		if leftCell == nil || rightCell == nil {
			LangError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO")
		}

		if leftCell.IsInt && rightCell.IsInt {
			if rightCell.Int == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}
			if leftCell.Int%rightCell.Int != 0 {
				frame.ip = info.Target
			}
			break
		}

		left := cellValue(leftCell)
		right := cellValue(rightCell)

		shouldJump := false

		switch l := left.(type) {
		case int:
			r := asInt(right)
			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = l%r != 0

		case int64:
			r := int64(asInt(right))
			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = l%r != 0

		case float64:
			r := asFloat64(right)
			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = math.Mod(l, r) != 0

		default:
			LangError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
		info := instr.Value.(JumpModLocalConstNotZeroInfo)

		frame := &vm.frames[len(vm.frames)-1]

		if info.LeftSlot < 0 || info.LeftSlot >= len(frame.locals) {
			LangError(ErrorInternal, "local slot out of range in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
		}
		if info.Right == 0 {
			LangError(ErrorRuntime, "cannot modulo by zero")
		}

		leftCell := frame.locals[info.LeftSlot]
		if leftCell == nil {
			LangError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
		}

		shouldJump := false

		if leftCell.IsInt {
			shouldJump = leftCell.Int%info.Right != 0
		} else {
			left := cellValue(leftCell)
			switch l := left.(type) {
			case int:
				shouldJump = l%info.Right != 0
			case int64:
				shouldJump = l%int64(info.Right) != 0
			case float64:
				shouldJump = math.Mod(l, float64(info.Right)) != 0
			case float32:
				shouldJump = math.Mod(float64(l), float64(info.Right)) != 0
			default:
				LangError(ErrorType, "cannot modulo %s and number", TypeName(left))
			}
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_ADD_ASSIGN_LOCAL:
		info := instr.Value.(AssignLocalInfo)

		frame := &vm.frames[len(vm.frames)-1]

		if info.TargetSlot < 0 || info.TargetSlot >= len(frame.locals) {
			LangError(ErrorInternal, "target local slot out of range in OP_ADD_ASSIGN_LOCAL")
		}

		if info.SourceSlot < 0 || info.SourceSlot >= len(frame.locals) {
			LangError(ErrorInternal, "source local slot out of range in OP_ADD_ASSIGN_LOCAL")
		}

		targetCell := frame.locals[info.TargetSlot]
		sourceCell := frame.locals[info.SourceSlot]

		if targetCell == nil || sourceCell == nil {
			LangError(ErrorInternal, "nil local cell in OP_ADD_ASSIGN_LOCAL")
		}

		if frame.constants[info.TargetSlot] {
			LangError(ErrorConst, "cannot assign to constant local")
		}

		if targetCell.IsInt && sourceCell.IsInt {
			targetCell.Int += sourceCell.Int
			break
		}

		switch target := cellValue(targetCell).(type) {
		case int:
			switch source := cellValue(sourceCell).(type) {
			case int:
				targetCell.Int = target + source
				targetCell.IsInt = true
			case int64:
				setCellValue(targetCell, int64(target)+source)
			case float64:
				setCellValue(targetCell, float64(target)+source)
			case float32:
				setCellValue(targetCell, float32(target)+source)
			default:
				LangError(ErrorType, "cannot add %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case int64:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target+int64(source))
			case int64:
				setCellValue(targetCell, target+source)
			case float64:
				setCellValue(targetCell, float64(target)+source)
			case float32:
				setCellValue(targetCell, float32(target)+source)
			default:
				LangError(ErrorType, "cannot add %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case float64:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target+float64(source))
			case int64:
				setCellValue(targetCell, target+float64(source))
			case float64:
				setCellValue(targetCell, target+source)
			case float32:
				setCellValue(targetCell, target+float64(source))
			default:
				LangError(ErrorType, "cannot add %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case float32:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target+float32(source))
			case int64:
				setCellValue(targetCell, target+float32(source))
			case float64:
				setCellValue(targetCell, float64(target)+source)
			case float32:
				setCellValue(targetCell, target+source)
			default:
				LangError(ErrorType, "cannot add %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case string:
			setCellValue(targetCell, target+valueToString(cellValue(sourceCell)))

		default:
			LangError(ErrorType, "cannot add to %s", TypeName(cellValue(targetCell)))
		}

	case OP_SUB_ASSIGN_LOCAL:
		info := instr.Value.(AssignLocalInfo)

		frame := &vm.frames[len(vm.frames)-1]

		if info.TargetSlot < 0 || info.TargetSlot >= len(frame.locals) {
			LangError(ErrorInternal, "target local slot out of range in OP_SUB_ASSIGN_LOCAL")
		}

		if info.SourceSlot < 0 || info.SourceSlot >= len(frame.locals) {
			LangError(ErrorInternal, "source local slot out of range in OP_SUB_ASSIGN_LOCAL")
		}

		targetCell := frame.locals[info.TargetSlot]
		sourceCell := frame.locals[info.SourceSlot]

		if targetCell == nil || sourceCell == nil {
			LangError(ErrorInternal, "nil local cell in OP_SUB_ASSIGN_LOCAL")
		}

		if frame.constants[info.TargetSlot] {
			LangError(ErrorConst, "cannot assign to constant local")
		}

		if targetCell.IsInt && sourceCell.IsInt {
			targetCell.Int -= sourceCell.Int
			break
		}

		switch target := cellValue(targetCell).(type) {
		case int:
			switch source := cellValue(sourceCell).(type) {
			case int:
				targetCell.Int = target - source
				targetCell.IsInt = true
			case int64:
				setCellValue(targetCell, int64(target)-source)
			case float64:
				setCellValue(targetCell, float64(target)-source)
			case float32:
				setCellValue(targetCell, float32(target)-source)
			default:
				LangError(ErrorType, "cannot subtract %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case int64:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target-int64(source))
			case int64:
				setCellValue(targetCell, target-source)
			case float64:
				setCellValue(targetCell, float64(target)-source)
			case float32:
				setCellValue(targetCell, float32(target)-source)
			default:
				LangError(ErrorType, "cannot subtract %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case float64:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target-float64(source))
			case int64:
				setCellValue(targetCell, target-float64(source))
			case float64:
				setCellValue(targetCell, target-source)
			case float32:
				setCellValue(targetCell, target-float64(source))
			default:
				LangError(ErrorType, "cannot subtract %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		case float32:
			switch source := cellValue(sourceCell).(type) {
			case int:
				setCellValue(targetCell, target-float32(source))
			case int64:
				setCellValue(targetCell, target-float32(source))
			case float64:
				setCellValue(targetCell, float64(target)-source)
			case float32:
				setCellValue(targetCell, target-source)
			default:
				LangError(ErrorType, "cannot subtract %s and %s", TypeName(cellValue(targetCell)), TypeName(cellValue(sourceCell)))
			}

		default:
			LangError(ErrorType, "cannot subtract to %s", TypeName(cellValue(targetCell)))
		}

	case OP_JUMP_LOCAL_GE_CONST:
		info := instr.Value.(JumpLocalGEConstInfo)

		frame := &vm.frames[len(vm.frames)-1]

		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			LangError(ErrorInternal, "local slot out of range in OP_JUMP_LOCAL_GE_CONST")
		}

		cell := frame.locals[info.Slot]
		if cell == nil {
			LangError(ErrorInternal, "local cell is nil in OP_JUMP_LOCAL_GE_CONST")
		}

		if cell.IsInt {
			if cell.Int >= info.Value {
				if len(vm.frames) == 0 {
					vm.ip = info.Target
				} else {
					vm.frames[len(vm.frames)-1].ip = info.Target
				}
			}
			break
		}

		shouldJump := false

		switch v := cellValue(cell).(type) {
		case int:
			shouldJump = v >= info.Value

		case int64:
			shouldJump = v >= int64(info.Value)

		case float64:
			shouldJump = v >= float64(info.Value)

		case float32:
			shouldJump = v >= float32(info.Value)

		default:
			LangError(ErrorType, "cannot compare %s and number", TypeName(cellValue(cell)))
		}

		if shouldJump {
			if len(vm.frames) == 0 {
				vm.ip = info.Target
			} else {
				vm.frames[len(vm.frames)-1].ip = info.Target
			}
		}

	case OP_STRING_JOIN:
		count := instr.Value.(int)

		values := vm.popArgs(count)

		var builder strings.Builder

		for _, value := range values {
			builder.WriteString(valueToString(value))
		}

		vm.push(builder.String())

	case OP_CALL_DIRECT:
		info := instr.Value.(DirectCallInfo)

		var fn Function
		var ok bool

		if info.ID >= 0 && info.ID < len(vm.functionList) {
			fn = vm.functionList[info.ID]

			if fn.Name != info.Name {
				fn, ok = vm.functions[info.Name]
				if !ok {
					vm.runtimeError(ErrorName, "undefined function: %s", info.Name)
					return false
				}
			}
		} else {
			fn, ok = vm.functions[info.Name]
			if !ok {
				vm.runtimeError(ErrorName, "invalid function id for %s", info.Name)
				return false
			}
		}

		vm.callFunctionDirectFromStack(fn, info.ArgCount, "function "+info.Name)

	case OP_OBJECT_IN:
		keyValue := vm.pop()
		objectValue := asObject(vm.pop(), vm)

		found := false
		_, found = objectValue[keyValue]

		vm.push(found)

	case OP_INSTANCEOF:
		classValue := vm.pop()
		objectValue := vm.pop()

		var className string

		switch c := classValue.(type) {
		case Class:
			className = c.Name

		case *Class:
			className = c.Name

		default:
			LangError(ErrorType, "right side of instanceof must be class, got %s", TypeName(classValue))
		}

		vm.push(vm.isInstanceOf(objectValue, className))

	case OP_SPAWN:
		value := vm.pop()

		fn, ok := value.(FunctionValue)
		if !ok {
			LangError(ErrorType, "spawn expects function, got %s", TypeName(value))
		}

		task := &NativeTaskValue{
			Done: make(chan TaskResult, 1),
		}

		taskVM := vm.CloneForTask()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					task.Done <- TaskResult{
						Error: r,
					}
				}
			}()

			result := taskVM.callFunctionValue(fn, []Value{})

			task.Done <- TaskResult{
				Value: result,
			}
		}()

		vm.push(task)

	case OP_TYPEOF:
		value := vm.pop()
		vm.push(TypeName(value))

	case OP_NEGATE:
		value := vm.pop()

		switch v := value.(type) {
		case int:
			vm.push(-v)

		case int64:
			vm.push(-v)

		case float64:
			vm.push(-v)

		case float32:
			vm.push(-v)

		default:
			LangError(ErrorType, "cannot negate %s", TypeName(value))
		}
	case OP_CLOSURE:
		info := instr.Value.(ClosureInfo)

		captures := map[int]*Cell{}

		if len(info.Captures) > 0 {
			if len(vm.frames) == 0 {
				LangError(ErrorInternal, "closure has captures but no current function frame")
			}

			frame := vm.currentFrame()
			frame.hasEscapedLocals = true

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
			vm.runtimeError(ErrorType, "expected object, got %s", TypeName(objectValue))
		}

		_, isClass := object["__class"]
		if isClass {
			constFields := object["__constFields"].(map[string]bool)
			if _, isConstant := constFields[name]; isConstant {
				vm.runtimeError(ErrorRuntime, "cannot assign to constant field: %s", name)
			}

			if !vm.canAccessField(object, name) {
				vm.runtimeError(ErrorRuntime, "cannot assign private field: %s", name)
			}
		}

		object[name] = value

	case OP_METHOD_CALL_SAFE:
		info := instr.Value.(MethodCallInfo)

		args := vm.popArgs(info.ArgCount)
		objectValue := vm.pop()

		if isNullish(objectValue) {
			vm.push(UndefinedValue{})
			break
		}

		vm.callMethodResolved(info.Method, objectValue, args)

	case OP_GET_PROPERTY_SAFE:
		name := instr.Value.(string)
		objectValue := vm.pop()

		if isNullish(objectValue) {
			vm.push(UndefinedValue{})
			break
		}

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
			LangError(ErrorType, "expected object, got %s", TypeName(objectValue))
		}

		if !vm.canAccessField(object, name) {
			LangError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := object[name]
		if !exists {
			LangError(ErrorName, "object has no property: %s", name)
		}

		vm.push(value)

	case OP_LOAD_GLOBAL:
		var name string

		switch operand := instr.Value.(type) {
		case string:
			name = operand

		case VariableInfo:
			name = operand.Name

		default:
			vm.runtimeError(ErrorRuntime, "OP_LOAD_GLOBAL expected string or VariableInfo, got %v", instr)
		}

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

		if !CheckTypeHint(value, info.TypeHint) {
			LangError(
				ErrorType,
				"variable %s expected %s, got %s",
				info.Name,
				info.TypeHint.Name,
				TypeName(value),
			)
		}

		vm.globals[info.Name] = value
		vm.globalConstants[info.Name] = info.Constant
		vm.globalTypes[info.Name] = info.TypeHint

	case OP_LOAD_LOCAL:
		slot := instr.Value.(int)
		frame := &vm.frames[len(vm.frames)-1]

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

		vm.push(cellValue(frame.locals[slot]))

	case OP_LOAD_LOCAL_0:
		frame := &vm.frames[len(vm.frames)-1]
		cell := frame.locals[0]
		if cell == nil {
			LangError(ErrorInternal, "local slot is nil: function=%s slot=0 locals=%d", frame.function.Name, len(frame.locals))
		}
		vm.push(cellValue(cell))

	case OP_LOAD_LOCAL_1:
		frame := &vm.frames[len(vm.frames)-1]
		cell := frame.locals[1]
		if cell == nil {
			LangError(ErrorInternal, "local slot is nil: function=%s slot=1 locals=%d", frame.function.Name, len(frame.locals))
		}
		vm.push(cellValue(cell))

	case OP_LOAD_LOCAL_2:
		frame := &vm.frames[len(vm.frames)-1]
		cell := frame.locals[2]
		if cell == nil {
			LangError(ErrorInternal, "local slot is nil: function=%s slot=2 locals=%d", frame.function.Name, len(frame.locals))
		}
		vm.push(cellValue(cell))

	case OP_LOAD_LOCAL_3:
		frame := &vm.frames[len(vm.frames)-1]
		cell := frame.locals[3]
		if cell == nil {
			LangError(ErrorInternal, "local slot is nil: function=%s slot=3 locals=%d", frame.function.Name, len(frame.locals))
		}
		vm.push(cellValue(cell))

	case OP_STORE_LOCAL:
		info := instr.Value.(VariableInfo)
		value := vm.pop()

		frame := vm.currentFrame()

		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			LangError(ErrorInternal, "local slot out of range: %d", info.Slot)
		}

		if !info.TypeHint.IsEmpty() && !CheckTypeHint(value, info.TypeHint) {
			LangError(
				ErrorType,
				"variable %s expected %s, got %s",
				info.Name,
				info.TypeHint.Name,
				TypeName(value),
			)
		}

		frame.locals[info.Slot] = &Cell{}
		setCellValue(frame.locals[info.Slot], value)
		frame.constants[info.Slot] = info.Constant
		frame.localTypes[info.Slot] = info.TypeHint

	case OP_ASSIGN_GLOBAL:
		name := instr.Value.(string)
		value := vm.pop()

		if vm.globalConstants[name] {
			LangError(ErrorConst, "cannot assign to constant global")
		}

		hint := vm.globalTypes[name]

		if !hint.IsEmpty() && !CheckTypeHint(value, hint) {
			LangError(
				ErrorType,
				"global %s expected %s, got %s",
				name,
				hint.Name,
				TypeName(value),
			)
		}

		vm.globals[name] = value

	case OP_DEC_LOCAL:
		info := instr.Value.(DecrementInfo)
		slot := info.Slot

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
				"local slot is nil during increment: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.constants[slot] {
			LangError(ErrorConst, "cannot increment constant local")
		}

		value := cellValue(frame.locals[slot])

		switch v := value.(type) {
		case int:
			if info.IsFloat {
				setCellValue(frame.locals[slot], float64(v)-info.FloatAmount)
			} else {
				frame.locals[slot].Int = v - info.IntAmount
				frame.locals[slot].IsInt = true
			}

		case float64:
			if info.IsFloat {
				setCellValue(frame.locals[slot], v-info.FloatAmount)
			} else {
				setCellValue(frame.locals[slot], v-float64(info.IntAmount))
			}

		default:
			LangError(ErrorType, "cannot increment %s", TypeName(value))
		}

	case OP_DEC_GLOBAL:
		info := instr.Value.(DecrementInfo)
		name := info.Name

		if vm.globalConstants[name] {
			LangError(ErrorConst, "cannot increment constant global")
		}

		value, exists := vm.globals[name]
		if !exists {
			LangError(ErrorName, "undefined global variable: %s", name)
		}

		switch v := value.(type) {
		case int:
			vm.globals[name] = v - info.IntAmount

		case float64:
			if info.IsFloat {
				vm.globals[name] = v - info.FloatAmount
			} else {
				vm.globals[name] = v - float64(info.IntAmount)
			}

		default:
			LangError(ErrorType, "cannot increment %s", TypeName(value))
		}

	case OP_INC_LOCAL:
		info := instr.Value.(IncrementInfo)

		frame := &vm.frames[len(vm.frames)-1]

		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			LangError(ErrorInternal, "local slot out of range in OP_INC_LOCAL")
		}

		cell := frame.locals[info.Slot]

		if cell == nil {
			LangError(ErrorInternal, "local cell is nil in OP_INC_LOCAL")
		}

		if frame.constants[info.Slot] {
			LangError(ErrorConst, "cannot assign to constant local")
		}

		if cell.IsInt && !info.IsFloat {
			cell.Int += info.IntAmount
			break
		}

		switch v := cellValue(cell).(type) {
		case int:
			cell.Int = v + info.IntAmount
			cell.IsInt = true

		case int64:
			setCellValue(cell, v+int64(info.IntAmount))

		case float64:
			setCellValue(cell, v+info.FloatAmount)

		case float32:
			setCellValue(cell, v+float32(info.FloatAmount))

		default:
			vm.runtimeError(ErrorType, "cannot increment %s", TypeName(cellValue(cell)))
		}

	case OP_INC_GLOBAL:
		info := instr.Value.(IncrementInfo)
		name := info.Name

		if vm.globalConstants[name] {
			LangError(ErrorConst, "cannot increment constant global")
		}

		value, exists := vm.globals[name]
		if !exists {
			LangError(ErrorName, "undefined global variable: %s", name)
		}

		switch v := value.(type) {
		case int:
			if info.IsFloat {
				vm.globals[name] = float64(v) + info.FloatAmount
			} else {
				vm.globals[name] = v + info.IntAmount
			}

		case float64:
			if info.IsFloat {
				vm.globals[name] = v + info.FloatAmount
			} else {
				vm.globals[name] = v + float64(info.IntAmount)
			}

		default:
			LangError(ErrorType, "cannot increment %s", TypeName(value))
		}

	case OP_ASSIGN_LOCAL:
		slot := instr.Value.(int)
		value := vm.popFast()

		frame := &vm.frames[len(vm.frames)-1]

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

		hint := frame.localTypes[slot]

		if !hint.IsEmpty() && !CheckTypeHint(value, hint) {
			LangError(
				ErrorType,
				"local variable expected %s, got %s",
				hint.Name,
				TypeName(value),
			)
		}

		setCellValue(frame.locals[slot], value)

	case OP_MUL_LOCAL_CONST:
		info := instr.Value.(LocalConstInfo)
		frame := &vm.frames[len(vm.frames)-1]
		vm.push(multiplyByInt(frameLocalValue(frame, info.Slot, "OP_MUL_LOCAL_CONST"), info.Value))

	case OP_ADD:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(addValues(left, right))

	case OP_SUB:
		right := vm.popFast()
		left := vm.popFast()

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
			LangError(ErrorType, "cannot subtract %s and %s", TypeName(left), TypeName(right))
		}

	case OP_MUL:
		right := vm.popFast()
		left := vm.popFast()

		if isNumber(left) && isNumber(right) {
			if _, ok := left.(float64); ok {
				vm.push(asFloat(left) * asFloat(right))
			} else if _, ok := right.(float64); ok {
				vm.push(asFloat(left) * asFloat(right))
			} else {
				vm.push(left.(int) * right.(int))
			}
		} else {
			LangError(ErrorType, "cannot multiply %s and %s", TypeName(left), TypeName(right))
		}

	case OP_DIV:
		right := vm.popFast()
		left := vm.popFast()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot divide %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(asFloat(left) / asFloat(right))

	case OP_EQ:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(valuesEqual(left, right))

	case OP_NEQ:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(!valuesEqual(left, right))

	case OP_LT:
		right := vm.popFast()
		left := vm.popFast()

		switch l := left.(type) {
		case int:
			switch r := right.(type) {
			case int:
				vm.push(l < r)

			case float64:
				vm.push(float64(l) < r)

			default:
				LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		case float64:
			switch r := right.(type) {
			case int:
				vm.push(l < float64(r))

			case float64:
				vm.push(l < r)

			default:
				LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		default:
			LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

	case OP_GT:
		right := vm.popFast()
		left := vm.popFast()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(asFloat(left) > asFloat(right))

	case OP_LTE:
		right := vm.popFast()
		left := vm.popFast()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(asFloat(left) <= asFloat(right))

	case OP_GTE:
		right := vm.popFast()
		left := vm.popFast()

		if !isNumber(left) || !isNumber(right) {
			LangError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(asFloat(left) >= asFloat(right))

	case OP_AND:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(isTruthy(left) && isTruthy(right))

	case OP_OR:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(isTruthy(left) || isTruthy(right))

	case OP_JUMP:
		target := instr.Value.(int)
		vm.setIP(target)

	case OP_JUMP_IF_FALSE:
		target := instr.Value.(int)
		condition := vm.popFast()

		if !isTruthy(condition) {
			vm.setIP(target)
		}

	case OP_JUMP_IF_TRUE:
		target := instr.Value.(int)
		condition := vm.popFast()

		if isTruthy(condition) {
			vm.setIP(target)
		}

	case OP_METHOD_CALL:
		info := instr.Value.(MethodCallInfo)

		vm.callMethod(info.Method, info.ArgCount)

	case OP_METHOD_CALL_LOCAL_0:
		info := instr.Value.(MethodLocalCallInfo)
		frame := &vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_0")

		if vm.callZeroArgNativeMethod(info.Method, objectValue) {
			break
		}
		vm.callMethodResolved(info.Method, objectValue, nil)

	case OP_METHOD_CALL_LOCAL_1:
		info := instr.Value.(MethodLocalCallInfo)
		frame := &vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_1")
		arg := frameLocalValue(frame, info.ArgSlot, "OP_METHOD_CALL_LOCAL_1")

		if vm.callOneArgNativeMethod(info.Method, objectValue, arg) {
			break
		}
		vm.callMethodResolved(info.Method, objectValue, []Value{arg})

	case OP_ARRAY_LEN_LOCAL:
		info := instr.Value.(ArrayLocalCallInfo)
		frame := &vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_LEN_LOCAL")

		if array, ok := arrayValue.(*ArrayValue); ok {
			vm.push(len(array.Elements))
			break
		}
		vm.callMethodResolved("length", arrayValue, nil)

	case OP_ARRAY_GET_LOCAL:
		info := instr.Value.(ArrayLocalCallInfo)
		frame := &vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_GET_LOCAL")
		indexValue := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_GET_LOCAL")

		if array, ok := arrayValue.(*ArrayValue); ok {
			index, ok := indexValue.(int)
			if !ok {
				vm.runtimeError(ErrorType, "array.get argument 1 expected number, got %s", TypeName(indexValue))
			}
			if index < 0 || index >= len(array.Elements) {
				vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			}
			vm.push(array.Elements[index])
			break
		}
		vm.callMethodResolved("get", arrayValue, []Value{indexValue})

	case OP_ARRAY_PUSH_LOCAL:
		info := instr.Value.(ArrayLocalCallInfo)
		frame := &vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL")
		value := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL")

		if array, ok := arrayValue.(*ArrayValue); ok {
			array.Elements = append(array.Elements, value)
			vm.push(array)
			break
		}
		vm.callMethodResolved("push", arrayValue, []Value{value})

	case OP_ARRAY_PUSH_LOCAL_MUL_CONST:
		info := instr.Value.(ArrayLocalMulConstInfo)
		frame := &vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST")
		arg := multiplyByInt(frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST"), info.Factor)

		if array, ok := arrayValue.(*ArrayValue); ok {
			array.Elements = append(array.Elements, arg)
			vm.push(array)
			break
		}
		vm.callMethodResolved("push", arrayValue, []Value{arg})

	case OP_LEN:
		value := vm.pop()

		switch v := value.(type) {
		case *ArrayValue:
			vm.push(len(v.Elements))

		case ArrayValue:
			vm.push(len(v.Elements))

		case string:
			vm.push(len([]rune(v)))

		case ObjectValue:
			vm.push(len(v))

		case BufferValue:
			vm.push(len(v.Bytes))

		case *BufferValue:
			vm.push(len(v.Bytes))

		default:
			LangError(ErrorType, "cannot get length of %s", TypeName(value))
		}

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
			LangError(ErrorType, "expected function or class, got %s", TypeName(callee))
		}

	case OP_BUILTIN_CALL:
		info := instr.Value.(BuiltinCallInfo)
		vm.callBuiltin(info.Object, info.Method, info.ArgCount)

	case OP_MOD:
		right := vm.popFast()
		left := vm.popFast()

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

		case float32:
			r := asFloat64(right)

			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(math.Mod(float64(l), r))

		case float64:
			r := asFloat64(right)

			if r == 0 {
				LangError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(math.Mod(l, r))

		default:
			LangError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
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
			key := indexValue

			value, exists := obj[key]
			if !exists {
				obj[key] = UndefinedValue{}
				value = obj[key]
			}

			vm.push(value)

		default:
			LangError(ErrorType, "cannot index %s", TypeName(objectValue))
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
			key := indexValue
			if className, isClass := obj["__class"]; isClass {
				vm.runtimeError(ErrorRuntime, "cannot modify class '%s' by index operator.", className)
			}
			obj[key] = value

		default:
			LangError(ErrorType, "cannot index assign %s", TypeName(objectValue))
		}

	case OP_RETURN:
		var returnValue Value

		if len(vm.stack) == 0 {
			returnValue = UndefinedValue{}
		} else {
			returnValue = vm.pop()
		}

		if len(vm.frames) == 0 {
			vm.push(returnValue)
			return true
		}

		returningDepth := len(vm.frames)
		vm.removeTryHandlersAtOrAbove(returningDepth)

		frame := vm.frames[len(vm.frames)-1]

		if !frame.function.ReturnType.IsEmpty() && !CheckTypeHint(returnValue, frame.function.ReturnType) {
			LangError(
				ErrorType,
				"function %s should return %s, got %s",
				frame.function.Name,
				frame.function.ReturnType.Name,
				TypeName(returnValue),
			)
		}

		if len(vm.frames) == 0 {
			LangError(ErrorRuntime, "return used outside of function")
		}

		vm.frames = vm.frames[:len(vm.frames)-1]

		vm.releaseFrame(frame)

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

		if info.ExprCount == 1 {
			value := vm.pop()
			vm.push(info.Parts[0] + valueToString(value) + info.Parts[1])
			break
		}

		values := make([]Value, info.ExprCount)

		for i := info.ExprCount - 1; i >= 0; i-- {
			values[i] = vm.pop()
		}

		var builder strings.Builder

		for i := 0; i < info.ExprCount; i++ {
			builder.WriteString(info.Parts[i])
			builder.WriteString(valueToString(values[i]))
		}

		builder.WriteString(info.Parts[len(info.Parts)-1])

		vm.push(builder.String())

	case OP_OBJECT:
		info := instr.Value.(ObjectInfo)

		object := make(ObjectValue, len(info.Names))

		for i := len(info.Names) - 1; i >= 0; i-- {
			object[info.Names[i]] = vm.pop()
		}

		vm.push(object)

	case OP_NOT:
		value := vm.pop()
		vm.push(!isTruthy(value))

	case OP_GET_PROPERTY_LOCAL:
		info := instr.Value.(PropertyLocalInfo)
		frame := &vm.frames[len(vm.frames)-1]
		vm.push(propertyValue(vm, frameLocalValue(frame, info.Slot, "OP_GET_PROPERTY_LOCAL"), info.Name))

	case OP_ADD_PROPERTY_LOCAL_LOCAL:
		info := instr.Value.(PropertyLocalAssignInfo)
		frame := &vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ObjectSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
		object, ok := objectValue.(ObjectValue)
		if !ok {
			LangError(ErrorType, "expected object, got %s", TypeName(objectValue))
		}

		_, isClass := object["__class"]
		if isClass {
			constFields := object["__constFields"].(map[string]bool)
			if _, isConstant := constFields[info.Name]; isConstant {
				vm.runtimeError(ErrorRuntime, "cannot assign to constant field: %s", info.Name)
			}
			if !vm.canAccessField(object, info.Name) {
				vm.runtimeError(ErrorRuntime, "cannot assign private field: %s", info.Name)
			}
		}

		current := propertyValue(vm, object, info.Name)
		source := frameLocalValue(frame, info.SourceSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
		object[info.Name] = addValues(current, source)

	case OP_GET_PROPERTY:
		name := instr.Value.(string)
		objectValue := vm.pop()
		vm.push(propertyValue(vm, objectValue, name))

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

		trace := vm.stackTrace()

		panic(LangErrorType{
			Kind:    ErrorKind(kind),
			Message: message + "\n\nStack trace:\n" + trace,
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

		setCellValue(frame.locals[handler.Slot], errorObject)
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

	expected := len(fn.Params)
	isVariadic := expected > 0 && fn.Params[expected-1].Variadic

	if isVariadic {
		minArgs := expected - 1

		if len(args) < minArgs {
			vm.runtimeError(
				ErrorRuntime,
				"function %s expects at least %d arguments, got %d",
				fn.Name,
				minArgs,
				len(args),
			)
		}
	} else {
		if fn.HasDefaults {
			args = vm.applyDefaultArgs(fn, args, 0, fn.Name)
		} else if len(args) != expected {
			vm.runtimeError(
				ErrorRuntime,
				"function %s expects %d arguments, gosst %d",
				fn.Name,
				expected,
				len(args),
			)
		}
	}

	frame := vm.getFrame(fn)

	if len(fnValue.Captures) > 0 {
		frame.hasEscapedLocals = true
	}

	for slot, cell := range fnValue.Captures {
		if slot < 0 || slot >= len(frame.locals) {
			LangError(ErrorInternal, "capture slot out of range in function value: %d", slot)
		}

		frame.locals[slot] = cell
	}

	if isVariadic {
		fixedCount := expected - 1

		for i := 0; i < fixedCount; i++ {
			setCellValue(frame.locals[i], args[i])
			frame.constants[i] = false
		}

		rest := &ArrayValue{
			Elements: []Value{},
		}

		for i := fixedCount; i < len(args); i++ {
			rest.Elements = append(rest.Elements, args[i])
		}

		setCellValue(frame.locals[fixedCount], rest)
		frame.constants[fixedCount] = false
	} else {
		for i, arg := range args {
			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
		}
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callFunctionDirect(fn Function, args []Value) {
	args = vm.applyDefaultArgs(fn, args, 0, "function "+fn.Name)

	frame := vm.getFrame(fn)

	for i, arg := range args {
		setCellValue(frame.locals[i], arg)
		frame.constants[i] = false
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callFunctionValue(fnValue FunctionValue, args []Value) Value {
	frameDepthBefore := len(vm.frames)
	stackDepthBefore := vm.top

	vm.callFunctionValueWithArgs(fnValue, args)

	for len(vm.frames) > frameDepthBefore {
		if vm.step() {
			LangError(ErrorRuntime, "program halted while running function value")
		}
	}

	if len(vm.stack) <= stackDepthBefore {
		return UndefinedValue{}
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

func writeServerResponse(w http.ResponseWriter, value any, responseType HttpResponseType) {
	switch responseType {
	case HttpJson:
		w.Header().Set("Content-Type", "application/json")
		jsonValue := valueToJSONCompatible(value)
		bytes, _ := json.Marshal(jsonValue)
		fmt.Fprint(w, string(bytes))

	case HttpText:
		stringValue, _ := value.(string)
		trimmed := strings.TrimSpace(stringValue)
		fmt.Fprint(w, trimmed)
	}
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

func (vm *VM) findEmbeddedMethod(object ObjectValue, method string) (ObjectValue, FunctionValue, bool) {
	classNameValue, ok := object["__class"]
	if !ok {
		return nil, FunctionValue{}, false
	}

	className, ok := classNameValue.(string)
	if !ok {
		return nil, FunctionValue{}, false
	}

	class, ok := vm.classes[className]
	if !ok {
		return nil, FunctionValue{}, false
	}

	for _, fieldName := range class.Embeds {
		fieldValue, exists := object[fieldName]
		if !exists {
			continue
		}

		embeddedObject, ok := fieldValue.(ObjectValue)
		if !ok {
			continue
		}

		methodValue, exists := embeddedObject[method]
		if !exists {
			// optional: recursive embedding
			if receiver, fn, ok := vm.findEmbeddedMethod(embeddedObject, method); ok {
				return receiver, fn, true
			}

			continue
		}

		fnValue, ok := methodValue.(FunctionValue)
		if !ok {
			continue
		}

		return embeddedObject, fnValue, true
	}

	return nil, FunctionValue{}, false
}

func (vm *VM) callZeroArgNativeMethod(method string, objectValue Value) bool {
	switch value := objectValue.(type) {
	case *ArrayValue:
		switch method {
		case "length":
			vm.push(len(value.Elements))
			return true
		case "pop":
			if len(value.Elements) == 0 {
				vm.push(UndefinedValue{})
				return true
			}

			last := value.Elements[len(value.Elements)-1]
			value.Elements = value.Elements[:len(value.Elements)-1]
			vm.push(last)
			return true
		case "clear":
			value.Elements = []Value{}
			vm.push(true)
			return true
		}

	case string:
		switch method {
		case "length":
			vm.push(len(value))
			return true
		case "toUpperCase", "upper":
			if method == "upper" {
				vm.push(strings.ToUpper(value[:1]) + value[1:])
			} else {
				vm.push(strings.ToUpper(value))
			}
			return true
		case "toLowerCase", "lower":
			if method == "lower" {
				vm.push(strings.ToLower(value[:1]) + value[1:])
			} else {
				vm.push(strings.ToLower(value))
			}
			return true
		case "trim":
			vm.push(strings.TrimSpace(value))
			return true
		}
	}

	return false
}

func (vm *VM) callOneArgNativeMethod(method string, objectValue Value, arg Value) bool {
	switch value := objectValue.(type) {
	case *ArrayValue:
		switch method {
		case "push":
			value.Elements = append(value.Elements, arg)
			vm.push(value)
			return true
		case "get":
			index, ok := arg.(int)
			if !ok {
				vm.runtimeError(ErrorType, "array.get argument 1 expected number, got %s", TypeName(arg))
				return true
			}
			if index < 0 || index >= len(value.Elements) {
				vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
				return true
			}
			vm.push(value.Elements[index])
			return true
		case "join":
			separator, ok := arg.(string)
			if !ok {
				vm.runtimeError(ErrorType, "array.join argument 1 expected string, got %s", TypeName(arg))
				return true
			}

			var sb strings.Builder
			for i, item := range value.Elements {
				sb.WriteString(valueToString(item))
				if i != len(value.Elements)-1 {
					sb.WriteString(separator)
				}
			}
			vm.push(sb.String())
			return true
		}

	case string:
		switch method {
		case "charAt":
			idx, ok := arg.(int)
			if !ok {
				vm.runtimeError(ErrorType, "string.chatrAt argument 1 expected number, got %s", TypeName(arg))
				return true
			}
			runes := []rune(value)
			if idx < 0 || idx >= len(runes) {
				vm.push(NullValue{})
				return true
			}
			vm.push(string(runes[idx]))
			return true
		case "split":
			separator, ok := arg.(string)
			if !ok {
				vm.runtimeError(ErrorType, "string.split argument 1 expected string, got %s", TypeName(arg))
				return true
			}

			if separator == "" {
				runes := []rune(value)
				elements := make([]Value, len(runes))
				for i, r := range runes {
					elements[i] = string(r)
				}
				vm.push(&ArrayValue{Elements: elements})
				return true
			}

			count := strings.Count(value, separator) + 1
			elements := make([]Value, 0, count)

			for {
				idx := strings.Index(value, separator)
				if idx == -1 {
					elements = append(elements, value)
					break
				}
				elements = append(elements, value[:idx])
				value = value[idx+len(separator):]
			}

			vm.push(&ArrayValue{Elements: elements})
			return true
		case "includes":
			search, ok := arg.(string)
			if !ok {
				vm.runtimeError(ErrorType, "string.includes argument 1 expected string, got %s", TypeName(arg))
				return true
			}
			vm.push(strings.Contains(value, search))
			return true
		}
	}

	return false
}

func (vm *VM) callTwoArgNativeMethod(method string, objectValue Value, arg0 Value, arg1 Value) bool {
	switch value := objectValue.(type) {
	case *ArrayValue:
		if method != "set" {
			return false
		}

		index, ok := arg0.(int)
		if !ok {
			vm.runtimeError(ErrorType, "array.set argument 1 expected number, got %s", TypeName(arg0))
			return true
		}
		if index < 0 || index >= len(value.Elements) {
			vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
			return true
		}

		value.Elements[index] = arg1
		vm.push(value)
		return true

	case string:
		oldText, oldOK := arg0.(string)
		newText, newOK := arg1.(string)

		switch method {
		case "replace":
			if !oldOK {
				vm.runtimeError(ErrorType, "string.replace argument 1 expected string, got %s", TypeName(arg0))
				return true
			}
			if !newOK {
				vm.runtimeError(ErrorType, "string.replace argument 2 expected string, got %s", TypeName(arg1))
				return true
			}
			vm.push(strings.Replace(value, oldText, newText, 1))
			return true
		case "replaceAll":
			if !oldOK {
				vm.runtimeError(ErrorType, "string.replaceAll argument 1 expected string, got %s", TypeName(arg0))
				return true
			}
			if !newOK {
				vm.runtimeError(ErrorType, "string.replaceAll argument 2 expected string, got %s", TypeName(arg1))
				return true
			}
			vm.push(strings.ReplaceAll(value, oldText, newText))
			return true
		}
	}

	return false
}

func (vm *VM) callStdObjectFast1(method string, objectValue Value, arg0 Value) bool {
	module, ok := objectValue.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "length" {
		return false
	}

	obj, ok := arg0.(ObjectValue)
	if !ok {
		vm.runtimeError(ErrorType, "object.length argument 1 expected object, got %s", TypeName(arg0))
		return true
	}
	vm.push(len(obj))
	return true
}

func (vm *VM) callStdObjectFast2(method string, objectValue Value, arg0 Value, arg1 Value) bool {
	module, ok := objectValue.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "get" {
		return false
	}

	obj, ok := arg0.(ObjectValue)
	if !ok {
		vm.runtimeError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(arg0))
		return true
	}
	key, ok := arg1.(string)
	if !ok {
		vm.runtimeError(ErrorType, "object.get argument 2 expected string, got %s", TypeName(arg1))
		return true
	}
	vm.push(obj[key])
	return true
}

func (vm *VM) callStdObjectFast3(method string, objectValue Value, arg0 Value, arg1 Value, arg2 Value) bool {
	module, ok := objectValue.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "set" {
		return false
	}

	obj, ok := arg0.(ObjectValue)
	if !ok {
		vm.runtimeError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(arg0))
		return true
	}
	key, ok := arg1.(string)
	if !ok {
		vm.runtimeError(ErrorType, "object.set argument 2 expected string, got %s", TypeName(arg1))
		return true
	}
	obj[key] = arg2
	vm.push(UndefinedValue{})
	return true
}

func (vm *VM) callStdObjectFast(method string, objectValue Value, args ...Value) bool {
	module, ok := objectValue.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	switch method {
	case "get":
		if len(args) != 2 {
			return false
		}
		obj, ok := args[0].(ObjectValue)
		if !ok {
			vm.runtimeError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}
		key, ok := args[1].(string)
		if !ok {
			vm.runtimeError(ErrorType, "object.get argument 2 expected string, got %s", TypeName(args[1]))
			return true
		}
		vm.push(obj[key])
		return true

	case "set":
		if len(args) != 3 {
			return false
		}
		obj, ok := args[0].(ObjectValue)
		if !ok {
			vm.runtimeError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}
		key, ok := args[1].(string)
		if !ok {
			vm.runtimeError(ErrorType, "object.set argument 2 expected string, got %s", TypeName(args[1]))
			return true
		}
		obj[key] = args[2]
		vm.push(UndefinedValue{})
		return true

	case "length":
		if len(args) != 1 {
			return false
		}
		obj, ok := args[0].(ObjectValue)
		if !ok {
			vm.runtimeError(ErrorType, "object.length argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}
		vm.push(len(obj))
		return true
	}

	return false
}

func (vm *VM) callMethodFast(method string, argCount int) {
	switch argCount {
	case 0:
		objectValue := vm.pop()
		if vm.callZeroArgNativeMethod(method, objectValue) {
			return
		}
		vm.callMethodResolved(method, objectValue, nil)
		return

	case 1:
		arg0 := vm.pop()
		objectValue := vm.pop()
		if vm.callStdObjectFast1(method, objectValue, arg0) {
			return
		}
		if vm.callOneArgNativeMethod(method, objectValue, arg0) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0})
		return

	case 2:
		arg1 := vm.pop()
		arg0 := vm.pop()
		objectValue := vm.pop()
		if vm.callStdObjectFast2(method, objectValue, arg0, arg1) {
			return
		}
		if vm.callTwoArgNativeMethod(method, objectValue, arg0, arg1) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0, arg1})
		return

	case 3:
		arg2 := vm.pop()
		arg1 := vm.pop()
		arg0 := vm.pop()
		objectValue := vm.pop()
		if vm.callStdObjectFast3(method, objectValue, arg0, arg1, arg2) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0, arg1, arg2})
		return

	default:
		args := vm.popArgs(argCount)
		objectValue := vm.pop()
		vm.callMethodResolved(method, objectValue, args)
		return
	}
}

func (vm *VM) callMethodResolved(method string, objectValue Value, args []Value) {
	if method == "toString" {
		if _, ok := objectValue.(*BufferValue); !ok {
			vm.push(valueToString(objectValue))
			return
		}
	}

	switch val := objectValue.(type) {
	case NamespaceValue:
		vm.callNamespaceMethod(val, method, args)
		return

	case *NamespaceValue:
		vm.callNamespaceMethod(*val, method, args)
		return

	case *NativePluginValue:
		vm.callNativePlugin(val, method, args)
		return

	case *NativeAppValue:
		vm.callNativeAppMethod(val, method, args)
		return

	case *StandardModuleValue:
		vm.callStandardModule(val.Name, method, args)
		return

	case *NativeServerValue:
		vm.callServerMethod(val, method, args)
		return

	case *NativeTcpServerValue:
		vm.callTcpServerMethod(val, method, args)
		return

	case *NativeTcpConnectionValue:
		vm.callTcpConnMethod(val, method, args)
		return

	case *BufferValue:
		vm.callBufferMethod(val, method, args)
		return

	case *NativeFileValue:
		vm.callFileMethod(val, method, args)
		return

	case *ArrayValue:
		vm.callArrayMethod(val, method, args)
		return

	case *NativeTaskValue:
		vm.callTaskMethod(val, method, args)
		return

	case *NativeProcessValue:
		vm.callProcessMethod(val, method, args)
		return

	case *NativeStringBuilderValue:
		vm.callStringBuilderMethod(val, method, args)
		return

	case string:
		vm.callStringMethod(val, method, args)
		return
	}

	object, ok := objectValue.(ObjectValue)
	if !ok {
		LangError(ErrorType, "expected object, got %s", TypeName(objectValue))
	}

	receiver := object

	methodValue, exists := object[method]

	var fnValue FunctionValue

	if exists {
		var ok bool

		fnValue, ok = methodValue.(FunctionValue)
		if !ok {
			LangError(ErrorType, "property %s is not callable", method)
		}
	} else {
		embeddedReceiver, embeddedFn, ok := vm.findEmbeddedMethod(object, method)
		if !ok {
			LangError(ErrorName, "object has no method: %s", method)
		}

		receiver = embeddedReceiver
		fnValue = embeddedFn
	}

	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		LangError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	ownerClass := methodOwnerClass(fnValue.Name)

	if isClass(receiver) && !vm.canAccessMethod(object, method) {
		vm.runtimeError(ErrorRuntime, "cannot access private method %s in class %s", method, ownerClass)
	}

	paramOffset := 0
	if len(fn.Params) > 0 && fn.Params[0].Name == "this" {
		paramOffset = 1
	}

	userParamCount := len(fn.Params) - paramOffset
	isVariadic := userParamCount > 0 && fn.Params[len(fn.Params)-1].Variadic

	if isVariadic {
		minArgs := userParamCount - 1

		if len(args) < minArgs {
			vm.runtimeError(
				ErrorRuntime,
				"method %s expects at least %d arguments, got %d",
				method,
				minArgs,
				len(args),
			)
		}
	} else {
		if fn.HasDefaults {
			args = vm.applyDefaultArgs(fn, args, paramOffset, "method "+method)
		} else if len(args) != userParamCount {
			vm.runtimeError(
				ErrorRuntime,
				"method %s expects %d arguments, got %d",
				method,
				userParamCount,
				len(args),
			)
		}
	}

	frame := vm.getFrame(fn)
	frame.methodClass = ownerClass

	setCellValue(frame.locals[0], receiver)
	frame.constants[0] = true

	if isVariadic {
		fixedCount := userParamCount - 1

		// normal params before ...args
		for i := 0; i < fixedCount; i++ {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]
			arg := args[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() && !CheckTypeHint(arg, param.TypeHint) {
				LangError(
					ErrorType,
					"method %s parameter %s expected %s, got %s",
					method,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
				)
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.constants[paramIndex] = false
			frame.localTypes[paramIndex] = param.TypeHint
		}

		// rest param
		restSlot := paramOffset + fixedCount
		restParam := fn.Params[restSlot]

		rest := &ArrayValue{
			Elements: []Value{},
		}

		for i := fixedCount; i < len(args); i++ {
			arg := args[i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() && !CheckTypeHint(arg, restParam.TypeHint) {
				LangError(
					ErrorType,
					"method %s rest parameter %s expected %s, got %s",
					method,
					restParam.Name,
					restParam.TypeHint.String(),
					TypeName(arg),
				)
			}

			rest.Elements = append(rest.Elements, arg)
		}

		setCellValue(frame.locals[restSlot], rest)
		frame.constants[restSlot] = false
		frame.localTypes[restSlot] = TypeHint{
			Name: "array",
		}
	} else {
		for i, arg := range args {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() && !CheckTypeHint(arg, param.TypeHint) {
				LangError(
					ErrorType,
					"method %s parameter %s expected %s, got %s",
					method,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
				)
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.constants[paramIndex] = false
			frame.localTypes[paramIndex] = param.TypeHint
		}
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callMethod(method string, argCount int) {
	vm.callMethodFast(method, argCount)
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

		name := asString(args[0], vm)

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
		vm.push(UndefinedValue{})

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

	args = vm.applyDefaultArgs(fn, args, 0, "function "+fn.Name)

	frame := vm.getFrame(fn)

	for i, arg := range args {
		param := fn.Params[i]

		if !param.TypeHint.IsEmpty() && !CheckTypeHint(arg, param.TypeHint) {
			LangError(
				ErrorType,
				"function %s parameter %s expected %s, got %s",
				fn.Name,
				param.Name,
				param.TypeHint.String(),
				TypeName(arg),
			)
		}

		setCellValue(frame.locals[i], arg)
		frame.constants[i] = false
		frame.localTypes[i] = param.TypeHint
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
	if vm.top < count {
		vm.handleUnderflow()
	}

	args := make([]Value, count)

	start := vm.top - count

	copy(args, vm.stack[start:vm.top])

	for i := start; i < vm.top; i++ {
		vm.stack[i] = nil
	}

	vm.top = start

	return args
}

func (vm *VM) push(value Value) {
	if vm.top == len(vm.stack) {
		newStack := make([]Value, len(vm.stack)*2)
		copy(newStack, vm.stack)
		vm.stack = newStack
	}

	vm.stack[vm.top] = value
	vm.top++
}

func (vm *VM) pop() Value {
	if vm.top == 0 {
		vm.handleUnderflow()
	}

	vm.top--
	val := vm.stack[vm.top]

	return val
}

func (vm *VM) handleUnderflow() {
	LangError(
		ErrorInternal,
		"stack underflow at function=%s ip=%d op=%v value=%#v",
		vm.lastFunctionName,
		vm.lastInstructionIndex,
		vm.lastInstruction.Op,
		vm.lastInstruction.Value,
	)
}

func (vm *VM) popFast() Value {
	vm.top--
	val := vm.stack[vm.top]
	vm.stack[vm.top] = nil
	return val
}

func (vm *VM) peekFast() Value {
	if len(vm.stack) == 0 {
		vm.runtimeError(ErrorInternal, "peek from empty stack")
		return UndefinedValue{}
	}

	return vm.stack[len(vm.stack)-1]
}
