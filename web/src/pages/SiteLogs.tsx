import { ExternalLink, Pause, ScrollText } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import type { Site } from "@/api/types";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

// UI only for now: the viewer isn't streaming yet. The global Logs page already
// supports ?site=<id>, so the link below works today.
export function SiteLogsPage() {
  return <SiteShell title="Logs" preview>{(site) => <LogsTab site={site} />}</SiteShell>;
}

function LogsTab({ site }: { site: Site }) {
  const base = site.root_path ? site.root_path.replace(/\/public\/?$/, "") + "/logs" : "/var/log/nginx";
  const files = [
    { label: "Access log", path: `${base}/access.log` },
    { label: "Error log", path: `${base}/error.log` },
    { label: "PHP errors", path: `${base}/php-error.log` },
    { label: "Cron output", path: `${base}/cron.log` },
  ];
  const [path, setPath] = useState(files[0]!.path);

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Select aria-label="Log file" className="w-auto min-w-56" value={path} onChange={(e) => setPath(e.target.value)}>
          {files.map((f) => (
            <option key={f.path} value={f.path}>
              {f.label}
            </option>
          ))}
        </Select>
        <Input aria-label="Filter" placeholder="Filter lines" className="w-48" disabled />
        <Button variant="outline" size="sm" disabled>
          <Pause /> Pause
        </Button>
        <Link
          to={`/logs?site=${site.id}`}
          className="ml-auto inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
        >
          Open in Logs <ExternalLink className="size-3.5" />
        </Link>
      </div>
      <Card>
        <CardContent className="p-0">
          <div className="h-[55vh] overflow-auto rounded-[1.25rem] bg-background p-4 font-mono text-xs leading-relaxed shadow-inset">
            <EmptyState icon={ScrollText} title="Live tail coming soon">
              This tab will stream this site's logs. Until then, use <strong>Open in Logs</strong>.
            </EmptyState>
          </div>
        </CardContent>
      </Card>
      <p className="font-mono text-xs text-muted-foreground">{path}</p>
    </>
  );
}
