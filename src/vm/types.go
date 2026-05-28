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
		if value.IsInt {
			return true
		}

		switch value.Value.(type) {
		case float64, float32, uint64:
			return true
		default:
			return false
		}

	case "string":
		switch value.Value.(type) {
		case string:
			return true
		default:
			return false
		}

	case "bool":
		switch value.Value.(type) {
		case bool:
			return true
		default:
			return false
		}

	case "array":
		switch value.Value.(type) {
		case ArrayValue, *ArrayValue:
			return true
		default:
			return false
		}

	case "object":
		switch value.Value.(type) {
		case ObjectValue:
			return true
		default:
			return false
		}

	case "function":
		switch value.Value.(type) {
		case FunctionValue, *FunctionValue:
			return true
		default:
			return false
		}

	case "null":
		return !value.IsInt && (value.Value == nil || TypeName(value) == "null")

	case "undefined":
		return TypeName(value) == "undefined"

	default:
		if obj, ok := value.Value.(ObjectValue); ok {
			classValue, exists := obj["__class"]
			if exists {
				className, ok := classValue.Value.(string)
				if ok && className == hint {
					return true
				}
			}
		}
		return TypeName(value) == hint
	}
}
