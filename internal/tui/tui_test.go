package tui

import "testing"

func TestParseTargets(t *testing.T) {
	targets := parseTargets("A=auto:public-ipv4, AAAA=auto:public-ipv6, CNAME=site-1.example.com")
	if len(targets) != 3 {
		t.Fatalf("expected 3 targets, got %#v", targets)
	}
	if targets[0].Type != "A" || targets[0].Value != "auto:public-ipv4" {
		t.Fatalf("unexpected first target: %#v", targets[0])
	}
}

func TestEncodeTargets(t *testing.T) {
	raw := encodeTargets(parseTargets("A=192.0.2.1,AAAA=2001:db8::1"))
	if raw != "A=192.0.2.1,AAAA=2001:db8::1" {
		t.Fatalf("unexpected encoding: %s", raw)
	}
}

func TestInverseFitKeepsReset(t *testing.T) {
	rendered := inverse(fit("this is a very long selected row", 10))
	if rendered != "\x1b[7mthis is a>\x1b[0m" {
		t.Fatalf("unexpected rendered string: %q", rendered)
	}
}

func TestFitStripsANSI(t *testing.T) {
	rendered := fit(inverse("abcdef"), 4)
	if rendered != "abc>" {
		t.Fatalf("unexpected fit string: %q", rendered)
	}
}

func TestInputViewportShortInput(t *testing.T) {
	visible, cursor := inputViewport("abc", 3, 6)
	if visible != "abc   " || cursor != 3 {
		t.Fatalf("unexpected viewport %q cursor %d", visible, cursor)
	}
}

func TestInputViewportLongInput(t *testing.T) {
	visible, cursor := inputViewport("abcdefghijklmnopqrstuvwxyz", 26, 10)
	if visible != ">rstuvwxyz" || cursor != 10 {
		t.Fatalf("unexpected viewport %q cursor %d", visible, cursor)
	}
}

func TestInputViewportMiddleCursor(t *testing.T) {
	visible, cursor := inputViewport("abcdefghijklmnopqrstuvwxyz", 5, 10)
	if visible != "abcdefghi<" || cursor != 5 {
		t.Fatalf("unexpected viewport %q cursor %d", visible, cursor)
	}
}

func TestInputViewportScrolledCursor(t *testing.T) {
	visible, cursor := inputViewport("abcdefghijklmnopqrstuvwxyz", 13, 10)
	if visible != ">fghijklm<" || cursor != 9 {
		t.Fatalf("unexpected viewport %q cursor %d", visible, cursor)
	}
}
