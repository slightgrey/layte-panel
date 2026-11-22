import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, Trash2 } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { databasesKey, useDatabases } from "@/api/databases";
import { siteKey, sitesKey, useDeleteSite, useSiteDisk, usePHPVersions, useSite, useSiteVhost, useSwitchPHP } from "@/api/sites";
import type { Site } from "@/api/types";
import { JobProgress } from "@/components/JobProgress";
import { SiteHeader, SiteJobCard } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Checkbox, Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { formatBytes } from "@/lib/format";
import { AdoptSite } from "./AdoptSite";
import { FormError } from "./AuthShell";

export function SiteDetailPage() {
  const id = Number(useParams().id);
  const site = useSite(id);
  const [params] = useSearchParams();
  const jobId = params.get("job");

  if (jobId && site.error) {
    // The site row is removed when creation fails and is fully rolled back.
    return (
      <div className="mx-auto max-w-5xl">
        <BackLink />
        <Card>
          <CardHeader>
            <CardTitle>Creating site</CardTitle>
          </CardHeader>
          <CardContent>
            <JobProgress jobId={jobId} />
          </CardContent>
        </Card>
      </div>
    );
  }

  if (site.isPending) {
    return (
      <div className="grid h-64 place-items-center">
        <Spinner />
      </div>
    );
  }
  if (site.error) {
    return (
      <div className="mx-auto max-w-5xl">
        <BackLink />
        <p className="text-sm text-destructive">{site.error.message}</p>
      </div>
    );
  }
  const s = site.data;
  const canDelete = s.managed && s.status !== "creating" && s.status !== "deleting";
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <SiteHeader site={s} actions={canDelete && <DeleteSite site={s} />} />
      <SiteJobCard site={s} />

      {!s.managed && !s.adopted && (
        <Notice action={s.status === "active" && s.root_path && <AdoptSite site={s} />}>
          This site was found in your nginx config. Layte Panel shows it <strong>read-only</strong> and never changes files
          it didn't create. Adopt it to manage it here: it gets its own Linux user, and its nginx config stays yours.
        </Notice>
      )}
      {s.adopted && (
        <Notice>
          Adopted: this site runs as <code className="font-mono">{s.system_user}</code> with a Layte-managed PHP-FPM pool. Its
          nginx config is still yours and is never rewritten by Layte Panel.
        </Notice>
      )}

      <OverviewCard site={s} />
      <VhostCard site={s} />
    </div>
  );
}

/** PHP version select + "Switch" for managed sites; progress shows in the job panel. */
function PHPSwitch({ site }: { site: Site }) {
  const php = usePHPVersions();
  const sw = useSwitchPHP(site.id);
  const [, setParams] = useSearchParams();
  const [version, setVersion] = useState(site.php_version);
  const others = php.data?.versions ?? [site.php_version];

  return (
    <div className="flex flex-wrap items-center gap-2">
      <Select aria-label="PHP version" className="w-32" value={version} onChange={(e) => setVersion(e.target.value)}>
        {others.map((v) => (
          <option key={v} value={v}>
            PHP {v}
          </option>
        ))}
      </Select>
      <Button
        size="sm"
        variant="outline"
        disabled={version === site.php_version || sw.isPending}
        onClick={() => sw.mutate(version, { onSuccess: (r) => setParams({ job: r.job_id }, { replace: true }) })}
      >
        Switch
      </Button>
      {sw.error && <span className="text-xs text-destructive">{sw.error.message}</span>}
    </div>
  );
}

/** "Drop databases" checkbox listing every database linked to the site (plus the legacy db_name). */
function DropDatabasesOption({ site, checked, onChange }: { site: Site; checked: boolean; onChange: (v: boolean) => void }) {
  const dbs = useDatabases();
  const names = new Set((dbs.data?.databases ?? []).filter((d) => d.site_id === site.id).map((d) => d.name));
  if (site.db_name) names.add(site.db_name);
  if (names.size === 0) return null;
  const list = [...names].sort();
  return (
    <Checkbox
      label={list.length === 1 ? `Drop database ${list[0]}` : `Drop ${list.length} databases: ${list.join(", ")}`}
      description={list.length === 1 ? "Deletes the database and its user. This can't be undone." : "Deletes these databases and their users. This can't be undone."}
      checked={checked}
      onChange={(e) => onChange(e.target.checked)}
    />
  );
}

/** Delete button + confirmation dialog; shows the deletion job's progress. */
function DeleteSite({ site }: { site: Site }) {
  const [open, setOpen] = useState(false);
  const [confirm, setConfirm] = useState("");
  const [removeFiles, setRemoveFiles] = useState(true);
  const [dropDb, setDropDb] = useState(true);
  const [jobId, setJobId] = useState<string | null>(null);
  const [finished, setFinished] = useState(false);
  const del = useDeleteSite(site.id);
  const navigate = useNavigate();
  const qc = useQueryClient();

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    del.mutate({ confirm_domain: confirm, remove_files: removeFiles, drop_db: dropDb }, { onSuccess: (r) => setJobId(r.job_id) });
  };
  const close = () => {
    setOpen(false);
    setJobId(null);
    setFinished(false);
    setConfirm("");
    del.reset();
  };

  return (
    <>
      <Button variant="outline" size="sm" onClick={() => setOpen(true)} className="text-destructive">
        <Trash2 /> Delete
      </Button>
      <Dialog open={open} onClose={close} title={`Delete ${site.domain}`} locked={Boolean(jobId) && !finished}>
        {jobId ? (
          <JobProgress
            jobId={jobId}
            onDone={(ok) => {
              setFinished(true);
              qc.invalidateQueries({ queryKey: sitesKey });
              qc.invalidateQueries({ queryKey: siteKey(site.id) });
              qc.invalidateQueries({ queryKey: databasesKey });
              if (ok) navigate("/sites", { replace: true });
            }}
          />
        ) : (
          <form onSubmit={onSubmit} className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">
              This removes the nginx config and PHP-FPM pool, so the site stops being served.
            </p>
            <Checkbox
              label="Delete all site files"
              description={`Removes the Linux user ${site.system_user} and /home/${site.system_user}. This can't be undone.`}
              checked={removeFiles}
              onChange={(e) => setRemoveFiles(e.target.checked)}
            />
            <DropDatabasesOption site={site} checked={dropDb} onChange={setDropDb} />
            <Field label={`Type ${site.domain} to confirm`} htmlFor="confirm">
              <Input id="confirm" autoComplete="off" spellCheck={false} value={confirm} onChange={(e) => setConfirm(e.target.value)} />
            </Field>
            <FormError message={del.error?.message} />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" className="bg-destructive hover:bg-destructive/90" disabled={confirm.trim().toLowerCase() !== site.domain || del.isPending}>
                Delete site
              </Button>
            </div>
          </form>
        )}
      </Dialog>
    </>
  );
}

function BackLink() {
  return (
    <Link to="/sites" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
      <ArrowLeft className="size-4" /> Sites
    </Link>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 py-2 sm:grid-cols-[10rem_1fr] sm:gap-4">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm break-words">{children}</dd>
    </div>
  );
}

const mono = "font-mono text-xs";

/** Total size, plus the public/logs/tmp breakdown for Layte sites. */
function DiskSize({ site }: { site: Site }) {
  const canMeasure = site.status === "active" && Boolean(site.managed || site.adopted ? site.system_user : site.root_path);
  const disk = useSiteDisk(site.id, canMeasure);
  if (!canMeasure) return <span className="text-muted-foreground">–</span>;
  if (disk.isPending) return <span className="text-muted-foreground">Measuring…</span>;
  if (disk.error) return <span className="text-destructive">{disk.error.message}</span>;
  return (
    <span className="flex flex-wrap items-baseline gap-x-3 gap-y-1">
      <span className="font-medium tabular-nums">{formatBytes(disk.data.total)}</span>
      {disk.data.parts?.map((p) => (
        <span key={p.name} className="text-xs text-muted-foreground tabular-nums">
          {p.name} {formatBytes(p.bytes)}
        </span>
      ))}
      <button
        type="button"
        className="text-xs text-primary hover:underline disabled:opacity-50"
        disabled={disk.isFetching}
        onClick={() => disk.refetch()}
      >
        {disk.isFetching ? "Measuring…" : "Refresh"}
      </button>
    </span>
  );
}

function OverviewCard({ site: s }: { site: Site }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="divide-y">
          <Row label="Aliases">{s.aliases.length ? s.aliases.join(", ") : "–"}</Row>
          <Row label="PHP version">
            {s.managed && s.status === "active" ? <PHPSwitch site={s} /> : s.php_version || "none (static)"}
          </Row>
          <Row label="Web root">
            <span className={mono}>{s.root_path || "–"}</span>
          </Row>
          <Row label="Size on disk">
            <DiskSize site={s} />
          </Row>
          {s.system_user && <Row label="System user">{s.system_user}</Row>}
          {s.db_name && (
            <Row label="Database">
              <span className={mono}>{s.db_name}</span> (user <span className={mono}>{s.db_user}</span>)
            </Row>
          )}
          <Row label="nginx config">
            <span className={mono}>{s.nginx_conf || "–"}</span>
          </Row>
          {s.fpm_pool && (
            <Row label="PHP-FPM pool">
              <span className={mono}>{s.fpm_pool}</span>
            </Row>
          )}
          <Row label="Source">
            {s.source === "created" ? "Created by Layte Panel" : s.adopted ? "Imported, then adopted" : "Imported (read-only)"}
          </Row>
        </dl>
      </CardContent>
    </Card>
  );
}

function VhostCard({ site }: { site: Site }) {
  const vhost = useSiteVhost(site.id, Boolean(site.nginx_conf));
  return (
    <Card>
      <CardHeader>
        <CardTitle>nginx config</CardTitle>
        <span className="text-xs text-muted-foreground">read-only</span>
      </CardHeader>
      <CardContent>
        {vhost.isPending ? (
          <Spinner />
        ) : vhost.error ? (
          <p className="text-sm text-muted-foreground">Unavailable: {vhost.error.message}</p>
        ) : (
          <pre className="max-h-[32rem] overflow-auto rounded-[12px] bg-background p-4 font-mono text-xs leading-relaxed">
            {vhost.data.content}
          </pre>
        )}
      </CardContent>
    </Card>
  );
}
