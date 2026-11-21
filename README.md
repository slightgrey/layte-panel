# Layte Panel

A lightweight, self-hosted control panel for unmanaged VPS running WordPress/PHP sites. A cPanel alternative that ships as one Go binary with the web UI embedded. *By Web of D.*

> Status: early development.
> - Phase 1 (foundation) is done: login with 2FA and a read-only server dashboard (services, software versions, CPU/RAM/disk, pending updates).
> - Phase 2 (sites) is done: import existing nginx sites read-only; create sites (Linux user, PHP-FPM pool, nginx vhost, MySQL/MariaDB database, WordPress via WP-CLI); delete sites; switch PHP version per site. Each operation runs as a job with live progress and automatic rollback.

## Design in one paragraph
`layte serve` (unprivileged) serves the API and UI behind nginx. Anything that needs root goes over a Unix socket to `layte agent`, which runs only a fixed list of validated actions; there is no generic "run a command" action. Built for Ubuntu 24.04 servers with 1 vCPU and 1 GB RAM.

## Development

See [docs/development.md](docs/development.md).

```bash
go run ./cmd/layte agent --dev    # fake server data, never touches your machine
go run ./cmd/layte serve --dev
cd web && npm install && npm run dev
```
