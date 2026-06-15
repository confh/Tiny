package vm

import "testing"

func TestValidateStringTrimAndNonEmpty(t *testing.T) {
	vm := NewVM(VMInfo{JITDisabled: true})
	schema := &NativeValidateType{
		Type:     String,
		Trim:     true,
		NonEmpty: true,
	}

	result := validateValueWithSchema(vm, schema, NewNative("  hello  "))
	if !result.ok {
		t.Fatalf("expected string validation to succeed, got %q", result.err)
	}

	value, ok := result.value.Value.(string)
	if !ok || value != "hello" {
		t.Fatalf("expected trimmed string, got %#v", result.value)
	}
}

func TestValidateObjectStrictAndDefault(t *testing.T) {
	vm := NewVM(VMInfo{JITDisabled: true})
	schema := &NativeValidateType{
		Type:   ObjectSchema,
		Strict: true,
		Fields: []*NativeValidateType{
			{
				Name:       "name",
				Type:       String,
				Required:   true,
				Trim:       true,
				HasDefault: false,
			},
			{
				Name:       "age",
				Type:       Number,
				HasDefault: true,
				Default:    NewInt(18),
			},
		},
	}

	result := validateValueWithSchema(vm, schema, NewNative(ObjectValue{
		"name": NewNative("  Ada "),
	}))
	if !result.ok {
		t.Fatalf("expected object validation to succeed, got %q", result.err)
	}

	obj := result.value.Value.(ObjectValue)
	if got := obj["name"].Value; got != "Ada" {
		t.Fatalf("expected trimmed name, got %#v", got)
	}
	if !obj["age"].IsInt || obj["age"].AsInt != 18 {
		t.Fatalf("expected default age 18, got %#v", obj["age"])
	}

	fail := validateValueWithSchema(vm, schema, NewNative(ObjectValue{
		"name":  NewNative("Ada"),
		"extra": NewInt(1),
	}))
	if fail.ok {
		t.Fatal("expected strict object schema to reject extra field")
	}
}

func TestValidateUnionAndArrayItems(t *testing.T) {
	vm := NewVM(VMInfo{JITDisabled: true})
	union := &NativeValidateType{
		Type: UnionSchema,
		UnionSchemas: []*NativeValidateType{
			{Type: String},
			{Type: Number, Positive: true},
		},
	}

	if result := validateValueWithSchema(vm, union, NewNative("ok")); !result.ok {
		t.Fatalf("expected union to accept string, got %q", result.err)
	}
	if result := validateValueWithSchema(vm, union, NewInt(3)); !result.ok {
		t.Fatalf("expected union to accept positive number, got %q", result.err)
	}
	if result := validateValueWithSchema(vm, union, NewNative(false)); result.ok {
		t.Fatal("expected union to reject bool")
	}

	arraySchema := &NativeValidateType{
		Type:       ArraySchema,
		ItemSchema: &NativeValidateType{Type: Number, IntOnly: true},
	}
	result := validateValueWithSchema(vm, arraySchema, NewNative(&ArrayValue{
		Elements: []TinyValue{NewInt(1), NewInt(2)},
	}))
	if !result.ok {
		t.Fatalf("expected array items schema to succeed, got %q", result.err)
	}
}

func TestSchemaSafeParseResultShape(t *testing.T) {
	vm := NewVM(VMInfo{JITDisabled: true})
	schema := &NativeValidateType{Type: Number, Positive: true}

	okResult := safeParseResult(validateValueWithSchema(vm, schema, NewInt(5)))
	okObj := okResult.Value.(ObjectValue)
	if success, _ := okObj["success"].Value.(bool); !success {
		t.Fatalf("expected safeParse success true, got %#v", okObj["success"])
	}

	failResult := safeParseResult(validateValueWithSchema(vm, schema, NewInt(-1)))
	failObj := failResult.Value.(ObjectValue)
	if success, _ := failObj["success"].Value.(bool); success {
		t.Fatalf("expected safeParse success false, got %#v", failObj["success"])
	}
	if _, ok := failObj["error"].Value.(ErrorValue); !ok {
		t.Fatalf("expected safeParse error object, got %#v", failObj["error"])
	}
}
