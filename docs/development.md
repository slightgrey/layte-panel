# Development

## Requirements
- Go 1.26+ (see `go.mod`)
- Node.js 22+ and npm
- For real system testing: an Ubuntu 24.04 VM ([Multipass](https://multipass.run/) is easiest)

## How the pieces fit
```
browser ──▶ Vite dev server :5173 ──/api──▶ layte serve --dev :8443 ──unix socket──▶ layte agent --dev
                                                     │                                    │
                                              .dev/data/layte.db                fake Ubuntu host (embedded
                                              .dev/secret.key                   fixtures, never your machine)
```
`--dev` keeps all state in `./.dev/` (git-ignored), turns off the Secure cookie flag, allows
the Vite origin, and makes the agent work on a **fake root**, `.dev/fakeroot/`:
- It is seeded from `internal/actions/fixtures/`: a fake Ubuntu host with three existing nginx sites.
- Commands (`nginx -t`, `useradd`, `mysql`, WP-CLI…) are faked.
- Files the agent writes (vhosts, pools, site dirs) really land in the fake root, so you can inspect them.
- Delete `.dev/fakeroot` to start over. Missing fixture files are re-copied on every agent start.
- On Windows, symlinks are stored as small pointer files, because creating real symlinks needs admin rights.

## Run it locally
Each in its own terminal, from the repo root:

```bash
go run ./cmd/layte agent --dev
go run ./cmd/layte admin create --dev --username admin   # once; prints a 2FA secret for your authenticator app
go run ./cmd/layte serve --dev
cd web && npm install && npm run dev                     # open http://localhost:5173
```

Or serve the built UI from the Go binary itself (no Vite):

```bash
cd web && npm run build && cd ..
go run ./cmd/layte serve --dev                           # open http://127.0.0.1:8443
```

`go build` embeds whatever is in `web/dist`. Without a frontend build it still compiles
(thanks to `web/dist/.gitkeep`) and the panel shows a "UI not built" message.

## Tests
```bash
go vet ./... && go test ./...
cd web && npm test && npm run typecheck
```

## Admin accounts
```bash
layte admin create         --username NAME   # password + 2FA enrolment
layte admin reset-password --username NAME   # also clears lockout, logs out sessions
layte admin reset-2fa      --username NAME   # new authenticator secret
```
On a server, run these as the `layte` user so the database stays owned by it:
`sudo -u layte layte admin create --username alice`.

## Testing on a real server (VM)
**Never run the agent without `--dev` on your own machine.** Use a VM:

```bash
make vm-up       # Ubuntu 24.04, 1 vCPU / 1 GB, with nginx, php-fpm, mysql, certbot
make vm-deploy   # builds linux/amd64 and runs deploy/install.sh --binary in the VM (also tests the installer)
make vm-shell    # then: sudo -u layte layte admin create --username admin
```
Reach the panel with `multipass exec layte-dev -- curl -s localhost:8443/api/v1/health`,
or put `deploy/nginx-proxy.conf.example` in front of it.

`make test-integration` builds `test/integration` (build tag `integration`) for Linux and runs it
as root in the VM against the real agent. It:
1. Creates a WordPress site and checks that nginx serves it.
2. Switches PHP (when two versions are installed).
3. Deletes the site.
4. Checks that the user, the database and the configs are gone, that `nginx -t` passes, and that
   no foreign nginx or PHP config changed (checksums before and after).

## How site jobs work
Creating a site, deleting it and switching its PHP version are **jobs** ([internal/sites](../internal/sites)).
- Each job is a sequence of small agent actions (`site.user_create`, `php.pool_write`, `nginx.vhost_write`, …).
- If a step fails, the completed steps are undone in reverse order.
- Config files go through `system.WriteConfig`: backup → write → `nginx -t`/`php-fpm -t` → reload,
  restoring the backup on failure. Files without the `Managed by Layte Panel` marker are never touched.
- Progress streams to the UI over SSE (`/api/v1/jobs/{id}/events`).
- Generated passwords are held in memory and can be fetched once (`/api/v1/jobs/{id}/secret`).
  They are never written to SQLite, logs or command lines.

## Production layout
| Path | What | Owner / mode |
|---|---|---|
| `/usr/local/bin/layte` | the binary | root, 0755 |
| `/etc/layte/config.toml` | config (see `deploy/config.toml.example`) | root:layte, 0640 |
| `/etc/layte/secret.key` | encrypts 2FA secrets — **back it up** | layte, 0600 |
| `/var/lib/layte/layte.db` | SQLite database | layte, 0640 |
| `/run/layte/agent.sock` | serve → agent socket | root:layte, 0660 |

`layte serve` runs with `ProtectSystem=strict`, so it can't create `/etc/layte/secret.key`
itself; `deploy/install.sh` creates it (and the systemd units).

## SSL, databases, cron, logs
- **SSL**:
  - `certbot certonly --webroot` with the cert name `layte-<domain>`. certbot only drops a challenge file into `public/`; it never edits nginx configs.
  - Layte then re-renders its own vhost with the HTTPS block.
  - A deploy hook (`/etc/letsencrypt/renewal-hooks/deploy/layte-reload-nginx`) reloads nginx after certbot's timer renews a cert.
  - Certificates under 21 days are flagged, because certbot renews at 30 days.
  - Dev mode writes self-signed certs; the imported blog site's cert "expires" in 20 days.
- **Databases**:
  - Every database is listed with its size.
  - Only databases recorded in Layte's `databases` table can be dropped or have their password reset.
  - Site databases are dropped together with their site.
  - WordPress databases can't have their password reset here, because that would break `wp-config.php`.
- **Cron**:
  - One `/etc/cron.d/layte-<domain_with_underscores>` file per site.
  - Each line runs as the site user, from the site folder, appending output to `logs/cron.log`.
  - `%` characters are escaped automatically.
- **Logs**:
  - `logs.read` opens only nginx logs, `php*-fpm.log` and `/home/<user>/<domain>/logs/*.log`. Paths are checked again after following symlinks.
  - The live tail is serve polling the agent once a second, pushed to the browser over SSE.
  - The poll action is "quiet" (debug-level logging), so it doesn't flood the journal.

## File manager
- Every operation (list, read, write chunk, mkdir, rename, delete) runs in a short-lived helper, `layte fsop`. The agent starts it with the site user's uid/gid, so the kernel enforces that user's permissions. The helper refuses to run as root.
- The helper opens the site folder with `os.Root`, so no path or symlink can leave it. `..` is rejected outright.
- **Layte sites:** the jail is `/home/<user>/<domain>`, with read and write access.
- **Imported sites:** the jail is the web root from the nginx vhost. The agent checks that the root really belongs to that domain. Access is read-only, as the folder's owner (or `www-data` if root owns it).
- Uploads and saves are sent in 512 KB chunks to a temp file that replaces the target only after the last chunk, so a broken upload never corrupts a file.
- Only the first chunk of each file goes to the audit log, and file data is never stored there.
- In dev mode the operations run in-process on `.dev/fakeroot`.

## Security page
- **Firewall:**
  - `ufw status` is parsed; rules can be added (`ufw allow proto … from … to any port …`) or deleted by number.
  - A delete only goes ahead if the rule text still matches what the user saw.
  - Allow rules for the SSH port(s) (read from `sshd_config`), 80 and 443 can't be deleted.
  - Enabling ufw first allows those ports.
- **Updates:** "Install security updates" runs `unattended-upgrade` as a job (security pocket only, per the default config).
- **Failed SSH logins:** read from the last 2 MB of `/var/log/auth.log` and grouped by IP.

## Adding an agent action
1. Add a handler in `internal/actions/<domain>.go` with a typed params struct that implements
   `Validate() error`. Validate **everything** here, even if the API already did.
2. Register it in `actions.Register` with `agent.Def(name, readOnly, timeout, fn)`.
   Mutating actions (`readOnly=false`) are written to the audit log automatically.
3. Run binaries with absolute paths through `deps.Runner`; never a shell. Put the path in
   `internal/actions/bins.go`. Secrets go in `Cmd.Stdin`, never in `Args`.
4. Touch files only through `deps.FS` (it's jailed with `os.Root`), and change configs only via
   `system.WriteConfig` / `system.RemoveConfig`.
5. Add fixtures under `internal/actions/fixtures/` (and a fake answer in `dev.go`) so tests and
   `agent --dev` work.
