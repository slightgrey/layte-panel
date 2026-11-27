#!/usr/bin/env bash
# Layte Panel installer — Ubuntu 24.04/26.04 and Debian 13 (tested), amd64/arm64.
#
#   curl -fsSL https://raw.githubusercontent.com/slightgrey/layte-panel/main/deploy/install.sh | sudo bash
#
# Re-running it upgrades Layte Panel in place (binary + units; config, data and
# keys are kept). It never overwrites configs it didn't create.
#
# Options:
#   --version vX.Y.Z      install this release (default: latest)
#   --binary PATH         install a local binary instead of downloading (testing)
#   --with-stack          also install nginx, PHP-FPM, MySQL, certbot, ufw, unattended-upgrades
#   --no-stack            don't install the stack (and don't ask)
#   --admin NAME          create this admin user (asks for password + 2FA)
#   --panel-domain NAME   serve the panel at https://NAME via nginx (Let's Encrypt)
#   --email ADDR          Let's Encrypt account email (with --panel-domain)
#   --allow-ip IP|CIDR    only these addresses may reach the panel (repeatable)
#   --yes                 don't ask questions; use the options given
set -euo pipefail

REPO="slightgrey/layte-panel"
BIN=/usr/local/bin/layte
ETC=/etc/layte
DATA=/var/lib/layte
UNIT_DIR=/etc/systemd/system
MARKER="# Managed by Layte Panel"

VERSION="" LOCAL_BINARY="" STACK="ask" ADMIN="" ADMIN_SET=0 PANEL_DOMAIN="" EMAIL="" YES=0
ALLOW_IPS=()

say()  { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33mwarning:\033[0m %s\n' "$*" >&2; }
die()  { printf '\033[1;31merror:\033[0m %s\n' "$*" >&2; exit 1; }

while [ $# -gt 0 ]; do
  case "$1" in
    --version)      VERSION="${2:?}"; shift 2 ;;
    --binary)       LOCAL_BINARY="${2:?}"; shift 2 ;;
    --with-stack)   STACK="yes"; shift ;;
    --no-stack)     STACK="no"; shift ;;
    --admin)        [ $# -ge 2 ] || die "--admin needs a value ('' = don't create one)"
                    ADMIN="$2"; ADMIN_SET=1; shift 2 ;;
    --panel-domain) PANEL_DOMAIN="${2:?}"; shift 2 ;;
    --email)        EMAIL="${2:?}"; shift 2 ;;
    --allow-ip)     ALLOW_IPS+=("${2:?}"); shift 2 ;;
    --yes|-y)       YES=1; shift ;;
    -h|--help)      sed -n '2,20p' "$0"; exit 0 ;;
    *)              die "unknown option: $1 (see --help)" ;;
  esac
done

# Questions read from the terminal, so `curl … | sudo bash` still works.
TTY=/dev/tty
ask() { # ask "question" default → echoes answer
  local q="$1" def="$2" a=""
  if [ "$YES" = 1 ] || [ ! -r "$TTY" ]; then echo "$def"; return; fi
  read -r -p "$q [$def] " a < "$TTY" || true
  echo "${a:-$def}"
}

# ---------------------------------------------------------------- checks
[ "$(id -u)" = 0 ] || die "run as root (sudo bash install.sh)"
command -v systemctl >/dev/null || die "systemd is required"
# Read single fields from os-release. Don't source it: it defines VERSION,
# which would overwrite our own release VERSION.
os_field() { sed -n "s/^$1=//p" /etc/os-release | tr -d '"'; }
OS_ID="$(os_field ID)"
OS_VERSION_ID="$(os_field VERSION_ID)"
# Tested: Ubuntu 24.04 and 26.04, Debian 13 (see docs/install.md).
case "${OS_ID}-${OS_VERSION_ID}" in
  ubuntu-24.04|ubuntu-26.04|debian-13) ;;
  ubuntu-22.04) warn "Ubuntu 22.04 works, but its PHP 8.1 no longer gets security fixes; prefer Ubuntu 24.04 or newer" ;;
  debian-12) warn "Debian 12 should work but isn't tested; Debian 13 is" ;;
  *) warn "untested OS: $(os_field PRETTY_NAME). Layte Panel is tested on Ubuntu 24.04/26.04 and Debian 13." ;;
esac
case "$(uname -m)" in
  x86_64)  ARCH=amd64 ;;
  aarch64) ARCH=arm64 ;;
  *) die "unsupported CPU architecture: $(uname -m)" ;;
esac
[[ "$ADMIN" =~ ^([a-z][a-z0-9_-]{2,31})?$ ]] || die "invalid --admin name"
[[ "$PANEL_DOMAIN" =~ ^([a-z0-9]([a-z0-9-]*[a-z0-9])?\.)*([a-z0-9]([a-z0-9-]*[a-z0-9])?)?$ ]] || die "invalid --panel-domain"
for ip in "${ALLOW_IPS[@]}"; do
  [[ "$ip" =~ ^[0-9a-fA-F.:]+(/[0-9]{1,3})?$ ]] || die "invalid --allow-ip $ip"
done

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export DEBIAN_FRONTEND=noninteractive

# ---------------------------------------------------------------- stack
if [ "$STACK" = ask ]; then
  if command -v nginx >/dev/null && ls /etc/php/*/fpm >/dev/null 2>&1; then
    STACK=no
  else
    a="$(ask "Install the web stack (nginx, PHP-FPM, MySQL/MariaDB, certbot, ufw)?" "Y")"
    case "$a" in [Yy]*) STACK=yes ;; *) STACK=no ;; esac
  fi
fi
if [ "$STACK" = yes ]; then
  # The distribution's own default PHP (Ubuntu 24.04: 8.3, 26.04: 8.5;
  # Debian 13: 8.4) and database (MySQL on Ubuntu, MariaDB on Debian).
  # More PHP versions can be added later (e.g. from the ondrej/php PPA).
  say "Installing nginx, PHP-FPM, MySQL/MariaDB, certbot, cron, ufw, unattended-upgrades"
  apt-get update -q
  apt-get install -y -q nginx \
    php-fpm php-cli php-mysql php-curl php-gd php-mbstring php-xml php-zip php-intl php-bcmath \
    default-mysql-server certbot ufw unattended-upgrades curl ca-certificates
  # cron runs the sites' cron jobs (Debian's cloud images don't ship it).
  # Without its recommends: on Debian those pull in a mail server (exim4).
  # quota: disk limits for site logins (turned on later, from Users → Disk quotas).
  apt-get install -y -q --no-install-recommends cron quota
  PHP_FPM_UNIT="$(systemctl list-unit-files 'php*-fpm.service' --no-legend 2>/dev/null | awk '{print $1}' | sort -V | tail -n1)"
  DB_UNIT=mysql
  systemctl list-unit-files mariadb.service --no-legend 2>/dev/null | grep -q mariadb && DB_UNIT=mariadb
  systemctl enable --now nginx "$PHP_FPM_UNIT" "$DB_UNIT" cron >/dev/null
  say "Installed $(php -r 'echo "PHP ", PHP_MAJOR_VERSION, ".", PHP_MINOR_VERSION;' 2>/dev/null) and ${DB_UNIT}"
fi
[ -x /usr/sbin/cron ] || warn "cron isn't installed, so sites' cron jobs won't run (apt-get install cron)"

# ---------------------------------------------------------------- binary
if [ -n "$LOCAL_BINARY" ]; then
  say "Installing local binary $LOCAL_BINARY"
  install -m 0755 "$LOCAL_BINARY" "$TMP/layte"
else
  command -v curl >/dev/null || { apt-get update -q && apt-get install -y -q curl ca-certificates; }
  if [ -z "$VERSION" ]; then
    VERSION="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n1)"
    [ -n "$VERSION" ] || die "couldn't find the latest release (use --version vX.Y.Z)"
  fi
  [[ "$VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([-.][0-9A-Za-z.]+)?$ ]] || die "invalid version $VERSION"
  NUM="${VERSION#v}"
  BASE="https://github.com/$REPO/releases/download/$VERSION"
  ARCHIVE="layte_${NUM}_linux_${ARCH}.tar.gz"
  say "Downloading Layte Panel $VERSION ($ARCH)"
  curl -fsSL -o "$TMP/$ARCHIVE" "$BASE/$ARCHIVE"
  curl -fsSL -o "$TMP/checksums.txt" "$BASE/checksums.txt"
  # Signature (cosign keyless, GitHub Actions identity): verified if cosign is installed.
  if command -v cosign >/dev/null; then
    curl -fsSL -o "$TMP/checksums.txt.sig" "$BASE/checksums.txt.sig"
    curl -fsSL -o "$TMP/checksums.txt.pem" "$BASE/checksums.txt.pem"
    cosign verify-blob --certificate "$TMP/checksums.txt.pem" --signature "$TMP/checksums.txt.sig" \
      --certificate-identity-regexp "^https://github.com/$REPO/" \
      --certificate-oidc-issuer https://token.actions.githubusercontent.com "$TMP/checksums.txt" >/dev/null \
      || die "signature check of checksums.txt FAILED"
    say "Signature verified"
  fi
  (cd "$TMP" && grep " $ARCHIVE\$" checksums.txt | sha256sum -c --quiet -) || die "checksum mismatch for $ARCHIVE"
  tar -xzf "$TMP/$ARCHIVE" -C "$TMP" layte
fi
"$TMP/layte" version >/dev/null || die "the downloaded binary doesn't run on this machine"

# ---------------------------------------------------------------- user, dirs, config
getent group layte >/dev/null || groupadd --system layte
id layte >/dev/null 2>&1 || useradd --system --gid layte --home-dir "$DATA" --no-create-home --shell /usr/sbin/nologin layte
install -d -m 0750 -o root -g layte "$ETC"
install -d -m 0750 -o layte -g layte "$DATA"

if [ ! -f "$ETC/config.toml" ]; then
  cat > "$ETC/config.toml" <<'EOF'
# Layte Panel configuration. Every key can also be set as LAYTE_<KEY> (e.g. LAYTE_LISTEN).
listen          = "127.0.0.1:8443"        # keep on loopback; nginx (or an SSH tunnel) in front
data_dir        = "/var/lib/layte"
agent_socket    = "/run/layte/agent.sock"
secret_key_file = "/etc/layte/secret.key" # encrypts 2FA secrets: back it up
trusted_proxies = ["127.0.0.1", "::1"]
session_idle    = "30m"
session_max     = "12h"
EOF
  chown root:layte "$ETC/config.toml"; chmod 0640 "$ETC/config.toml"
fi
if [ ! -f "$ETC/secret.key" ]; then
  head -c 32 /dev/urandom > "$ETC/secret.key"
fi
chown layte:layte "$ETC/secret.key"; chmod 0600 "$ETC/secret.key"

# ---------------------------------------------------------------- services
UPGRADE=0
[ -x "$BIN" ] && UPGRADE=1
install -m 0755 "$TMP/layte" "$BIN"

write_unit() { # write_unit name — systemd units are ours; replaced on upgrade
  cat > "$UNIT_DIR/$1.service"
}
write_unit layte-agent <<EOF
$MARKER — reinstalled by install.sh
[Unit]
Description=Layte Panel agent (privileged helper)
After=network.target

[Service]
# Runs as root (the default). Don't set User=root explicitly: together with
# NoNewPrivileges, systemd then drops CAP_SETUID and the agent can't start
# helpers as site users.
Group=layte
ExecStart=$BIN agent --config $ETC/config.toml
Restart=on-failure
RestartSec=2s
RuntimeDirectory=layte
RuntimeDirectoryMode=0750
NoNewPrivileges=true
PrivateTmp=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectClock=true
LockPersonality=true
RestrictRealtime=true
SystemCallArchitectures=native
UMask=0022

[Install]
WantedBy=multi-user.target
EOF
write_unit layte <<EOF
$MARKER — reinstalled by install.sh
[Unit]
Description=Layte Panel (web UI and API)
After=network.target layte-agent.service
Wants=layte-agent.service

[Service]
User=layte
Group=layte
ExecStart=$BIN serve --config $ETC/config.toml
Restart=on-failure
RestartSec=2s
StateDirectory=layte
StateDirectoryMode=0750
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
PrivateDevices=true
ProtectKernelTunables=true
ProtectKernelModules=true
ProtectKernelLogs=true
ProtectControlGroups=true
ProtectClock=true
ProtectHostname=true
RestrictNamespaces=true
RestrictRealtime=true
RestrictSUIDSGID=true
LockPersonality=true
MemoryDenyWriteExecute=true
SystemCallArchitectures=native
CapabilityBoundingSet=
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
UMask=0027

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
if [ "$UPGRADE" = 1 ]; then
  say "Restarting Layte Panel (database migrations run on start)"
  systemctl restart layte-agent layte
else
  systemctl enable --now layte-agent layte >/dev/null
fi
for _ in $(seq 1 20); do
  curl -fsS http://127.0.0.1:8443/api/v1/health >/dev/null 2>&1 && break
  sleep 0.5
done
curl -fsS http://127.0.0.1:8443/api/v1/health >/dev/null 2>&1 || die "layte didn't start: journalctl -u layte -u layte-agent"

# ---------------------------------------------------------------- panel behind nginx
# Put the vhost where this nginx actually loads it: some servers only include
# conf.d/*.conf, and a file in sites-enabled would pass `nginx -t` but never load.
NGINX_CONF=/etc/nginx/sites-available/layte-panel.conf
NGINX_LINK=/etc/nginx/sites-enabled/layte-panel.conf
# (nginx -T goes into a variable: piping it into `grep -q` trips pipefail on SIGPIPE.)
NGINX_DUMP=""
if command -v nginx >/dev/null; then NGINX_DUMP="$(nginx -T 2>/dev/null || true)"; fi
if [ -f /etc/nginx/conf.d/layte-panel.conf ] || { [ -n "$NGINX_DUMP" ] &&
     ! grep -qE '^[[:space:]]*include[[:space:]]+/etc/nginx/sites-enabled/' <<<"$NGINX_DUMP"; }; then
  NGINX_CONF=/etc/nginx/conf.d/layte-panel.conf
  NGINX_LINK=""
fi
panel_vhost() { # panel_vhost http|https
  local allow="" ip
  for ip in "${ALLOW_IPS[@]}"; do allow+="    allow $ip;"$'\n'; done
  [ -n "$allow" ] && allow+="    deny all;"$'\n'
  {
    echo "$MARKER — the panel itself (written by install.sh)"
    echo "server {"
    echo "    listen 80;"
    echo "    listen [::]:80;"
    echo "    server_name $PANEL_DOMAIN;"
    echo "    location ^~ /.well-known/acme-challenge/ { root /var/www/layte-acme; }"
    if [ "$1" = https ]; then
      echo "    location / { return 301 https://\$host\$request_uri; }"
      echo "}"
      echo "server {"
      echo "    listen 443 ssl http2;"
      echo "    listen [::]:443 ssl http2;"
      echo "    server_name $PANEL_DOMAIN;"
      echo "    ssl_certificate     /etc/letsencrypt/live/layte-panel/fullchain.pem;"
      echo "    ssl_certificate_key /etc/letsencrypt/live/layte-panel/privkey.pem;"
      echo "    ssl_protocols TLSv1.2 TLSv1.3;"
      echo "    add_header Strict-Transport-Security \"max-age=63072000\" always;"
    fi
    printf '%s' "$allow"
    echo "    client_max_body_size 256m;"
    echo "    location / {"
    echo "        proxy_pass http://127.0.0.1:8443;"
    echo "        proxy_http_version 1.1;"
    echo "        proxy_set_header Host \$host;"
    echo "        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;"
    echo "        proxy_set_header X-Forwarded-Proto \$scheme;"
    echo "        proxy_buffering off;"
    echo "        proxy_read_timeout 1h;"
    echo "    }"
    echo "}"
  } > "$TMP/panel.conf"
  if [ -f "$NGINX_CONF" ] && ! head -n1 "$NGINX_CONF" | grep -q "^$MARKER"; then
    die "$NGINX_CONF exists and wasn't written by Layte Panel; not touching it"
  fi
  [ -f "$NGINX_CONF" ] && cp "$NGINX_CONF" "$TMP/panel.conf.bak"
  install -m 0644 "$TMP/panel.conf" "$NGINX_CONF"
  if [ -n "$NGINX_LINK" ]; then
    ln -sf "$NGINX_CONF" "$NGINX_LINK"
  else # drop a copy we wrote earlier in sites-available (never loaded here)
    local old=/etc/nginx/sites-available/layte-panel.conf
    if [ -f "$old" ] && head -n1 "$old" | grep -q "^$MARKER"; then rm -f "$old" /etc/nginx/sites-enabled/layte-panel.conf; fi
  fi
  if ! nginx -t >/dev/null 2>&1; then
    if [ -f "$TMP/panel.conf.bak" ]; then cp "$TMP/panel.conf.bak" "$NGINX_CONF"; else rm -f "$NGINX_CONF" ${NGINX_LINK:+"$NGINX_LINK"}; fi
    nginx -t || true
    die "nginx rejected the panel config; rolled back"
  fi
  systemctl reload nginx
  NGINX_DUMP="$(nginx -T 2>/dev/null || true)"
  grep -qF "server_name $PANEL_DOMAIN;" <<<"$NGINX_DUMP" \
    || warn "nginx doesn't seem to load $NGINX_CONF (check the include lines in /etc/nginx/nginx.conf)"
}

if [ -z "$PANEL_DOMAIN" ] && [ "$UPGRADE" = 0 ] && command -v nginx >/dev/null; then
  PANEL_DOMAIN="$(ask "Domain for the panel (e.g. panel.example.com; empty = use an SSH tunnel)?" "")"
fi
PANEL_URL=""
if [ -n "$PANEL_DOMAIN" ]; then
  command -v nginx >/dev/null || die "--panel-domain needs nginx (use --with-stack)"
  [ ${#ALLOW_IPS[@]} -gt 0 ] || warn "no --allow-ip given: the login page will be reachable from anywhere (2FA and rate limits still apply)"
  install -d -m 0755 /var/www/layte-acme
  say "Configuring nginx for $PANEL_DOMAIN"
  panel_vhost http
  [ -n "$EMAIL" ] || EMAIL="$(ask "Email for Let's Encrypt expiry notices?" "")"
  if [ -n "$EMAIL" ] && command -v certbot >/dev/null; then
    say "Requesting a Let's Encrypt certificate for $PANEL_DOMAIN"
    if certbot certonly --webroot --webroot-path /var/www/layte-acme --cert-name layte-panel \
         --domains "$PANEL_DOMAIN" --email "$EMAIL" --agree-tos --no-eff-email --non-interactive --keep-until-expiring; then
      install -d -m 0755 /etc/letsencrypt/renewal-hooks/deploy
      printf '#!/bin/sh\n%s — reloads nginx after certbot renews a certificate\nexec /usr/bin/systemctl reload nginx\n' "$MARKER" \
        > /etc/letsencrypt/renewal-hooks/deploy/layte-reload-nginx
      chmod 0755 /etc/letsencrypt/renewal-hooks/deploy/layte-reload-nginx
      panel_vhost https
      PANEL_URL="https://$PANEL_DOMAIN"
    else
      warn "certbot failed (does $PANEL_DOMAIN point to this server?). The panel needs HTTPS for logins; use the SSH tunnel below until then."
    fi
  else
    warn "no email or certbot: skipping HTTPS. Use the SSH tunnel below."
  fi
fi

if command -v ufw >/dev/null && ufw status | grep -q "Status: active"; then
  ufw allow 80/tcp >/dev/null; ufw allow 443/tcp >/dev/null
fi

# ---------------------------------------------------------------- first admin
if [ -z "$ADMIN" ] && [ "$ADMIN_SET" = 0 ] && [ "$UPGRADE" = 0 ]; then
  ADMIN="$(ask "Create an admin user now? Username (empty = later)" "admin")"
fi
if [ -n "$ADMIN" ]; then
  if [ -r "$TTY" ]; then
    say "Creating admin $ADMIN (you'll set a password and scan a 2FA code)"
    # Root opens the terminal and hands it to the admin command on purpose.
    # shellcheck disable=SC2024
    sudo -u layte "$BIN" admin create --config "$ETC/config.toml" --username "$ADMIN" < "$TTY" \
      || warn "admin not created; run: sudo -u layte layte admin create --username $ADMIN"
  else
    warn "no terminal: create the admin later with: sudo -u layte layte admin create --username $ADMIN"
  fi
fi

# ---------------------------------------------------------------- done
VER="$("$BIN" version)"
echo
say "$VER is running."
if [ -n "$PANEL_URL" ]; then
  echo "    Open $PANEL_URL"
else
  echo "    The panel listens on 127.0.0.1:8443 only. From your computer:"
  echo "      ssh -L 8443:127.0.0.1:8443 $(whoami)@<this-server>"
  echo "    then open http://localhost:8443"
fi
echo "    Logs:    journalctl -u layte -u layte-agent -f"
echo "    Backup:  $ETC/secret.key and $DATA/layte.db"
