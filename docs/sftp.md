# SFTP per site

Each site Layte created or adopted can get its own **SFTP login**: it only reaches that site's folder and works as the site's own Linux user, so files you upload keep the right owner and WordPress can still update them.

Turn it on under **site → Files → SFTP access**.

## Connecting
| Setting | Value |
|---|---|
| Protocol | SFTP (not FTP) |
| Host | your server's own address, e.g. the panel's domain (not a domain behind Cloudflare's proxy: it doesn't pass SSH through) |
| Port | 22 |
| Username | the site's user, shown on the card (e.g. `examplecom`) |
| Folder | `/<domain>`: after logging in you see only that folder |

- **Command line:** `sftp examplecom@your-server`
- **rsync:** `rsync -av ./theme/ examplecom@your-server:/example.com/public/wp-content/themes/theme/` (sites Layte created have `public/`, `logs/` and `tmp/` in the folder; adopted sites show their web root).
- **VS Code / FileZilla:** add an SFTP connection with the values above and your private key.

## Keys and passwords
- **SSH keys (recommended):** paste the line from your `.pub` file (`ssh-ed25519 AAAA… you@pc`). ed25519 and ecdsa keys, and RSA keys of 2048 bits or more, are accepted. Key options such as `command=` are refused.
- **Password (optional):** tick **Allow password login**, then **Generate password**. It's shown **once** and never stored; generate a new one if you lose it. Unticking it locks the password again.

## What it can't do
SFTP only: no shell, no tunnels or port forwarding, and nothing outside the site folder. Your own SSH login (and every other account) is not affected.

## How it works
- `/etc/ssh/sshd_config.d/zz-layte-sftp.conf` applies to members of the groups `layte-sftp` and `layte-sftp-password` only (chroot, `internal-sftp`, no forwarding).
- `/srv/layte-sftp/<user>/<domain>` is a bind mount of the site folder, made by `layte-sftp-<user>.service` (comes back after a reboot).
- Keys are in `/etc/ssh/layte-sftp/<user>` (root-owned).

Turning SFTP off (or deleting the site) removes the user from those groups, locks the password, and removes the keys file, the mount and the service.

## Troubleshooting
- **"Permission denied (publickey)":** check the key on the card matches your private key (`ssh-keygen -lf ~/.ssh/id_ed25519.pub` shows the same `SHA256:` fingerprint).
- **Your `sshd_config` has `AllowUsers` / `AllowGroups`:** the card warns about it; add `layte-sftp` to `AllowGroups` (or the user to `AllowUsers`).
- **Blocked after failed logins:** if sshguard is installed, see [troubleshooting.md](troubleshooting.md#blocked-by-sshguard).
