package main

import (
	"fmt"
	"strconv"
	"strings"
)

type NullValue struct{}

type UndefinedValue struct{}

type ArrayValue []Value

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
		panic(fmt.Sprintf("expected number, got %T", value))
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
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asString(value Value) string {
	stringValue, ok := value.(string)
	if !ok {
		panic(fmt.Sprintf("expected string, got %T", value))
	}

	return stringValue
}

func asBool(value Value) bool {
	boolean, ok := value.(bool)
	if !ok {
		panic(fmt.Sprintf("expected bool, got %T", value))
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
