package bytecode

import (
	"bytes"
	"testing"

	json "github.com/goccy/go-json"

	"language.com/src/vm"
)

func TestBytecodeRoundTripPreservesFunctionMetadata(t *testing.T) {
	main := []vm.Instruction{
		{Op: vm.OP_CALL_DIRECT, Value: vm.DirectCallInfo{ID: 99, Name: "answer", ArgCount: 0}, File: `C:\Users\confis\Desktop\project\main.tiny`, Line: 1, Column: 1},
		{Op: vm.OP_HALT},
	}

	functions := map[string]vm.Function{
		"answer": {
			ID:         99,
			Name:       "answer",
			ReturnType: vm.TypeHint{Name: "number"},
			Params: []vm.Param{
				{Name: "fallback", TypeHint: vm.TypeHint{Name: "number"}, HasDefault: true, DefaultValue: vm.NewInt(42)},
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
				{Name: "name", Value: vm.NewNative("Tiny"), TypeHint: vm.TypeHint{Name: "string"}, Constant: true, Private: true},
			},
			Methods:        map[string]string{"label": "User.label"},
			PrivateMethods: map[string]bool{"secret": true},
			Embeds:         []string{"logger"},
		},
	}

	interfaces := map[string]vm.Interface{
		"Test": {
			Name: "Test",
			Fields: map[string]vm.TypeHint{
				"testData": vm.TypeHint{Name: "string"},
			},
		},
	}

	_, loadedFunctions, loadedClasses, loadedInterfaces, _ := LoadBytecodeFromBytes(SaveBytecodeToBytes(main, functions, classes, interfaces, nil, true))

	// if len(loadedMain) != len(main) || loadedMain[0].File != bytecodeSourceLabel || loadedMain[0].Line != 1 {
	// 	t.Fatalf("main instructions did not round trip: %#v", loadedMain)
	// }

	fn := loadedFunctions["answer"]
	if fn.Name != "answer" || fn.ReturnType.Name != "number" || fn.LocalCount != 1 {
		t.Fatalf("function metadata did not round trip: %#v", fn)
	}

	if len(fn.Params) != 1 || !fn.Params[0].HasDefault || fn.Params[0].DefaultValue.AsInt != 42 {
		t.Fatalf("param metadata did not round trip: %#v", fn.Params)
	}

	class := loadedClasses["User"]
	if class.Name != "User" || !class.Fields[0].Constant || !class.Fields[0].Private {
		t.Fatalf("class metadata did not round trip: %#v", class)
	}

	interfaceData := loadedInterfaces["Test"]
	if interfaceData.Name != "Test" || interfaceData.Fields["testData"].Name != "string" {
		t.Fatalf("interface metadata did not round trip: %#v", class)
	}
}

func TestSaveBytecodeToBytesUsesBinaryFormat(t *testing.T) {
	data := SaveBytecodeToBytes([]vm.Instruction{{Op: vm.OP_HALT}}, nil, nil, nil, nil, false)

	if !bytes.HasPrefix(data, bytecodeMagic) {
		t.Fatalf("bytecode missing binary magic header: %q", data[:min(len(data), len(bytecodeMagic))])
	}

	if json.Valid(data) {
		t.Fatal("bytecode should be binary, got valid JSON")
	}
}

func TestSaveBytecodeToBytesHidesSourcePaths(t *testing.T) {
	sourcePath := `C:\Users\confis\Desktop\Programming\Go\compiler\core.tiny`
	data := SaveBytecodeToBytes([]vm.Instruction{
		{Op: vm.OP_HALT, File: sourcePath, Line: 12, Column: 3},
	}, nil, nil, nil, nil, false)

	if bytes.Contains(data, []byte(sourcePath)) {
		t.Fatal("bytecode leaked absolute source path")
	}

	if bytes.Contains(data, []byte("core.tiny")) {
		t.Fatal("bytecode leaked source filename")
	}

	// main, _, _ := LoadBytecodeFromBytes(data)
	// if len(main) != 1 || main[0].File != bytecodeSourceLabel || main[0].Line != 12 || main[0].Column != 3 {
	// 	t.Fatalf("sanitized source location did not round trip: %#v", main)
	// }
}

func TestLoadBytecodeFromBytesSupportsLegacyJSON(t *testing.T) {
	file := BytecodeFile{
		Version:   BytecodeVersion,
		Main:      serializeInstructions([]vm.Instruction{{Op: vm.OP_HALT}}, false),
		Functions: map[string]SerializableFunction{},
		Classes:   map[string]SerializableClass{},
	}

	data, err := json.Marshal(file)
	if err != nil {
		t.Fatalf("marshal legacy bytecode: %v", err)
	}

	main, functions, classes, interfaces, _ := LoadBytecodeFromBytes(data)
	if len(main) != 1 || main[0].Op != vm.OP_HALT {
		t.Fatalf("legacy main did not load: %#v", main)
	}
	if len(functions) != 0 || len(classes) != 0 || len(interfaces) != 0 {
		t.Fatalf("legacy maps did not load: functions=%#v classes=%#v interfaces=%#v", functions, classes, interfaces)
	}
}

func TestEncodeDecodeNamespaceValue(t *testing.T) {
	original := vm.NamespaceValue{
		Name: "Report",
		Members: map[string]vm.Value{
			"status": vm.NewNative(vm.NamespaceMemberRef{GlobalName: "Report.status"}),
			"count":  vm.NewInt(3),
		},
	}

	decoded, ok := DecodeValue(EncodeValue(original)).(vm.NamespaceValue)
	if !ok {
		t.Fatalf("expected NamespaceValue, got %T", decoded)
	}

	if decoded.Name != original.Name || decoded.Members["count"].AsInt != 3 {
		t.Fatalf("namespace did not round trip: %#v", decoded)
	}
}
