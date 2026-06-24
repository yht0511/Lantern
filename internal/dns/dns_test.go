package dns

import "testing"

func TestRedactRef(t *testing.T) {
	if got := redactRef("short"); got != "***" {
		t.Fatalf("short ref redaction = %q", got)
	}
	if got := redactRef("abcdefghijklmnop"); got != "abcd...mnop" {
		t.Fatalf("long ref redaction = %q", got)
	}
}
