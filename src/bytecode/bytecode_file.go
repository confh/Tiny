package bytecode

import (
	"bytes"
	"os"

	json "github.com/goccy/go-json"
	"github.com/vmihailenco/msgpack/v5"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

const BytecodeVersion = 1

var bytecodeMagic = []byte{'T', 'B', 'C', 2}

const bytecodeSourceLabel = "<tiny>"

type BytecodeFile struct {
	Version     int                              `json:"version"`
	Main        []SerializableInstruction        `json:"main"`
	Functions   map[string]SerializableFunction  `json:"functions"`
	Classes     map[string]SerializableClass     `json:"classes"`
	Interfaces  map[string]SerializableInterface `json:"interfaces"`
	GlobalIndex map[string]int                   `json:"globalIndex"`
}

type SerializableParam struct {
	Name         string       `json:"name"`
	TypeHint     TypeHint     `json:"typeHint"`
	HasDefault   bool         `json:"hasDefault"`
	DefaultValue EncodedValue `json:"defaultValue"`
	Variadic     bool         `json:"variadic"`
}

type SerializableFunction struct {
	ID           int                       `json:"id"`
	Name         string                    `json:"name"`
	Params       []SerializableParam       `json:"params"`
	ReturnType   TypeHint                  `json:"returnType"`
	LocalCount   int                       `json:"localCount"`
	Captures     []CapturedVar             `json:"captures"`
	Instructions []SerializableInstruction `json:"instructions"`
	Async        bool                      `json:"async"`
	HasDefaults  bool                      `json:"hasDefaults"`
	HasTypeHints bool                      `json:"hasTypeHints"`
}

type SerializableInterface struct {
	Name   string              `json:"name"`
	Fields map[string]TypeHint `json:"fields"`
}

type SerializableClassField struct {
	Name     string       `json:"name"`
	Value    EncodedValue `json:"value"`
	TypeHint TypeHint     `json:"typeHint"`
	Constant bool         `json:"constant"`
	Private  bool         `json:"private"`
}

type SerializableClass struct {
	Name           string                   `json:"name"`
	Fields         []SerializableClassField `json:"fields"`
	Methods        map[string]string        `json:"methods"`
	Embeds         []string                 `json:"embeds"`
	PrivateMethods map[string]bool          `json:"privateMethods"`
}

type SerializableNamespaceValue struct {
	Name    string                  `json:"name"`
	Members map[string]EncodedValue `json:"members"`
}

type SerializableNamespaceMemberRef struct {
	GlobalName string `json:"globalName"`
}

type SerializableInstruction struct {
	Op     OpCode       `json:"op"`
	Value  EncodedValue `json:"value"`
	File   string       `json:"file,omitempty"`
	Line   int          `json:"line,omitempty"`
	Column int          `json:"column,omitempty"`
}

type EncodedValue struct {
	Type string `json:"type"`
	Data any    `json:"data,omitempty"`
}

func serializeParams(params []Param) []SerializableParam {
	result := make([]SerializableParam, len(params))

	for i, param := range params {
		encodedDefault := EncodedValue{Type: "undefined"}

		if param.HasDefault {
			encodedDefault = EncodeValue(param.DefaultValue)
		}

		result[i] = SerializableParam{
			Name:         param.Name,
			TypeHint:     param.TypeHint,
			HasDefault:   param.HasDefault,
			DefaultValue: encodedDefault,
			Variadic:     param.Variadic,
		}
	}

	return result
}

func deserializeParams(params []SerializableParam) []Param {
	result := make([]Param, len(params))

	for i, param := range params {
		defaultValue := NewUndefined()

		if param.HasDefault {
			decoded := DecodeValue(param.DefaultValue)

			if valStruct, ok := decoded.(Value); ok {
				defaultValue = valStruct
			} else if intVal, ok := decoded.(int); ok {
				defaultValue = NewInt(intVal)
			} else {
				defaultValue = NewNative(decoded)
			}
		}

		result[i] = Param{
			Name:         param.Name,
			TypeHint:     param.TypeHint,
			HasDefault:   param.HasDefault,
			DefaultValue: defaultValue,
			Variadic:     param.Variadic,
		}
	}

	return result
}

func SaveBytecode(path string, main []Instruction, functions map[string]Function, classes map[string]Class, interfaces map[string]Interface, globalIndex map[string]int, cache bool) {
	file := BytecodeFile{
		Version:     BytecodeVersion,
		Main:        serializeInstructions(main, cache),
		Functions:   map[string]SerializableFunction{},
		Interfaces:  map[string]SerializableInterface{},
		Classes:     serializeClasses(classes),
		GlobalIndex: globalIndex,
	}

	for name, fn := range functions {
		file.Functions[name] = SerializableFunction{
			ID:           fn.ID,
			Name:         fn.Name,
			Params:       serializeParams(fn.Params),
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: serializeInstructions(fn.Instructions, cache),
			HasDefaults:  fn.HasDefaults,
			HasTypeHints: fn.HasTypeHints,
			Async:        fn.Async,
		}
	}

	for name, interfaceData := range interfaces {
		file.Interfaces[name] = SerializableInterface{
			Name:   interfaceData.Name,
			Fields: interfaceData.Fields,
		}
	}

	err := os.WriteFile(path, encodeBytecodeFile(file), 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write bytecode file: %v", err)
	}
}

func SaveBytecodeToBytes(main []Instruction, functions map[string]Function, classes map[string]Class, interfaces map[string]Interface, globalIndex map[string]int, cache bool) []byte {
	file := BytecodeFile{
		Version:     BytecodeVersion,
		Main:        serializeInstructions(main, cache),
		Functions:   map[string]SerializableFunction{},
		Interfaces:  map[string]SerializableInterface{},
		Classes:     serializeClasses(classes),
		GlobalIndex: globalIndex,
	}

	for name, fn := range functions {
		file.Functions[name] = SerializableFunction{
			ID:           fn.ID,
			Name:         fn.Name,
			Params:       serializeParams(fn.Params),
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: serializeInstructions(fn.Instructions, cache),
			HasDefaults:  fn.HasDefaults,
			HasTypeHints: fn.HasTypeHints,
			Async:        fn.Async,
		}
	}

	for name, interfaceData := range interfaces {
		file.Interfaces[name] = SerializableInterface{
			Name:   interfaceData.Name,
			Fields: interfaceData.Fields,
		}
	}

	return encodeBytecodeFile(file)
}

func LoadBytecode(path string) ([]Instruction, map[string]Function, map[string]Class, map[string]Interface, map[string]int) {
	data, err := os.ReadFile(path)
	if err != nil {
		LangError(ErrorRuntime, "failed to read bytecode file: %v", err)
	}

	return LoadBytecodeFromBytes(data)
}

func LoadBytecodeFromBytes(data []byte) ([]Instruction, map[string]Function, map[string]Class, map[string]Interface, map[string]int) {
	var file BytecodeFile

	decodeBytecodeFile(data, &file)

	if file.Version != BytecodeVersion {
		LangError(ErrorRuntime, "unsupported bytecode version: %d", file.Version)
	}

	main := deserializeInstructions(file.Main)

	functions := map[string]Function{}
	interfaces := map[string]Interface{}

	for name, fn := range file.Functions {
		functions[name] = Function{
			ID:           fn.ID,
			Name:         fn.Name,
			Params:       deserializeParams(fn.Params),
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: deserializeInstructions(fn.Instructions),
			HasDefaults:  fn.HasDefaults,
			HasTypeHints: fn.HasTypeHints,
			Async:        fn.Async,
		}
	}

	for name, interfaceData := range file.Interfaces {
		interfaces[name] = Interface{
			Name:   interfaceData.Name,
			Fields: interfaceData.Fields,
		}
	}

	return main, functions, deserializeClasses(file.Classes), interfaces, file.GlobalIndex
}

func encodeBytecodeFile(file BytecodeFile) []byte {
	data, err := msgpack.Marshal(file)
	if err != nil {
		LangError(ErrorRuntime, "failed to encode bytecode: %v", err)
	}

	result := make([]byte, 0, len(bytecodeMagic)+len(data))
	result = append(result, bytecodeMagic...)
	result = append(result, data...)
	return result
}

func decodeBytecodeFile(data []byte, file *BytecodeFile) {
	if bytes.HasPrefix(data, bytecodeMagic) {
		err := msgpack.Unmarshal(data[len(bytecodeMagic):], file)
		if err != nil {
			LangError(ErrorRuntime, "failed to decode bytecode file: %v", err)
		}
		return
	}

	err := json.Unmarshal(data, file)
	if err != nil {
		LangError(ErrorRuntime, "failed to decode bytecode file: %v", err)
	}
}

func serializeClasses(classes map[string]Class) map[string]SerializableClass {
	result := map[string]SerializableClass{}

	for name, class := range classes {
		fields := []SerializableClassField{}

		for _, field := range class.Fields {
			fields = append(fields, SerializableClassField{
				Name:     field.Name,
				Value:    EncodeValue(field.Value),
				TypeHint: field.TypeHint,
				Constant: field.Constant,
				Private:  field.Private,
			})
		}

		result[name] = SerializableClass{
			Name:           class.Name,
			Fields:         fields,
			Methods:        class.Methods,
			Embeds:         class.Embeds,
			PrivateMethods: class.PrivateMethods,
		}
	}

	return result
}

func deserializeClasses(classes map[string]SerializableClass) map[string]Class {
	result := map[string]Class{}

	for name, class := range classes {
		fields := []ClassField{}

		for _, field := range class.Fields {
			fields = append(fields, ClassField{
				Name:     field.Name,
				Value:    ToValue(DecodeValue(field.Value)),
				TypeHint: field.TypeHint,
				Constant: field.Constant,
				Private:  field.Private,
			})
		}

		result[name] = Class{
			Name:           class.Name,
			Fields:         fields,
			Methods:        class.Methods,
			Embeds:         class.Embeds,
			PrivateMethods: class.PrivateMethods,
		}
	}

	return result
}

func serializeInstructions(instructions []Instruction, cache bool) []SerializableInstruction {
	result := make([]SerializableInstruction, len(instructions))

	for i, instr := range instructions {
		var filePath string

		if !cache {
			sanitizeBytecodeFilePath(instr.File)
		} else {
			filePath = instr.File
		}

		result[i] = SerializableInstruction{
			Op:     instr.Op,
			Value:  EncodeValue(instr.Value),
			File:   filePath,
			Line:   instr.Line,
			Column: instr.Column,
		}
	}

	return result
}

func sanitizeBytecodeFilePath(file string) string {
	if file == "" {
		return ""
	}

	return bytecodeSourceLabel
}

func deserializeInstructions(instructions []SerializableInstruction) []Instruction {
	result := make([]Instruction, len(instructions))

	for i, instr := range instructions {
		val := DecodeValue(instr.Value)

		intVal := 0
		hasInt := false
		if v, ok := val.(int); ok {
			intVal = v
			hasInt = true
		}

		result[i] = Instruction{
			Op:     instr.Op,
			Value:  val,
			IntArg: intVal,
			IsInt:  hasInt,
			File:   instr.File,
			Line:   instr.Line,
			Column: instr.Column,
		}
	}

	return result
}

func EncodeValue(value any) EncodedValue {
	switch v := value.(type) {
	case nil:
		return EncodedValue{Type: "nil"}

	case int:
		return EncodedValue{Type: "int", Data: v}

	case int64:
		return EncodedValue{Type: "int64", Data: v}

	case float64:
		return EncodedValue{Type: "float", Data: v}

	case string:
		obfuscated := xor([]byte(v), 0x5A)
		return EncodedValue{Type: "string", Data: obfuscated}

	case bool:
		return EncodedValue{Type: "bool", Data: v}

	case Value:
		if v.IsInt {
			return EncodeValue(v.AsInt)
		}
		return EncodeValue(v.Value)

	case VariableInfo:
		return EncodedValue{Type: "variable", Data: v}

	case CallInfo:
		return EncodedValue{Type: "call", Data: v}

	case DirectCallInfo:
		return EncodedValue{Type: "directCall", Data: v}

	case BuiltinCallInfo:
		return EncodedValue{Type: "builtinCall", Data: v}

	case MethodCallInfo:
		return EncodedValue{Type: "methodCall", Data: v}

	case MethodLocalCallInfo:
		return EncodedValue{Type: "methodLocalCall", Data: v}

	case ArrayLocalCallInfo:
		return EncodedValue{Type: "arrayLocalCall", Data: v}

	case ArrayLocalMulConstInfo:
		return EncodedValue{Type: "arrayLocalMulConst", Data: v}

	case PropertyLocalInfo:
		return EncodedValue{Type: "propertyLocal", Data: v}

	case PropertyLocalAssignInfo:
		return EncodedValue{Type: "propertyLocalAssign", Data: v}

	case LocalConstInfo:
		return EncodedValue{Type: "localConst", Data: v}

	case JumpLocalGELocalInfo:
		return EncodedValue{Type: "jumpLocalGELocal", Data: v}

	case JumpLocalGTConstInfo:
		return EncodedValue{Type: "jumpLocalGTConst", Data: v}

	case AddLocalLocalStoreInfo:
		return EncodedValue{Type: "addLocalStore", Data: v}

	case JumpLocalGTLocalInfo:
		return EncodedValue{Type: "jumpLocalGTLocal", Data: v}

	case CallDirectSubConstInfo:
		return EncodedValue{Type: "callDirectSubConst", Data: v}

	case InterpolateInfo:
		return EncodedValue{Type: "interpolate", Data: v}

	case ObjectInfo:
		return EncodedValue{Type: "object", Data: v}

	case ClosureInfo:
		return EncodedValue{Type: "closure", Data: v}

	case JumpLocalGEConstInfo:
		return EncodedValue{Type: "jumpLocalGEConst", Data: v}

	case ArrayInfo:
		return EncodedValue{Type: "array", Data: v}

	case FunctionValue:
		return EncodedValue{Type: "functionValue", Data: v}

	case NullValue:
		return EncodedValue{Type: "null"}

	case UndefinedValue:
		return EncodedValue{Type: "undefined"}

	case IncrementInfo:
		return EncodedValue{Type: "incLocal", Data: v}

	case AssignLocalInfo:
		return EncodedValue{Type: "assignLocal", Data: v}

	case JumpModLocalLocalNotZeroInfo:
		return EncodedValue{Type: "jumpModLocalLocalNotZero", Data: v}

	case JumpModLocalConstNotZeroInfo:
		return EncodedValue{Type: "jumpModLocalConstNotZero", Data: v}

	case NamespaceValue:
		members := map[string]EncodedValue{}

		for name, member := range v.Members {
			members[name] = EncodeValue(member)
		}

		return EncodedValue{
			Type: "namespace",
			Data: SerializableNamespaceValue{
				Name:    v.Name,
				Members: members,
			},
		}

	case *NamespaceValue:
		members := map[string]EncodedValue{}

		for name, member := range v.Members {
			members[name] = EncodeValue(member)
		}

		return EncodedValue{
			Type: "namespace",
			Data: SerializableNamespaceValue{
				Name:    v.Name,
				Members: members,
			},
		}

	case NamespaceMemberRef:
		return EncodedValue{
			Type: "namespaceRef",
			Data: SerializableNamespaceMemberRef{
				GlobalName: v.GlobalName,
			},
		}

	case *NamespaceMemberRef:
		return EncodedValue{
			Type: "namespaceRef",
			Data: SerializableNamespaceMemberRef{
				GlobalName: v.GlobalName,
			},
		}

	case Class:
		return EncodedValue{
			Type: "class",
			Data: v,
		}

	case *Class:
		return EncodedValue{
			Type: "class",
			Data: *v,
		}

	case TryInfo:
		return EncodedValue{
			Type: "try",
			Data: v,
		}

	case *ArrayValue:
		return EncodedValue{
			Type: "arrayValue",
			Data: v,
		}

	case *BufferValue:
		obfuscatedBytes := xor(v.Bytes, 0x5A)
		return EncodedValue{
			Type: "bufferValue",
			Data: BufferValue{Bytes: obfuscatedBytes},
		}

	case ObjectValue:
		members := map[string]EncodedValue{}

		for key, val := range v {
			members[key.(string)] = EncodeValue(val)
		}

		return EncodedValue{
			Type: "objectValue",
			Data: members,
		}

	default:
		LangError(ErrorRuntime, "cannot encode bytecode value: %T", value)
		return EncodedValue{Type: "nil"}
	}
}

func DecodeValue(value EncodedValue) any {
	switch value.Type {
	case "nil":
		return nil

	case "int":
		return int(toFloat64(value.Data))

	case "int64":
		return int64(toFloat64(value.Data))

	case "float":
		return toFloat64(value.Data)

	case "string":
		var obfuscated []byte
		decodeInto(value.Data, &obfuscated)

		original := xor(obfuscated, 0x5A)
		return string(original)

	case "bool":
		return value.Data.(bool)

	case "value":
		var result Value
		decodeInto(value.Data, &result)
		return result

	case "incLocal":
		var result IncrementInfo
		decodeInto(value.Data, &result)
		return result

	case "assignLocal":
		var result AssignLocalInfo
		decodeInto(value.Data, &result)
		return result

	case "try":
		var result TryInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpLocalGEConst":
		var result JumpLocalGEConstInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpLocalGELocal":
		var result JumpLocalGELocalInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpLocalGTLocal":
		var result JumpLocalGTLocalInfo
		decodeInto(value.Data, &result)
		return result

	case "addLocalStore":
		var result AddLocalLocalStoreInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpLocalGTConst":
		var result JumpLocalGTConstInfo
		decodeInto(value.Data, &result)
		return result

	case "callDirectSubConst":
		var result CallDirectSubConstInfo
		decodeInto(value.Data, &result)
		return result

	case "variable":
		var result VariableInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpModLocalLocalNotZero":
		var result JumpModLocalLocalNotZeroInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpModLocalConstNotZero":
		var result JumpModLocalConstNotZeroInfo
		decodeInto(value.Data, &result)
		return result

	case "call":
		var result CallInfo
		decodeInto(value.Data, &result)
		return result

	case "directCall":
		var result DirectCallInfo
		decodeInto(value.Data, &result)
		return result

	case "builtinCall":
		var result BuiltinCallInfo
		decodeInto(value.Data, &result)
		return result

	case "class":
		var result Class
		decodeInto(value.Data, &result)
		return result

	case "methodCall":
		var result MethodCallInfo
		decodeInto(value.Data, &result)
		return result

	case "methodLocalCall":
		var result MethodLocalCallInfo
		decodeInto(value.Data, &result)
		return result

	case "arrayLocalCall":
		var result ArrayLocalCallInfo
		decodeInto(value.Data, &result)
		return result

	case "arrayValue":
		var result *ArrayValue
		decodeInto(value.Data, &result)
		return result

	case "bufferValue":
		var result BufferValue
		decodeInto(value.Data, &result)

		result.Bytes = xor(result.Bytes, 0x5A)
		return &result

	case "arrayLocalMulConst":
		var result ArrayLocalMulConstInfo
		decodeInto(value.Data, &result)
		return result

	case "propertyLocal":
		var result PropertyLocalInfo
		decodeInto(value.Data, &result)
		return result

	case "propertyLocalAssign":
		var result PropertyLocalAssignInfo
		decodeInto(value.Data, &result)
		return result

	case "localConst":
		var result LocalConstInfo
		decodeInto(value.Data, &result)
		return result

	case "interpolate":
		var result InterpolateInfo
		decodeInto(value.Data, &result)
		return result

	case "closure":
		var result ClosureInfo
		decodeInto(value.Data, &result)
		return result

	case "object":
		var result ObjectInfo
		decodeInto(value.Data, &result)
		return result

	case "objectValue":
		raw := map[string]EncodedValue{}
		decodeInto(value.Data, &raw)

		obj := ObjectValue{}

		for key, encoded := range raw {
			obj[key] = ToValue(DecodeValue(encoded))
		}

		return NewNative(obj)

	case "array":
		var result ArrayInfo
		decodeInto(value.Data, &result)
		return result

	case "functionValue":
		var result FunctionValue
		decodeInto(value.Data, &result)
		return result

	case "null":
		return NewNull()

	case "undefined":
		return NewUndefined()

	case "namespace":
		var data SerializableNamespaceValue
		decodeInto(value.Data, &data)

		members := map[string]Value{}

		for name, encodedMember := range data.Members {
			members[name] = ToValue(DecodeValue(encodedMember))
		}

		return NamespaceValue{
			Name:    data.Name,
			Members: members,
		}

	case "namespaceRef":
		var data SerializableNamespaceMemberRef
		decodeInto(value.Data, &data)

		return NamespaceMemberRef{
			GlobalName: data.GlobalName,
		}

	default:
		LangError(ErrorRuntime, "unknown encoded value type: %s", value.Type)
		return nil
	}
}

func decodeInto(data any, target any) {
	bytes, err := json.Marshal(data)
	if err != nil {
		LangError(ErrorRuntime, "failed to re-encode bytecode value: %v", err)
	}

	err = json.Unmarshal(bytes, target)
	if err != nil {
		LangError(ErrorRuntime, "failed to decode bytecode value: %v", err)
	}
}

func toFloat64(value any) float64 {
	switch number := value.(type) {
	case int:
		return float64(number)
	case int8:
		return float64(number)
	case int16:
		return float64(number)
	case int32:
		return float64(number)
	case int64:
		return float64(number)
	case uint:
		return float64(number)
	case uint8:
		return float64(number)
	case uint16:
		return float64(number)
	case uint32:
		return float64(number)
	case uint64:
		return float64(number)
	case float32:
		return float64(number)
	case float64:
		return number
	}

	LangError(ErrorRuntime, "expected bytecode number, got %T", value)
	return 0
}

func xor(data []byte, key byte) []byte {
	result := make([]byte, len(data))
	for i := range data {
		result[i] = data[i] ^ key
	}
	return result
}
