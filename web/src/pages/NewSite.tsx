import { ArrowLeft } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router";
import { useMe } from "@/api/auth";
import { useCreateSite, usePHPVersions, useSite } from "@/api/sites";
import { useCertificates } from "@/api/ssl";
import type { SetupLink } from "@/api/types";
import { useCreateUser } from "@/api/users";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Checkbox, Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "./AuthShell";

const domainRe = /^(?=.{1,253}$)([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]*$/i;

/** Mirrors validate.DefaultUsername on the server (just for the placeholder). */
function defaultUsername(domain: string) {
  let u = domain.toLowerCase().replace(/^www\./, "").replace(/[^a-z0-9]/g, "");
  if (!/^[a-z]/.test(u)) u = "site" + u;
  return u.slice(0, 24);
}

const labelRe = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/i;

export function NewSitePage() {
  const navigate = useNavigate();
  const php = usePHPVersions();
  const create = useCreateSite();
  const user = useMe().data?.user;
  const isAdmin = user?.role === "admin";
  // A subdomain site of a main site: always for site accounts, ?parent=<id> for admins.
  const [params] = useSearchParams();
  const parentId = isAdmin ? Number(params.get("parent")) || 0 : (user?.site_id ?? 0);
  const parent = useSite(parentId, parentId > 0);
  const sub = parentId > 0;
  const [label, setLabel] = useState("");

  const [domain, setDomain] = useState("");
  const [aliases, setAliases] = useState("");
  const [username, setUsername] = useState("");
  const [phpVersion, setPhpVersion] = useState("");
  const [createDb, setCreateDb] = useState(true);
  const [wordpress, setWordpress] = useState(true);
  const [title, setTitle] = useState("");
  const [adminUser, setAdminUser] = useState("");
  const [adminEmail, setAdminEmail] = useState("");
  const [withSSL, setWithSSL] = useState(false);
  const [sslEmail, setSslEmail] = useState("");
  const certs = useCertificates();
  // The admin's Let's Encrypt address, as the SSL tab remembers it.
  useEffect(() => {
    if (certs.data?.email && !sslEmail) setSslEmail(certs.data.email);
  }, [certs.data, sslEmail]);
  const [withLogin, setWithLogin] = useState(false);
  const [loginUser, setLoginUser] = useState("");
  const [loginLimit, setLoginLimit] = useState("5");
  const createUser = useCreateUser();

  // Default to the newest installed PHP.
  useEffect(() => {
    const v = php.data?.versions;
    if (v?.length && !phpVersion) setPhpVersion(v[v.length - 1]!);
  }, [php.data, phpVersion]);

  const fullDomain = sub ? (label.trim() && parent.data ? `${label.trim().toLowerCase()}.${parent.data.domain}` : "") : domain.trim();
  const domainOk = sub ? label === "" || labelRe.test(label.trim()) : domain === "" || domainRe.test(domain.trim());

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        domain: fullDomain,
        aliases: sub ? [] : aliases.split(/[\s,]+/).filter(Boolean),
        username: sub ? undefined : username.trim() || undefined,
        php_version: phpVersion,
        create_db: createDb || wordpress,
        wordpress: wordpress ? { title: title.trim() || fullDomain, admin_user: adminUser.trim(), admin_email: adminEmail.trim() } : undefined,
        parent_site_id: sub ? parentId : undefined,
        ssl: withSSL ? { email: sslEmail.trim() } : undefined,
      },
      {
        onSuccess: (res) => {
          const go = (login?: SetupLink) => navigate(`/sites/${res.site_id}?job=${res.job_id}`, { state: login ? { login } : undefined });
          if (!withLogin || sub) return go();
          // The site row exists as soon as the job starts, so its login can be made right away.
          const user = loginUser.trim() || defaultUsername(domain.trim());
          createUser.mutate(
            { username: user, site_id: res.site_id, max_subdomains: Number(loginLimit) },
            { onSuccess: (r) => go({ username: r.user.username, setup_url: r.setup_url, expires_at: r.expires_at }), onError: () => go() },
          );
        },
      },
    );
  };

  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <Link to="/sites" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Sites
        </Link>
        <h1 className="text-2xl font-bold tracking-tight">{sub ? "Add subdomain" : "New site"}</h1>
        <p className="text-sm text-muted-foreground">
          {sub
            ? `A separate site on a subdomain of ${parent.data?.domain ?? "your site"}, with its own folder, PHP and nginx config, plus a database and WordPress if you want them. Point the subdomain's DNS to this server.`
            : "Creates a Linux user, a PHP-FPM pool and an nginx config, plus a database and WordPress if you want them."}
        </p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Site</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            {sub ? (
              <Field label="Subdomain" htmlFor="label" hint={!domainOk ? <span className="text-destructive">Letters, digits and dashes only, like blog</span> : undefined}>
                <div className="flex items-center gap-2">
                  <Input id="label" required autoFocus placeholder="blog" spellCheck={false} className="max-w-48" value={label} onChange={(e) => setLabel(e.target.value)} />
                  <span className="font-mono text-sm text-muted-foreground">.{parent.data?.domain ?? "…"}</span>
                </div>
              </Field>
            ) : (
              <>
                <Field label="Domain" htmlFor="domain" hint={!domainOk ? <span className="text-destructive">Enter a domain like example.com</span> : undefined}>
                  <Input id="domain" required autoFocus placeholder="example.com" spellCheck={false} value={domain} onChange={(e) => setDomain(e.target.value)} />
                </Field>
                <Field label="Aliases" htmlFor="aliases" hint="Optional. Other names for the same site, separated by spaces.">
                  <Input id="aliases" placeholder={domain ? `www.${domain.replace(/^www\./, "")}` : "www.example.com"} spellCheck={false} value={aliases} onChange={(e) => setAliases(e.target.value)} />
                </Field>
              </>
            )}
            <div className="grid gap-4 sm:grid-cols-2">
              <Field label="PHP version" htmlFor="php">
                <Select id="php" required value={phpVersion} onChange={(e) => setPhpVersion(e.target.value)} disabled={!php.data}>
                  {php.data?.versions.map((v) => (
                    <option key={v} value={v}>
                      PHP {v}
                    </option>
                  ))}
                </Select>
              </Field>
              {!sub && (
                <Field label="Linux user" htmlFor="username" hint="Optional. Runs PHP and owns the files.">
                  <Input id="username" placeholder={domain ? defaultUsername(domain) : "derived from the domain"} spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} />
                </Field>
              )}
            </div>
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>Database & WordPress</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Checkbox
              label="Install WordPress"
              description="Downloads the latest WordPress and runs the installer. Admin password is generated."
              checked={wordpress}
              onChange={(e) => setWordpress(e.target.checked)}
            />
            {wordpress && (
              <div className="grid gap-4 border-l-2 pl-4 sm:grid-cols-2">
                <Field label="Site title" htmlFor="title">
                  <Input id="title" placeholder={domain || "My site"} value={title} onChange={(e) => setTitle(e.target.value)} />
                </Field>
                <Field label="Admin username" htmlFor="admin_user">
                  <Input id="admin_user" required pattern="[a-z][a-z0-9_\-]{2,31}" title="3-32 lowercase letters, digits, _ or -" value={adminUser} onChange={(e) => setAdminUser(e.target.value)} />
                </Field>
                <Field label="Admin email" htmlFor="admin_email">
                  <Input id="admin_email" type="email" required value={adminEmail} onChange={(e) => setAdminEmail(e.target.value)} />
                </Field>
              </div>
            )}
            <Checkbox
              label="Create a MySQL database"
              description={wordpress ? "Required for WordPress." : "A database and user for this site; the password is shown once."}
              checked={createDb || wordpress}
              disabled={wordpress}
              onChange={(e) => setCreateDb(e.target.checked)}
            />
          </CardContent>
        </Card>

        <Card>
          <CardHeader>
            <CardTitle>HTTPS</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Checkbox
              label="Issue a free SSL certificate (Let's Encrypt)"
              description="Right after the site is created, and WordPress switches to https://. The domain's DNS must already point to this server; if it doesn't yet, the site still works over HTTP and you can add SSL later on its SSL tab."
              checked={withSSL}
              onChange={(e) => setWithSSL(e.target.checked)}
            />
            {withSSL && (
              <div className="border-l-2 pl-4">
                <Field label="Email for expiry notices" htmlFor="ssl_email" hint="Let's Encrypt writes here if a certificate is about to expire.">
                  <Input id="ssl_email" type="email" required className="max-w-sm" value={sslEmail} onChange={(e) => setSslEmail(e.target.value)} />
                </Field>
              </div>
            )}
          </CardContent>
        </Card>

        {!sub && isAdmin && (
        <Card>
          <CardHeader>
            <CardTitle>Panel login</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Checkbox
              label="Create a login for this site"
              description="For whoever runs the site: it sees only this site and its subdomains, with files and SFTP limited to its own folder. You get a one-time setup link to send them; they choose their own password."
              checked={withLogin}
              onChange={(e) => setWithLogin(e.target.checked)}
            />
            {withLogin && (
              <div className="grid gap-4 border-l-2 pl-4 sm:grid-cols-2">
                <Field label="Username" htmlFor="login_user">
                  <Input id="login_user" pattern="[a-z][a-z0-9_\-]{2,31}" title="3-32 lowercase letters, digits, _ or -" placeholder={domain ? defaultUsername(domain) : ""} spellCheck={false} value={loginUser} onChange={(e) => setLoginUser(e.target.value)} />
                </Field>
                <Field label="Subdomain limit" htmlFor="login_limit" hint="Subdomains they may add (0-100).">
                  <Input id="login_limit" type="number" min={0} max={100} required value={loginLimit} onChange={(e) => setLoginLimit(e.target.value)} />
                </Field>
              </div>
            )}
          </CardContent>
        </Card>
        )}

        <FormError message={create.error?.message} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => navigate(sub && isAdmin ? `/sites/${parentId}` : "/sites")}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending || createUser.isPending || !domainOk || !phpVersion || (sub && !fullDomain)}>
            {create.isPending ? "Starting…" : sub ? "Add subdomain" : "Create site"}
          </Button>
        </div>
      </form>
    </div>
  );
}
