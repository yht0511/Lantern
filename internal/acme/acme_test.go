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

func TestACMEShUsesExistingDirectoryLayout(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Settings.ACME.Enabled = true
	cfg.Settings.ACME.Provider = "acme.sh"
	cfg.Settings.ACME.CertDir = "/root/.acme.sh"
	cfg.Settings.ACME.ACMEShECC = true
	cfg.Bindings[1].CertName = "site-2.teclab.org.cn"

	certs, diagnostics := DesiredCertificates(cfg)
	for _, diag := range diagnostics {
		if diag.Severity == "error" {
			t.Fatalf("unexpected diagnostic: %#v", diag)
		}
	}
	var found Certificate
	for _, cert := range certs {
		if cert.Name == "site-2.teclab.org.cn" {
			found = cert
			break
		}
	}
	if found.Name == "" {
		t.Fatalf("site-2 cert not planned: %#v", certs)
	}
	if got := strings.Join(found.Domains, ","); got != "site-2.teclab.org.cn,*.site-2.teclab.org.cn" {
		t.Fatalf("unexpected domains: %s", got)
	}
	if found.CertFile != "/root/.acme.sh/site-2.teclab.org.cn_ecc/fullchain.cer" {
		t.Fatalf("unexpected cert file: %s", found.CertFile)
	}
	if found.KeyFile != "/root/.acme.sh/site-2.teclab.org.cn_ecc/site-2.teclab.org.cn.key" {
		t.Fatalf("unexpected key file: %s", found.KeyFile)
	}
}
