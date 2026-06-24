package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lantern/internal/acme"
	"lantern/internal/apply"
	"lantern/internal/config"
	"lantern/internal/dns"
	"lantern/internal/frp"
	"lantern/internal/model"
	"lantern/internal/nginx"
	"lantern/internal/planner"
	"lantern/internal/tui"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "lantern: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 0 {
		printHelp()
		return nil
	}

	cmd := args[0]
	args = args[1:]

	switch cmd {
	case "help", "-h", "--help":
		printHelp()
		return nil
	case "init":
		fs := flag.NewFlagSet("init", flag.ExitOnError)
		configPath := fs.String("config", defaultConfigPath(), "config file path")
		force := fs.Bool("force", false, "overwrite existing config")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return initConfig(*configPath, *force)
	case "tui":
		fs := flag.NewFlagSet("tui", flag.ExitOnError)
		configPath := fs.String("config", defaultConfigPath(), "config file path")
		if err := fs.Parse(args); err != nil {
			return err
		}
		return tui.Run(ctx, *configPath)
	case "validate":
		cfg, _, err := loadConfigOnly(args)
		if err != nil {
			return err
		}
		p := planner.Build(cfg)
		printDiagnostics(p)
		if p.HasErrors() {
			return errors.New("validation failed")
		}
		fmt.Println("configuration is valid")
		return nil
	case "plan":
		cfg, _, err := loadConfigOnly(args)
		if err != nil {
			return err
		}
		p := planner.Build(cfg)
		printPlan(p)
		if p.HasErrors() {
			return errors.New("plan has errors")
		}
		return nil
	case "apply":
		fs := flag.NewFlagSet("apply", flag.ExitOnError)
		configPath := fs.String("config", defaultConfigPath(), "config file path")
		yes := fs.Bool("yes", false, "write files and run configured reload commands")
		skipReload := fs.Bool("skip-reload", false, "write files but do not reload services")
		syncDNS := fs.Bool("dns", false, "sync Cloudflare DNS records before writing local config")
		forceDNS := fs.Bool("force-dns", false, "update conflicting single DNS records during sync")
		renewCerts := fs.Bool("certs", false, "renew ACME certificates before reloading nginx")
		manageSystemd := fs.Bool("systemd", false, "run systemctl daemon-reload and enable/restart generated frpc units")
		if err := fs.Parse(args); err != nil {
			return err
		}
		cfg, secrets, err := loadAll(*configPath)
		if err != nil {
			return err
		}
		return apply.Run(ctx, cfg, secrets, apply.Options{
			AssumeYes:     *yes,
			SkipReload:    *skipReload,
			SyncDNS:       *syncDNS,
			ForceDNS:      *forceDNS,
			RenewCerts:    *renewCerts,
			ManageSystemd: *manageSystemd,
		})
	case "render":
		return render(args)
	case "dns":
		return dnsCommand(ctx, args)
	case "frp":
		return frpCommand(args)
	case "cert":
		return certCommand(ctx, args)
	default:
		return fmt.Errorf("unknown command %q", cmd)
	}
}

func loadConfigOnly(args []string) (*model.Config, string, error) {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	if err := fs.Parse(args); err != nil {
		return nil, "", err
	}
	cfg, err := config.Load(*configPath)
	if err != nil {
		return nil, "", err
	}
	return cfg, *configPath, nil
}

func loadAll(path string) (*model.Config, *model.Secrets, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	secretsPath := cfg.Settings.SecretsPath
	if !filepath.IsAbs(secretsPath) {
		secretsPath = filepath.Join(filepath.Dir(path), secretsPath)
	}
	secrets, err := config.LoadSecrets(secretsPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, nil, err
	}
	if secrets == nil {
		secrets = &model.Secrets{}
	}
	return cfg, secrets, nil
}

func render(args []string) error {
	if len(args) == 0 {
		return errors.New("render requires target: nginx or frp")
	}
	target := args[0]
	cfg, _, err := loadConfigOnly(args[1:])
	if err != nil {
		return err
	}
	secrets := &model.Secrets{}

	switch target {
	case "nginx":
		files, err := nginx.Generate(cfg)
		if err != nil {
			return err
		}
		printFiles(files)
	case "frp":
		files, err := frp.Generate(cfg, secrets)
		if err != nil {
			return err
		}
		printFiles(files)
	default:
		return fmt.Errorf("unknown render target %q", target)
	}
	return nil
}

func dnsCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("dns requires subcommand: plan or sync")
	}
	sub := args[0]
	fs := flag.NewFlagSet("dns "+sub, flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	yes := fs.Bool("yes", false, "perform Cloudflare mutations")
	force := fs.Bool("force", false, "update conflicting single records")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	switch sub {
	case "plan":
		cfg, _, err := loadConfigOnly([]string{"--config", *configPath})
		if err != nil {
			return err
		}
		records, diagnostics := dns.DesiredRecords(cfg)
		printDNS(records, diagnostics)
		if hasErrors(diagnostics) {
			return errors.New("dns plan has errors")
		}
	case "sync":
		cfg, secrets, err := loadAll(*configPath)
		if err != nil {
			return err
		}
		if !*yes {
			return errors.New("dns sync requires --yes")
		}
		result, err := dns.Sync(ctx, cfg, secrets, dns.SyncOptions{Force: *force})
		if err != nil {
			return err
		}
		for _, action := range result.Actions {
			fmt.Printf("%-8s %-8s %s\n", action.Kind, action.Operation, action.Target)
			if action.Details != "" {
				fmt.Printf("  %s\n", action.Details)
			}
		}
		for _, diag := range result.Diagnostics {
			fmt.Printf("%s: %s\n", strings.ToUpper(string(diag.Severity)), diag.Message)
		}
		if hasErrors(result.Diagnostics) {
			return errors.New("dns sync completed with errors")
		}
	default:
		return fmt.Errorf("unknown dns subcommand %q", sub)
	}
	return nil
}

func loadAllAllowMissingSecrets(path string) (*model.Config, *model.Secrets, error) {
	cfg, err := config.Load(path)
	if err != nil {
		return nil, nil, err
	}
	secretsPath := cfg.Settings.SecretsPath
	if !filepath.IsAbs(secretsPath) {
		secretsPath = filepath.Join(filepath.Dir(path), secretsPath)
	}
	secrets, err := config.LoadSecrets(secretsPath)
	if err != nil {
		return cfg, &model.Secrets{}, nil
	}
	return cfg, secrets, nil
}

func frpCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("frp requires subcommand: plan, render, install-script, or install")
	}
	sub := args[0]
	fs := flag.NewFlagSet("frp "+sub, flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	version := fs.String("version", "", "frp release version for install-script, such as v0.61.0")
	yes := fs.Bool("yes", false, "perform installation")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, secrets, err := loadAllAllowMissingSecrets(*configPath)
	if err != nil {
		return err
	}
	switch sub {
	case "plan":
		files, err := frp.Generate(cfg, secrets)
		if err != nil {
			return err
		}
		for _, path := range frp.SortedFiles(files) {
			fmt.Println(path)
		}
	case "render":
		files, err := frp.Generate(cfg, secrets)
		if err != nil {
			return err
		}
		printFiles(files)
	case "install-script":
		plan := frp.GenerateInstallPlan(cfg, *version)
		fmt.Printf("### %s\n%s", plan.ScriptPath, plan.Script)
	case "install":
		if *version == "" {
			return errors.New("frp install requires --version, for example --version v0.61.0")
		}
		plan := frp.GenerateInstallPlan(cfg, *version)
		if !*yes {
			fmt.Printf("would write and run %s; rerun with --yes to install\n", plan.ScriptPath)
			return nil
		}
		return frp.RunInstallScript(context.Background(), plan)
	default:
		return fmt.Errorf("unknown frp subcommand %q", sub)
	}
	return nil
}

func certCommand(ctx context.Context, args []string) error {
	if len(args) == 0 {
		return errors.New("cert requires subcommand: plan, issue, or renew")
	}
	sub := args[0]
	fs := flag.NewFlagSet("cert "+sub, flag.ExitOnError)
	configPath := fs.String("config", defaultConfigPath(), "config file path")
	yes := fs.Bool("yes", false, "run lego instead of printing the planned commands")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}
	cfg, secrets, err := loadAllAllowMissingSecrets(*configPath)
	if err != nil {
		return err
	}
	certs, diagnostics := acme.DesiredCertificates(cfg)
	for _, diag := range diagnostics {
		fmt.Printf("%s: %s\n", strings.ToUpper(string(diag.Severity)), diag.Message)
	}
	if hasErrors(diagnostics) {
		return errors.New("certificate plan has errors")
	}
	switch sub {
	case "plan":
		for _, cert := range certs {
			fmt.Printf("%s -> %s domains=%s provider=%s\n", cert.Name, cert.CertFile, strings.Join(cert.Domains, ","), cert.Provider)
		}
	case "issue", "renew":
		if !*yes {
			for _, cert := range certs {
				args := cert.IssueArgs
				if sub == "renew" {
					args = acme.RenewArgs(cert)
				}
				fmt.Println(acme.ShellCommand(acmeEnvName(cert), "<redacted>", acmeBinary(cert), args))
			}
			return nil
		}
		for _, cert := range certs {
			token := ""
			if secrets.CloudflareTokens != nil {
				token = secrets.CloudflareTokens[cert.TokenRef]
			}
			if token == "" {
				return fmt.Errorf("missing Cloudflare token %q for certificate %s", cert.TokenRef, cert.Name)
			}
			args := cert.IssueArgs
			if sub == "renew" {
				args = acme.RenewArgs(cert)
			}
			if err := acme.RunProvider(ctx, cert, token, args); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("unknown cert subcommand %q", sub)
	}
	return nil
}

func acmeEnvName(cert acme.Certificate) string {
	if cert.Provider == "acme.sh" {
		return "CF_Token"
	}
	return "CLOUDFLARE_DNS_API_TOKEN"
}

func acmeBinary(cert acme.Certificate) string {
	if cert.Provider == "acme.sh" {
		return cert.ACMEShPath
	}
	return "lego"
}

func initConfig(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists; pass --force to overwrite", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return err
	}
	cfg := model.ExampleConfig()
	if err := config.Save(path, cfg); err != nil {
		return err
	}
	secretsPath := cfg.Settings.SecretsPath
	if !filepath.IsAbs(secretsPath) {
		secretsPath = filepath.Join(filepath.Dir(path), secretsPath)
	}
	if _, err := os.Stat(secretsPath); errors.Is(err, os.ErrNotExist) {
		if err := config.SaveSecrets(secretsPath, model.ExampleSecrets()); err != nil {
			return err
		}
	}
	fmt.Printf("created %s\n", path)
	fmt.Printf("created %s\n", secretsPath)
	return nil
}

func defaultConfigPath() string {
	if v := os.Getenv("LANTERN_CONFIG"); v != "" {
		return v
	}
	if _, err := os.Stat("lantern.yaml"); err == nil {
		return "lantern.yaml"
	}
	return "/etc/lantern/config.yaml"
}

func printHelp() {
	fmt.Print(`Lantern - terminal gateway config manager

Usage:
  lantern init [--config lantern.yaml]
  lantern tui [--config lantern.yaml]
  lantern validate [--config lantern.yaml]
  lantern plan [--config lantern.yaml]
  lantern apply --yes [--config lantern.yaml] [--dns] [--certs] [--systemd]
  lantern render nginx|frp [--config lantern.yaml]
  lantern dns plan|sync [--config lantern.yaml] [--yes] [--force]
  lantern frp plan|render|install-script|install [--config lantern.yaml] [--version v0.61.0] [--yes]
  lantern cert plan|issue|renew [--config lantern.yaml] [--yes]
`)
}

func printDiagnostics(p planner.Plan) {
	for _, d := range p.Diagnostics {
		fmt.Printf("%s: %s\n", strings.ToUpper(string(d.Severity)), d.Message)
	}
}

func printPlan(p planner.Plan) {
	printDiagnostics(p)
	for _, a := range p.Actions {
		fmt.Printf("%-8s %-12s %s\n", a.Kind, a.Operation, a.Target)
		if a.Details != "" {
			fmt.Printf("  %s\n", a.Details)
		}
	}
}

func printFiles(files map[string]string) {
	for path, body := range files {
		fmt.Printf("### %s\n%s", path, body)
		if !strings.HasSuffix(body, "\n") {
			fmt.Println()
		}
	}
}

func printDNS(records []dns.DesiredRecord, diagnostics []planner.Diagnostic) {
	for _, diag := range diagnostics {
		fmt.Printf("%s: %s\n", strings.ToUpper(string(diag.Severity)), diag.Message)
	}
	for _, record := range records {
		fmt.Printf("%-5s %-44s -> %-32s proxied=%v zone=%s\n", record.Type, record.Name, record.Content, record.Proxied, record.Zone.Name)
	}
}

func hasErrors(diagnostics []planner.Diagnostic) bool {
	for _, diag := range diagnostics {
		if diag.Severity == planner.SeverityError {
			return true
		}
	}
	return false
}
