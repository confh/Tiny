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
	"sync"

	webview "github.com/abemedia/go-webview"

	_ "github.com/abemedia/go-webview/embedded"

	. "language.com/src/tinyerrors"
)

type HttpResponseType = int

const (
	HttpJson HttpResponseType = iota
	HttpText
)

type NullValue struct{}

type ArrayValue struct {
	Elements []TinyValue
}

type ObjectValue map[any]TinyValue

type NativeTaskValue struct {
	Done chan TaskResult
}

type NativeMutexValue struct {
	mu sync.Mutex
}

func (this *NativeMutexValue) Lock() {
	this.mu.Lock()
}

func (this *NativeMutexValue) Unlock() {
	this.mu.Unlock()
}

type TaskResult struct {
	Value TinyValue
	Error any
}

type BufferValue struct {
	Bytes []byte
}

type Cell struct {
	Value TinyValue
	Int   int
	IsInt bool
}

type ErrorValue struct {
	Kind    string
	Message string
}

type FunctionValue struct {
	ID       int
	Name     string
	Captures map[int]*Cell
	Async    bool
}

type NativeServerValue struct {
	Port         int
	GetRoutes    map[string]TinyValue
	PostRoutes   map[string]TinyValue
	GenericRoute TinyValue
	mux          *http.ServeMux
	closed       bool
}

type NativeWebViewValue struct {
	w webview.WebView
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
	Value TinyValue
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
	Members map[string]TinyValue
}

type NamespaceMemberRef struct {
	GlobalName string
}

type TinyTyped interface {
	TinyTypeName() string
}

type TinyValue struct {
	Value any
	IsInt bool
	AsInt int
}

func asInt64(value TinyValue) int64 {
	if value.IsInt {
		return int64(value.AsInt)
	}

	switch v := value.Value.(type) {
	case float64:
		return int64(v)
	case uint64:
		return int64(v)
	default:
		LangError(ErrorType, "expected number, got %s", TypeName(value))
		return 0
	}
}

func asInt(value TinyValue) int {
	if value.IsInt {
		return value.AsInt
	}

	switch n := value.Value.(type) {
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

func asFloat32(value TinyValue) float32 {
	if value.IsInt {
		return float32(value.AsInt)
	}

	switch n := value.Value.(type) {
	case float32:
		return n
	case float64:
		return float32(n)
	default:
		LangError(ErrorSyntax, "expected float, got %T", value)
		return -1
	}
}

func asFloat64(value TinyValue) float64 {
	if value.IsInt {
		return float64(value.AsInt)
	}

	switch v := value.Value.(type) {
	case float32:
		return float64(v)

	case float64:
		return v

	case uint64:
		return float64(v)

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

func isNumber(value TinyValue) bool {
	if value.IsInt {
		return true
	}

	switch value.Value.(type) {
	case float64, uint64:
		return true
	default:
		return false
	}
}

func isString(value TinyValue) bool {
	if value.IsInt {
		return false
	}

	switch value.Value.(type) {
	case string:
		return true
	default:
		return false
	}
}

func asFloat(value TinyValue, vm *VM) float64 {
	if value.IsInt {
		return float64(value.AsInt)
	}

	switch v := value.Value.(type) {
	case int:
		return float64(v)
	case int8:
		return float64(v)
	case int16:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	case uint:
		return float64(v)
	case uint8:
		return float64(v)
	case uint16:
		return float64(v)
	case uint32:
		return float64(v)
	case uint64:
		return float64(v)
	case float32:
		return float64(v)
	case float64:
		return v
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			vm.runtimeError(ErrorType, "cannot parse string '%s' as float: %v", v, err)
			return 0
		}
		return f
	default:
		vm.runtimeError(ErrorType, "expected number, got %s", TypeName(value))
		return 0
	}
}

func asUint(value TinyValue) uint64 {
	if value.IsInt {
		return uint64(value.AsInt)
	}

	switch v := value.Value.(type) {
	case int:
		return uint64(v)
	case int64:
		return uint64(v)
	case float64:
		return uint64(v)
	case uint64:
		return v
	case string:
		f, err := strconv.ParseUint(v, 10, 64)
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

func TypeName(value TinyValue) string {
	if value.Value == nil {
		if value.IsInt {
			return "number"
		}
		return "null"
	}

	if typed, ok := value.Value.(TinyTyped); ok {
		return typed.TinyTypeName()
	}

	return TypeNameStandard(value)
}

func TypeNameStandard(value TinyValue) string {
	if value.IsInt {
		return "number"
	}

	switch v := value.Value.(type) {
	case uint, uint64, uint32, uint16, uint8:
		return "number"
	case float64, float32:
		return "float"
	case string:
		return "string"
	case bool:
		return "bool"
	case ArrayValue:
		return "array"
	case NullValue, NullExpr:
		return "null"
	case ObjectValue:
		if classNameVal, exists := v["__class"]; exists {
			if className, ok := classNameVal.Value.(string); ok {
				return "class::" + className
			}
		}
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
	case *NativeMutexValue:
		return "mutex"
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

func valueToJSONCompatible(value TinyValue) any {
	if value.IsInt {
		return value.AsInt
	}

	switch v := value.Value.(type) {
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
	case nil:
		return nil

	default:
		LangError(ErrorType, "cannot convert %s to JSON", TypeName(value))
		return nil
	}
}

func ToValue(value any) TinyValue {
	switch v := value.(type) {
	case nil:
		return TinyValue{
			Value: NullValue{},
			IsInt: false,
			AsInt: 0,
		}

	case TinyValue:
		return v

	case string:
		return TinyValue{
			Value: v,
			IsInt: false,
			AsInt: 0,
		}

	case bool:
		return TinyValue{
			Value: v,
			IsInt: false,
			AsInt: 0,
		}

	case int:
		return NewInt(v)
	case int64:
		return NewInt(int(v))
	case int32:
		return NewInt(int(v))
	case float64:
		if v == float64(int(v)) {
			return NewInt(int(v))
		}
		return NewNative(v)

	case []any:
		elements := make([]TinyValue, len(v))

		for i, item := range v {
			elements[i] = jsonToTinyValue(item)
		}

		return TinyValue{
			Value: &ArrayValue{
				Elements: elements,
			},
			IsInt: false,
			AsInt: 0,
		}

	case map[string]any:
		object := ObjectValue{}

		for key, item := range v {
			object[key] = jsonToTinyValue(item)
		}

		return TinyValue{
			Value: object,
			IsInt: false,
			AsInt: 0,
		}

	default:
		return NewNative(v)
	}
}

func jsonToTinyValue(value any) TinyValue {
	return ToValue(value)
}

func valueToString(value TinyValue) string {
	if value.IsInt {
		return strconv.Itoa(value.AsInt)
	}

	switch v := value.Value.(type) {
	case string:
		return v
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
			value, ok := item.Value.(string)
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
	case nil:
		return "nil"
	case ObjectValue:
		type objectEntry struct {
			keyText string
			value   TinyValue
		}

		entries := make([]objectEntry, 0, len(v))

		for key, item := range v {
			entries = append(entries, objectEntry{
				keyText: valueToString(ToValue(key)),
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
	case *NativeMutexValue:
		return "<mutex>"
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

func asString(value TinyValue, vm *VM) string {
	stringValue, ok := value.Value.(string)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected string, got %s", TypeName(value))
	}

	return stringValue
}

func asObject(value TinyValue, vm *VM) ObjectValue {
	objectValue, ok := value.Value.(ObjectValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected object, got %s", TypeName(value))
	}

	return objectValue
}

func asBuffer(value TinyValue, vm *VM) *BufferValue {
	bufferValue, ok := value.Value.(*BufferValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected buffer, got %s", TypeName(value))
	}

	return bufferValue
}

func asArray(value TinyValue, vm *VM) *ArrayValue {
	arrayValue, ok := value.Value.(*ArrayValue)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected array, got %s", TypeName(value))
	}

	return arrayValue
}

func asBool(value TinyValue, vm *VM) bool {
	boolean, ok := value.Value.(bool)
	if !ok {
		vm.runtimeError(ErrorSyntax, "expected bool, got %s", TypeName(value))
	}

	return boolean
}

func isTruthy(value TinyValue) bool {
	if value.IsInt {
		return value.AsInt != 0
	}

	switch v := value.Value.(type) {
	case bool:
		return v
	case string:
		return v != ""
	case NullValue:
		return false
	default:
		return v != nil
	}
}

func valuesEqual(a TinyValue, b TinyValue) bool {
	if a.IsInt {
		if b.IsInt {
			return a.AsInt == b.AsInt
		}

		switch right := b.Value.(type) {
		case float64:
			return float64(a.AsInt) == right
		default:
			return false
		}
	}

	switch left := a.Value.(type) {
	case float64:
		switch right := b.Value.(type) {
		case int:
			return left == float64(right)
		case float64:
			return left == right
		default:
			return false
		}

	case string:
		right, ok := b.Value.(string)
		return ok && left == right

	case bool:
		right, ok := b.Value.(bool)
		return ok && left == right

	case NullValue:
		_, ok := b.Value.(NullValue)
		return ok

	default:
		return a == b
	}
}

func NewInt(val int) TinyValue {
	return TinyValue{
		Value: nil,
		IsInt: true,
		AsInt: val,
	}
}

func NewNull() TinyValue {
	return TinyValue{
		Value: NullValue{},
		IsInt: false,
		AsInt: 0,
	}
}

func NewNative(variable any) TinyValue {
	return TinyValue{
		Value: variable,
		IsInt: false,
		AsInt: 0,
	}
}
