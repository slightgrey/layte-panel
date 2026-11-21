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
the Vite origin, and makes the agent answer from `internal/actions/fixtures/` instead of running
real commands.

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
make vm-deploy   # builds linux/amd64, installs binary + systemd units, starts both
make vm-shell    # then: sudo -u layte layte admin create --username admin
```
Reach the panel with `multipass exec layte-dev -- curl -s localhost:8443/api/v1/health`,
or put `deploy/nginx-proxy.conf.example` in front of it.

## Production layout
| Path | What | Owner / mode |
|---|---|---|
| `/usr/local/bin/layte` | the binary | root, 0755 |
| `/etc/layte/config.toml` | config (see `deploy/config.toml.example`) | root:layte, 0640 |
| `/etc/layte/secret.key` | encrypts 2FA secrets — **back it up** | layte, 0600 |
| `/var/lib/layte/layte.db` | SQLite database | layte, 0640 |
| `/run/layte/agent.sock` | serve → agent socket | root:layte, 0660 |

`layte serve` runs with `ProtectSystem=strict`, so it can't create `/etc/layte/secret.key`
itself; the installer (or `make vm-deploy`) creates it.

## Adding an agent action
1. Add a handler in `internal/actions/<domain>.go` with a typed params struct that implements
   `Validate() error`. Validate **everything** here, even if the API already did.
2. Register it in `actions.Register` with `agent.Def(name, readOnly, timeout, fn)`.
   Mutating actions (`readOnly=false`) are written to the audit log automatically.
3. Run binaries with absolute paths through `deps.Runner`; never a shell.
4. Add fixtures under `internal/actions/fixtures/` so tests and `agent --dev` work.
