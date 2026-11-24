import { Bug, Power } from "lucide-react";
import { useLogSources } from "@/api/logs";
import { useSetWPDebug, useWPDebug } from "@/api/sites";
import type { LogSource, Site } from "@/api/types";
import { LogViewer } from "@/components/LogViewer";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";

export function SiteWPDebugPage() {
  return <SiteShell title="WP Debug">{(site) => <WPDebugTab site={site} />}</SiteShell>;
}

function WPDebugTab({ site }: { site: Site }) {
  const usable = site.wordpress && (site.managed || site.adopted) && site.status === "active";
  if (!usable) {
    return (
      <Card>
        <CardContent className="pt-6">
          <EmptyState icon={Bug} title="WP Debug isn't available for this site">
            {!site.wordpress
              ? "This site doesn't run WordPress."
              : site.status !== "active"
                ? `The site is ${site.status}.`
                : "This site is read-only here. Adopt it to turn on WP Debug."}
          </EmptyState>
        </CardContent>
      </Card>
    );
  }
  return <DebugPanel site={site} />;
}

function DebugPanel({ site }: { site: Site }) {
  const debug = useWPDebug(site.id);
  const set = useSetWPDebug(site.id);
  const sources = useLogSources();

  if (debug.isPending) return <Spinner />;
  if (debug.error) return <p className="text-sm text-destructive">{debug.error.message}</p>;

  const { enabled, log_path: logPath } = debug.data;
  const logExists = !!logPath && (sources.data?.sources ?? []).some((s) => s.path === logPath);
  const source: LogSource | null = logPath ? { path: logPath, label: "WP debug log", domain: site.domain, site_id: site.id } : null;

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>WordPress debugging</CardTitle>
          <div className="flex items-center gap-2">
            {enabled ? <Badge variant="warning">on</Badge> : <Badge>off</Badge>}
            <Button variant={enabled ? "outline" : "default"} size="sm" disabled={set.isPending} onClick={() => set.mutate(!enabled)}>
              {set.isPending ? <Spinner /> : <Power />} {enabled ? "Disable WP Debug" : "Enable WP Debug"}
            </Button>
          </div>
        </CardHeader>
        <CardContent className="flex flex-col gap-3">
          <p className="text-sm text-muted-foreground">
            Turns on <span className="font-mono text-xs">WP_DEBUG</span> in wp-config.php, so PHP errors, warnings and notices from
            WordPress, plugins and themes are written to a log. Visitors never see them (
            <span className="font-mono text-xs">WP_DEBUG_DISPLAY</span> stays off). Layte checks the site still answers afterwards
            and puts wp-config.php back if it doesn't.
          </p>
          {logPath ? (
            <p className="text-xs text-muted-foreground">
              Messages go to <span className="font-mono">{logPath}</span>
              {site.adopted && " (this site's PHP error log)"}.
            </p>
          ) : (
            <p className="text-xs text-muted-foreground">
              This site's PHP pool has no error log of its own, so messages go to the server's PHP-FPM log (see the Logs page).
            </p>
          )}
          {enabled && <Notice tone="warning">Debug logging slows the site down a little. Turn it off when you're done.</Notice>}
          {set.error && <p className="text-sm text-destructive">{set.error.message}</p>}
        </CardContent>
      </Card>

      {source && (enabled || logExists) && (
        <LogViewer sources={[source]} path={source.path} onPathChange={() => {}} heightClass="h-[55vh]" />
      )}
    </>
  );
}
