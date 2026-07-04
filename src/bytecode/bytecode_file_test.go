package bytecode

import (
	"bytes"
	"encoding/json"
	"testing"

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
				"testData": {Name: "string"},
			},
		},
	}

	_, loadedFunctions, loadedClasses, loadedInterfaces, _ := LoadBytecodeFromBytes(SaveBytecodeToBytes(main, functions, classes, interfaces, nil, true, false))

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
	data := SaveBytecodeToBytes([]vm.Instruction{{Op: vm.OP_HALT}}, nil, nil, nil, nil, false, false)

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
	}, nil, nil, nil, nil, false, false)

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
		Members: map[string]vm.TinyValue{
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

func TestBytecodeObfuscation(t *testing.T) {
	main := []vm.Instruction{
		{Op: vm.OP_CALL_DIRECT, Value: vm.DirectCallInfo{ID: 1, Name: "myFunction", ArgCount: 1}},
		{Op: vm.OP_CONST, Value: vm.FunctionValue{ID: 1, Name: "myFunction"}},
		{Op: vm.OP_CLOSURE, Value: vm.ClosureInfo{Name: "myFunction"}},
		{Op: vm.OP_CALL_DIRECT_SUB_CONST, Value: vm.CallDirectSubConstInfo{Slot: 0, SubValue: 1, FnID: 1, FnName: "myFunction", ArgCount: 0}},
		{Op: vm.OP_CONST, Value: vm.Class{Name: "Gateway.BuilderModule.Builder"}},
		{Op: vm.OP_STORE_GLOBAL, Value: vm.VariableInfo{Name: "Gateway.BuilderModule.Builder"}},
		{Op: vm.OP_CONST, Value: vm.NamespaceValue{Name: "Testing", Members: map[string]vm.TinyValue{
			"ass":     vm.NewNative(vm.NamespaceMemberRef{GlobalName: "myGlobal"}),
			"MyClass": vm.NewNative(vm.Class{Name: "MyClass"}),
		}}},
		{Op: vm.OP_CONST, Value: vm.NamespaceValue{Name: "Gateway", Members: map[string]vm.TinyValue{
			"Builder": vm.NewNative(vm.NamespaceMemberRef{GlobalName: "Gateway.BuilderModule.Builder"}),
		}}},
		{Op: vm.OP_STORE_GLOBAL, Value: vm.VariableInfo{Name: "data", TypeHint: vm.TypeHint{Name: "Gateway.Builder", Types: []string{"Gateway.Builder"}}}},
		{Op: vm.OP_HALT},
	}

	functions := map[string]vm.Function{
		"myFunction": {
			ID:   1,
			Name: "myFunction",
			Params: []vm.Param{
				{Name: "config", TypeHint: vm.TypeHint{Name: "MyInterface", Types: []string{"MyInterface"}}},
				{Name: "namespaced", TypeHint: vm.TypeHint{Name: "NamespacedInterface", Types: []string{"NamespacedInterface"}}},
			},
			ReturnType: vm.TypeHint{Name: "MyClass", Types: []string{"MyClass"}},
			Instructions: []vm.Instruction{
				{Op: vm.OP_LOAD_GLOBAL, Value: vm.VariableInfo{Name: "myGlobal"}},
				{Op: vm.OP_RETURN},
			},
		},
	}

	classes := map[string]vm.Class{
		"MyClass": {
			Name:       "MyClass",
			Implements: []string{"MyInterface"},
			Fields: []vm.ClassField{
				{Name: "config", TypeHint: vm.TypeHint{Name: "MyInterface", Types: []string{"MyInterface"}}},
			},
			Methods: map[string]string{
				"init": "myFunction",
			},
		},
		"Gateway.BuilderModule.Builder": {
			Name:    "Gateway.BuilderModule.Builder",
			Fields:  []vm.ClassField{{Name: "name"}},
			Methods: map[string]string{},
		},
	}

	interfaces := map[string]vm.Interface{
		"MyInterface": {
			Name: "MyInterface",
			Fields: map[string]vm.TypeHint{
				"child": {Name: "MyClass", Types: []string{"MyClass"}},
			},
		},
		"Namespace.NamespacedInterface": {
			Name:   "Namespace.NamespacedInterface",
			Fields: map[string]vm.TypeHint{},
		},
	}

	globalIndex := map[string]int{
		"myGlobal":                      0,
		"Gateway":                       1,
		"Gateway.BuilderModule.Builder": 2,
	}

	// Compile with obfuscation enabled
	data := SaveBytecodeToBytes(main, functions, classes, interfaces, globalIndex, false, true)

	// Load it back
	loadedMain, loadedFunctions, loadedClasses, loadedInterfaces, loadedGlobals := LoadBytecodeFromBytes(data)

	// Check main instruction call to myFunction got obfuscated
	if len(loadedMain) < 1 || loadedMain[0].Op != vm.OP_CALL_DIRECT {
		t.Fatalf("unexpected main: %#v", loadedMain)
	}
	directCall := loadedMain[0].Value.(vm.DirectCallInfo)
	if directCall.Name == "myFunction" {
		t.Fatal("direct call name was not obfuscated")
	}

	// Check function name was obfuscated
	var foundObfuscatedFunc bool
	for name, fn := range loadedFunctions {
		if name == "myFunction" || fn.Name == "myFunction" {
			t.Fatal("function name was not obfuscated in maps")
		}
		if name == directCall.Name && fn.Name == directCall.Name {
			foundObfuscatedFunc = true
			if fn.Params[0].TypeHint.Name == "MyInterface" {
				t.Fatal("function param type hint was not obfuscated")
			}
			if _, exists := loadedInterfaces[fn.Params[0].TypeHint.Name]; !exists {
				t.Fatalf("function param type hint points at missing interface %q", fn.Params[0].TypeHint.Name)
			}
			if fn.Params[1].TypeHint.Name == "NamespacedInterface" {
				t.Fatal("short namespaced function param type hint was not obfuscated")
			}
			if _, exists := loadedInterfaces[fn.Params[1].TypeHint.Name]; !exists {
				t.Fatalf("short namespaced function param type hint points at missing interface %q", fn.Params[1].TypeHint.Name)
			}
			if fn.ReturnType.Name == "MyClass" {
				t.Fatal("function return type hint was not obfuscated")
			}
			if _, exists := loadedClasses[fn.ReturnType.Name]; !exists {
				t.Fatalf("function return type hint points at missing class %q", fn.ReturnType.Name)
			}

			// Verify instruction inside function referencing myGlobal got renamed
			if len(fn.Instructions) < 1 || fn.Instructions[0].Op != vm.OP_LOAD_GLOBAL {
				t.Fatalf("unexpected function instructions: %#v", fn.Instructions)
			}
			varInfo := fn.Instructions[0].Value.(vm.VariableInfo)
			if varInfo.Name == "myGlobal" {
				t.Fatal("global variable name inside function was not obfuscated")
			}
		}
	}
	if !foundObfuscatedFunc {
		t.Fatal("obfuscated function not found in loaded functions")
	}

	callbackValue, ok := loadedMain[1].Value.(vm.FunctionValue)
	if !ok {
		t.Fatalf("expected function value constant, got %T", loadedMain[1].Value)
	}
	if callbackValue.Name == "myFunction" {
		t.Fatal("function value constant name was not obfuscated")
	}
	if _, exists := loadedFunctions[callbackValue.Name]; !exists {
		t.Fatalf("function value points at missing function %q; functions=%v", callbackValue.Name, loadedFunctions)
	}

	closureValue, ok := loadedMain[2].Value.(vm.ClosureInfo)
	if !ok {
		t.Fatalf("expected closure value, got %T", loadedMain[2].Value)
	}
	if closureValue.Name == "myFunction" {
		t.Fatal("closure function name was not obfuscated")
	}
	if _, exists := loadedFunctions[closureValue.Name]; !exists {
		t.Fatalf("closure points at missing function %q; functions=%v", closureValue.Name, loadedFunctions)
	}

	callSubConst, ok := loadedMain[3].Value.(vm.CallDirectSubConstInfo)
	if !ok {
		t.Fatalf("expected CallDirectSubConstInfo, got %T", loadedMain[3].Value)
	}
	if callSubConst.FnName == "myFunction" {
		t.Fatal("callDirectSubConst FnName was not obfuscated")
	}

	namespaceVal, ok := loadedMain[6].Value.(vm.NamespaceValue)
	if !ok {
		t.Fatalf("expected NamespaceValue, got %T", loadedMain[4].Value)
	}
	memberTinyVal := namespaceVal.Members["ass"]
	memberRef, ok := memberTinyVal.Value.(vm.NamespaceMemberRef)
	if !ok {
		t.Fatalf("expected NamespaceMemberRef, got %T", memberTinyVal.Value)
	}
	if memberRef.GlobalName == "myGlobal" {
		t.Fatal("namespace member ref globalName was not obfuscated")
	}
	classValue, ok := namespaceVal.Members["MyClass"].Value.(vm.Class)
	if !ok {
		t.Fatalf("expected namespace class value, got %T", namespaceVal.Members["MyClass"].Value)
	}
	if classValue.Name == "MyClass" {
		t.Fatal("namespace class value name was not obfuscated")
	}
	if _, exists := loadedClasses[classValue.Name]; !exists {
		t.Fatalf("namespace class value points at missing class %q; classes=%v", classValue.Name, loadedClasses)
	}

	aliasVarInfo, ok := loadedMain[8].Value.(vm.VariableInfo)
	if !ok {
		t.Fatalf("expected alias variable info, got %T", loadedMain[6].Value)
	}
	if aliasVarInfo.TypeHint.Name == "Gateway.Builder" {
		t.Fatal("namespace alias type hint was not obfuscated")
	}
	if _, exists := loadedClasses[aliasVarInfo.TypeHint.Name]; !exists {
		t.Fatalf("namespace alias type hint points at missing class %q", aliasVarInfo.TypeHint.Name)
	}

	// Check class name was obfuscated
	for name, cls := range loadedClasses {
		if name == "MyClass" || cls.Name == "MyClass" {
			t.Fatal("class name was not obfuscated")
		}
		if cls.Methods["init"] == "myFunction" {
			t.Fatal("class method function name was not obfuscated")
		}
		if initFn := cls.Methods["init"]; initFn != "" {
			if _, exists := loadedFunctions[initFn]; !exists {
				t.Fatalf("class method points at missing function %q; functions=%v", initFn, loadedFunctions)
			}
		}
		if len(cls.Implements) > 0 {
			if cls.Implements[0] == "MyInterface" {
				t.Fatal("class implements type was not obfuscated")
			}
			if _, exists := loadedInterfaces[cls.Implements[0]]; !exists {
				t.Fatalf("class implements missing interface %q", cls.Implements[0])
			}
		}
		if len(cls.Fields) > 0 && cls.Fields[0].TypeHint.Name == "MyInterface" {
			t.Fatal("class field type hint was not obfuscated")
		}
	}

	// Check interface name was obfuscated
	for name, inter := range loadedInterfaces {
		if name == "MyInterface" || inter.Name == "MyInterface" {
			t.Fatal("interface name was not obfuscated")
		}
		if childHint, hasChild := inter.Fields["child"]; hasChild {
			if childHint.Name == "MyClass" {
				t.Fatal("interface field type hint was not obfuscated")
			}
			if _, exists := loadedClasses[childHint.Name]; !exists {
				t.Fatalf("interface field type hint points at missing class %q", childHint.Name)
			}
		}
	}

	// Check global index map was obfuscated
	for name := range loadedGlobals {
		if name == "myGlobal" {
			t.Fatal("global variable name was not obfuscated in global index map")
		}
	}
}
