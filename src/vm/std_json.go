package vm

import (
	"fmt"
	"math"
	"os"
	"strconv"
	"sync"
	"unsafe"

	json "github.com/goccy/go-json"
	. "language.com/src/tinyerrors"
)

var stdJsonMetadata = StdModuleInfo{
	Name: "json",
}

var stdJsonMethods map[string]StdModuleFunc

var tinyJSONBufPool = sync.Pool{
	New: func() any {
		b := make([]byte, 0, 512)
		return &b
	},
}

func unsafeStringBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}

func appendJSONString(out []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == '"' || c == '\\' {
			return strconv.AppendQuote(out, s)
		}
	}

	out = append(out, '"')
	out = append(out, s...)
	out = append(out, '"')
	return out
}

func appendJSONKey(out []byte, key any) []byte {
	switch k := key.(type) {
	case string:
		return appendJSONString(out, k)
	default:
		return appendJSONString(out, fmt.Sprint(k))
	}
}

func appendTinyJSON(out []byte, value TinyValue) []byte {
	if value.IsInt {
		return strconv.AppendInt(out, int64(value.AsInt), 10)
	}

	if value.Value == nil {
		return append(out, "null"...)
	}

	switch v := value.Value.(type) {
	case NullValue, *NullValue:
		return append(out, "null"...)

	case string:
		return appendJSONString(out, v)

	case bool:
		if v {
			return append(out, "true"...)
		}
		return append(out, "false"...)

	case int:
		return strconv.AppendInt(out, int64(v), 10)

	case int64:
		return strconv.AppendInt(out, v, 10)

	case uint64:
		return strconv.AppendUint(out, v, 10)

	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return append(out, "null"...)
		}
		return strconv.AppendFloat(out, f, 'g', -1, 32)

	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return append(out, "null"...)
		}
		return strconv.AppendFloat(out, v, 'g', -1, 64)

	case ObjectValue:
		return appendTinyJSONObject(out, v)

	case *ObjectValue:
		if v == nil {
			return append(out, "null"...)
		}
		return appendTinyJSONObject(out, *v)

	case ArrayValue:
		return appendTinyJSONArray(out, v.Elements)

	case *ArrayValue:
		if v == nil {
			return append(out, "null"...)
		}
		return appendTinyJSONArray(out, v.Elements)

	default:
		compatible := valueToJSONCompatible(value)
		bytes, err := json.Marshal(compatible)
		if err != nil {
			return append(out, "null"...)
		}
		return append(out, bytes...)
	}
}

func appendTinyJSONArray(out []byte, elements []TinyValue) []byte {
	out = append(out, '[')

	for i, elem := range elements {
		if i > 0 {
			out = append(out, ',')
		}
		out = appendTinyJSON(out, elem)
	}

	out = append(out, ']')
	return out
}

func appendTinyJSONObject(out []byte, obj ObjectValue) []byte {
	out = append(out, '{')

	first := true
	for key, val := range obj {
		if !first {
			out = append(out, ',')
		}
		first = false

		out = appendJSONKey(out, key)
		out = append(out, ':')
		out = appendTinyJSON(out, val)
	}

	out = append(out, '}')
	return out
}

func stringifyTinyJSONFast(value TinyValue) string {
	bufPtr := tinyJSONBufPool.Get().(*[]byte)
	buf := (*bufPtr)[:0]

	buf = appendTinyJSON(buf, value)

	result := string(buf)

	if cap(buf) <= 64*1024 {
		*bufPtr = buf
		tinyJSONBufPool.Put(bufPtr)
	}

	return result
}

func init() {
	stdJsonMethods = map[string]StdModuleFunc{
		"stringify": stdJsonStringify,
		"pretty":    stdJsonPretty,
		"parse":     stdJsonParse,
		"readFile":  stdJsonReadFile,
		"writeFile": stdJsonWriteFile,
	}
	registerStdModule(stdJsonMetadata)
}

func (vm *VM) callStdJson(method string, args []TinyValue) {
	fn, ok := stdJsonMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown json function: %s", method)
		return
	}
	fn(vm, args)
}

func stdJsonStringify(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.stringify", args, 1)

	result := stringifyTinyJSONFast(args[0])

	vm.push(NewNative(result))
}

func stdJsonPretty(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.pretty", args, 1)

	switch value := args[0].Value.(type) {
	case ObjectValue, ArrayValue, *ArrayValue:
		jsonValue := valueToJSONCompatible(ToValue(value))
		bytes, err := json.MarshalIndent(jsonValue, "", "  ")
		if err != nil {
			vm.fatalError(ErrorRuntime, "failed to convert value to JSON: %v", err)
		}
		vm.push(NewNative(string(bytes)))
	default:
		vm.fatalError(ErrorType, "json.pretty expected an array or an object, got %s", TypeName(ToValue(value)))
	}
}

func stdJsonParse(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.parse", args, 1)

	stringified := argString(vm, "json.parse", args, 0)

	result, err := parseTinyJSONDirect(stringified)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "invalid JSON: %v", err)
		vm.push(NewNull())
		return
	}

	vm.push(result)
}

func stdJsonReadFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.readFile", args, 1)

	fileName := argString(vm, "json.readFile", args, 0)

	data, err := os.ReadFile(fileName)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error reading file: %s", err)
		vm.push(NewNull())
		return
	}

	var result any
	err = json.Unmarshal(data, &result)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "could not parse file '%s' as json", fileName)
		vm.push(NewNull())
		return
	}

	vm.push(jsonToTinyValue(result))
}

func stdJsonWriteFile(vm *VM, args []TinyValue) {
	expectArgs(vm, "json.writeFile", args, 2)

	value := argObject(vm, "json.writeFile", args, 0)
	fileName := argString(vm, "json.writeFile", args, 1)

	jsonValue := valueToJSONCompatible(NewNative(value))
	bytes, err := json.MarshalIndent(jsonValue, "", "  ")
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error converting value to JSON: %s", err)
		vm.push(NewNull())
		return
	}

	err = os.WriteFile(fileName, bytes, 0644)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "error writing json file: %s", err)
		vm.push(NewNull())
		return
	}

	vm.push(NewNull())
}
