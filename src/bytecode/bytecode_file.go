package bytecode

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"encoding/json"
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
	ID             int                       `json:"id"`
	Name           string                    `json:"name"`
	Params         []SerializableParam       `json:"params"`
	ReturnType     TypeHint                  `json:"returnType"`
	LocalCount     int                       `json:"localCount"`
	Captures       []CapturedVar             `json:"captures"`
	Instructions   []SerializableInstruction `json:"instructions"`
	StatementCount int                       `json:"statementCount"`
	Async          bool                      `json:"async"`
	HasDefaults    bool                      `json:"hasDefaults"`
	HasTypeHints   bool                      `json:"hasTypeHints"`
}

type SerializableInterface struct {
	Name           string              `json:"name"`
	TypeParameters []string            `json:"typeParameters,omitempty"`
	Extends        []string            `json:"extends,omitempty"`
	Fields         map[string]TypeHint `json:"fields"`
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
	Implements     []string                 `json:"implements"`
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
		encodedDefault := EncodedValue{Type: "null"}

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
		defaultValue := NewNull()

		if param.HasDefault {
			decoded := DecodeValue(param.DefaultValue)

			if valStruct, ok := decoded.(TinyValue); ok {
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

func SaveBytecode(path string, main []Instruction, functions map[string]Function, classes map[string]Class, interfaces map[string]Interface, globalIndex map[string]int, cache bool, obfuscate bool) {
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
			ID:             fn.ID,
			Name:           fn.Name,
			Params:         serializeParams(fn.Params),
			ReturnType:     fn.ReturnType,
			LocalCount:     fn.LocalCount,
			Captures:       fn.Captures,
			Instructions:   serializeInstructions(fn.Instructions, cache),
			StatementCount: fn.StatementCount,
			HasDefaults:    fn.HasDefaults,
			HasTypeHints:   fn.HasTypeHints,
			Async:          fn.Async,
		}
	}

	for name, interfaceData := range interfaces {
		file.Interfaces[name] = SerializableInterface{
			Name:           interfaceData.Name,
			TypeParameters: interfaceData.TypeParameters,
			Extends:        interfaceData.Extends,
			Fields:         interfaceData.Fields,
		}
	}

	if obfuscate {
		obfuscateBytecodeFile(&file)
	}

	err := os.WriteFile(path, encodeBytecodeFile(file), 0644)
	if err != nil {
		LangError(ErrorRuntime, "failed to write bytecode file: %v", err)
	}
}

func SaveBytecodeToBytes(main []Instruction, functions map[string]Function, classes map[string]Class, interfaces map[string]Interface, globalIndex map[string]int, cache bool, obfuscate bool) []byte {
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
			ID:             fn.ID,
			Name:           fn.Name,
			Params:         serializeParams(fn.Params),
			ReturnType:     fn.ReturnType,
			LocalCount:     fn.LocalCount,
			Captures:       fn.Captures,
			Instructions:   serializeInstructions(fn.Instructions, cache),
			StatementCount: fn.StatementCount,
			HasDefaults:    fn.HasDefaults,
			HasTypeHints:   fn.HasTypeHints,
			Async:          fn.Async,
		}
	}

	for name, interfaceData := range interfaces {
		file.Interfaces[name] = SerializableInterface{
			Name:           interfaceData.Name,
			TypeParameters: interfaceData.TypeParameters,
			Extends:        interfaceData.Extends,
			Fields:         interfaceData.Fields,
		}
	}

	if obfuscate {
		obfuscateBytecodeFile(&file)
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
			ID:             fn.ID,
			Name:           fn.Name,
			Params:         deserializeParams(fn.Params),
			ReturnType:     fn.ReturnType,
			LocalCount:     fn.LocalCount,
			Captures:       fn.Captures,
			Instructions:   deserializeInstructions(fn.Instructions),
			StatementCount: fn.StatementCount,
			HasDefaults:    fn.HasDefaults,
			HasTypeHints:   fn.HasTypeHints,
			Async:          fn.Async,
		}
	}

	for name, interfaceData := range file.Interfaces {
		interfaces[name] = Interface{
			Name:           interfaceData.Name,
			TypeParameters: interfaceData.TypeParameters,
			Extends:        interfaceData.Extends,
			Fields:         interfaceData.Fields,
		}
	}

	return main, functions, deserializeClasses(file.Classes), interfaces, file.GlobalIndex
}

func obfuscateBytecodeFile(file *BytecodeFile) {
	renameMap := make(map[string]string)

	// 1. Collect all functions
	funcCounter := 1
	for originalName := range file.Functions {
		if strings.HasPrefix(originalName, "__jit_region_") {
			continue
		}
		renameMap[originalName] = "f_" + strconv.Itoa(funcCounter)
		funcCounter++
	}

	// 2. Collect all classes
	classCounter := 1
	for originalName := range file.Classes {
		renameMap[originalName] = "c_" + strconv.Itoa(classCounter)
		classCounter++
	}

	// 3. Collect all interfaces
	interfaceCounter := 1
	for originalName := range file.Interfaces {
		renameMap[originalName] = "i_" + strconv.Itoa(interfaceCounter)
		interfaceCounter++
	}

	// 4. Collect all globals
	globalCounter := 1
	for originalName := range file.GlobalIndex {
		renameMap[originalName] = "g_" + strconv.Itoa(globalCounter)
		globalCounter++
	}

	typeRenameMap := map[string]string{}
	ambiguousTypeNames := map[string]bool{}
	for originalName, newName := range renameMap {
		if _, isClass := file.Classes[originalName]; !isClass {
			if _, isInterface := file.Interfaces[originalName]; !isInterface {
				continue
			}
		}
		typeRenameMap[originalName] = newName
		if dot := strings.LastIndex(originalName, "."); dot >= 0 && dot+1 < len(originalName) {
			shortName := originalName[dot+1:]
			if existing, exists := typeRenameMap[shortName]; exists && existing != newName {
				ambiguousTypeNames[shortName] = true
				delete(typeRenameMap, shortName)
			} else if !ambiguousTypeNames[shortName] {
				typeRenameMap[shortName] = newName
			}
		}
	}

	globalAliasValues := map[string]EncodedValue{}
	collectGlobalAliasValues := func(instructions []SerializableInstruction) {
		var lastConst EncodedValue
		hasLastConst := false
		for _, instr := range instructions {
			if instr.Op == OP_CONST {
				lastConst = instr.Value
				hasLastConst = true
				continue
			}

			if (instr.Op == OP_STORE_GLOBAL || instr.Op == OP_ASSIGN_GLOBAL) && hasLastConst {
				if varInfo, ok := instr.Value.Data.(VariableInfo); ok {
					globalAliasValues[varInfo.Name] = lastConst
				} else if varInfoMap, ok := instr.Value.Data.(map[string]any); ok {
					if name, ok := varInfoMap["Name"].(string); ok {
						globalAliasValues[name] = lastConst
					}
				}
			}

			hasLastConst = false
		}
	}
	collectGlobalAliasValues(file.Main)
	for _, fn := range file.Functions {
		collectGlobalAliasValues(fn.Instructions)
	}

	var encodedClassName func(value EncodedValue, seen map[string]bool) (string, bool)
	encodedClassName = func(value EncodedValue, seen map[string]bool) (string, bool) {
		switch value.Type {
		case "class":
			var classInfo Class
			if encoded, err := json.Marshal(value.Data); err == nil && json.Unmarshal(encoded, &classInfo) == nil && classInfo.Name != "" {
				return classInfo.Name, true
			}
		case "namespaceRef":
			var ref SerializableNamespaceMemberRef
			if encoded, err := json.Marshal(value.Data); err != nil || json.Unmarshal(encoded, &ref) != nil || ref.GlobalName == "" {
				return "", false
			}
			if seen[ref.GlobalName] {
				return "", false
			}
			seen[ref.GlobalName] = true
			if _, exists := file.Classes[ref.GlobalName]; exists {
				return ref.GlobalName, true
			}
			if aliased, exists := globalAliasValues[ref.GlobalName]; exists {
				return encodedClassName(aliased, seen)
			}
		}
		return "", false
	}

	var collectNamespaceTypeAliases func(value EncodedValue)
	collectNamespaceTypeAliases = func(value EncodedValue) {
		switch value.Type {
		case "namespace":
			var ns SerializableNamespaceValue
			switch data := value.Data.(type) {
			case SerializableNamespaceValue:
				ns = data
			case map[string]any:
				if encoded, err := json.Marshal(data); err == nil {
					_ = json.Unmarshal(encoded, &ns)
				}
			default:
				return
			}

			for memberName, member := range ns.Members {
				aliasName := ns.Name + "." + memberName
				switch member.Type {
				case "class", "namespaceRef":
					if className, ok := encodedClassName(member, map[string]bool{}); ok {
						if renamed, exists := renameMap[className]; exists {
							typeRenameMap[aliasName] = renamed
						}
					}
				case "namespace":
					collectNamespaceTypeAliases(member)
				}
			}

		case "objectValue":
			if obj, ok := value.Data.(map[string]EncodedValue); ok {
				for _, member := range obj {
					collectNamespaceTypeAliases(member)
				}
			}
		}
	}

	for _, instr := range file.Main {
		collectNamespaceTypeAliases(instr.Value)
	}
	for _, fn := range file.Functions {
		for _, instr := range fn.Instructions {
			collectNamespaceTypeAliases(instr.Value)
		}
	}

	var resolveTypeName func(name string) string
	resolveTypeName = func(name string) string {
		if newName, exists := typeRenameMap[name]; exists {
			return newName
		}
		if dot := strings.LastIndex(name, "."); dot >= 0 && dot+1 < len(name) {
			shortName := name[dot+1:]
			if newName, exists := typeRenameMap[shortName]; exists {
				return newName
			}
		}
		return name
	}

	renameTypeName := func(name string) string {
		if newName := resolveTypeName(name); newName != name {
			return newName
		}
		if strings.Contains(name, "|") {
			parts := strings.Split(name, "|")
			changed := false
			for i, part := range parts {
				trimmed := strings.TrimSpace(part)
				if newName := resolveTypeName(trimmed); newName != trimmed {
					parts[i] = strings.Replace(part, trimmed, newName, 1)
					changed = true
				}
			}
			if changed {
				return strings.Join(parts, "|")
			}
		}
		if strings.Contains(name, ":") {
			parts := strings.Split(name, ":")
			changed := false
			for i, part := range parts {
				if newName := resolveTypeName(part); newName != part {
					parts[i] = newName
					changed = true
				}
			}
			if changed {
				return strings.Join(parts, ":")
			}
		}
		return name
	}

	renameTypeHint := func(hint TypeHint) TypeHint {
		hint.Name = renameTypeName(hint.Name)
		for i, typ := range hint.Types {
			hint.Types[i] = renameTypeName(typ)
		}
		return hint
	}

	// Rename functions
	newFunctions := make(map[string]SerializableFunction)
	for originalName, fn := range file.Functions {
		newName, exists := renameMap[originalName]
		if exists {
			fn.Name = newName
		} else {
			newName = originalName
		}
		fn.ReturnType = renameTypeHint(fn.ReturnType)
		for i := range fn.Params {
			fn.Params[i].TypeHint = renameTypeHint(fn.Params[i].TypeHint)
		}
		newFunctions[newName] = fn
	}
	file.Functions = newFunctions

	// Rename classes
	newClasses := make(map[string]SerializableClass)
	for originalName, class := range file.Classes {
		newName, exists := renameMap[originalName]
		if exists {
			class.Name = newName
		} else {
			newName = originalName
		}
		for methodName, functionName := range class.Methods {
			if renamedFunction, exists := renameMap[functionName]; exists {
				class.Methods[methodName] = renamedFunction
			}
		}
		for i, name := range class.Implements {
			class.Implements[i] = renameTypeName(name)
		}
		for i, name := range class.Embeds {
			class.Embeds[i] = renameTypeName(name)
		}
		for i := range class.Fields {
			class.Fields[i].TypeHint = renameTypeHint(class.Fields[i].TypeHint)
		}
		newClasses[newName] = class
	}
	file.Classes = newClasses

	// Rename interfaces
	newInterfaces := make(map[string]SerializableInterface)
	for originalName, inter := range file.Interfaces {
		newName, exists := renameMap[originalName]
		if exists {
			inter.Name = newName
		} else {
			newName = originalName
		}
		for i, name := range inter.Extends {
			inter.Extends[i] = renameTypeName(name)
		}
		for fieldName, hint := range inter.Fields {
			inter.Fields[fieldName] = renameTypeHint(hint)
		}
		newInterfaces[newName] = inter
	}
	file.Interfaces = newInterfaces

	// Rename globals
	newGlobalIndex := make(map[string]int)
	for originalName, index := range file.GlobalIndex {
		newName, exists := renameMap[originalName]
		if exists {
			newGlobalIndex[newName] = index
		} else {
			newGlobalIndex[originalName] = index
		}
	}
	file.GlobalIndex = newGlobalIndex

	renameFunctionValue := func(value EncodedValue) EncodedValue {
		if value.Type != "functionValue" {
			return value
		}

		fn, ok := value.Data.(FunctionValue)
		if !ok {
			return value
		}

		originalName := string(xor([]byte(fn.Name), 0x5A))
		if newName, exists := renameMap[originalName]; exists {
			fn.Name = string(xor([]byte(newName), 0x5A))
			value.Data = fn
		}
		return value
	}

	var renameEncodedValue func(value EncodedValue) EncodedValue
	renameEncodedValue = func(value EncodedValue) EncodedValue {
		value = renameFunctionValue(value)

		switch value.Type {
		case "directCall":
			if callInfo, ok := value.Data.(DirectCallInfo); ok {
				if newName, exists := renameMap[callInfo.Name]; exists {
					callInfo.Name = newName
					value.Data = callInfo
				}
			} else if callInfoMap, ok := value.Data.(map[string]any); ok {
				if name, ok := callInfoMap["name"].(string); ok {
					if newName, exists := renameMap[name]; exists {
						callInfoMap["name"] = newName
					}
				}
			}
		case "call":
			if callInfo, ok := value.Data.(CallInfo); ok {
				if newName, exists := renameMap[callInfo.Name]; exists {
					callInfo.Name = newName
					value.Data = callInfo
				}
			} else if callInfoMap, ok := value.Data.(map[string]any); ok {
				if name, ok := callInfoMap["Name"].(string); ok {
					if newName, exists := renameMap[name]; exists {
						callInfoMap["Name"] = newName
					}
				}
			}
		case "callDirectSubConst":
			if callInfo, ok := value.Data.(CallDirectSubConstInfo); ok {
				if newName, exists := renameMap[callInfo.FnName]; exists {
					callInfo.FnName = newName
					value.Data = callInfo
				}
			} else if callInfoMap, ok := value.Data.(map[string]any); ok {
				if fnName, ok := callInfoMap["FnName"].(string); ok {
					if newName, exists := renameMap[fnName]; exists {
						callInfoMap["FnName"] = newName
					}
				}
			}
		case "variable":
			if varInfo, ok := value.Data.(VariableInfo); ok {
				if newName, exists := renameMap[varInfo.Name]; exists {
					varInfo.Name = newName
				}
				varInfo.TypeHint = renameTypeHint(varInfo.TypeHint)
				value.Data = varInfo
			} else if varInfoMap, ok := value.Data.(map[string]any); ok {
				if name, ok := varInfoMap["Name"].(string); ok {
					if newName, exists := renameMap[name]; exists {
						varInfoMap["Name"] = newName
					}
				}
				if hint, ok := varInfoMap["TypeHint"].(TypeHint); ok {
					varInfoMap["TypeHint"] = renameTypeHint(hint)
				}
			}
		case "closure":
			if closureInfo, ok := value.Data.(ClosureInfo); ok {
				if newName, exists := renameMap[closureInfo.Name]; exists {
					closureInfo.Name = newName
					value.Data = closureInfo
				}
			} else if closureInfoMap, ok := value.Data.(map[string]any); ok {
				if name, ok := closureInfoMap["Name"].(string); ok {
					if newName, exists := renameMap[name]; exists {
						closureInfoMap["Name"] = newName
					}
				}
			}
		case "class":
			if classInfo, ok := value.Data.(Class); ok {
				if newName, exists := renameMap[classInfo.Name]; exists {
					classInfo.Name = newName
					value.Data = classInfo
				}
			} else if classInfoMap, ok := value.Data.(map[string]any); ok {
				if name, ok := classInfoMap["Name"].(string); ok {
					if newName, exists := renameMap[name]; exists {
						classInfoMap["Name"] = newName
					}
				}
			}
		case "namespace":
			if ns, ok := value.Data.(SerializableNamespaceValue); ok {
				newMembers := map[string]EncodedValue{}
				for key, val := range ns.Members {
					newMembers[key] = renameEncodedValue(val)
				}
				ns.Members = newMembers
				value.Data = ns
			} else if nsMap, ok := value.Data.(map[string]any); ok {
				if members, ok := nsMap["members"].(map[string]any); ok {
					for key, val := range members {
						if encodedValJson, err := json.Marshal(val); err == nil {
							var encodedVal EncodedValue
							if json.Unmarshal(encodedValJson, &encodedVal) == nil {
								members[key] = renameEncodedValue(encodedVal)
							}
						}
					}
				}
			}
		case "namespaceRef":
			if ref, ok := value.Data.(SerializableNamespaceMemberRef); ok {
				if newName, exists := renameMap[ref.GlobalName]; exists {
					ref.GlobalName = newName
					value.Data = ref
				}
			} else if refMap, ok := value.Data.(map[string]any); ok {
				if globalName, ok := refMap["globalName"].(string); ok {
					if newName, exists := renameMap[globalName]; exists {
						refMap["globalName"] = newName
					}
				}
			}
		case "objectValue":
			if obj, ok := value.Data.(map[string]EncodedValue); ok {
				newObj := map[string]EncodedValue{}
				for key, val := range obj {
					newObj[key] = renameEncodedValue(val)
				}
				value.Data = newObj
			}
		}
		return value
	}

	// Rename in instructions
	renameInInstructions := func(instructions []SerializableInstruction) {
		for i := range instructions {
			instr := &instructions[i]
			instr.Value = renameEncodedValue(instr.Value)
		}
	}

	renameInInstructions(file.Main)

	for name, fn := range file.Functions {
		renameInInstructions(fn.Instructions)
		file.Functions[name] = fn
	}
}

func getBytecodeKey() []byte {
	key := make([]byte, 32)
	for i := 0; i < 32; i++ {
		key[i] = byte((i * 59) ^ 0xA5 ^ (32 - i))
	}
	return key
}

func encryptBytecodeBytes(plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(getBytecodeKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decryptBytecodeBytes(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(getBytecodeKey())
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, fmt.Errorf("ciphertext too short")
	}
	nonce, encrypted := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, encrypted, nil)
}

func encodeBytecodeFile(file BytecodeFile) []byte {
	data, err := msgpack.Marshal(file)
	if err != nil {
		LangError(ErrorRuntime, "failed to encode bytecode: %v", err)
	}

	encrypted, err := encryptBytecodeBytes(data)
	if err != nil {
		LangError(ErrorRuntime, "failed to encrypt bytecode: %v", err)
	}

	result := make([]byte, 0, len(bytecodeMagic)+len(encrypted))
	result = append(result, bytecodeMagic...)
	result = append(result, encrypted...)
	return result
}

func decodeBytecodeFile(data []byte, file *BytecodeFile) {
	if bytes.HasPrefix(data, bytecodeMagic) {
		encrypted := data[len(bytecodeMagic):]
		decrypted, err := decryptBytecodeBytes(encrypted)
		if err != nil {
			LangError(ErrorRuntime, "failed to decrypt bytecode file: %v", err)
		}

		err = msgpack.Unmarshal(decrypted, file)
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
			Implements:     class.Implements,
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
			Implements:     class.Implements,
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
		intVal, hasInt := asIntInternal(val)

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

	case []byte:
		obfuscatedBytes := xor(v, 0x5A)
		return EncodedValue{
			Type: "bytes",
			Data: obfuscatedBytes,
		}

	case TinyValue:
		if v.IsInt {
			return EncodeValue(v.AsInt)
		}
		return EncodeValue(v.Value)

	case VariableInfo:
		return EncodedValue{Type: "variable", Data: v}

	case PrintInfo:
		return EncodedValue{Type: "print", Data: v}

	case LocalConstOpInfo:
		return EncodedValue{Type: "localConstOp", Data: v}

	case ArrayIndexConstOpInfo:
		return EncodedValue{Type: "arrayIndexConstOp", Data: v}

	case AddLocalGlobalGlobalStoreInfo:
		return EncodedValue{Type: "addLocalGlobalGlobalStore", Data: v}

	case AddLocalArrayIndexStoreInfo:
		return EncodedValue{Type: "addLocalArrayIndexStore", Data: v}

	case PropertyLocalConstAssignInfo:
		return EncodedValue{Type: "propertyLocalConstAssign", Data: v}

	case PropertyLocalPropertyAssignInfo:
		return EncodedValue{Type: "propertyLocalPropertyAssign", Data: v}

	case AddLocalPropertiesStoreInfo:
		return EncodedValue{Type: "addLocalPropertiesStore", Data: v}

	case JumpPropertyLocalInfo:
		return EncodedValue{Type: "jumpPropertyLocal", Data: v}

	case ArrayIndexLocalStoreInfo:
		return EncodedValue{Type: "arrayIndexLocalStore", Data: v}

	case NativeCallInfo:
		return EncodedValue{Type: "nativeCallInfo", Data: v}

	case CallInfo:
		return EncodedValue{Type: "call", Data: v}

	case SpreadCallInfo:
		return EncodedValue{Type: "spreadCall", Data: v}

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
		v.Name = string(xor([]byte(v.Name), 0x5A))
		return EncodedValue{Type: "functionValue", Data: v}

	case NullValue:
		return EncodedValue{Type: "null"}

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

	case InterfaceValue:
		return EncodedValue{
			Type: "interfaceValue",
			Data: v,
		}

	case *InterfaceValue:
		return EncodedValue{
			Type: "interfaceValue",
			Data: *v,
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

	case "bytes":
		var obfuscated []byte
		decodeInto(value.Data, &obfuscated)

		original := xor(obfuscated, 0x5A)
		return original

	case "value":
		var result TinyValue
		decodeInto(value.Data, &result)
		return result

	case "nativeCallInfo":
		var result NativeCallInfo
		decodeInto(value.Data, &result)
		return result

	case "localConstOp":
		var result LocalConstOpInfo
		decodeInto(value.Data, &result)
		return result

	case "propertyLocalConstAssign":
		var result PropertyLocalConstAssignInfo
		decodeInto(value.Data, &result)
		return result

	case "propertyLocalPropertyAssign":
		var result PropertyLocalPropertyAssignInfo
		decodeInto(value.Data, &result)
		return result

	case "addLocalPropertiesStore":
		var result AddLocalPropertiesStoreInfo
		decodeInto(value.Data, &result)
		return result

	case "jumpPropertyLocal":
		var result JumpPropertyLocalInfo
		decodeInto(value.Data, &result)
		return result

	case "arrayIndexConstOp":
		var result ArrayIndexConstOpInfo
		decodeInto(value.Data, &result)
		return result

	case "arrayIndexLocalStore":
		var result ArrayIndexLocalStoreInfo
		decodeInto(value.Data, &result)
		return result

	case "addLocalGlobalGlobalStore":
		var result AddLocalGlobalGlobalStoreInfo
		decodeInto(value.Data, &result)
		return result

	case "addLocalArrayIndexStore":
		var result AddLocalArrayIndexStoreInfo
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

	case "print":
		var result PrintInfo
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

	case "spreadCall":
		var result SpreadCallInfo
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

		return obj

	case "array":
		var result ArrayInfo
		decodeInto(value.Data, &result)
		return result

	case "functionValue":
		var result FunctionValue
		decodeInto(value.Data, &result)

		result.Name = string(xor([]byte(result.Name), 0x5A))
		return result

	case "null":
		return NewNull()

	case "namespace":
		var data SerializableNamespaceValue
		decodeInto(value.Data, &data)

		members := map[string]TinyValue{}

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

	case "interfaceValue":
		var data InterfaceValue
		decodeInto(value.Data, &data)
		return data

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

func asIntInternal(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int8:
		return int(n), true
	case int16:
		return int(n), true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case uint:
		return int(n), true
	case uint8:
		return int(n), true
	case uint16:
		return int(n), true
	case uint32:
		return int(n), true
	case uint64:
		return int(n), true
	default:
		return 0, false
	}
}
