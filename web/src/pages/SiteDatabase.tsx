import { Database, KeyRound, Plus } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { useCreateDatabase, useDatabases, useResetDBPassword } from "@/api/databases";
import type { DatabaseRow, Site } from "@/api/types";
import { OneTimeSecrets } from "@/components/JobProgress";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { formatBytes } from "@/lib/format";
import { FormError } from "./AuthShell";

export function SiteDatabasePage() {
  return <SiteShell title="Database">{(site) => <DatabaseTab site={site} />}</SiteShell>;
}

function DatabaseTab({ site }: { site: Site }) {
  const dbs = useDatabases();
  // Secrets live only in this state: shown once, then dropped on dismiss.
  const [secrets, setSecrets] = useState<Record<string, string> | null>(null);

  if (dbs.isPending) return <Spinner />;
  if (dbs.error) return <p className="text-sm text-destructive">{dbs.error.message}</p>;

  const mine = dbs.data.databases.filter((d) => d.site_id === site.id);
  return (
    <>
      {secrets && <OneTimeSecrets secrets={secrets} onDismiss={() => setSecrets(null)} />}
      {mine.length === 0 ? (
        <NoDatabase site={site} onCreated={setSecrets} />
      ) : (
        mine.map((db) => <DatabaseCards key={db.name} db={db} onSecrets={setSecrets} />)
      )}
    </>
  );
}

/** Suggested DB name from the site user: lowercase, - → _, max 32 chars. */
function suggestName(site: Site) {
  const n = (site.system_user || "").toLowerCase().replace(/-/g, "_").replace(/[^a-z0-9_]/g, "");
  return /^[a-z]/.test(n) ? n.slice(0, 32) : "";
}

function NoDatabase({ site, onCreated }: { site: Site; onCreated: (s: Record<string, string>) => void }) {
  const create = useCreateDatabase();
  const [name, setName] = useState(() => suggestName(site));
  return (
    <Card>
      <CardContent className="pt-6">
        <EmptyState icon={Database} title="No database for this site">
          {site.managed ? (
            <>
              Static sites don't need one. You can add a MySQL database and a user with the same name for this site here.
              <form
                className="mt-4 flex flex-wrap items-start justify-center gap-2"
                onSubmit={(e: FormEvent) => {
                  e.preventDefault();
                  create.mutate({ name: name.trim(), site_id: site.id }, { onSuccess: (r) => onCreated(r.secrets) });
                }}
              >
                <Input
                  aria-label="Database name"
                  required
                  pattern="[a-z][a-z0-9_]{0,31}"
                  title="Lowercase letters, digits and _"
                  className="w-56 font-mono"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
                <Button type="submit" disabled={create.isPending || !name.trim()}>
                  <Plus /> Create database
                </Button>
              </form>
              <div className="mt-2">
                <FormError message={create.error?.message} />
              </div>
            </>
          ) : (
            "Imported sites are read-only, so a database can't be added here."
          )}
        </EmptyState>
      </CardContent>
    </Card>
  );
}

function DatabaseCards({ db, onSecrets }: { db: DatabaseRow; onSecrets: (s: Record<string, string>) => void }) {
  const reset = useResetDBPassword();
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <Card className="lg:col-span-2">
        <CardHeader>
          <CardTitle>Connection</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="divide-y">
            <Row label="Database">
              <Mono>{db.name}</Mono>
            </Row>
            <Row label="User">
              <Mono>{db.user || "–"}</Mono>
            </Row>
            <Row label="Host">
              <Mono>localhost</Mono>
            </Row>
            <Row label="Password">
              <span className="text-muted-foreground">Shown once when created or reset.</span>
            </Row>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Usage</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Stat label="Size" value={formatBytes(db.size)} />
          <Stat label="Charset" value="utf8mb4" />
        </CardContent>
      </Card>

      <Card className="lg:col-span-3">
        <CardHeader>
          <CardTitle>Actions</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-wrap gap-3">
            {db.managed && (
              <Button
                variant="outline"
                disabled={reset.isPending || db.wordpress}
                title={db.wordpress ? "Used by WordPress: changing it here would break wp-config.php" : "Generate a new password"}
                onClick={() => reset.mutate(db.name, { onSuccess: (r) => onSecrets(r.secrets) })}
              >
                <KeyRound /> Reset password
              </Button>
            )}
            <Button variant="outline" disabled title="Coming later">
              Export (.sql)
            </Button>
            <Button variant="outline" disabled title="Coming later">
              Import (.sql)
            </Button>
          </div>
          <FormError message={reset.error?.message} />
        </CardContent>
      </Card>
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-center">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono text-xs">{children}</span>;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <span className="eyebrow">{label}</span>
      <span className="font-mono text-sm">{value}</span>
    </div>
  );
}
