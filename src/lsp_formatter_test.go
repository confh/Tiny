package main

import "testing"

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

func TestFormatTinyDocumentFieldQuestionMarkSuffix(t *testing.T) {
	input := "class Bot {\nfield handler? = null\nfield private handle?: any = null\n}\n"
	got := formatTinyDocument(input)
	want := "class Bot {\n    field handler? = null\n    field private handle?: any = null\n}\n"

	if got != want {
		t.Fatalf("unexpected formatted document:\nwant:\n%q\ngot:\n%q", want, got)
	}
}

