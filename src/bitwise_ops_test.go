package main

import (
	"strings"
	"testing"
)

func TestTinyBitwiseOps(t *testing.T) {
	out := requireTinySuccess(t, runTinyFile(t, fixturePath("bitwise_ops.tiny")))

	want := strings.Join([]string{
		"2",
		"5",
		"6",
		"12",
		"4",
		"-6",
		"2",
		"6",
		"1",
		"4",
		"2",
		"",
	}, "\n")

	if out != want {
		t.Fatalf("unexpected output:\nwant:\n%q\ngot:\n%q", want, out)
	}
}
