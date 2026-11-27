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
- shares the main site's **SFTP login**, which sees both folders (see [sftp.md](sftp.md#subdomain-sites));
- needs its own DNS record pointing to the server before you issue SSL;
- can't take a name that's already in use. Layte refuses a name that another nginx config on the server serves (hand-written ones too, including `*.example.com` wildcards), and the panel's own address.

Deleting a subdomain site removes only its folder (if you tick "Delete all site files"). The Linux user and the main site stay. The main site can't be deleted while it has subdomain sites.

## Managing logins
**Users** (admins) lists every login. For each site login you can:
- change the **subdomain limit** (lowering it doesn't delete existing subdomains; it only stops new ones);
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
