package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestEncodeConfigUsesZoneRef(t *testing.T) {
	cfg := model.ExampleConfig()
	out := encodeConfig(cfg)
	if !strings.Contains(out, "zone_ref: cf_teclab_zone") {
		t.Fatalf("expected zone_ref in encoded config:\n%s", out)
	}
	if strings.Contains(out, "zone_id:") {
		t.Fatalf("encoded config should not contain zone_id:\n%s", out)
	}
}

func TestDecodeSecretsCloudflareZones(t *testing.T) {
	root, err := parseYAML([]byte(`
cloudflare_zones:
  cf_teclab_zone: zone-id
cloudflare_tokens:
  cf_teclab: token
frp_tokens:
  frp_site1: frp-token
`))
	if err != nil {
		t.Fatal(err)
	}
	secrets := decodeSecrets(root)
	if got := secrets.CloudflareZones["cf_teclab_zone"]; got != "zone-id" {
		t.Fatalf("cloudflare zone = %q", got)
	}
}

func TestDecodeServiceNormalizesLegacyBoolOptions(t *testing.T) {
	root, err := parseYAML([]byte(`
services:
  - name: pve
    protocol: https
    host: 192.168.1.2
    port: 8006
    options:
      backend_tls_verify: false
      buffering: false
      request_buffering: true
`))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := decodeConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := cfg.Services[0].Options.BackendTLSVerify; got != "off" {
		t.Fatalf("backend_tls_verify = %q", got)
	}
	if got := cfg.Services[0].Options.Buffering; got != "off" {
		t.Fatalf("buffering = %q", got)
	}
	if got := cfg.Services[0].Options.RequestBuffering; got != "on" {
		t.Fatalf("request_buffering = %q", got)
	}
}

func TestAuthConfigAndSecretsRoundTrip(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Settings.Auth.PublicURL = "https://auth.example.test:10043"
	cfg.Settings.Auth.Listen = "127.0.0.1:9183"
	cfg.Bindings[0].AuthRef = "private_site"
	root, err := parseYAML([]byte(encodeConfig(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := decodeConfig(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Settings.Auth.PublicURL != cfg.Settings.Auth.PublicURL || loaded.Bindings[0].AuthRef != "private_site" {
		t.Fatalf("auth config round trip failed: %#v %#v", loaded.Settings.Auth, loaded.Bindings[0])
	}
	secrets := &model.Secrets{AuthPasswords: map[string]string{"private_site": "$argon2id$example"}}
	secretRoot, err := parseYAML([]byte(encodeSecrets(secrets)))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeSecrets(secretRoot).AuthPasswords["private_site"]; got != "$argon2id$example" {
		t.Fatalf("auth password hash round trip = %q", got)
	}
}

func TestQuotedAuthPasswordKey(t *testing.T) {
	root, err := parseYAML([]byte("auth_passwords:\n  \"pve-lan\": \"$argon2id$example\"\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeSecrets(root).AuthPasswords["pve-lan"]; got != "$argon2id$example" {
		t.Fatalf("quoted auth key was not decoded: %q", got)
	}
}

func TestSaveSecretsReplacesInsecureFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := SaveSecrets(path, &model.Secrets{AuthPasswords: map[string]string{"site": "hash"}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("secrets permissions = %o", got)
	}
	secrets, err := LoadSecrets(path)
	if err != nil || secrets.AuthPasswords["site"] != "hash" {
		t.Fatalf("saved secrets = %#v, %v", secrets, err)
	}
}
