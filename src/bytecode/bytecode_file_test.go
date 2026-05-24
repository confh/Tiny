package bytecode

import (
	"testing"

	"language.com/src/vm"
)

func TestBytecodeRoundTripPreservesFunctionMetadata(t *testing.T) {
	main := []vm.Instruction{
		{Op: vm.OP_CALL_DIRECT, Value: vm.DirectCallInfo{ID: 99, Name: "answer", ArgCount: 0}, File: "main.tiny", Line: 1, Column: 1},
		{Op: vm.OP_HALT},
	}

	functions := map[string]vm.Function{
		"answer": {
			ID:         99,
			Name:       "answer",
			ReturnType: vm.TypeHint{Name: "number"},
			Params: []vm.Param{
				{Name: "fallback", TypeHint: vm.TypeHint{Name: "number"}, HasDefault: true, DefaultValue: 42},
			},
			LocalCount: 1,
			Instructions: []vm.Instruction{
				{Op: vm.OP_LOAD_LOCAL, Value: 0},
				{Op: vm.OP_RETURN},
			},
			Captures: []vm.CapturedVar{
				{Name: "outer", OuterSlot: 0, InnerSlot: 1},
			},
		},
	}

	classes := map[string]vm.Class{
		"User": {
			Name: "User",
			Fields: []vm.ClassField{
				{Name: "name", Value: "Tiny", TypeHint: vm.TypeHint{Name: "string"}, Constant: true, Private: true},
			},
			Methods:        map[string]string{"label": "User.label"},
			PrivateMethods: map[string]bool{"secret": true},
			Embeds:         []string{"logger"},
		},
	}

	loadedMain, loadedFunctions, loadedClasses := LoadBytecodeFromBytes(SaveBytecodeToBytes(main, functions, classes))

	if len(loadedMain) != len(main) || loadedMain[0].File != "main.tiny" || loadedMain[0].Line != 1 {
		t.Fatalf("main instructions did not round trip: %#v", loadedMain)
	}

	fn := loadedFunctions["answer"]
	if fn.Name != "answer" || fn.ReturnType.Name != "number" || fn.LocalCount != 1 {
		t.Fatalf("function metadata did not round trip: %#v", fn)
	}

	if len(fn.Params) != 1 || !fn.Params[0].HasDefault || fn.Params[0].DefaultValue != 42 {
		t.Fatalf("param metadata did not round trip: %#v", fn.Params)
	}

	class := loadedClasses["User"]
	if class.Name != "User" || !class.Fields[0].Constant || !class.Fields[0].Private {
		t.Fatalf("class metadata did not round trip: %#v", class)
	}
}

func TestEncodeDecodeNamespaceValue(t *testing.T) {
	original := vm.NamespaceValue{
		Name: "Report",
		Members: map[string]vm.Value{
			"status": vm.NamespaceMemberRef{GlobalName: "Report.status"},
			"count":  3,
		},
	}

	decoded, ok := DecodeValue(EncodeValue(original)).(vm.NamespaceValue)
	if !ok {
		t.Fatalf("expected NamespaceValue, got %T", decoded)
	}

	if decoded.Name != original.Name || decoded.Members["count"] != 3 {
		t.Fatalf("namespace did not round trip: %#v", decoded)
	}
}
