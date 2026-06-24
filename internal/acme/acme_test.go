package acme

import (
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestDesiredCertificatesUsesLegoPaths(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Settings.ACME.Enabled = true
	certs, diagnostics := DesiredCertificates(cfg)
	for _, diag := range diagnostics {
		if diag.Severity == "error" {
			t.Fatalf("unexpected diagnostic: %#v", diag)
		}
	}
	if len(certs) == 0 {
		t.Fatal("expected certificates")
	}
	for _, cert := range certs {
		if !strings.Contains(cert.CertFile, "/certificates/") || !strings.HasSuffix(cert.KeyFile, ".key") {
			t.Fatalf("unexpected cert paths: %#v", cert)
		}
	}
}
