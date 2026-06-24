package planner

import (
	"fmt"
	"net"
	"sort"
	"strings"

	"lantern/internal/model"
)

type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

type Diagnostic struct {
	Severity Severity
	Message  string
}

type Action struct {
	Kind      string
	Operation string
	Target    string
	Details   string
}

type Plan struct {
	Diagnostics []Diagnostic
	Actions     []Action
}

func (p Plan) HasErrors() bool {
	for _, d := range p.Diagnostics {
		if d.Severity == SeverityError {
			return true
		}
	}
	return false
}

func Build(cfg *model.Config) Plan {
	cfg.ApplyDefaults()
	b := builder{cfg: cfg}
	b.validateSettings()
	b.validateDomains()
	b.validateServices()
	b.validateExits()
	b.validateBindings()
	b.planNginx()
	b.planFRP()
	b.planACME()
	b.planDNS()
	return Plan{Diagnostics: b.diagnostics, Actions: b.actions}
}

type builder struct {
	cfg         *model.Config
	diagnostics []Diagnostic
	actions     []Action
}

func (b *builder) diag(sev Severity, format string, args ...any) {
	b.diagnostics = append(b.diagnostics, Diagnostic{Severity: sev, Message: fmt.Sprintf(format, args...)})
}

func (b *builder) action(kind, op, target, details string) {
	b.actions = append(b.actions, Action{Kind: kind, Operation: op, Target: target, Details: details})
}

func (b *builder) validateSettings() {
	if b.cfg.Settings.SecretsPath == "" {
		b.diag(SeverityError, "settings.secrets_path is required")
	}
	if b.cfg.Settings.Nginx.GeneratedDir == "" {
		b.diag(SeverityError, "settings.nginx.generated_dir is required")
	}
	switch b.cfg.Settings.ACME.Provider {
	case "lego", "acme.sh":
	default:
		b.diag(SeverityError, "settings.acme.provider %q is not supported", b.cfg.Settings.ACME.Provider)
	}
	if b.cfg.Settings.ACME.Provider == "acme.sh" && b.cfg.Settings.ACME.ACMEShPath == "" {
		b.diag(SeverityError, "settings.acme.acme_sh_path is required for acme.sh")
	}
}

func (b *builder) validateDomains() {
	seen := map[string]bool{}
	for _, d := range b.cfg.Domains {
		if d.Name == "" {
			b.diag(SeverityError, "domain name is required")
			continue
		}
		if seen[d.Name] {
			b.diag(SeverityError, "duplicate domain %q", d.Name)
		}
		seen[d.Name] = true
		if d.Provider != "" && d.Provider != "cloudflare" {
			b.diag(SeverityWarn, "domain %q uses unsupported provider %q; only Cloudflare is implemented now", d.Name, d.Provider)
		}
		if d.Provider == "cloudflare" {
			if d.ZoneRef == "" && d.ZoneID == "" {
				b.diag(SeverityWarn, "domain %q has empty Cloudflare zone_ref", d.Name)
			}
			if d.TokenRef == "" {
				b.diag(SeverityWarn, "domain %q has empty token_ref", d.Name)
			}
		}
	}
}

func (b *builder) validateServices() {
	seen := map[string]bool{}
	for _, s := range b.cfg.Services {
		if s.Name == "" {
			b.diag(SeverityError, "service name is required")
			continue
		}
		if seen[s.Name] {
			b.diag(SeverityError, "duplicate service %q", s.Name)
		}
		seen[s.Name] = true
		if !validProtocol(s.Protocol) {
			b.diag(SeverityError, "service %q has invalid protocol %q", s.Name, s.Protocol)
		}
		if net.ParseIP(s.Host) == nil && s.Host == "" {
			b.diag(SeverityError, "service %q requires host", s.Name)
		}
		if s.Port <= 0 || s.Port > 65535 {
			b.diag(SeverityError, "service %q has invalid port %d", s.Name, s.Port)
		}
	}
}

func (b *builder) validateExits() {
	domains := domainSet(b.cfg)
	seen := map[string]bool{}
	for _, e := range b.cfg.Exits {
		if e.Name == "" {
			b.diag(SeverityError, "exit name is required")
			continue
		}
		if seen[e.Name] {
			b.diag(SeverityError, "duplicate exit %q", e.Name)
		}
		seen[e.Name] = true
		if e.Type != "direct" && e.Type != "frp" {
			b.diag(SeverityError, "exit %q has invalid type %q", e.Name, e.Type)
		}
		if e.Domain == "" {
			b.diag(SeverityError, "exit %q requires domain", e.Name)
		} else if !domains[e.Domain] {
			b.diag(SeverityWarn, "exit %q references domain %q that is not in domains list", e.Name, e.Domain)
		}
		if len(e.DNSTargets) == 0 {
			b.diag(SeverityWarn, "exit %q has no dns_targets", e.Name)
		}
		for _, target := range e.DNSTargets {
			switch target.Type {
			case "A", "AAAA", "CNAME":
			default:
				b.diag(SeverityError, "exit %q has unsupported DNS target type %q", e.Name, target.Type)
			}
			if target.Value == "" {
				b.diag(SeverityError, "exit %q has empty DNS target value", e.Name)
			}
		}
		if e.Type == "frp" {
			if e.FRP.ServerAddr == "" {
				b.diag(SeverityError, "frp exit %q requires frp.server_addr", e.Name)
			}
			if e.FRP.TokenRef == "" {
				b.diag(SeverityWarn, "frp exit %q has empty token_ref", e.Name)
			}
		}
	}
}

func (b *builder) validateBindings() {
	services := serviceMap(b.cfg)
	exits := exitMap(b.cfg)
	names := map[string]bool{}
	hosts := map[string]string{}
	ports := map[string]string{}
	for _, binding := range b.cfg.Bindings {
		if binding.Disabled {
			continue
		}
		if binding.Name == "" {
			b.diag(SeverityError, "binding name is required")
			continue
		}
		if names[binding.Name] {
			b.diag(SeverityError, "duplicate binding %q", binding.Name)
		}
		names[binding.Name] = true
		service, ok := services[binding.Service]
		if !ok {
			b.diag(SeverityError, "binding %q references missing service %q", binding.Name, binding.Service)
			continue
		}
		exit, ok := exits[binding.Exit]
		if !ok {
			b.diag(SeverityError, "binding %q references missing exit %q", binding.Name, binding.Exit)
			continue
		}
		if binding.Hostname == "" {
			b.diag(SeverityError, "binding %q requires hostname", binding.Name)
		} else {
			if other := hosts[binding.Hostname]; other != "" {
				b.diag(SeverityError, "hostname %q is used by both %q and %q", binding.Hostname, other, binding.Name)
			}
			hosts[binding.Hostname] = binding.Name
		}
		if service.Protocol == "tcp" || service.Protocol == "udp" {
			if binding.ExternalPort == 0 && exit.Type == "frp" {
				b.diag(SeverityError, "TCP/UDP frp binding %q requires external_port", binding.Name)
			}
			if binding.ExternalPort != 0 {
				key := fmt.Sprintf("%s/%d/%s", exit.Name, binding.ExternalPort, service.Protocol)
				if other := ports[key]; other != "" {
					b.diag(SeverityError, "external port conflict on %s between %q and %q", key, other, binding.Name)
				}
				ports[key] = binding.Name
			}
			if binding.Proxied {
				b.diag(SeverityWarn, "binding %q is %s but proxied=true; Cloudflare orange cloud only applies to HTTP(S) records", binding.Name, service.Protocol)
			}
		} else if binding.ExternalPort != 0 {
			b.diag(SeverityWarn, "binding %q is %s; external_port=%d is ignored because HTTP(S) frp bindings use exit.frp.remote_https_port", binding.Name, service.Protocol, binding.ExternalPort)
		}
	}
}

func (b *builder) planNginx() {
	count := 0
	for _, binding := range b.cfg.Bindings {
		if binding.Disabled {
			continue
		}
		service, ok := serviceMap(b.cfg)[binding.Service]
		if !ok {
			continue
		}
		if service.Protocol == "http" || service.Protocol == "https" {
			count++
			mode := "HTTP"
			if binding.SSL {
				mode = "HTTPS"
			}
			b.action("nginx", "render", binding.Hostname, fmt.Sprintf("%s proxy to %s://%s:%d", mode, service.Protocol, service.Host, service.Port))
		}
	}
	if count > 0 {
		b.action("nginx", "reload", b.cfg.Settings.Nginx.ReloadCommand, "after nginx -t succeeds")
	}
}

func (b *builder) planFRP() {
	exits := exitMap(b.cfg)
	services := serviceMap(b.cfg)
	frpExits := map[string]bool{}
	httpsProxyPlanned := map[string]bool{}
	for _, binding := range b.cfg.Bindings {
		if binding.Disabled {
			continue
		}
		exit, ok := exits[binding.Exit]
		if !ok || exit.Type != "frp" {
			continue
		}
		frpExits[exit.Name] = true
		service := services[binding.Service]
		if service.Protocol == "http" || service.Protocol == "https" {
			if !httpsProxyPlanned[exit.Name] {
				b.action("frp", "proxy", exit.Name, fmt.Sprintf("remote tcp %d -> local 127.0.0.1:%d for HTTPS gateway", exit.FRP.RemoteHTTPSPort, exit.FRP.LocalHTTPSPort))
				httpsProxyPlanned[exit.Name] = true
			}
		} else {
			b.action("frp", "proxy", binding.Name, fmt.Sprintf("remote %s %d -> %s:%d", service.Protocol, binding.ExternalPort, service.Host, service.Port))
		}
	}
	names := make([]string, 0, len(frpExits))
	for name := range frpExits {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		b.action("frp", "render", name, "generate frpc config and systemd unit on this host")
	}
}

func (b *builder) planACME() {
	if !b.cfg.Settings.ACME.Enabled {
		b.diag(SeverityInfo, "ACME is disabled; generated nginx SSL paths must already exist")
		return
	}
	seen := map[string]bool{}
	for _, binding := range b.cfg.Bindings {
		if binding.Disabled || !binding.SSL {
			continue
		}
		certName := binding.CertName
		if certName == "" {
			certName = binding.Hostname
		}
		if seen[certName] {
			continue
		}
		seen[certName] = true
		b.action("acme", "ensure", certName, fmt.Sprintf("provider=%s email=%s", b.cfg.Settings.ACME.Provider, b.cfg.Settings.ACME.Email))
	}
}

func (b *builder) planDNS() {
	seen := map[string]string{}
	for _, binding := range b.cfg.Bindings {
		if binding.Disabled {
			continue
		}
		exit, ok := exitMap(b.cfg)[binding.Exit]
		if !ok {
			continue
		}
		for _, target := range exit.DNSTargets {
			key := strings.Join([]string{binding.Hostname, target.Type}, "|")
			value := target.Value
			if old := seen[key]; old != "" && old != value {
				b.diag(SeverityError, "desired DNS conflict for %s %s: %q vs %q", target.Type, binding.Hostname, old, value)
			}
			seen[key] = value
			b.action("dns", "ensure", fmt.Sprintf("%s %s", target.Type, binding.Hostname), fmt.Sprintf("content=%s proxied=%v", target.Value, binding.Proxied))
		}
	}
}

func validProtocol(protocol string) bool {
	switch protocol {
	case "http", "https", "tcp", "udp":
		return true
	default:
		return false
	}
}

func serviceMap(cfg *model.Config) map[string]model.Service {
	out := map[string]model.Service{}
	for _, s := range cfg.Services {
		out[s.Name] = s
	}
	return out
}

func exitMap(cfg *model.Config) map[string]model.Exit {
	out := map[string]model.Exit{}
	for _, e := range cfg.Exits {
		out[e.Name] = e
	}
	return out
}

func domainSet(cfg *model.Config) map[string]bool {
	out := map[string]bool{}
	for _, d := range cfg.Domains {
		out[d.Name] = true
	}
	return out
}
