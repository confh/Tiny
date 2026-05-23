package vm

import (
	"bufio"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"

	. "language.com/src/tinyerrors"
)

type HttpResponseType = int

const (
	HttpJson HttpResponseType = iota
	HttpText
)

type NullValue struct{}

type UndefinedValue struct{}

type ArrayValue struct {
	Elements []Value
}

type ObjectValue map[Value]Value

type NativeTaskValue struct {
	Done chan TaskResult
}

type TaskResult struct {
	Value Value
	Error any
}

type BufferValue struct {
	Bytes []byte
}

type Cell struct {
	Value Value
}

type ErrorValue struct {
	Kind    string
	Message string
}

type FunctionValue struct {
	ID       int
	Name     string
	Captures map[int]*Cell
}

type NativeServerValue struct {
	Port       int
	GetRoutes  map[string]Value
	PostRoutes map[string]Value
	mux        *http.ServeMux
	closed     bool
}

type NativeTcpServerValue struct {
	Host              string
	Port              int
	Listener          *net.Listener
	ConnectionHandler *FunctionValue
}

type NativeTcpConnectionValue struct {
	Connection net.Conn
	Reader     *bufio.Reader
}

type NativeHttpResponseValue struct {
	Type  HttpResponseType
	Value Value
}

type NativeAppValue struct {
	Name     string
	Commands map[string]FunctionValue
}

type StandardModuleValue struct {
	Name string
}

type NativeFileValue struct {
	File   *os.File
	Path   string
	Closed bool
}

type NativeStringBuilderValue struct {
	Builder *strings.Builder
}

type NativeProcessValue struct {
	Cmd     *exec.Cmd
	Running bool
}

type NamespaceValue struct {
	Name    string
	Members map[string]Value
}

type NamespaceMemberRef struct {
	GlobalName string
}

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
	case float64:
		return int(n)
	case float32:
		return int(n)
	case string:
		f64, err := strconv.ParseFloat(n, 64)
		f := int(f64)
		if err != nil {
			LangError(ErrorType, "cannot parse string '%s' as number: %v", n, err)
			return 0
		}
		return f
	default:
		LangError(ErrorSyntax, "expected number, got %T", value)
		return -1
	}
}

func asFloat32(value Value) float32 {
	switch n := value.(type) {
	case float32:
		return n
	case float64:
		return float32(n)
	default:
		LangError(ErrorSyntax, "expected float, got %T", value)
		return -1
	}
}

func asFloat64(value Value) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)

	case int64:
		return float64(v)

	case float32:
		return float64(v)

	case float64:
		return v

	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			LangError(ErrorType, "cannot parse string '%s' as number: %v", v, err)
			return 0
		}
		return f

	default:
		LangError(ErrorType, "expected number, got %s", TypeName(value))
		return 0
	}
}

func isNumber(value Value) bool {
	switch value.(type) {
	case int, int64, float64:
		return true
	default:
		return false
	}
}

func isString(value Value) bool {
	switch value.(type) {
	case string:
		return true
	default:
		return false
	}
}

func asFloat(value Value) float64 {
	switch v := value.(type) {
	case int:
		return float64(v)
	case float64:
		return v
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			LangError(ErrorType, "cannot parse string '%s' as float: %v", v, err)
			return 0
		}
		return f
	default:
		LangError(ErrorType, "expected number, got %s", TypeName(value))
		return 0
	}
}

func TypeName(value Value) string {
	switch v := value.(type) {
	case int:
		return "number"
	case float64:
		return "float"
	case string:
		return "string"
	case bool:
		return "bool"
	case ArrayValue:
		return "array"
	case NullValue, NullExpr:
		return "null"
	case UndefinedValue:
		return "undefined"
	case ObjectValue:
		return "object"
	case nil:
		return "nil"
	case FunctionValue:
		return "<function " + v.Name + ">"
	case *FunctionValue:
		return "<function " + v.Name + ">"
	case NativeServerValue:
		return "server"
	case *NativeServerValue:
		return "server"
	case *NativeTcpServerValue:
		return "tcp server"
	case *NativeTcpConnectionValue:
		return "tcp connection"
	case *NativeProcessValue:
		return "process"
	case ErrorValue:
		return "error"
	case *ErrorValue:
		return "error"
	case *NativePluginValue:
		return "plugin"
	case *StandardModuleValue:
		return "standard module"
	case *NativeFileValue:
		return "file"
	case *NativeAppValue:
		return "app"
	case *NativeTaskValue:
		return "task"
	case NamespaceValue:
		return "namespace"
	case *NamespaceValue:
		return "namespace"
	case *NativeStringBuilderValue:
		return "string builder"
	case BufferValue:
		return "buffer"
	case *BufferValue:
		return "buffer"
	case NamespaceMemberRef:
		return "namespace member ref"
	case *NamespaceMemberRef:
		return "namespace member ref"
	default:
		return fmt.Sprintf("%T", value)
	}
}

func valueToJSONCompatible(value Value) any {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return v
	case float64:
		return v

	case string:
		return v

	case bool:
		return v

	case ObjectValue:
		result := map[string]any{}

		for key, item := range v {
			strKey, ok := key.(string)
			if !ok {
				LangError(ErrorType, "cannot convert non-string key (%T) to JSON", key)
				continue
			}
			result[strKey] = valueToJSONCompatible(item)
		}

		return result

	case ArrayValue:
		result := make([]any, len(v.Elements))

		for i, item := range v.Elements {
			result[i] = valueToJSONCompatible(item)
		}

		return result

	case *ArrayValue:
		result := make([]any, len(v.Elements))

		for i, item := range v.Elements {
			result[i] = valueToJSONCompatible(item)
		}

		return result

	case BufferValue:
		return v.Bytes

	case *BufferValue:
		return v.Bytes

	case NullValue:
		return nil

	case UndefinedValue:
		return nil

	case nil:
		return nil

	default:
		LangError(ErrorType, "cannot convert %s to JSON", TypeName(value))
		return nil
	}
}

func jsonToTinyValue(value any) Value {
	switch v := value.(type) {
	case nil:
		return NullValue{}

	case string:
		return v

	case bool:
		return v

	case float64:
		if v == float64(int(v)) {
			return int(v)
		}

		return v

	case []any:
		elements := make([]Value, len(v))

		for i, item := range v {
			elements[i] = jsonToTinyValue(item)
		}

		return &ArrayValue{
			Elements: elements,
		}

	case map[string]any:
		object := ObjectValue{}

		for key, item := range v {
			object[key] = jsonToTinyValue(item)
		}

		return object

	default:
		return valueToString(v)
	}
}

func valueToString(value Value) string {
	switch v := value.(type) {
	case string:
		return v
	case int:
		return strconv.Itoa(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case bool:
		if v {
			return "true"
		}
		return "false"

	case *ArrayValue:
		parts := make([]string, len(v.Elements))

		for i, item := range v.Elements {
			value, ok := item.(string)
			if ok {
				parts[i] = "\"" + value + "\""
			} else {
				parts[i] = valueToString(item)
			}
		}

		return "[" + strings.Join(parts, ", ") + "]"

	case ErrorValue:
		return v.Kind + ": " + v.Message

	case *ErrorValue:
		return v.Kind + ": " + v.Message
	case NullValue:
		return "null"
	case UndefinedValue:
		return "undefined"
	case nil:
		return "nil"
	case ObjectValue:
		type objectEntry struct {
			keyText string
			value   Value
		}

		entries := make([]objectEntry, 0, len(v))

		for key, item := range v {
			entries = append(entries, objectEntry{
				keyText: valueToString(key),
				value:   item,
			})
		}

		sort.Slice(entries, func(i, j int) bool {
			return entries[i].keyText < entries[j].keyText
		})

		parts := make([]string, 0, len(entries))

		for _, entry := range entries {
			parts = append(parts, entry.keyText+": "+valueToString(entry.value))
		}

		return "{" + strings.Join(parts, ", ") + "}"

	case FunctionValue:
		return "<function " + v.Name + ">"
	case NativeServerValue:
		return "<server :" + strconv.Itoa(v.Port) + ">"
	case *NativeServerValue:
		return "<server :" + strconv.Itoa(v.Port) + ">"
	case *NativeTcpServerValue:
		return "<tcp server :" + strconv.Itoa(v.Port) + ">"
	case *NativeTcpConnectionValue:
		return "<tcp connection :" + v.Connection.RemoteAddr().String() + ">"
	case *NativePluginValue:
		return "<plugin " + v.Path + ">"
	case *StandardModuleValue:
		return "<std " + v.Name + ">"
	case *NativeFileValue:
		return "<file " + v.Path + ">"
	case *NativeAppValue:
		return "<app " + v.Name + ">"
	case *NativeTaskValue:
		return "<task>"
	case NamespaceValue:
		return "<namespace " + v.Name + ">"
	case *NamespaceValue:
		return "<namespace " + v.Name + ">"
	case NamespaceMemberRef:
		return "<namespace ref " + v.GlobalName + ">"
	case *NamespaceMemberRef:
		return "<namespace ref " + v.GlobalName + ">"
	case BufferValue:
		return "<buffer " + string(v.Bytes) + ">"
	case *NativeStringBuilderValue:
		return "<string builder>"
	case *BufferValue:
		return string(v.Bytes)
	case *NativeProcessValue:
		return "<process>"
	default:
		return fmt.Sprintf("%v", v)
	}
}

func asString(value Value, vm *VM) string {
	stringValue, ok := value.(string)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected string, got %s", TypeName(value))
	}

	return stringValue
}

func asObject(value Value, vm *VM) ObjectValue {
	objectValue, ok := value.(ObjectValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected object, got %s", TypeName(value))
	}

	return objectValue
}

func asBuffer(value Value, vm *VM) *BufferValue {
	bufferValue, ok := value.(*BufferValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected buffer, got %s", TypeName(value))
	}

	return bufferValue
}

func asArray(value Value, vm *VM) *ArrayValue {
	arrayValue, ok := value.(*ArrayValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected array, got %s", TypeName(value))
	}

	return arrayValue
}

func asBool(value Value, vm *VM) bool {
	boolean, ok := value.(bool)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected bool, got %s", TypeName(value))
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
		switch right := b.(type) {
		case int:
			return left == right
		case float64:
			return float64(left) == right
		default:
			return false
		}

	case float64:
		switch right := b.(type) {
		case int:
			return left == float64(right)
		case float64:
			return left == right
		default:
			return false
		}

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
