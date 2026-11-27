import { Database, Download, Link2, Plus, Trash2, Unlink, Upload } from "lucide-react";
import { type FormEvent, type ReactNode, useRef, useState } from "react";
import {
  useCreateDatabase,
  useDatabases,
  useExportDatabase,
  useImportDatabase,
  useLinkDatabase,
  useUnlinkDatabase,
} from "@/api/databases";
import { useIsAdmin } from "@/api/auth";
import { downloadURL, useFileActions, useFiles } from "@/api/files";
import type { DatabaseRow, Site } from "@/api/types";
import { OneTimeSecrets } from "@/components/JobProgress";
import { ResetDBPassword } from "@/components/ResetDBPassword";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes } from "@/lib/format";
import { FormError } from "./AuthShell";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";

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
        <NoDatabase site={site} all={dbs.data.databases} onCreated={setSecrets} />
      ) : (
        mine.map((db) => <DatabaseCards key={db.name} site={site} db={db} onSecrets={setSecrets} />)
      )}
    </>
  );
}

/** Suggested DB name from the site user: lowercase, - → _, max 32 chars. */
function suggestName(site: Site) {
  const n = (site.system_user || "").toLowerCase().replace(/-/g, "_").replace(/[^a-z0-9_]/g, "");
  return /^[a-z]/.test(n) ? n.slice(0, 32) : "";
}

function NoDatabase({ site, all, onCreated }: { site: Site; all: DatabaseRow[]; onCreated: (s: Record<string, string>) => void }) {
  const create = useCreateDatabase();
  const [name, setName] = useState(() => suggestName(site));
  const isAdmin = useIsAdmin();
  if (!isAdmin) {
    return (
      <Card>
        <CardContent className="pt-6">
          <EmptyState icon={Database} title="No database for this site">
            Ask your admin to add one.
          </EmptyState>
        </CardContent>
      </Card>
    );
  }
  return (
    <Card>
      <CardContent className="pt-6">
        <EmptyState icon={Database} title="No database for this site">
          {site.adopted && <LinkDatabase site={site} all={all} />}
          {site.managed || site.adopted ? (
            <>
              {site.adopted
                ? "Or create a new, empty database and a user with the same name for this site:"
                : "Static sites don't need one. You can add a MySQL database and a user with the same name for this site here."}
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

/** Adopted sites: pick the existing database the site already uses (e.g. from wp-config.php). */
function LinkDatabase({ site, all }: { site: Site; all: DatabaseRow[] }) {
  const link = useLinkDatabase(site.id);
  const free = all.filter((d) => !d.system && !d.managed);
  const [name, setName] = useState(free[0]?.name ?? "");
  if (free.length === 0) return null;
  return (
    <div className="mb-6">
      Link the database this site already uses{site.wordpress && " (see DB_NAME in wp-config.php)"}. It's never dropped or
      changed by Layte Panel; you can export and import it here.
      <form
        className="mt-4 flex flex-wrap items-start justify-center gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          link.mutate(name);
        }}
      >
        <Select aria-label="Existing database" className="w-56 font-mono" value={name} onChange={(e) => setName(e.target.value)}>
          {free.map((d) => (
            <option key={d.name} value={d.name}>
              {d.name}
            </option>
          ))}
        </Select>
        <Button type="submit" disabled={link.isPending || !name}>
          <Link2 /> Link database
        </Button>
      </form>
      <div className="mt-2">
        <FormError message={link.error?.message} />
      </div>
    </div>
  );
}

function DatabaseCards({ site, db, onSecrets }: { site: Site; db: DatabaseRow; onSecrets: (s: Record<string, string>) => void }) {
  const exp = useExportDatabase();
  const job = useJobFeedback(`export-${db.name}`, `Exporting ${db.name}`);
  const canDump = db.managed && (site.managed || site.adopted) && site.status === "active";
  const unlink = useUnlinkDatabase();
  const isAdmin = useIsAdmin();
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
              {db.user ? (
                <Mono>{db.user}</Mono>
              ) : (
                <span className="text-muted-foreground">{db.linked ? "Not known: it's set in the site's own config" : "–"}</span>
              )}
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
            {db.linked && isAdmin && (
              <Button
                variant="outline"
                disabled={unlink.isPending}
                title="Layte forgets this database; it stays on the server unchanged"
                onClick={() => unlink.mutate(db.name)}
              >
                <Unlink /> Unlink
              </Button>
            )}
            {db.managed && (!db.linked || db.wordpress) && (site.managed || site.adopted) && (
              <ResetDBPassword db={db} onSecrets={onSecrets} />
            )}
            {canDump && (
              <Button
                variant="outline"
                disabled={exp.isPending}
                title="Dump the database to a .sql file in the site's tmp/ folder"
                onClick={() => exp.mutate(db.name, { onSuccess: job.start })}
              >
                <JobButtonLabel state={job.state}>
                  <Download /> Export (.sql)
                </JobButtonLabel>
              </Button>
            )}
          </div>
          {db.linked && (
            <p className="text-xs text-muted-foreground">
              Linked existing database: Layte Panel didn't create it, so it never drops it or changes its password.
            </p>
          )}
          <FormError message={exp.error?.message ?? unlink.error?.message} />
        </CardContent>
      </Card>

      {canDump && <DumpFiles site={site} db={db} />}
    </div>
  );
}

/** .sql files in the site's tmp/ folder: download, import, delete, upload. */
function DumpFiles({ site, db }: { site: Site; db: DatabaseRow }) {
  // The data area: tmp/ in the site folder (Layte sites) or in the user's home (adopted).
  const files = useFiles(site.id, "tmp", "data");
  const fileActions = useFileActions(site.id, "data");
  const tmpDir = site.adopted ? `/home/${site.system_user}/tmp` : "tmp/ in the site folder";
  const [importPath, setImportPath] = useState<string | null>(null);
  const input = useRef<HTMLInputElement>(null);
  const dumps = (files.data?.entries ?? [])
    .filter((e) => e.type === "file" && e.name.endsWith(".sql"))
    .sort((a, b) => b.mtime.localeCompare(a.mtime));

  return (
    <Card className="lg:col-span-3">
      <CardHeader>
        <CardTitle>SQL files</CardTitle>
        <input
          ref={input}
          type="file"
          accept=".sql"
          className="hidden"
          onChange={(e) => {
            const picked = [...(e.target.files ?? [])];
            e.target.value = "";
            if (picked.length) fileActions.upload.mutate({ dir: "tmp", files: picked });
          }}
        />
        <Button variant="outline" size="sm" disabled={fileActions.upload.isPending} onClick={() => input.current?.click()}>
          <Upload /> {fileActions.upload.isPending ? "Uploading…" : "Upload .sql"}
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Exports and uploads are kept in <code className="font-mono">{tmpDir}</code>, which isn't served on the web. Uploads can be up to 256 MB. Delete old dumps once you no longer need them.
        </p>
        <FormError message={fileActions.upload.error?.message ?? fileActions.remove.error?.message} />
        {files.isPending ? (
          <Spinner />
        ) : files.error ? (
          <p className="text-sm text-destructive">{files.error.message}</p>
        ) : dumps.length === 0 ? (
          <p className="py-4 text-center text-sm text-muted-foreground">No .sql files yet. Export the database or upload a dump.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>File</TableHead>
                <TableHead className="text-right">Size</TableHead>
                <TableHead>Modified</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {dumps.map((f) => {
                const path = `tmp/${f.name}`;
                return (
                  <TableRow key={f.name}>
                    <TableCell className="font-mono text-xs break-all">{f.name}</TableCell>
                    <TableCell className="text-right text-xs tabular-nums">{formatBytes(f.size)}</TableCell>
                    <TableCell className="text-xs text-muted-foreground">{new Date(f.mtime).toLocaleString()}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1">
                        <a
                          href={downloadURL(site.id, path, "data")}
                          className="inline-flex h-8 items-center rounded-[10px] px-2.5 text-muted-foreground hover:text-primary"
                          aria-label={`Download ${f.name}`}
                          title="Download"
                        >
                          <Download className="size-4" />
                        </a>
                        <Button variant="ghost" size="sm" onClick={() => setImportPath(path)}>
                          Import
                        </Button>
                        <Button
                          variant="ghost"
                          size="sm"
                          className="text-destructive"
                          aria-label={`Delete ${f.name}`}
                          disabled={fileActions.remove.isPending}
                          onClick={() => fileActions.remove.mutate({ path })}
                        >
                          <Trash2 />
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                );
              })}
            </TableBody>
          </Table>
        )}
        {importPath && <ImportDialog db={db} path={importPath} onClose={() => setImportPath(null)} />}
      </CardContent>
    </Card>
  );
}

function ImportDialog({ db, path, onClose }: { db: DatabaseRow; path: string; onClose: () => void }) {
  const imp = useImportDatabase();
  const job = useJobFeedback(`import-${db.name}`, `Importing into ${db.name}`);
  const [confirm, setConfirm] = useState("");
  return (
    <Dialog open onClose={onClose} title={`Import into ${db.name}`}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          imp.mutate(
            { name: db.name, path, confirm },
            {
              onSuccess: (r) => {
                onClose();
                job.start(r);
              },
            },
          );
        }}
      >
        <p className="text-sm text-muted-foreground">
          Runs <code className="font-mono">{path}</code> against <code className="font-mono">{db.name}</code>. Tables in the
          file replace the existing ones, and this can't be undone. Export first if you might need the current data. The
          import stops at the first error, and statements before it stay applied.
          {db.wordpress && " If the dump comes from another site, update the WordPress URLs afterwards."}
        </p>
        <Field label={`Type ${db.name} to confirm`} htmlFor="import-confirm">
          <Input id="import-confirm" autoComplete="off" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        <FormError message={imp.error?.message} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" className="bg-destructive hover:bg-destructive/90" disabled={confirm !== db.name || imp.isPending}>
            Import
          </Button>
        </div>
      </form>
    </Dialog>
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
