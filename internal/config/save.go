package config

import (
	"fmt"
	"sort"
	"strings"

	"lantern/internal/model"
)

func encodeConfig(cfg *model.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "version: %d\n", cfg.Version)
	b.WriteString("settings:\n")
	writeKV(&b, 2, "state_dir", cfg.Settings.StateDir)
	writeKV(&b, 2, "secrets_path", cfg.Settings.SecretsPath)
	b.WriteString("  nginx:\n")
	writeKV(&b, 4, "generated_dir", cfg.Settings.Nginx.GeneratedDir)
	writeKV(&b, 4, "test_command", cfg.Settings.Nginx.TestCommand)
	writeKV(&b, 4, "reload_command", cfg.Settings.Nginx.ReloadCommand)
	b.WriteString("  acme:\n")
	writeBool(&b, 4, "enabled", cfg.Settings.ACME.Enabled)
	writeKV(&b, 4, "provider", cfg.Settings.ACME.Provider)
	writeKV(&b, 4, "email", cfg.Settings.ACME.Email)
	writeKV(&b, 4, "cert_dir", cfg.Settings.ACME.CertDir)
	writeKV(&b, 4, "acme_sh_path", cfg.Settings.ACME.ACMEShPath)
	writeBool(&b, 4, "acme_sh_ecc", cfg.Settings.ACME.ACMEShECC)
	writeKV(&b, 4, "acme_sh_dns", cfg.Settings.ACME.ACMEShDNS)
	writeKV(&b, 4, "acme_sh_server", cfg.Settings.ACME.ACMEShServer)
	b.WriteString("  frp:\n")
	writeBool(&b, 4, "enabled", cfg.Settings.FRP.Enabled)
	writeKV(&b, 4, "install_dir", cfg.Settings.FRP.InstallDir)
	writeKV(&b, 4, "config_dir", cfg.Settings.FRP.ConfigDir)
	writeKV(&b, 4, "systemd_dir", cfg.Settings.FRP.SystemdDir)
	writeKV(&b, 4, "service_prefix", cfg.Settings.FRP.ServicePrefix)
	writeBool(&b, 4, "manage_systemd", cfg.Settings.FRP.ManageSystemd)
	writeKV(&b, 4, "reload_command", cfg.Settings.FRP.ReloadCommand)
	b.WriteString("  cloudflare:\n")
	writeBool(&b, 4, "enabled", cfg.Settings.Cloudflare.Enabled)
	writeKV(&b, 4, "conflict_policy", cfg.Settings.Cloudflare.ConflictPolicy)

	b.WriteString("domains:\n")
	for _, d := range cfg.Domains {
		b.WriteString("  - name: " + quote(d.Name) + "\n")
		writeKV(&b, 4, "provider", d.Provider)
		if d.ZoneRef != "" {
			writeKV(&b, 4, "zone_ref", d.ZoneRef)
		} else {
			writeKV(&b, 4, "zone_id", d.ZoneID)
		}
		writeKV(&b, 4, "token_ref", d.TokenRef)
		writeBool(&b, 4, "default_proxied", d.DefaultProxied)
		writeBool(&b, 4, "allow_dns_updates", d.AllowDNSUpdates)
		writeBool(&b, 4, "allow_acme", d.AllowACME)
	}

	b.WriteString("services:\n")
	for _, s := range cfg.Services {
		b.WriteString("  - name: " + quote(s.Name) + "\n")
		writeKV(&b, 4, "protocol", s.Protocol)
		writeKV(&b, 4, "host", s.Host)
		writeInt(&b, 4, "port", s.Port)
		b.WriteString("    options:\n")
		writeBool(&b, 6, "websocket", s.Options.Websocket)
		writeKV(&b, 6, "backend_tls_verify", s.Options.BackendTLSVerify)
		writeKV(&b, 6, "connect_timeout", s.Options.ConnectTimeout)
		writeKV(&b, 6, "send_timeout", s.Options.SendTimeout)
		writeKV(&b, 6, "read_timeout", s.Options.ReadTimeout)
		writeKV(&b, 6, "client_max_body_size", s.Options.ClientMaxBodySize)
		writeKV(&b, 6, "buffering", s.Options.Buffering)
		writeKV(&b, 6, "request_buffering", s.Options.RequestBuffering)
		writeKV(&b, 6, "range_mode", s.Options.RangeMode)
	}

	b.WriteString("exits:\n")
	for _, e := range cfg.Exits {
		b.WriteString("  - name: " + quote(e.Name) + "\n")
		writeKV(&b, 4, "type", e.Type)
		writeKV(&b, 4, "domain", e.Domain)
		b.WriteString("    dns_targets:\n")
		for _, t := range e.DNSTargets {
			b.WriteString("      - type: " + quote(t.Type) + "\n")
			writeKV(&b, 8, "value", t.Value)
		}
		if e.Type == "frp" || e.FRP.ServerAddr != "" {
			b.WriteString("    frp:\n")
			writeKV(&b, 6, "server_addr", e.FRP.ServerAddr)
			writeInt(&b, 6, "server_port", e.FRP.ServerPort)
			writeKV(&b, 6, "token_ref", e.FRP.TokenRef)
			writeBool(&b, 6, "tls", e.FRP.TLS)
			writeInt(&b, 6, "remote_https_port", e.FRP.RemoteHTTPSPort)
			writeInt(&b, 6, "local_https_port", e.FRP.LocalHTTPSPort)
		}
	}

	b.WriteString("bindings:\n")
	for _, binding := range cfg.Bindings {
		b.WriteString("  - name: " + quote(binding.Name) + "\n")
		writeKV(&b, 4, "service", binding.Service)
		writeKV(&b, 4, "exit", binding.Exit)
		writeKV(&b, 4, "hostname", binding.Hostname)
		writeBool(&b, 4, "ssl", binding.SSL)
		writeBool(&b, 4, "proxied", binding.Proxied)
		writeInt(&b, 4, "external_port", binding.ExternalPort)
		writeKV(&b, 4, "cert_name", binding.CertName)
		writeBool(&b, 4, "disabled", binding.Disabled)
	}
	return b.String()
}

func encodeSecrets(secrets *model.Secrets) string {
	var b strings.Builder
	b.WriteString("cloudflare_zones:\n")
	writeSortedMap(&b, secrets.CloudflareZones)
	b.WriteString("cloudflare_tokens:\n")
	writeSortedMap(&b, secrets.CloudflareTokens)
	b.WriteString("frp_tokens:\n")
	writeSortedMap(&b, secrets.FRPTokens)
	return b.String()
}

func writeSortedMap(b *strings.Builder, values map[string]string) {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		writeKV(b, 2, k, values[k])
	}
}

func writeKV(b *strings.Builder, indent int, key, value string) {
	fmt.Fprintf(b, "%s%s: %s\n", strings.Repeat(" ", indent), key, quote(value))
}

func writeBool(b *strings.Builder, indent int, key string, value bool) {
	fmt.Fprintf(b, "%s%s: %v\n", strings.Repeat(" ", indent), key, value)
}

func writeInt(b *strings.Builder, indent int, key string, value int) {
	fmt.Fprintf(b, "%s%s: %d\n", strings.Repeat(" ", indent), key, value)
}

func quote(s string) string {
	if s == "" {
		return `""`
	}
	needs := strings.ContainsAny(s, ":#{}[]&,*?|-<>=!%@`'\" \t") || strings.HasPrefix(s, "0") || s == "true" || s == "false"
	if !needs {
		return s
	}
	return fmt.Sprintf("%q", s)
}
