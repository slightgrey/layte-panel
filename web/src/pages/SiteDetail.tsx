import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, ExternalLink, Info, Trash2 } from "lucide-react";
import { type FormEvent, type ReactNode, useState } from "react";
import { Link, useNavigate, useParams, useSearchParams } from "react-router";
import { siteKey, sitesKey, useDeleteSite, useSite, useSiteVhost } from "@/api/sites";
import type { Site } from "@/api/types";
import { JobProgress } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Checkbox, Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "./AuthShell";
import { SiteBadges } from "./SiteBadges";

export function SiteDetailPage() {
  const id = Number(useParams().id);
  const site = useSite(id);
  const [params, setParams] = useSearchParams();
  const jobId = params.get("job");
  const qc = useQueryClient();

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
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <div>
        <BackLink />
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-xl font-semibold tracking-tight">{s.domain}</h1>
          <SiteBadges site={s} />
          <a
            href={`${s.ssl ? "https" : "http"}://${s.domain}`}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            Visit <ExternalLink className="size-3.5" />
          </a>
          {s.managed && s.status !== "creating" && s.status !== "deleting" && (
            <div className="ml-auto">
              <DeleteSite site={s} />
            </div>
          )}
        </div>
      </div>

      {jobId && (
        <Card>
          <CardHeader>
            <CardTitle>{s.status === "creating" ? "Creating site" : "Job"}</CardTitle>
            <Button variant="ghost" size="sm" onClick={() => setParams({}, { replace: true })}>
              Dismiss
            </Button>
          </CardHeader>
          <CardContent>
            <JobProgress
              jobId={jobId}
              onDone={() => {
                qc.invalidateQueries({ queryKey: siteKey(id) });
                qc.invalidateQueries({ queryKey: sitesKey });
              }}
            />
          </CardContent>
        </Card>
      )}

      {!s.managed && (
        <div className="flex items-start gap-2 rounded-lg border bg-muted/50 px-4 py-3 text-sm">
          <Info className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
          <p>
            This site was found in your nginx config. Layte Panel shows it <strong>read-only</strong> and never changes
            files it didn't create. Adopting existing sites will come in a later release.
          </p>
        </div>
      )}

      <OverviewCard site={s} />
      <VhostCard site={s} />
    </div>
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
            {site.db_name && (
              <Checkbox
                label={`Drop database ${site.db_name}`}
                description="Deletes the database and its user. This can't be undone."
                checked={dropDb}
                onChange={(e) => setDropDb(e.target.checked)}
              />
            )}
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

function OverviewCard({ site: s }: { site: Site }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Overview</CardTitle>
      </CardHeader>
      <CardContent>
        <dl className="divide-y">
          <Row label="Aliases">{s.aliases.length ? s.aliases.join(", ") : "–"}</Row>
          <Row label="PHP version">{s.php_version || "none (static)"}</Row>
          <Row label="Web root">
            <span className={mono}>{s.root_path || "–"}</span>
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
          <Row label="Source">{s.source === "created" ? "Created by Layte Panel" : "Imported (read-only)"}</Row>
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
          <pre className="max-h-[32rem] overflow-auto rounded-md border bg-muted/50 p-4 font-mono text-xs leading-relaxed">
            {vhost.data.content}
          </pre>
        )}
      </CardContent>
    </Card>
  );
}
