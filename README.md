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
- `--systemd` enables and restarts the auth unit when configured, plus generated frpc units when `settings.frp.manage_systemd` is true.

## Password protected websites

Lantern can protect individual HTTP/HTTPS bindings, including WebSocket upgrade
requests. Other bindings remain open. Set a distinct `auth_ref` on each binding
you want to protect; leave it empty to disable gateway authentication:

```yaml
settings:
  auth:
    public_url: https://auth.site-2.teclab.org.cn:10043
    listen: 127.0.0.1:9183
    session_file: /var/lib/lantern/auth-sessions.json
    binary_path: /usr/local/bin/lantern

bindings:
  - name: pve-lan
    # ...the existing binding fields...
    auth_ref: pve_lan_password
```

The public URL must route through Nginx to the local auth listener as an
**unprotected** HTTP service and binding. `lantern.yaml` includes this gateway
service for the site-2 deployment. It uses the existing `*.site-2` certificate.
Keep the listener bound to loopback and prevent direct public access to each
backend; otherwise clients can bypass the gateway.

Build and install a stable executable on the gateway host. After adding
`auth_ref` to a binding, set its password interactively on the host that holds
`secrets.yaml`, then apply the configuration:

```bash
go build -o lantern ./cmd/lantern
sudo install -m 0755 lantern /usr/local/bin/lantern
sudo /usr/local/bin/lantern auth set-password --config lantern.yaml --binding pve-lan
sudo /usr/local/bin/lantern apply --config lantern.yaml --yes --dns --systemd
```

This saves an Argon2id password hash in `auth_passwords` inside the configured
secrets file. Passwords are never stored in `lantern.yaml`. Changing a password
invalidates existing sessions for bindings using its `auth_ref`; restart the
auth service to load the new hash. Passwords must have at least 12 characters.

`--systemd` enables and restarts `lantern-auth.service` when
`settings.auth.public_url` is configured. The login flow is: password, then
credential lifetime (2 or 12 hours; 1, 7, 30, or 90 days; or no server-side
expiry). The "browser session only" option creates a session cookie without a
persistent browser expiration while keeping the selected server-side lifetime.
Browser session restore may preserve session cookies. Long-lived cookies may
also be removed by the browser, even when the server-side session has no expiry.

The central logout page is
`https://auth.site-2.teclab.org.cn:10043/logout`. It lists this browser's
active protected-site sessions and can revoke one site or all sites. A protected
site also redirects `/__lantern/account` to that page. This is a top-level
navigation, so the portal does not need CORS or access to HttpOnly cookies.
Logout blocks new requests and new WebSocket handshakes. Existing WebSocket
connections stay open until the application or transport closes them.

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
