//go:generate go run ../cmd/gen_hints

package vm

import (
	"fmt"
	"slices"
	"sort"
	"strings"
)

type TypeHint struct {
	Name   string              `json:"name"`
	Types  []string            `json:"types,omitempty"`
	Fields map[string]TypeHint `json:"fields,omitempty"`
	Range  SourceRange         `json:"range,omitempty"`
}

func (t TypeHint) IsEmpty() bool {
	return t.Name == "" && len(t.Types) == 0 && len(t.Fields) == 0
}

func (t TypeHint) AllTypes() []string {
	if len(t.Types) > 0 {
		return t.Types
	}

	if t.Name != "" {
		return []string{t.Name}
	}

	return []string{}
}

func (t TypeHint) String() string {
	if len(t.Fields) > 0 {
		parts := make([]string, 0, len(t.Fields))
		for name, field := range t.Fields {
			parts = append(parts, name+": "+field.String())
		}
		sort.Strings(parts)
		return "{" + strings.Join(parts, ", ") + "}"
	}

	types := t.AllTypes()
	if len(types) == 0 {
		return "any"
	}

	return strings.Join(types, " | ")
}

func CheckTypeHint(value TinyValue, hint TypeHint, interfaces map[string]Interface) (bool, string) {
	return CheckTypeHintWithGlobals(value, hint, interfaces, nil, nil)
}

func CheckTypeHintWithGlobals(value TinyValue, hint TypeHint, interfaces map[string]Interface, globals []TinyValue, globalNames map[string]int) (bool, string) {
	if hint.IsEmpty() || hint.Name == "any" {
		return true, ""
	}
	if len(hint.Fields) > 0 {
		return checkStructuralFieldsWithGlobals(value, hint.Fields, interfaces, globals, globalNames, "structural type")
	}

	var lastReason string
	for _, typ := range hint.AllTypes() {
		ok, reason := checkSingleTypeHintWithGlobals(value, typ, interfaces, globals, globalNames)
		if ok {
			return true, ""
		}
		lastReason = reason
	}

	return false, lastReason
}

func checkStructuralFieldsWithGlobals(value TinyValue, fields map[string]TypeHint, interfaces map[string]Interface, globals []TinyValue, globalNames map[string]int, label string) (bool, string) {
	_, objectLike := tinyValueAsInterfaceObject(value)
	if !objectLike {
		if _, _, ok := tinyValueInterfaceField(value, ""); !ok {
			return false, ": expected object to match " + label
		}
	}

	for fieldName, expectedHint := range fields {
		optional := slices.Contains(expectedHint.AllTypes(), "null")
		val, hasField, okObject := tinyValueInterfaceField(value, fieldName)
		if !okObject {
			return false, ": expected object to match " + label
		}
		if !hasField {
			if optional {
				continue
			}
			return false, fmt.Sprintf(" (missing field '%s')", fieldName)
		}
		if ok, subReason := CheckTypeHintWithGlobals(val, expectedHint, interfaces, globals, globalNames); !ok {
			return false, fmt.Sprintf(" (field '%s' type mismatch%s)", fieldName, subReason)
		}
	}

	return true, ""
}

func tinyValueAsInterfaceObject(value TinyValue) (ObjectValue, bool) {
	if value.IsInt {
		return nil, false
	}

	switch obj := value.Value.(type) {
	case *InstanceValue:
		if obj == nil {
			return nil, false
		}
		return obj.Fields, true
	case ObjectValue:
		return obj, true
	case *ObjectValue:
		if obj == nil {
			return nil, false
		}
		return *obj, true
	case WasmObjectValue:
		if obj.VM == nil {
			return nil, false
		}
		return obj.VM.wasmObjectToObjectValue(obj)
	case *WasmObjectValue:
		if obj == nil || obj.VM == nil {
			return nil, false
		}
		return obj.VM.wasmObjectToObjectValue(*obj)
	default:
		return nil, false
	}
}

func wasmObjectHasShapeField(vm *VM, obj WasmObjectValue, fieldName string) bool {
	if vm == nil || vm.jitModule == nil {
		return false
	}

	base := uint32(obj.Address)
	shapeIDF, ok := vm.readWasmFloatMaybe(base + 8)
	if !ok {
		return false
	}

	shapeID := int(shapeIDF)
	if shapeID < 0 || shapeID >= len(vm.objectShapes) {
		return false
	}

	for _, name := range vm.objectShapes[shapeID] {
		if name == fieldName {
			return true
		}
	}

	return false
}

func wasmObjectInterfaceField(obj WasmObjectValue, fieldName string) (TinyValue, bool) {
	vm := obj.VM
	if vm == nil || vm.jitModule == nil {
		return TinyValue{}, false
	}

	base := uint32(obj.Address)
	if tag, ok := vm.readWasmFloatMaybe(base); !ok || tag != 4.0 {
		return TinyValue{}, false
	}

	offset, hasOffset := vm.propertyOffsets[fieldName]
	if !hasOffset {
		return TinyValue{}, false
	}

	tag, okTag := vm.readWasmFloatMaybe(base + offset)
	val, okVal := vm.readWasmFloatMaybe(base + offset + 8)
	if !okTag || !okVal {
		return TinyValue{}, false
	}

	if wasmObjectHasShapeField(vm, obj, fieldName) {
		return vm.wasmTaggedValueToTinyValue(tag, val, 1), true
	}

	// Some older JIT object-shape metadata can be incomplete even though the
	// property was physically written at its global property offset. Interface
	// checks must not report a present JIT field as missing just because the shape
	// list is stale or incomplete. If the slot contains a non-null tagged value,
	// treat it as present.
	if tag != 0.0 || val != 0.0 {
		return vm.wasmTaggedValueToTinyValue(tag, val, 1), true
	}

	return TinyValue{}, false
}

func tinyValueInterfaceField(value TinyValue, fieldName string) (TinyValue, bool, bool) {
	if value.IsInt {
		return TinyValue{}, false, false
	}

	switch obj := value.Value.(type) {
	case *InstanceValue:
		if obj == nil {
			return TinyValue{}, false, false
		}
		val, ok := obj.Fields[fieldName]
		return val, ok, true
	case ObjectValue:
		val, ok := obj[fieldName]
		return val, ok, true
	case *ObjectValue:
		if obj == nil {
			return TinyValue{}, false, false
		}
		val, ok := (*obj)[fieldName]
		return val, ok, true
	case WasmObjectValue:
		val, ok := wasmObjectInterfaceField(obj, fieldName)
		return val, ok, obj.VM != nil
	case *WasmObjectValue:
		if obj == nil {
			return TinyValue{}, false, false
		}
		val, ok := wasmObjectInterfaceField(*obj, fieldName)
		return val, ok, obj.VM != nil
	default:
		return TinyValue{}, false, false
	}
}

func checkSingleTypeHintWithGlobals(value TinyValue, hint string, interfaces map[string]Interface, globals []TinyValue, globalNames map[string]int) (bool, string) {
	if hint == "array" {
		hint = "array:any"
	}
	if strings.HasPrefix(hint, "function(") {
		hint = "function"
	}

	if strings.HasPrefix(hint, "array:") {
		var arrObj []TinyValue
		switch v := value.Value.(type) {
		case ArrayValue:
			arrObj = v.Elements
		case *ArrayValue:
			arrObj = v.Elements
		case WasmArrayValue:
			if v.VM != nil {
				lengthF, ok := v.VM.readWasmFloatMaybe(uint32(v.Address) + 8)
				if ok {
					elemPtrF, ok := v.VM.readWasmFloatMaybe(uint32(v.Address) + 16)
					if ok {
						length := int(lengthF)
						elemPtr := uint32(elemPtrF)
						arrObj = make([]TinyValue, length)
						for i := 0; i < length; i++ {
							addr := elemPtr + uint32(i*16)
							tag := v.VM.ReadWasmFloat(addr)
							val := v.VM.ReadWasmFloat(addr + 8)

							var element TinyValue
							switch tag {
							case 1.0:
								element = NewNative(val)
							case 2.0:
								element = NewNative(val != 0.0)
							case 4.0:
								element = NewNative(WasmObjectValue{Address: val, VM: v.VM})
							case 5.0:
								element = NewNative(WasmArrayValue{Address: val, VM: v.VM})
							case 6.0:
								strVal, ok := v.VM.readWasmStringMaybe(uint32(val))
								if ok {
									element = NewNative(strVal)
								} else {
									element = NewNull()
								}
							default:
								element = NewNull()
							}
							arrObj[i] = element
						}
					}
				}
			}
		default:
			return false, ": expected array"
		}

		elementType := strings.TrimPrefix(hint, "array:")
		if elementType == "any" {
			return true, ""
		}
		elementHint := TypeHint{Name: elementType, Types: []string{elementType}}
		for idx, elem := range arrObj {
			if ok, subReason := CheckTypeHintWithGlobals(elem, elementHint, interfaces, globals, globalNames); !ok {
				return false, fmt.Sprintf(" (at index %d: expected %s, got %s%s)", idx, elementType, TypeName(elem), subReason)
			}
		}
		return true, ""
	}

	switch hint {
	case "any":
		return true, ""

	case "number":
		if value.IsInt {
			return true, ""
		}
		switch value.Value.(type) {
		case float64, float32, uint64:
			return true, ""
		default:
			return false, ": expected number, got " + TypeName(value)
		}

	case "string":
		switch value.Value.(type) {
		case string:
			return true, ""
		default:
			return false, ": expected string, got " + TypeName(value)
		}

	case "bool":
		switch value.Value.(type) {
		case bool:
			return true, ""
		default:
			return false, ": expected bool, got " + TypeName(value)
		}

	case "array":
		switch value.Value.(type) {
		case ArrayValue, *ArrayValue, WasmArrayValue:
			return true, ""
		default:
			return false, ""
		}

	case "object":
		switch value.Value.(type) {
		case ObjectValue, *ObjectValue, *InstanceValue, WasmObjectValue, *WasmObjectValue:
			return true, ""
		default:
			return false, ""
		}

	case "function":
		switch value.Value.(type) {
		case FunctionValue, *FunctionValue, *HostFunctionValue, *CallbackFunctionValue:
			return true, ""
		default:
			return false, ""
		}

	case "null":
		if !value.IsInt && (value.Value == nil || TypeName(value) == "null") {
			return true, ""
		}
		return false, ""

	default:
		if strings.HasPrefix(hint, "{") {
			fields := parseStructuralTypeFieldsFromHint(hint)
			return checkStructuralFieldsWithGlobals(value, fields, interfaces, globals, globalNames, "structural type")
		}

		if iface, exists := resolveInterfaceHintWithGlobals(hint, interfaces, globals, globalNames); exists {
			_, objectLike := tinyValueAsInterfaceObject(value)
			if !objectLike {
				if _, _, ok := tinyValueInterfaceField(value, ""); !ok {
					return false, ": expected object to match interface '" + hint + "'"
				}
			}

			for fieldName, expectedHint := range iface.Fields {
				optional := slices.Contains(expectedHint.AllTypes(), "null")

				val, hasField, okObject := tinyValueInterfaceField(value, fieldName)
				if !okObject {
					return false, ": expected object to match interface '" + hint + "'"
				}
				if !hasField {
					if optional {
						continue
					}
					return false, fmt.Sprintf(" (missing field '%s')", fieldName)
				}

				if ok, subReason := CheckTypeHintWithGlobals(val, expectedHint, interfaces, globals, globalNames); !ok {
					return false, fmt.Sprintf(" (field '%s' type mismatch%s)", fieldName, subReason)
				}
			}

			return true, ""
		}

		if inst, ok := instanceValue(value); ok {
			checkHint := hint
			if strings.Contains(checkHint, ":") {
				checkHint = strings.Split(checkHint, ":")[0]
			}

			if inst.ClassName == checkHint || strings.HasSuffix(inst.ClassName, "."+checkHint) {
				return true, ""
			}
		}

		if globalNames != nil && globals != nil {
			if idx, exists := globalNames[hint]; exists && idx < len(globals) {
				if enumObj, ok := globals[idx].Value.(ObjectValue); ok {
					if obj, ok := tinyValueAsInterfaceObject(value); ok {
						enumTag, ok := obj["_enum"]
						if ok {
							enumTagStr, ok := enumTag.Value.(string)
							if ok && (enumTagStr == hint || strings.HasSuffix(enumTagStr, "."+hint)) {
								return true, ""
							}
						}
					}
					for _, memberVal := range enumObj {
						if memberVal.Value == value.Value {
							return true, ""
						}
					}
					return false, fmt.Sprintf(": expected enum %s member", hint)
				}
			} else {
				// Try with suffix (namespaced enum)
				for name, idx := range globalNames {
					if (strings.HasSuffix(name, "."+hint) || name == hint) && idx < len(globals) {
						if enumObj, ok := globals[idx].Value.(ObjectValue); ok {
							if obj, ok := tinyValueAsInterfaceObject(value); ok {
								enumTag, ok := obj["_enum"]
								if ok {
									enumTagStr, ok := enumTag.Value.(string)
									if ok && (enumTagStr == name || enumTagStr == hint || strings.HasSuffix(enumTagStr, "."+hint)) {
										return true, ""
									}
								}
							}
							for _, memberVal := range enumObj {
								if memberVal.Value == value.Value {
									return true, ""
								}
							}
							return false, fmt.Sprintf(": expected enum %s member", hint)
						}
					}
				}
			}
		}

		resolvedHint := resolveStandardTypeAlias(hint, globals, globalNames)
		if TypeName(value) == resolvedHint {
			return true, ""
		}
		return false, ""
	}
}

func substituteTypeHintName(typeName string, subst map[string]string) string {
	if val, exists := subst[typeName]; exists {
		return val
	}
	if strings.Contains(typeName, "|") {
		parts := strings.Split(typeName, "|")
		for i, part := range parts {
			parts[i] = substituteTypeHintName(strings.TrimSpace(part), subst)
		}
		return strings.Join(parts, "|")
	}
	if strings.Contains(typeName, ":") {
		parts := strings.Split(typeName, ":")
		for i, part := range parts {
			parts[i] = substituteTypeHintName(part, subst)
		}
		return strings.Join(parts, ":")
	}
	return typeName
}

func resolveInterfaceHintWithGlobals(hint string, interfaces map[string]Interface, globals []TinyValue, globalNames map[string]int) (Interface, bool) {
	baseName := hint
	typeArgs := []string{}

	if strings.Contains(hint, ":") {
		parts := strings.Split(hint, ":")
		baseName = parts[0]
		typeArgs = parts[1:]
	}

	resolveBase := func(name string) (Interface, bool) {
		if iface, exists := interfaces[name]; exists {
			return iface, true
		}
		if iface, exists := standardInterfaceHints[name]; exists {
			return iface, true
		}
		if iface, exists := resolveStandardInterfaceAlias(name, globals, globalNames); exists {
			return iface, true
		}
		for key, iface := range interfaces {
			if strings.HasSuffix(key, "."+name) {
				return iface, true
			}
		}
		return Interface{}, false
	}

	iface, ok := resolveBase(baseName)
	if !ok {
		if dot := strings.LastIndex(baseName, "."); dot >= 0 {
			iface, ok = resolveBase(baseName[dot+1:])
		}
	}

	if !ok {
		return Interface{}, false
	}

	iface = mergeInterfaceExtends(iface, resolveBase, map[string]bool{})

	if len(typeArgs) > 0 && len(iface.TypeParameters) > 0 {
		subst := map[string]string{}
		for i, tp := range iface.TypeParameters {
			if i < len(typeArgs) {
				subst[tp] = typeArgs[i]
			}
		}

		instantiatedFields := map[string]TypeHint{}
		for fieldName, fieldHint := range iface.Fields {
			instantiatedFields[fieldName] = TypeHint{
				Name: substituteTypeHintName(fieldHint.Name, subst),
			}
		}

		return Interface{
			Name:           iface.Name,
			TypeParameters: iface.TypeParameters,
			Extends:        iface.Extends,
			Fields:         instantiatedFields,
		}, true
	}

	return iface, true
}

func mergeInterfaceExtends(iface Interface, resolveBase func(string) (Interface, bool), visiting map[string]bool) Interface {
	if len(iface.Extends) == 0 {
		return iface
	}

	if visiting[iface.Name] {
		return iface
	}
	visiting[iface.Name] = true
	defer delete(visiting, iface.Name)

	mergedFields := map[string]TypeHint{}
	for _, parentName := range iface.Extends {
		parentBase := parentName
		if strings.Contains(parentBase, ":") {
			parentBase = strings.Split(parentBase, ":")[0]
		}
		parent, ok := resolveBase(parentBase)
		if !ok {
			if dot := strings.LastIndex(parentBase, "."); dot >= 0 {
				parent, ok = resolveBase(parentBase[dot+1:])
			}
		}
		if !ok || visiting[parent.Name] {
			continue
		}

		parent = mergeInterfaceExtends(parent, resolveBase, visiting)
		for fieldName, fieldHint := range parent.Fields {
			mergedFields[fieldName] = fieldHint
		}
	}
	for fieldName, fieldHint := range iface.Fields {
		mergedFields[fieldName] = fieldHint
	}

	iface.Fields = mergedFields
	return iface
}

func resolveStandardInterfaceAlias(name string, globals []TinyValue, globalNames map[string]int) (Interface, bool) {
	if globals == nil || globalNames == nil {
		return Interface{}, false
	}

	dot := strings.Index(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return Interface{}, false
	}

	alias := name[:dot]
	typeName := name[dot+1:]
	idx, exists := globalNames[alias]
	if !exists || idx >= len(globals) {
		return Interface{}, false
	}

	module, ok := globals[idx].Value.(*StandardModuleValue)
	if !ok || module == nil {
		return Interface{}, false
	}

	iface, exists := standardInterfaceHints[module.Name+"."+typeName]
	return iface, exists
}

func resolveStandardTypeAlias(name string, globals []TinyValue, globalNames map[string]int) string {
	if globals == nil || globalNames == nil {
		return name
	}

	dot := strings.Index(name, ".")
	if dot <= 0 || dot == len(name)-1 {
		return name
	}

	alias := name[:dot]
	typeName := name[dot+1:]
	idx, exists := globalNames[alias]
	if !exists || idx >= len(globals) {
		return name
	}

	module, ok := globals[idx].Value.(*StandardModuleValue)
	if !ok || module == nil {
		return name
	}

	return module.Name + "." + typeName
}

func stdTypeHint(name string) TypeHint {
	return TypeHintFromString(name)
}

func TypeHintFromString(name string) TypeHint {
	name = strings.TrimSpace(name)
	if strings.HasPrefix(name, "{") && strings.HasSuffix(name, "}") {
		return TypeHint{Name: name, Fields: parseStructuralTypeFieldsFromHint(name)}
	}
	parts := splitTopLevel(name, '|')
	if len(parts) > 1 {
		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		return TypeHint{Name: strings.Join(parts, " | "), Types: parts}
	}
	return TypeHint{Name: name, Types: []string{name}}
}

var standardInterfaceHints = map[string]Interface{}

func HasStandardInterfaceHint(name string) bool {
	_, exists := standardInterfaceHints[name]
	return exists
}

func GetStandardInterfaceHint(name string) (Interface, bool) {
	iface, exists := standardInterfaceHints[name]
	return iface, exists
}

func GetStandardInterfaceFields(name string) map[string]string {
	iface, exists := standardInterfaceHints[name]
	if !exists {
		return nil
	}
	fields := map[string]string{}
	for fname, ftype := range iface.Fields {
		fields[fname] = ftype.Name
	}
	return fields
}

func parseStructuralTypeFieldsFromHint(hint string) map[string]TypeHint {
	fields := map[string]TypeHint{}
	hint = strings.TrimPrefix(hint, "{")
	hint = strings.TrimSuffix(hint, "}")
	if hint == "" {
		return fields
	}
	parts := splitTopLevel(hint, ',')
	for _, part := range parts {
		kv := splitTopLevelN(strings.TrimSpace(part), ':', 2)
		if len(kv) == 2 {
			name := strings.TrimSpace(strings.TrimSuffix(kv[0], "?"))
			typ := strings.TrimSpace(kv[1])
			if strings.HasSuffix(strings.TrimSpace(kv[0]), "?") && !strings.Contains(typ, "|null") && !strings.Contains(typ, "| null") {
				typ += " | null"
			}
			fields[name] = TypeHintFromString(typ)
		}
	}
	return fields
}

func SplitTopLevelTypeList(s string, sep rune) []string {
	return splitTopLevel(s, sep)
}

func splitTopLevelN(s string, sep rune, n int) []string {
	if n <= 0 {
		return nil
	}
	parts := splitTopLevel(s, sep)
	if len(parts) <= n {
		return parts
	}
	merged := append([]string{}, parts[:n-1]...)
	merged = append(merged, strings.Join(parts[n-1:], string(sep)))
	return merged
}

func splitTopLevel(s string, sep rune) []string {
	parts := []string{}
	start := 0
	braceDepth, parenDepth, bracketDepth := 0, 0, 0
	inString := rune(0)
	escaped := false

	for i, r := range s {
		if inString != 0 {
			if escaped {
				escaped = false
				continue
			}
			if r == '\\' {
				escaped = true
				continue
			}
			if r == inString {
				inString = 0
			}
			continue
		}

		switch r {
		case '\'', '"', '`':
			inString = r
		case '{':
			braceDepth++
		case '}':
			if braceDepth > 0 {
				braceDepth--
			}
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '[':
			bracketDepth++
		case ']':
			if bracketDepth > 0 {
				bracketDepth--
			}
		default:
			if r == sep && braceDepth == 0 && parenDepth == 0 && bracketDepth == 0 {
				parts = append(parts, strings.TrimSpace(s[start:i]))
				start = i + len(string(r))
			}
		}
	}
	parts = append(parts, strings.TrimSpace(s[start:]))

	filtered := parts[:0]
	for _, part := range parts {
		if part != "" {
			filtered = append(filtered, part)
		}
	}
	return filtered
}
