package vm

import (
	"unsafe"

	"github.com/vmihailenco/msgpack/v5"
	. "language.com/src/tinyerrors"
)

func cBytesToGo(ptr uintptr) []byte {
	if ptr == 0 {
		return nil
	}
	length := *(*uint32)(unsafe.Pointer(ptr))
	dataPtr := ptr + 4
	rawSlice := unsafe.Slice((*byte)(unsafe.Pointer(dataPtr)), int(length))

	// Copy the bytes to Go memory so the plugin can safely free the C pointer
	goBytes := make([]byte, len(rawSlice))
	copy(goBytes, rawSlice)
	return goBytes
}

func (vm *VM) encodeToMsgPack(method string, args []TinyValue) []byte {
	compatArgs := make([]any, len(args))
	for i, arg := range args {
		compatArgs[i] = valueToJSONCompatible(arg)
	}

	payload := map[string]any{
		"method": method,
		"args":   compatArgs,
	}

	bytes, err := msgpack.Marshal(payload)
	if err != nil {
		vm.fatalError(ErrorRuntime, "failed to encode plugin MsgPack payload: %v", err)
	}
	return bytes
}

func (vm *VM) decodeFromMsgPack(data []byte) TinyValue {
	var result any
	err := msgpack.Unmarshal(data, &result)
	if err != nil {
		vm.fatalError(ErrorRuntime, "plugin returned invalid MsgPack: %v", err)
	}
	// Handle native plugin errors returned from the library
	if obj, ok := result.(map[string]any); ok {
		if errValue, exists := obj["error"]; exists {
			if errObj, ok := errValue.(map[string]any); ok {
				kind, _ := errObj["kind"].(string)
				message, _ := errObj["message"].(string)
				if kind == "" {
					kind = string(ErrorRuntime)
				}
				if message == "" {
					message = "plugin returned an error"
				}
				vm.fatalError(ErrorKind(kind), "%s", message)
			}
		}
	}
	return ToValue(result)
}

var AvailablePlugins = []string{
	"array",
	"math",
	"strings",
	"json",
	"fs",
	"app",
	"buffer",
	"regex",
	"io",
	"process",
	"time",
	"error",
	"http",
	"os",
	"runtime",
	"net",
	"path",
	"object",
	"observer",
	"desktop",
	"sync",
	"tests",
	"ui",
	"websocket",
	"tray",
	"validate",
	"url",
	"crypto",
	"sqlite",
	"wasm",
}
