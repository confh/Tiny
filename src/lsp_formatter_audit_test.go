package main

import (
	"testing"
)

func assertFormat(t *testing.T, name, input, want string) {
	t.Helper()
	got := formatTinyDocument(input)
	if got != want {
		t.Errorf("%s:\n  input:  %q\n  want:   %q\n  got:    %q", name, input, want, got)
	}
}

func assertFormatStable(t *testing.T, name, input string) {
	t.Helper()
	first := formatTinyDocument(input)
	second := formatTinyDocument(first)
	if first != second {
		t.Errorf("%s not idempotent:\n  input:    %q\n  pass 1:   %q\n  pass 2:   %q", name, input, first, second)
	}
}

func TestFormatterAuditNestedObjectLiterals(t *testing.T) {
	assertFormat(t, "nested object",
		`const test = { payload: { sub: "123" }, valid: false }`,
		`const test = { payload: { sub: "123" }, valid: false }`,
	)
	assertFormatStable(t, "nested object",
		`const test = { payload: { sub: "123" }, valid: false }`,
	)
}

func TestFormatterAuditArrayAssignment(t *testing.T) {
	assertFormat(t, "array assign",
		`const arr = [3]`,
		`const arr = [3]`,
	)
	assertFormatStable(t, "array assign", `const arr = [3]`)

	assertFormat(t, "array assign multi",
		`const arr = [1, 2, 3]`,
		`const arr = [1, 2, 3]`,
	)
}

func TestFormatterAuditEmptyObject(t *testing.T) {
	assertFormat(t, "empty object",
		`const x = {}`,
		`const x = {}`,
	)
}

func TestFormatterAuditEmptyArray(t *testing.T) {
	assertFormat(t, "empty array",
		`const x = []`,
		`const x = []`,
	)
}

func TestFormatterAuditObjectWithTrailingComma(t *testing.T) {
	assertFormat(t, "trailing comma object",
		`const x = { a: 1, }`,
		`const x = { a: 1, }`,
	)
}

func TestFormatterAuditArrayWithTrailingComma(t *testing.T) {
	assertFormat(t, "trailing comma array",
		`const x = [1, 2, ]`,
		`const x = [1, 2,]`,
	)
}

func TestFormatterAuditDeeplyNestedObjects(t *testing.T) {
	assertFormat(t, "deeply nested",
		`const x = { a: { b: { c: 1 } } }`,
		`const x = { a: { b: { c: 1 } } }`,
	)
	assertFormatStable(t, "deeply nested",
		`const x = { a: { b: { c: 1 } } }`,
	)
}

func TestFormatterAuditNestedArrays(t *testing.T) {
	assertFormat(t, "nested arrays",
		`const x = [[1], [2, 3]]`,
		`const x = [[1], [2, 3]]`,
	)
}

func TestFormatterAuditFunctionCallWithObjectArg(t *testing.T) {
	assertFormat(t, "fn call with object",
		`foo({ a: 1, b: 2 })`,
		`foo({ a: 1, b: 2 })`,
	)
}

func TestFormatterAuditFunctionCallWithArrayArg(t *testing.T) {
	assertFormat(t, "fn call with array",
		`foo([1, 2, 3])`,
		`foo([1, 2, 3])`,
	)
}

func TestFormatterAuditMethodCallWithObject(t *testing.T) {
	assertFormat(t, "method call with object",
		`arr.push({ x: 1 })`,
		`arr.push({ x: 1 })`,
	)
}

func TestFormatterAuditAssignmentOperators(t *testing.T) {
	assertFormat(t, "plus assign",
		`x += 1`,
		`x += 1`,
	)
	assertFormat(t, "minus assign",
		`x -= 1`,
		`x -= 1`,
	)
	assertFormat(t, "multiply assign",
		`x *= 2`,
		`x *= 2`,
	)
	assertFormat(t, "divide assign",
		`x /= 2`,
		`x /= 2`,
	)
}

func TestFormatterAuditTernary(t *testing.T) {
	assertFormat(t, "ternary",
		`const x = a ? b : c`,
		`const x = a ? b : c`,
	)
}

func TestFormatterAuditChainedMethodCalls(t *testing.T) {
	assertFormat(t, "chained methods",
		`arr.filter(fn(x) { x > 0 }).map(fn(x) { x * 2 })`,
		`arr.filter(fn(x) { x > 0 }).map(fn(x) { x * 2 })`,
	)
}

func TestFormatterAuditStringInterpolation(t *testing.T) {
	assertFormat(t, "string interpolation",
		`const msg = "hello ${name}"`,
		`const msg = "hello ${name}"`,
	)
}

func TestFormatterAuditObjectInArray(t *testing.T) {
	assertFormat(t, "object in array",
		`const items = [{ name: "a" }, { name: "b" }]`,
		`const items = [{ name: "a" }, { name: "b" }]`,
	)
	assertFormatStable(t, "object in array",
		`const items = [{ name: "a" }, { name: "b" }]`,
	)
}

func TestFormatterAuditArrayInObject(t *testing.T) {
	assertFormat(t, "array in object",
		`const config = { tags: [1, 2, 3] }`,
		`const config = { tags: [1, 2, 3] }`,
	)
	assertFormatStable(t, "array in object",
		`const config = { tags: [1, 2, 3] }`,
	)
}

func TestFormatterAuditSpreadInObject(t *testing.T) {
	assertFormat(t, "spread in object",
		`const x = { ...a, b: 1 }`,
		`const x = { ...a, b: 1 }`,
	)
}

func TestFormatterAuditSpreadInArray(t *testing.T) {
	assertFormat(t, "spread in array",
		`const x = [...a, 1]`,
		`const x = [...a, 1]`,
	)
}

func TestFormatterAuditNullCoalesce(t *testing.T) {
	assertFormat(t, "null coalesce",
		`const x = a ?? b`,
		`const x = a ?? b`,
	)
}

func TestFormatterAuditOptionalChaining(t *testing.T) {
	assertFormat(t, "optional chaining",
		`const x = a?.b?.c`,
		`const x = a?.b?.c`,
	)
}

func TestFormatterAuditComparisonOperators(t *testing.T) {
	assertFormat(t, "equals",
		`if a == b {}`,
		`if a == b {}`,
	)
	assertFormat(t, "not equals",
		`if a != b {}`,
		`if a != b {}`,
	)
	assertFormat(t, "less or equal",
		`if a <= b {}`,
		`if a <= b {}`,
	)
	assertFormat(t, "greater or equal",
		`if a >= b {}`,
		`if a >= b {}`,
	)
}

func TestFormatterAuditLogicalOperators(t *testing.T) {
	assertFormat(t, "and",
		`if a && b {}`,
		`if a && b {}`,
	)
	assertFormat(t, "or",
		`if a || b {}`,
		`if a || b {}`,
	)
}

func TestFormatterAuditArrowFunction(t *testing.T) {
	assertFormat(t, "arrow fn",
		`const add = (a, b) => a + b`,
		`const add = (a, b) => a + b`,
	)
}

func TestFormatterAuditObjectLiteralAsFunctionArg(t *testing.T) {
	assertFormat(t, "object literal as fn arg",
		`http.post("url", { name: "test" })`,
		`http.post("url", { name: "test" })`,
	)
}

func TestFormatterAuditNestedObjectLiteralAsFunctionArg(t *testing.T) {
	assertFormat(t, "nested object as fn arg",
		`http.post("url", { files: [{ filename: "a.png" }] })`,
		`http.post("url", { files: [{ filename: "a.png" }] })`,
	)
	assertFormatStable(t, "nested object as fn arg",
		`http.post("url", { files: [{ filename: "a.png" }] })`,
	)
}

func TestFormatterAuditMapCall(t *testing.T) {
	assertFormat(t, "map call",
		`arr.map(fn(x) { return x * 2 })`,
		`arr.map(fn(x) { return x * 2 })`,
	)
}

func TestFormatterAuditMultiLineObject(t *testing.T) {
	input := "const x = {\n  name: \"test\",\n  value: 123\n}\n"
	want := "const x = {\n    name: \"test\",\n    value: 123\n}\n"
	assertFormat(t, "multiline object", input, want)
}

func TestFormatterAuditMultiLineArray(t *testing.T) {
	assertFormat(t, "multiline array",
		"const x = [\n  1,\n  2,\n  3\n]\n",
		"const x = [1, 2, 3]\n",
	)
}

func TestFormatterAuditSingleLineIfBraces(t *testing.T) {
	assertFormat(t, "single line if",
		`if true { io.println("yes") }`,
		`if true { io.println("yes") }`,
	)
}

func TestFormatterAuditReturnObject(t *testing.T) {
	assertFormat(t, "return object",
		`return { name: "test" }`,
		`return { name: "test" }`,
	)
}

func TestFormatterAuditReturnArray(t *testing.T) {
	assertFormat(t, "return array",
		`return [1, 2, 3]`,
		`return [1, 2, 3]`,
	)
}

func TestFormatterAuditTypeHintObject(t *testing.T) {
	assertFormat(t, "type hint object",
		`fn test(x: { name: string }) {}`,
		`fn test(x: { name: string }) {}`,
	)
}

func TestFormatterAuditConstArray(t *testing.T) {
	assertFormat(t, "const array",
		`const arr: string[] = []`,
		`const arr: string[] = []`,
	)
}

func TestFormatterAuditObjectField(t *testing.T) {
	assertFormat(t, "object field",
		`const x = { name: "a", age: 1, active: true }`,
		`const x = { name: "a", age: 1, active: true }`,
	)
	assertFormatStable(t, "object field",
		`const x = { name: "a", age: 1, active: true }`,
	)
}

func TestFormatterAuditColonInTypeHint(t *testing.T) {
	assertFormat(t, "colon in type hint",
		`fn test(x: string): number {}`,
		`fn test(x: string): number {}`,
	)
}

func TestFormatterAuditForLoop(t *testing.T) {
	assertFormat(t, "for loop",
		`for let i = 0; i < 10; i++ { io.println(i) }`,
		`for let i = 0; i < 10; i++ { io.println(i) }`,
	)
}

func TestFormatterAuditForEach(t *testing.T) {
	assertFormat(t, "for each",
		`arr.forEach(fn(i, item) { io.println(item) })`,
		`arr.forEach(fn(i, item) { io.println(item) })`,
	)
}

func TestFormatterAuditMatchExpression(t *testing.T) {
	assertFormat(t, "match",
		`match x { case 1: "one" case _: "other" }`,
		`match x { case 1: "one" case _: "other" }`,
	)
}

func TestFormatterAuditWhileLoop(t *testing.T) {
	assertFormat(t, "while",
		`while x > 0 { x-- }`,
		`while x > 0 { x-- }`,
	)
}

func TestFormatterAuditLockStatement(t *testing.T) {
	assertFormat(t, "lock",
		`lock mutex { doWork() }`,
		`lock mutex { doWork() }`,
	)
}

func TestFormatterAuditSpawnExpression(t *testing.T) {
	assertFormat(t, "spawn",
		`spawn fn() { doWork() }`,
		`spawn fn() { doWork() }`,
	)
}

func TestFormatterAuditNegativeNumber(t *testing.T) {
	assertFormat(t, "negative number",
		`const x = -1`,
		`const x = -1`,
	)
}

func TestFormatterAuditNegativeFloat(t *testing.T) {
	assertFormat(t, "negative float",
		`const x = -1.5`,
		`const x = -1.5`,
	)
}

func TestFormatterAuditPowerOperator(t *testing.T) {
	assertFormat(t, "power",
		`const x = a ** b`,
		`const x = a ** b`,
	)
}

func TestFormatterAuditModulo(t *testing.T) {
	assertFormat(t, "modulo",
		`const x = a % b`,
		`const x = a % b`,
	)
}

func TestFormatterAuditBitwiseOperators(t *testing.T) {
	assertFormat(t, "bitwise and",
		`const x = a & b`,
		`const x = a & b`,
	)
	assertFormat(t, "bitwise or",
		`const x = a | b`,
		`const x = a | b`,
	)
	assertFormat(t, "bitwise xor",
		`const x = a ^ b`,
		`const x = a ^ b`,
	)
}

func TestFormatterAuditShiftOperators(t *testing.T) {
	assertFormat(t, "left shift",
		`const x = a << 1`,
		`const x = a << 1`,
	)
	assertFormat(t, "right shift",
		`const x = a >> 1`,
		`const x = a >> 1`,
	)
}

func TestFormatterAuditIncrementDecrement(t *testing.T) {
	assertFormat(t, "increment",
		`x++`,
		`x++`,
	)
	assertFormat(t, "decrement",
		`x--`,
		`x--`,
	)
}

func TestFormatterAuditBangOperator(t *testing.T) {
	assertFormat(t, "bang",
		`if !flag {}`,
		`if !flag {}`,
	)
}

func TestFormatterAuditBangEquals(t *testing.T) {
	assertFormat(t, "bang equals",
		`if a != b {}`,
		`if a != b {}`,
	)
}

func TestFormatterAuditStringConcat(t *testing.T) {
	assertFormat(t, "string concat",
		`const msg = "hello" + " " + "world"`,
		`const msg = "hello" + " " + "world"`,
	)
}

func TestFormatterAuditNumberLiteral(t *testing.T) {
	assertFormat(t, "hex literal",
		`const x = 0xFF`,
		`const x = 0xFF`,
	)
	assertFormat(t, "binary literal",
		`const x = 0b1010`,
		`const x = 0b1010`,
	)
	assertFormat(t, "octal literal",
		`const x = 0o77`,
		`const x = 0o77`,
	)
}

func TestFormatterAuditClassDefinition(t *testing.T) {
	input := "class Foo {\nfield name = \"\"\nfield age = 0\nfn greet() { return \"hi\" }\n}\n"
	want := "class Foo {\n    field name = \"\"\n    field age = 0\n    fn greet() { return \"hi\" }\n}\n"
	assertFormat(t, "class definition", input, want)
}

func TestFormatterAuditInterfaceDefinition(t *testing.T) {
	input := "interface Drawable {\nx: number\ny: number\nfn draw(): void\n}\n"
	want := "interface Drawable {\n    x: number\n    y: number\n    fn draw(): void\n}\n"
	assertFormat(t, "interface definition", input, want)
}

func TestFormatterAuditEnumDefinition(t *testing.T) {
	input := "enum Direction {\nUP\nDOWN\nLEFT\nRIGHT\n}\n"
	want := "enum Direction {\n    UP\n    DOWN\n    LEFT\n    RIGHT\n}\n"
	assertFormat(t, "enum definition", input, want)
}

func TestFormatterAuditImportStatement(t *testing.T) {
	assertFormat(t, "import",
		`import std "io"`,
		`import std "io"`,
	)
}

func TestFormatterAuditExportFunction(t *testing.T) {
	assertFormat(t, "export fn",
		`export fn main() {}`,
		`export fn main() {}`,
	)
}

func TestFormatterAuditCommentPreservation(t *testing.T) {
	assertFormat(t, "inline comment",
		`const x = 1 // important`,
		`const x = 1 // important`,
	)
}

func TestFormatterAuditEmptyLinesCollapsed(t *testing.T) {
	input := "const a = 1\n\n\n\nconst b = 2\n"
	want := "const a = 1\n\nconst b = 2\n"
	assertFormat(t, "empty line collapse", input, want)
}

func TestFormatterAuditTryCatchFinally(t *testing.T) {
	input := "try {\ndoWork()\n}\ncatch (e) {\nhandleError(e)\n}\nfinally {\ncleanup()\n}\n"
	want := "try {\n    doWork()\n} catch (e) {\n    handleError(e)\n} finally {\n    cleanup()\n}\n"
	assertFormat(t, "try catch finally", input, want)
}

func TestFormatterAuditNestedTernary(t *testing.T) {
	assertFormat(t, "nested ternary",
		`const x = a ? (b ? c : d) : e`,
		`const x = a ? (b ? c : d) : e`,
	)
}

func TestFormatterAuditMethodChaining(t *testing.T) {
	assertFormat(t, "method chaining",
		`arr.filter(fn(x) { x > 0 }).map(fn(x) { x * 2 }).length()`,
		`arr.filter(fn(x) { x > 0 }).map(fn(x) { x * 2 }).length()`,
	)
}

func TestFormatterAuditComplexNestedExpression(t *testing.T) {
	assertFormat(t, "complex nested",
		`const result = fn({ a: 1, b: [2, 3] })`,
		`const result = fn({ a: 1, b: [2, 3] })`,
	)
}

func TestFormatterAuditObjectWithNestedFunction(t *testing.T) {
	assertFormat(t, "object with fn",
		`const handlers = { click: fn(e) { handle(e) } }`,
		`const handlers = { click: fn(e) { handle(e) } }`,
	)
}

func TestFormatterAuditArrayWithNestedObject(t *testing.T) {
	assertFormat(t, "array with nested object",
		`const items = [{ type: "a", data: {} }, { type: "b", data: {} }]`,
		`const items = [{ type: "a", data: {} }, { type: "b", data: {} }]`,
	)
	assertFormatStable(t, "array with nested object",
		`const items = [{ type: "a", data: {} }, { type: "b", data: {} }]`,
	)
}

func TestFormatterAuditTernaryWithObject(t *testing.T) {
	assertFormat(t, "ternary with object",
		`const x = cond ? { a: 1 } : { b: 2 }`,
		`const x = cond ? { a: 1 } : { b: 2 }`,
	)
}

func TestFormatterAuditSpreadInNestedObject(t *testing.T) {
	assertFormat(t, "spread in nested object",
		`const x = { a: { ...b, c: 1 } }`,
		`const x = { a: { ...b, c: 1 } }`,
	)
}

func TestFormatterAuditObjectNestedThreeLevels(t *testing.T) {
	assertFormat(t, "3 levels deep",
		`const x = { a: { b: { c: { d: 1 } } } }`,
		`const x = { a: { b: { c: { d: 1 } } } }`,
	)
	assertFormatStable(t, "3 levels deep",
		`const x = { a: { b: { c: { d: 1 } } } }`,
	)
}

func TestFormatterAuditMixedNestedObjectArray(t *testing.T) {
	assertFormat(t, "mixed nested",
		`const x = { a: [1, { b: 2 }], c: [3] }`,
		`const x = { a: [1, { b: 2 }], c: [3] }`,
	)
	assertFormatStable(t, "mixed nested",
		`const x = { a: [1, { b: 2 }], c: [3] }`,
	)
}

func TestFormatterAuditObjectInReturn(t *testing.T) {
	assertFormat(t, "return nested object",
		`return { data: { items: [1, 2] } }`,
		`return { data: { items: [1, 2] } }`,
	)
	assertFormatStable(t, "return nested object",
		`return { data: { items: [1, 2] } }`,
	)
}

func TestFormatterAuditArraySpreadObject(t *testing.T) {
	assertFormat(t, "array spread object",
		`const x = [{ ...defaults, name: "test" }]`,
		`const x = [{ ...defaults, name: "test" }]`,
	)
}

func TestFormatterAuditComplexFunctionCall(t *testing.T) {
	assertFormat(t, "complex fn call",
		`http.post("url", { files: [{ filename: "a.png", content: data }], headers: { auth: token } })`,
		`http.post("url", { files: [{ filename: "a.png", content: data }], headers: { auth: token } })`,
	)
	assertFormatStable(t, "complex fn call",
		`http.post("url", { files: [{ filename: "a.png", content: data }], headers: { auth: token } })`,
	)
}

func TestFormatterAuditAssignArray(t *testing.T) {
	assertFormat(t, "assign array",
		`let arr = []`,
		`let arr = []`,
	)
}

func TestFormatterAuditReassignArray(t *testing.T) {
	assertFormat(t, "reassign array",
		`arr = [1, 2, 3]`,
		`arr = [1, 2, 3]`,
	)
}

func TestFormatterAuditPushObject(t *testing.T) {
	assertFormat(t, "push object",
		`arr.push({ name: "test" })`,
		`arr.push({ name: "test" })`,
	)
}

func TestFormatterAuditArrayIndex(t *testing.T) {
	assertFormat(t, "array index",
		`const x = arr[0]`,
		`const x = arr[0]`,
	)
}

func TestFormatterAuditObjectPropertyAccess(t *testing.T) {
	assertFormat(t, "property access",
		`const x = obj.name`,
		`const x = obj.name`,
	)
}

func TestFormatterAuditNestedPropertyAccess(t *testing.T) {
	assertFormat(t, "nested property access",
		`const x = obj.nested.name`,
		`const x = obj.nested.name`,
	)
}

func TestFormatterAuditComputedPropertyAccess(t *testing.T) {
	assertFormat(t, "computed property",
		`const x = obj[key]`,
		`const x = obj[key]`,
	)
}

func TestFormatterAuditStringInObject(t *testing.T) {
	assertFormat(t, "string in object",
		`const x = { msg: "hello world" }`,
		`const x = { msg: "hello world" }`,
	)
}

func TestFormatterAuditObjectWithBooleanValues(t *testing.T) {
	assertFormat(t, "boolean values",
		`const x = { a: true, b: false }`,
		`const x = { a: true, b: false }`,
	)
}

func TestFormatterAuditObjectWithNullValue(t *testing.T) {
	assertFormat(t, "null value",
		`const x = { a: null }`,
		`const x = { a: null }`,
	)
}

func TestFormatterAuditObjectWithNumberValues(t *testing.T) {
	assertFormat(t, "number values",
		`const x = { a: 1, b: 2.5, c: 0xFF }`,
		`const x = { a: 1, b: 2.5, c: 0xFF }`,
	)
}

func TestFormatterAuditConditionalAssignment(t *testing.T) {
	assertFormat(t, "conditional assign",
		`const x = a ?? []`,
		`const x = a ?? []`,
	)
}

func TestFormatterAuditTernaryWithArray(t *testing.T) {
	assertFormat(t, "ternary with array",
		`const x = cond ? [1, 2] : [3, 4]`,
		`const x = cond ? [1, 2] : [3, 4]`,
	)
}

func TestFormatterAuditObjectInConditional(t *testing.T) {
	assertFormat(t, "object in conditional",
		`if x == { a: 1 } {}`,
		`if x == { a: 1 } {}`,
	)
}

func TestFormatterAuditArrayInConditional(t *testing.T) {
	assertFormat(t, "array in conditional",
		`if x == [1, 2] {}`,
		`if x == [1, 2] {}`,
	)
}

func TestFormatterAuditAssignmentObject(t *testing.T) {
	assertFormat(t, "assignment object",
		`x = { name: "test" }`,
		`x = { name: "test" }`,
	)
}

func TestFormatterAuditAssignmentArray(t *testing.T) {
	assertFormat(t, "assignment array",
		`x = [1, 2, 3]`,
		`x = [1, 2, 3]`,
	)
}

func TestFormatterAuditMultiObjectLiterals(t *testing.T) {
	assertFormat(t, "multi object literals",
		`const a = { x: 1 }; const b = { y: 2 }`,
		`const a = { x: 1 }; const b = { y: 2 }`,
	)
}

func TestFormatterAuditObjectWithNestedArrayAndObject(t *testing.T) {
	assertFormat(t, "nested array and object",
		`const config = { routes: [{ path: "/", handler: {} }], middleware: [] }`,
		`const config = { routes: [{ path: "/", handler: {} }], middleware: [] }`,
	)
	assertFormatStable(t, "nested array and object",
		`const config = { routes: [{ path: "/", handler: {} }], middleware: [] }`,
	)
}

func TestFormatterAuditMethodChainingWithObject(t *testing.T) {
	assertFormat(t, "method chain with object",
		`http.post("/api", { body: data }).then(fn(r) { return r.json() })`,
		`http.post("/api", { body: data }).then(fn(r) { return r.json() })`,
	)
}

func TestFormatterAuditObjectInForIn(t *testing.T) {
	assertFormat(t, "object in for-in",
		`for key, value in obj { io.println(key) }`,
		`for key, value in obj { io.println(key) }`,
	)
}

func TestFormatterAuditSwitchLike(t *testing.T) {
	assertFormat(t, "switch-like",
		`match status { case 200: "ok" case 404: "not found" case _: "error" }`,
		`match status { case 200: "ok" case 404: "not found" case _: "error" }`,
	)
}

func TestFormatterAuditComplexSpread(t *testing.T) {
	assertFormat(t, "complex spread",
		`const merged = { ...defaults, ...overrides, specific: 1 }`,
		`const merged = { ...defaults, ...overrides, specific: 1 }`,
	)
}

func TestFormatterAuditNestedSpread(t *testing.T) {
	assertFormat(t, "nested spread",
		`const x = { a: { ...b, c: 1 }, d: [...e, 2] }`,
		`const x = { a: { ...b, c: 1 }, d: [...e, 2] }`,
	)
	assertFormatStable(t, "nested spread",
		`const x = { a: { ...b, c: 1 }, d: [...e, 2] }`,
	)
}

func TestFormatterAuditObjectLiteralInlineFunction(t *testing.T) {
	assertFormat(t, "object with inline fn",
		`const handlers = { onClick: fn(e) { handle(e) }, onClose: fn() { cleanup() } }`,
		`const handlers = { onClick: fn(e) { handle(e) }, onClose: fn() { cleanup() } }`,
	)
	assertFormatStable(t, "object with inline fn",
		`const handlers = { onClick: fn(e) { handle(e) }, onClose: fn() { cleanup() } }`,
	)
}

func TestFormatterAuditEmptyObjectInArray(t *testing.T) {
	assertFormat(t, "empty object in array",
		`const items = [{}]`,
		`const items = [{}]`,
	)
}

func TestFormatterAuditEmptyArrayInObject(t *testing.T) {
	assertFormat(t, "empty array in object",
		`const config = { items: [] }`,
		`const config = { items: [] }`,
	)
}

func TestFormatterAuditMultipleEmptyObjects(t *testing.T) {
	assertFormat(t, "multiple empty objects",
		`const a = {}; const b = {}`,
		`const a = {}; const b = {}`,
	)
}

func TestFormatterAuditMultipleEmptyArrays(t *testing.T) {
	assertFormat(t, "multiple empty arrays",
		`const a = []; const b = []`,
		`const a = []; const b = []`,
	)
}

func TestFormatterAuditMixedEmpty(t *testing.T) {
	assertFormat(t, "mixed empty",
		`const a = {}; const b = []; const c = { d: [] }; const e = { f: {} }`,
		`const a = {}; const b = []; const c = { d: [] }; const e = { f: {} }`,
	)
	assertFormatStable(t, "mixed empty",
		`const a = {}; const b = []; const c = { d: [] }; const e = { f: {} }`,
	)
}

func TestFormatterAuditObjectAfterReturn(t *testing.T) {
	assertFormat(t, "return object multiline",
		"return {\n  name: \"test\",\n  value: 42\n}\n",
		"return {\n    name: \"test\",\n    value: 42\n}\n",
	)
}

func TestFormatterAuditArrayAfterReturn(t *testing.T) {
	assertFormat(t, "return array multiline",
		"return [\n  1,\n  2,\n  3\n]\n",
		"return [1, 2, 3]\n",
	)
}

func TestFormatterAuditObjectAfterAssignment(t *testing.T) {
	assertFormat(t, "assign object multiline",
		"const x = {\n  name: \"test\",\n  value: 42\n}\n",
		"const x = {\n    name: \"test\",\n    value: 42\n}\n",
	)
}

func TestFormatterAuditArrayAfterAssignment(t *testing.T) {
	assertFormat(t, "assign array multiline",
		"const x = [\n  1,\n  2,\n  3\n]\n",
		"const x = [1, 2, 3]\n",
	)
}

func TestFormatterAuditObjectAfterLet(t *testing.T) {
	assertFormat(t, "let object",
		`let config = { debug: true }`,
		`let config = { debug: true }`,
	)
}

func TestFormatterAuditFieldInitializer(t *testing.T) {
	assertFormat(t, "field init",
		`field name = ""`,
		`field name = ""`,
	)
}

func TestFormatterAuditFieldInitObject(t *testing.T) {
	assertFormat(t, "field init object",
		`field config = {}`,
		`field config = {}`,
	)
}

func TestFormatterAuditFieldInitArray(t *testing.T) {
	assertFormat(t, "field init array",
		`field items = []`,
		`field items = []`,
	)
}

func TestFormatterAuditConditionalExpression(t *testing.T) {
	assertFormat(t, "conditional expression",
		`const x = a > 0 ? a : 0`,
		`const x = a > 0 ? a : 0`,
	)
}

func TestFormatterAuditBooleanOperators(t *testing.T) {
	assertFormat(t, "boolean operators",
		`if a == true && b == false || c != null {}`,
		`if a == true && b == false || c != null {}`,
	)
}

func TestFormatterAuditStringComparison(t *testing.T) {
	assertFormat(t, "string comparison",
		`if name == "test" {}`,
		`if name == "test" {}`,
	)
}

func TestFormatterAuditNullComparison(t *testing.T) {
	assertFormat(t, "null comparison",
		`if x == null {}`,
		`if x == null {}`,
	)
}

func TestFormatterAuditInstanceof(t *testing.T) {
	assertFormat(t, "instanceof",
		`if x instanceof string {}`,
		`if x instanceof string {}`,
	)
}

func TestFormatterAuditTypeof(t *testing.T) {
	assertFormat(t, "typeof",
		`if typeof x == "string" {}`,
		`if typeof x == "string" {}`,
	)
}

func TestFormatterAuditInOperator(t *testing.T) {
	assertFormat(t, "in operator",
		`if "key" in obj {}`,
		`if "key" in obj {}`,
	)
}

func TestFormatterAuditStringTemplate(t *testing.T) {
	assertFormat(t, "template string",
		"const msg = `hello ${name}`",
		"const msg = `hello ${name}`",
	)
}

func TestFormatterAuditDestructuring(t *testing.T) {
	assertFormat(t, "destructuring",
		`const { a, b } = obj`,
		`const { a, b } = obj`,
	)
}

func TestFormatterAuditArrayDestructuring(t *testing.T) {
	assertFormat(t, "array destructuring",
		`const [a, b] = arr`,
		`const [a, b] = arr`,
	)
}

func TestFormatterAuditFunctionDestructuring(t *testing.T) {
	assertFormat(t, "fn param destructuring",
		`fn test({ name, age }) {}`,
		`fn test({ name, age }) {}`,
	)
}

func TestFormatterAuditExportConst(t *testing.T) {
	assertFormat(t, "export const",
		`export const VERSION = "1.0"`,
		`export const VERSION = "1.0"`,
	)
}

func TestFormatterAuditExportClass(t *testing.T) {
	assertFormat(t, "export class",
		`export class Foo {}`,
		`export class Foo {}`,
	)
}

func TestFormatterAuditExportInterface(t *testing.T) {
	assertFormat(t, "export interface",
		`export interface Bar {}`,
		`export interface Bar {}`,
	)
}

func TestFormatterAuditExportEnum(t *testing.T) {
	assertFormat(t, "export enum",
		`export enum Color { RED, GREEN, BLUE }`,
		`export enum Color { RED, GREEN, BLUE }`,
	)
}

func TestFormatterAuditExportDefault(t *testing.T) {
	assertFormat(t, "export default",
		`export default fn() {}`,
		`export default fn() {}`,
	)
}
