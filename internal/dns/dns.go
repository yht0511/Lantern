package dns

import (
	"context"
	"fmt"
	"strings"

	"lantern/internal/cloudflare"
	"lantern/internal/model"
	"lantern/internal/netprobe"
	"lantern/internal/planner"
)

type DesiredRecord struct {
	Zone    model.Domain
	Name    string
	Type    string
	Content string
	Proxied bool
}

type SyncOptions struct {
	Force bool
}

type SyncResult struct {
	Actions     []planner.Action
	Diagnostics []planner.Diagnostic
}

func DesiredRecords(cfg *model.Config) ([]DesiredRecord, []planner.Diagnostic) {
	cfg.ApplyDefaults()
	var diagnostics []planner.Diagnostic
	exits := mapExits(cfg)
	zones := cfg.Domains
	seen := map[string]DesiredRecord{}
	for _, binding := range cfg.Bindings {
		if binding.Disabled {
			continue
		}
		exit, ok := exits[binding.Exit]
		if !ok {
			diagnostics = append(diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("binding %q references missing exit %q", binding.Name, binding.Exit)})
			continue
		}
		zone, ok := longestMatchingZone(binding.Hostname, zones)
		if !ok {
			diagnostics = append(diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("hostname %q has no matching domain zone", binding.Hostname)})
			continue
		}
		for _, target := range exit.DNSTargets {
			record := DesiredRecord{
				Zone:    zone,
				Name:    binding.Hostname,
				Type:    strings.ToUpper(target.Type),
				Content: target.Value,
				Proxied: binding.Proxied,
			}
			key := record.Type + "|" + record.Name
			if old, exists := seen[key]; exists && (old.Content != record.Content || old.Proxied != record.Proxied) {
				diagnostics = append(diagnostics, planner.Diagnostic{
					Severity: planner.SeverityError,
					Message:  fmt.Sprintf("desired DNS conflict for %s %s: %s/proxied=%v vs %s/proxied=%v", record.Type, record.Name, old.Content, old.Proxied, record.Content, record.Proxied),
				})
				continue
			}
			seen[key] = record
		}
	}
	records := make([]DesiredRecord, 0, len(seen))
	for _, record := range seen {
		records = append(records, record)
	}
	return records, diagnostics
}

func Sync(ctx context.Context, cfg *model.Config, secrets *model.Secrets, opts SyncOptions) (SyncResult, error) {
	records, diagnostics := DesiredRecords(cfg)
	result := SyncResult{Diagnostics: diagnostics}
	if hasErrors(diagnostics) {
		return result, nil
	}
	for _, desired := range records {
		resolved, err := resolveContent(ctx, desired.Type, desired.Content)
		if err != nil {
			result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("failed to resolve %s for %s: %v", desired.Content, desired.Name, err)})
			continue
		}
		desired.Content = resolved
		if desired.Zone.Provider != "cloudflare" {
			result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityWarn, Message: fmt.Sprintf("skipping %s: provider %q is not implemented", desired.Name, desired.Zone.Provider)})
			continue
		}
		if !desired.Zone.AllowDNSUpdates {
			result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityWarn, Message: fmt.Sprintf("skipping %s: DNS updates disabled for zone %s", desired.Name, desired.Zone.Name)})
			continue
		}
		token := ""
		if secrets != nil && secrets.CloudflareTokens != nil {
			token = secrets.CloudflareTokens[desired.Zone.TokenRef]
		}
		if token == "" {
			result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("missing Cloudflare token for token_ref %q in zone %s", redactRef(desired.Zone.TokenRef), desired.Zone.Name)})
			continue
		}
		zoneID := cloudflareZoneID(desired.Zone, secrets)
		if zoneID == "" {
			if desired.Zone.ZoneRef != "" {
				result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("missing Cloudflare zone id for zone_ref %q in zone %s", redactRef(desired.Zone.ZoneRef), desired.Zone.Name)})
			} else {
				result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("missing Cloudflare zone_ref for zone %s", desired.Zone.Name)})
			}
			continue
		}
		client := cloudflare.NewClient(zoneID, token)
		current, err := client.ListRecords(ctx, desired.Type, desired.Name)
		if err != nil {
			return result, err
		}
		switch len(current) {
		case 0:
			if err := client.CreateRecord(ctx, cloudflare.Record{
				Type:    desired.Type,
				Name:    desired.Name,
				Content: desired.Content,
				Proxied: desired.Proxied,
			}); err != nil {
				return result, err
			}
			result.Actions = append(result.Actions, planner.Action{Kind: "dns", Operation: "create", Target: desired.Name, Details: desired.Type + " " + desired.Content})
		case 1:
			existing := current[0]
			if existing.Content == desired.Content && existing.Proxied == desired.Proxied {
				result.Actions = append(result.Actions, planner.Action{Kind: "dns", Operation: "noop", Target: desired.Name, Details: "already matches"})
				continue
			}
			if !opts.Force {
				result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("record %s %s differs; rerun with --force to update", desired.Type, desired.Name)})
				continue
			}
			existing.Content = desired.Content
			existing.Proxied = desired.Proxied
			if err := client.UpdateRecord(ctx, existing); err != nil {
				return result, err
			}
			result.Actions = append(result.Actions, planner.Action{Kind: "dns", Operation: "update", Target: desired.Name, Details: desired.Type + " " + desired.Content})
		default:
			result.Diagnostics = append(result.Diagnostics, planner.Diagnostic{Severity: planner.SeverityError, Message: fmt.Sprintf("record %s %s has %d duplicates; delete or consolidate manually first", desired.Type, desired.Name, len(current))})
		}
	}
	return result, nil
}

func cloudflareZoneID(zone model.Domain, secrets *model.Secrets) string {
	if zone.ZoneRef != "" && secrets != nil && secrets.CloudflareZones != nil {
		if zoneID := secrets.CloudflareZones[zone.ZoneRef]; zoneID != "" {
			return zoneID
		}
	}
	return zone.ZoneID
}

func redactRef(ref string) string {
	if ref == "" {
		return ""
	}
	if len(ref) <= 8 {
		return "***"
	}
	return ref[:4] + "..." + ref[len(ref)-4:]
}

func resolveContent(ctx context.Context, recordType, content string) (string, error) {
	switch content {
	case "auto:public-ipv4":
		if recordType != "A" {
			return "", fmt.Errorf("auto:public-ipv4 requires A record, got %s", recordType)
		}
		return netprobe.PublicIP(ctx, "ipv4")
	case "auto:public-ipv6":
		if recordType != "AAAA" {
			return "", fmt.Errorf("auto:public-ipv6 requires AAAA record, got %s", recordType)
		}
		return netprobe.PublicIP(ctx, "ipv6")
	default:
		return content, nil
	}
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

func mapExits(cfg *model.Config) map[string]model.Exit {
	out := map[string]model.Exit{}
	for _, e := range cfg.Exits {
		out[e.Name] = e
	}
	return out
}

func hasErrors(diagnostics []planner.Diagnostic) bool {
	for _, d := range diagnostics {
		if d.Severity == planner.SeverityError {
			return true
		}
	}
	return false
}
