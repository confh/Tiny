package vm

import (
	"net/http"
	"os"
	"runtime"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	. "language.com/src/tinyerrors"
)

type ObserverRuntimeStats struct {
	TasksStarted   atomic.Int64
	TasksActive    atomic.Int64
	TasksCompleted atomic.Int64
	TasksFailed    atomic.Int64
	FunctionCalls  atomic.Int64
	GCPercent      atomic.Int64
	mu             sync.Mutex
	FunctionCounts map[string]int64
	Status         atomic.Value
	messages       []ObjectValue
	events         []ObjectValue
	commands       map[string]FunctionValue
	exposed        map[string]FunctionValue
	shutdown       *FunctionValue
}

func newObserverRuntimeStats() *ObserverRuntimeStats {
	stats := &ObserverRuntimeStats{
		FunctionCounts: map[string]int64{},
		commands:       map[string]FunctionValue{},
		exposed:        map[string]FunctionValue{},
		events:         []ObjectValue{},
		messages:       []ObjectValue{},
	}
	stats.GCPercent.Store(100)
	stats.Status.Store("starting")
	return stats
}

func (s *ObserverRuntimeStats) TaskStarted() {
	if s == nil {
		return
	}
	s.TasksStarted.Add(1)
	s.TasksActive.Add(1)
}

func (s *ObserverRuntimeStats) TaskCompleted() {
	if s == nil {
		return
	}
	s.TasksCompleted.Add(1)
	s.TasksActive.Add(-1)
}

func (s *ObserverRuntimeStats) TaskFailed() {
	if s == nil {
		return
	}
	s.TasksFailed.Add(1)
	s.TasksActive.Add(-1)
}

func (s *ObserverRuntimeStats) FunctionCalled(name string) {
	if s == nil {
		return
	}
	s.FunctionCalls.Add(1)
	s.mu.Lock()
	s.FunctionCounts[name]++
	s.mu.Unlock()
}

func (s *ObserverRuntimeStats) FunctionCallRows() *ArrayValue {
	rows := &ArrayValue{Elements: []TinyValue{}}
	if s == nil {
		return rows
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for name, count := range s.FunctionCounts {
		rows.Elements = append(rows.Elements, NewNative(ObjectValue{
			"name":  NewNative(name),
			"calls": NewNative(float64(count)),
		}))
	}
	return rows
}

func (s *ObserverRuntimeStats) AddEvent(kind string, message string, data TinyValue) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, ObjectValue{
		"time":    NewNative(time.Now().Format(time.RFC3339)),
		"kind":    NewNative(kind),
		"message": NewNative(message),
		"data":    data,
	})
	if len(s.events) > 200 {
		s.events = append([]ObjectValue{}, s.events[len(s.events)-200:]...)
	}
}

func (s *ObserverRuntimeStats) AddMessage(from string, text string, data TinyValue) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.messages = append(s.messages, ObjectValue{
		"time": NewNative(time.Now().Format(time.RFC3339)),
		"from": NewNative(from),
		"text": NewNative(text),
		"data": data,
	})
	if len(s.messages) > 200 {
		s.messages = append([]ObjectValue{}, s.messages[len(s.messages)-200:]...)
	}
}

func objectRows(values []ObjectValue) *ArrayValue {
	rows := &ArrayValue{Elements: make([]TinyValue, 0, len(values))}
	for _, value := range values {
		rows.Elements = append(rows.Elements, NewNative(value))
	}
	return rows
}

var stdObserverMetadata = StdModuleInfo{
	Name: "observer",
}

var stdObserverMethods map[string]StdModuleFunc

func init() {
	stdObserverMethods = map[string]StdModuleFunc{
		"snapshot":   stdObserverSnapshot,
		"start":      stdObserverStart,
		"status":     stdObserverStatus,
		"event":      stdObserverEvent,
		"message":    stdObserverMessage,
		"command":    stdObserverCommand,
		"expose":     stdObserverExpose,
		"onShutdown": stdObserverOnShutdown,
	}
	registerStdModule(stdObserverMetadata)
}

func (vm *VM) callStdObserver(method string, args []TinyValue) {
	fn, ok := stdObserverMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown observer function: %s", method)
		return
	}

	fn(vm, args)
}

func stdObserverSnapshot(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "observer.snapshot", args)
	vm.push(NewNative(vm.observerSnapshot()))
}

func stdObserverStatus(vm *VM, args []TinyValue) {
	expectArgs(vm, "observer.status", args, 1)
	if vm.observerStats != nil {
		status := argString(vm, "observer.status", args, 0)
		vm.observerStats.Status.Store(status)
		vm.observerStats.AddEvent("status", status, NewNull())
	}
	vm.push(NewNull())
}

func stdObserverEvent(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "observer.event", args, 2, 3)
	if vm.observerStats != nil {
		data := NewNull()
		if len(args) == 3 {
			data = args[2]
		}
		vm.observerStats.AddEvent(argString(vm, "observer.event", args, 0), argString(vm, "observer.event", args, 1), data)
	}
	vm.push(NewNull())
}

func stdObserverMessage(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "observer.message", args, 2, 3)
	if vm.observerStats != nil {
		data := NewNull()
		if len(args) == 3 {
			data = args[2]
		}
		vm.observerStats.AddMessage(argString(vm, "observer.message", args, 0), argString(vm, "observer.message", args, 1), data)
	}
	vm.push(NewNull())
}

func stdObserverCommand(vm *VM, args []TinyValue) {
	expectArgs(vm, "observer.command", args, 2)
	if vm.observerStats != nil {
		name := argString(vm, "observer.command", args, 0)
		fn := argFn(vm, "observer.command", args, 1)
		vm.observerStats.mu.Lock()
		vm.observerStats.commands[name] = fn
		vm.observerStats.mu.Unlock()
		vm.observerStats.AddEvent("command.registered", name, NewNull())
	}
	vm.push(NewNull())
}

func stdObserverExpose(vm *VM, args []TinyValue) {
	expectArgs(vm, "observer.expose", args, 2)
	if vm.observerStats != nil {
		name := argString(vm, "observer.expose", args, 0)
		fn := argFn(vm, "observer.expose", args, 1)
		vm.observerStats.mu.Lock()
		vm.observerStats.exposed[name] = fn
		vm.observerStats.mu.Unlock()
		vm.observerStats.AddEvent("function.exposed", name, NewNull())
	}
	vm.push(NewNull())
}

func stdObserverOnShutdown(vm *VM, args []TinyValue) {
	expectArgs(vm, "observer.onShutdown", args, 1)
	if vm.observerStats != nil {
		fn := argFn(vm, "observer.onShutdown", args, 0)
		vm.observerStats.mu.Lock()
		vm.observerStats.shutdown = &fn
		vm.observerStats.mu.Unlock()
		vm.observerStats.AddEvent("shutdown.registered", "shutdown handler registered", NewNull())
	}
	vm.push(NewNull())
}

func stdObserverStart(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "observer.start", args, 0, 1)

	host := "127.0.0.1"
	port := 4040
	password := ""

	if len(args) == 1 {
		if args[0].IsInt {
			port = args[0].AsInt
		} else if options, ok := vm.valueAsObjectForRead(args[0]); ok {
			host = objectString(options, "host", host)
			port = objectInt(vm, options, "port", port)
			password = objectString(options, "password", password)
		} else {
			vm.runtimeError(ErrorType, "observer.start expects a port number or options object")
			return
		}
	}

	startedAt := time.Now()
	var requestCount atomic.Int64
	var lastAccess atomic.Value
	lastAccess.Store("")

	authorize := func(w http.ResponseWriter, r *http.Request) bool {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization, X-Observer-Password")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return false
		}
		if password == "" {
			return true
		}
		got := r.Header.Get("X-Observer-Password")
		if got == "" {
			const prefix = "Bearer "
			auth := r.Header.Get("Authorization")
			if len(auth) > len(prefix) && auth[:len(prefix)] == prefix {
				got = auth[len(prefix):]
			}
		}
		if got == "" {
			got = r.URL.Query().Get("password")
		}
		if got != password {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "observer password required",
			})
			return false
		}
		requestCount.Add(1)
		lastAccess.Store(time.Now().Format(time.RFC3339))
		return true
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/snapshot", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(cleanValueForJSON(NewNative(vm.observerSnapshot(ObjectValue{
			"serverStartedAt": NewNative(startedAt.Format(time.RFC3339)),
			"requestCount":    NewNative(float64(requestCount.Load())),
			"lastAccess":      NewNative(lastAccess.Load().(string)),
			"authRequired":    NewNative(password != ""),
		}))))
	})
	mux.HandleFunc("/gc", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		runtime.GC()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "gc",
		})
	})
	mux.HandleFunc("/control/gomaxprocs", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		value, err := strconv.Atoi(r.URL.Query().Get("value"))
		if err != nil || value < 1 {
			writeObserverError(w, http.StatusBadRequest, "value must be a positive number")
			return
		}
		previous := runtime.GOMAXPROCS(value)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"action":   "gomaxprocs",
			"previous": previous,
			"value":    runtime.GOMAXPROCS(0),
		})
	})
	mux.HandleFunc("/control/gc-percent", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		value, err := strconv.Atoi(r.URL.Query().Get("value"))
		if err != nil || value < -1 {
			writeObserverError(w, http.StatusBadRequest, "value must be -1 or greater")
			return
		}
		previous := debug.SetGCPercent(value)
		if vm.observerStats != nil {
			vm.observerStats.GCPercent.Store(int64(value))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":       true,
			"action":   "gc-percent",
			"previous": previous,
			"value":    value,
		})
	})
	mux.HandleFunc("/control/global", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		name := r.URL.Query().Get("name")
		kind := r.URL.Query().Get("type")
		raw := r.URL.Query().Get("value")
		if name == "" {
			writeObserverError(w, http.StatusBadRequest, "name is required")
			return
		}
		value, ok := observerParsePrimitive(kind, raw)
		if !ok {
			writeObserverError(w, http.StatusBadRequest, "unsupported global value type")
			return
		}
		if err := vm.observerSetGlobal(name, value); err != "" {
			writeObserverError(w, http.StatusBadRequest, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "global",
			"name":   name,
		})
	})
	mux.HandleFunc("/command", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		name := r.URL.Query().Get("name")
		payload := r.URL.Query().Get("payload")
		result, errText := vm.observerCallRegistered("command", name, payload)
		if errText != "" {
			writeObserverError(w, http.StatusBadRequest, errText)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "command",
			"name":   name,
			"result": cleanValueForJSON(result),
		})
	})
	mux.HandleFunc("/call", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		name := r.URL.Query().Get("name")
		payload := r.URL.Query().Get("payload")
		result, errText := vm.observerCallRegistered("exposed", name, payload)
		if errText != "" {
			writeObserverError(w, http.StatusBadRequest, errText)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "call",
			"name":   name,
			"result": cleanValueForJSON(result),
		})
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		from := r.URL.Query().Get("from")
		if from == "" {
			from = "observer"
		}
		text := r.URL.Query().Get("text")
		vm.observerStats.AddMessage(from, text, NewNull())
		vm.observerStats.AddEvent("message", text, NewNative(ObjectValue{"from": NewNative(from)}))
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "message",
		})
	})
	mux.HandleFunc("/shutdown", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		if vm.observerStats == nil || vm.observerStats.shutdown == nil {
			writeObserverError(w, http.StatusBadRequest, "shutdown handler is not registered")
			return
		}
		fn := *vm.observerStats.shutdown
		go func() {
			time.Sleep(100 * time.Millisecond)
			defer func() { recover() }()
			vm.callFunctionValue(fn, []TinyValue{NewNative("observer")})
		}()
		vm.observerStats.AddEvent("shutdown", "shutdown requested", NewNull())
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"action": "shutdown",
		})
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !authorize(w, r) {
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("Tiny observer is running. Try /snapshot\n"))
	})

	addr := host + ":" + strconv.Itoa(port)
	server := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		err := server.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			vm.runtimeError(ErrorRuntime, "observer server failed: %v", err)
		}
	}()

	vm.push(NewNative(ObjectValue{
		"host":         NewNative(host),
		"port":         NewInt(port),
		"url":          NewNative("http://" + addr),
		"authRequired": NewNative(password != ""),
	}))
}

func writeObserverError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": message,
	})
}

func observerParsePrimitive(kind string, raw string) (TinyValue, bool) {
	switch kind {
	case "string":
		return NewNative(raw), true
	case "number":
		i, err := strconv.Atoi(raw)
		if err == nil {
			return NewInt(i), true
		}
		f, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			return NewNull(), false
		}
		return NewNative(f), true
	case "bool":
		b, err := strconv.ParseBool(raw)
		if err != nil {
			return NewNull(), false
		}
		return NewNative(b), true
	case "null":
		return NewNull(), true
	default:
		return NewNull(), false
	}
}

func (vm *VM) observerSetGlobal(name string, value TinyValue) string {
	vm.mu.Lock()
	defer vm.mu.Unlock()

	slot, ok := vm.globalNames[name]
	if !ok {
		return "unknown global"
	}
	if vm.globalConstants[name] {
		return "cannot edit const global"
	}
	if slot < 0 || slot >= len(*vm.globals) {
		return "global slot out of range"
	}

	currentType := TypeName((*vm.globals)[slot])
	if currentType != "string" && currentType != "number" && currentType != "float" && currentType != "bool" && currentType != "null" {
		return "only primitive globals can be edited"
	}

	vm.setGlobalUnlocked(slot, value)
	return ""
}

func (vm *VM) observerCallRegistered(kind string, name string, payload string) (TinyValue, string) {
	if name == "" {
		return NewNull(), "name is required"
	}
	if vm.observerStats == nil {
		return NewNull(), "observer is not initialized"
	}
	var fn FunctionValue
	var ok bool
	vm.observerStats.mu.Lock()
	if kind == "command" {
		fn, ok = vm.observerStats.commands[name]
	} else {
		fn, ok = vm.observerStats.exposed[name]
	}
	vm.observerStats.mu.Unlock()
	if !ok {
		return NewNull(), kind + " is not registered"
	}

	arg := NewNative(payload)
	trimmed := strings.TrimSpace(payload)
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
		if parsed, err := parseTinyJSONDirect(trimmed); err == nil {
			arg = parsed
		}
	}

	var result TinyValue
	var panicValue any
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicValue = r
			}
		}()
		taskVM := vm.CloneForTask()
		result = taskVM.callFunctionValue(fn, []TinyValue{arg})
	}()
	if panicValue != nil {
		vm.observerStats.AddEvent(kind+".error", name, NewNative(valueToString(ToValue(panicValue))))
		return NewNull(), valueToString(ToValue(panicValue))
	}
	vm.observerStats.AddEvent(kind+".called", name, result)
	return result, ""
}

func (vm *VM) observerSnapshot(extra ...ObjectValue) ObjectValue {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	taskPool := ObjectValue{
		"active": NewInt(0),
		"idle":   NewInt(0),
		"limit":  NewInt(0),
	}
	if vm.taskPool != nil {
		taskPool = vm.taskPool.Snapshot()
	}

	globals := 0
	if vm.globals != nil {
		globals = len(*vm.globals)
	}

	globalNames := &ArrayValue{Elements: []TinyValue{}}
	globalValues := &ArrayValue{Elements: []TinyValue{}}
	vm.mu.RLock()
	sortedGlobalNames := make([]string, 0, len(vm.globalNames))
	for name := range vm.globalNames {
		sortedGlobalNames = append(sortedGlobalNames, name)
	}
	sort.Strings(sortedGlobalNames)
	for _, name := range sortedGlobalNames {
		globalNames.Elements = append(globalNames.Elements, NewNative(name))
		slot := vm.globalNames[name]
		value := NewNull()
		if slot >= 0 && slot < len(*vm.globals) {
			value = (*vm.globals)[slot]
		}
		valueType := TypeName(value)
		editable := !vm.globalConstants[name] && (valueType == "string" || valueType == "number" || valueType == "float" || valueType == "bool" || valueType == "null")
		globalValues.Elements = append(globalValues.Elements, NewNative(ObjectValue{
			"name":     NewNative(name),
			"type":     NewNative(valueType),
			"value":    NewNative(valueToString(value)),
			"constant": NewNative(vm.globalConstants[name]),
			"editable": NewNative(editable),
		}))
	}
	vm.mu.RUnlock()

	functionNames := &ArrayValue{Elements: []TinyValue{}}
	sortedFunctionNames := make([]string, 0, len(vm.functions))
	for name := range vm.functions {
		sortedFunctionNames = append(sortedFunctionNames, name)
	}
	sort.Strings(sortedFunctionNames)
	for _, name := range sortedFunctionNames {
		functionNames.Elements = append(functionNames.Elements, NewNative(name))
	}

	classNames := &ArrayValue{Elements: []TinyValue{}}
	sortedClassNames := make([]string, 0, len(vm.classes))
	for name := range vm.classes {
		sortedClassNames = append(sortedClassNames, name)
	}
	sort.Strings(sortedClassNames)
	for _, name := range sortedClassNames {
		classNames.Elements = append(classNames.Elements, NewNative(name))
	}

	observer := ObjectValue{
		"serverStartedAt": NewNative(""),
		"requestCount":    NewInt(0),
		"lastAccess":      NewNative(""),
		"authRequired":    NewNative(false),
	}
	if len(extra) > 0 {
		for k, v := range extra[0] {
			observer[k] = v
		}
	}

	tasks := ObjectValue{
		"started":   NewNative(float64(0)),
		"active":    NewNative(float64(0)),
		"completed": NewNative(float64(0)),
		"failed":    NewNative(float64(0)),
		"calls":     NewNative(float64(0)),
	}
	if vm.observerStats != nil {
		tasks = ObjectValue{
			"started":   NewNative(float64(vm.observerStats.TasksStarted.Load())),
			"active":    NewNative(float64(vm.observerStats.TasksActive.Load())),
			"completed": NewNative(float64(vm.observerStats.TasksCompleted.Load())),
			"failed":    NewNative(float64(vm.observerStats.TasksFailed.Load())),
			"calls":     NewNative(float64(vm.observerStats.FunctionCalls.Load())),
		}
	}
	functionCalls := &ArrayValue{Elements: []TinyValue{}}
	if vm.observerStats != nil {
		functionCalls = vm.observerStats.FunctionCallRows()
	}

	commands := &ArrayValue{Elements: []TinyValue{}}
	exposed := &ArrayValue{Elements: []TinyValue{}}
	events := &ArrayValue{Elements: []TinyValue{}}
	messages := &ArrayValue{Elements: []TinyValue{}}
	status := "starting"
	shutdownRegistered := false
	if vm.observerStats != nil {
		if v, ok := vm.observerStats.Status.Load().(string); ok {
			status = v
		}
		vm.observerStats.mu.Lock()
		commandNames := make([]string, 0, len(vm.observerStats.commands))
		for name := range vm.observerStats.commands {
			commandNames = append(commandNames, name)
		}
		sort.Strings(commandNames)
		for _, name := range commandNames {
			commands.Elements = append(commands.Elements, NewNative(ObjectValue{"name": NewNative(name)}))
		}
		exposedNames := make([]string, 0, len(vm.observerStats.exposed))
		for name := range vm.observerStats.exposed {
			exposedNames = append(exposedNames, name)
		}
		sort.Strings(exposedNames)
		for _, name := range exposedNames {
			exposed.Elements = append(exposed.Elements, NewNative(ObjectValue{"name": NewNative(name)}))
		}
		events = objectRows(append([]ObjectValue{}, vm.observerStats.events...))
		messages = objectRows(append([]ObjectValue{}, vm.observerStats.messages...))
		shutdownRegistered = vm.observerStats.shutdown != nil
		vm.observerStats.mu.Unlock()
	}

	gcPercent := int64(100)
	if vm.observerStats != nil {
		gcPercent = vm.observerStats.GCPercent.Load()
	}

	executable, _ := os.Executable()
	cwd, _ := os.Getwd()

	return ObjectValue{
		"uptimeMs":       NewNative(float64(time.Since(time.UnixMilli(vm.start)).Milliseconds())),
		"pid":            NewInt(os.Getpid()),
		"executable":     NewNative(executable),
		"cwd":            NewNative(cwd),
		"goos":           NewNative(runtime.GOOS),
		"goarch":         NewNative(runtime.GOARCH),
		"goroutines":     NewInt(runtime.NumGoroutine()),
		"gomaxprocs":     NewInt(runtime.GOMAXPROCS(0)),
		"functionCount":  NewInt(len(vm.functions)),
		"classCount":     NewInt(len(vm.classes)),
		"interfaceCount": NewInt(len(vm.interfaces)),
		"globalCount":    NewInt(globals),
		"globalNames":    NewNative(globalNames),
		"globals":        NewNative(globalValues),
		"functionNames":  NewNative(functionNames),
		"functionCalls":  NewNative(functionCalls),
		"classNames":     NewNative(classNames),
		"stackDepth":     NewInt(vm.top),
		"frameDepth":     NewInt(len(vm.frames)),
		"taskPool":       NewNative(taskPool),
		"tasks":          NewNative(tasks),
		"observer":       NewNative(observer),
		"status":         NewNative(status),
		"commands":       NewNative(commands),
		"exposed":        NewNative(exposed),
		"events":         NewNative(events),
		"messages":       NewNative(messages),
		"shutdown": NewNative(ObjectValue{
			"registered": NewNative(shutdownRegistered),
		}),
		"controls": NewNative(ObjectValue{
			"gomaxprocs": NewInt(runtime.GOMAXPROCS(0)),
			"gcPercent":  NewNative(float64(gcPercent)),
		}),
		"memory": NewNative(ObjectValue{
			"alloc":       NewNative(float64(mem.Alloc)),
			"totalAlloc":  NewNative(float64(mem.TotalAlloc)),
			"sys":         NewNative(float64(mem.Sys)),
			"heapAlloc":   NewNative(float64(mem.HeapAlloc)),
			"heapSys":     NewNative(float64(mem.HeapSys)),
			"heapObjects": NewNative(float64(mem.HeapObjects)),
			"numGC":       NewInt(int(mem.NumGC)),
			"nextGC":      NewNative(float64(mem.NextGC)),
			"pauseTotal":  NewNative(float64(mem.PauseTotalNs)),
		}),
	}
}
