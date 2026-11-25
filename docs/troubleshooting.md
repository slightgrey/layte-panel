# Troubleshooting

**Logs first:**
```bash
journalctl -u layte -u layte-agent -e        # recent
journalctl -u layte -u layte-agent -f        # follow
systemctl status layte layte-agent
```

### "Cannot reach layte-agent" banner
The panel is running but the root helper isn't.
```bash
sudo systemctl restart layte-agent && journalctl -u layte-agent -e
```
The socket is `/run/layte/agent.sock`. It is created by the agent and owned by group `layte`.

### Login doesn't stick / "missing or invalid CSRF token"
Session cookies are `Secure`, so the panel must be opened over **HTTPS**, or via `http://localhost` through an SSH tunnel. Opening it over plain `http://<ip>` doesn't work.

If you use your own nginx in front, make sure it passes `Host` and `X-Forwarded-For` (see `deploy/nginx-proxy.conf.example`).

### Locked out of the panel
- **Account locked:** after 5 failed attempts the account is locked for 15 minutes. To clear it now, run `sudo -u layte layte admin reset-password --username NAME`.
- **Lost 2FA device:** run `sudo -u layte layte admin reset-2fa --username NAME`.

### Creating a site failed
The job shows which step failed, and every completed step has already been rolled back. Common causes:
- **`nginx -t` failed:** the job shows nginx's own message. Often a broken *other* config file is the cause; check `sudo nginx -t`.
- **"Linux user … already exists":** choose another username in the form.
- **Database errors:** check that MySQL/MariaDB is running and root can log in over the socket: `sudo mysql -e 'select 1'`.
- **WordPress install:** the server needs outbound HTTPS to github.com (for WP-CLI) and wordpress.org.

### HTTPS (Let's Encrypt) failed
Every domain and alias must already resolve to this server, and port 80 must be open. Test with `curl -I http://example.com/.well-known/acme-challenge/test` from another machine; a 404 from nginx is fine.

### A certificate shows "expiring"
certbot renews 30 days before expiry, so under 21 days means renewal is failing. Run `sudo certbot renew --dry-run` to see why.

### Give an adopted site's nginx config back to yourself
After **Let Layte manage nginx**, your original config is kept: as `<file>.layte-orig` (in `conf.d/`, or in
`sites-available/` if it was a file in `sites-enabled/`), or untouched in `sites-available/` if only its
`sites-enabled/` link was removed. To go back by hand:

1. Remove Layte's vhost: `/etc/nginx/conf.d/layte-<domain>.conf`, or
   `/etc/nginx/sites-enabled/layte-<domain>.conf` and `/etc/nginx/sites-available/layte-<domain>.conf`.
2. Put yours back (rename the `.layte-orig` file, or recreate the `sites-enabled/` link).
3. `nginx -t && systemctl reload nginx`.

The site's PHP pool is then Layte's standard pool (`/etc/php/<v>/fpm/pool.d/layte-<domain>.conf`); point your config's
`fastcgi_pass` at its socket, or restore the pool from before the adoption (`<pool>.layte-orig`).

### Letting Layte manage nginx fails at the certificate step
For sites with HTTPS, Layte first gets its own certificate (HTTP-01, files in the site's web root, served by your
current config). Nothing is changed when this fails. Common causes: your config denies `/.well-known/` (e.g. a
`location ~ /\.` rule without an exception), or a CDN/proxy doesn't pass `/.well-known/acme-challenge/` through.

### Everything else
Run `layte version` and include its output, plus the relevant journal lines, in a GitHub issue.
