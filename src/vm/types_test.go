package vm

import "testing"

func TestCheckTypeHintStdHttpRequestObject(t *testing.T) {
	request := NewNative(ObjectValue{
		"path":    NewNative("/test"),
		"method":  NewNative("GET"),
		"body":    NewNative(""),
		"params":  NewNative(ObjectValue{}),
		"query":   NewNative(ObjectValue{}),
		"headers": NewNative(ObjectValue{}),
	})

	ok, reason := CheckTypeHint(request, stdTypeHint("http.RequestObject"), map[string]Interface{})
	if !ok {
		t.Fatalf("expected http.RequestObject to accept request object: %s", reason)
	}
}

func TestCheckTypeHintStdHttpRequestObjectRejectsMissingField(t *testing.T) {
	request := NewNative(ObjectValue{
		"path":   NewNative("/test"),
		"method": NewNative("GET"),
		"body":   NewNative(""),
		"params": NewNative(ObjectValue{}),
		"query":  NewNative(ObjectValue{}),
	})

	ok, reason := CheckTypeHint(request, stdTypeHint("http.RequestObject"), map[string]Interface{})
	if ok {
		t.Fatal("expected http.RequestObject to reject request object without headers")
	}
	if reason != " (missing field 'headers')" {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
}
