package main

import (
	"strings"
	"testing"
)

func TestFormatTinyDocument(t *testing.T) {
	input := "import std \"io\";\nfn main(){\nio.println(1+2); // sum\nif true{\nio.println(\"ok\");\n}\n}\n"

	got := formatTinyDocument(input)
	want := "import std \"io\";\nfn main() {\n    io.println(1 + 2); // sum\n    if true {\n        io.println(\"ok\");\n    }\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted document:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentPreservesOperatorsInsideStrings(t *testing.T) {
	input := "let text=\"a+b // not comment\";\n"
	got := formatTinyDocument(input)
	want := "let text = \"a+b // not comment\";\n"

	if got != want {
		t.Fatalf("unexpected formatted string line:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentPreservesMultilineInterpolatedStringContents(t *testing.T) {
	input := strings.Join([]string{
		"fn page(){",
		"const html=`",
		"<div class=\"app\">",
		"  ${user.name}",
		"  <script>",
		"    const x=1+2",
		"    if(x){console.log(x)}",
		"  </script>",
		"</div>",
		"`",
		"io.println(html)",
		"}",
		"",
	}, "\n")

	got := formatTinyDocument(input)
	want := strings.Join([]string{
		"fn page() {",
		"    const html = `",
		"<div class=\"app\">",
		"  ${user.name}",
		"  <script>",
		"    const x=1+2",
		"    if(x){console.log(x)}",
		"  </script>",
		"</div>",
		"`",
		"    io.println(html)",
		"}",
		"",
	}, "\n")

	if got != want {
		t.Fatalf("unexpected formatted interpolated string:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentFieldQuestionMarkSuffix(t *testing.T) {
	input := "class Bot {\nfield handler? = null\nfield private handle?: any = null\n}\n"
	got := formatTinyDocument(input)
	want := "class Bot {\n    field handler? = null\n    field private handle?: any = null\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted document:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentKeepsUnaryOperatorsTight(t *testing.T) {
	input := "if ! enabled {\nlet negative = - value\nlet positive = + value\nlet compact = -1 + +2\nif a!=b {\n}\n}\n"
	got := formatTinyDocument(input)
	want := "if !enabled {\n    let negative = - value\n    let positive = + value\n    let compact = -1 + +2\n    if a != b {\n    }\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted unary operators:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentKeepsBinaryPlusMinusSpaced(t *testing.T) {
	input := "value1+value2\nvalue1 + value2\nvalue1-value2\nvalue1 - value2\n"
	got := formatTinyDocument(input)
	want := "value1 + value2\nvalue1 + value2\nvalue1 - value2\nvalue1 - value2\n"

	if got != want {
		t.Fatalf("unexpected formatted binary plus/minus:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentMatchesLongestOperatorsFirst(t *testing.T) {
	input := "flags&^=mask\nvalue<<=1\nvalue>>=1\nvalue&=mask\nvalue^=mask\nvalue|=mask\nfallback=a??b\n"
	got := formatTinyDocument(input)
	want := "flags &^= mask\nvalue <<= 1\nvalue >>= 1\nvalue &= mask\nvalue ^= mask\nvalue |= mask\nfallback = a ?? b\n"

	if got != want {
		t.Fatalf("unexpected formatted assignment operators:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentNormalizesColonSpacing(t *testing.T) {
	input := "fn handle(ctx:Httpx.Context,next){\nconst app=Httpx.app({port:3000,name:\"tiny\"})\nfield private handle?:any=null\nvalue:=1\n}\n"
	got := formatTinyDocument(input)
	want := "fn handle(ctx: Httpx.Context, next) {\n    const app = Httpx.app({ port: 3000, name: \"tiny\" })\n    field private handle?: any = null\n    value := 1\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted colon spacing:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentGenerics(t *testing.T) {
	input := strings.Join([]string{
		`interface Box:T {`,
		`    value: T`,
		`}`,
		`class Container:T:U {`,
		`    field item: T = null`,
		`}`,
		`fn identity:T(x: T): T {`,
		`    return x;`,
		`}`,
		`let b: Box:number = Box:number(42);`,
	}, "\n")

	got := formatTinyDocument(input)
	want := strings.Join([]string{
		`interface Box:T {`,
		`    value: T`,
		`}`,
		`class Container:T:U {`,
		`    field item: T = null`,
		`}`,
		`fn identity:T(x: T): T {`,
		`    return x;`,
		`}`,
		`let b: Box:number = Box:number(42);`,
	}, "\n")

	if got != want {
		t.Fatalf("unexpected formatted generics:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentReturnTypeSpacing(t *testing.T) {
	input := "fn test():string {\n    return \"ok\";\n}\n"
	got := formatTinyDocument(input)
	want := "fn test(): string {\n    return \"ok\";\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted return type spacing:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCuddledElse(t *testing.T) {
	input := "if true {\n    io.println(\"yes\")\n}\nelse {\n    io.println(\"no\")\n}\n"
	got := formatTinyDocument(input)
	want := "if true {\n    io.println(\"yes\")\n} else {\n    io.println(\"no\")\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted cuddled else:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCuddledElseIf(t *testing.T) {
	input := "if x > 0 {\n    io.println(\"positive\")\n}\nelse if x < 0 {\n    io.println(\"negative\")\n}\nelse {\n    io.println(\"zero\")\n}\n"
	got := formatTinyDocument(input)
	want := "if x > 0 {\n    io.println(\"positive\")\n} else if x < 0 {\n    io.println(\"negative\")\n} else {\n    io.println(\"zero\")\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted cuddled else if:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCuddledCatch(t *testing.T) {
	input := "try {\n    io.println(\"try\")\n}\ncatch (e) {\n    io.println(\"catch\")\n}\n"
	got := formatTinyDocument(input)
	want := "try {\n    io.println(\"try\")\n} catch (e) {\n    io.println(\"catch\")\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted cuddled catch:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCuddledFinally(t *testing.T) {
	input := "try {\n    io.println(\"try\")\n}\ncatch (e) {\n    io.println(\"catch\")\n}\nfinally {\n    io.println(\"finally\")\n}\n"
	got := formatTinyDocument(input)
	want := "try {\n    io.println(\"try\")\n} catch (e) {\n    io.println(\"catch\")\n} finally {\n    io.println(\"finally\")\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted cuddled finally:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseBlankLines(t *testing.T) {
	input := "fn main() {\n    io.println(1)\n\n\n    io.println(2)\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    io.println(1)\n\n    io.println(2)\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted blank line collapse:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCuddledElseWithBlankLines(t *testing.T) {
	input := "if true {\n    io.println(\"yes\")\n}\n\nelse {\n    io.println(\"no\")\n}\n"
	got := formatTinyDocument(input)
	want := "if true {\n    io.println(\"yes\")\n} else {\n    io.println(\"no\")\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted cuddled else with blank lines:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseMultilineCallArgs(t *testing.T) {
	input := "fn main() {\n    function(\n        data\n    )\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    function(data)\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted collapse multiline call args:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseMultilineCallMultipleArgs(t *testing.T) {
	input := "fn main() {\n    function(\n        a,\n        b\n    )\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    function(a, b)\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted collapse multiline call multiple args:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseMultilineArray(t *testing.T) {
	input := "fn main() {\n    const x = [\n        1,\n        2,\n        3\n    ]\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    const x = [1, 2, 3]\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted collapse multiline array:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentDoesNotCollapseLongArgs(t *testing.T) {
	longArg := strings.Repeat("a", 90)
	input := "fn main() {\n    function(\n        " + longArg + "\n    )\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    function(\n    " + longArg + "\n    )\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted long args should not collapse:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseNestedCalls(t *testing.T) {
	input := "fn main() {\n    foo(\n        bar(1, 2),\n        baz(3)\n    )\n}\n"
	got := formatTinyDocument(input)
	want := "fn main() {\n    foo(bar(1, 2), baz(3))\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted collapse nested calls:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

func TestFormatTinyDocumentCollapseMultilineFnParams(t *testing.T) {
	input := "fn process(\n    a,\n    b\n) {\n    io.println(a)\n}\n"
	got := formatTinyDocument(input)
	want := "fn process(a, b) {\n    io.println(a)\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted collapse fn params:\nwant:\n%q\ngot:\n%q", want, got)
	}
}
