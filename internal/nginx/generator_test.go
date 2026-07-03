package nginx

import (
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestGenerateNginx(t *testing.T) {
	cfg := model.ExampleConfig()
	files, err := Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, body := range files {
		if strings.Contains(body, "server_name bitshare.lan.site-2.teclab.org.cn;") &&
			strings.Contains(body, "proxy_pass http://192.168.1.244:13830;") &&
			strings.Contains(body, "/certificates/bitshare.lan.site-2.teclab.org.cn.pem") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected bitshare nginx config, got %#v", files)
	}
}

func TestGenerateWebsocketMap(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Services[0].Options.Websocket = true
	files, err := Generate(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var hasMap bool
	var hasUpgradeHeader bool
	for path, body := range files {
		if strings.HasSuffix(path, "00_lantern_websocket_map.conf") &&
			strings.Contains(body, "map $http_upgrade $connection_upgrade") {
			hasMap = true
		}
		if strings.Contains(body, "proxy_set_header Connection $connection_upgrade;") {
			hasUpgradeHeader = true
		}
	}
	if !hasMap || !hasUpgradeHeader {
		t.Fatalf("missing websocket config: map=%v upgrade=%v files=%#v", hasMap, hasUpgradeHeader, files)
	}
}
