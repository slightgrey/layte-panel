import { ArrowLeft, ExternalLink, Info } from "lucide-react";
import type { ReactNode } from "react";
import { Link, useParams } from "react-router";
import { useSite, useSiteVhost } from "@/api/sites";
import type { Site } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { SiteBadges } from "./SiteBadges";

export function SiteDetailPage() {
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
        </div>
      </div>

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
