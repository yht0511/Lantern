package nginx

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"lantern/internal/acme"
	"lantern/internal/model"
)

func Generate(cfg *model.Config) (map[string]string, error) {
	cfg.ApplyDefaults()
	files := map[string]string{}
	services := mapServices(cfg)
	for _, binding := range cfg.Bindings {
		if binding.Disabled {
			continue
		}
		service, ok := services[binding.Service]
		if !ok {
			continue
		}
		if service.Protocol != "http" && service.Protocol != "https" {
			continue
		}
		name := safeName(binding.Hostname) + ".conf"
		files[filepath.Join(cfg.Settings.Nginx.GeneratedDir, name)] = renderServer(cfg, service, binding)
	}
	return files, nil
}

func renderServer(cfg *model.Config, service model.Service, binding model.Binding) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n", cfg.Settings.GeneratedByTag)
	fmt.Fprintf(&b, "# binding: %s -> %s://%s:%d\n\n", binding.Name, service.Protocol, service.Host, service.Port)
	if binding.SSL {
		fmt.Fprintf(&b, "server {\n")
		fmt.Fprintf(&b, "    listen 80;\n")
		fmt.Fprintf(&b, "    listen [::]:80;\n")
		fmt.Fprintf(&b, "    server_name %s;\n\n", binding.Hostname)
		fmt.Fprintf(&b, "    return 301 https://$host$request_uri;\n")
		fmt.Fprintf(&b, "}\n\n")
	}
	fmt.Fprintf(&b, "server {\n")
	if binding.SSL {
		fmt.Fprintf(&b, "    listen 443 ssl;\n")
		fmt.Fprintf(&b, "    listen [::]:443 ssl;\n")
	} else {
		fmt.Fprintf(&b, "    listen 80;\n")
		fmt.Fprintf(&b, "    listen [::]:80;\n")
	}
	fmt.Fprintf(&b, "    server_name %s;\n\n", binding.Hostname)
	if binding.SSL {
		certName := binding.CertName
		if certName == "" {
			certName = binding.Hostname
		}
		_ = certName
		certFile, keyFile := acme.NginxCertificateFiles(cfg, binding)
		fmt.Fprintf(&b, "    ssl_certificate     %s;\n", certFile)
		fmt.Fprintf(&b, "    ssl_certificate_key %s;\n\n", keyFile)
	}
	if service.Options.ClientMaxBodySize != "" {
		fmt.Fprintf(&b, "    client_max_body_size %s;\n\n", service.Options.ClientMaxBodySize)
	}
	fmt.Fprintf(&b, "    location / {\n")
	fmt.Fprintf(&b, "        proxy_pass %s://%s:%d;\n\n", service.Protocol, service.Host, service.Port)
	fmt.Fprintf(&b, "        proxy_http_version 1.1;\n")
	if service.Options.Websocket {
		fmt.Fprintf(&b, "        proxy_set_header Upgrade $http_upgrade;\n")
		fmt.Fprintf(&b, "        proxy_set_header Connection \"upgrade\";\n")
	} else {
		fmt.Fprintf(&b, "        proxy_set_header Connection \"\";\n")
	}
	fmt.Fprintf(&b, "\n")
	fmt.Fprintf(&b, "        proxy_set_header Host $host;\n")
	fmt.Fprintf(&b, "        proxy_set_header X-Real-IP $remote_addr;\n")
	fmt.Fprintf(&b, "        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;\n")
	fmt.Fprintf(&b, "        proxy_set_header X-Forwarded-Proto $scheme;\n\n")
	if service.Protocol == "https" && service.Options.BackendTLSVerify == "off" {
		fmt.Fprintf(&b, "        proxy_ssl_verify off;\n\n")
	}
	switch service.Options.RangeMode {
	case "pass":
		fmt.Fprintf(&b, "        proxy_set_header Range $http_range;\n")
		fmt.Fprintf(&b, "        proxy_set_header If-Range $http_if_range;\n\n")
	case "strip":
		fmt.Fprintf(&b, "        proxy_set_header Range \"\";\n")
		fmt.Fprintf(&b, "        proxy_set_header If-Range \"\";\n\n")
	}
	if service.Options.Buffering == "off" || service.Options.Buffering == "on" {
		fmt.Fprintf(&b, "        proxy_buffering %s;\n", service.Options.Buffering)
	}
	if service.Options.RequestBuffering == "off" || service.Options.RequestBuffering == "on" {
		fmt.Fprintf(&b, "        proxy_request_buffering %s;\n", service.Options.RequestBuffering)
	}
	if service.Options.Buffering != "default" || service.Options.RequestBuffering != "default" {
		fmt.Fprintf(&b, "\n")
	}
	fmt.Fprintf(&b, "        proxy_connect_timeout %s;\n", service.Options.ConnectTimeout)
	fmt.Fprintf(&b, "        proxy_send_timeout %s;\n", service.Options.SendTimeout)
	fmt.Fprintf(&b, "        proxy_read_timeout %s;\n", service.Options.ReadTimeout)
	fmt.Fprintf(&b, "        send_timeout %s;\n", service.Options.SendTimeout)
	fmt.Fprintf(&b, "    }\n")
	fmt.Fprintf(&b, "}\n")
	return b.String()
}

func mapServices(cfg *model.Config) map[string]model.Service {
	out := map[string]model.Service{}
	for _, s := range cfg.Services {
		out[s.Name] = s
	}
	return out
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
