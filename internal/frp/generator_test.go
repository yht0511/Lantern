package frp

import (
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestGenerateFRP(t *testing.T) {
	cfg := model.ExampleConfig()
	secrets := model.ExampleSecrets()
	files, err := Generate(cfg, secrets)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, body := range files {
		if strings.Contains(body, "serverAddr = \"site-1.teclab.org.cn\"") &&
			strings.Contains(body, "remotePort = 8443") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected frp config, got %#v", files)
	}
}
