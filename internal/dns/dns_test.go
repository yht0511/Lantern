package dns

import (
	"testing"

	"lantern/internal/cloudflare"
)

func TestRedactRef(t *testing.T) {
	if got := redactRef("short"); got != "***" {
		t.Fatalf("short ref redaction = %q", got)
	}
	if got := redactRef("abcdefghijklmnop"); got != "abcd...mnop" {
		t.Fatalf("long ref redaction = %q", got)
	}
}

func TestSplitRecordsFindsAddressCNAMEConflicts(t *testing.T) {
	records := []cloudflare.Record{
		{Type: "A", Name: "app.example.com", Content: "192.0.2.1"},
		{Type: "TXT", Name: "app.example.com", Content: "keep"},
		{Type: "CNAME", Name: "app.example.com", Content: "target.example.com"},
	}
	same, conflicts := splitRecords(records, "CNAME")
	if len(same) != 1 || same[0].Type != "CNAME" {
		t.Fatalf("same records = %#v", same)
	}
	if len(conflicts) != 1 || conflicts[0].Type != "A" {
		t.Fatalf("conflicts = %#v", conflicts)
	}
}
