package apply

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"lantern/internal/acme"
	"lantern/internal/dns"
	"lantern/internal/frp"
	"lantern/internal/model"
	"lantern/internal/nginx"
	"lantern/internal/planner"
)

type Options struct {
	AssumeYes     bool
	SkipReload    bool
	SyncDNS       bool
	ForceDNS      bool
	RenewCerts    bool
	ManageSystemd bool
}

func Run(ctx context.Context, cfg *model.Config, secrets *model.Secrets, opts Options) error {
	p := planner.Build(cfg)
	for _, d := range p.Diagnostics {
		fmt.Printf("%s: %s\n", strings.ToUpper(string(d.Severity)), d.Message)
	}
	for _, action := range p.Actions {
		fmt.Printf("%-8s %-12s %s\n", action.Kind, action.Operation, action.Target)
	}
	if p.HasErrors() {
		return errors.New("apply aborted because plan has errors")
	}
	if !opts.AssumeYes {
		return errors.New("apply requires --yes")
	}
	if opts.SyncDNS {
		fmt.Println("syncing DNS")
		result, err := dns.Sync(ctx, cfg, secrets, dns.SyncOptions{Force: opts.ForceDNS})
		if err != nil {
			return err
		}
		for _, action := range result.Actions {
			fmt.Printf("%-8s %-12s %s\n", action.Kind, action.Operation, action.Target)
			if action.Details != "" {
				fmt.Printf("  %s\n", action.Details)
			}
		}
		for _, diag := range result.Diagnostics {
			fmt.Printf("%s: %s\n", strings.ToUpper(string(diag.Severity)), diag.Message)
		}
		if hasErrors(result.Diagnostics) {
			return errors.New("apply aborted because DNS sync has errors")
		}
	}
	if opts.RenewCerts {
		if err := renewCertificates(ctx, cfg, secrets); err != nil {
			return err
		}
	}
	nginxFiles, err := nginx.Generate(cfg)
	if err != nil {
		return err
	}
	frpFiles, err := frp.Generate(cfg, secrets)
	if err != nil {
		return err
	}
	for path, body := range merge(nginxFiles, frpFiles) {
		if err := writeFile(path, body, 0o644); err != nil {
			return err
		}
		fmt.Printf("wrote %s\n", path)
	}
	if opts.SkipReload {
		return nil
	}
	if len(nginxFiles) > 0 {
		if err := runCommand(ctx, cfg.Settings.Nginx.TestCommand); err != nil {
			return err
		}
		if err := runCommand(ctx, cfg.Settings.Nginx.ReloadCommand); err != nil {
			return err
		}
	}
	if opts.ManageSystemd && cfg.Settings.FRP.ManageSystemd {
		if err := systemdReload(ctx); err != nil {
			return err
		}
		for _, unit := range frp.DesiredUnitNames(cfg) {
			if err := runCommand(ctx, "systemctl enable --now "+unit); err != nil {
				return err
			}
			if err := runCommand(ctx, "systemctl restart "+unit); err != nil {
				return err
			}
		}
	} else if cfg.Settings.FRP.ReloadCommand != "" && len(frpFiles) > 0 {
		if err := runCommand(ctx, cfg.Settings.FRP.ReloadCommand); err != nil {
			return err
		}
	}
	return nil
}

func renewCertificates(ctx context.Context, cfg *model.Config, secrets *model.Secrets) error {
	certs, diagnostics := acme.DesiredCertificates(cfg)
	for _, diag := range diagnostics {
		fmt.Printf("%s: %s\n", strings.ToUpper(string(diag.Severity)), diag.Message)
	}
	if hasErrors(diagnostics) {
		return errors.New("certificate plan has errors")
	}
	for _, cert := range certs {
		token := ""
		if secrets != nil && secrets.CloudflareTokens != nil {
			token = secrets.CloudflareTokens[cert.TokenRef]
		}
		if token == "" {
			return fmt.Errorf("missing Cloudflare token %q for certificate %s", cert.TokenRef, cert.Name)
		}
		fmt.Printf("renewing certificate %s\n", cert.Name)
		if err := acme.RunProvider(ctx, cert, token, acme.RenewArgs(cert)); err != nil {
			return err
		}
	}
	return nil
}

func merge(a, b map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

func writeFile(path, body string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(body), mode)
}

func runCommand(ctx context.Context, command string) error {
	parts := strings.Fields(command)
	if len(parts) == 0 {
		return nil
	}
	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func systemdReload(ctx context.Context) error {
	return runCommand(ctx, "systemctl daemon-reload")
}

func hasErrors(diagnostics []planner.Diagnostic) bool {
	for _, diag := range diagnostics {
		if diag.Severity == planner.SeverityError {
			return true
		}
	}
	return false
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
