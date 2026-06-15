package vm

import . "language.com/src/tinyerrors"

var stdValidateMetadata = StdModuleInfo{
	Name: "validate",
}

var stdValidateMethods map[string]StdModuleFunc

func init() {
	stdValidateMethods = map[string]StdModuleFunc{
		"string": validateString,
		"number": validateNumber,
		"bool":   validateBool,
		"array":  validateArray,
		"object": validateObject,
		"enum":   validateEnum,
		"union":  validateUnion,
		"any":    validateAny,
		"body":   validateBody,
		"query":  validateQuery,
		"params": validateParams,
	}
	registerStdModule(stdValidateMetadata)
}

func (vm *VM) callStdValidate(method string, args []TinyValue) {
	fn, ok := stdValidateMethods[method]
	if !ok {
		vm.runtimeError(ErrorName, "unknown validate function: %s", method)
		return
	}

	fn(vm, args)
}

func validateString(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "validate.string", args)
	vm.push(newSchemaValue(&NativeValidateType{Type: String}))
}

func validateNumber(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "validate.number", args)
	vm.push(newSchemaValue(&NativeValidateType{Type: Number}))
}

func validateBool(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "validate.bool", args)
	vm.push(newSchemaValue(&NativeValidateType{Type: Bool}))
}

func validateArray(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "validate.array", args, 0, 1)
	schema := &NativeValidateType{Type: ArraySchema}
	if len(args) == 1 {
		schema.ItemSchema = expectSchemaArg(vm, "validate.array", args, 0)
	}
	vm.push(newSchemaValue(schema))
}

func validateObject(vm *VM, args []TinyValue) {
	expectArgsRange(vm, "validate.object", args, 0, 1)
	schema := &NativeValidateType{Type: ObjectSchema}
	if len(args) == 1 {
		schema.Fields = schemaFieldsFromObject(vm, "validate.object", argObject(vm, "validate.object", args, 0))
	}
	vm.push(newSchemaValue(schema))
}

func validateEnum(vm *VM, args []TinyValue) {
	values := enumValuesFromArgs(vm, "validate.enum", args)
	vm.push(newSchemaValue(&NativeValidateType{
		Type:       EnumSchema,
		EnumValues: values,
	}))
}

func validateUnion(vm *VM, args []TinyValue) {
	schemas := unionSchemasFromArgs(vm, "validate.union", args)
	vm.push(newSchemaValue(&NativeValidateType{
		Type:         UnionSchema,
		UnionSchemas: schemas,
	}))
}

func validateAny(vm *VM, args []TinyValue) {
	dontExpectArgs(vm, "validate.any", args)
	vm.push(newSchemaValue(&NativeValidateType{Type: AnySchema}))
}

func validateBody(vm *VM, args []TinyValue) {
	validateWebSource(vm, "validate.body", "body", args)
}

func validateQuery(vm *VM, args []TinyValue) {
	validateWebSource(vm, "validate.query", "query", args)
}

func validateParams(vm *VM, args []TinyValue) {
	validateWebSource(vm, "validate.params", "params", args)
}

func validateWebSource(vm *VM, fnName string, source string, args []TinyValue) {
	expectArgs(vm, fnName, args, 1)
	schema := cloneValidateType(expectSchemaArg(vm, fnName, args, 0))
	schema.WebSource = source
	vm.push(newSchemaValue(schema))
}
