# Security model

## Architecture
```
Browser ── HTTPS ──▶ nginx (TLS, optional IP allowlist) ──▶ layte serve  (user: layte, no privileges)
                                                                 │  JSON over /run/layte/agent.sock (0660, group layte)
                                                                 ▼
                                                           layte agent  (root, fixed list of actions)
                                                                 │  exec with argument lists, never a shell
                                                                 ▼
                                           nginx · php-fpm · mysql · certbot · cron · ufw · files
```

**The web-facing process has no privileges.**
- `layte serve` runs as `layte`, with a tight systemd sandbox: read-only system, no capabilities.
- It can't change anything on the server itself. It can only ask the agent to run one of its named actions, such as `nginx.vhost_write` or `db.create`.
- There is no "run this command" action.

**The agent trusts nothing.**
- Every request goes into a typed struct that rejects unknown fields, and is validated again even though the panel already checked it:
  - domains: IDN normalisation and a strict label regex
  - usernames: `^[a-z][a-z0-9_-]{2,31}$`
  - PHP versions: only installed ones
- Programs are started by absolute path with argument lists, with a clean environment and timeouts.
- The socket only accepts root or members of the `layte` group (file mode plus a kernel `SO_PEERCRED` check).

## What Layte Panel will and won't touch
- **Ownership marker:** every file it writes starts with `# Managed by Layte Panel` (`;` for PHP-FPM pools). It refuses to overwrite or delete files without that marker, so hand-written and Certbot-managed configs are safe.
- **Safe config changes:** each change is backed up, written atomically, then checked with `nginx -t` / `php-fpm -t`. On failure the old file is restored and the service keeps running.
- **Imported sites are read-only:** sites found on the server can be viewed and their files downloaded, but nothing is changed.
- **Unprivileged site work:** site files are handled by a helper process (`layte fsop`) running **as the site's own Linux user**, jailed to the site folder with `os.Root`. WP-CLI and cron jobs also run as the site user, never as root.

## Logins
- **Passwords:** hashed with argon2id (OWASP parameters).
- **2FA:** TOTP is mandatory. Codes can't be reused.
- **Brute force:** 5 failed attempts lock the account for 15 minutes, and each IP is rate-limited on the login endpoints.
- **Sessions:** cookies are `HttpOnly`, `Secure` and `SameSite=Strict`. The token changes after 2FA. Sessions end after 30 minutes idle or 12 hours in total.
- **CSRF:** every state-changing request needs a per-session token and a matching `Origin`.

## Secrets
- **Generated passwords** (databases, WordPress admin):
  - Shown **once**, then wiped from memory.
  - Never stored in SQLite, logs or command lines; they're passed to `mysql` and WP-CLI on stdin.
- **Encryption at rest:** 2FA secrets are encrypted with AES-256-GCM using `/etc/layte/secret.key`.
- **API keys:** only their SHA-256 hash is stored, and keys are read-only.

## Audit log
Every change made through the agent (who, what, arguments with secrets redacted, result) is written to the audit log in `/var/lib/layte/layte.db`, as are API key creation and revocation. Read-only actions are logged to the journal (`journalctl -u layte-agent`).

## Recommended hardening
- **Restrict the panel:** use `--allow-ip` (or edit `allow` lines in `layte-panel.conf`) so only your IPs can reach it, or skip the domain and use an SSH tunnel.
- **SSH:** use key-only logins (`PasswordAuthentication no`) and consider fail2ban.
- **Firewall:** keep ufw on. The Security page won't let you remove the rules for SSH, 80 or 443.
- **Backups:** back up `/etc/layte/secret.key` and the database (see [upgrade.md](upgrade.md)).

## Reporting a vulnerability
Please don't open a public issue. Use GitHub's **private vulnerability reporting** on the repository (Security → Report a vulnerability).
