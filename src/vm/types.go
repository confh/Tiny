package vm

type TypeHint struct {
	Name string
}

func (t TypeHint) IsEmpty() bool {
	return t.Name == ""
}

func checkTypeHint(value Value, hint TypeHint) bool {
	if hint.IsEmpty() {
		return true
	}

	switch hint.Name {
	case "any":
		return true

	case "number":
		switch value.(type) {
		case int, int64, float64, float32:
			return true
		default:
			return false
		}

	case "string":
		_, ok := value.(string)
		return ok

	case "bool":
		_, ok := value.(bool)
		return ok

	case "array":
		switch value.(type) {
		case ArrayValue, *ArrayValue:
			return true
		default:
			return false
		}

	case "object":
		_, ok := value.(ObjectValue)
		return ok

	case "function":
		switch value.(type) {
		case FunctionValue, *FunctionValue:
			return true
		default:
			return false
		}

	case "null":
		return value == nil || typeName(value) == "null"

	case "undefined":
		return typeName(value) == "undefined"

	default:
		// Later: class names.
		return typeName(value) == hint.Name
	}
}
