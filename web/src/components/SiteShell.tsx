import { useQueryClient } from "@tanstack/react-query";
import { ArrowLeft, CheckCircle2, Clock, Database, ExternalLink, FolderOpen, LayoutGrid, LoaderCircle, type LucideIcon, ScrollText, ShieldCheck, XCircle } from "lucide-react";
import { type ReactNode, useState } from "react";
import { Link, NavLink, useParams, useSearchParams } from "react-router";
import { databasesKey } from "@/api/databases";
import { siteKey, sitesKey, useSite } from "@/api/sites";
import { certsKey } from "@/api/ssl";
import type { Site } from "@/api/types";
import { JobProgress } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { progressFrame } from "@/components/ui/notice";
import { cn } from "@/lib/utils";
import { SiteBadges } from "@/pages/SiteBadges";

const tabs: { label: string; path: string; icon: LucideIcon }[] = [
  { label: "Overview", path: "", icon: LayoutGrid },
  { label: "SSL", path: "ssl", icon: ShieldCheck },
  { label: "Database", path: "database", icon: Database },
  { label: "Cron", path: "cron", icon: Clock },
  { label: "Logs", path: "logs", icon: ScrollText },
  { label: "Files", path: "files", icon: FolderOpen },
];

/** Per-site tabs, so you never need the global sidebar to get around one site. */
export function SiteNav({ siteId }: { siteId: number }) {
  return (
    <nav aria-label="Site" className="flex w-full gap-1 overflow-x-auto rounded-[16px] p-1.5 shadow-inset sm:w-fit">
      {tabs.map(({ label, path, icon: Icon }) => (
        <NavLink
          key={label}
          to={path ? `/sites/${siteId}/${path}` : `/sites/${siteId}`}
          end={!path}
          className={({ isActive }) =>
            cn(
              "flex shrink-0 items-center gap-2 rounded-[12px] px-3.5 py-2 text-sm transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
              isActive ? "bg-background font-medium text-primary shadow-raised-sm" : "text-muted-foreground hover:text-foreground",
            )
          }
        >
          <Icon className="size-4" />
          {label}
        </NavLink>
      ))}
    </nav>
  );
}

/** Back link, domain, badges, "Visit", an optional action slot, then the site tabs. */
export function SiteHeader({ site, actions }: { site: Site; actions?: ReactNode }) {
  return (
    <div className="flex flex-col gap-4">
      <div>
        <Link to="/sites" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Sites
        </Link>
        <div className="flex flex-wrap items-center gap-3">
          <h1 className="text-2xl font-bold tracking-tight">{site.domain}</h1>
          <SiteBadges site={site} />
          <a
            href={`${site.ssl ? "https" : "http"}://${site.domain}`}
            target="_blank"
            rel="noreferrer noopener"
            className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
          >
            Visit <ExternalLink className="size-3.5" />
          </a>
          {actions && <div className="ml-auto">{actions}</div>}
        </div>
        {site.root_path && <p className="mt-1 font-mono text-xs text-muted-foreground">{site.root_path}</p>}
      </div>
      <SiteNav siteId={site.id} />
    </div>
  );
}

/** Progress of a job started on a site page (?job=<id> in the URL). */
export function SiteJobCard({ site }: { site: Site }) {
  const [params, setParams] = useSearchParams();
  const jobId = params.get("job");
  const qc = useQueryClient();
  const [result, setResult] = useState<{ job: string; ok: boolean } | null>(null);
  if (!jobId) return null;
  const done = result?.job === jobId ? result : null;
  const StateIcon = !done ? LoaderCircle : done.ok ? CheckCircle2 : XCircle;
  return (
    <Card className={progressFrame}>
      <CardHeader>
        <CardTitle className={cn("flex items-center gap-2", done && !done.ok ? "text-destructive" : "text-primary")}>
          <StateIcon className={cn("size-4", !done && "animate-spin")} />
          {site.status === "creating" ? "Creating site" : !done ? "In progress" : done.ok ? "Done" : "Failed"}
        </CardTitle>
        <Button variant="ghost" size="sm" onClick={() => setParams({}, { replace: true })}>
          Dismiss
        </Button>
      </CardHeader>
      <CardContent>
        <JobProgress
          jobId={jobId}
          onDone={(ok) => {
            setResult({ job: jobId, ok });
            qc.invalidateQueries({ queryKey: siteKey(site.id) });
            qc.invalidateQueries({ queryKey: sitesKey });
            qc.invalidateQueries({ queryKey: certsKey });
            qc.invalidateQueries({ queryKey: databasesKey }); // sizes after an import
            qc.invalidateQueries({ queryKey: ["files", site.id] }); // new dumps in tmp/
          }}
        />
      </CardContent>
    </Card>
  );
}

/** Loads the site from the URL and renders the shared header + tabs around a tab's content. */
export function SiteShell({ title, children }: { title: string; children: (site: Site) => ReactNode }) {
  const id = Number(useParams().id);
  const site = useSite(id);

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
        <Link to="/sites" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
          <ArrowLeft className="size-4" /> Sites
        </Link>
        <p className="text-sm text-destructive">{site.error.message}</p>
      </div>
    );
  }
  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <SiteHeader site={site.data} />
      <h2 className="text-lg font-semibold tracking-tight">{title}</h2>
      <SiteJobCard site={site.data} />
      {children(site.data)}
    </div>
  );
}

/** Small "nothing here yet" block used by the tab pages. */
export function EmptyState({ icon: Icon, title, children }: { icon: LucideIcon; title: string; children?: ReactNode }) {
  return (
    <div className="flex flex-col items-center gap-2 py-10 text-center">
      <span className="mb-1 grid size-12 place-items-center rounded-2xl text-muted-foreground shadow-inset">
        <Icon className="size-5" />
      </span>
      <p className="font-medium">{title}</p>
      {children && <div className="max-w-md text-sm text-muted-foreground">{children}</div>}
    </div>
  );
}
