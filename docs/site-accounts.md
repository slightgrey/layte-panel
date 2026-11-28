# Site logins and subdomains

A **site login** (a "site account") is a panel login for whoever runs one site: a client, a colleague, the site's own developer. It sees **only that site and its subdomains**. It never sees the server's other sites, the dashboard, Security, Updates, Users or API keys.

## Creating a login
As an admin, use either of these:
- **New site → Panel login → Create a login for this site**, while creating the site; or
- **site → Overview → Panel login → Create login**, for an existing site Layte created or adopted.

Choose a username and a **subdomain limit** (how many subdomains the login may add, 0 to 100, default 5). You get a **one-time setup link**, shown once. Send it to the user privately. The link:
- works **once** and expires after **72 hours**;
- lets the user choose their **own password**, so no one else ever sees it;
- then has them set up **two-factor login** by scanning a QR code with an authenticator app. 2FA is required, just as it is for admins.

If the link reaches the wrong person, or expires, issue a new one. The old link stops working at once.

Each main site can have one login. Subdomain sites use the login of their main site.

## What a site login can do
For its own sites only:

| | |
|---|---|
| Overview | PHP version and PHP settings, page cache, object cache, size on disk |
| SSL | issue, renew and remove Let's Encrypt certificates |
| Database | see the database, reset its password, export and import `.sql` files |
| Cron | add, pause and delete cron jobs (they run as the site's user) |
| Redirects, WP Debug | as for admins |
| Logs | the site's own nginx and PHP logs, with live tail |
| Files & SFTP | file manager limited to the site's folder, and the SFTP login |
| Subdomains | add subdomain sites up to the limit, and delete them |

Admins only: deleting the main site, viewing the nginx config, adopting or taking over a site, creating, linking and unlinking databases, and anything server-wide.

The server enforces every one of these rules. A site login that opens another site's address gets "not found", and server pages answer "forbidden". Hiding them in the menu is only for convenience.

## Subdomains
A site login can add `<name>.<main domain>`, for example `blog.example.com` under `example.com`. Only one level is allowed: `a.b.example.com` and other domains are refused. Admins can add one too from the main site's page, using **Add subdomain**.

A subdomain site:
- runs as the **main site's Linux user**, in `/home/<user>/blog.example.com/{public,logs,tmp}`, with its own PHP-FPM pool, nginx config, cron file and (optionally) database and WordPress;
- has **no aliases** and no username of its own;
- has PHP's shell functions blocked, like every site of a site login (an admin can allow them per site; see [security.md](security.md#sites-that-share-a-linux-user));
- shares the main site's **SFTP login**, which sees both folders (see [sftp.md](sftp.md#subdomain-sites));
- needs its own DNS record pointing to the server before you issue SSL;
- can't take a name that's already in use. Layte refuses a name that another nginx config on the server serves (hand-written ones too, including `*.example.com` wildcards), and the panel's own address.

Deleting a subdomain site removes only its folder (if you tick "Delete all site files"). The Linux user and the main site stay. The main site can't be deleted while it has subdomain sites.

## Limits
Each site login has limits, set when you create it and changed any time on its Panel login card or the Users page (0 = no limit):

| Limit | Default | How it's enforced |
|---|---|---|
| Subdomains | 5 | Layte refuses new subdomain sites past it |
| CPU | 100% (one core) | kernel cgroup (`CPUQuota`): the login's PHP and cron jobs share it |
| RAM | 512 MB | kernel cgroup (`MemoryMax`, no swap past it): if PHP needs more, requests fail |
| Disk | none until quotas are on | Linux disk quota: writes past it fail everywhere (PHP, SFTP, cron, uploads) |

How it works:
- Each login gets a systemd slice, `layte-acct-u<uid>.slice`, that holds its limits.
- The login's sites' PHP runs in **its own PHP-FPM service** inside that slice: `layte-fpm-<user>-<version>.service`, with its pools in `/etc/layte/fpm/<user>/`. One login can't take PHP workers or RAM from another. Moving a site in or out of it has no downtime: the new pool starts before nginx switches over.
- **Cron jobs** run in the slice too. Their line in `/etc/cron.d` only calls `layte cron-exec <site> <n>`; the commands themselves sit in a root-only file and run as the site's user.
- **Disk quotas** need turning on once: Users → **Disk quotas** → Turn on disk quotas. That:
  - installs the `quota` tools;
  - installs the kernel's quota module if the running kernel lacks it. Ubuntu's cloud kernels ship it separately, in `linux-modules-extra-<version>` (about 110 MB), and `linux-image-extra-virtual` keeps it there after kernel updates;
  - adds `usrquota` to the `/home` filesystem's line in `/etc/fstab`. The original is kept as `/etc/fstab.layte-orig`, and the new file is checked with `findmnt --verify` and put back if the check fails;
  - remounts the filesystem and builds the quota file.

  Only ext4 is supported; xfs needs boot options that Layte doesn't change.

  **Turn off disk quotas** on the same card undoes it:
  - quotas are switched off;
  - `usrquota` is taken out of `/etc/fstab`, checked the same way, so the quota service doesn't switch them back on at boot;
  - the quota file is removed;
  - every login's disk limit goes back to none.

  The quota tools and kernel module stay installed, so turning quotas on again is quick.

Each login sees **Your plan** in the sidebar on every page (on the Sites page on phones):
- disk used of its limit;
- RAM and CPU in use right now;
- subdomains used.

Admins see the same on the site's Panel login card and under **Usage** on the Users page; their own sidebar shows the whole server's CPU load, RAM and disk instead. The numbers come straight from the kernel (the slice's cgroup and the disk quota). Without quotas, disk use is measured with `du`.

To stay light on busy servers:
- RAM and CPU refresh every 30 seconds (and not in background tabs);
- disk use is measured at most every 5 minutes per login, because it can cost a `du` over all their files;
- the ↻ button measures everything right away; its tooltip shows when the disk was last checked.

Adopted sites whose nginx config is still yours keep running in the shared PHP-FPM service, because their socket can't be moved without changing your config. They have no CPU or RAM limit until you let Layte manage the site's nginx config.

## Managing logins
**Users** (admins) lists every login. For each site login you can:
- change the **limits**: subdomains, CPU, RAM and disk. Lowering the subdomain limit doesn't delete existing subdomains; it only stops new ones. New CPU and RAM limits apply at once;
- see its **usage**;
- issue a **password reset link** (shown once, works like the setup link). Their current password keeps working until the link is used; then every session is signed out;
- **reset 2FA** (for a lost phone; the user sets it up again at the next sign-in);
- **delete the login** (the sites and their files stay).

Deleting a main site deletes its login too.

New admins are added on the server with `layte admin create`. An admin can change their own password and 2FA on the Account page. A locked-out admin is helped on the server with `layte admin reset-password | reset-2fa`.

## Good to know
- Site logins follow the same rules as admins: argon2id passwords, mandatory 2FA, lockout after 5 failed attempts, and session timeouts. See [security.md](security.md#logins).
- Everything a site login changes goes through the same agent actions as an admin's, and the audit log records it under the login's username.
- Site logins have the same **Account page** as admins (gear icon, top right), where they can change their password, move 2FA to a new phone and sign out other sessions.
- A site login can't create API keys. API keys are read-only and server-wide, so only admins create them.
