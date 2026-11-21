# Upgrading, backups and uninstalling

## Upgrade
Run the installer again:
```bash
curl -fsSL https://raw.githubusercontent.com/slightgrey/layte-panel/main/deploy/install.sh | sudo bash
```
It replaces the binary and the systemd units, then restarts both services. Database migrations run automatically on start.

The installer keeps:
- `/etc/layte/config.toml`
- `/etc/layte/secret.key`
- the database
- your sites

To move to a specific version (including rolling back), use `--version vX.Y.Z`.

Releases are listed at <https://github.com/slightgrey/layte-panel/releases>.

## Backups
Layte Panel's own state is two files:
```bash
# consistent copy while running (needs: apt install sqlite3)
sudo sqlite3 /var/lib/layte/layte.db ".backup '/root/layte-$(date +%F).db'"
# or without sqlite3: stop the panel for a moment (sites keep running)
sudo systemctl stop layte && sudo cp /var/lib/layte/layte.db /root/layte-$(date +%F).db && sudo systemctl start layte

sudo cp /etc/layte/secret.key /root/layte-secret.key
```
- **Without `secret.key`**, every admin has to re-enroll 2FA (`layte admin reset-2fa`).
- **Your sites are not in these files.** Back them up separately: `/home/<user>/<domain>`, the MySQL databases (`mysqldump`) and `/etc/letsencrypt`.

## Uninstall
This removes the panel and keeps your sites running:
```bash
sudo systemctl disable --now layte layte-agent
sudo rm /etc/systemd/system/layte.service /etc/systemd/system/layte-agent.service
sudo systemctl daemon-reload
sudo rm -f /usr/local/bin/layte /etc/nginx/sites-enabled/layte-panel.conf /etc/nginx/sites-available/layte-panel.conf /etc/nginx/conf.d/layte-panel.conf
sudo nginx -t && sudo systemctl reload nginx
# Panel data and the system user (optional):
sudo rm -rf /etc/layte /var/lib/layte && sudo userdel layte
```

Sites created by Layte Panel keep working after an uninstall. Their configs are normal files:
- `/etc/nginx/sites-available/layte-*.conf` (or `/etc/nginx/conf.d/layte-*.conf`)
- `/etc/php/*/fpm/pool.d/layte-*.conf`
- `/etc/cron.d/layte-*`

They still carry the "Managed by Layte Panel" comment. If you reinstall later, the panel only knows about sites that are in its database, so restore your database backup to get them back as managed sites.
