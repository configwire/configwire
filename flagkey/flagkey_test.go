package flagkey

import (
	"strings"
	"testing"
)

func TestCheckBoundaries(t *testing.T) {
	if err := Check(""); err == nil {
		t.Fatal("empty key must fail")
	}
	if err := Check(strings.Repeat("f", MaxLen)); err != nil {
		t.Fatalf("key at exactly MaxLen must pass, got %v", err)
	}
	if err := Check(strings.Repeat("f", MaxLen+1)); err == nil {
		t.Fatalf("key at MaxLen+1 must fail")
	}
}

func TestCheckShape(t *testing.T) {
	for _, bad := range []string{"has space", "9starts-digit", "-starts-dash", ".starts-dot", "has/slash", "ünïcode"} {
		if err := Check(bad); err == nil {
			t.Fatalf("shape %q must fail", bad)
		}
	}
	for _, ok := range []string{"a", "launch_flag", "_x", "A1.-_b"} {
		if err := Check(ok); err != nil {
			t.Fatalf("shape %q must pass, got %v", ok, err)
		}
	}
}
