# Installing Layte Panel

## Requirements
- **Ubuntu 24.04 LTS** (amd64 or arm64). Debian 12 should work but gets less testing.
- At least **1 vCPU and 1 GB RAM**. The panel itself uses about 15 MB.
- Root access (sudo).
- A domain name for the panel, such as `panel.example.com`, whose DNS points to the server. You can also skip this and reach the panel through an SSH tunnel.

Layte Panel works on servers that already host sites. It never changes nginx, PHP or Certbot configs it didn't create; existing sites show up as **imported** and stay read-only.

## Quick install
```bash
curl -fsSL https://raw.githubusercontent.com/slightgrey/layte-panel/main/deploy/install.sh | sudo bash
```
The installer asks a few questions:
- Whether to install the web stack (nginx, PHP 8.3-FPM, MySQL 8, certbot, ufw, unattended-upgrades). It skips this question if nginx and PHP are already installed.
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
| `--with-stack` / `--no-stack` | Install nginx, PHP 8.3-FPM, MySQL, certbot, ufw and unattended-upgrades, or don't (and don't ask) |
| `--panel-domain NAME` | Serve the panel at `https://NAME`, with a Let's Encrypt certificate |
| `--email ADDR` | Let's Encrypt account email |
| `--allow-ip IP/CIDR` | Only these addresses can reach the panel (repeatable). Strongly recommended. |
| `--admin NAME` | Create this admin. It still asks for the password and 2FA on the terminal. |
| `--yes` | Don't ask questions |
| `--binary PATH` | Install a locally built binary (for testing) |

The installer checks the release's SHA-256 checksum. If [cosign](https://docs.sigstore.dev/cosign/system_config/installation/) is installed on the server, it also verifies the checksum file's signature, which ties the release to this repository's GitHub Actions.

## Reaching the panel
The panel only listens on `127.0.0.1:8443`, so it is never exposed directly.

- **With a domain** (`--panel-domain`): nginx serves it at `https://panel.example.com`. The nginx config is `/etc/nginx/sites-available/layte-panel.conf`; it is written by the installer and marked as managed by Layte Panel. Use `--allow-ip` to limit who can see the login page.
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
