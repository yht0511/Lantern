package config

import (
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
