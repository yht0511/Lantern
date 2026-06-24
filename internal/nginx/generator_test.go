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
