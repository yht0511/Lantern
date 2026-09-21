package acme

import (
	"context"
	"os"
	"path/filepath"
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

func TestACMEShUsesCredentialsForEachZone(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fake-acme.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s|%s\\n' \"$CF_Token\" \"$CF_Zone_ID\" > \"$1\"\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CF_Zone_ID", "stale-zone")
	secrets := &model.Secrets{
		CloudflareTokens: map[string]string{"user_a": "token-a", "user_b": "token-b"},
		CloudflareZones:  map[string]string{"zone_a": "zone-a", "zone_b": "zone-b"},
	}
	for _, tc := range []struct {
		name, tokenRef, zoneRef, want string
	}{
		{"a.example.com", "user_a", "zone_a", "token-a|zone-a\n"},
		{"b.example.com", "user_b", "zone_b", "token-b|zone-b\n"},
	} {
		cert := Certificate{Name: tc.name, Provider: "acme.sh", ACMEShPath: script, TokenRef: tc.tokenRef, Zone: model.Domain{ZoneRef: tc.zoneRef}}
		credentials, err := CredentialsFor(cert, secrets)
		if err != nil {
			t.Fatal(err)
		}
		output := filepath.Join(dir, tc.name+".txt")
		if err := RunProvider(context.Background(), cert, credentials, []string{output}); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(output)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != tc.want {
			t.Fatalf("%s credentials = %q, want %q", tc.name, got, tc.want)
		}
		preview := ShellCommand(cert, []string{"--renew"})
		if !strings.Contains(preview, "CF_Token=") || !strings.Contains(preview, "CF_Zone_ID=") {
			t.Fatalf("preview omitted Cloudflare credentials: %s", preview)
		}
	}
}

func TestACMEShRequiresZoneID(t *testing.T) {
	cert := Certificate{Name: "example.com", Provider: "acme.sh", TokenRef: "user_a", Zone: model.Domain{ZoneRef: "zone_a"}}
	_, err := CredentialsFor(cert, &model.Secrets{CloudflareTokens: map[string]string{"user_a": "token-a"}})
	if err == nil || !strings.Contains(err.Error(), "zone_a") {
		t.Fatalf("expected missing zone ID error, got %v", err)
	}
	cert.Zone.ZoneID = "inline-zone"
	credentials, err := CredentialsFor(cert, &model.Secrets{CloudflareTokens: map[string]string{"user_a": "token-a"}})
	if err != nil || credentials.ZoneID != "inline-zone" {
		t.Fatalf("inline zone ID fallback = %#v, %v", credentials, err)
	}
}

func TestCertificateAcrossCloudflareUsersIsRejected(t *testing.T) {
	cfg := &model.Config{
		Settings: model.Settings{ACME: model.ACMESettings{Enabled: true, Provider: "acme.sh"}},
		Domains: []model.Domain{
			{Name: "a.example.com", TokenRef: "user_a", ZoneRef: "zone_a", AllowACME: true},
			{Name: "b.example.com", TokenRef: "user_b", ZoneRef: "zone_b", AllowACME: true},
		},
		Bindings: []model.Binding{
			{Hostname: "a.example.com", CertName: "shared", SSL: true},
			{Hostname: "b.example.com", CertName: "shared", SSL: true},
		},
	}
	certs, diagnostics := DesiredCertificates(cfg)
	if len(certs) != 0 {
		t.Fatalf("expected no certificate with mixed credentials, got %#v", certs)
	}
	if len(diagnostics) != 1 || diagnostics[0].Severity != "error" || !strings.Contains(diagnostics[0].Message, "different credentials") {
		t.Fatalf("expected mixed credentials diagnostic, got %#v", diagnostics)
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
