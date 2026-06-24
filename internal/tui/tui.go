package tui

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"unicode/utf8"

	"lantern/internal/config"
	"lantern/internal/model"
	"lantern/internal/planner"
)

const (
	keyNone = iota
	keyUp
	keyDown
	keyLeft
	keyRight
	keyEnter
	keyEsc
	keyBackspace
	keyDelete
	keyHome
	keyEnd
	keyTab
	keyRune
)

type key struct {
	code int
	r    rune
}

type terminal struct {
	restore string
	raw     bool
}

func Run(ctx context.Context, path string) error {
	_ = ctx
	cfg, err := config.Load(path)
	if os.IsNotExist(err) {
		cfg = model.ExampleConfig()
	} else if err != nil {
		return err
	}
	cfg.ApplyDefaults()

	term, err := enterRaw()
	if err != nil {
		return err
	}
	defer term.close()

	ui := &app{
		cfg:      cfg,
		path:     path,
		selected: map[int]int{},
		message:  "Loaded " + path,
	}
	if err := ui.run(); err != nil && !errors.Is(err, errQuit) {
		return err
	}
	return nil
}

type app struct {
	cfg      *model.Config
	path     string
	section  int
	selected map[int]int
	dirty    bool
	message  string
	showPlan bool
	confirm  string
}

type section struct {
	title string
}

var sections = []section{
	{title: "Services"},
	{title: "Domains"},
	{title: "Exits"},
	{title: "Bindings"},
	{title: "Settings"},
}

func (a *app) run() error {
	defer showCursor()
	hideCursor()
	a.clampSelection()
	a.draw()
	for {
		k, err := readKey(os.Stdin)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return fmt.Errorf("terminal input closed; run lantern tui from an interactive terminal")
			}
			return err
		}
		if k.code == keyNone {
			continue
		}
		if err := a.handle(k); err != nil {
			return err
		}
		a.clampSelection()
		a.draw()
	}
}

func (a *app) handle(k key) error {
	if a.confirm != "" {
		switch k.r {
		case 'y', 'Y':
			return errQuit
		case 'n', 'N':
			a.confirm = ""
			a.message = "Quit cancelled"
		}
		if k.code == keyEsc {
			a.confirm = ""
			a.message = "Quit cancelled"
		}
		return nil
	}
	if a.showPlan {
		if k.code == keyEsc || k.r == 'p' || k.r == 'q' {
			a.showPlan = false
		}
		return nil
	}

	switch k.code {
	case keyUp:
		a.selected[a.section]--
	case keyDown:
		a.selected[a.section]++
	case keyLeft:
		a.section--
		if a.section < 0 {
			a.section = len(sections) - 1
		}
	case keyRight, keyTab:
		a.section = (a.section + 1) % len(sections)
	case keyEnter:
		a.editSelected()
	}

	switch k.r {
	case 'k':
		a.selected[a.section]--
	case 'j':
		a.selected[a.section]++
	case 'h':
		a.section--
		if a.section < 0 {
			a.section = len(sections) - 1
		}
	case 'l':
		a.section = (a.section + 1) % len(sections)
	case 'a':
		a.addSelected()
	case 'e':
		a.editSelected()
	case 'd':
		a.deleteSelected()
	case 'p':
		a.showPlan = true
	case 's':
		if err := config.Save(a.path, a.cfg); err != nil {
			a.message = err.Error()
		} else {
			a.dirty = false
			a.message = "Saved " + a.path
		}
	case 'q':
		if a.dirty {
			a.confirm = "Configuration has unsaved changes. Quit without saving? [y/N]"
		} else {
			return errQuit
		}
	}
	return nil
}

var errQuit = errors.New("quit")

func (a *app) draw() {
	rows, cols := terminalSize()
	var b strings.Builder
	b.WriteString("\x1b[H\x1b[2J")
	drawHeader(&b, cols, a.dirty)
	drawTabs(&b, cols, a.section)
	if a.showPlan {
		a.drawPlan(&b, rows, cols)
	} else {
		a.drawMain(&b, rows, cols)
	}
	drawFooter(&b, rows, cols, a.message)
	if a.confirm != "" {
		drawBox(&b, rows/2-2, max(2, (cols-70)/2), 5, min(70, cols-4), "Confirm", []string{a.confirm})
	}
	fmt.Print(b.String())
}

func drawHeader(b *strings.Builder, cols int, dirty bool) {
	title := "Lantern"
	status := "clean"
	if dirty {
		status = "modified"
	}
	line := fmt.Sprintf(" %s - single-host gateway manager [%s]", title, status)
	b.WriteString(reverse(fit(line, cols)))
	newline(b)
}

func drawTabs(b *strings.Builder, cols, active int) {
	var line strings.Builder
	line.WriteString(" ")
	for i, s := range sections {
		name := " " + s.title + " "
		if i == active {
			line.WriteString(inverse(fit(name, displayWidth(name))))
		} else {
			line.WriteString(name)
		}
		line.WriteString(" ")
	}
	b.WriteString(pad(line.String(), cols))
	newline(b)
}

func (a *app) drawMain(b *strings.Builder, rows, cols int) {
	bodyRows := max(8, rows-5)
	leftW := min(max(34, cols/3), max(34, cols-44))
	rightW := cols - leftW - 1
	list := a.rowsForSection()
	selected := a.selected[a.section]

	for i := 0; i < bodyRows; i++ {
		var left, right string
		if i == 0 {
			left = bold(fit(sectionTitle(a.section), leftW))
			right = bold(fit("Details", rightW))
		} else {
			idx := i - 1
			if idx < len(list) {
				if idx == selected {
					left = inverse(fit(list[idx], leftW))
				} else {
					left = fit(list[idx], leftW)
				}
			}
			right = fit(a.detailLine(idx), rightW)
		}
		b.WriteString(left)
		b.WriteString(" ")
		b.WriteString(right)
		newline(b)
	}
}

func (a *app) drawPlan(b *strings.Builder, rows, cols int) {
	p := planner.Build(a.cfg)
	lines := []string{"Plan preview", ""}
	for _, d := range p.Diagnostics {
		lines = append(lines, strings.ToUpper(string(d.Severity))+": "+d.Message)
	}
	if len(p.Diagnostics) > 0 {
		lines = append(lines, "")
	}
	for _, act := range p.Actions {
		line := fmt.Sprintf("%-8s %-12s %s", act.Kind, act.Operation, act.Target)
		lines = append(lines, line)
		if act.Details != "" {
			lines = append(lines, "  "+act.Details)
		}
	}
	bodyRows := max(8, rows-5)
	for i := 0; i < bodyRows; i++ {
		line := ""
		if i < len(lines) {
			line = lines[i]
		}
		b.WriteString(fit(line, cols))
		newline(b)
	}
}

func drawFooter(b *strings.Builder, rows, cols int, msg string) {
	help := " arrows/hjkl move  Enter/e edit  a add  d delete  p plan  s save  q quit "
	if msg != "" {
		help = msg + " |" + help
	}
	b.WriteString("\x1b[" + strconv.Itoa(rows) + ";1H")
	b.WriteString(reverse(fit(help, cols)))
}

func newline(b *strings.Builder) {
	b.WriteString("\r\n")
}

func (a *app) rowsForSection() []string {
	switch a.section {
	case 0:
		out := make([]string, 0, len(a.cfg.Services))
		for _, s := range a.cfg.Services {
			out = append(out, fmt.Sprintf("%-18s %-5s %s:%d", s.Name, s.Protocol, s.Host, s.Port))
		}
		if len(out) == 0 {
			return []string{"No services. Press a to add."}
		}
		return out
	case 1:
		out := make([]string, 0, len(a.cfg.Domains))
		for _, d := range a.cfg.Domains {
			out = append(out, fmt.Sprintf("%-28s %s", d.Name, d.Provider))
		}
		if len(out) == 0 {
			return []string{"No domains. Press a to add."}
		}
		return out
	case 2:
		out := make([]string, 0, len(a.cfg.Exits))
		for _, e := range a.cfg.Exits {
			out = append(out, fmt.Sprintf("%-18s %-6s %s", e.Name, e.Type, e.Domain))
		}
		if len(out) == 0 {
			return []string{"No exits. Press a to add."}
		}
		return out
	case 3:
		out := make([]string, 0, len(a.cfg.Bindings))
		for _, binding := range a.cfg.Bindings {
			state := "on"
			if binding.Disabled {
				state = "off"
			}
			out = append(out, fmt.Sprintf("%-18s %-3s %s", binding.Name, state, binding.Hostname))
		}
		if len(out) == 0 {
			return []string{"No bindings. Press a to add."}
		}
		return out
	case 4:
		return []string{"General", "Nginx", "ACME", "FRP", "Cloudflare"}
	default:
		return nil
	}
}

func (a *app) detailLine(idx int) string {
	if idx < 0 {
		return ""
	}
	switch a.section {
	case 0:
		if idx >= len(a.cfg.Services) {
			return ""
		}
		s := a.cfg.Services[idx]
		return fmt.Sprintf("protocol=%s target=%s:%d websocket=%v buffering=%s request_buffering=%s range=%s",
			s.Protocol, s.Host, s.Port, s.Options.Websocket, s.Options.Buffering, s.Options.RequestBuffering, s.Options.RangeMode)
	case 1:
		if idx >= len(a.cfg.Domains) {
			return ""
		}
		d := a.cfg.Domains[idx]
		return fmt.Sprintf("zone_ref=%s token=%s proxied_default=%v dns=%v acme=%v",
			d.ZoneRef, d.TokenRef, d.DefaultProxied, d.AllowDNSUpdates, d.AllowACME)
	case 2:
		if idx >= len(a.cfg.Exits) {
			return ""
		}
		e := a.cfg.Exits[idx]
		var targets []string
		for _, t := range e.DNSTargets {
			targets = append(targets, t.Type+"="+t.Value)
		}
		if e.Type == "frp" {
			return fmt.Sprintf("domain=%s targets=%s frp=%s:%d remote_https=%d local_https=%d tls=%v",
				e.Domain, strings.Join(targets, ","), e.FRP.ServerAddr, e.FRP.ServerPort, e.FRP.RemoteHTTPSPort, e.FRP.LocalHTTPSPort, e.FRP.TLS)
		}
		return fmt.Sprintf("domain=%s targets=%s", e.Domain, strings.Join(targets, ","))
	case 3:
		if idx >= len(a.cfg.Bindings) {
			return ""
		}
		b := a.cfg.Bindings[idx]
		return fmt.Sprintf("service=%s exit=%s ssl=%v proxied=%v external_port=%d cert=%s",
			b.Service, b.Exit, b.SSL, b.Proxied, b.ExternalPort, b.CertName)
	case 4:
		return a.settingsDetail(idx)
	default:
		return ""
	}
}

func (a *app) settingsDetail(idx int) string {
	switch idx {
	case 0:
		return fmt.Sprintf("state_dir=%s secrets_path=%s", a.cfg.Settings.StateDir, a.cfg.Settings.SecretsPath)
	case 1:
		return fmt.Sprintf("generated_dir=%s test=%q reload=%q", a.cfg.Settings.Nginx.GeneratedDir, a.cfg.Settings.Nginx.TestCommand, a.cfg.Settings.Nginx.ReloadCommand)
	case 2:
		return fmt.Sprintf("enabled=%v provider=%s email=%s cert_dir=%s", a.cfg.Settings.ACME.Enabled, a.cfg.Settings.ACME.Provider, a.cfg.Settings.ACME.Email, a.cfg.Settings.ACME.CertDir)
	case 3:
		return fmt.Sprintf("enabled=%v install=%s config=%s systemd=%s manage=%v", a.cfg.Settings.FRP.Enabled, a.cfg.Settings.FRP.InstallDir, a.cfg.Settings.FRP.ConfigDir, a.cfg.Settings.FRP.SystemdDir, a.cfg.Settings.FRP.ManageSystemd)
	case 4:
		return fmt.Sprintf("enabled=%v conflict_policy=%s", a.cfg.Settings.Cloudflare.Enabled, a.cfg.Settings.Cloudflare.ConflictPolicy)
	default:
		return ""
	}
}

func (a *app) addSelected() {
	switch a.section {
	case 0:
		s := model.Service{Name: "new-service", Protocol: "http", Host: "192.168.1.10", Port: 80}
		a.cfg.Services = append(a.cfg.Services, s)
		a.cfg.ApplyDefaults()
		idx := len(a.cfg.Services) - 1
		a.selected[a.section] = idx
		a.editService(idx)
	case 1:
		d := model.Domain{Provider: "cloudflare", AllowDNSUpdates: true, AllowACME: true}
		a.cfg.Domains = append(a.cfg.Domains, d)
		idx := len(a.cfg.Domains) - 1
		a.selected[a.section] = idx
		a.editDomain(idx)
	case 2:
		e := model.Exit{Name: "new-exit", Type: "direct"}
		if len(a.cfg.Domains) > 0 {
			e.Domain = a.cfg.Domains[0].Name
		}
		a.cfg.Exits = append(a.cfg.Exits, e)
		a.cfg.ApplyDefaults()
		idx := len(a.cfg.Exits) - 1
		a.selected[a.section] = idx
		a.editExit(idx)
	case 3:
		b := model.Binding{Name: "new-binding", SSL: true}
		if len(a.cfg.Services) > 0 {
			b.Service = a.cfg.Services[0].Name
		}
		if len(a.cfg.Exits) > 0 {
			b.Exit = a.cfg.Exits[0].Name
			b.Hostname = "new." + a.cfg.Exits[0].Domain
		}
		a.cfg.Bindings = append(a.cfg.Bindings, b)
		idx := len(a.cfg.Bindings) - 1
		a.selected[a.section] = idx
		a.editBinding(idx)
	default:
		a.message = "Add is not available here"
	}
}

func (a *app) editSelected() {
	idx := a.selected[a.section]
	switch a.section {
	case 0:
		a.editService(idx)
	case 1:
		a.editDomain(idx)
	case 2:
		a.editExit(idx)
	case 3:
		a.editBinding(idx)
	case 4:
		a.editSettings(idx)
	}
}

func (a *app) deleteSelected() {
	idx := a.selected[a.section]
	switch a.section {
	case 0:
		if idx >= 0 && idx < len(a.cfg.Services) {
			name := a.cfg.Services[idx].Name
			a.cfg.Services = append(a.cfg.Services[:idx], a.cfg.Services[idx+1:]...)
			a.markDirty("Deleted service " + name)
		}
	case 1:
		if idx >= 0 && idx < len(a.cfg.Domains) {
			name := a.cfg.Domains[idx].Name
			a.cfg.Domains = append(a.cfg.Domains[:idx], a.cfg.Domains[idx+1:]...)
			a.markDirty("Deleted domain " + name)
		}
	case 2:
		if idx >= 0 && idx < len(a.cfg.Exits) {
			name := a.cfg.Exits[idx].Name
			a.cfg.Exits = append(a.cfg.Exits[:idx], a.cfg.Exits[idx+1:]...)
			a.markDirty("Deleted exit " + name)
		}
	case 3:
		if idx >= 0 && idx < len(a.cfg.Bindings) {
			name := a.cfg.Bindings[idx].Name
			a.cfg.Bindings = append(a.cfg.Bindings[:idx], a.cfg.Bindings[idx+1:]...)
			a.markDirty("Deleted binding " + name)
		}
	default:
		a.message = "Delete is not available here"
	}
	a.clampSelection()
}

func (a *app) editService(idx int) {
	if idx < 0 || idx >= len(a.cfg.Services) {
		return
	}
	s := a.cfg.Services[idx]
	fields := []field{
		textField("Name", s.Name),
		choiceField("Protocol", s.Protocol, []string{"http", "https", "tcp", "udp"}),
		textField("Internal host/IP", s.Host),
		intField("Internal port", s.Port),
		boolField("Websocket", s.Options.Websocket),
		choiceField("Backend TLS verify", s.Options.BackendTLSVerify, []string{"on", "off"}),
		textField("Connect timeout", s.Options.ConnectTimeout),
		textField("Send timeout", s.Options.SendTimeout),
		textField("Read timeout", s.Options.ReadTimeout),
		textField("Client max body size", s.Options.ClientMaxBodySize),
		choiceField("Proxy buffering", s.Options.Buffering, []string{"default", "on", "off"}),
		choiceField("Request buffering", s.Options.RequestBuffering, []string{"default", "on", "off"}),
		choiceField("Range mode", s.Options.RangeMode, []string{"default", "pass", "strip"}),
	}
	if editForm("Edit Service", fields) {
		s.Name = fields[0].value
		s.Protocol = fields[1].value
		s.Host = fields[2].value
		s.Port = atoiDefault(fields[3].value, s.Port)
		s.Options.Websocket = parseBool(fields[4].value)
		s.Options.BackendTLSVerify = fields[5].value
		s.Options.ConnectTimeout = fields[6].value
		s.Options.SendTimeout = fields[7].value
		s.Options.ReadTimeout = fields[8].value
		s.Options.ClientMaxBodySize = fields[9].value
		s.Options.Buffering = fields[10].value
		s.Options.RequestBuffering = fields[11].value
		s.Options.RangeMode = fields[12].value
		a.cfg.Services[idx] = s
		a.cfg.ApplyDefaults()
		a.markDirty("Updated service " + s.Name)
	}
}

func (a *app) editDomain(idx int) {
	if idx < 0 || idx >= len(a.cfg.Domains) {
		return
	}
	d := a.cfg.Domains[idx]
	fields := []field{
		textField("Domain root", d.Name),
		choiceField("Provider", defaultString(d.Provider, "cloudflare"), []string{"cloudflare"}),
		textField("Cloudflare zone ref", d.ZoneRef),
		textField("Token ref", d.TokenRef),
		boolField("Default orange cloud", d.DefaultProxied),
		boolField("Allow DNS updates", d.AllowDNSUpdates),
		boolField("Allow ACME", d.AllowACME),
	}
	if editForm("Edit Domain", fields) {
		d.Name = fields[0].value
		d.Provider = fields[1].value
		d.ZoneRef = fields[2].value
		d.TokenRef = fields[3].value
		d.DefaultProxied = parseBool(fields[4].value)
		d.AllowDNSUpdates = parseBool(fields[5].value)
		d.AllowACME = parseBool(fields[6].value)
		a.cfg.Domains[idx] = d
		a.markDirty("Updated domain " + d.Name)
	}
}

func (a *app) editExit(idx int) {
	if idx < 0 || idx >= len(a.cfg.Exits) {
		return
	}
	e := a.cfg.Exits[idx]
	fields := []field{
		textField("Name", e.Name),
		choiceField("Type", e.Type, []string{"direct", "frp"}),
		choiceField("Domain", e.Domain, a.domainChoices()),
		textField("DNS targets", encodeTargets(e.DNSTargets)),
		textField("FRP server addr", e.FRP.ServerAddr),
		intField("FRP server port", e.FRP.ServerPort),
		textField("FRP token ref", e.FRP.TokenRef),
		boolField("FRP TLS", e.FRP.TLS),
		intField("Remote HTTPS port", e.FRP.RemoteHTTPSPort),
		intField("Local HTTPS port", e.FRP.LocalHTTPSPort),
	}
	if editForm("Edit Exit", fields) {
		e.Name = fields[0].value
		e.Type = fields[1].value
		e.Domain = fields[2].value
		e.DNSTargets = parseTargets(fields[3].value)
		e.FRP.ServerAddr = fields[4].value
		e.FRP.ServerPort = atoiDefault(fields[5].value, e.FRP.ServerPort)
		e.FRP.TokenRef = fields[6].value
		e.FRP.TLS = parseBool(fields[7].value)
		e.FRP.RemoteHTTPSPort = atoiDefault(fields[8].value, e.FRP.RemoteHTTPSPort)
		e.FRP.LocalHTTPSPort = atoiDefault(fields[9].value, e.FRP.LocalHTTPSPort)
		a.cfg.Exits[idx] = e
		a.cfg.ApplyDefaults()
		a.markDirty("Updated exit " + e.Name)
	}
}

func (a *app) editBinding(idx int) {
	if idx < 0 || idx >= len(a.cfg.Bindings) {
		return
	}
	b := a.cfg.Bindings[idx]
	service := a.serviceByName(b.Service)
	externalPort := b.ExternalPort
	if service.Protocol != "tcp" && service.Protocol != "udp" {
		externalPort = 0
	}
	fields := []field{
		textField("Name", b.Name),
		choiceField("Service", b.Service, a.serviceChoices()),
		choiceField("Exit", b.Exit, a.exitChoices()),
		textField("Hostname", b.Hostname),
		boolField("SSL", b.SSL),
		boolField("Cloudflare orange cloud", b.Proxied),
		textField("Certificate name", b.CertName),
		boolField("Disabled", b.Disabled),
	}
	hasExternalPort := service.Protocol == "tcp" || service.Protocol == "udp"
	if hasExternalPort {
		fields = insertField(fields, 6, intField("External port", externalPort))
	}
	if editForm("Edit Binding", fields) {
		b.Name = fields[0].value
		b.Service = fields[1].value
		b.Exit = fields[2].value
		b.Hostname = fields[3].value
		b.SSL = parseBool(fields[4].value)
		b.Proxied = parseBool(fields[5].value)
		if hasExternalPort {
			b.ExternalPort = atoiDefault(fields[6].value, b.ExternalPort)
			b.CertName = fields[7].value
			b.Disabled = parseBool(fields[8].value)
		} else {
			b.ExternalPort = 0
			b.CertName = fields[6].value
			b.Disabled = parseBool(fields[7].value)
		}
		a.cfg.Bindings[idx] = b
		a.markDirty("Updated binding " + b.Name)
	}
}

func (a *app) editSettings(idx int) {
	switch idx {
	case 0:
		fields := []field{
			textField("State dir", a.cfg.Settings.StateDir),
			textField("Secrets path", a.cfg.Settings.SecretsPath),
		}
		if editForm("General Settings", fields) {
			a.cfg.Settings.StateDir = fields[0].value
			a.cfg.Settings.SecretsPath = fields[1].value
			a.markDirty("Updated general settings")
		}
	case 1:
		fields := []field{
			textField("Generated conf dir", a.cfg.Settings.Nginx.GeneratedDir),
			textField("Test command", a.cfg.Settings.Nginx.TestCommand),
			textField("Reload command", a.cfg.Settings.Nginx.ReloadCommand),
		}
		if editForm("Nginx Settings", fields) {
			a.cfg.Settings.Nginx.GeneratedDir = fields[0].value
			a.cfg.Settings.Nginx.TestCommand = fields[1].value
			a.cfg.Settings.Nginx.ReloadCommand = fields[2].value
			a.markDirty("Updated nginx settings")
		}
	case 2:
		fields := []field{
			boolField("Enable ACME", a.cfg.Settings.ACME.Enabled),
			choiceField("Provider", a.cfg.Settings.ACME.Provider, []string{"lego", "acme.sh"}),
			textField("Email", a.cfg.Settings.ACME.Email),
			textField("Certificate dir", a.cfg.Settings.ACME.CertDir),
			textField("acme.sh path", a.cfg.Settings.ACME.ACMEShPath),
			boolField("acme.sh ECC", a.cfg.Settings.ACME.ACMEShECC),
			textField("acme.sh DNS", a.cfg.Settings.ACME.ACMEShDNS),
			textField("acme.sh server", a.cfg.Settings.ACME.ACMEShServer),
		}
		if editForm("ACME Settings", fields) {
			a.cfg.Settings.ACME.Enabled = parseBool(fields[0].value)
			a.cfg.Settings.ACME.Provider = fields[1].value
			a.cfg.Settings.ACME.Email = fields[2].value
			a.cfg.Settings.ACME.CertDir = fields[3].value
			a.cfg.Settings.ACME.ACMEShPath = fields[4].value
			a.cfg.Settings.ACME.ACMEShECC = parseBool(fields[5].value)
			a.cfg.Settings.ACME.ACMEShDNS = fields[6].value
			a.cfg.Settings.ACME.ACMEShServer = fields[7].value
			a.markDirty("Updated ACME settings")
		}
	case 3:
		fields := []field{
			boolField("Enable FRP", a.cfg.Settings.FRP.Enabled),
			textField("Install dir", a.cfg.Settings.FRP.InstallDir),
			textField("Config dir", a.cfg.Settings.FRP.ConfigDir),
			textField("Systemd dir", a.cfg.Settings.FRP.SystemdDir),
			textField("Service prefix", a.cfg.Settings.FRP.ServicePrefix),
			boolField("Manage systemd", a.cfg.Settings.FRP.ManageSystemd),
			textField("Reload command", a.cfg.Settings.FRP.ReloadCommand),
		}
		if editForm("FRP Settings", fields) {
			a.cfg.Settings.FRP.Enabled = parseBool(fields[0].value)
			a.cfg.Settings.FRP.InstallDir = fields[1].value
			a.cfg.Settings.FRP.ConfigDir = fields[2].value
			a.cfg.Settings.FRP.SystemdDir = fields[3].value
			a.cfg.Settings.FRP.ServicePrefix = fields[4].value
			a.cfg.Settings.FRP.ManageSystemd = parseBool(fields[5].value)
			a.cfg.Settings.FRP.ReloadCommand = fields[6].value
			a.markDirty("Updated FRP settings")
		}
	case 4:
		fields := []field{
			boolField("Enable Cloudflare", a.cfg.Settings.Cloudflare.Enabled),
			choiceField("Conflict policy", a.cfg.Settings.Cloudflare.ConflictPolicy, []string{"prompt", "skip", "force"}),
		}
		if editForm("Cloudflare Settings", fields) {
			a.cfg.Settings.Cloudflare.Enabled = parseBool(fields[0].value)
			a.cfg.Settings.Cloudflare.ConflictPolicy = fields[1].value
			a.markDirty("Updated Cloudflare settings")
		}
	}
	a.cfg.ApplyDefaults()
}

func (a *app) markDirty(msg string) {
	a.dirty = true
	a.message = msg
}

func (a *app) clampSelection() {
	maxIdx := len(a.rowsForSection()) - 1
	if a.section == 0 && len(a.cfg.Services) == 0 {
		maxIdx = 0
	}
	if a.section == 1 && len(a.cfg.Domains) == 0 {
		maxIdx = 0
	}
	if a.section == 2 && len(a.cfg.Exits) == 0 {
		maxIdx = 0
	}
	if a.section == 3 && len(a.cfg.Bindings) == 0 {
		maxIdx = 0
	}
	if a.selected[a.section] < 0 {
		a.selected[a.section] = 0
	}
	if a.selected[a.section] > maxIdx {
		a.selected[a.section] = maxIdx
	}
}

func (a *app) domainChoices() []string {
	out := make([]string, 0, len(a.cfg.Domains))
	for _, d := range a.cfg.Domains {
		out = append(out, d.Name)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func (a *app) serviceChoices() []string {
	out := make([]string, 0, len(a.cfg.Services))
	for _, s := range a.cfg.Services {
		out = append(out, s.Name)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func (a *app) exitChoices() []string {
	out := make([]string, 0, len(a.cfg.Exits))
	for _, e := range a.cfg.Exits {
		out = append(out, e.Name)
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func (a *app) serviceByName(name string) model.Service {
	for _, s := range a.cfg.Services {
		if s.Name == name {
			return s
		}
	}
	return model.Service{}
}

func insertField(fields []field, idx int, value field) []field {
	if idx < 0 || idx > len(fields) {
		return append(fields, value)
	}
	fields = append(fields, field{})
	copy(fields[idx+1:], fields[idx:])
	fields[idx] = value
	return fields
}

type fieldKind int

const (
	fieldText fieldKind = iota
	fieldInt
	fieldBool
	fieldChoice
)

type field struct {
	label   string
	value   string
	kind    fieldKind
	choices []string
}

func textField(label, value string) field {
	return field{label: label, value: value, kind: fieldText}
}

func intField(label string, value int) field {
	return field{label: label, value: strconv.Itoa(value), kind: fieldInt}
}

func boolField(label string, value bool) field {
	return field{label: label, value: strconv.FormatBool(value), kind: fieldBool}
}

func choiceField(label, value string, choices []string) field {
	if value == "" && len(choices) > 0 {
		value = choices[0]
	}
	return field{label: label, value: value, kind: fieldChoice, choices: choices}
}

func editForm(title string, fields []field) bool {
	selected := 0
	message := "Enter edit  Space toggle/cycle  Left/Right cycle  s save  Esc cancel"
	for {
		rows, cols := terminalSize()
		var b strings.Builder
		b.WriteString("\x1b[H\x1b[2J")
		drawHeader(&b, cols, false)
		height := min(rows-4, len(fields)+6)
		width := min(cols-4, max(70, cols*3/4))
		top := max(3, (rows-height)/2)
		left := max(2, (cols-width)/2)
		lines := make([]string, 0, height-2)
		for i, f := range fields {
			prefix := "  "
			if i == selected {
				prefix = "> "
			}
			value := displayFieldValue(f)
			line := fmt.Sprintf("%s%-24s %s", prefix, f.label, value)
			if i == selected {
				line = inverse(fit(line, width-2))
			} else {
				line = fit(line, width-2)
			}
			lines = append(lines, line)
		}
		lines = append(lines, "", message)
		drawBox(&b, top, left, height, width, title, lines)
		fmt.Print(b.String())

		k, err := readKey(os.Stdin)
		if err != nil {
			return false
		}
		switch k.code {
		case keyEsc:
			return false
		case keyUp:
			selected--
		case keyDown:
			selected++
		case keyEnter:
			editFieldValue(&fields[selected])
		case keyLeft:
			cycleChoice(&fields[selected], -1)
		case keyRight:
			cycleChoice(&fields[selected], 1)
		}
		switch k.r {
		case 'k':
			selected--
		case 'j':
			selected++
		case ' ':
			if fields[selected].kind == fieldBool {
				fields[selected].value = strconv.FormatBool(!parseBool(fields[selected].value))
			} else {
				cycleChoice(&fields[selected], 1)
			}
		case 's':
			return true
		case 'q':
			return false
		}
		if selected < 0 {
			selected = len(fields) - 1
		}
		if selected >= len(fields) {
			selected = 0
		}
	}
}

func editFieldValue(f *field) {
	switch f.kind {
	case fieldBool:
		f.value = strconv.FormatBool(!parseBool(f.value))
	case fieldChoice:
		cycleChoice(f, 1)
	default:
		if value, ok := readLine("Edit "+f.label, f.value); ok {
			if f.kind == fieldInt {
				if _, err := strconv.Atoi(value); err != nil && value != "" {
					return
				}
			}
			f.value = value
		}
	}
}

func readLine(title, initial string) (string, bool) {
	input := []rune(initial)
	cursor := len(input)
	for {
		rows, cols := terminalSize()
		var b strings.Builder
		b.WriteString("\x1b[H\x1b[2J")
		drawHeader(&b, cols, false)
		width := min(cols-4, max(70, cols*3/4))
		top := max(5, rows/2-3)
		left := max(2, (cols-width)/2)
		inputWidth := max(1, width-4)
		visibleInput, cursorOffset := inputViewport(string(input), cursor, inputWidth)
		lines := []string{
			visibleInput,
			"",
			"Enter accept  Esc cancel  Left/Right move  Backspace/Delete edit",
		}
		drawBox(&b, top, left, 7, width, title, lines)
		b.WriteString(fmt.Sprintf("\x1b[%d;%dH", top+1, left+1+cursorOffset))
		showCursor()
		fmt.Print(b.String())
		k, err := readKey(os.Stdin)
		hideCursor()
		if err != nil {
			return "", false
		}
		switch k.code {
		case keyEnter:
			return string(input), true
		case keyEsc:
			return "", false
		case keyLeft:
			if cursor > 0 {
				cursor--
			}
		case keyRight:
			if cursor < len(input) {
				cursor++
			}
		case keyHome:
			cursor = 0
		case keyEnd:
			cursor = len(input)
		case keyBackspace:
			if cursor > 0 {
				input = append(input[:cursor-1], input[cursor:]...)
				cursor--
			}
		case keyDelete:
			if cursor < len(input) {
				input = append(input[:cursor], input[cursor+1:]...)
			}
		case keyRune:
			if k.r >= 32 && k.r != 127 {
				input = append(input, 0)
				copy(input[cursor+1:], input[cursor:])
				input[cursor] = k.r
				cursor++
			}
		}
	}
}

func inputViewport(s string, cursor, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	runes := []rune(s)
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(runes) {
		cursor = len(runes)
	}
	if len(runes) <= width {
		return fit(s, width), cursor
	}
	start := 0
	if cursor >= width {
		start = cursor - width + 1
	}
	if start > len(runes)-width {
		start = len(runes) - width
	}
	end := min(len(runes), start+width)
	visible := string(runes[start:end])
	cursorOffset := cursor - start
	if start > 0 {
		visibleRunes := []rune(visible)
		if len(visibleRunes) > 0 {
			visibleRunes[0] = '>'
			visible = string(visibleRunes)
		}
		if cursorOffset == 0 {
			cursorOffset = 1
		}
	}
	if end < len(runes) {
		visibleRunes := []rune(visible)
		if len(visibleRunes) > 0 {
			visibleRunes[len(visibleRunes)-1] = '<'
			visible = string(visibleRunes)
		}
		if cursorOffset >= width {
			cursorOffset = width - 1
		}
	}
	return fit(visible, width), cursorOffset
}

func displayFieldValue(f field) string {
	switch f.kind {
	case fieldBool:
		if parseBool(f.value) {
			return "[x]"
		}
		return "[ ]"
	case fieldChoice:
		return "< " + f.value + " >"
	default:
		if f.value == "" {
			return "(empty)"
		}
		return f.value
	}
}

func cycleChoice(f *field, delta int) {
	if f.kind != fieldChoice || len(f.choices) == 0 {
		return
	}
	idx := 0
	for i, choice := range f.choices {
		if choice == f.value {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(f.choices)) % len(f.choices)
	f.value = f.choices[idx]
}

func drawBox(b *strings.Builder, top, left, height, width int, title string, lines []string) {
	if height < 3 || width < 8 {
		return
	}
	hline := strings.Repeat("-", width-2)
	b.WriteString(fmt.Sprintf("\x1b[%d;%dH+%s+", top, left, hline))
	title = " " + title + " "
	if len(title) < width-2 {
		b.WriteString(fmt.Sprintf("\x1b[%d;%dH%s", top, left+2, title))
	}
	for i := 1; i < height-1; i++ {
		content := ""
		if i-1 < len(lines) {
			content = lines[i-1]
		}
		b.WriteString(fmt.Sprintf("\x1b[%d;%dH|%s|", top+i, left, fit(content, width-2)))
	}
	b.WriteString(fmt.Sprintf("\x1b[%d;%dH+%s+", top+height-1, left, hline))
}

func sectionTitle(idx int) string {
	if idx >= 0 && idx < len(sections) {
		return sections[idx].title
	}
	return ""
}

func encodeTargets(targets []model.DNSTarget) string {
	parts := make([]string, 0, len(targets))
	for _, t := range targets {
		parts = append(parts, t.Type+"="+t.Value)
	}
	return strings.Join(parts, ",")
}

func parseTargets(raw string) []model.DNSTarget {
	var out []model.DNSTarget
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			key, value, ok = strings.Cut(part, ":")
		}
		if !ok {
			continue
		}
		out = append(out, model.DNSTarget{Type: strings.ToUpper(strings.TrimSpace(key)), Value: strings.TrimSpace(value)})
	}
	return out
}

func readKey(in *os.File) (key, error) {
	var buf [8]byte
	n, err := in.Read(buf[:1])
	if err != nil {
		return key{}, err
	}
	if n == 0 {
		return key{}, nil
	}
	c := buf[0]
	switch c {
	case 13, 10:
		return key{code: keyEnter}, nil
	case 27:
		_, _ = stty("min", "0", "time", "1")
		defer stty("min", "1", "time", "0")
		n, _ = in.Read(buf[1:4])
		if n >= 2 && buf[1] == '[' {
			switch buf[2] {
			case 'A':
				return key{code: keyUp}, nil
			case 'B':
				return key{code: keyDown}, nil
			case 'C':
				return key{code: keyRight}, nil
			case 'D':
				return key{code: keyLeft}, nil
			case 'H':
				return key{code: keyHome}, nil
			case 'F':
				return key{code: keyEnd}, nil
			case '1', '7':
				return key{code: keyHome}, nil
			case '4', '8':
				return key{code: keyEnd}, nil
			case '3':
				return key{code: keyDelete}, nil
			}
		}
		return key{code: keyEsc}, nil
	case 127, 8:
		return key{code: keyBackspace}, nil
	case 9:
		return key{code: keyTab}, nil
	default:
		if c < utf8.RuneSelf {
			return key{code: keyRune, r: rune(c)}, nil
		}
		rest := make([]byte, utf8.UTFMax)
		rest[0] = c
		for i := 1; i < utf8.UTFMax; i++ {
			if !utf8.FullRune(rest[:i]) {
				n, err := in.Read(rest[i : i+1])
				if err != nil || n == 0 {
					break
				}
				continue
			}
			r, _ := utf8.DecodeRune(rest[:i])
			return key{code: keyRune, r: r}, nil
		}
		return key{code: keyRune, r: rune(c)}, nil
	}
}

func enterRaw() (*terminal, error) {
	if !isTerminal() {
		return nil, fmt.Errorf("tui requires an interactive terminal")
	}
	restore, err := stty("-g")
	if err != nil {
		return nil, err
	}
	if _, err := stty("raw", "-echo", "min", "1", "time", "0"); err != nil {
		return nil, err
	}
	fmt.Print("\x1b[?1049h")
	return &terminal{restore: strings.TrimSpace(restore), raw: true}, nil
}

func (t *terminal) close() {
	if t == nil || !t.raw {
		return
	}
	fmt.Print("\x1b[?1049l\x1b[0m")
	_, _ = stty(t.restore)
}

func stty(args ...string) (string, error) {
	cmd := exec.Command("stty", args...)
	cmd.Stdin = os.Stdin
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

func terminalSize() (int, int) {
	out, err := stty("size")
	if err == nil {
		parts := strings.Fields(out)
		if len(parts) == 2 {
			rows, _ := strconv.Atoi(parts[0])
			cols, _ := strconv.Atoi(parts[1])
			if rows > 0 && cols > 0 {
				return rows, cols
			}
		}
	}
	return 24, 100
}

func isTerminal() bool {
	info, err := os.Stdin.Stat()
	return err == nil && (info.Mode()&os.ModeCharDevice) != 0
}

func hideCursor() {
	fmt.Print("\x1b[?25l")
}

func showCursor() {
	fmt.Print("\x1b[?25h")
}

func bold(s string) string {
	return "\x1b[1m" + s + "\x1b[0m"
}

func inverse(s string) string {
	return "\x1b[7m" + s + "\x1b[0m"
}

func reverse(s string) string {
	return inverse(s)
}

func pad(s string, width int) string {
	plain := stripANSI(s)
	if displayWidth(plain) >= width {
		return trunc(s, width)
	}
	return s + strings.Repeat(" ", width-displayWidth(plain))
}

func fit(s string, width int) string {
	return pad(truncPlain(stripANSI(s), width), width)
}

func trunc(s string, width int) string {
	if width <= 0 {
		return ""
	}
	var out strings.Builder
	visible := 0
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			out.WriteRune(r)
			continue
		}
		if inEsc {
			out.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		if visible >= width {
			break
		}
		out.WriteRune(r)
		visible++
	}
	if visible < displayWidth(stripANSI(s)) && width > 1 {
		result := out.String()
		result = stripTrailingANSI(result)
		return result[:max(0, len(result)-1)] + ">"
	}
	return out.String()
}

func truncPlain(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if displayWidth(s) <= width {
		return s
	}
	runes := []rune(s)
	if width == 1 {
		return ">"
	}
	if len(runes) > width-1 {
		runes = runes[:width-1]
	}
	return string(runes) + ">"
}

func stripANSI(s string) string {
	var out strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		out.WriteRune(r)
	}
	return out.String()
}

func stripTrailingANSI(s string) string {
	return strings.TrimSuffix(s, "\x1b[0m")
}

func displayWidth(s string) int {
	return utf8.RuneCountInString(s)
}

func parseBool(s string) bool {
	return s == "true" || s == "yes" || s == "on" || s == "1"
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	value, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return value
}

func defaultString(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func init() {
	// Keep bufio imported for older Go vet paths that inspect generated docs.
	_ = bufio.ErrInvalidUnreadByte
}
