package bytecode

import (
	"encoding/json"
	"os"

	. "language.com/src/tinyerrors"
	. "language.com/src/vm"
)

const BytecodeVersion = 1

type BytecodeFile struct {
	Version   int                             `json:"version"`
	Main      []SerializableInstruction       `json:"main"`
	Functions map[string]SerializableFunction `json:"functions"`
	Classes   map[string]Class                `json:"classes"`
}

type SerializableFunction struct {
	Name         string                    `json:"name"`
	Params       []Param                   `json:"params"`
	ReturnType   TypeHint                  `json:"returnType"`
	LocalCount   int                       `json:"localCount"`
	Captures     []CapturedVar             `json:"captures"`
	Instructions []SerializableInstruction `json:"instructions"`
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

func SaveBytecode(path string, main []Instruction, functions map[string]Function, classes map[string]Class) {
	file := BytecodeFile{
		Version:   BytecodeVersion,
		Main:      serializeInstructions(main),
		Functions: map[string]SerializableFunction{},
		Classes:   classes,
	}

	for name, fn := range functions {
		file.Functions[name] = SerializableFunction{
			Name:         fn.Name,
			Params:       fn.Params,
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: serializeInstructions(fn.Instructions),
		}
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		LangError(ErrorRuntime, "failed to encode bytecode: %v", err)
	}

	err = os.WriteFile(path, data, 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write bytecode file: %v", err)
	}
}

func SaveBytecodeToBytes(main []Instruction, functions map[string]Function, classes map[string]Class) []byte {
	file := BytecodeFile{
		Version:   BytecodeVersion,
		Main:      serializeInstructions(main),
		Functions: map[string]SerializableFunction{},
		Classes:   classes,
	}

	for name, fn := range functions {
		file.Functions[name] = SerializableFunction{
			Name:         fn.Name,
			Params:       fn.Params,
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: serializeInstructions(fn.Instructions),
		}
	}

	bytes, err := json.Marshal(file)
	if err != nil {
		LangError(ErrorRuntime, "failed to encode bytecode: %v", err)
	}

	return bytes
}

func LoadBytecode(path string) ([]Instruction, map[string]Function, map[string]Class) {
	data, err := os.ReadFile(path)
	if err != nil {
		LangError(ErrorRuntime, "failed to read bytecode file: %v", err)
	}

	return LoadBytecodeFromBytes(data)
}

func LoadBytecodeFromBytes(data []byte) ([]Instruction, map[string]Function, map[string]Class) {
	var file BytecodeFile

	err := json.Unmarshal(data, &file)
	if err != nil {
		LangError(ErrorRuntime, "failed to decode bytecode file: %v", err)
	}

	if file.Version != BytecodeVersion {
		LangError(ErrorRuntime, "unsupported bytecode version: %d", file.Version)
	}

	main := deserializeInstructions(file.Main)

	functions := map[string]Function{}

	for name, fn := range file.Functions {
		functions[name] = Function{
			Name:         fn.Name,
			Params:       fn.Params,
			ReturnType:   fn.ReturnType,
			LocalCount:   fn.LocalCount,
			Captures:     fn.Captures,
			Instructions: deserializeInstructions(fn.Instructions),
		}
	}

	return main, functions, file.Classes
}

func serializeInstructions(instructions []Instruction) []SerializableInstruction {
	result := make([]SerializableInstruction, len(instructions))

	for i, instr := range instructions {
		result[i] = SerializableInstruction{
			Op:     instr.Op,
			Value:  EncodeValue(instr.Value),
			File:   instr.File,
			Line:   instr.Line,
			Column: instr.Column,
		}
	}

	return result
}

func deserializeInstructions(instructions []SerializableInstruction) []Instruction {
	result := make([]Instruction, len(instructions))

	for i, instr := range instructions {
		result[i] = Instruction{
			Op:     instr.Op,
			Value:  DecodeValue(instr.Value),
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
		return EncodedValue{Type: "string", Data: v}

	case bool:
		return EncodedValue{Type: "bool", Data: v}

	case VariableInfo:
		return EncodedValue{Type: "variable", Data: v}

	case CallInfo:
		return EncodedValue{Type: "call", Data: v}

	case BuiltinCallInfo:
		return EncodedValue{Type: "builtinCall", Data: v}

	case MethodCallInfo:
		return EncodedValue{Type: "methodCall", Data: v}

	case InterpolateInfo:
		return EncodedValue{Type: "interpolate", Data: v}

	case ObjectInfo:
		return EncodedValue{Type: "object", Data: v}

	case ClosureInfo:
		return EncodedValue{Type: "closure", Data: v}

	case ArrayInfo:
		return EncodedValue{Type: "array", Data: v}

	case FunctionValue:
		return EncodedValue{Type: "functionValue", Data: v}

	case NullValue:
		return EncodedValue{Type: "null"}

	case UndefinedValue:
		return EncodedValue{Type: "undefined"}

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
		return value.Data.(string)

	case "bool":
		return value.Data.(bool)

	case "variable":
		var result VariableInfo
		decodeInto(value.Data, &result)
		return result

	case "call":
		var result CallInfo
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
			obj[key] = DecodeValue(encoded)
		}

		return obj

	case "array":
		var result ArrayInfo
		decodeInto(value.Data, &result)
		return result

	case "functionValue":
		var result FunctionValue
		decodeInto(value.Data, &result)
		return result

	case "null":
		return NullValue{}

	case "undefined":
		return UndefinedValue{}

	case "namespace":
		var data SerializableNamespaceValue
		decodeInto(value.Data, &data)

		members := map[string]Value{}

		for name, encodedMember := range data.Members {
			members[name] = DecodeValue(encodedMember)
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
	number, ok := value.(float64)
	if !ok {
		LangError(ErrorRuntime, "expected JSON number, got %T", value)
	}

	return number
}
