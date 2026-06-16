package vm

import (
	"net/mail"
	neturl "net/url"
	"regexp"
	"strings"

	. "language.com/src/tinyerrors"
)

var schemaNativeMetadata = NativeTypeInfo{
	Name: "schema",
}

var schemaTypeMethods map[string]NativeModuleFunc[*NativeValidateType]

func init() {
	schemaTypeMethods = map[string]NativeModuleFunc[*NativeValidateType]{
		"required":  schemaTypeRequired,
		"optional":  schemaTypeOptional,
		"nullable":  schemaTypeNullable,
		"default":   schemaTypeDefault,
		"parse":     schemaTypeParse,
		"safeParse": schemaTypeSafeParse,
		"check":     schemaTypeCheck,
		"message":   schemaTypeMessage,
		"min":       schemaTypeMin,
		"max":       schemaTypeMax,
		"length":    schemaTypeLength,
		"nonempty":  schemaTypeNonempty,
		"email":     schemaTypeEmail,
		"url":       schemaTypeURL,
		"regex":     schemaTypeRegex,
		"trim":      schemaTypeTrim,
		"int":       schemaTypeInt,
		"positive":  schemaTypePositive,
		"items":     schemaTypeItems,
		"shape":     schemaTypeShape,
		"strict":    schemaTypeStrict,
		"partial":   schemaTypePartial,
		"refine":    schemaTypeRefine,
		"transform": schemaTypeTransform,
	}
}

type validateResult struct {
	value TinyValue
	ok    bool
	err   string
}

func newSchemaValue(schema *NativeValidateType) TinyValue {
	return NewNative(&NativeValidateTop{Schema: schema})
}

func unwrapSchemaValue(value TinyValue) (*NativeValidateType, bool) {
	switch schema := value.Value.(type) {
	case *NativeValidateType:
		return schema, true
	case *NativeValidateTop:
		if schema == nil || schema.Schema == nil {
			return nil, false
		}
		return schema.Schema, true
	default:
		return nil, false
	}
}

func cloneValidateType(schema *NativeValidateType) *NativeValidateType {
	if schema == nil {
		return nil
	}

	cloned := *schema

	if schema.ItemSchema != nil {
		cloned.ItemSchema = cloneValidateType(schema.ItemSchema)
	}
	if len(schema.Fields) > 0 {
		cloned.Fields = make([]*NativeValidateType, len(schema.Fields))
		for i, field := range schema.Fields {
			cloned.Fields[i] = cloneValidateType(field)
		}
	}
	if len(schema.UnionSchemas) > 0 {
		cloned.UnionSchemas = make([]*NativeValidateType, len(schema.UnionSchemas))
		for i, item := range schema.UnionSchemas {
			cloned.UnionSchemas[i] = cloneValidateType(item)
		}
	}
	if len(schema.EnumValues) > 0 {
		cloned.EnumValues = make([]TinyValue, len(schema.EnumValues))
		copy(cloned.EnumValues, schema.EnumValues)
	}

	return &cloned
}

func schemaFieldsFromObject(vm *VM, fnName string, obj ObjectValue) []*NativeValidateType {
	schemas := make([]*NativeValidateType, 0, len(obj))

	for key, v := range obj {
		keyStr, ok := key.(string)
		if !ok {
			vm.runtimeError(ErrorRuntime, "%s requires object keys to be strings", fnName)
			return nil
		}

		validateType := cloneValidateType(expectSchemaValue(vm, fnName, v))
		validateType.Name = keyStr
		schemas = append(schemas, validateType)
	}

	return schemas
}

func expectSchemaArg(vm *VM, fnName string, args []TinyValue, index int) *NativeValidateType {
	if index < 0 || index >= len(args) {
		vm.runtimeError(ErrorRuntime, "%s missing argument %d", fnName, index+1)
	}
	return expectSchemaValue(vm, fnName, args[index])
}

func expectSchemaValue(vm *VM, fnName string, value TinyValue) *NativeValidateType {
	schema, ok := unwrapSchemaValue(value)
	if !ok {
		vm.runtimeError(ErrorType, "%s expected schema, got %s", fnName, TypeName(value))
	}
	return schema
}

func enumValuesFromArgs(vm *VM, fnName string, args []TinyValue) []TinyValue {
	if len(args) == 1 {
		if arr, ok := vm.valueAsArrayForRead(args[0]); ok {
			values := make([]TinyValue, len(arr.Elements))
			copy(values, arr.Elements)
			return values
		}
	}
	if len(args) == 0 {
		vm.runtimeError(ErrorRuntime, "%s expects at least 1 argument", fnName)
	}
	values := make([]TinyValue, len(args))
	copy(values, args)
	return values
}

func unionSchemasFromArgs(vm *VM, fnName string, args []TinyValue) []*NativeValidateType {
	if len(args) == 1 {
		if arr, ok := vm.valueAsArrayForRead(args[0]); ok {
			schemas := make([]*NativeValidateType, 0, len(arr.Elements))
			for _, item := range arr.Elements {
				schemas = append(schemas, cloneValidateType(expectSchemaValue(vm, fnName, item)))
			}
			return schemas
		}
	}
	if len(args) == 0 {
		vm.runtimeError(ErrorRuntime, "%s expects at least 1 argument", fnName)
	}
	schemas := make([]*NativeValidateType, 0, len(args))
	for _, arg := range args {
		schemas = append(schemas, cloneValidateType(expectSchemaValue(vm, fnName, arg)))
	}
	return schemas
}

func schemaErrorMessage(schema *NativeValidateType, fallback string) string {
	if schema != nil && schema.Message != "" {
		return schema.Message
	}
	if schema != nil && schema.Name != "" {
		return schema.Name + ": " + fallback
	}
	return fallback
}

func schemaSuccess(value TinyValue) validateResult {
	return validateResult{value: value, ok: true}
}

func schemaFailure(schema *NativeValidateType, fallback string) validateResult {
	return validateResult{ok: false, err: schemaErrorMessage(schema, fallback)}
}

func isTinyIntLikeNumber(value TinyValue) bool {
	if value.IsInt {
		return true
	}
	switch v := value.Value.(type) {
	case float64:
		return v == float64(int(v))
	default:
		return false
	}
}

func numberValue(vm *VM, value TinyValue) (float64, bool) {
	if value.IsInt {
		return float64(value.AsInt), true
	}
	switch v := value.Value.(type) {
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func schemaValueForWebSource(vm *VM, schema *NativeValidateType, input TinyValue) (TinyValue, validateResult, bool) {
	if schema == nil || schema.WebSource == "" {
		return input, validateResult{}, true
	}

	if obj, ok := vm.valueAsObjectForRead(input); ok {
		if prop, exists := obj[schema.WebSource]; exists {
			return prop, validateResult{}, true
		}

		// validate.body(schema) accepts either a full request object ({ body, query, params })
		// or the raw body value directly. This keeps body schemas usable in tests and in
		// code paths that already extracted req.body.
		if schema.WebSource == "body" {
			return input, validateResult{}, true
		}

		return NewNull(), schemaFailure(schema, "missing "+schema.WebSource), false
	}

	if schema.WebSource == "body" {
		return input, validateResult{}, true
	}

	return NewNull(), schemaFailure(schema, "expected object for "+schema.WebSource+" validation, got "+TypeName(input)), false
}

func validateValueWithSchema(vm *VM, schema *NativeValidateType, input TinyValue) validateResult {
	if schema == nil {
		return schemaSuccess(input)
	}

	value, webFailure, ok := schemaValueForWebSource(vm, schema, input)
	if !ok {
		return webFailure
	}

	if isNullish(value) {
		if schema.HasDefault {
			value = cloneValue(schema.Default)
		} else if schema.Nullable {
			return schemaSuccess(value)
		} else if !schema.Required {
			return schemaSuccess(value)
		} else {
			return schemaFailure(schema, "value is required")
		}
	}

	var result validateResult
	switch schema.Type {
	case String:
		result = validateStringSchema(vm, schema, value)
	case Number:
		result = validateNumberSchema(vm, schema, value)
	case Bool:
		result = validateBoolSchema(schema, value)
	case ArraySchema:
		result = validateArraySchema(vm, schema, value)
	case ObjectSchema:
		result = validateObjectSchema(vm, schema, value)
	case EnumSchema:
		result = validateEnumSchema(schema, value)
	case UnionSchema:
		result = validateUnionSchema(vm, schema, value)
	case AnySchema:
		result = schemaSuccess(value)
	default:
		result = schemaFailure(schema, "unsupported schema type")
	}

	if !result.ok {
		return result
	}

	if schema.RefineFn != nil {
		refined := vm.callFunctionValue(*schema.RefineFn, []TinyValue{result.value})
		if !isTruthy(refined) {
			return schemaFailure(schema, "custom validation failed")
		}
	}

	if schema.TransformFn != nil {
		result.value = vm.callFunctionValue(*schema.TransformFn, []TinyValue{result.value})
	}

	return result
}

func validateStringSchema(vm *VM, schema *NativeValidateType, value TinyValue) validateResult {
	str, ok := value.Value.(string)
	if !ok {
		return schemaFailure(schema, "expected string, got "+TypeName(value))
	}
	if schema.Trim {
		str = strings.TrimSpace(str)
	}
	length := len(str)
	if schema.ExactLen != nil && length != *schema.ExactLen {
		return schemaFailure(schema, "expected string length "+intToString(*schema.ExactLen))
	}
	if schema.MinLen != nil && length < *schema.MinLen {
		return schemaFailure(schema, "expected string length at least "+intToString(*schema.MinLen))
	}
	if schema.MaxLen != nil && length > *schema.MaxLen {
		return schemaFailure(schema, "expected string length at most "+intToString(*schema.MaxLen))
	}
	if schema.NonEmpty && length == 0 {
		return schemaFailure(schema, "expected non-empty string")
	}
	if schema.Email {
		addr, err := mail.ParseAddress(str)
		if err != nil || addr.Address != str {
			return schemaFailure(schema, "expected valid email")
		}
	}
	if schema.Url {
		parsed, err := neturl.Parse(str)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return schemaFailure(schema, "expected valid url")
		}
	}
	if schema.Regex != "" {
		re, err := regexp.Compile(schema.Regex)
		if err != nil {
			vm.runtimeError(ErrorRuntime, "invalid regex pattern %q: %v", schema.Regex, err)
		}
		if !re.MatchString(str) {
			return schemaFailure(schema, "expected string to match regex")
		}
	}
	return schemaSuccess(NewNative(str))
}

func validateNumberSchema(vm *VM, schema *NativeValidateType, value TinyValue) validateResult {
	num, ok := numberValue(vm, value)
	if !ok {
		return schemaFailure(schema, "expected number, got "+TypeName(value))
	}
	if schema.IntOnly && !isTinyIntLikeNumber(value) {
		return schemaFailure(schema, "expected integer")
	}
	if schema.Positive && num <= 0 {
		return schemaFailure(schema, "expected positive number")
	}
	if schema.MinNum != nil && num < *schema.MinNum {
		return schemaFailure(schema, "expected number >= "+FloatToString(*schema.MinNum))
	}
	if schema.MaxNum != nil && num > *schema.MaxNum {
		return schemaFailure(schema, "expected number <= "+FloatToString(*schema.MaxNum))
	}
	if value.IsInt || (schema.IntOnly && num == float64(int(num))) {
		return schemaSuccess(NewInt(int(num)))
	}
	return schemaSuccess(NewNative(num))
}

func validateBoolSchema(schema *NativeValidateType, value TinyValue) validateResult {
	if _, ok := value.Value.(bool); !ok {
		return schemaFailure(schema, "expected bool, got "+TypeName(value))
	}
	return schemaSuccess(value)
}

func validateArraySchema(vm *VM, schema *NativeValidateType, value TinyValue) validateResult {
	arr, ok := vm.valueAsArrayForRead(value)
	if !ok {
		return schemaFailure(schema, "expected array, got "+TypeName(value))
	}
	length := len(arr.Elements)
	if schema.ExactLen != nil && length != *schema.ExactLen {
		return schemaFailure(schema, "expected array length "+intToString(*schema.ExactLen))
	}
	if schema.MinLen != nil && length < *schema.MinLen {
		return schemaFailure(schema, "expected array length at least "+intToString(*schema.MinLen))
	}
	if schema.MaxLen != nil && length > *schema.MaxLen {
		return schemaFailure(schema, "expected array length at most "+intToString(*schema.MaxLen))
	}
	if schema.NonEmpty && length == 0 {
		return schemaFailure(schema, "expected non-empty array")
	}
	if schema.ItemSchema == nil {
		return schemaSuccess(value)
	}

	out := &ArrayValue{Elements: make([]TinyValue, length)}
	for i, item := range arr.Elements {
		validated := validateValueWithSchema(vm, schema.ItemSchema, item)
		if !validated.ok {
			return schemaFailure(schema, "item "+intToString(i)+": "+validated.err)
		}
		out.Elements[i] = validated.value
	}

	return schemaSuccess(NewNative(out))
}

func validateObjectSchema(vm *VM, schema *NativeValidateType, value TinyValue) validateResult {
	obj, ok := vm.valueAsObjectForRead(value)
	if !ok {
		return schemaFailure(schema, "expected object, got "+TypeName(value))
	}

	out := ObjectValue{}
	for key, val := range obj {
		out[key] = val
	}

	fieldsByName := map[string]*NativeValidateType{}
	for _, field := range schema.Fields {
		fieldsByName[field.Name] = field
	}

	for _, field := range schema.Fields {
		fieldValue, exists := obj[field.Name]
		if !exists {
			fieldValue = NewNull()
		}
		if !exists && field.HasDefault {
			out[field.Name] = cloneValue(field.Default)
			continue
		}
		if !exists && !field.Required {
			continue
		}

		validated := validateValueWithSchema(vm, field, fieldValue)
		if !validated.ok {
			return schemaFailure(schema, validated.err)
		}
		out[field.Name] = validated.value
	}

	if schema.Strict {
		for key := range obj {
			keyStr, ok := key.(string)
			if !ok {
				continue
			}
			if _, exists := fieldsByName[keyStr]; !exists {
				return schemaFailure(schema, "unexpected field "+keyStr)
			}
		}
	}

	return schemaSuccess(NewNative(out))
}

func validateEnumSchema(schema *NativeValidateType, value TinyValue) validateResult {
	for _, enumValue := range schema.EnumValues {
		if valuesEqual(enumValue, value) {
			return schemaSuccess(value)
		}
	}
	return schemaFailure(schema, "expected one of enum values")
}

func validateUnionSchema(vm *VM, schema *NativeValidateType, value TinyValue) validateResult {
	for _, unionSchema := range schema.UnionSchemas {
		result := validateValueWithSchema(vm, unionSchema, value)
		if result.ok {
			return result
		}
	}
	return schemaFailure(schema, "no union variant matched")
}

func safeParseResult(result validateResult) TinyValue {
	out := ObjectValue{
		"success": NewNative(result.ok),
	}
	if result.ok {
		out["data"] = result.value
		out["error"] = NewNull()
	} else {
		out["data"] = NewNull()
		out["error"] = NewNative(ErrorValue{
			Kind:    "ValidationError",
			Message: result.err,
		})
	}
	return NewNative(out)
}

func (vm *VM) callNativeSchemaTypeMethod(schemaType *NativeValidateType, method string, args []TinyValue) {
	fn, ok := schemaTypeMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown schema method: %s", method)
		return
	}
	fn(vm, schemaType, args)
}

func (vm *VM) callNativeSchemaTopMethod(schemaTop *NativeValidateTop, method string, args []TinyValue) {
	if schemaTop == nil || schemaTop.Schema == nil {
		vm.runtimeError(ErrorRuntime, "invalid schema")
		return
	}
	vm.callNativeSchemaTypeMethod(schemaTop.Schema, method, args)
}

func schemaTypeRequired(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.required", args)
	schemaType.Required = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeOptional(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.optional", args)
	schemaType.Required = false
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeNullable(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.nullable", args)
	schemaType.Nullable = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeDefault(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.default", args, 1)
	schemaType.HasDefault = true
	schemaType.Default = args[0]
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeParse(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.parse", args, 1)
	result := validateValueWithSchema(vm, schemaType, args[0])
	if !result.ok {
		vm.runtimeError(ErrorRuntime, "%s", result.err)
		return
	}
	vm.push(result.value)
}

func schemaTypeSafeParse(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.safeParse", args, 1)
	vm.push(safeParseResult(validateValueWithSchema(vm, schemaType, args[0])))
}

func schemaTypeCheck(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.check", args, 1)
	vm.push(NewNative(validateValueWithSchema(vm, schemaType, args[0]).ok))
}

func schemaTypeMessage(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.message", args, 1)
	schemaType.Message = argString(vm, "schema.message", args, 0)
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeMin(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.min", args, 1)
	switch schemaType.Type {
	case String, ArraySchema:
		value := argInt(vm, "schema.min", args, 0)
		schemaType.MinLen = &value
	case Number:
		value := asFloat(args[0], vm)
		schemaType.MinNum = &value
	default:
		vm.runtimeError(ErrorRuntime, "schema.min is not supported for this schema")
		return
	}
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeMax(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.max", args, 1)
	switch schemaType.Type {
	case String, ArraySchema:
		value := argInt(vm, "schema.max", args, 0)
		schemaType.MaxLen = &value
	case Number:
		value := asFloat(args[0], vm)
		schemaType.MaxNum = &value
	default:
		vm.runtimeError(ErrorRuntime, "schema.max is not supported for this schema")
		return
	}
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeLength(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.length", args, 1)
	if schemaType.Type != String && schemaType.Type != ArraySchema {
		vm.runtimeError(ErrorRuntime, "schema.length is only supported for string and array schemas")
		return
	}
	value := argInt(vm, "schema.length", args, 0)
	schemaType.ExactLen = &value
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeNonempty(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.nonempty", args)
	schemaType.NonEmpty = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeEmail(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.email", args)
	if schemaType.Type != String {
		vm.runtimeError(ErrorRuntime, "schema.email is only supported for string schemas")
		return
	}
	schemaType.Email = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeURL(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.url", args)
	if schemaType.Type != String {
		vm.runtimeError(ErrorRuntime, "schema.url is only supported for string schemas")
		return
	}
	schemaType.Url = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeRegex(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.regex", args, 1)
	if schemaType.Type != String {
		vm.runtimeError(ErrorRuntime, "schema.regex is only supported for string schemas")
		return
	}
	schemaType.Regex = argString(vm, "schema.regex", args, 0)
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeTrim(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.trim", args)
	if schemaType.Type != String {
		vm.runtimeError(ErrorRuntime, "schema.trim is only supported for string schemas")
		return
	}
	schemaType.Trim = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeInt(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.int", args)
	if schemaType.Type != Number {
		vm.runtimeError(ErrorRuntime, "schema.int is only supported for number schemas")
		return
	}
	schemaType.IntOnly = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypePositive(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.positive", args)
	if schemaType.Type != Number {
		vm.runtimeError(ErrorRuntime, "schema.positive is only supported for number schemas")
		return
	}
	schemaType.Positive = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeItems(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.items", args, 1)
	if schemaType.Type != ArraySchema {
		vm.runtimeError(ErrorRuntime, "schema.items is only supported for array schemas")
		return
	}
	schemaType.ItemSchema = cloneValidateType(expectSchemaArg(vm, "schema.items", args, 0))
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeShape(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.shape", args, 1)
	if schemaType.Type != ObjectSchema {
		vm.runtimeError(ErrorRuntime, "schema.shape is only supported for object schemas")
		return
	}
	schemaType.Fields = schemaFieldsFromObject(vm, "schema.shape", argObject(vm, "schema.shape", args, 0))
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeStrict(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.strict", args)
	if schemaType.Type != ObjectSchema {
		vm.runtimeError(ErrorRuntime, "schema.strict is only supported for object schemas")
		return
	}
	schemaType.Strict = true
	vm.push(newSchemaValue(schemaType))
}

func schemaTypePartial(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	dontExpectArgs(vm, "schema.partial", args)
	if schemaType.Type != ObjectSchema {
		vm.runtimeError(ErrorRuntime, "schema.partial is only supported for object schemas")
		return
	}
	for _, field := range schemaType.Fields {
		field.Required = false
	}
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeRefine(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.refine", args, 1)
	fn := argFn(vm, "schema.refine", args, 0)
	schemaType.RefineFn = &fn
	vm.push(newSchemaValue(schemaType))
}

func schemaTypeTransform(vm *VM, schemaType *NativeValidateType, args []TinyValue) {
	expectArgs(vm, "schema.transform", args, 1)
	fn := argFn(vm, "schema.transform", args, 0)
	schemaType.TransformFn = &fn
	vm.push(newSchemaValue(schemaType))
}
