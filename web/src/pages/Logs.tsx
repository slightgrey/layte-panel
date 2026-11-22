import { Eraser, Pause, Play } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { useLogSources, useLogStream } from "@/api/logs";
import type { LogSource } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { cn } from "@/lib/utils";

/** Group sources: server-wide first, then one group per site. */
function groupSources(sources: LogSource[]) {
  const groups = new Map<string, LogSource[]>();
  for (const s of sources) {
    const key = s.domain ?? "Server";
    groups.set(key, [...(groups.get(key) ?? []), s]);
  }
  return [...groups.entries()].sort(([a], [b]) => (a === "Server" ? -1 : b === "Server" ? 1 : a.localeCompare(b)));
}

function lineClass(line: string) {
  if (/\b(error|crit|alert|emerg|fatal)\b/i.test(line) || /" 5\d\d /.test(line)) return "text-destructive";
  if (/\b(warn|warning)\b/i.test(line) || /" 4\d\d /.test(line)) return "text-warning";
  return "";
}

export function LogsPage() {
  const sources = useLogSources();
  const [params, setParams] = useSearchParams();
  const path = params.get("path");
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const stream = useLogStream(path, paused);
  const bottom = useRef<HTMLDivElement>(null);

  // Pick a default: ?site=<id> → that site's first log, else the first source.
  useEffect(() => {
    const list = sources.data?.sources;
    if (!list?.length || path) return;
    const site = Number(params.get("site"));
    const pick = (site && list.find((s) => s.site_id === site)) || list[0]!;
    setParams({ path: pick.path }, { replace: true });
  }, [sources.data, path, params, setParams]);

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? stream.lines.filter((l) => l.toLowerCase().includes(f)) : stream.lines;
  }, [stream.lines, filter]);

  useEffect(() => {
    if (!paused) bottom.current?.scrollIntoView({ block: "end" });
  }, [shown, paused]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Logs</h1>
        <p className="text-sm text-muted-foreground">Live view of nginx, PHP-FPM and site logs.</p>
      </div>

      {sources.isPending ? (
        <Spinner />
      ) : sources.error ? (
        <p className="text-sm text-destructive">{sources.error.message}</p>
      ) : (
        <div className="flex flex-wrap items-center gap-2">
          <Select aria-label="Log file" className="w-auto min-w-72" value={path ?? ""} onChange={(e) => setParams({ path: e.target.value })}>
            {groupSources(sources.data.sources).map(([group, items]) => (
              <optgroup key={group} label={group}>
                {items.map((s) => (
                  <option key={s.path} value={s.path}>
                    {s.label} ({s.path.split("/").pop()})
                  </option>
                ))}
              </optgroup>
            ))}
          </Select>
          <Input aria-label="Filter" placeholder="Filter lines" className="w-48" value={filter} onChange={(e) => setFilter(e.target.value)} />
          <Button variant="outline" size="sm" onClick={() => setPaused((p) => !p)}>
            {paused ? <Play /> : <Pause />} {paused ? "Resume" : "Pause"}
          </Button>
          <Button variant="ghost" size="sm" onClick={stream.clear}>
            <Eraser /> Clear
          </Button>
          <span className="ml-auto">
            {stream.status === "live" && !paused && <Badge variant="success">live</Badge>}
            {paused && <Badge variant="warning">paused</Badge>}
            {stream.status === "error" && <Badge variant="danger">disconnected</Badge>}
          </span>
        </div>
      )}

      {stream.error && <p className="text-sm text-destructive">{stream.error}</p>}
      <Card>
        <CardContent className="p-0">
          <div className="h-[65vh] overflow-auto rounded-[1.25rem] bg-background p-4 shadow-inset font-mono text-xs leading-relaxed">
            {path && stream.status === "connecting" && <Spinner />}
            {shown.length === 0 && stream.status === "live" && <p className="text-muted-foreground">{filter ? "No matching lines." : "The log is empty."}</p>}
            {shown.map((l, i) => (
              <div key={i} className={cn("break-all whitespace-pre-wrap", lineClass(l))}>
                {l}
              </div>
            ))}
            <div ref={bottom} />
          </div>
        </CardContent>
      </Card>
      {path && <p className="font-mono text-xs text-muted-foreground">{path}</p>}
    </div>
  );
}
