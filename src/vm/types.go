package vm

import (
	"fmt"
	"slices"
	"strings"
)

type TypeHint struct {
	Name  string   `json:"name"`
	Types []string `json:"types,omitempty"`
}

func (t TypeHint) IsEmpty() bool {
	return t.Name == "" && len(t.Types) == 0
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

func tinyValueAsInterfaceObject(value TinyValue) (ObjectValue, bool) {
	if value.IsInt {
		return nil, false
	}

	switch obj := value.Value.(type) {
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
		case ObjectValue, *ObjectValue, WasmObjectValue, *WasmObjectValue:
			return true, ""
		default:
			return false, ""
		}

	case "function":
		switch value.Value.(type) {
		case FunctionValue, *FunctionValue:
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

		if obj, ok := tinyValueAsInterfaceObject(value); ok {
			classValue, exists := obj["__class"]
			if exists {
				className, ok := classValue.Value.(string)

				checkHint := hint
				if strings.Contains(checkHint, ":") {
					checkHint = strings.Split(checkHint, ":")[0]
				}

				if ok && (className == checkHint || strings.HasSuffix(className, "."+checkHint)) {
					return true, ""
				}
			}
		}

		if globalNames != nil && globals != nil {
			if idx, exists := globalNames[hint]; exists && idx < len(globals) {
				if enumObj, ok := globals[idx].Value.(ObjectValue); ok {
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

		if TypeName(value) == hint {
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
			Name:   iface.Name,
			Fields: instantiatedFields,
		}, true
	}

	return iface, true
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

func stdTypeHint(name string) TypeHint {
	return TypeHint{Name: name, Types: []string{name}}
}

var standardInterfaceHints = map[string]Interface{
	"tray.Bounds": {
		Name: "tray.Bounds",
		Fields: map[string]TypeHint{
			"x":      stdTypeHint("number"),
			"y":      stdTypeHint("number"),
			"width":  stdTypeHint("number"),
			"height": stdTypeHint("number"),
		},
	},
	"http.RequestObject": {
		Name: "http.RequestObject",
		Fields: map[string]TypeHint{
			"path":    stdTypeHint("string"),
			"method":  stdTypeHint("string"),
			"body":    stdTypeHint("string"),
			"params":  stdTypeHint("object"),
			"query":   stdTypeHint("object"),
			"headers": stdTypeHint("object"),
		},
	},
	"http.HttpResponse": {
		Name: "http.HttpResponse",
		Fields: map[string]TypeHint{
			"status":     stdTypeHint("number"),
			"statusText": stdTypeHint("string"),
			"body":       stdTypeHint("string"),
			"headers":    stdTypeHint("object"),
		},
	},
	"websocket.Message": {
		Name: "websocket.Message",
		Fields: map[string]TypeHint{
			"type": stdTypeHint("string"),
			"data": stdTypeHint("any"),
		},
	},
	"websocket.ClientOptions": {
		Name: "websocket.ClientOptions",
		Fields: map[string]TypeHint{
			"headers":        stdTypeHint("object"),
			"timeoutMs":      stdTypeHint("number"),
			"maxMessageSize": stdTypeHint("number"),
		},
	},
	"websocket.ServerOptions": {
		Name: "websocket.ServerOptions",
		Fields: map[string]TypeHint{
			"port":           stdTypeHint("number"),
			"host":           stdTypeHint("string"),
			"path":           stdTypeHint("string"),
			"readTimeoutMs":  stdTypeHint("number"),
			"writeTimeoutMs": stdTypeHint("number"),
			"maxMessageSize": stdTypeHint("number"),
		},
	},
	"websocket.CloseEvent": {
		Name: "websocket.CloseEvent",
		Fields: map[string]TypeHint{
			"code":     stdTypeHint("number"),
			"reason":   stdTypeHint("string"),
			"wasClean": stdTypeHint("bool"),
		},
	},
}
