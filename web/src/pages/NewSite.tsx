import { ArrowLeft } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useCreateSite, usePHPVersions } from "@/api/sites";
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

export function NewSitePage() {
  const navigate = useNavigate();
  const php = usePHPVersions();
  const create = useCreateSite();

  const [domain, setDomain] = useState("");
  const [aliases, setAliases] = useState("");
  const [username, setUsername] = useState("");
  const [phpVersion, setPhpVersion] = useState("");
  const [createDb, setCreateDb] = useState(true);
  const [wordpress, setWordpress] = useState(true);
  const [title, setTitle] = useState("");
  const [adminUser, setAdminUser] = useState("");
  const [adminEmail, setAdminEmail] = useState("");
  const [withLogin, setWithLogin] = useState(false);
  const [loginUser, setLoginUser] = useState("");
  const [loginLimit, setLoginLimit] = useState("5");
  const createUser = useCreateUser();

  // Default to the newest installed PHP.
  useEffect(() => {
    const v = php.data?.versions;
    if (v?.length && !phpVersion) setPhpVersion(v[v.length - 1]!);
  }, [php.data, phpVersion]);

  const domainOk = domain === "" || domainRe.test(domain.trim());

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    create.mutate(
      {
        domain: domain.trim(),
        aliases: aliases.split(/[\s,]+/).filter(Boolean),
        username: username.trim() || undefined,
        php_version: phpVersion,
        create_db: createDb || wordpress,
        wordpress: wordpress ? { title: title.trim() || domain.trim(), admin_user: adminUser.trim(), admin_email: adminEmail.trim() } : undefined,
      },
      {
        onSuccess: (res) => {
          const go = (login?: { username: string; password: string }) => navigate(`/sites/${res.site_id}?job=${res.job_id}`, { state: login ? { login } : undefined });
          if (!withLogin) return go();
          // The site row exists as soon as the job starts, so its login can be made right away.
          const user = loginUser.trim() || defaultUsername(domain.trim());
          createUser.mutate(
            { username: user, site_id: res.site_id, max_subdomains: Number(loginLimit) },
            { onSuccess: (r) => go({ username: r.user.username, password: r.password }), onError: () => go() },
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
        <h1 className="text-2xl font-bold tracking-tight">New site</h1>
        <p className="text-sm text-muted-foreground">
          Creates a Linux user, a PHP-FPM pool and an nginx config, plus a database and WordPress if you want them.
        </p>
      </div>

      <form onSubmit={onSubmit} className="flex flex-col gap-6">
        <Card>
          <CardHeader>
            <CardTitle>Site</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Field label="Domain" htmlFor="domain" hint={!domainOk ? <span className="text-destructive">Enter a domain like example.com</span> : undefined}>
              <Input id="domain" required autoFocus placeholder="example.com" spellCheck={false} value={domain} onChange={(e) => setDomain(e.target.value)} />
            </Field>
            <Field label="Aliases" htmlFor="aliases" hint="Optional. Other names for the same site, separated by spaces.">
              <Input id="aliases" placeholder={domain ? `www.${domain.replace(/^www\./, "")}` : "www.example.com"} spellCheck={false} value={aliases} onChange={(e) => setAliases(e.target.value)} />
            </Field>
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
              <Field label="Linux user" htmlFor="username" hint="Optional. Runs PHP and owns the files.">
                <Input id="username" placeholder={domain ? defaultUsername(domain) : "derived from the domain"} spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} />
              </Field>
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
            <CardTitle>Panel login</CardTitle>
          </CardHeader>
          <CardContent className="flex flex-col gap-4">
            <Checkbox
              label="Create a login for this site"
              description="For whoever runs the site: it sees only this site and its subdomains, with files and SFTP limited to its own folder. The password is generated and shown once."
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

        <FormError message={create.error?.message} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={() => navigate("/sites")}>
            Cancel
          </Button>
          <Button type="submit" disabled={create.isPending || createUser.isPending || !domainOk || !phpVersion}>
            {create.isPending ? "Starting…" : "Create site"}
          </Button>
        </div>
      </form>
    </div>
  );
}
