package vm

import (
	"strings"
	"testing"
)

func TestCheckTypeHintStdHttpRequestObject(t *testing.T) {
	request := NewNative(ObjectValue{
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
	if !strings.HasPrefix(reason, " (missing field '") {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
}

func TestCheckTypeHintStdAliasInterface(t *testing.T) {
	bounds := NewNative(ObjectValue{
		"x":      NewInt(1),
		"y":      NewInt(2),
		"width":  NewInt(300),
		"height": NewInt(200),
	})
	globals := []TinyValue{NewNative(&StandardModuleValue{Name: "tray"})}
	globalNames := map[string]int{"t": 0}

	ok, reason := CheckTypeHintWithGlobals(bounds, stdTypeHint("t.Bounds"), map[string]Interface{}, globals, globalNames)
	if !ok {
		t.Fatalf("expected t.Bounds to resolve through tray std alias: %s", reason)
	}

	empty := NewNative(ObjectValue{})
	ok, reason = CheckTypeHintWithGlobals(empty, stdTypeHint("t.Bounds"), map[string]Interface{}, globals, globalNames)
	if ok {
		t.Fatal("expected t.Bounds to reject missing fields")
	}
	if !strings.HasPrefix(reason, " (missing field '") {
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

func TestCheckTypeHintNamespacedInterface(t *testing.T) {
	interfaces := map[string]Interface{
		"Client.Results.ConnectResult": {
			Name: "Client.Results.ConnectResult",
			Fields: map[string]TypeHint{
				"client": stdTypeHint("number"),
			},
		},
	}

	val := NewNative(ObjectValue{
		"client": NewInt(42),
	})

	// 1. Matches using prefix path suffix: Results.ConnectResult
	ok, reason := CheckTypeHint(val, stdTypeHint("Results.ConnectResult"), interfaces)
	if !ok {
		t.Fatalf("expected Results.ConnectResult to resolve and match Client.Results.ConnectResult: %s", reason)
	}

	// 2. Matches using shortName: ConnectResult
	ok, reason = CheckTypeHint(val, stdTypeHint("ConnectResult"), interfaces)
	if !ok {
		t.Fatalf("expected ConnectResult to resolve and match Client.Results.ConnectResult: %s", reason)
	}

	// 3. Fails when key matches but fields mismatch
	badVal := NewNative(ObjectValue{
		"client": NewNative("not-a-number"),
	})
	ok, reason = CheckTypeHint(badVal, stdTypeHint("Results.ConnectResult"), interfaces)
	if ok {
		t.Fatal("expected Results.ConnectResult to reject value due to field type mismatch")
	}
}

func TestRuntimeVMTypeHintAcceptsNonIsolatedClassAlias(t *testing.T) {
	value := NewNative(&InstanceValue{ClassName: "Discord.Client"})
	hint := stdTypeHint("Gateway.Client")

	vm := NewVM(VMInfo{
		Classes: map[string]Class{
			"Gateway.Client": {
				Name: "Gateway.Client",
				Fields: []ClassField{
					{Name: "latency"},
				},
			},
		},
	})
	value.Value.(*InstanceValue).Fields = ObjectValue{
		"latency": NewInt(42),
	}

	ok, reason := vm.checkTypeHint(value, hint)
	if !ok {
		t.Fatalf("expected non-isolated VM to accept class alias: %s", reason)
	}

	isolated := NewVM(VMInfo{
		Isolated: true,
		Classes: map[string]Class{
			"Gateway.Client": {Name: "Gateway.Client"},
		},
	})

	ok, _ = isolated.checkTypeHint(value, hint)
	if ok {
		t.Fatal("expected isolated VM to reject class alias")
	}

	differentShape := NewVM(VMInfo{
		Classes: map[string]Class{
			"Gateway.Client": {
				Name: "Gateway.Client",
				Fields: []ClassField{
					{Name: "token"},
				},
			},
		},
	})

	ok, _ = differentShape.checkTypeHint(value, hint)
	if ok {
		t.Fatal("expected non-isolated VM to reject unrelated class with different shape")
	}

	unresolvedAliasVM := NewVM(VMInfo{})
	ok, reason = unresolvedAliasVM.checkTypeHint(NewNative(&InstanceValue{ClassName: "Discord.CommandsModule.CommandBuilder"}), stdTypeHint("Discord.CommandBuilder"))
	if !ok {
		t.Fatalf("expected unresolved namespace class alias with matching basename to be accepted: %s", reason)
	}
}

func TestCheckTypeHintInterfaceExtends(t *testing.T) {
	interfaces := map[string]Interface{
		"Base": {
			Name: "Base",
			Fields: map[string]TypeHint{
				"id": stdTypeHint("number"),
			},
		},
		"User": {
			Name:    "User",
			Extends: []string{"Base"},
			Fields: map[string]TypeHint{
				"name": stdTypeHint("string"),
			},
		},
	}

	okValue := NewNative(ObjectValue{
		"id":   NewInt(1),
		"name": NewNative("Ada"),
	})
	ok, reason := CheckTypeHint(okValue, stdTypeHint("User"), interfaces)
	if !ok {
		t.Fatalf("expected User to accept inherited and own fields: %s", reason)
	}

	missingBase := NewNative(ObjectValue{
		"name": NewNative("Ada"),
	})
	ok, reason = CheckTypeHint(missingBase, stdTypeHint("User"), interfaces)
	if ok {
		t.Fatal("expected User to reject missing inherited field")
	}
	if reason != " (missing field 'id')" {
		t.Fatalf("unexpected rejection reason: %q", reason)
	}
}

func TestCheckTypeHintStructuralUnionField(t *testing.T) {
	value := NewNative(ObjectValue{"name": NewNative("Ada")})
	hint := TypeHintFromString("{name: string | null}")
	ok, reason := CheckTypeHint(value, hint, map[string]Interface{})
	if !ok {
		t.Fatalf("expected structural union field to accept string: %s", reason)
	}

	bad := NewNative(ObjectValue{"name": NewInt(42)})
	ok, _ = CheckTypeHint(bad, hint, map[string]Interface{})
	if ok {
		t.Fatal("expected structural union field to reject number")
	}
}

func TestCheckTypeHintUsesFieldsRepresentation(t *testing.T) {
	value := NewNative(ObjectValue{"id": NewInt(1)})
	hint := TypeHint{Fields: map[string]TypeHint{"id": stdTypeHint("number")}}
	ok, reason := CheckTypeHint(value, hint, map[string]Interface{})
	if !ok {
		t.Fatalf("expected TypeHint.Fields structural hint to be checked: %s", reason)
	}
}
