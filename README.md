# Lantern

Lantern is a single-host terminal gateway manager.

It keeps a YAML source of truth for internal services, domain zones, exits,
service bindings, Nginx reverse proxy config, Cloudflare DNS records, ACME
certificates, and frpc config.

It does not manage remote servers. If a site-1 server runs frps, configure frps
there yourself. Lantern only manages the host where it is installed.

## Model

Lantern has four main objects:

- `services`: internal targets, with protocol `http`, `https`, `tcp`, or `udp`
- `domains`: Cloudflare zones and token references
- `exits`: direct LAN exits or frp exits
- `bindings`: service-to-exit publications with hostnames, SSL, orange-cloud, and optional external ports

HTTP and HTTPS services are rendered as Nginx virtual hosts. TCP and UDP services
are rendered as frpc port proxies when they are bound to an frp exit.

## Quick Start

```bash
go run ./cmd/lantern init --config lantern.yaml
go run ./cmd/lantern tui --config lantern.yaml
go run ./cmd/lantern plan --config lantern.yaml
```

The TUI is modeled after `nmtui`: tabs across the top, a list on the left,
details on the right, and shortcut hints at the bottom.

Useful keys:

- arrows or `hjkl`: move
- `Enter` or `e`: edit selected item
- `a`: add item
- `d`: delete item
- `p`: plan preview
- `s`: save YAML
- `q`: quit

Render generated config without writing it:

```bash
go run ./cmd/lantern render nginx --config lantern.yaml
go run ./cmd/lantern frp render --config lantern.yaml
go run ./cmd/lantern dns plan --config lantern.yaml
go run ./cmd/lantern cert plan --config lantern.yaml
```

For local testing, the example config writes generated files under `.lantern/`.

## Apply

`apply` always requires `--yes`.

```bash
go run ./cmd/lantern apply --config lantern.yaml --yes
```

Optional stages are explicit:

```bash
go run ./cmd/lantern apply --config lantern.yaml --yes --dns
go run ./cmd/lantern apply --config lantern.yaml --yes --certs
go run ./cmd/lantern apply --config lantern.yaml --yes --systemd
```

- `--dns` syncs Cloudflare DNS.
- `--force-dns` updates a single existing DNS record when its content differs.
- `--certs` runs `lego renew` with Cloudflare DNS-01.
- `--systemd` enables and restarts generated frpc units only when `settings.frp.manage_systemd` is true.

## DNS

Cloudflare tokens live in `secrets.yaml`; `lantern.yaml` only stores token refs.

Supported target values:

- ordinary IP or CNAME values
- `auto:public-ipv4` for A records
- `auto:public-ipv6` for AAAA records

DNS sync creates missing records, no-ops matching records, reports duplicates,
and requires `--force` to update a single conflicting record:

```bash
go run ./cmd/lantern dns sync --config lantern.yaml --yes --force
```

## Certificates

The first ACME provider is `lego` with Cloudflare DNS-01.

Preview commands:

```bash
go run ./cmd/lantern cert issue --config lantern.yaml
go run ./cmd/lantern cert renew --config lantern.yaml
```

Run them:

```bash
go run ./cmd/lantern cert issue --config lantern.yaml --yes
go run ./cmd/lantern cert renew --config lantern.yaml --yes
```

Lantern generates Nginx certificate paths using lego's `--pem` layout.

## frp

Generate frpc config and units:

```bash
go run ./cmd/lantern frp render --config lantern.yaml
```

Generate an install script:

```bash
go run ./cmd/lantern frp install-script --config lantern.yaml --version v0.61.0
```

Run the install script:

```bash
go run ./cmd/lantern frp install --config lantern.yaml --version v0.61.0 --yes
```

## Deploy Shape

On the Docker host `192.168.1.4`, a typical production config would use:

```yaml
settings:
  nginx:
    generated_dir: /etc/nginx/conf.d/lantern
    test_command: nginx -t
    reload_command: systemctl reload nginx
  acme:
    enabled: true
    provider: lego
    cert_dir: /etc/lantern/certs
  frp:
    install_dir: /opt/frp
    config_dir: /etc/frp
    systemd_dir: /etc/systemd/system
    manage_systemd: true
```

OpenWrt should forward only 80/443 to this host for direct LAN/public web entry.

## Status

Implemented:

- YAML config and secrets files
- simple terminal menu for add/edit/delete
- validation and plan output
- Nginx HTTP/HTTPS generation with websocket, buffering, timeout, Range modes
- frpc TOML generation for HTTPS gateway and TCP/UDP bindings
- generated systemd unit files
- frp install script generation and explicit install command
- Cloudflare DNS planning and sync
- ACME lego command planning and execution

Still worth hardening before real production:

- config backup and rollback
- richer TUI keyboard navigation
- safer generated-file pruning for removed bindings
- first-class Nginx stream generation for non-frp TCP services
- service health checks
