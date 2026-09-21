package auth

import (
	"path/filepath"
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestUnitQuotesConfigPath(t *testing.T) {
	cfg := &model.Config{}
	cfg.Settings.FRP.SystemdDir = t.TempDir()
	path, body, err := Unit(cfg, filepath.Join(t.TempDir(), "Lantern config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(path, UnitName) || !strings.Contains(body, `auth serve --config "`) || !strings.Contains(body, "UMask=0077") {
		t.Fatalf("invalid auth unit: %s %s", path, body)
	}
}
