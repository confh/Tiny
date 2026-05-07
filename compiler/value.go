package main

import (
	"fmt"
	"strconv"
	"strings"
)

type NullValue struct{}

type UndefinedValue struct{}

type ArrayValue []Value

type ObjectValue map[string]Value

type Value any

func asInt(value Value) int {
	switch n := value.(type) {
	case int:
		return n
	case int8:
		return int(n)
	case int16:
		return int(n)
	case int32:
		return int(n)
	case int64:
		return int(n)
	case uint:
		return int(n)
	case uint8:
		return int(n)
	case uint16:
		return int(n)
	case uint32:
		return int(n)
	case uint64:
		return int(n)
	default:
		langError(ErrorSyntax, "expected number, got %T", value)
		return -1
	}
}

func typeName(value Value) string {
	switch value.(type) {
	case int:
		return "number"
	case string:
		return "string"
	case bool:
		return "bool"
	case ArrayValue:
		return "array"
	case NullValue:
		return "null"
	case UndefinedValue:
		return "undefined"
	case nil:
		return "nil"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func valueToJSONCompatible(value Value) any {
	switch v := value.(type) {
	case int:
		return v

	case string:
		return v

	case bool:
		return v

	case ObjectValue:
		result := map[string]any{}

		for key, item := range v {
			result[key] = valueToJSONCompatible(item)
		}

		return result

	case ArrayValue:
		result := make([]any, len(v))

		for i, item := range v {
			result[i] = valueToJSONCompatible(item)
		}

		return result

	case NullValue:
		return nil

	case UndefinedValue:
		return nil

	case nil:
		return nil

	default:
		langError(ErrorType, "cannot convert %s to JSON", typeName(value))
		return nil
	}
}

func valueToString(value Value) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case bool:
		if v {
			return "true"
		}
		return "false"

	case ArrayValue:
		parts := make([]string, len(v))

		for i, item := range v {
			parts[i] = valueToString(item)
		}

		return "[" + strings.Join(parts, ", ") + "]"
	case NullValue:
		return "null"
	case UndefinedValue:
		return "undefined"
	case nil:
		return "nil"
	case ObjectValue:
		parts := []string{}

		for key, item := range v {
			parts = append(parts, key+": "+valueToString(item))
		}

		return "{" + strings.Join(parts, ", ") + "}"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asString(value Value) string {
	stringValue, ok := value.(string)
	if !ok {
		langError(ErrorSyntax, "expected string, got %T", value)
	}

	return stringValue
}

func asBool(value Value) bool {
	boolean, ok := value.(bool)
	if !ok {
		langError(ErrorSyntax, "expected bool, got %T", value)
	}

	return boolean
}

func isTruthy(value Value) bool {
	switch v := value.(type) {
	case bool:
		return v
	case int:
		return v != 0
	case string:
		return v != ""
	case NullValue:
		return false
	case UndefinedValue:
		return false
	default:
		return value != nil
	}
}

func valuesEqual(a Value, b Value) bool {
	switch left := a.(type) {
	case int:
		right, ok := b.(int)
		return ok && left == right

	case string:
		right, ok := b.(string)
		return ok && left == right

	case bool:
		right, ok := b.(bool)
		return ok && left == right

	case NullValue:
		_, ok := b.(NullValue)
		return ok

	case UndefinedValue:
		_, ok := b.(UndefinedValue)
		return ok

	default:
		return a == b
	}
}
