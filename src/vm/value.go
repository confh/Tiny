package vm

import (
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	. "language.com/src/tinyerrors"
)

type HttpResponseType = int

const (
	HttpJson HttpResponseType = iota
	HttpText
	HttpHtml
	HttpResponse
	HttpRedirect
	HttpNoContent
	HttpFile
	HttpDownload
)

type NullValue struct{}

type ArrayValue struct {
	Elements []TinyValue
}

type ObjectValue map[any]TinyValue

func (ov ObjectValue) MarshalJSON() ([]byte, error) {
	cleanMap := make(map[string]TinyValue)

	for key, val := range ov {
		stringKey := fmt.Sprintf("%v", key)
		cleanMap[stringKey] = val
	}

	return json.Marshal(cleanMap)
}

type InstanceValue struct {
	ClassName      string
	Fields         ObjectValue
	ConstFields    map[string]bool
	PrivateFields  map[string]bool
	PrivateMethods map[string]bool
}

func (v *InstanceValue) TinyTypeName() string {
	if v == nil {
		return "class::<nil>"
	}
	return "class::" + v.ClassName
}

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
	Value    TinyValue
	Int      int
	IsInt    bool
	Constant bool
	TypeHint TypeHint
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
	Host           string
	Port           int
	ReadTimeoutMs  int
	WriteTimeoutMs int
	MaxBodySize    int64
	Routes         map[string]map[string]TinyValue
	GetRoutes      map[string]TinyValue
	PostRoutes     map[string]TinyValue
	StaticRoutes   map[string]string
	GenericRoute   TinyValue
	mux            *http.ServeMux
	httpServer     *http.Server
	closed         bool
	Workers        *VMPool
}

type NativeSqliteValue struct {
	DB     *sql.DB
	Closed bool
}

type NativeTimerType byte

const (
	Timer NativeTimerType = iota
	Ticker
)

func (t NativeTimerType) String() string {
	if t == Timer {
		return "timer"
	}
	return "ticker"
}

type NativeTimerValue struct {
	Type   NativeTimerType
	Timer  *time.Timer
	Ticker *time.Ticker
	Quit   chan bool

	once      sync.Once
	cancelled atomic.Bool
}

func (t *NativeTimerValue) Cancel() {
	if t == nil {
		return
	}

	t.once.Do(func() {
		t.cancelled.Store(true)

		if t.Timer != nil {
			t.Timer.Stop()
		}

		if t.Ticker != nil {
			t.Ticker.Stop()
		}

		if t.Quit != nil {
			close(t.Quit)
		}
	})
}

func (t *NativeTimerValue) IsCancelled() bool {
	if t == nil {
		return true
	}

	return t.cancelled.Load()
}

type ValidateType byte

const (
	String ValidateType = iota
	Number
	Bool
	ObjectSchema
	ArraySchema
	EnumSchema
	UnionSchema
	AnySchema
)

type NativeValidateType struct {
	Name         string
	Type         ValidateType
	Required     bool
	Nullable     bool
	HasDefault   bool
	Default      TinyValue
	Message      string
	MinLen       *int
	MaxLen       *int
	ExactLen     *int
	MinNum       *float64
	MaxNum       *float64
	NonEmpty     bool
	Email        bool
	Url          bool
	Regex        string
	Trim         bool
	IntOnly      bool
	Positive     bool
	ItemSchema   *NativeValidateType
	Fields       []*NativeValidateType
	Strict       bool
	EnumValues   []TinyValue
	UnionSchemas []*NativeValidateType
	RefineFn     *FunctionValue
	TransformFn  *FunctionValue
	WebSource    string
}

type NativeValidateTop struct {
	Schema *NativeValidateType
}

type NativeWebViewValue struct {
	w                PlatformWebView
	iconBig          uintptr
	iconSmall        uintptr
	hidden           bool
	width            int
	height           int
	frameless        bool
	userWantedHidden bool
}

type NativeTcpServerValue struct {
	Host              string
	Port              int
	Listener          *net.Listener
	ConnectionHandler *FunctionValue
	Workers           *VMPool
}

type NativeTcpConnectionValue struct {
	Connection net.Conn
	Reader     *bufio.Reader
}

type NativeWebsocketServerValue struct {
	Port           int
	Host           string
	Path           string
	MaxMessageSize int

	OnConnection *FunctionValue
	OnMessage    *FunctionValue
	OnClose      *FunctionValue
	OnError      *FunctionValue

	Running  bool
	server   *http.Server
	upgrader websocket.Upgrader

	mu      sync.Mutex
	conns   map[*NativeWebsocketConnValue]bool
	Workers *VMPool
}

type NativeWebsocketConnValue struct {
	Url string

	OnMessage *FunctionValue
	OnClose   *FunctionValue
	OnError   *FunctionValue

	conn   *websocket.Conn
	mu     sync.Mutex
	closed bool

	headers ObjectValue
	server  *NativeWebsocketServerValue
}

type NativeHttpResponseValue struct {
	Type         HttpResponseType
	Value        TinyValue
	Status       int
	Headers      ObjectValue
	Path         string
	DownloadName string
	RedirectURL  string
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

type NativeVMValue struct {
	VM             *VM
	Isolated       bool
	RunMainOnLoad  bool
	MainRan        bool
	Loaded         bool
	AllowedStdlib  map[string]bool
	InjectedGlobal ObjectValue
}

func (v *NativeVMValue) TinyTypeName() string {
	return "runtime.VM"
}

type HostFunctionValue struct {
	VM          *VM
	Function    FunctionValue
	Name        string
	Receiver    TinyValue
	HasReceiver bool
}

func (v *HostFunctionValue) TinyTypeName() string {
	return "host function"
}

type CallbackFunctionValue struct {
	Name     string
	Callback func([]TinyValue) (TinyValue, error)
}

func (v *CallbackFunctionValue) TinyTypeName() string {
	return "callback function"
}

type NamespaceValue struct {
	Name    string
	Members map[string]TinyValue
}

type NamespaceMemberRef struct {
	GlobalName string
}

type InterfaceValue struct {
	Name string
}

type TinyTyped interface {
	TinyTypeName() string
}

type TinyValue struct {
	Value any
	IsInt bool
	AsInt int
}

func (vm *VM) asInt(value TinyValue) int {
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
			vm.runtimeError(ErrorType, "cannot parse string '%s' as number: %v", n, err)
			return 0
		}
		return f
	default:
		vm.runtimeError(ErrorSyntax, "expected number, got %T", value)
		return -1
	}
}

func (vm *VM) asFloat64(value TinyValue) float64 {
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
			vm.runtimeError(ErrorType, "cannot parse string '%s' as number: %v", v, err)
			return 0
		}
		return f

	default:
		vm.runtimeError(ErrorType, "expected number, got %s", TypeName(value))
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
	case *ArrayValue:
		return "array"
	case NullValue, NullExpr:
		return "null"
	case ObjectValue:
		return "object"
	case *InstanceValue:
		return v.TinyTypeName()
	case nil:
		return "nil"
	case FunctionValue:
		return "<function " + v.Name + ">"
	case *FunctionValue:
		return "<function " + v.Name + ">"
	case NativeServerValue:
		return "server"
	case *NativeValidateTop:
		return "schema"
	case *NativeValidateType:
		return "schema type"
	case *NativeServerValue:
		return "server"
	case *NativeSqliteValue:
		return "sqlite"
	case *NativeTimerValue:
		return "<" + v.Type.String() + ">"
	case *NativeTcpServerValue:
		return "tcp server"
	case *NativeTcpConnectionValue:
		return "tcp connection"
	case *NativeWebsocketServerValue:
		return "websocket server"
	case *NativeWebsocketConnValue:
		return "websocket connection"
	case *NativeTrayValue:
		return "tray"
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
	case InterfaceValue:
		return "interface"
	case *InterfaceValue:
		return "interface"
	default:
		return fmt.Sprintf("%T", value.Value)
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

	case *InstanceValue:
		if v == nil {
			return nil
		}
		return valueToJSONCompatible(NewNative(v.Fields))

	case *ObjectValue:
		if v == nil {
			return nil
		}
		return valueToJSONCompatible(NewNative(*v))

	case WasmObjectValue:
		if v.VM == nil {
			return nil
		}
		if obj, ok := v.VM.wasmObjectToObjectValue(v); ok {
			return valueToJSONCompatible(NewNative(obj))
		}
		return nil

	case *WasmObjectValue:
		if v == nil || v.VM == nil {
			return nil
		}
		if obj, ok := v.VM.wasmObjectToObjectValue(*v); ok {
			return valueToJSONCompatible(NewNative(obj))
		}
		return nil

	case ArrayValue:
		result := make([]any, len(v.Elements))

		for i, item := range v.Elements {
			result[i] = valueToJSONCompatible(item)
		}

		return result

	case *ArrayValue:
		if v == nil {
			return nil
		}
		result := make([]any, len(v.Elements))

		for i, item := range v.Elements {
			result[i] = valueToJSONCompatible(item)
		}

		return result

	case WasmArrayValue:
		if v.VM == nil {
			return nil
		}
		if arr, ok := v.VM.wasmArrayToArrayValue(v); ok {
			return valueToJSONCompatible(NewNative(arr))
		}
		return nil

	case *WasmArrayValue:
		if v == nil || v.VM == nil {
			return nil
		}
		if arr, ok := v.VM.wasmArrayToArrayValue(*v); ok {
			return valueToJSONCompatible(NewNative(arr))
		}
		return nil

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

const prettyIndent = "    " // 4 spaces

func prettyIndentText(level int) string {
	if level <= 0 {
		return ""
	}
	return strings.Repeat(prettyIndent, level)
}

func formatPrettyObject(v ObjectValue, indent int, forPrint bool) string {
	return formatPrettyObjectFields(v, "", indent, forPrint)
}

func formatPrettyInstance(v *InstanceValue, indent int, forPrint bool) string {
	if v == nil {
		return "null"
	}
	return formatPrettyObjectFields(v.Fields, v.ClassName, indent, forPrint)
}

func formatPrettyObjectFields(v ObjectValue, classNameText string, indent int, forPrint bool) string {
	type objectEntry struct {
		keyText string
		value   TinyValue
	}

	className := NewNative(classNameText)
	isClass := classNameText != ""

	entries := make([]objectEntry, 0, len(v))

	for key, item := range v {
		keyText := valueToString(ToValue(key), false)

		entries = append(entries, objectEntry{
			keyText: keyText,
			value:   item,
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		return entries[i].keyText < entries[j].keyText
	})

	if len(entries) == 0 {
		if isClass {
			name, _ := className.Value.(string)
			if name == "" {
				name = "<unknown>"
			}
			return "class " + name + " {}"
		}

		return "{}"
	}

	currentIndent := prettyIndentText(indent)
	fieldIndent := prettyIndentText(indent + 1)

	parts := make([]string, 0, len(entries))

	for _, entry := range entries {
		val := formatPrettyObjectFieldValue(entry.value, indent+1, forPrint)
		parts = append(parts, fieldIndent+entry.keyText+": "+val)
	}

	body := strings.Join(parts, ",\n")

	if isClass {
		name, _ := className.Value.(string)
		if name == "" {
			name = "<unknown>"
		}

		return "class " + name + " {\n" + body + "\n" + currentIndent + "}"
	}

	return "{\n" + body + "\n" + currentIndent + "}"
}

func formatPrettyObjectFieldValue(value TinyValue, indent int, forPrint bool) string {
	if !value.IsInt {
		switch v := value.Value.(type) {
		case ObjectValue:
			return formatPrettyObject(v, indent, forPrint)

		case *ObjectValue:
			if v == nil {
				return "null"
			}
			return formatPrettyObject(*v, indent, forPrint)
		}
	}

	val := valueToString(value, false)

	if value.IsInt {
		if forPrint {
			val = "\033[36m" + val + "\033[0m"
		}
		return val
	}

	if _, ok := value.Value.(string); ok {
		val = "'" + val + "'"
		if forPrint {
			val = "\033[32m" + val + "\033[0m"
		}
		return val
	}

	return indentPrettyMultiline(val, indent)
}

func indentPrettyMultiline(s string, indent int) string {
	if !strings.Contains(s, "\n") {
		return s
	}

	prefix := prettyIndentText(indent)

	lines := strings.Split(s, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "" {
			continue
		}
		lines[i] = prefix + lines[i]
	}

	return strings.Join(lines, "\n")
}

func valueToString(value TinyValue, forPrint ...bool) string {
	if value.IsInt {
		return strconv.Itoa(value.AsInt)
	}

	isForPrint := func() bool {
		if len(forPrint) > 0 && forPrint[0] {
			return true
		}

		return false
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
				parts[i] = "'" + value + "'"
				if isForPrint() {
					parts[i] = "\033[32m" + parts[i] + "\033[0m"
				}
			} else {
				parts[i] = valueToString(item)
				if isForPrint() && item.IsInt {
					parts[i] = "\033[36m" + parts[i] + "\033[0m"
				}
			}
		}

		return "[" + strings.Join(parts, ", ") + "]"

	case WasmArrayValue:
		return v.String()

	case *WasmArrayValue:
		if v == nil {
			return "null"
		}
		return v.String()

	case ErrorValue:
		return v.Kind + ": " + v.Message

	case *ErrorValue:
		return v.Kind + ": " + v.Message
	case NullValue:
		return "null"
	case nil:
		return "nil"

	case Class:
		return "<class " + v.Name + ">"

	case ObjectValue:
		return formatPrettyObject(v, 0, isForPrint())

	case *ObjectValue:
		if v == nil {
			return "null"
		}
		return formatPrettyObject(*v, 0, isForPrint())

	case *InstanceValue:
		return formatPrettyInstance(v, 0, isForPrint())

	case WasmObjectValue:
		return v.String()

	case *WasmObjectValue:
		if v == nil {
			return "null"
		}
		return v.String()

	case FunctionValue:
		return "<function " + v.Name + ">"
	case NativeServerValue:
		return "<server :" + strconv.Itoa(v.Port) + ">"
	case *NativeServerValue:
		return "<server :" + strconv.Itoa(v.Port) + ">"
	case *NativeSqliteValue:
		return "<sqlite>"
	case *NativeTimerValue:
		return "<" + v.Type.String() + ">"
	case *NativeTcpServerValue:
		return "<tcp server :" + strconv.Itoa(v.Port) + ">"
	case *NativeWebViewValue:
		return "<webview>"
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
	case InterfaceValue:
		return "<interface " + v.Name + ">"
	case *InterfaceValue:
		return "<interface " + v.Name + ">"
	case BufferValue:
		return "<buffer " + string(v.Bytes) + ">"
	case *NativeStringBuilderValue:
		return "<string builder>"
	case *BufferValue:
		return string(v.Bytes)
	case *NativeProcessValue:
		return "<process>"
	default:
		return strings.NewReplacer("*vm.", "", "vm.", "").Replace(fmt.Sprintf("%T", v))
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
	objectValue, ok := vm.valueAsObjectForRead(value)
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
	arrayValue, ok := vm.valueAsArrayForRead(value)
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
	case float64:
		return v != 0.0
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
	case *BufferValue:
		right, ok := b.Value.(*BufferValue)
		return ok && bytes.Equal(left.Bytes, right.Bytes)
	case float64:
		if b.IsInt {
			return left == float64(b.AsInt)
		}
		switch right := b.Value.(type) {
		case float64:
			return left == right
		default:
			return false
		}

	case string:
		if b.IsInt {
			return false
		}
		right, ok := b.Value.(string)
		return ok && left == right

	case bool:
		if b.IsInt {
			return false
		}
		right, ok := b.Value.(bool)
		return ok && left == right

	case NullValue:
		if b.IsInt {
			return false
		}
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

func NewArray(arr []TinyValue) TinyValue {
	return TinyValue{
		Value: &ArrayValue{
			Elements: arr,
		},
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
