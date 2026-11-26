# Installing Layte Panel

## Requirements
- A supported Linux (see the table below). amd64 is tested; arm64 builds are published but not yet tested on real hardware.
- At least **1 vCPU and 1 GB RAM**. The panel itself uses about 15 MB.
- Root access (sudo).
- A domain name for the panel, such as `panel.example.com`, whose DNS points to the server. You can also skip this and reach the panel through an SSH tunnel.

### Supported systems
Each one below passed a full install (`--with-stack`) and the integration tests: a WordPress site created, one-click WP-Admin login, page cache, Redis object cache, SFTP, the dashboard, updates and security pages, then site deletion.

| System | Status | Default stack (`--with-stack`) |
|---|---|---|
| **Ubuntu 24.04 LTS** | Supported (main target; also runs in production) | PHP 8.3, MySQL 8.0, nginx 1.24 |
| **Ubuntu 26.04 LTS** | Supported | PHP 8.5, MySQL 8.4, nginx 1.28 |
| **Debian 13** (trixie) | Supported | PHP 8.4, MariaDB 11.8, nginx 1.26 |
| Ubuntu 22.04 LTS | Works, not recommended: its PHP 8.1 no longer gets security fixes from PHP | PHP 8.1, MySQL 8.0, nginx 1.18 |
| Debian 12 | Should work, not tested | PHP 8.2, MariaDB 10.11, nginx 1.22 |

The installer warns about anything not marked as supported. The stack is the distribution's own packages; to run other PHP versions next to the default one, add them yourself (for example from [ondrej/php](https://launchpad.net/~ondrej/+archive/ubuntu/php) on Ubuntu). Layte finds every installed PHP-FPM version.

Layte Panel works on servers that already host sites. It never changes nginx, PHP or Certbot configs it didn't create; existing sites show up as **imported** and stay read-only.

## Quick install
```bash
curl -fsSL https://raw.githubusercontent.com/slightgrey/layte-panel/main/deploy/install.sh | sudo bash
```
The installer asks a few questions:
- Whether to install the web stack: nginx, the system's PHP-FPM, MySQL (Ubuntu) or MariaDB (Debian), certbot, cron, ufw and unattended-upgrades. It skips this question if nginx and PHP are already installed.
- Which domain to use for the panel.
- An email address for Let's Encrypt.
- The name of the first admin user.

Creating the admin asks for a password (at least 12 characters) and shows a **two-factor secret**. Add it to an authenticator app (1Password, Bitwarden, Authy, Google Authenticator, …) and type the 6-digit code to confirm.

### Unattended
```bash
curl -fsSL https://raw.githubusercontent.com/slightgrey/layte-panel/main/deploy/install.sh -o install.sh
sudo bash install.sh --with-stack --panel-domain panel.example.com --email you@example.com \
  --allow-ip 203.0.113.10 --admin alice --yes
```

| Option | |
|---|---|
| `--version vX.Y.Z` | Install a specific release (default: latest) |
| `--with-stack` / `--no-stack` | Install nginx, PHP-FPM, MySQL/MariaDB, certbot, cron, ufw and unattended-upgrades, or don't (and don't ask) |
| `--panel-domain NAME` | Serve the panel at `https://NAME`, with a Let's Encrypt certificate |
| `--email ADDR` | Let's Encrypt account email |
| `--allow-ip IP/CIDR` | Only these addresses can reach the panel (repeatable). Strongly recommended. |
| `--admin NAME` | Create this admin. It still asks for the password and 2FA on the terminal. |
| `--yes` | Don't ask questions |
| `--binary PATH` | Install a locally built binary (for testing) |

The installer checks the release's SHA-256 checksum. If [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is installed on the server, it also verifies the checksum file's signature, which ties the release to this repository's GitHub Actions.

## Reaching the panel
The panel only listens on `127.0.0.1:8443`, so it is never exposed directly.

- **With a domain** (`--panel-domain`): nginx serves it at `https://panel.example.com`. The nginx config is `/etc/nginx/sites-available/layte-panel.conf` (or `/etc/nginx/conf.d/layte-panel.conf` when your `nginx.conf` only includes `conf.d`); it is written by the installer and marked as managed by Layte Panel. Use `--allow-ip` to limit who can see the login page.
- **Without a domain**: use an SSH tunnel from your computer, then open <http://localhost:8443>.
  ```bash
  ssh -L 8443:127.0.0.1:8443 you@your-server
  ```

Logins need HTTPS or `localhost`, because session cookies are `Secure`.

## What gets installed
| Path | |
|---|---|
| `/usr/local/bin/layte` | The binary: web UI, API, agent |
| `/etc/layte/config.toml` | Settings; `LAYTE_*` environment variables override them |
| `/etc/layte/secret.key` | Encrypts 2FA secrets. **Back it up.** |
| `/var/lib/layte/layte.db` | Layte Panel's own database (SQLite). **Back it up.** |
| `layte.service` | Web UI + API, running as the unprivileged `layte` user |
| `layte-agent.service` | The root helper. It runs only a fixed list of validated actions. |

## More admins and account recovery
```bash
sudo -u layte layte admin create --username bob
sudo -u layte layte admin reset-password --username alice   # also clears a lockout
sudo -u layte layte admin reset-2fa --username alice         # lost phone
```

Next: [upgrading, backups and uninstalling](upgrade.md) · [security model](security.md) · [troubleshooting](troubleshooting.md) · [API](api.md)
