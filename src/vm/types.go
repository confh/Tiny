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

func CheckTypeHint(value Value, hint TypeHint, interfaces map[string]Interface) (bool, string) {
	if hint.IsEmpty() || hint.Name == "any" {
		return true, ""
	}

	var lastReason string
	for _, typ := range hint.AllTypes() {
		ok, reason := checkSingleTypeHint(value, typ, interfaces)
		if ok {
			return true, ""
		}
		lastReason = reason
	}

	return false, lastReason
}

func checkSingleTypeHint(value Value, hint string, interfaces map[string]Interface) (bool, string) {
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
			return false, ""
		}

	case "string":
		switch value.Value.(type) {
		case string:
			return true, ""
		default:
			return false, ""
		}

	case "bool":
		switch value.Value.(type) {
		case bool:
			return true, ""
		default:
			return false, ""
		}

	case "array":
		switch value.Value.(type) {
		case ArrayValue, *ArrayValue:
			return true, ""
		default:
			return false, ""
		}

	case "object":
		switch value.Value.(type) {
		case ObjectValue:
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

	case "undefined":
		if TypeName(value) == "undefined" {
			return true, ""
		}
		return false, ""

	default:
		if iface, exists := interfaces[hint]; exists {
			obj, ok := value.Value.(ObjectValue)
			if !ok {
				return false, ": expected object to match interface '" + hint + "'"
			}

			for fieldName, expectedHint := range iface.Fields {
				required := false
				if slices.Contains(expectedHint.Types, "undefined") {
					required = true
				}

				val, hasField := obj[fieldName]
				if !required && !hasField {
					return false, fmt.Sprintf(" (missing field '%s')", fieldName)
				}

				if ok, subReason := CheckTypeHint(val, expectedHint, interfaces); !ok {
					return false, fmt.Sprintf(" (field '%s' type mismatch%s)", fieldName, subReason)
				}
			}

			return true, ""
		}

		if obj, ok := value.Value.(ObjectValue); ok {
			classValue, exists := obj["__class"]
			if exists {
				className, ok := classValue.Value.(string)
				if ok && className == hint {
					return true, ""
				}
			}
		}

		if TypeName(value) == hint {
			return true, ""
		}
		return false, ""
	}
}
