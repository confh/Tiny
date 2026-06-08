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

func checkSingleTypeHint(value TinyValue, hint string, interfaces map[string]Interface) (bool, string) {
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

	default:
		if iface, exists := resolveInterfaceHint(hint, interfaces); exists {
			obj, ok := value.Value.(ObjectValue)
			if !ok {
				return false, ": expected object to match interface '" + hint + "'"
			}

			for fieldName, expectedHint := range iface.Fields {
				required := false
				if slices.Contains(expectedHint.Types, "null") {
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

func resolveInterfaceHint(hint string, interfaces map[string]Interface) (Interface, bool) {
	if iface, exists := interfaces[hint]; exists {
		return iface, true
	}

	if iface, exists := standardInterfaceHints[hint]; exists {
		return iface, true
	}

	if dot := strings.LastIndex(hint, "."); dot >= 0 {
		shortName := hint[dot+1:]
		if iface, exists := interfaces[shortName]; exists {
			return iface, true
		}
	}

	return Interface{}, false
}

func stdTypeHint(name string) TypeHint {
	return TypeHint{Name: name, Types: []string{name}}
}

var standardInterfaceHints = map[string]Interface{
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
			"status":  stdTypeHint("number"),
			"body":    stdTypeHint("string"),
			"headers": stdTypeHint("object"),
		},
	},
}
