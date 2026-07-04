package main

import (
	"strings"
	"testing"
)

func TestNewFeaturesNumberLiterals(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("new_features_number_literals.tiny")))
	want := strings.Join([]string{
		"255",
		"10",
		"63",
		"26",
		"4096",
		"328",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestNewFeaturesArrowFunctions(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("new_features_arrow_functions.tiny")))
	want := strings.Join([]string{
		"7",
		"25",
		"hello",
		"14",
		"15",
		"15",
		"12",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}

func TestNewFeaturesStructuralTypes(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("new_features_structural_types.tiny")))
	want := strings.Join([]string{
		"Alice",
		"30",
		"Bob",
		"25",
		"1",
		"test",
		"35",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}
