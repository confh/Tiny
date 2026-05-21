package vm

type TypeHint struct {
	Name string
}

func (t TypeHint) IsEmpty() bool {
	return t.Name == ""
}

func CheckTypeHint(value Value, hint TypeHint) bool {
	if hint.IsEmpty() {
		return true
	}

	switch hint.Name {
	case "any":
		return true

	case "number":
		switch value.(type) {
		case int, int64, float64, float32, NumberExpr:
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
		// Later: class names.
		return TypeName(value) == hint.Name
	}
}
