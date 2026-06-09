package main

import (
	"reflect"
	"testing"
)

func TestSortedNativeImportsDeterministic(t *testing.T) {
	imports := map[string]bool{
		`"strings"`: true,
		`"fmt"`:     true,
		`"math"`:    true,
	}

	want := []string{`"fmt"`, `"math"`, `"strings"`}

	for i := 0; i < 20; i++ {
		got := sortedNativeImports(imports)
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("sortedNativeImports() = %#v, want %#v", got, want)
		}
	}
}
