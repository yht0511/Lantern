package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"lantern/internal/model"
)

func Load(path string) (*model.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := parseYAML(data)
	if err != nil {
		return nil, err
	}
	cfg, err := decodeConfig(root)
	if err != nil {
		return nil, err
	}
	cfg.ApplyDefaults()
	return cfg, nil
}

func Save(path string, cfg *model.Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	cfg.ApplyDefaults()
	return os.WriteFile(path, []byte(encodeConfig(cfg)), 0o644)
}

func LoadSecrets(path string) (*model.Secrets, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	root, err := parseYAML(data)
	if err != nil {
		return nil, err
	}
	return decodeSecrets(root), nil
}

func SaveSecrets(path string, secrets *model.Secrets) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	return os.WriteFile(path, []byte(encodeSecrets(secrets)), 0o600)
}

func decodeConfig(root map[string]any) (*model.Config, error) {
	cfg := &model.Config{}
	cfg.Version = intValue(root["version"])

	if settings, ok := mapValue(root["settings"]); ok {
		cfg.Settings = decodeSettings(settings)
	}
	if domains, ok := sliceValue(root["domains"]); ok {
		for _, item := range domains {
			m, ok := mapValue(item)
			if !ok {
				return nil, errors.New("domains entries must be maps")
			}
			cfg.Domains = append(cfg.Domains, decodeDomain(m))
		}
	}
	if services, ok := sliceValue(root["services"]); ok {
		for _, item := range services {
			m, ok := mapValue(item)
			if !ok {
				return nil, errors.New("services entries must be maps")
			}
			cfg.Services = append(cfg.Services, decodeService(m))
		}
	}
	if exits, ok := sliceValue(root["exits"]); ok {
		for _, item := range exits {
			m, ok := mapValue(item)
			if !ok {
				return nil, errors.New("exits entries must be maps")
			}
			cfg.Exits = append(cfg.Exits, decodeExit(m))
		}
	}
	if bindings, ok := sliceValue(root["bindings"]); ok {
		for _, item := range bindings {
			m, ok := mapValue(item)
			if !ok {
				return nil, errors.New("bindings entries must be maps")
			}
			cfg.Bindings = append(cfg.Bindings, decodeBinding(m))
		}
	}
	return cfg, nil
}

func decodeSettings(m map[string]any) model.Settings {
	s := model.Settings{
		StateDir:       stringValue(m["state_dir"]),
		SecretsPath:    stringValue(m["secrets_path"]),
		GeneratedByTag: stringValue(m["generated_by_tag"]),
	}
	if nginx, ok := mapValue(m["nginx"]); ok {
		s.Nginx = model.NginxSettings{
			GeneratedDir:  stringValue(nginx["generated_dir"]),
			TestCommand:   stringValue(nginx["test_command"]),
			ReloadCommand: stringValue(nginx["reload_command"]),
		}
	}
	if acme, ok := mapValue(m["acme"]); ok {
		s.ACME = model.ACMESettings{
			Enabled:      boolValue(acme["enabled"]),
			Provider:     stringValue(acme["provider"]),
			Email:        stringValue(acme["email"]),
			CertDir:      stringValue(acme["cert_dir"]),
			ACMEShPath:   stringValue(acme["acme_sh_path"]),
			ACMEShECC:    boolValue(acme["acme_sh_ecc"]),
			ACMEShDNS:    stringValue(acme["acme_sh_dns"]),
			ACMEShServer: stringValue(acme["acme_sh_server"]),
		}
	}
	if frp, ok := mapValue(m["frp"]); ok {
		s.FRP = model.FRPSettings{
			Enabled:       boolValue(frp["enabled"]),
			InstallDir:    stringValue(frp["install_dir"]),
			ConfigDir:     stringValue(frp["config_dir"]),
			SystemdDir:    stringValue(frp["systemd_dir"]),
			ServicePrefix: stringValue(frp["service_prefix"]),
			ManageSystemd: boolValue(frp["manage_systemd"]),
			ReloadCommand: stringValue(frp["reload_command"]),
		}
	}
	if cf, ok := mapValue(m["cloudflare"]); ok {
		s.Cloudflare = model.CloudflareSettings{
			Enabled:        boolValue(cf["enabled"]),
			ConflictPolicy: stringValue(cf["conflict_policy"]),
		}
	}
	return s
}

func decodeDomain(m map[string]any) model.Domain {
	return model.Domain{
		Name:            stringValue(m["name"]),
		Provider:        stringValue(m["provider"]),
		ZoneRef:         stringValue(m["zone_ref"]),
		ZoneID:          stringValue(m["zone_id"]),
		TokenRef:        stringValue(m["token_ref"]),
		DefaultProxied:  boolValue(m["default_proxied"]),
		AllowDNSUpdates: boolValue(m["allow_dns_updates"]),
		AllowACME:       boolValue(m["allow_acme"]),
	}
}

func decodeService(m map[string]any) model.Service {
	s := model.Service{
		Name:     stringValue(m["name"]),
		Protocol: stringValue(m["protocol"]),
		Host:     stringValue(m["host"]),
		Port:     intValue(m["port"]),
	}
	if options, ok := mapValue(m["options"]); ok {
		s.Options = model.ServiceOptions{
			Websocket:         boolValue(options["websocket"]),
			BackendTLSVerify:  stringValue(options["backend_tls_verify"]),
			ConnectTimeout:    stringValue(options["connect_timeout"]),
			SendTimeout:       stringValue(options["send_timeout"]),
			ReadTimeout:       stringValue(options["read_timeout"]),
			ClientMaxBodySize: stringValue(options["client_max_body_size"]),
			Buffering:         stringValue(options["buffering"]),
			RequestBuffering:  stringValue(options["request_buffering"]),
			RangeMode:         stringValue(options["range_mode"]),
		}
	}
	return s
}

func decodeExit(m map[string]any) model.Exit {
	e := model.Exit{
		Name:   stringValue(m["name"]),
		Type:   stringValue(m["type"]),
		Domain: stringValue(m["domain"]),
	}
	if targets, ok := sliceValue(m["dns_targets"]); ok {
		for _, item := range targets {
			tm, ok := mapValue(item)
			if !ok {
				continue
			}
			e.DNSTargets = append(e.DNSTargets, model.DNSTarget{
				Type:  strings.ToUpper(stringValue(tm["type"])),
				Value: stringValue(tm["value"]),
			})
		}
	}
	if frp, ok := mapValue(m["frp"]); ok {
		e.FRP = model.FRPExit{
			ServerAddr:      stringValue(frp["server_addr"]),
			ServerPort:      intValue(frp["server_port"]),
			TokenRef:        stringValue(frp["token_ref"]),
			TLS:             boolValue(frp["tls"]),
			RemoteHTTPSPort: intValue(frp["remote_https_port"]),
			LocalHTTPSPort:  intValue(frp["local_https_port"]),
		}
	}
	return e
}

func decodeBinding(m map[string]any) model.Binding {
	return model.Binding{
		Name:         stringValue(m["name"]),
		Service:      stringValue(m["service"]),
		Exit:         stringValue(m["exit"]),
		Hostname:     stringValue(m["hostname"]),
		SSL:          boolValue(m["ssl"]),
		Proxied:      boolValue(m["proxied"]),
		ExternalPort: intValue(m["external_port"]),
		CertName:     stringValue(m["cert_name"]),
		Disabled:     boolValue(m["disabled"]),
	}
}

func decodeSecrets(root map[string]any) *model.Secrets {
	secrets := &model.Secrets{
		CloudflareZones:  map[string]string{},
		CloudflareTokens: map[string]string{},
		FRPTokens:        map[string]string{},
	}
	if cf, ok := mapValue(root["cloudflare_zones"]); ok {
		for k, v := range cf {
			secrets.CloudflareZones[k] = stringValue(v)
		}
	}
	if cf, ok := mapValue(root["cloudflare_tokens"]); ok {
		for k, v := range cf {
			secrets.CloudflareTokens[k] = stringValue(v)
		}
	}
	if frp, ok := mapValue(root["frp_tokens"]); ok {
		for k, v := range frp {
			secrets.FRPTokens[k] = stringValue(v)
		}
	}
	return secrets
}

func mapValue(v any) (map[string]any, bool) {
	m, ok := v.(map[string]any)
	return m, ok
}

func sliceValue(v any) ([]any, bool) {
	s, ok := v.([]any)
	return s, ok
}

func stringValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case int:
		return strconv.Itoa(t)
	case bool:
		if t {
			return "true"
		}
		return "false"
	case nil:
		return ""
	default:
		return fmt.Sprint(t)
	}
}

func boolValue(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "yes" || t == "on"
	default:
		return false
	}
}

func intValue(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case string:
		i, _ := strconv.Atoi(t)
		return i
	default:
		return 0
	}
}
