package vm

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gorilla/websocket"
	. "language.com/src/tinyerrors"
)

var websocketConnMethods map[string]NativeModuleFunc[*NativeWebsocketConnValue]
var websocketServerMethods map[string]NativeModuleFunc[*NativeWebsocketServerValue]

func init() {
	websocketConnMethods = map[string]NativeModuleFunc[*NativeWebsocketConnValue]{
		"send":       websocketConnSend,
		"sendText":   websocketConnSendText,
		"sendJson":   websocketConnSendJson,
		"sendBytes":  websocketConnSendBytes,
		"read":       websocketConnRead,
		"readText":   websocketConnReadText,
		"readJson":   websocketConnReadJson,
		"onMessage":  websocketConnOnMessage,
		"onClose":    websocketConnOnClose,
		"onError":    websocketConnOnError,
		"ping":       websocketConnPing,
		"pong":       websocketConnPong,
		"close":      websocketConnClose,
		"isOpen":     websocketConnIsOpen,
		"remoteAddr": websocketConnRemoteAddr,
		"headers":    websocketConnHeaders,
	}

	websocketServerMethods = map[string]NativeModuleFunc[*NativeWebsocketServerValue]{
		"onConnection":  websocketServerOnConnection,
		"onMessage":     websocketServerOnMessage,
		"onClose":       websocketServerOnClose,
		"onError":       websocketServerOnError,
		"start":         websocketServerStart,
		"stop":          websocketServerStop,
		"isRunning":     websocketServerIsRunning,
		"broadcastText": websocketServerBroadcastText,
		"broadcastJson": websocketServerBroadcastJson,
		"closeAll":      websocketServerCloseAll,
	}
}

func (vm *VM) callNativeWebsocketConnMethod(conn *NativeWebsocketConnValue, method string, args []TinyValue) {
	fn, ok := websocketConnMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown websocket connection method: %s", method)
		return
	}
	fn(vm, conn, args)
}

func (vm *VM) callNativeWebsocketServerMethod(server *NativeWebsocketServerValue, method string, args []TinyValue) {
	fn, ok := websocketServerMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown websocket server method: %s", method)
		return
	}
	fn(vm, server, args)
}

// Connection Methods

func websocketConnSend(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.send", args, 1)
	data := argString(vm, "websocketConnection.send", args, 0)

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return
	}

	err := conn.conn.WriteMessage(websocket.TextMessage, []byte(data))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket send failed: %v", err)
	}
	vm.push(NewNull())
}

func websocketConnSendText(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	websocketConnSend(vm, conn, args)
}

func websocketConnSendJson(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.sendJson", args, 1)
	data := argObject(vm, "websocketConnection.sendJson", args, 0)

	jsonData, err := json.Marshal(cleanValueForJSON(NewNative(data)))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket sendJson failed: %v", err)
		return
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return
	}

	err = conn.conn.WriteMessage(websocket.TextMessage, jsonData)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket send failed: %v", err)
	}
	vm.push(NewNull())
}

func websocketConnSendBytes(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.sendBytes", args, 1)
	data := asBuffer(args[0], vm)

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return
	}

	err := conn.conn.WriteMessage(websocket.BinaryMessage, data.Bytes)
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket sendBytes failed: %v", err)
	}
	vm.push(NewNull())
}

func websocketConnRead(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.read", args, 0)

	msgType, data, err := conn.conn.ReadMessage()
	if err != nil {
		if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			vm.push(NewNative(ObjectValue{
				"type": NewNative("close"),
				"data": NewNull(),
			}))
			return
		}
		vm.runtimeError(ErrorRuntime, "websocket read failed: %v", err)
		return
	}

	typeName := "text"
	var resultData TinyValue

	if msgType == websocket.BinaryMessage {
		typeName = "binary"
		resultData = NewNative(&BufferValue{Bytes: data})
	} else {
		resultData = NewNative(string(data))
	}

	vm.push(NewNative(ObjectValue{
		"type": NewNative(typeName),
		"data": resultData,
	}))
}

func websocketConnReadText(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.readText", args, 0)

	msgType, data, err := conn.conn.ReadMessage()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket readText failed: %v", err)
		return
	}

	if msgType != websocket.TextMessage {
		vm.runtimeError(ErrorRuntime, "websocket expected text message, got %d", msgType)
		return
	}

	vm.push(NewNative(string(data)))
}

func websocketConnReadJson(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.readJson", args, 0)

	msgType, data, err := conn.conn.ReadMessage()
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket readJson failed: %v", err)
		return
	}

	if msgType != websocket.TextMessage {
		vm.runtimeError(ErrorRuntime, "websocket expected text message for JSON, got %d", msgType)
		return
	}

	result, err := parseTinyJSONDirect(string(data))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket failed to parse JSON: %v", err)
		return
	}

	vm.push(jsonToTinyValue(result))
}

func (conn *NativeWebsocketConnValue) startReadLoop(vm *VM, pool *VMPool) {
	if pool == nil {
		pool = NewVMPool(4, 2, func() *VM {
			return vm.CloneForTask()
		})
	}

	go func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("[websocket connection handler panic] %v\n", r)
			}
		}()
		for {
			msgType, data, err := conn.conn.ReadMessage()
			if err != nil {
				conn.mu.Lock()
				conn.closed = true
				conn.mu.Unlock()

				if conn.OnClose != nil {
					code := 1006
					reason := err.Error()
					if closeErr, ok := err.(*websocket.CloseError); ok {
						code = closeErr.Code
						reason = closeErr.Text
					}

					event := ObjectValue{
						"code":     NewInt(code),
						"reason":   NewNative(reason),
						"wasClean": NewNative(websocket.IsCloseError(err, websocket.CloseNormalClosure)),
					}

					worker := pool.Get()
					worker.callFunctionValue(*conn.OnClose, []TinyValue{NewNative(event)})
					pool.Put(worker)
				}

				if conn.server != nil {
					conn.server.mu.Lock()
					delete(conn.server.conns, conn)
					conn.server.mu.Unlock()
				}
				return
			}

			if conn.OnMessage != nil {
				typeName := "text"
				var resultData TinyValue

				if msgType == websocket.BinaryMessage {
					typeName = "binary"
					resultData = NewNative(&BufferValue{Bytes: data})
				} else {
					typeName = "text"
					resultData = NewNative(string(data))
				}

				msg := ObjectValue{
					"type": NewNative(typeName),
					"data": resultData,
				}

				worker := pool.Get()
				worker.callFunctionValue(*conn.OnMessage, []TinyValue{NewNative(conn), NewNative(msg)})
				pool.Put(worker)
			}
		}
	}()
}

func websocketConnOnMessage(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.onMessage", args, 1)
	fn := argFn(vm, "websocketConnection.onMessage", args, 0)
	conn.OnMessage = &fn
	conn.startReadLoop(vm, nil)
	vm.push(NewNull())
}

func websocketConnOnClose(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.onClose", args, 1)
	fn := argFn(vm, "websocketConnection.onClose", args, 0)
	conn.OnClose = &fn
	vm.push(NewNull())
}

func websocketConnOnError(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.onError", args, 1)
	fn := argFn(vm, "websocketConnection.onError", args, 0)
	conn.OnError = &fn
	vm.push(NewNull())
}

func websocketConnPing(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgsRange(vm, "websocketConnection.ping", args, 0, 1)
	data := ""
	if len(args) == 1 {
		data = argString(vm, "websocketConnection.ping", args, 0)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return
	}

	err := conn.conn.WriteMessage(websocket.PingMessage, []byte(data))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket ping failed: %v", err)
	}
	vm.push(NewNull())
}

func websocketConnPong(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgsRange(vm, "websocketConnection.pong", args, 0, 1)
	data := ""
	if len(args) == 1 {
		data = argString(vm, "websocketConnection.pong", args, 0)
	}

	conn.mu.Lock()
	defer conn.mu.Unlock()
	if conn.closed {
		return
	}

	err := conn.conn.WriteMessage(websocket.PongMessage, []byte(data))
	if err != nil {
		vm.runtimeError(ErrorRuntime, "websocket pong failed: %v", err)
	}
	vm.push(NewNull())
}

func websocketConnClose(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgsRange(vm, "websocketConnection.close", args, 0, 2)
	code := websocket.CloseNormalClosure
	reason := ""

	if len(args) >= 1 {
		code = argInt(vm, "websocketConnection.close", args, 0)
	}
	if len(args) >= 2 {
		reason = argString(vm, "websocketConnection.close", args, 1)
	}

	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		vm.push(NewNull())
		return
	}
	conn.closed = true
	conn.mu.Unlock()

	msg := websocket.FormatCloseMessage(code, reason)
	_ = conn.conn.WriteMessage(websocket.CloseMessage, msg)
	_ = conn.conn.Close()

	vm.push(NewNull())
}

func websocketConnIsOpen(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.isOpen", args, 0)
	conn.mu.Lock()
	open := !conn.closed
	conn.mu.Unlock()
	vm.push(NewNative(open))
}

func websocketConnRemoteAddr(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.remoteAddr", args, 0)
	vm.push(NewNative(conn.conn.RemoteAddr().String()))
}

func websocketConnHeaders(vm *VM, conn *NativeWebsocketConnValue, args []TinyValue) {
	expectArgs(vm, "websocketConnection.headers", args, 0)
	if conn.headers == nil {
		vm.push(NewNative(ObjectValue{}))
		return
	}
	vm.push(NewNative(conn.headers))
}

// Server Methods

func websocketServerOnConnection(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.onConnection", args, 1)
	fn := argFn(vm, "websocketServer.onConnection", args, 0)
	server.OnConnection = &fn
	vm.push(NewNull())
}

func websocketServerOnMessage(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.onMessage", args, 1)
	fn := argFn(vm, "websocketServer.onMessage", args, 0)
	server.OnMessage = &fn
	vm.push(NewNull())
}

func websocketServerOnClose(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.onClose", args, 1)
	fn := argFn(vm, "websocketServer.onClose", args, 0)
	server.OnClose = &fn
	vm.push(NewNull())
}

func websocketServerOnError(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.onError", args, 1)
	fn := argFn(vm, "websocketServer.onError", args, 0)
	server.OnError = &fn
	vm.push(NewNull())
}

func websocketServerStart(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgsRange(vm, "websocketServer.start", args, 0, 1)
	async := false
	if len(args) == 1 {
		async = argBool(vm, "websocketServer.start", args, 0)
	}
	if server.Workers == nil {
		server.Workers = NewVMPool(16, 8, func() *VM {
			return vm.CloneForTask()
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc(server.Path, func(w http.ResponseWriter, r *http.Request) {
		conn, err := server.upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}

		headers := ObjectValue{}
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = NewNative(v[0])
			}
		}

		nativeConn := &NativeWebsocketConnValue{
			conn:    conn,
			server:  server,
			headers: headers,
		}

		server.mu.Lock()
		server.conns[nativeConn] = true
		server.mu.Unlock()

		if server.OnConnection != nil {
			worker := server.Workers.Get()
			defer server.Workers.Put(worker)

			worker.callFunctionValue(*server.OnConnection, []TinyValue{NewNative(nativeConn)})
		}

		if server.OnMessage != nil {
			nativeConn.OnMessage = server.OnMessage
		}
		if server.OnClose != nil {
			nativeConn.OnClose = server.OnClose
		}

		nativeConn.startReadLoop(vm, server.Workers)
	})

	addr := server.Host + ":" + strconv.Itoa(server.Port)
	s := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	server.server = s
	server.Running = true

	if async {
		go func() {
			_ = s.ListenAndServe()
			server.Running = false
		}()
	} else {
		_ = s.ListenAndServe()
		server.Running = false
	}

	vm.push(NewNull())
}

func websocketServerStop(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.stop", args, 0)
	if server.server != nil {
		_ = server.server.Close()
	}
	server.Running = false
	vm.push(NewNull())
}

func websocketServerIsRunning(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.isRunning", args, 0)
	vm.push(NewNative(server.Running))
}

func websocketServerBroadcastText(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.broadcastText", args, 1)
	data := argString(vm, "websocketServer.broadcastText", args, 0)

	server.mu.Lock()
	defer server.mu.Unlock()

	for conn := range server.conns {
		conn.mu.Lock()
		if !conn.closed {
			_ = conn.conn.WriteMessage(websocket.TextMessage, []byte(data))
		}
		conn.mu.Unlock()
	}
	vm.push(NewNull())
}

func websocketServerBroadcastJson(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgs(vm, "websocketServer.broadcastJson", args, 1)
	data := argObject(vm, "websocketServer.broadcastJson", args, 0)

	jsonData, _ := json.Marshal(cleanValueForJSON(NewNative(data)))

	server.mu.Lock()
	defer server.mu.Unlock()

	for conn := range server.conns {
		conn.mu.Lock()
		if !conn.closed {
			_ = conn.conn.WriteMessage(websocket.TextMessage, jsonData)
		}
		conn.mu.Unlock()
	}
	vm.push(NewNull())
}

func websocketServerCloseAll(vm *VM, server *NativeWebsocketServerValue, args []TinyValue) {
	expectArgsRange(vm, "websocketServer.closeAll", args, 0, 2)
	code := websocket.CloseNormalClosure
	reason := ""

	if len(args) >= 1 {
		code = argInt(vm, "websocketServer.closeAll", args, 0)
	}
	if len(args) >= 2 {
		reason = argString(vm, "websocketServer.closeAll", args, 1)
	}

	server.mu.Lock()
	defer server.mu.Unlock()

	msg := websocket.FormatCloseMessage(code, reason)
	for conn := range server.conns {
		conn.mu.Lock()
		if !conn.closed {
			_ = conn.conn.WriteMessage(websocket.CloseMessage, msg)
			_ = conn.conn.Close()
			conn.closed = true
		}
		conn.mu.Unlock()
		delete(server.conns, conn)
	}
	vm.push(NewNull())
}
