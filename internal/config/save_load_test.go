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
