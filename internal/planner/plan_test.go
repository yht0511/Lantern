package planner

import (
	"strings"
	"testing"

	"lantern/internal/model"
)

func TestHTTPExternalPortWarns(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Bindings[0].ExternalPort = 9999
	p := Build(cfg)
	if !hasDiagnostic(p, SeverityWarn, "external_port=9999 is ignored") {
		t.Fatalf("expected ignored external_port warning, got %#v", p.Diagnostics)
	}
}

func TestTCPFRPRequiresExternalPort(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Services = append(cfg.Services, model.Service{
		Name:     "ssh",
		Protocol: "tcp",
		Host:     "192.168.1.4",
		Port:     22,
	})
	cfg.Bindings = append(cfg.Bindings, model.Binding{
		Name:     "ssh-public",
		Service:  "ssh",
		Exit:     "site1-frp",
		Hostname: "ssh.site-2.teclab.org.cn",
	})
	p := Build(cfg)
	if !hasDiagnostic(p, SeverityError, "requires external_port") {
		t.Fatalf("expected required external_port error, got %#v", p.Diagnostics)
	}
}

func TestAuthRequiresHTTPSAndHTTPService(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Bindings[0].AuthRef = "private_site"
	cfg.Bindings[0].SSL = false
	cfg.Settings.Auth.PublicURL = "https://auth.example.test"
	p := Build(cfg)
	if !hasDiagnostic(p, SeverityError, "requires ssl=true for auth") {
		t.Fatalf("expected HTTPS auth diagnostic, got %#v", p.Diagnostics)
	}

	cfg.Bindings[0].SSL = true
	cfg.Services[0].Protocol = "tcp"
	p = Build(cfg)
	if !hasDiagnostic(p, SeverityError, "can only use auth with HTTP(S)") {
		t.Fatalf("expected protocol auth diagnostic, got %#v", p.Diagnostics)
	}
}

func TestAuthRejectsUnsafePublicOrigin(t *testing.T) {
	cfg := model.ExampleConfig()
	cfg.Bindings[0].AuthRef = "private_site"
	cfg.Settings.Auth.PublicURL = "https://auth.example.test/#fragment"
	p := Build(cfg)
	if !hasDiagnostic(p, SeverityError, "must be an HTTPS origin") {
		t.Fatalf("expected invalid auth origin diagnostic, got %#v", p.Diagnostics)
	}
}

func hasDiagnostic(p Plan, severity Severity, needle string) bool {
	for _, d := range p.Diagnostics {
		if d.Severity == severity && strings.Contains(d.Message, needle) {
			return true
		}
	}
	return false
}
