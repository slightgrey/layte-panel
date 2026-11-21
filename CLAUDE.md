# CLAUDE.md — Layte Panel

**Layte Panel** — a lightweight control panel for unmanaged VPS. *By Web of D.*

Open-source, self-hosted server control panel for WordPress/PHP servers (a cPanel/SpinupWP/RunCloud alternative).
Ships as **one Go binary** with the React UI embedded inside it. Anyone can install it on their own Ubuntu server.

> Naming: product **Layte Panel** (UI, README, docs). Binary, service, system user/group, and paths use the short lowercase `layte` (like `docker` for Docker Desktop). Repo: `layte-panel`.
> Never use "cPanel" in the product name, tagline, logo, or package names (cPanel is a trademark). Mentioning it in README text as "a cPanel alternative" is fine.

## Owner preferences
- Keep explanations brief and to the point.
- The owner knows PHP/WordPress/React well and is **new to Go**. Write idiomatic, simple Go, and briefly explain Go-specific patterns the first time they appear (goroutines, contexts, interfaces, embed, error wrapping).
- Prefer the standard library over frameworks when it's reasonable.

## Scope
**v1 manages the server it is installed on (single server).** Multi-server support may come later, so keep the agent boundary clean.

Features:
- **Sites**: create (system user + nginx vhost + PHP-FPM pool + DB + optional WordPress install), details, delete, import existing
- **SSL**: issue/renew/revoke via certbot, expiry status
- **Vhosts**: view, edit with validation, PHP version per site
- **Databases**: MySQL/MariaDB databases + users, per-site credentials
- **Cron**: per-site cron jobs (run as the site's user)
- **Logs**: nginx access/error, PHP-FPM, site logs; live tail
- **File manager**: browse/upload/edit/delete, limited to each site's root
- **Security**: pending security updates, service status, firewall (ufw), failed logins
- **API**: scoped API keys so external apps (e.g. siteherd) can read data. Read-only by default.

Out of scope for v1: email hosting, DNS hosting, multi-server, Docker app hosting.

## Target environment
- Ubuntu 24.04 LTS (amd64 + arm64); Debian 12 is a stretch goal
- nginx, PHP-FPM (multiple versions via Ondřej Surý packages), MySQL 8 / MariaDB, certbot, systemd
- Must run comfortably on **1 vCPU / 1 GB RAM**. Target: under 40 MB RSS idle.
- Must work on servers that already have hand-written or Certbot-managed configs. **Never overwrite configs Layte Panel didn't create.**

## Architecture

```
Browser ── HTTPS ──▶ nginx (reverse proxy, IP allowlist) ──▶ layte serve   (user: layte, unprivileged)
                                                                 │  JSON over Unix socket
                                                                 ▼
                                                           layte agent   (root, allowlisted actions only)
                                                                 │  exec.Command (arg arrays)
                                                                 ▼
                                                  nginx · php-fpm · mysql · certbot · cron · ufw · fs
```

- **One binary, two modes**: `layte serve` (HTTP API + embedded UI, unprivileged) and `layte agent` (root helper).
  Both are systemd units.
- `serve` never runs privileged commands. It sends **named actions** to the agent over `/run/layte/agent.sock`
  (mode 0660, group `layte`).
- The agent exposes a **fixed registry of actions** (`site.create`, `nginx.test`, `nginx.reload`, `ssl.issue`,
  `cron.set`, `db.create`, `fs.list`, …). Each action has a typed request struct and strict validation.
  **No generic "run command" action, ever.**
- Long-running actions (site create, certbot, WP install) run as **jobs**: a job ID is returned right away,
  progress is streamed over SSE, and the final status is stored in SQLite.

## Tech stack
| Area | Choice |
|---|---|
| Language | Go (latest stable), `CGO_ENABLED=0` static builds |
| HTTP | stdlib `net/http` (method + path patterns); `chi` only if routing gets messy |
| DB (Layte Panel's own data) | SQLite via `modernc.org/sqlite` (pure Go, no CGO); SQL migrations embedded with `embed` |
| Queries | `sqlc` or plain `database/sql`. No heavy ORM. |
| Auth | Session cookies (HttpOnly, Secure, SameSite=Strict), argon2id passwords, TOTP 2FA (`pquerna/otp`), CSRF token |
| API keys | Random 32-byte tokens; store only a SHA-256 hash; scopes (`sites:read`, `ssl:read`, …) |
| Logging | `log/slog` (JSON in production) |
| Config | `/etc/layte/config.toml` + `LAYTE_*` env overrides |
| Frontend | React + TypeScript + Vite + Tailwind + shadcn/ui, TanStack Query, React Router |
| Templates | Go `text/template` for nginx vhost, FPM pool, systemd, cron (embedded) |

## Repo layout
```
cmd/layte/            main.go: subcommands serve | agent | install | version
internal/
  api/                HTTP handlers, middleware (auth, csrf, ratelimit, apikey)
  auth/               sessions, passwords, totp, api keys
  agent/              socket server, action registry, client used by api/
  actions/            one file per domain: site.go, nginx.go, php.go, ssl.go, db.go, cron.go, fs.go, logs.go, security.go
  system/             thin wrappers: exec runner, systemctl, apt, file writes with backup
  templates/          *.tmpl (nginx, fpm, cron) + embed.go
  store/              SQLite, migrations, queries
  jobs/               job runner + SSE progress
web/                  React app (Vite). Build output → web/dist (embedded)
deploy/               systemd units, nginx proxy example, install.sh
test/integration/     tests run inside an Ubuntu VM/container
docs/
```

## Security rules (non-negotiable)
1. **No shell.** Use `exec.CommandContext(ctx, bin, args...)` with absolute binary paths. Never `sh -c` and never string concatenation.
2. **Validate every input** in the agent, even if the API already did. Domains: strict regex + IDN normalisation, no `..`, no slashes.
   Usernames: `^[a-z][a-z0-9_-]{2,31}$`. PHP versions come from the installed list only.
3. **Filesystem access is jailed** with `os.Root` (Go 1.24+), rooted at the site's home. Reject symlinks that escape it.
   The file manager works as the site's user, never as root.
4. **Safe config writes** (`system.WriteConfig`): back up the existing file → write a temp file → `nginx -t` / `php-fpmX.Y -t` →
   atomic rename → reload → on failure, restore the backup and reload. One bad template must never take down other sites.
5. **Imported/unmanaged sites are read-only** until the user explicitly adopts them. Adopting shows a diff first.
6. Secrets (DB passwords, API keys, TOTP seeds) never appear in logs or API responses after creation. Encrypt TOTP seeds at rest.
7. Every **mutating** agent action is written to an **audit log** (who, what, args with secrets redacted, result).
   Read-only actions (dashboard polling etc.) are logged by the agent (slog/journald) only.
8. Login: rate limiting + lockout; 2FA required for admins; sessions expire.
9. Run `govulncheck` and `npm audit` in CI. Pin dependencies.

## Conventions
- Errors: wrap with context (`fmt.Errorf("create vhost %s: %w", domain, err)`); never `panic` in request paths.
- Pass `context.Context` as the first argument to anything that does I/O; timeouts on all exec calls.
- Interfaces belong where they're used (e.g. `system.Runner` so actions can be unit-tested with a fake runner).
- Table-driven tests. Golden files for rendered templates (`testdata/*.golden`).
- API: JSON, `/api/v1/...`, errors shaped as `{ "error": { "code": "...", "message": "..." } }`.
- Frontend: keep API types in `web/src/api/`, one TanStack Query hook per endpoint.
- Small PRs; Conventional Commits (`feat:`, `fix:`, `chore:`).

## Local development
```bash
# backend (unprivileged; agent calls go to a fake runner or a VM)
go run ./cmd/layte serve --dev

# frontend (Vite dev server proxies /api to the Go server)
cd web && npm install && npm run dev

# tests
go test ./...
cd web && npm test
```
- **Never run the agent against your own machine.** Real system actions are tested inside an Ubuntu 24.04 VM
  (Multipass or Vagrant), which has full systemd. Containers don't run systemd well. Example: `make vm-up && make test-integration`.
- `--dev` turns off the Secure-cookie requirement and enables CORS for the Vite origin only.

## Build & release
```bash
cd web && npm run build          # → web/dist
go build -trimpath -ldflags "-s -w -X main.version=$(git describe --tags)" -o layte ./cmd/layte
```
- Release with GoReleaser: linux/amd64 + linux/arm64 binaries, checksums, signed.
- `install.sh`: download the binary → create the `layte` user/group → set up systemd units → create the first admin → print the URL.
- Updates: replace the binary + restart both units; migrations run on startup.

## Roadmap
1. **Foundation**: binary skeleton, serve/agent split, socket protocol, auth + 2FA, dashboard (services, versions, disk/RAM, pending updates)
2. **Sites**: import existing sites (read-only), create site end-to-end, site details, PHP version switch
3. **SSL · Databases · Cron · Logs** (live tail via SSE)
4. **File manager · Security** page (updates, ufw, failed logins)
5. **Public API** with scoped keys + docs (for siteherd and other clients)
6. Installer, release pipeline, docs site

## Integration with siteherd (later)
siteherd is a separate Next.js app. It connects to Layte Panel with an API key (read-only scopes first) over
`https://<layte-host>/api/v1`, or over localhost if both run on the same server. Don't add anything specific
to siteherd to Layte Panel; keep the API generic.

## Open decisions
- License (MIT vs AGPL-3.0)
- MariaDB support level in v1
