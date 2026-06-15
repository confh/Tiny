package vm

import (
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	. "language.com/src/tinyerrors"
)

func (v *NativeWebsocketServerValue) TinyTypeName() string {
	return "websocket.Server"
}

func (v *NativeWebsocketConnValue) TinyTypeName() string {
	return "websocket.Conn"
}

var stdWebsocketMetadata = StdModuleInfo{
	Name: "websocket",
}

var stdWebsocketMethods map[string]StdModuleFunc

func init() {
	stdWebsocketMethods = map[string]StdModuleFunc{
		"connect":   websocketConnect,
		"server":    websocketServer,
		"text":      websocketText,
		"json":      websocketJson,
		"binary":    websocketBinary,
		"isText":    websocketIsText,
		"isBinary":  websocketIsBinary,
		"isClose":   websocketIsClose,
		"parseJson": websocketParseJson,
	}
	registerStdModule(stdWebsocketMetadata)
}

func (vm *VM) callStdWebsocket(method string, args []TinyValue) {
	fn, ok := stdWebsocketMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown websocket function: %s", method)
		return
	}

	fn(vm, args)
}

func websocketConnect(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "websocket.connect", args, 1, 2)

	url := argString(vm, "websocket.connect", args, 0)
	options := optionalObjectArg(vm, args, 1)

	header := http.Header{}
	if hVal, exists := options["headers"]; exists {
		if h, ok := vm.valueAsObjectForRead(hVal); ok {
			for k, v := range h {
				header.Set(valueToString(ToValue(k)), valueToString(v))
			}
		}
	}

	dialer := websocket.DefaultDialer
	if timeout := objectInt(vm, options, "timeoutMs", 0); timeout > 0 {
		dialer.HandshakeTimeout = time.Duration(timeout) * time.Millisecond
	}

	conn, _, err := dialer.Dial(url, header)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket dial failed: %v", err)
		return
	}

	if maxMsg := objectInt(vm, options, "maxMessageSize", 0); maxMsg > 0 {
		conn.SetReadLimit(int64(maxMsg))
	}

	vm.push(NewNative(&NativeWebsocketConnValue{
		Url:  url,
		conn: conn,
	}))
}

func websocketServer(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.server", args, 1)

	server := &NativeWebsocketServerValue{
		conns: make(map[*NativeWebsocketConnValue]bool),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}

	if args[0].IsInt {
		server.Port = args[0].AsInt
		server.Path = "/"
	} else if config, ok := vm.valueAsObjectForRead(args[0]); ok {
		server.Port = objectInt(vm, config, "port", 0)
		server.Host = objectString(config, "host", "")
		server.Path = objectString(config, "path", "/")

		if server.Path == "" {
			server.Path = "/"
		}

		if !strings.HasPrefix(server.Path, "/") {
			server.Path = "/" + server.Path
		}

		server.MaxMessageSize = objectInt(vm, config, "maxMessageSize", 0)
	} else {
		vm.runtimeError(ErrorType, "websocket.server expects port number or options object")
		return
	}

	if server.Port == 0 {
		vm.runtimeError(ErrorRuntime, "websocket.server requires a non-zero port")
		return
	}

	if server.MaxMessageSize > 0 {
		server.upgrader.ReadBufferSize = server.MaxMessageSize
		server.upgrader.WriteBufferSize = server.MaxMessageSize
	}

	vm.push(NewNative(server))
}

func websocketText(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.text", args, 1)
	data := argString(vm, "websocket.text", args, 0)

	vm.push(NewNative(ObjectValue{
		"type": NewNative("text"),
		"data": NewNative(data),
	}))
}

func websocketJson(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.json", args, 1)
	data := argObject(vm, "websocket.json", args, 0)

	vm.push(NewNative(ObjectValue{
		"type": NewNative("text"),
		"data": NewNative(data),
	}))
}

func websocketBinary(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.binary", args, 1)
	data := asBuffer(args[0], vm)

	vm.push(NewNative(ObjectValue{
		"type": NewNative("binary"),
		"data": NewNative(data),
	}))
}

func websocketIsText(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.isText", args, 1)
	msg := argObject(vm, "websocket.isText", args, 0)

	vm.push(NewNative(objectString(msg, "type", "") == "text"))
}

func websocketIsBinary(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.isBinary", args, 1)
	msg := argObject(vm, "websocket.isBinary", args, 0)

	vm.push(NewNative(objectString(msg, "type", "") == "binary"))
}

func websocketIsClose(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.isClose", args, 1)
	msg := argObject(vm, "websocket.isClose", args, 0)

	vm.push(NewNative(objectString(msg, "type", "") == "close"))
}

func websocketParseJson(vm *VM, args []TinyValue) {
	expectArgs(vm, "websocket.parseJson", args, 1)
	msg := argObject(vm, "websocket.parseJson", args, 0)

	data := msg["data"]
	if str, ok := data.Value.(string); ok {
		vm.push(jsonToTinyValue(str)) // This is a bit simplified, but follows pattern
	} else {
		vm.push(data)
	}
}
