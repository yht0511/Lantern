package frp

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"lantern/internal/model"
)

func Generate(cfg *model.Config, secrets *model.Secrets) (map[string]string, error) {
	cfg.ApplyDefaults()
	files := map[string]string{}
	services := mapServices(cfg)
	exits := mapExits(cfg)
	grouped := map[string][]model.Binding{}
	for _, binding := range cfg.Bindings {
		if binding.Disabled {
			continue
		}
		exit, ok := exits[binding.Exit]
		if !ok || exit.Type != "frp" {
			continue
		}
		grouped[exit.Name] = append(grouped[exit.Name], binding)
	}
	for exitName, bindings := range grouped {
		exit := exits[exitName]
		path := filepath.Join(cfg.Settings.FRP.ConfigDir, fmt.Sprintf("frpc-%s.toml", safeName(exit.Name)))
		files[path] = renderFRPC(cfg, secrets, exit, bindings, services)
		unitPath := filepath.Join(cfg.Settings.FRP.SystemdDir, UnitName(cfg, exit.Name))
		files[unitPath] = renderSystemdUnit(cfg, exit, path)
	}
	return files, nil
}

func renderFRPC(cfg *model.Config, secrets *model.Secrets, exit model.Exit, bindings []model.Binding, services map[string]model.Service) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", cfg.Settings.GeneratedByTag)
	fmt.Fprintf(&b, "serverAddr = %q\n", exit.FRP.ServerAddr)
	fmt.Fprintf(&b, "serverPort = %d\n\n", exit.FRP.ServerPort)
	fmt.Fprintf(&b, "auth.method = \"token\"\n")
	fmt.Fprintf(&b, "auth.token = %q\n\n", secretOrPlaceholder(secrets.FRPTokens, exit.FRP.TokenRef))
	if exit.FRP.TLS {
		fmt.Fprintf(&b, "transport.tls.enable = true\n\n")
	}

	hasHTTPS := false
	for _, binding := range bindings {
		service := services[binding.Service]
		if service.Protocol == "http" || service.Protocol == "https" {
			hasHTTPS = true
		}
	}
	if hasHTTPS {
		fmt.Fprintf(&b, "[[proxies]]\n")
		fmt.Fprintf(&b, "name = %q\n", "lantern-"+safeName(exit.Name)+"-https")
		fmt.Fprintf(&b, "type = \"tcp\"\n")
		fmt.Fprintf(&b, "localIP = \"127.0.0.1\"\n")
		fmt.Fprintf(&b, "localPort = %d\n", exit.FRP.LocalHTTPSPort)
		fmt.Fprintf(&b, "remotePort = %d\n\n", exit.FRP.RemoteHTTPSPort)
	}

	for _, binding := range bindings {
		service := services[binding.Service]
		if service.Protocol != "tcp" && service.Protocol != "udp" {
			continue
		}
		fmt.Fprintf(&b, "[[proxies]]\n")
		fmt.Fprintf(&b, "name = %q\n", "lantern-"+safeName(binding.Name))
		fmt.Fprintf(&b, "type = %q\n", service.Protocol)
		fmt.Fprintf(&b, "localIP = %q\n", service.Host)
		fmt.Fprintf(&b, "localPort = %d\n", service.Port)
		fmt.Fprintf(&b, "remotePort = %d\n\n", binding.ExternalPort)
	}
	return b.String()
}

func renderSystemdUnit(cfg *model.Config, exit model.Exit, configPath string) string {
	binary := filepath.Join(cfg.Settings.FRP.InstallDir, "frpc")
	var b strings.Builder
	fmt.Fprintf(&b, "[Unit]\n")
	fmt.Fprintf(&b, "Description=Lantern frpc tunnel for %s\n", exit.Name)
	fmt.Fprintf(&b, "After=network-online.target\n")
	fmt.Fprintf(&b, "Wants=network-online.target\n\n")
	fmt.Fprintf(&b, "[Service]\n")
	fmt.Fprintf(&b, "Type=simple\n")
	fmt.Fprintf(&b, "ExecStart=%s -c %s\n", binary, configPath)
	fmt.Fprintf(&b, "Restart=always\n")
	fmt.Fprintf(&b, "RestartSec=5s\n\n")
	fmt.Fprintf(&b, "[Install]\n")
	fmt.Fprintf(&b, "WantedBy=multi-user.target\n")
	return b.String()
}

func UnitName(cfg *model.Config, exitName string) string {
	cfg.ApplyDefaults()
	return fmt.Sprintf("%s-%s.service", cfg.Settings.FRP.ServicePrefix, safeName(exitName))
}

func DesiredUnitNames(cfg *model.Config) []string {
	cfg.ApplyDefaults()
	exits := mapExits(cfg)
	seen := map[string]bool{}
	for _, binding := range cfg.Bindings {
		if binding.Disabled {
			continue
		}
		exit, ok := exits[binding.Exit]
		if ok && exit.Type == "frp" {
			seen[UnitName(cfg, exit.Name)] = true
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func mapServices(cfg *model.Config) map[string]model.Service {
	out := map[string]model.Service{}
	for _, s := range cfg.Services {
		out[s.Name] = s
	}
	return out
}

func mapExits(cfg *model.Config) map[string]model.Exit {
	out := map[string]model.Exit{}
	for _, e := range cfg.Exits {
		out[e.Name] = e
	}
	return out
}

func secretOrPlaceholder(values map[string]string, ref string) string {
	if values != nil {
		if v := values[ref]; v != "" {
			return v
		}
	}
	if ref == "" {
		return "{{ missing frp token }}"
	}
	return "{{ secret:" + ref + " }}"
}

func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer("*", "wildcard", ".", "_", ":", "_", "/", "_", "\\", "_")
	return replacer.Replace(s)
}

func SortedFiles(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
