package vm

import (
	"testing"
)

func TestSignedIntegerToStringHandlesNegativeValues(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want string
	}{
		{name: "negative int", got: intToString(-42), want: "-42"},
		{name: "minimum int", got: intToString(-int(^uint(0)>>1) - 1), want: "-9223372036854775808"},
		{name: "negative int64", got: int64ToString(-42), want: "-42"},
		{name: "minimum int64", got: int64ToString(-1 << 63), want: "-9223372036854775808"},
	}

	for _, tc := range cases {
		if tc.got != tc.want {
			t.Fatalf("%s = %q, want %q", tc.name, tc.got, tc.want)
		}
	}
}
