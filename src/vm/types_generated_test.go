package vm

import (
	"testing"
)

func TestStandardInterfaceHintsPopulated(t *testing.T) {
	required := map[string]int{
		"http.RequestObject":   14,
		"http.HttpResponse":    4,
		"http.RequestOptions":  3,
		"http.ServerOptions":   5,
		"tray.Bounds":          4,
		"websocket.Message":    2,
		"websocket.CloseEvent": 3,
		"process.ProcessResult": 4,
		"runtime.MemoryStats":  4,
		"validate.SafeParseResult": 3,
	}

	for key, expectedFields := range required {
		iface, ok := standardInterfaceHints[key]
		if !ok {
			t.Errorf("standardInterfaceHints missing %q", key)
			continue
		}
		if len(iface.Fields) != expectedFields {
			t.Errorf("standardInterfaceHints[%q] has %d fields, expected %d", key, len(iface.Fields), expectedFields)
		}
	}
}

func TestRuntimeTypeResolutionViaAlias(t *testing.T) {
	globals := []TinyValue{NewNative(&StandardModuleValue{Name: "http"})}
	globalNames := map[string]int{"htt": 0}

	iface, ok := resolveStandardInterfaceAlias("htt.RequestObject", globals, globalNames)
	if !ok {
		t.Fatal("expected htt.RequestObject to resolve via standardInterfaceAlias")
	}
	if len(iface.Fields) == 0 {
		t.Fatal("expected http.RequestObject to have fields")
	}
	if _, exists := iface.Fields["path"]; !exists {
		t.Fatal("expected http.RequestObject to have 'path' field")
	}
}

func TestRuntimeTypeCheckWithStdType(t *testing.T) {
	req := NewNative(ObjectValue{
		"path":          NewNative("/test"),
		"url":           NewNative("/test"),
		"method":        NewNative("GET"),
		"body":          NewNative(""),
		"bodyBytes":     NewNative(&BufferValue{}),
		"params":        NewNative(ObjectValue{}),
		"query":         NewNative(ObjectValue{}),
		"headers":       NewNative(ObjectValue{}),
		"form":          NewNative(ObjectValue{}),
		"formAll":       NewNative(ObjectValue{}),
		"files":         NewNative(ObjectValue{}),
		"multipart":     NewNative(false),
		"contentLength": NewInt(0),
		"remoteAddr":    NewNative(""),
	})

	hint := TypeHint{Name: "http.RequestObject"}
	ok, reason := CheckTypeHint(req, hint, map[string]Interface{})
	if !ok {
		t.Fatalf("expected http.RequestObject to accept request: %s", reason)
	}
}

func TestRuntimeTypeCheckRejectsInvalidField(t *testing.T) {
	req := NewNative(ObjectValue{
		"path":   NewNative("/test"),
		"method": NewNative(123), // wrong type
	})

	hint := TypeHint{Name: "http.RequestObject"}
	ok, _ := CheckTypeHint(req, hint, map[string]Interface{})
	if ok {
		t.Fatal("expected http.RequestObject to reject request with wrong field type")
	}
}
