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

func TestCheckTypeHintTypedArray(t *testing.T) {
	strArr := NewNative(ArrayValue{Elements: []TinyValue{NewNative("hello"), NewNative("world")}})
	mixedArr := NewNative(ArrayValue{Elements: []TinyValue{NewNative("hello"), NewInt(123)}})
	nestedNumArr := NewNative(ArrayValue{Elements: []TinyValue{
		NewNative(ArrayValue{Elements: []TinyValue{NewInt(1), NewInt(2)}}),
		NewNative(ArrayValue{Elements: []TinyValue{NewInt(3)}}),
	}})
	nestedMixedArr := NewNative(ArrayValue{Elements: []TinyValue{
		NewNative(ArrayValue{Elements: []TinyValue{NewInt(1), NewNative("oops")}}),
	}})
	emptyArr := NewNative(ArrayValue{Elements: []TinyValue{}})

	// 1. array:string with all strings -> valid
	ok, reason := CheckTypeHint(strArr, stdTypeHint("array:string"), map[string]Interface{})
	if !ok {
		t.Fatalf("expected array:string to accept string array: %s", reason)
	}

	// 2. array:string with string and number -> invalid
	ok, reason = CheckTypeHint(mixedArr, stdTypeHint("array:string"), map[string]Interface{})
	if ok {
		t.Fatal("expected array:string to reject mixed array")
	}

	// 3. array (untyped) with mixed array -> valid
	ok, reason = CheckTypeHint(mixedArr, stdTypeHint("array"), map[string]Interface{})
	if !ok {
		t.Fatalf("expected untyped array to accept mixed array: %s", reason)
	}

	// 4. array:array:number nested array -> valid
	ok, reason = CheckTypeHint(nestedNumArr, stdTypeHint("array:array:number"), map[string]Interface{})
	if !ok {
		t.Fatalf("expected array:array:number to accept nested number array: %s", reason)
	}

	// 5. array:array:number nested mixed array -> invalid
	ok, reason = CheckTypeHint(nestedMixedArr, stdTypeHint("array:array:number"), map[string]Interface{})
	if ok {
		t.Fatal("expected array:array:number to reject nested mixed array")
	}

	// 6. empty array is valid for any typed array
	ok, reason = CheckTypeHint(emptyArr, stdTypeHint("array:string"), map[string]Interface{})
	if !ok {
		t.Fatalf("expected array:string to accept empty array: %s", reason)
	}
}

