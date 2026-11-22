import { ExternalLink, ScrollText } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { useLogSources } from "@/api/logs";
import type { Site } from "@/api/types";
import { LogViewer } from "@/components/LogViewer";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent } from "@/components/ui/card";

export function SiteLogsPage() {
  return <SiteShell title="Logs">{(site) => <LogsTab site={site} />}</SiteShell>;
}

function LogsTab({ site }: { site: Site }) {
  const sources = useLogSources();
  const mine = (sources.data?.sources ?? []).filter((s) => s.site_id === site.id);
  const [picked, setPicked] = useState<string | null>(null);
  // Default to the site's first log until the user picks one.
  const path = picked && mine.some((s) => s.path === picked) ? picked : (mine[0]?.path ?? null);

  if (sources.isPending) return <Spinner />;
  if (sources.error) return <p className="text-sm text-destructive">{sources.error.message}</p>;

  const openInLogs = (
    <Link
      to={path ? `/logs?path=${encodeURIComponent(path)}` : `/logs?site=${site.id}`}
      className="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
    >
      Open in Logs <ExternalLink className="size-3.5" />
    </Link>
  );

  if (mine.length === 0) {
    return (
      <Card>
        <CardContent className="pt-6">
          <EmptyState icon={ScrollText} title="No logs for this site yet">
            Log files show up here once the site has written them.
            <div className="mt-3 flex justify-center">{openInLogs}</div>
          </EmptyState>
        </CardContent>
      </Card>
    );
  }

  return <LogViewer sources={mine} path={path} onPathChange={setPicked} toolbarExtra={openInLogs} heightClass="h-[55vh]" />;
}
