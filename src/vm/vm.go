package vm

import (
	"fmt"
	"maps"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

type NativeCallFrame struct {
	Name   string
	File   string
	Line   int
	Column int
}

type TryHandler struct {
	CatchIP int
	Name    string
	Slot    int
	IsLocal bool

	FrameDepth int
}

type DeferHandler struct {
	Function   FunctionValue
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
	interfaces       map[string]Interface
	framePool        []*Frame
	functionList     []Function

	nativeFrames []NativeCallFrame
	currentInstr Instruction

	top int

	lastInstruction      Instruction
	lastInstructionIndex int
	lastFunctionName     string

	globalTypes map[string]TypeHint

	cliArgs []string

	tryHandlers   []TryHandler
	deferHandlers []DeferHandler

	mu sync.Mutex

	ip int

	stack           []Value
	globals         []Value
	globalNames     map[string]int
	globalConstants map[string]bool

	frames []*Frame
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

func int64ToString(n int64) string {
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

func uintToString(n uint64) string {
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
	return strconv.FormatFloat(val, 'f', 6, 64)
}

func isClass(value ObjectValue) bool {
	_, exists := value["__class"]

	return exists
}

func NewVM(mainInstructions []Instruction, functions map[string]Function, classes map[string]Class, interfaces map[string]Interface, globalIndex map[string]int) *VM {
	mainInstructions, functions, functionList := normalizeFunctionIDs(mainInstructions, functions)

	return &VM{
		start:            time.Now().UnixMilli(),
		mainInstructions: mainInstructions,
		functions:        functions,
		interfaces:       interfaces,
		functionList:     functionList,
		classes:          classes,
		globals:          make([]Value, 0, 256),
		globalNames:      map[string]int{},
		globalConstants:  map[string]bool{},
		mu:               sync.Mutex{},
		cliArgs:          []string{},
		globalTypes:      map[string]TypeHint{},
		top:              0,
		stack:            make([]Value, 1024),
		framePool:        make([]*Frame, 0, 1024),
		frames:           []*Frame{},
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

func (vm *VM) getFrame(fn Function) *Frame {
	var frame *Frame

	if len(vm.framePool) > 0 {
		last := len(vm.framePool) - 1
		frame = vm.framePool[last]
		vm.framePool = vm.framePool[:last]
	}

	if frame == nil {
		frame = &Frame{}
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

		setCellValue(frame.locals[i], NewUndefined())
		frame.constants[i] = false
		frame.localTypes[i] = TypeHint{}
	}

	frame.function = fn
	frame.ip = 0
	frame.instructions = fn.Instructions
	frame.methodClass = ""
	frame.returnOverride = Value{}
	frame.hasReturnOverride = false
	frame.hasEscapedLocals = false

	return frame
}

func (vm *VM) releaseFrame(frame *Frame) {
	if frame.hasEscapedLocals {
		return
	}

	// Keep pool from growing forever.
	if len(vm.framePool) >= 1024 {
		return
	}

	for i := range frame.locals {
		if frame.locals[i] != nil {
			setCellValue(frame.locals[i], Value{})
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
		framePool:   make([]*Frame, 0, 256),
		frames:      []*Frame{},
		tryHandlers: []TryHandler{},

		globals:         vm.globals,
		globalNames:     vm.globalNames,
		globalConstants: vm.globalConstants,
		globalTypes:     vm.globalTypes,

		cliArgs: vm.cliArgs,
	}
}

func cloneValue(value Value) Value {
	var raw any
	if value.IsInt {
		raw = value.AsInt
	} else {
		raw = value.Value
	}

	switch v := raw.(type) {
	case ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range v {
			copyObj[key] = cloneValue(val)
		}

		return NewNative(copyObj)

	case *ObjectValue:
		copyObj := ObjectValue{}

		for key, val := range *v {
			copyObj[key] = cloneValue(val)
		}

		return NewNative(copyObj)

	case *ArrayValue:
		copyArr := &ArrayValue{
			Elements: make([]Value, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return NewNative(copyArr)

	case ArrayValue:
		copyArr := ArrayValue{
			Elements: make([]Value, len(v.Elements)),
		}

		for i, val := range v.Elements {
			copyArr.Elements[i] = cloneValue(val)
		}

		return NewNative(copyArr)

	case *BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return NewNative(&BufferValue{
			Bytes: bytes,
		})

	case BufferValue:
		bytes := make([]byte, len(v.Bytes))
		copy(bytes, v.Bytes)

		return NewNative(&BufferValue{
			Bytes: bytes,
		})

	default:
		return value
	}
}

func cellValue(cell *Cell) Value {
	if cell.IsInt {
		return NewInt(cell.Int)
	}
	return cell.Value
}

func setCellValue(cell *Cell, value Value) {
	if value.IsInt {
		cell.Int = value.AsInt
		cell.Value = Value{}
		cell.IsInt = true
	} else {
		cell.Value = value
		cell.Int = 0
		cell.IsInt = false
	}
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
	if object, ok := objectValue.Value.(ObjectValue); ok {
		if !vm.canAccessField(object, name) {
			vm.fatalError(ErrorRuntime, "cannot access private field: %s", name)
		}

		value, exists := object[name]
		if !exists {
			vm.fatalError(ErrorName, "object has no property: %s", name)
		}

		return value
	}

	if ns, ok := objectValue.Value.(NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	if ns, ok := objectValue.Value.(*NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, name)
		}
		return resolveNamespaceValue(vm, value)
	}

	vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
	return NewNull()
}

func resolveNamespaceValue(vm *VM, value Value) Value {
	if ref, ok := value.Value.(NamespaceMemberRef); ok {
		slot, exists := vm.globalNames[ref.GlobalName]
		if !exists {
			vm.fatalError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
		}
		return vm.globals[slot]
	}

	if ref, ok := value.Value.(*NamespaceMemberRef); ok {
		slot, exists := vm.globalNames[ref.GlobalName]
		if !exists {
			vm.fatalError(ErrorName, "undefined namespace global: %s", ref.GlobalName)
		}
		return vm.globals[slot]
	}

	return value
}

func multiplyByInt(value Value, factor int) Value {
	if value.IsInt {
		return NewInt(value.AsInt * factor)
	}

	switch v := value.Value.(type) {
	case float64:
		return NewNative(v * float64(factor))
	case float32:
		return NewNative(v * float32(factor))
	default:
		LangError(ErrorType, "cannot multiply %s and number", TypeName(value))
		return Value{}
	}
}

func addValues(left Value, right Value) Value {
	var leftVal any
	if left.IsInt {
		leftVal = left.AsInt
	} else {
		leftVal = left.Value
	}

	var rightVal any
	if right.IsInt {
		rightVal = right.AsInt
	} else {
		rightVal = right.Value
	}

	switch l := leftVal.(type) {
	case int:
		switch r := rightVal.(type) {
		case int:
			return NewInt(l + r)
		case float64:
			return NewNative(float64(l) + r)
		case uint64:
			return NewNative(uint64(l) + r)
		case int64:
			return NewNative(int64(l) + r)
		case string:
			return NewNative(intToString(l) + r)
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case int64:
		switch r := rightVal.(type) {
		case int:
			return NewNative(l + int64(r))
		case int64:
			return NewNative(l + r)
		case float64:
			return NewNative(float64(l) + r)
		case uint64:
			return NewNative(uint64(l) + r)
		case string:
			return NewNative(int64ToString(l) + r)
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case float64:
		switch r := rightVal.(type) {
		case int:
			return NewNative(l + float64(r))
		case float64:
			return NewNative(l + r)
		case uint64:
			return NewNative(l + float64(r))
		case int64:
			return NewNative(l + float64(r))
		case string:
			return NewNative(FloatToString(l) + r)
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case string:
		switch r := rightVal.(type) {
		case string:
			return NewNative(l + r)
		case float64:
			return NewNative(l + FloatToString(r))
		case int:
			return NewNative(l + intToString(r))
		case int64:
			return NewNative(l + int64ToString(r))
		case uint64:
			return NewNative(l + uintToString(r))
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	case uint64:
		switch r := rightVal.(type) {
		case uint64:
			return NewNative(l + r)
		case int:
			return NewNative(l + uint64(r))
		case int64:
			return NewNative(l + uint64(r))
		case float64:
			return NewNative(float64(l) + r)
		case string:
			return NewNative(uintToString(l) + r)
		default:
			LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
		}

	default:
		LangError(ErrorType, "cannot add %s and %s", TypeName(left), TypeName(right))
	}

	return Value{}
}

func (vm *VM) getGlobalByName(name string) (Value, bool) {
	slot, exists := vm.globalNames[name]
	if !exists {
		return Value{}, false
	}

	return vm.globals[slot], true
}

func (vm *VM) setGlobal(slot int, value Value) {
	if slot >= len(vm.globals) {
		newSize := slot + 1

		if newSize < len(vm.globals)*2 {
			newSize = len(vm.globals) * 2
		}
		newGlobals := make([]Value, newSize)
		copy(newGlobals, vm.globals)
		vm.globals = newGlobals
	}
	vm.globals[slot] = value
}

func (vm *VM) getGlobal(slot int) Value {
	if slot < 0 || slot >= len(vm.globals) {
		return NewUndefined()
	}
	return vm.globals[slot]
}

func (vm *VM) setGlobalByName(name string, value Value) {
	slot, exists := vm.globalNames[name]
	if !exists {
		return
	}

	vm.globals[slot] = value
}

func (vm *VM) canAccessField(object ObjectValue, field string) bool {
	className, isClass := object["__class"]
	if !isClass {
		return true
	}
	privateFields := object["__privateFields"].Value.(map[string]bool)
	if _, fieldIsPrivate := privateFields[field]; fieldIsPrivate {
		return vm.currentMethodClass() == className.Value.(string)
	}

	return true
}

func (vm *VM) canAccessMethod(object ObjectValue, method string) bool {
	className, isClass := object["__class"]
	if !isClass {
		return true
	}
	privateMethods := object["__privateMethods"].Value.(map[string]bool)
	if _, methodIsPrivate := privateMethods[method]; methodIsPrivate {
		return vm.currentMethodClass() == className.Value.(string)
	}

	return true
}

func (vm *VM) getProperty(objectValue Value, name string, safe bool) Value {
	if safe && isNullish(objectValue) {
		return NewUndefined()
	}

	if ns, ok := objectValue.Value.(NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			if safe {
				return NewUndefined()
			}
			vm.nameError("namespace %s has no member: %s", ns.Name, name)
		}

		if ref, ok := value.Value.(NamespaceMemberRef); ok {
			slot, exists := vm.globalNames[ref.GlobalName]
			if !exists {
				if safe {
					return NewUndefined()
				}
				vm.nameError("undefined namespace global: %s", ref.GlobalName)
			}
			return vm.globals[slot]
		}

		return value
	}

	if ns, ok := objectValue.Value.(*NamespaceValue); ok {
		value, exists := ns.Members[name]
		if !exists {
			if safe {
				return NewUndefined()
			}
			vm.nameError("namespace %s has no member: %s", ns.Name, name)
		}

		if ref, ok := value.Value.(NamespaceMemberRef); ok {
			slot, exists := vm.globalNames[ref.GlobalName]
			if !exists {
				if safe {
					return NewUndefined()
				}
				vm.nameError("undefined namespace global: %s", ref.GlobalName)
			}
			return vm.globals[slot]
		}

		return value
	}

	object, ok := objectValue.Value.(ObjectValue)
	if !ok {
		if safe {
			return NewUndefined()
		}
		vm.typeError("expected object, got %s", TypeName(objectValue))
	}

	value, exists := object[name]
	if !exists {
		if safe {
			return NewUndefined()
		}
		vm.nameError("object has no property: %s", name)
	}

	return value
}

func (vm *VM) callClassWithArgs(class Class, args []Value) {
	object := ObjectValue{
		"__class":          NewNative(class.Name),
		"__constFields":    NewNative(map[string]bool{}),
		"__privateFields":  NewNative(map[string]bool{}),
		"__privateMethods": NewNative(map[string]bool{}),
	}

	constFields, _ := object["__constFields"].Value.(map[string]bool)
	privateFields, _ := object["__privateFields"].Value.(map[string]bool)
	privateMethods, _ := object["__privateMethods"].Value.(map[string]bool)

	for methodName, functionName := range class.Methods {
		object[methodName] = NewNative(FunctionValue{
			Name: functionName,
		})

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
			vm.fatalError(ErrorName, "undefined init function: %s", initName)
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

		setCellValue(frame.locals[0], NewNative(object))
		frame.constants[0] = true

		for i, arg := range args {
			setCellValue(frame.locals[i+1], arg)
			frame.constants[i+1] = false
		}

		vm.frames = append(vm.frames, frame)

		for len(vm.frames) > frameDepthBefore {
			if vm.step() {
				vm.fatalError(ErrorRuntime, "program halted while running constructor")
			}
		}

		if vm.top > 0 {
			vm.pop()
		}
	}

	vm.push(NewNative(object))
}

func (vm *VM) callClassByName(name string, args []Value) {
	class, exists := vm.classes[name]
	if !exists {
		vm.fatalError(ErrorName, "undefined class: %s", name)
	}

	vm.callClassWithArgs(class, args)
}

func (vm *VM) stackTrace() string {
	lines := []string{}

	for i := len(vm.nativeFrames) - 1; i >= 0; i-- {
		frame := vm.nativeFrames[i]

		location := ""
		if frame.File != "" && frame.Line > 0 {
			location = fmt.Sprintf(" (%s:%d", frame.File, frame.Line)

			if frame.Column > 0 {
				location += fmt.Sprintf(":%d", frame.Column)
			}

			location += ")"
		}

		lines = append(lines, "  at "+frame.Name+location)
	}

	if len(vm.frames) == 0 {
		location := ""

		var instr Instruction
		ip := vm.ip - 1
		if ip >= 0 && ip < len(vm.mainInstructions) {
			instr = vm.mainInstructions[ip]
		}

		if instr.File != "" && instr.Line > 0 {
			location = fmt.Sprintf(" (%s:%d", instr.File, instr.Line)

			if instr.Column > 0 {
				location += fmt.Sprintf(":%d", instr.Column)
			}

			location += ")"
		}

		lines = append(lines, "  at <main>"+location)
		return strings.Join(lines, "\n")
	}

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

func (vm *VM) typeError(format string, args ...any) {
	vm.fatalError(ErrorType, format, args...)
}

func (vm *VM) nameError(format string, args ...any) {
	vm.fatalError(ErrorName, format, args...)
}

func (vm *VM) internalError(format string, args ...any) {
	vm.fatalError(ErrorInternal, format, args...)
}

func (vm *VM) fatalError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)

	trace := vm.stackTrace()
	if trace != "" {
		message += "\n\nStack trace:\n" + trace
	}

	panic(LangErrorType{
		Kind:    kind,
		Message: message,
	})
}

func (vm *VM) runtimeError(kind ErrorKind, format string, args ...any) {
	message := fmt.Sprintf(format, args...)

	errObj := ObjectValue{
		"kind":    NewNative(string(kind)),
		"message": NewNative(message),
	}

	vm.throwValue(NewNative(errObj))
}

func (vm *VM) isInstanceOf(value Value, className string) bool {
	object, ok := value.Value.(ObjectValue)
	if !ok {
		return false
	}

	return vm.objectIsOrEmbedsClass(object, className)
}

func (vm *VM) objectIsOrEmbedsClass(object ObjectValue, className string) bool {
	currentClassValue, ok := object["__class"]
	if ok {
		currentClassName, ok := currentClassValue.Value.(string)
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

					embeddedObject, ok := fieldValue.Value.(ObjectValue)
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

		for i := 0; i < fixedCount; i++ {
			arg := vm.stack[start+i]
			param := fn.Params[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
			frame.localTypes[i] = param.TypeHint

			vm.stack[start+i] = Value{}
		}

		restParam := fn.Params[fixedCount]
		rest := &ArrayValue{
			Elements: make([]Value, 0, argCount-fixedCount),
		}

		for i := fixedCount; i < argCount; i++ {
			arg := vm.stack[start+i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, restParam.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"function %s rest parameter %s expected %s, got %s%s",
						fn.Name,
						restParam.Name,
						restParam.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			rest.Elements = append(rest.Elements, arg)
			vm.stack[start+i] = Value{}
		}

		setCellValue(frame.locals[fixedCount], NewNative(rest))
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

			if !param.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
			frame.localTypes[i] = param.TypeHint

			vm.stack[start+i] = Value{}
		}
	} else {
		for i := 0; i < argCount; i++ {
			setCellValue(frame.locals[i], vm.stack[start+i])
			frame.constants[i] = false

			vm.stack[start+i] = Value{}
		}
	}

	vm.top = start
	vm.frames = append(vm.frames, frame)
}

func isNullish(value Value) bool {
	if value.IsInt {
		return false
	}
	switch value.Value.(type) {
	case NullValue, UndefinedValue:
		return true
	default:
		return false
	}
}

func (vm *VM) throwValue(value Value) {
	errorObject := makeErrorObject(value)

	if len(vm.tryHandlers) == 0 {
		var rawMsg any
		if errorObject["message"].IsInt {
			rawMsg = errorObject["message"].AsInt
		} else {
			rawMsg = errorObject["message"].Value
		}

		var rawKind any
		if errorObject["kind"].IsInt {
			rawKind = errorObject["kind"].AsInt
		} else {
			rawKind = errorObject["kind"].Value
		}

		message := valueToString(NewNative(rawMsg))
		kind := valueToString(NewNative(rawKind))

		trace := vm.stackTrace()

		vm.runDefersAboveDepth(0)

		panic(LangErrorType{
			Kind:    ErrorKind(kind),
			Message: message + "\n\nStack trace:\n" + trace,
		})
	}

	handler := vm.tryHandlers[len(vm.tryHandlers)-1]
	vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

	vm.runDefersAboveDepth(handler.FrameDepth)

	for len(vm.frames) > handler.FrameDepth {
		vm.frames = vm.frames[:len(vm.frames)-1]
	}

	if handler.IsLocal {
		if handler.FrameDepth == 0 {
			vm.fatalError(ErrorInternal, "local catch handler has no frame")
		}

		frame := vm.frames[handler.FrameDepth-1]

		if handler.Slot < 0 || handler.Slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "catch local slot out of range")
		}

		setCellValue(frame.locals[handler.Slot], NewNative(errorObject))
		frame.constants[handler.Slot] = false
	} else {
		vm.setGlobal(handler.Slot, NewNative(errorObject))
		vm.globalConstants[handler.Name] = false
	}

	if handler.FrameDepth == 0 {
		vm.ip = handler.CatchIP
	} else {
		vm.frames[handler.FrameDepth-1].ip = handler.CatchIP
	}
}

func makeErrorObject(value Value) ObjectValue {
	var raw any
	if value.IsInt {
		raw = value.AsInt
	} else {
		raw = value.Value
	}

	switch err := raw.(type) {
	case ErrorValue:
		return ObjectValue{
			"kind":    NewNative(err.Kind),
			"message": NewNative(err.Message),
		}

	case *ErrorValue:
		return ObjectValue{
			"kind":    NewNative(err.Kind),
			"message": NewNative(err.Message),
		}

	case ObjectValue:
		return err

	case string:
		return ObjectValue{
			"kind":    NewNative("Error"),
			"message": NewNative(err),
		}

	default:
		return ObjectValue{
			"kind":    NewNative("Error"),
			"message": NewNative(valueToString(value)),
		}
	}
}

func (vm *VM) callFunctionValueWithArgs(fnValue FunctionValue, args []Value) {
	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		vm.fatalError(ErrorName, "undefined function: %s", fnValue.Name)
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
				"function %s expects %d arguments, got %d",
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
			vm.fatalError(ErrorInternal, "capture slot out of range in function value: %d", slot)
		}

		frame.locals[slot] = cell
	}

	if isVariadic {
		fixedCount := expected - 1

		for i := range fixedCount {
			setCellValue(frame.locals[i], args[i])
			frame.constants[i] = false
		}

		rest := &ArrayValue{
			Elements: make([]Value, 0, len(args)-fixedCount),
		}

		for i := fixedCount; i < len(args); i++ {
			rest.Elements = append(rest.Elements, args[i])
		}

		setCellValue(frame.locals[fixedCount], NewNative(rest))
		frame.constants[fixedCount] = false
	} else {
		for i, arg := range args {
			param := fn.Params[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"function %s parameter %s expected %s, got %s%s",
						fn.Name,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[i], arg)
			frame.constants[i] = false
			frame.localTypes[i] = param.TypeHint
		}
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) runFunctionToCompletion(fn Function, args []Value) Value {
	vm.callFunctionDirect(fn, args)

	targetDepth := len(vm.frames) - 1

	for len(vm.frames) > targetDepth {
		if vm.step() {
			break
		}
	}

	return vm.pop()
}

func (vm *VM) runFrameToCompletion(frame *Frame) Value {
	vm.frames = append(vm.frames, frame)

	targetDepth := len(vm.frames) - 1

	for len(vm.frames) > targetDepth {
		if vm.step() {
			break
		}
	}

	return vm.pop()
}

func (vm *VM) callFunctionDirect(fn Function, args []Value) {
	args = vm.applyDefaultArgs(fn, args, 0, "function "+fn.Name)

	frame := vm.getFrame(fn)

	for i, arg := range args {
		param := fn.Params[i]

		if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
			if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"function %s parameter %s expected %s, got %s%s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
					reason,
				)
			}
		}

		setCellValue(frame.locals[i], arg)
		frame.constants[i] = false
		frame.localTypes[i] = param.TypeHint
	}

	vm.frames = append(vm.frames, frame)
}

func (vm *VM) callFunctionValue(fnValue FunctionValue, args []Value) Value {
	frameDepthBefore := len(vm.frames)
	stackDepthBefore := vm.top

	vm.callFunctionValueWithArgs(fnValue, args)

	for len(vm.frames) > frameDepthBefore {
		if vm.step() {
			vm.fatalError(ErrorRuntime, "program halted while running function value")
		}
	}

	if vm.top <= stackDepthBefore {
		return NewUndefined()
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

func (vm *VM) runDefersAboveDepth(targetDepth int) {
	for len(vm.deferHandlers) > 0 {
		handler := vm.deferHandlers[len(vm.deferHandlers)-1]

		if handler.FrameDepth > targetDepth {
			vm.deferHandlers = vm.deferHandlers[:len(vm.deferHandlers)-1]

			vm.callFunctionValue(handler.Function, nil)
		} else {
			break
		}
	}
}

func (vm *VM) step() bool {
	instr := vm.fetchInstruction()

	if len(vm.frames) > 0 {
		vm.lastFunctionName = vm.frames[len(vm.frames)-1].function.Name
	} else {
		vm.lastFunctionName = "<main>"
	}

	switch instr.Op {
	case OP_ADD_LOCAL_LOCAL_STORE:
		info := instr.Value.(AddLocalLocalStoreInfo)
		frame := vm.frames[len(vm.frames)-1]

		valA := frame.locals[info.SlotA]
		valB := frame.locals[info.SlotB]

		if valA.IsInt && valB.IsInt {
			frame.locals[info.DestSlot].Int = valA.Int + valB.Int
			frame.locals[info.DestSlot].IsInt = true
		} else {
			vm.fatalError(ErrorType, "Optimized addition expects integers")
		}
	case OP_JUMP_LOCAL_GT_LOCAL:
		info := instr.Value.(JumpLocalGTLocalInfo)
		frame := vm.frames[len(vm.frames)-1]

		valA := frame.locals[info.SlotA]
		valB := frame.locals[info.SlotB]

		if valA.IsInt && valB.IsInt {
			if valA.Int > valB.Int {
				frame.ip = info.Target
			}
		} else {
			vm.fatalError(ErrorType, "Loop condition expects integers")
		}
	case OP_CALL_DIRECT_SUB_CONST:
		info := instr.Value.(CallDirectSubConstInfo)

		currentFrame := vm.frames[len(vm.frames)-1]
		if info.Slot < 0 || info.Slot >= len(currentFrame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_CALL_DIRECT_SUB_CONST")
		}
		cell := currentFrame.locals[info.Slot]
		if cell == nil || !cell.IsInt {
			vm.fatalError(ErrorType, "expected int in local slot for math optimization")
		}

		finalArgValue := cell.Int - info.SubValue

		fn, exists := vm.functions[info.FnName]
		if !exists {
			vm.fatalError(ErrorRuntime, "undefined function: %s", info.FnName)
		}

		newFrame := vm.getFrame(fn)

		if len(newFrame.locals) > 0 {
			setCellValue(newFrame.locals[0], NewInt(finalArgValue))
			newFrame.constants[0] = false
		}

		vm.frames = append(vm.frames, newFrame)

		return false

	case OP_JUMP_LOCAL_GT_CONST:
		info := instr.Value.(JumpLocalGTConstInfo)
		frame := vm.frames[len(vm.frames)-1]
		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_LOCAL_GT_CONST")
		}
		cell := frame.locals[info.Slot]
		if cell == nil {
			vm.fatalError(ErrorInternal, "local cell is nil in OP_JUMP_LOCAL_GT_CONST")
		}

		if cell.IsInt {
			if cell.Int > info.Value {
				if len(vm.frames) == 0 {
					vm.ip = info.Target
				} else {
					vm.frames[len(vm.frames)-1].ip = info.Target
				}
			}
			break
		}

		shouldJump := false
		var val any
		if cell.Value.IsInt {
			val = cell.Value.AsInt
		} else {
			val = cell.Value.Value
		}

		switch v := val.(type) {
		case int:
			shouldJump = v > info.Value
		case int64:
			shouldJump = v > int64(info.Value)
		case float64:
			shouldJump = v > float64(info.Value)
		case float32:
			shouldJump = v > float32(info.Value)
		default:
			vm.fatalError(ErrorType, "cannot compare %s and number", TypeName(cell.Value))
		}

		if shouldJump {
			if len(vm.frames) == 0 {
				vm.ip = info.Target
			} else {
				vm.frames[len(vm.frames)-1].ip = info.Target
			}
		}
	case OP_JUMP_LOCAL_GE_LOCAL:
		info := instr.Value.(JumpLocalGELocalInfo)

		frame := vm.frames[len(vm.frames)-1]

		leftCell := frame.locals[info.LeftSlot]
		rightCell := frame.locals[info.RightSlot]

		if leftCell == nil || rightCell == nil {
			vm.fatalError(ErrorInternal, "nil local in OP_JUMP_LOCAL_GE_LOCAL")
		}

		if leftCell.IsInt && rightCell.IsInt {
			if leftCell.Int >= rightCell.Int {
				frame.ip = info.Target
			}
			break
		}

		var leftVal any
		if leftCell.Value.IsInt {
			leftVal = leftCell.Value.AsInt
		} else {
			leftVal = leftCell.Value.Value
		}

		var rightVal any
		if rightCell.Value.IsInt {
			rightVal = rightCell.Value.AsInt
		} else {
			rightVal = rightCell.Value.Value
		}

		shouldJump := false

		switch l := leftVal.(type) {
		case int:
			switch r := rightVal.(type) {
			case int:
				shouldJump = l >= r
			case int64:
				shouldJump = int64(l) >= r
			case float64:
				shouldJump = float64(l) >= r
			default:
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(leftCell.Value), TypeName(rightCell.Value))
			}

		case int64:
			switch r := rightVal.(type) {
			case int:
				shouldJump = l >= int64(r)
			case int64:
				shouldJump = l >= r
			case float64:
				shouldJump = float64(l) >= r
			default:
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(leftCell.Value), TypeName(rightCell.Value))
			}

		case float64:
			shouldJump = l >= asFloat64(rightCell.Value)

		default:
			vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(leftCell.Value), TypeName(rightCell.Value))
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO:
		info := instr.Value.(JumpModLocalLocalNotZeroInfo)

		frame := vm.frames[len(vm.frames)-1]

		leftCell := frame.locals[info.LeftSlot]
		rightCell := frame.locals[info.RightSlot]

		if leftCell == nil || rightCell == nil {
			vm.fatalError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_LOCAL_NOT_ZERO")
		}

		if leftCell.IsInt && rightCell.IsInt {
			if rightCell.Int == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}
			if leftCell.Int%rightCell.Int != 0 {
				frame.ip = info.Target
			}
			break
		}

		var leftVal any
		if leftCell.Value.IsInt {
			leftVal = leftCell.Value.AsInt
		} else {
			leftVal = leftCell.Value.Value
		}

		shouldJump := false

		switch l := leftVal.(type) {
		case int:
			r := asInt(rightCell.Value)
			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = l%r != 0

		case int64:
			r := int64(asInt(rightCell.Value))
			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = l%r != 0

		case float64:
			r := asFloat64(rightCell.Value)
			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}
			shouldJump = math.Mod(l, r) != 0

		default:
			vm.fatalError(ErrorType, "cannot modulo %s and %s", TypeName(leftCell.Value), TypeName(rightCell.Value))
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO:
		info := instr.Value.(JumpModLocalConstNotZeroInfo)

		frame := vm.frames[len(vm.frames)-1]

		if info.LeftSlot < 0 || info.LeftSlot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
		}
		if info.Right == 0 {
			vm.fatalError(ErrorRuntime, "cannot modulo by zero")
		}

		leftCell := frame.locals[info.LeftSlot]
		if leftCell == nil {
			vm.fatalError(ErrorInternal, "nil local in OP_JUMP_MOD_LOCAL_CONST_NOT_ZERO")
		}

		shouldJump := false

		if leftCell.IsInt {
			shouldJump = leftCell.Int%info.Right != 0
		} else {
			var leftVal any
			if leftCell.Value.IsInt {
				leftVal = leftCell.Value.AsInt
			} else {
				leftVal = leftCell.Value.Value
			}
			switch l := leftVal.(type) {
			case int:
				shouldJump = l%info.Right != 0
			case int64:
				shouldJump = l%int64(info.Right) != 0
			case float64:
				shouldJump = math.Mod(l, float64(info.Right)) != 0
			case float32:
				shouldJump = math.Mod(float64(l), float64(info.Right)) != 0
			default:
				vm.fatalError(ErrorType, "cannot modulo %s and number", TypeName(leftCell.Value))
			}
		}

		if shouldJump {
			frame.ip = info.Target
		}

	case OP_ADD_ASSIGN_LOCAL:
		info := instr.Value.(AssignLocalInfo)

		frame := vm.frames[len(vm.frames)-1]

		if info.TargetSlot < 0 || info.TargetSlot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "target local slot out of range in OP_ADD_ASSIGN_LOCAL")
		}

		if info.SourceSlot < 0 || info.SourceSlot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "source local slot out of range in OP_ADD_ASSIGN_LOCAL")
		}

		targetCell := frame.locals[info.TargetSlot]
		sourceCell := frame.locals[info.SourceSlot]

		if targetCell == nil || sourceCell == nil {
			vm.fatalError(ErrorInternal, "nil local cell in OP_ADD_ASSIGN_LOCAL")
		}

		if frame.constants[info.TargetSlot] {
			vm.fatalError(ErrorConst, "cannot assign to constant local")
		}

		if targetCell.IsInt && sourceCell.IsInt {
			targetCell.Int += sourceCell.Int
			break
		}

		var targetVal any
		if targetCell.Value.IsInt {
			targetVal = targetCell.Value.AsInt
		} else {
			targetVal = targetCell.Value.Value
		}

		var sourceVal any
		if sourceCell.Value.IsInt {
			sourceVal = sourceCell.Value.AsInt
		} else {
			sourceVal = sourceCell.Value.Value
		}

		switch target := targetVal.(type) {
		case int:
			switch source := sourceVal.(type) {
			case int:
				targetCell.Int = target + source
				targetCell.IsInt = true
			case int64:
				setCellValue(targetCell, NewNative(int64(target)+source))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)+source))
			case float32:
				setCellValue(targetCell, NewNative(float32(target)+source))
			default:
				vm.fatalError(ErrorType, "cannot add %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case int64:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target+int64(source)))
			case int64:
				setCellValue(targetCell, NewNative(target+source))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)+source))
			case float32:
				setCellValue(targetCell, NewNative(float32(target)+source))
			default:
				vm.fatalError(ErrorType, "cannot add %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case float64:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target+float64(source)))
			case int64:
				setCellValue(targetCell, NewNative(target+float64(source)))
			case float64:
				setCellValue(targetCell, NewNative(target+source))
			case float32:
				setCellValue(targetCell, NewNative(target+float64(source)))
			default:
				vm.fatalError(ErrorType, "cannot add %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case float32:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target+float32(source)))
			case int64:
				setCellValue(targetCell, NewNative(target+float32(source)))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)+source))
			case float32:
				setCellValue(targetCell, NewNative(target+source))
			default:
				vm.fatalError(ErrorType, "cannot add %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case string:
			setCellValue(targetCell, NewNative(target+valueToString(sourceCell.Value)))

		default:
			vm.fatalError(ErrorType, "cannot add to %s", TypeName(targetCell.Value))
		}

	case OP_SUB_ASSIGN_LOCAL:
		info := instr.Value.(AssignLocalInfo)

		frame := vm.frames[len(vm.frames)-1]

		if info.TargetSlot < 0 || info.TargetSlot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "target local slot out of range in OP_SUB_ASSIGN_LOCAL")
		}

		if info.SourceSlot < 0 || info.SourceSlot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "source local slot out of range in OP_SUB_ASSIGN_LOCAL")
		}

		targetCell := frame.locals[info.TargetSlot]
		sourceCell := frame.locals[info.SourceSlot]

		if targetCell == nil || sourceCell == nil {
			vm.fatalError(ErrorInternal, "nil local cell in OP_SUB_ASSIGN_LOCAL")
		}

		if frame.constants[info.TargetSlot] {
			vm.fatalError(ErrorConst, "cannot assign to constant local")
		}

		if targetCell.IsInt && sourceCell.IsInt {
			targetCell.Int -= sourceCell.Int
			break
		}

		var targetVal any
		if targetCell.Value.IsInt {
			targetVal = targetCell.Value.AsInt
		} else {
			targetVal = targetCell.Value.Value
		}

		var sourceVal any
		if sourceCell.Value.IsInt {
			sourceVal = sourceCell.Value.AsInt
		} else {
			sourceVal = sourceCell.Value.Value
		}

		switch target := targetVal.(type) {
		case int:
			switch source := sourceVal.(type) {
			case int:
				targetCell.Int = target - source
				targetCell.IsInt = true
			case int64:
				setCellValue(targetCell, NewNative(int64(target)-source))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)-source))
			case float32:
				setCellValue(targetCell, NewNative(float32(target)-source))
			default:
				vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case int64:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target-int64(source)))
			case int64:
				setCellValue(targetCell, NewNative(target-source))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)-source))
			case float32:
				setCellValue(targetCell, NewNative(float32(target)-source))
			default:
				vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case float64:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target-float64(source)))
			case int64:
				setCellValue(targetCell, NewNative(target-float64(source)))
			case float64:
				setCellValue(targetCell, NewNative(target-source))
			case float32:
				setCellValue(targetCell, NewNative(target-float64(source)))
			default:
				vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		case float32:
			switch source := sourceVal.(type) {
			case int:
				setCellValue(targetCell, NewNative(target-float32(source)))
			case int64:
				setCellValue(targetCell, NewNative(target-float32(source)))
			case float64:
				setCellValue(targetCell, NewNative(float64(target)-source))
			case float32:
				setCellValue(targetCell, NewNative(target-source))
			default:
				vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(targetCell.Value), TypeName(sourceCell.Value))
			}

		default:
			vm.fatalError(ErrorType, "cannot subtract to %s", TypeName(targetCell.Value))
		}

	case OP_JUMP_LOCAL_GE_CONST:
		info := instr.Value.(JumpLocalGEConstInfo)

		frame := vm.frames[len(vm.frames)-1]

		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_JUMP_LOCAL_GE_CONST")
		}

		cell := frame.locals[info.Slot]
		if cell == nil {
			vm.fatalError(ErrorInternal, "local cell is nil in OP_JUMP_LOCAL_GE_CONST")
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
		var val any
		if cell.Value.IsInt {
			val = cell.Value.AsInt
		} else {
			val = cell.Value.Value
		}

		switch v := val.(type) {
		case int:
			shouldJump = v >= info.Value

		case int64:
			shouldJump = v >= int64(info.Value)

		case float64:
			shouldJump = v >= float64(info.Value)

		case float32:
			shouldJump = v >= float32(info.Value)

		default:
			vm.fatalError(ErrorType, "cannot compare %s and number", TypeName(cell.Value))
		}

		if shouldJump {
			if len(vm.frames) == 0 {
				vm.ip = info.Target
			} else {
				vm.frames[len(vm.frames)-1].ip = info.Target
			}
		}

	case OP_STRING_JOIN:
		count := instr.IntArg

		if vm.top < count {
			vm.handleUnderflow()
		}

		start := vm.top - count

		var builder strings.Builder

		for i := start; i < vm.top; i++ {
			builder.WriteString(valueToString(vm.stack[i]))
			vm.stack[i] = Value{}
		}

		vm.top = start
		vm.push(NewNative(builder.String()))

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

		if fn.Async {
			args := vm.popArgs(info.ArgCount)
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

				result := taskVM.runFunctionToCompletion(fn, args)

				task.Done <- TaskResult{
					Value: result,
				}
			}()

			vm.push(NewNative(task))
		} else {
			vm.callFunctionDirectFromStack(fn, info.ArgCount, "function "+info.Name)
		}

		return false

	case OP_OBJECT_IN:
		keyValue := vm.popFast()
		objectValue := asObject(vm.popFast(), vm)

		var rawKey any
		if keyValue.IsInt {
			rawKey = keyValue.AsInt
		} else {
			rawKey = keyValue.Value
		}

		found := false
		_, found = objectValue[rawKey]

		vm.push(NewNative(found))

	case OP_INSTANCEOF:
		classValue := vm.popFast()
		objectValue := vm.popFast()

		var className string
		var rawClass any
		if classValue.IsInt {
			rawClass = classValue.AsInt
		} else {
			rawClass = classValue.Value
		}

		switch c := rawClass.(type) {
		case Class:
			className = c.Name

		case *Class:
			className = c.Name

		default:
			vm.fatalError(ErrorType, "right side of instanceof must be class, got %s", TypeName(classValue))
		}

		vm.push(NewNative(vm.isInstanceOf(objectValue, className)))

	case OP_AWAIT:
		value := vm.popFast()

		task, ok := value.Value.(*NativeTaskValue)

		if !ok {
			vm.push(value)
			break
		}

		result := <-task.Done

		if result.Error != nil {
			panic(result.Error)
		}

		vm.push(result.Value)

	case OP_LOCK_MUTEX:
		value := vm.popFast()

		mutex, ok := value.Value.(*NativeMutexValue)
		if !ok {
			vm.fatalError(ErrorType, "lock mutex expects mutex, got %s", TypeName(value))
		}

		mutex.Lock()

		vm.push(NewUndefined())

	case OP_UNLOCK_MUTEX:
		value := vm.popFast()

		mutex, ok := value.Value.(*NativeMutexValue)
		if !ok {
			vm.fatalError(ErrorType, "unlock mutex expects mutex, got %s", TypeName(value))
		}

		mutex.Unlock()

		vm.push(NewUndefined())

	case OP_DEFER:
		value := vm.popFast()

		fn, ok := value.Value.(FunctionValue)
		if !ok {
			vm.fatalError(ErrorType, "defer expects function, got %s", TypeName(value))
		}

		for _, handler := range vm.deferHandlers {
			if handler.FrameDepth == len(vm.frames) {
				vm.fatalError(ErrorRuntime, "multiple defer statements are not permitted within the same function scope")
			}
		}

		vm.deferHandlers = append(vm.deferHandlers, DeferHandler{
			Function:   fn,
			FrameDepth: len(vm.frames),
		})

		vm.push(NewUndefined())

	case OP_SPAWN:
		value := vm.popFast()

		fn, ok := value.Value.(FunctionValue)
		if !ok {
			vm.fatalError(ErrorType, "spawn expects function, got %s", TypeName(value))
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

		vm.push(NewNative(task))

	case OP_TYPEOF:
		value := vm.popFast()
		vm.push(NewNative(TypeName(value)))

	case OP_NEGATE:
		value := vm.popFast()

		if value.IsInt {
			vm.push(NewInt(-value.AsInt))
			break
		}

		switch v := value.Value.(type) {
		case int:
			vm.push(NewInt(-v))

		case int64:
			vm.push(NewNative(-v))

		case float64:
			vm.push(NewNative(-v))

		case float32:
			vm.push(NewNative(-v))

		default:
			vm.fatalError(ErrorType, "cannot negate %s", TypeName(value))
		}
	case OP_CLOSURE:
		info := instr.Value.(ClosureInfo)

		captures := map[int]*Cell{}

		if len(info.Captures) > 0 {
			if len(vm.frames) == 0 {
				vm.fatalError(ErrorInternal, "closure has captures but no current function frame")
			}

			frame := vm.currentFrame()
			frame.hasEscapedLocals = true

			for _, capture := range info.Captures {
				if capture.OuterSlot < 0 || capture.OuterSlot >= len(frame.locals) {
					vm.fatalError(
						ErrorInternal,
						"capture slot out of range: function=%s outerSlot=%d locals=%d",
						frame.function.Name,
						capture.OuterSlot,
						len(frame.locals),
					)
				}

				if frame.locals[capture.OuterSlot] == nil {
					vm.fatalError(
						ErrorInternal,
						"captured local is nil: function=%s outerSlot=%d",
						frame.function.Name,
						capture.OuterSlot,
					)
				}

				captures[capture.InnerSlot] = frame.locals[capture.OuterSlot]
			}
		}

		vm.push(NewNative(FunctionValue{
			Name:     info.Name,
			Captures: captures,
		}))

	case OP_CONST:
		var wrapped Value
		switch v := instr.Value.(type) {
		case int:
			wrapped = NewInt(v)
		case int64:
			wrapped = NewInt(int(v))
		default:
			wrapped = NewNative(v)
		}
		vm.push(wrapped)

	case OP_SET_PROPERTY:
		name := instr.Value.(string)

		value := vm.popFast()
		objectValue := vm.popFast()

		object, ok := objectValue.Value.(ObjectValue)
		if !ok {
			vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
		}

		_, isClass := object["__class"]
		if isClass {
			constFields, _ := object["__constFields"].Value.(map[string]bool)
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
		objectValue := vm.popFast()

		if isNullish(objectValue) {
			vm.push(NewUndefined())
			break
		}

		vm.callMethodResolved(info.Method, objectValue, args)

	case OP_COALESCE_JUMP:
		right := vm.popFast()
		left := vm.popFast()

		if isNullish(left) {
			vm.push(right)
		} else {
			vm.push(left)
		}

	case OP_GET_PROPERTY_SAFE:
		name := instr.Value.(string)
		objectValue := vm.popFast()
		vm.push(vm.getProperty(objectValue, name, true))

	case OP_LOAD_GLOBAL:
		var slot int
		if info, ok := instr.Value.(VariableInfo); ok {
			slot = info.Slot
		} else if name, ok := instr.Value.(string); ok {
			var exists bool

			slot, exists = vm.globalNames[name]

			if !exists {
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}
		}

		vm.push(vm.getGlobal(slot))

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
			vm.fatalError(ErrorInternal, "try handler stack underflow")
		}

		vm.tryHandlers = vm.tryHandlers[:len(vm.tryHandlers)-1]

	case OP_STORE_GLOBAL:
		info := instr.Value.(VariableInfo)
		value := vm.popFast()

		if ok, reason := CheckTypeHint(value, info.TypeHint, vm.interfaces); !ok {
			vm.fatalError(
				ErrorType,
				"variable %s expected %s, got %s%s",
				info.Name,
				info.TypeHint.Name,
				TypeName(value),
				reason,
			)
		}

		vm.mu.Lock()

		if vm.globalNames == nil {
			vm.globalNames = map[string]int{}
		}
		vm.globalNames[info.Name] = info.Slot

		vm.setGlobal(info.Slot, value)

		vm.globalConstants[info.Name] = info.Constant
		vm.globalTypes[info.Name] = info.TypeHint
		vm.mu.Unlock()

	case OP_LOAD_LOCAL:
		slot := instr.IntArg
		frame := vm.frames[len(vm.frames)-1]

		if slot < 0 || slot >= len(frame.locals) {
			vm.fatalError(
				ErrorInternal,
				"local slot out of range: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.locals[slot] == nil {
			vm.fatalError(
				ErrorInternal,
				"local slot is nil: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		vm.push(cellValue(frame.locals[slot]))

	case OP_LOAD_LOCAL_0:
		frame := vm.frames[len(vm.frames)-1]
		cell := frame.locals[0]
		if cell == nil {
			vm.fatalError(ErrorInternal, "local slot is nil: function=%s slot=0 locals=%d", frame.function.Name, len(frame.locals))
		}
		if cell.IsInt {
			vm.push(NewInt(cell.Int))
		} else {
			vm.push(cell.Value)
		}

	case OP_LOAD_LOCAL_1:
		frame := vm.frames[len(vm.frames)-1]
		cell := frame.locals[1]
		if cell.IsInt {
			vm.push(NewInt(cell.Int))
		} else {
			if cell == nil {
				vm.fatalError(ErrorInternal, "local slot is nil: function=%s slot=1 locals=%d", frame.function.Name, len(frame.locals))
			}
			vm.push(cell.Value)
		}

	case OP_LOAD_LOCAL_2:
		frame := vm.frames[len(vm.frames)-1]
		cell := frame.locals[2]
		if cell.IsInt {
			vm.push(NewInt(cell.Int))
		} else {
			if cell == nil {
				vm.fatalError(ErrorInternal, "local slot is nil: function=%s slot=2 locals=%d", frame.function.Name, len(frame.locals))
			}
			vm.push(cell.Value)
		}

	case OP_LOAD_LOCAL_3:
		frame := vm.frames[len(vm.frames)-1]
		cell := frame.locals[3]
		if cell.IsInt {
			vm.push(NewInt(cell.Int))
		} else {
			if cell == nil {
				vm.fatalError(ErrorInternal, "local slot is nil: function=%s slot=3 locals=%d", frame.function.Name, len(frame.locals))
			}
			vm.push(cell.Value)
		}

	case OP_STORE_LOCAL:
		info := instr.Value.(VariableInfo)
		value := vm.popFast()

		frame := vm.currentFrame()

		if info.Slot < 0 || info.Slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range: %d", info.Slot)
		}

		if !info.TypeHint.IsEmpty() {
			if ok, reason := CheckTypeHint(value, info.TypeHint, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"variable %s expected %s, got %s%s",
					info.Name,
					info.TypeHint.Name,
					TypeName(value),
					reason,
				)
			}
		}

		frame.locals[info.Slot] = &Cell{}
		setCellValue(frame.locals[info.Slot], value)
		frame.constants[info.Slot] = info.Constant
		frame.localTypes[info.Slot] = info.TypeHint

	case OP_ASSIGN_GLOBAL:
		value := vm.popFast()

		var slot int
		var name string

		if info, ok := instr.Value.(VariableInfo); ok {
			slot = info.Slot
			name = info.Name
		} else if s, ok := instr.Value.(string); ok {
			name = s
			var exists bool
			slot, exists = vm.globalNames[name]
			if !exists {
				vm.fatalError(ErrorName, "undefined global variable: %s", name)
			}
		} else {
			vm.fatalError(ErrorInternal, "unexpected type for OP_ASSIGN_GLOBAL: %T", instr.Value)
		}

		if vm.globalConstants[name] {
			vm.fatalError(ErrorConst, "cannot assign to constant global")
		}

		hint := vm.globalTypes[name]

		if !hint.IsEmpty() {
			if ok, reason := CheckTypeHint(value, hint, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"global %s expected %s, got %s%s",
					name,
					hint.Name,
					TypeName(value),
					reason,
				)
			}
		}

		vm.mu.Lock()
		vm.setGlobal(slot, value)
		vm.mu.Unlock()

	case OP_INC_LOCAL:
		var slot int
		intAmount := 1
		floatAmount := 1.0
		isFloat := false

		switch info := instr.Value.(type) {
		case int:
			slot = info
		case int64:
			slot = int(info)
		case IncrementInfo:
			slot = info.Slot
			intAmount = info.IntAmount
			floatAmount = info.FloatAmount
			isFloat = info.IsFloat
		default:
			vm.fatalError(ErrorInternal, "unexpected type for OP_INC_LOCAL: %T", instr.Value)
		}

		frame := vm.frames[len(vm.frames)-1]

		if slot < 0 || slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_INC_LOCAL")
		}

		cell := frame.locals[slot]

		if cell == nil {
			vm.fatalError(ErrorInternal, "local cell is nil in OP_INC_LOCAL")
		}

		if frame.constants[slot] {
			vm.fatalError(ErrorConst, "cannot assign to constant local")
		}

		if cell.IsInt && !isFloat {
			cell.Int += intAmount
			break
		}

		var rawVal any
		if cell.Value.IsInt {
			rawVal = cell.Value.AsInt
		} else {
			rawVal = cell.Value.Value
		}

		switch v := rawVal.(type) {
		case int:
			cell.Int = v + intAmount
			cell.IsInt = true

		case int64:
			setCellValue(cell, NewNative(v+int64(intAmount)))

		case float64:
			setCellValue(cell, NewNative(v+floatAmount))

		case float32:
			setCellValue(cell, NewNative(v+float32(floatAmount)))

		default:
			vm.runtimeError(ErrorType, "cannot increment %s", TypeName(cell.Value))
		}

	case OP_DEC_LOCAL:
		var slot int
		intAmount := 1
		floatAmount := 1.0
		isFloat := false

		switch info := instr.Value.(type) {
		case int:
			slot = info
		case int64:
			slot = int(info)
		case DecrementInfo:
			slot = info.Slot
			intAmount = info.IntAmount
			floatAmount = info.FloatAmount
			isFloat = info.IsFloat
		default:
			vm.fatalError(ErrorInternal, "unexpected type for OP_DEC_LOCAL: %T", instr.Value)
		}

		frame := vm.frames[len(vm.frames)-1]

		if slot < 0 || slot >= len(frame.locals) {
			vm.fatalError(ErrorInternal, "local slot out of range in OP_DEC_LOCAL")
		}

		cell := frame.locals[slot]

		if cell == nil {
			vm.fatalError(ErrorInternal, "local cell is nil in OP_DEC_LOCAL")
		}

		if frame.constants[slot] {
			vm.fatalError(ErrorConst, "cannot assign to constant local")
		}

		if cell.IsInt && !isFloat {
			cell.Int -= intAmount
			break
		}

		var rawVal any
		if cell.Value.IsInt {
			rawVal = cell.Value.AsInt
		} else {
			rawVal = cell.Value.Value
		}

		switch v := rawVal.(type) {
		case int:
			cell.Int = v - intAmount
			cell.IsInt = true

		case int64:
			setCellValue(cell, NewNative(v-int64(intAmount)))

		case float64:
			setCellValue(cell, NewNative(v-floatAmount))

		case float32:
			setCellValue(cell, NewNative(v-float32(floatAmount)))

		default:
			vm.runtimeError(ErrorType, "cannot decrement %s", TypeName(cell.Value))
		}

	case OP_INC_GLOBAL:
		var name string
		intAmount := 1
		floatAmount := 1.0
		isFloat := false

		switch info := instr.Value.(type) {
		case string:
			name = info
		case IncrementInfo:
			name = info.Name
			intAmount = info.IntAmount
			floatAmount = info.FloatAmount
			isFloat = info.IsFloat
		default:
			vm.fatalError(ErrorInternal, "unexpected type for OP_INC_GLOBAL: %T", instr.Value)
		}

		if vm.globalConstants[name] {
			vm.fatalError(ErrorConst, "cannot increment constant global")
		}

		value, exists := vm.getGlobalByName(name)
		if !exists {
			vm.fatalError(ErrorName, "undefined global variable: %s", name)
		}

		var rawVal any
		if value.IsInt {
			rawVal = value.AsInt
		} else {
			rawVal = value.Value
		}

		vm.mu.Lock()
		switch v := rawVal.(type) {
		case int:
			if isFloat {
				vm.setGlobalByName(name, NewNative(float64(v)+floatAmount))
			} else {
				vm.setGlobalByName(name, NewInt(v+intAmount))
			}

		case float64:
			if isFloat {
				vm.setGlobalByName(name, NewNative(v+floatAmount))
			} else {
				vm.setGlobalByName(name, NewNative(v+float64(intAmount)))
			}

		default:
			vm.fatalError(ErrorType, "cannot increment %s", TypeName(value))
		}
		vm.mu.Unlock()

	case OP_DEC_GLOBAL:
		var name string
		intAmount := 1
		floatAmount := 1.0
		isFloat := false

		switch info := instr.Value.(type) {
		case string:
			name = info
		case DecrementInfo:
			name = info.Name
			intAmount = info.IntAmount
			floatAmount = info.FloatAmount
			isFloat = info.IsFloat
		default:
			vm.fatalError(ErrorInternal, "unexpected type for OP_DEC_GLOBAL: %T", instr.Value)
		}

		if vm.globalConstants[name] {
			vm.fatalError(ErrorConst, "cannot decrement constant global")
		}

		value, exists := vm.getGlobalByName(name)
		if !exists {
			vm.fatalError(ErrorName, "undefined global variable: %s", name)
		}

		var rawVal any
		if value.IsInt {
			rawVal = value.AsInt
		} else {
			rawVal = value.Value
		}

		vm.mu.Lock()
		switch v := rawVal.(type) {
		case int:
			if isFloat {
				vm.setGlobalByName(name, NewNative(float64(v)-floatAmount))
			} else {
				vm.setGlobalByName(name, NewInt(v-intAmount))
			}

		case float64:
			if isFloat {
				vm.setGlobalByName(name, NewNative(v-floatAmount))
			} else {
				vm.setGlobalByName(name, NewNative(v-float64(intAmount)))
			}

		default:
			vm.fatalError(ErrorType, "cannot decrement %s", TypeName(value))
		}
		vm.mu.Unlock()

	case OP_ASSIGN_LOCAL:
		slot := instr.IntArg
		value := vm.popFast()

		frame := vm.frames[len(vm.frames)-1]

		if slot < 0 || slot >= len(frame.locals) {
			vm.fatalError(
				ErrorInternal,
				"local slot out of range: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.locals[slot] == nil {
			vm.fatalError(
				ErrorInternal,
				"local slot is nil during assignment: function=%s slot=%d locals=%d",
				frame.function.Name,
				slot,
				len(frame.locals),
			)
		}

		if frame.constants[slot] {
			vm.fatalError(ErrorConst, "cannot assign to constant local")
		}

		hint := frame.localTypes[slot]

		if !hint.IsEmpty() {
			if ok, reason := CheckTypeHint(value, hint, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"local variable expected %s, got %s%s",
					hint.Name,
					TypeName(value),
					reason,
				)
			}
		}

		setCellValue(frame.locals[slot], value)

	case OP_MUL_LOCAL_CONST:
		info := instr.Value.(LocalConstInfo)
		frame := vm.frames[len(vm.frames)-1]
		vm.push(multiplyByInt(frameLocalValue(frame, info.Slot, "OP_MUL_LOCAL_CONST"), info.Value))

	case OP_ADD:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewInt(left.AsInt + right.AsInt))
		} else {
			vm.push(addValues(left, right))
		}

	case OP_SUB:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewInt(left.AsInt - right.AsInt))
			break
		}

		var leftVal any
		if left.IsInt {
			leftVal = left.AsInt
		} else {
			leftVal = left.Value
		}

		var rightVal any
		if right.IsInt {
			rightVal = right.AsInt
		} else {
			rightVal = right.Value
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot subtract %s and %s", TypeName(left), TypeName(right))
		}

		if _, ok := leftVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) - asFloat(right, vm)))
			break
		}

		if _, ok := rightVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) - asFloat(right, vm)))
			break
		}

		if _, ok := leftVal.(uint64); ok {
			vm.push(NewNative(asUint(left) - asUint(right)))
			break
		}

		if _, ok := rightVal.(uint64); ok {
			vm.push(NewNative(asUint(left) - asUint(right)))
			break
		}

		if _, ok := leftVal.(int64); ok {
			vm.push(NewNative(asInt64(left) - asInt64(right)))
			break
		}

		if _, ok := rightVal.(int64); ok {
			vm.push(NewNative(asInt64(left) - asInt64(right)))
			break
		}

		vm.push(NewInt(leftVal.(int) - rightVal.(int)))

	case OP_MUL:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewInt(left.AsInt * right.AsInt))
			break
		}

		var leftVal any
		if left.IsInt {
			leftVal = left.AsInt
		} else {
			leftVal = left.Value
		}

		var rightVal any
		if right.IsInt {
			rightVal = right.AsInt
		} else {
			rightVal = right.Value
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot multiply %s and %s", TypeName(left), TypeName(right))
		}

		if _, ok := leftVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) * asFloat(right, vm)))
			break
		}

		if _, ok := rightVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) * asFloat(right, vm)))
			break
		}

		if _, ok := leftVal.(uint64); ok {
			vm.push(NewNative(asUint(left) * asUint(right)))
			break
		}

		if _, ok := rightVal.(uint64); ok {
			vm.push(NewNative(asUint(left) * asUint(right)))
			break
		}

		if _, ok := leftVal.(int64); ok {
			vm.push(NewNative(asInt64(left) * asInt64(right)))
			break
		}

		if _, ok := rightVal.(int64); ok {
			vm.push(NewNative(asInt64(left) * asInt64(right)))
			break
		}

		vm.push(NewInt(leftVal.(int) * rightVal.(int)))

	case OP_DIV:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			if right.AsInt == 0 {
				vm.fatalError(ErrorRuntime, "cannot divide by zero")
			}
			vm.push(NewInt(left.AsInt / right.AsInt))
			break
		}

		var leftVal any
		if left.IsInt {
			leftVal = left.AsInt
		} else {
			leftVal = left.Value
		}

		var rightVal any
		if right.IsInt {
			rightVal = right.AsInt
		} else {
			rightVal = right.Value
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot divide %s and %s", TypeName(left), TypeName(right))
		}

		if _, ok := leftVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) / asFloat(right, vm)))
			break
		}

		if _, ok := rightVal.(float64); ok {
			vm.push(NewNative(asFloat(left, vm) / asFloat(right, vm)))
			break
		}

		if _, ok := leftVal.(uint64); ok {
			vm.push(NewNative(asUint(left) / asUint(right)))
			break
		}

		if _, ok := rightVal.(uint64); ok {
			vm.push(NewNative(asUint(left) / asUint(right)))
			break
		}

		if _, ok := leftVal.(int64); ok {
			vm.push(NewNative(asInt64(left) / asInt64(right)))
			break
		}

		if _, ok := rightVal.(int64); ok {
			vm.push(NewNative(asInt64(left) / asInt64(right)))
			break
		}

		vm.push(NewInt(leftVal.(int) / rightVal.(int)))

	case OP_EQ:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(NewNative(valuesEqual(left, right)))

	case OP_NEQ:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(NewNative(!valuesEqual(left, right)))

	case OP_LT:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewNative(left.AsInt < right.AsInt))
			break
		}

		var leftVal any
		if left.IsInt {
			leftVal = left.AsInt
		} else {
			leftVal = left.Value
		}

		var rightVal any
		if right.IsInt {
			rightVal = right.AsInt
		} else {
			rightVal = right.Value
		}

		switch l := leftVal.(type) {
		case int:
			switch r := rightVal.(type) {
			case int:
				vm.push(NewNative(l < r))

			case float64:
				vm.push(NewNative(float64(l) < r))

			default:
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		case float64:
			switch r := rightVal.(type) {
			case int:
				vm.push(NewNative(l < float64(r)))

			case float64:
				vm.push(NewNative(l < r))

			default:
				vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
			}

		default:
			vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

	case OP_GT:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewNative(left.AsInt > right.AsInt))
			break
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(NewNative(asFloat(left, vm) > asFloat(right, vm)))

	case OP_LTE:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewNative(left.AsInt <= right.AsInt))
			break
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(NewNative(asFloat(left, vm) <= asFloat(right, vm)))

	case OP_GTE:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			vm.push(NewNative(left.AsInt >= right.AsInt))
			break
		}

		if !isNumber(left) || !isNumber(right) {
			vm.fatalError(ErrorType, "cannot compare %s and %s", TypeName(left), TypeName(right))
		}

		vm.push(NewNative(asFloat(left, vm) >= asFloat(right, vm)))

	case OP_AND:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(NewNative(isTruthy(left) && isTruthy(right)))

	case OP_OR:
		right := vm.popFast()
		left := vm.popFast()
		vm.push(NewNative(isTruthy(left) || isTruthy(right)))

	case OP_JUMP:
		target := instr.IntArg
		vm.setIP(target)

	case OP_JUMP_IF_FALSE:
		target := instr.IntArg
		condition := vm.popFast()

		if !isTruthy(condition) {
			vm.setIP(target)
		}

	case OP_JUMP_IF_TRUE:
		target := instr.IntArg
		condition := vm.popFast()

		if isTruthy(condition) {
			vm.setIP(target)
		}

	case OP_METHOD_CALL:
		info := instr.Value.(MethodCallInfo)

		vm.callMethod(info.Method, info.ArgCount)

	case OP_METHOD_CALL_LOCAL_0:
		info := instr.Value.(MethodLocalCallInfo)
		frame := vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_0")

		if vm.callZeroArgNativeMethod(info.Method, objectValue) {
			break
		}
		vm.callMethodResolved(info.Method, objectValue, nil)

	case OP_METHOD_CALL_LOCAL_1:
		info := instr.Value.(MethodLocalCallInfo)
		frame := vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ReceiverSlot, "OP_METHOD_CALL_LOCAL_1")
		arg := frameLocalValue(frame, info.ArgSlot, "OP_METHOD_CALL_LOCAL_1")

		if vm.callOneArgNativeMethod(info.Method, objectValue, arg) {
			break
		}
		vm.callMethodResolved(info.Method, objectValue, []Value{arg})

	case OP_ARRAY_LEN_LOCAL:
		info := instr.Value.(ArrayLocalCallInfo)
		frame := vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_LEN_LOCAL")

		if array, ok := arrayValue.Value.(*ArrayValue); ok {
			vm.push(NewInt(len(array.Elements)))
			break
		}
		vm.callMethodResolved("length", arrayValue, nil)

	case OP_ARRAY_GET_LOCAL:
		info := instr.Value.(ArrayLocalCallInfo)
		frame := vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_GET_LOCAL")
		indexValue := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_GET_LOCAL")

		if array, ok := arrayValue.Value.(*ArrayValue); ok {
			var index int
			if indexValue.IsInt {
				index = indexValue.AsInt
			} else {
				var ok bool
				index, ok = indexValue.Value.(int)
				if !ok {
					vm.runtimeError(ErrorType, "array.get argument 1 expected number, got %s", TypeName(indexValue))
				}
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
		frame := vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL")
		value := frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL")

		if array, ok := arrayValue.Value.(*ArrayValue); ok {
			array.Elements = append(array.Elements, value)
			vm.push(arrayValue)
			break
		}
		vm.callMethodResolved("push", arrayValue, []Value{value})

	case OP_ARRAY_PUSH_LOCAL_MUL_CONST:
		info := instr.Value.(ArrayLocalMulConstInfo)
		frame := vm.frames[len(vm.frames)-1]
		arrayValue := frameLocalValue(frame, info.ArraySlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST")
		arg := multiplyByInt(frameLocalValue(frame, info.ArgSlot, "OP_ARRAY_PUSH_LOCAL_MUL_CONST"), info.Factor)

		if array, ok := arrayValue.Value.(*ArrayValue); ok {
			array.Elements = append(array.Elements, arg)
			vm.push(arrayValue)
			break
		}
		vm.callMethodResolved("push", arrayValue, []Value{arg})

	case OP_LEN:
		value := vm.popFast()

		var rawVal any
		if value.IsInt {
			rawVal = value.AsInt
		} else {
			rawVal = value.Value
		}

		switch v := rawVal.(type) {
		case *ArrayValue:
			vm.push(NewInt(len(v.Elements)))

		case ArrayValue:
			vm.push(NewInt(len(v.Elements)))

		case string:
			vm.push(NewInt(len([]rune(v))))

		case ObjectValue:
			vm.push(NewInt(len(v)))

		case BufferValue:
			vm.push(NewInt(len(v.Bytes)))

		case *BufferValue:
			vm.push(NewInt(len(v.Bytes)))

		default:
			vm.fatalError(ErrorType, "cannot get length of %s", TypeName(value))
		}

	case OP_CALL:
		info := instr.Value.(CallInfo)

		if class, exists := vm.classes[info.Name]; exists {
			vm.callClass(class, info.ArgCount)
			return false
		}

		vm.callFunction(info.Name, info.ArgCount)

		return false

	case OP_CALL_VALUE:
		info := instr.Value.(CallInfo)

		args := vm.popArgs(info.ArgCount)

		callee := vm.popFast()

		switch v := callee.Value.(type) {
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
			vm.fatalError(ErrorType, "expected function or class, got %s", TypeName(callee))
		}

		return false

	case OP_BUILTIN_CALL:
		info := instr.Value.(BuiltinCallInfo)
		vm.callBuiltin(info.Object, info.Method, info.ArgCount)

	case OP_MOD:
		right := vm.popFast()
		left := vm.popFast()

		if left.IsInt && right.IsInt {
			if right.AsInt == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}
			vm.push(NewInt(left.AsInt % right.AsInt))
			break
		}

		var leftVal any
		if left.IsInt {
			leftVal = left.AsInt
		} else {
			leftVal = left.Value
		}

		switch l := leftVal.(type) {
		case int:
			r := asInt(right)

			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(NewInt(l % r))

		case int64:
			r := int64(asInt(right))

			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(NewNative(l % r))

		case float32:
			r := asFloat64(right)

			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(NewNative(math.Mod(float64(l), r)))

		case float64:
			r := asFloat64(right)

			if r == 0 {
				vm.fatalError(ErrorRuntime, "cannot modulo by zero")
			}

			vm.push(NewNative(math.Mod(l, r)))

		default:
			vm.fatalError(ErrorType, "cannot modulo %s and %s", TypeName(left), TypeName(right))
		}

	case OP_ARRAY:
		info := instr.Value.(ArrayInfo)

		if vm.top < info.Count {
			vm.handleUnderflow()
		}

		elements := make([]Value, info.Count)
		start := vm.top - info.Count

		copy(elements, vm.stack[start:vm.top])
		for i := start; i < vm.top; i++ {
			vm.stack[i] = Value{}
		}
		vm.top = start

		vm.push(NewNative(&ArrayValue{Elements: elements}))

	case OP_INDEX:
		indexValue := vm.popFast()
		objectValue := vm.popFast()

		var rawObj any
		if objectValue.IsInt {
			rawObj = objectValue.AsInt
		} else {
			rawObj = objectValue.Value
		}

		switch obj := rawObj.(type) {
		case *ArrayValue:
			var index int
			if indexValue.IsInt {
				index = indexValue.AsInt
			} else {
				index = asInt(indexValue)
			}

			if index < 0 || index >= len(obj.Elements) {
				vm.runtimeError(ErrorRuntime, "array index out of range: %d", index)
				return false
			}

			vm.push(obj.Elements[index])

		case ObjectValue:
			var key string
			if indexValue.IsInt {
				key = intToString(indexValue.AsInt)
			} else {
				key = valueToString(indexValue)
			}

			value, exists := obj[key]
			if !exists {
				obj[key] = NewUndefined()
				value = obj[key]
			}

			vm.push(value)

		default:
			vm.fatalError(ErrorType, "cannot index %s", TypeName(objectValue))
		}

	case OP_SET_INDEX:
		value := vm.popFast()
		indexValue := vm.popFast()
		objectValue := vm.popFast()

		var rawObj any
		if objectValue.IsInt {
			rawObj = objectValue.AsInt
		} else {
			rawObj = objectValue.Value
		}

		switch obj := rawObj.(type) {
		case *ArrayValue:
			var index int
			if indexValue.IsInt {
				index = indexValue.AsInt
			} else {
				index = asInt(indexValue)
			}

			if index < 0 || index >= len(obj.Elements) {
				vm.fatalError(ErrorRuntime, "array index out of range: %d", index)
			}

			obj.Elements[index] = value

		case ObjectValue:
			var key string
			if indexValue.IsInt {
				key = intToString(indexValue.AsInt)
			} else {
				key = valueToString(indexValue)
			}

			if className, isClass := obj["__class"]; isClass {
				vm.runtimeError(ErrorRuntime, "cannot modify class '%s' by index operator.", className.Value)
			}
			obj[key] = value

		default:
			vm.fatalError(ErrorType, "cannot index assign %s", TypeName(objectValue))
		}

	case OP_RETURN:
		var returnValue Value

		if vm.top == 0 {
			returnValue = NewUndefined()
		} else {
			returnValue = vm.popFast()
		}

		if len(vm.frames) == 0 {
			vm.push(returnValue)
			return true // Halt
		}

		if len(vm.deferHandlers) > 0 {
			vm.runDefersAboveDepth(len(vm.frames) - 1)
		}

		frame := vm.frames[len(vm.frames)-1]
		vm.frames = vm.frames[:len(vm.frames)-1]

		if !frame.function.ReturnType.IsEmpty() {
			if ok, reason := CheckTypeHint(returnValue, frame.function.ReturnType, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"function %s should return %s, got %s%s",
					frame.function.Name,
					frame.function.ReturnType.Name,
					TypeName(returnValue),
					reason,
				)
			}
		}

		vm.releaseFrame(frame)

		if frame.hasReturnOverride {
			vm.push(frame.returnOverride)
		} else {
			vm.push(returnValue)
		}

	case OP_THROW:
		value := vm.popFast()
		vm.throwValue(value)

	case OP_POP:
		vm.popFast()

	case OP_INTERPOLATE:
		info := instr.Value.(InterpolateInfo)

		if info.ExprCount == 1 {
			value := vm.popFast()
			vm.push(NewNative(info.Parts[0] + valueToString(value) + info.Parts[1]))
			break
		}

		if vm.top < info.ExprCount {
			vm.handleUnderflow()
		}

		start := vm.top - info.ExprCount

		var builder strings.Builder

		for i := 0; i < info.ExprCount; i++ {
			builder.WriteString(info.Parts[i])
			builder.WriteString(valueToString(vm.stack[start+i]))
			vm.stack[start+i] = Value{}
		}

		vm.top = start
		builder.WriteString(info.Parts[len(info.Parts)-1])

		vm.push(NewNative(builder.String()))

	case OP_OBJECT:
		info := instr.Value.(ObjectInfo)

		if vm.top < len(info.Names) {
			vm.handleUnderflow()
		}

		object := make(ObjectValue, len(info.Names))
		start := vm.top - len(info.Names)

		for i, fieldInfo := range info.Names {
			if fieldInfo.Copy {
				obj, ok := vm.stack[start+i].Value.(ObjectValue)

				if !ok {
					vm.fatalError(ErrorType, "expected an object to copy with {...%s}, but got %s", fieldInfo.Name, TypeName(vm.stack[start+i]))
				}

				maps.Copy(object, obj)
			} else {
				object[fieldInfo.Name] = vm.stack[start+i]
			}
			vm.stack[start+i] = Value{}
		}
		vm.top = start

		vm.push(NewNative(object))

	case OP_NOT:
		value := vm.popFast()
		vm.push(NewNative(!isTruthy(value)))

	case OP_GET_PROPERTY_LOCAL:
		info := instr.Value.(PropertyLocalInfo)
		frame := vm.frames[len(vm.frames)-1]
		vm.push(propertyValue(vm, frameLocalValue(frame, info.Slot, "OP_GET_PROPERTY_LOCAL"), info.Name))

	case OP_ADD_PROPERTY_LOCAL_LOCAL:
		info := instr.Value.(PropertyLocalAssignInfo)
		frame := vm.frames[len(vm.frames)-1]
		objectValue := frameLocalValue(frame, info.ObjectSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
		object, ok := objectValue.Value.(ObjectValue)
		if !ok {
			vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
		}

		_, isClass := object["__class"]
		if isClass {
			constFields, _ := object["__constFields"].Value.(map[string]bool)
			if _, isConstant := constFields[info.Name]; isConstant {
				vm.fatalError(ErrorRuntime, "cannot assign to constant field: %s", info.Name)
			}
			if !vm.canAccessField(object, info.Name) {
				vm.fatalError(ErrorRuntime, "cannot assign private field: %s", info.Name)
			}
		}

		current := propertyValue(vm, NewNative(object), info.Name)
		source := frameLocalValue(frame, info.SourceSlot, "OP_ADD_PROPERTY_LOCAL_LOCAL")
		object[info.Name] = addValues(current, source)

	case OP_GET_PROPERTY:
		name := instr.Value.(string)
		objectValue := vm.popFast()
		vm.push(vm.getProperty(objectValue, name, false))

	case OP_HALT:
		return true

	default:
		vm.fatalError(ErrorInternal, "unknown opcode: %d", instr.Op)
	}

	return false
}

func writeServerResponse(w http.ResponseWriter, value Value, responseType HttpResponseType) {
	switch responseType {
	case HttpJson:
		w.Header().Set("Content-Type", "application/json")
		jsonValue := valueToJSONCompatible(ToValue(value))
		bytes, _ := json.Marshal(jsonValue)
		fmt.Fprint(w, string(bytes))

	case HttpText:
		stringValue, _ := value.Value.(string)
		trimmed := strings.TrimSpace(stringValue)
		fmt.Fprint(w, trimmed)
	}
}

func (vm *VM) callNamespaceMethod(ns NamespaceValue, method string, args []Value) {
	value, exists := ns.Members[method]
	if !exists {
		vm.fatalError(ErrorName, "namespace %s has no member: %s", ns.Name, method)
	}

	var rawVal any
	if value.IsInt {
		rawVal = value.AsInt
	} else {
		rawVal = value.Value
	}

	switch v := rawVal.(type) {
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
		vm.fatalError(ErrorType, "namespace member %s is not callable", method)
	}
}

func (vm *VM) findEmbeddedMethod(object ObjectValue, method string) (ObjectValue, FunctionValue, bool) {
	classNameValue, ok := object["__class"]
	if !ok {
		return nil, FunctionValue{}, false
	}

	className, ok := classNameValue.Value.(string)
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

		embeddedObject, ok := fieldValue.Value.(ObjectValue)
		if !ok {
			continue
		}

		methodValue, exists := embeddedObject[method]
		if !exists {
			if receiver, fn, ok := vm.findEmbeddedMethod(embeddedObject, method); ok {
				return receiver, fn, true
			}

			continue
		}

		fnValue, ok := methodValue.Value.(FunctionValue)
		if !ok {
			continue
		}

		return embeddedObject, fnValue, true
	}

	return nil, FunctionValue{}, false
}

func (vm *VM) callZeroArgNativeMethod(method string, objectValue Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *ArrayValue:
		vm.callArrayMethod(value, method, []Value{})
		return true
	case string:
		vm.callStringMethod(value, method, []Value{})
		return true
	case *NativeMutexValue:
		vm.callNativeMutexMethod(value, method, []Value{})
		return true
	}
	return false
}

func (vm *VM) callOneArgNativeMethod(method string, objectValue Value, arg Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *ArrayValue:
		vm.callArrayMethod(value, method, []Value{arg})
		return true
	case string:
		vm.callStringMethod(value, method, []Value{arg})
		return true
	}
	return false
}

func (vm *VM) callTwoArgNativeMethod(method string, objectValue Value, arg0 Value, arg1 Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch value := rawVal.(type) {
	case *ArrayValue:
		vm.callArrayMethod(value, method, []Value{arg0, arg1})
		return true
	case string:
		vm.callStringMethod(value, method, []Value{arg0, arg1})
		return true
	}
	return false
}

func (vm *VM) callStdObjectFast1(method string, objectValue Value, arg0 Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "length" {
		return false
	}

	var rawArg any
	if arg0.IsInt {
		rawArg = arg0.AsInt
	} else {
		rawArg = arg0.Value
	}

	obj, ok := rawArg.(ObjectValue)
	if !ok {
		vm.fatalError(ErrorType, "object.length argument 1 expected object, got %s", TypeName(arg0))
		return true
	}
	vm.push(NewInt(len(obj)))
	return true
}

func (vm *VM) callStdObjectFast2(method string, objectValue Value, arg0 Value, arg1 Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "get" {
		return false
	}

	var rawArg0 any
	if arg0.IsInt {
		rawArg0 = arg0.AsInt
	} else {
		rawArg0 = arg0.Value
	}

	obj, ok := rawArg0.(ObjectValue)
	if !ok {
		vm.fatalError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(arg0))
		return true
	}

	var rawArg1 any
	if arg1.IsInt {
		rawArg1 = arg1.AsInt
	} else {
		rawArg1 = arg1.Value
	}

	key, ok := rawArg1.(string)
	if !ok {
		vm.fatalError(ErrorType, "object.get argument 2 expected string, got %s", TypeName(arg1))
		return true
	}
	vm.push(obj[key])
	return true
}

func (vm *VM) callStdObjectFast3(method string, objectValue Value, arg0 Value, arg1 Value, arg2 Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	if method != "set" {
		return false
	}

	var rawArg0 any
	if arg0.IsInt {
		rawArg0 = arg0.AsInt
	} else {
		rawArg0 = arg0.Value
	}

	obj, ok := rawArg0.(ObjectValue)
	if !ok {
		vm.fatalError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(arg0))
		return true
	}

	var rawArg1 any
	if arg1.IsInt {
		rawArg1 = arg1.AsInt
	} else {
		rawArg1 = arg1.Value
	}

	key, ok := rawArg1.(string)
	if !ok {
		vm.fatalError(ErrorType, "object.set argument 2 expected string, got %s", TypeName(arg1))
		return true
	}
	obj[key] = arg2
	vm.push(NewUndefined())
	return true
}

func (vm *VM) callStdObjectFast(method string, objectValue Value, args ...Value) bool {
	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	module, ok := rawVal.(*StandardModuleValue)
	if !ok || module.Name != "object" {
		return false
	}

	switch method {
	case "get":
		if len(args) != 2 {
			return false
		}

		var rawArg0 any
		if args[0].IsInt {
			rawArg0 = args[0].AsInt
		} else {
			rawArg0 = args[0].Value
		}

		obj, ok := rawArg0.(ObjectValue)
		if !ok {
			vm.fatalError(ErrorType, "object.get argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}

		var rawArg1 any
		if args[1].IsInt {
			rawArg1 = args[1].AsInt
		} else {
			rawArg1 = args[1].Value
		}

		key, ok := rawArg1.(string)
		if !ok {
			vm.fatalError(ErrorType, "object.get argument 2 expected string, got %s", TypeName(args[1]))
			return true
		}
		vm.push(obj[key])
		return true

	case "set":
		if len(args) != 3 {
			return false
		}

		var rawArg0 any
		if args[0].IsInt {
			rawArg0 = args[0].AsInt
		} else {
			rawArg0 = args[0].Value
		}

		obj, ok := rawArg0.(ObjectValue)
		if !ok {
			vm.fatalError(ErrorType, "object.set argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}

		var rawArg1 any
		if args[1].IsInt {
			rawArg1 = args[1].AsInt
		} else {
			rawArg1 = args[1].Value
		}

		key, ok := rawArg1.(string)
		if !ok {
			vm.runtimeError(ErrorType, "object.set argument 2 expected string, got %s", TypeName(args[1]))
			return true
		}
		obj[key] = args[2]
		vm.push(NewUndefined())
		return true

	case "length":
		if len(args) != 1 {
			return false
		}

		var rawArg0 any
		if args[0].IsInt {
			rawArg0 = args[0].AsInt
		} else {
			rawArg0 = args[0].Value
		}

		obj, ok := rawArg0.(ObjectValue)
		if !ok {
			vm.fatalError(ErrorType, "object.length argument 1 expected object, got %s", TypeName(args[0]))
			return true
		}
		vm.push(NewInt(len(obj)))
		return true
	}

	return false
}

func (vm *VM) callMethodFast(method string, argCount int) {
	switch argCount {
	case 0:
		objectValue := vm.popFast()
		if vm.callZeroArgNativeMethod(method, objectValue) {
			return
		}
		vm.callMethodResolved(method, objectValue, nil)
		return

	case 1:
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast1(method, objectValue, arg0) {
			return
		}
		if vm.callOneArgNativeMethod(method, objectValue, arg0) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0})
		return

	case 2:
		arg1 := vm.popFast()
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast2(method, objectValue, arg0, arg1) {
			return
		}
		if vm.callTwoArgNativeMethod(method, objectValue, arg0, arg1) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0, arg1})
		return

	case 3:
		arg2 := vm.popFast()
		arg1 := vm.popFast()
		arg0 := vm.popFast()
		objectValue := vm.popFast()
		if vm.callStdObjectFast3(method, objectValue, arg0, arg1, arg2) {
			return
		}
		vm.callMethodResolved(method, objectValue, []Value{arg0, arg1, arg2})
		return

	default:
		args := vm.popArgs(argCount)
		objectValue := vm.popFast()
		vm.callMethodResolved(method, objectValue, args)
		return
	}
}

func (vm *VM) callMethodResolved(method string, objectValue Value, args []Value) {
	if method == "toString" {
		vm.push(NewNative(valueToString(objectValue)))
		return
	}

	var rawVal any
	if objectValue.IsInt {
		rawVal = objectValue.AsInt
	} else {
		rawVal = objectValue.Value
	}

	switch val := rawVal.(type) {
	case NamespaceValue:
		vm.callNamespaceMethod(val, method, args)
		return

	case *NamespaceValue:
		vm.callNamespaceMethod(*val, method, args)
		return

	case *NativePluginValue:
		popNative := vm.pushNativeFrame("plugin." + method)
		defer popNative()

		vm.callNativePlugin(val, method, args)
		return

	case *NativeAppValue:
		vm.callNativeAppMethod(val, method, args)
		return

	case *StandardModuleValue:
		popNative := vm.pushNativeFrame(val.Name + "." + method)
		defer popNative()

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

	case *NativeMutexValue:
		vm.callNativeMutexMethod(val, method, args)
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

	object, ok := rawVal.(ObjectValue)
	if !ok {
		vm.fatalError(ErrorType, "expected object, got %s", TypeName(objectValue))
	}

	receiver := object

	methodValue, exists := object[method]

	var fnValue FunctionValue

	if exists {
		var ok bool

		fnValue, ok = methodValue.Value.(FunctionValue)
		if !ok {
			vm.fatalError(ErrorType, "property %s is not callable", method)
		}
	} else {
		embeddedReceiver, embeddedFn, ok := vm.findEmbeddedMethod(object, method)
		if !ok {
			vm.fatalError(ErrorName, "object has no method: %s", method)
		}

		receiver = embeddedReceiver
		fnValue = embeddedFn
	}

	fn, ok := vm.functions[fnValue.Name]
	if !ok {
		vm.fatalError(ErrorName, "undefined function: %s", fnValue.Name)
	}

	ownerClass := methodOwnerClass(fnValue.Name)

	if isClass(receiver) && !vm.canAccessMethod(object, method) {
		vm.fatalError(ErrorRuntime, "cannot access private method %s in class %s", method, ownerClass)
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
			vm.fatalError(
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
			vm.fatalError(
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

	setCellValue(frame.locals[0], NewNative(receiver))
	frame.constants[0] = true

	if isVariadic {
		fixedCount := userParamCount - 1

		// normal params before ...args
		for i := range fixedCount {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]
			arg := args[i]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"method %s parameter %s expected %s, got %s%s",
						method,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.constants[paramIndex] = false
			frame.localTypes[paramIndex] = param.TypeHint
		}

		// rest param
		restSlot := paramOffset + fixedCount
		restParam := fn.Params[restSlot]

		rest := &ArrayValue{
			Elements: make([]Value, 0, len(args)-fixedCount),
		}

		for i := fixedCount; i < len(args); i++ {
			arg := args[i]

			if fn.HasTypeHints && !restParam.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, restParam.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"method %s rest parameter %s expected %s, got %s%s",
						method,
						restParam.Name,
						restParam.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			rest.Elements = append(rest.Elements, arg)
		}

		setCellValue(frame.locals[restSlot], NewNative(rest))
		frame.constants[restSlot] = false
		frame.localTypes[restSlot] = TypeHint{
			Name: "array",
		}
	} else {
		for i, arg := range args {
			paramIndex := paramOffset + i
			param := fn.Params[paramIndex]

			if fn.HasTypeHints && !param.TypeHint.IsEmpty() {
				if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
					vm.fatalError(
						ErrorType,
						"method %s parameter %s expected %s, got %s%s",
						method,
						param.Name,
						param.TypeHint.String(),
						TypeName(arg),
						reason,
					)
				}
			}

			setCellValue(frame.locals[paramIndex], arg)
			frame.constants[paramIndex] = false
			frame.localTypes[paramIndex] = param.TypeHint
		}
	}

	if fn.Async {
		task := &NativeTaskValue{
			Done: make(chan TaskResult, 1),
		}

		taskVM := vm.CloneForTask()

		go func() {
			defer func() {
				if r := recover(); r != nil {
					task.Done <- TaskResult{Error: r}
				}
			}()

			result := taskVM.runFrameToCompletion(frame)

			task.Done <- TaskResult{
				Value: result,
			}
		}()

		vm.push(NewNative(task))

		return
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
		vm.fatalError(ErrorRuntime, "unknown command: %s", commandName)
	}

	tinyArgs := &ArrayValue{
		Elements: make([]Value, len(commandArgs)),
	}

	for i, arg := range commandArgs {
		tinyArgs.Elements[i] = NewNative(arg)
	}

	vm.callFunctionValue(fn, []Value{NewNative(tinyArgs)})
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
		vm.fatalError(ErrorName, "undefined function: %s", name)
	}

	args := vm.popArgs(argCount)

	args = vm.applyDefaultArgs(fn, args, 0, "function "+fn.Name)

	frame := vm.getFrame(fn)

	for i, arg := range args {
		param := fn.Params[i]

		if !param.TypeHint.IsEmpty() {
			if ok, reason := CheckTypeHint(arg, param.TypeHint, vm.interfaces); !ok {
				vm.fatalError(
					ErrorType,
					"function %s parameter %s expected %s, got %s%s",
					fn.Name,
					param.Name,
					param.TypeHint.String(),
					TypeName(arg),
					reason,
				)
			}
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

func (vm *VM) pushNativeFrame(name string) func() {
	var instr Instruction
	if len(vm.frames) == 0 {
		if vm.ip-1 >= 0 && vm.ip-1 < len(vm.mainInstructions) {
			instr = vm.mainInstructions[vm.ip-1]
		}
	} else {
		frame := vm.frames[len(vm.frames)-1]
		if frame.ip-1 >= 0 && frame.ip-1 < len(frame.instructions) {
			instr = frame.instructions[frame.ip-1]
		}
	}

	vm.nativeFrames = append(vm.nativeFrames, NativeCallFrame{
		Name:   name,
		File:   instr.File,
		Line:   instr.Line,
		Column: instr.Column,
	})

	return func() {
		if len(vm.nativeFrames) > 0 {
			vm.nativeFrames = vm.nativeFrames[:len(vm.nativeFrames)-1]
		}
	}
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

func (vm *VM) fetchInstruction() Instruction {
	if len(vm.frames) == 0 {
		ip := vm.ip
		instructions := vm.mainInstructions

		if ip < 0 || ip >= len(instructions) {
			vm.fatalError(ErrorInternal, "instruction pointer out of range: ip=%d len=%d", ip, len(instructions))
		}

		instr := instructions[ip]
		vm.ip = ip + 1
		return instr
	}

	frame := vm.frames[len(vm.frames)-1]
	ip := frame.ip
	instructions := frame.instructions

	if ip < 0 || ip >= len(instructions) {
		vm.fatalError(ErrorInternal, "instruction pointer out of range: ip=%d len=%d", ip, len(instructions))
	}

	instr := instructions[ip]
	frame.ip = ip + 1
	return instr
}

func (vm *VM) currentFrame() *Frame {
	if len(vm.frames) == 0 {
		vm.fatalError(ErrorInternal, "no current function frame")
	}

	return vm.frames[len(vm.frames)-1]
}

func (vm *VM) popArgs(count int) []Value {
	if vm.top < count {
		vm.handleUnderflow()
	}

	args := make([]Value, count)

	start := vm.top - count

	copy(args, vm.stack[start:vm.top])

	for i := start; i < vm.top; i++ {
		vm.stack[i] = Value{}
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
	vm.stack[vm.top] = Value{}

	return val
}

func (vm *VM) handleUnderflow() {
	lastFunctionName := "<main>"
	lastInstructionIndex := vm.ip - 1
	var lastInstruction Instruction

	if len(vm.frames) > 0 {
		frame := vm.frames[len(vm.frames)-1]
		lastFunctionName = frame.function.Name
		lastInstructionIndex = frame.ip - 1
		if lastInstructionIndex >= 0 && lastInstructionIndex < len(frame.instructions) {
			lastInstruction = frame.instructions[lastInstructionIndex]
		}
	} else if lastInstructionIndex >= 0 && lastInstructionIndex < len(vm.mainInstructions) {
		lastInstruction = vm.mainInstructions[lastInstructionIndex]
	}

	vm.fatalError(
		ErrorInternal,
		"stack underflow at function=%s ip=%d op=%v value=%#v",
		lastFunctionName,
		lastInstructionIndex,
		lastInstruction.Op.String(),
		lastInstruction.Value,
	)
}

func (vm *VM) popFast() Value {
	vm.top--
	val := vm.stack[vm.top]
	vm.stack[vm.top] = Value{}
	return val
}
