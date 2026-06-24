package frp

import (
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestInstallScriptRequiresExplicitVersionForLatest(t *testing.T) {
	cfg := model.ExampleConfig()
	plan := GenerateInstallPlan(cfg, "")
	if !strings.Contains(plan.Script, "FRP_VERSION is required") {
		t.Fatalf("expected latest script to require FRP_VERSION, got:\n%s", plan.Script)
	}
}

func TestSystemdUnitPathUsesSystemdDir(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Settings.FRP.SystemdDir = "/tmp/systemd"
	files, err := Generate(cfg, model.ExampleSecrets())
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for path := range files {
		if strings.HasPrefix(path, "/tmp/systemd/") && strings.HasSuffix(path, ".service") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected service under systemd dir, got %#v", files)
	}
}
