package vm

import "strings"

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

func CheckTypeHint(value Value, hint TypeHint) bool {
	if hint.IsEmpty() || hint.Name == "any" {
		return true
	}

	for _, typ := range hint.AllTypes() {
		if checkSingleTypeHint(value, typ) {
			return true
		}
	}

	return false
}

func checkSingleTypeHint(value Value, hint string) bool {
	switch hint {
	case "any":
		return true

	case "number":
		switch value.(type) {
		case int, int64, float64, float32, uint64:
			return true
		default:
			return false
		}

	case "string":
		switch value.(type) {
		case string, StringExpr:
			return true
		default:
			return false
		}

	case "bool":
		switch value.(type) {
		case bool, BoolExpr:
			return true
		default:
			return false
		}

	case "array":
		switch value.(type) {
		case ArrayValue, *ArrayValue, ArrayExpr:
			return true
		default:
			return false
		}

	case "object":
		switch value.(type) {
		case ObjectValue, ObjectExpr:
			return true
		default:
			return false
		}

	case "function":
		switch value.(type) {
		case FunctionValue, *FunctionValue, FunctionExpr:
			return true
		default:
			return false
		}

	case "null":
		return value == nil || TypeName(value) == "null"

	case "undefined":
		return TypeName(value) == "undefined"

	default:
		if obj, ok := value.(ObjectValue); ok {
			classValue, exists := obj["__class"]
			if exists {
				className, ok := classValue.(string)
				if ok && className == hint {
					return true
				}
			}
		}
		return TypeName(value) == hint
	}
}
