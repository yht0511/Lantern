package acme

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"lantern/internal/model"
	"lantern/internal/planner"
)

type Certificate struct {
	Name      string
	Domains   []string
	Zone      model.Domain
	TokenRef  string
	CertDir   string
	CertFile  string
	KeyFile   string
	Provider  string
	Email     string
	ACMEShPath string
	ACMEShDNS  string
	ACMEShECC  bool
	ACMEShServer string
	IssueArgs []string
}

func DesiredCertificates(cfg *model.Config) ([]Certificate, []planner.Diagnostic) {
	cfg.ApplyDefaults()
	var diagnostics []planner.Diagnostic
	if !cfg.Settings.ACME.Enabled {
		diagnostics = append(diagnostics, planner.Diagnostic{Severity: planner.SeverityInfo, Message: "ACME is disabled"})
		return nil, diagnostics
	}
	zones := cfg.Domains
	certHosts := map[string]map[string]bool{}
	for _, binding := range cfg.Bindings {
		if binding.Disabled || !binding.SSL {
			continue
		}
		certName := binding.CertName
		if certName == "" {
			certName = binding.Hostname
		}
		if certHosts[certName] == nil {
			certHosts[certName] = map[string]bool{}
		}
		certHosts[certName][binding.Hostname] = true
	}

	var certs []Certificate
	for name, hosts := range certHosts {
		domains := make([]string, 0, len(hosts))
		for host := range hosts {
			domains = append(domains, host)
		}
		sort.Strings(domains)
		zone, ok := longestMatchingZone(domains[0], zones)
		if !ok {
			diagnostics = append(diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("certificate %q has no matching zone for %s", name, domains[0])})
			continue
		}
		if !zone.AllowACME {
			diagnostics = append(diagnostics, planner.Diagnostic{Severity: planner.SeverityWarn, Message: fmt.Sprintf("certificate %q uses zone %s where ACME is disabled", name, zone.Name)})
		}
		cert := Certificate{
			Name:         name,
			Domains:      domains,
			Zone:         zone,
			TokenRef:     zone.TokenRef,
			CertDir:      certDirFor(cfg.Settings.ACME, name),
			Provider:     cfg.Settings.ACME.Provider,
			Email:        cfg.Settings.ACME.Email,
			ACMEShPath:   cfg.Settings.ACME.ACMEShPath,
			ACMEShDNS:    cfg.Settings.ACME.ACMEShDNS,
			ACMEShECC:    cfg.Settings.ACME.ACMEShECC,
			ACMEShServer: cfg.Settings.ACME.ACMEShServer,
		}
		cert.CertFile, cert.KeyFile = filesForProvider(cert.Provider, cert.CertDir, domains[0])
		cert.IssueArgs = IssueArgs(cert)
		certs = append(certs, cert)
	}
	sort.Slice(certs, func(i, j int) bool { return certs[i].Name < certs[j].Name })
	return certs, diagnostics
}

func NginxCertificateFiles(cfg *model.Config, binding model.Binding) (string, string) {
	cfg.ApplyDefaults()
	certName := binding.CertName
	if certName == "" {
		certName = binding.Hostname
	}
	mainDomain := binding.Hostname
	var domains []string
	for _, candidate := range cfg.Bindings {
		if candidate.Disabled || !candidate.SSL {
			continue
		}
		candidateName := candidate.CertName
		if candidateName == "" {
			candidateName = candidate.Hostname
		}
		if candidateName == certName {
			domains = append(domains, candidate.Hostname)
		}
	}
	if len(domains) > 0 {
		sort.Strings(domains)
		mainDomain = domains[0]
	}
	return filesForProvider(cfg.Settings.ACME.Provider, certDirFor(cfg.Settings.ACME, certName), mainDomain)
}

func certDirFor(settings model.ACMESettings, certName string) string {
	if settings.Provider == "acme.sh" {
		name := certName
		if settings.ACMEShECC {
			name += "_ecc"
		}
		return filepath.Join(settings.CertDir, name)
	}
	return filepath.Join(settings.CertDir, safeName(certName))
}

func filesForProvider(provider, certDir, mainDomain string) (string, string) {
	switch provider {
	case "lego":
		base := filepath.Join(certDir, "certificates", mainDomain)
		return base + ".pem", base + ".key"
	case "acme.sh":
		base := filepath.Base(strings.TrimSuffix(certDir, string(filepath.Separator)))
		certName := strings.TrimSuffix(base, "_ecc")
		return filepath.Join(certDir, "fullchain.cer"), filepath.Join(certDir, certName+".key")
	default:
		return filepath.Join(certDir, "fullchain.pem"), filepath.Join(certDir, "privkey.pem")
	}
}

func IssueArgs(cert Certificate) []string {
	switch cert.Provider {
	case "acme.sh":
		return acmeShArgs(cert, "--issue")
	default:
		return legoIssueArgs(cert)
	}
}

func legoIssueArgs(cert Certificate) []string {
	args := []string{
		"--accept-tos",
		"--email", cert.Email,
		"--dns", "cloudflare",
		"--pem",
		"--path", cert.CertDir,
	}
	for _, domain := range cert.Domains {
		args = append(args, "--domains", domain)
	}
	args = append(args, "run")
	return args
}

func RenewArgs(cert Certificate) []string {
	if cert.Provider == "acme.sh" {
		return acmeShArgs(cert, "--renew")
	}
	args := []string{
		"--accept-tos",
		"--email", cert.Email,
		"--dns", "cloudflare",
		"--pem",
		"--path", cert.CertDir,
	}
	for _, domain := range cert.Domains {
		args = append(args, "--domains", domain)
	}
	args = append(args, "renew", "--days", "30")
	return args
}

func acmeShArgs(cert Certificate, mode string) []string {
	args := []string{mode}
	for _, domain := range cert.Domains {
		args = append(args, "-d", domain)
	}
	if mode == "--issue" && cert.ACMEShDNS != "" {
		args = append(args, "--dns", cert.ACMEShDNS)
	}
	if cert.ACMEShECC {
		args = append(args, "--ecc")
	}
	if cert.Email != "" {
		args = append(args, "--accountemail", cert.Email)
	}
	if cert.ACMEShServer != "" {
		args = append(args, "--server", cert.ACMEShServer)
	}
	return args
}

func ShellCommand(envName, token, binary string, args []string) string {
	if binary == "" {
		binary = "lego"
	}
	quoted := make([]string, 0, len(args)+2)
	quoted = append(quoted, envName+"="+shellQuote(token), shellQuote(binary))
	for _, arg := range args {
		quoted = append(quoted, shellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func RunProvider(ctx context.Context, cert Certificate, token string, args []string) error {
	binary := "lego"
	envName := "CLOUDFLARE_DNS_API_TOKEN"
	if cert.Provider == "acme.sh" {
		binary = cert.ACMEShPath
		envName = "CF_Token"
	}
	if binary == "" {
		binary = "lego"
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), envName+"="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func RunLego(ctx context.Context, envName, token string, args []string) error {
	cmd := exec.CommandContext(ctx, "lego", args...)
	cmd.Env = append(os.Environ(), envName+"="+token)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	return cmd.Run()
}

func longestMatchingZone(hostname string, zones []model.Domain) (model.Domain, bool) {
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	var best model.Domain
	for _, zone := range zones {
		name := strings.TrimSuffix(strings.ToLower(zone.Name), ".")
		if hostname == name || strings.HasSuffix(hostname, "."+name) {
			if len(name) > len(best.Name) {
				best = zone
			}
		}
	}
	return best, best.Name != ""
}

func safeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	replacer := strings.NewReplacer("*", "wildcard", ".", "_", ":", "_", "/", "_", "\\", "_")
	return replacer.Replace(s)
}

func shellQuote(s string) string {
	if s == "" {
		return "''"
	}
	if !strings.ContainsAny(s, " \t\n'\"$`\\") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
