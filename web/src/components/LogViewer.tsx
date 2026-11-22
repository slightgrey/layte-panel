import { Eraser, Pause, Play } from "lucide-react";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { useLogStream } from "@/api/logs";
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

function SourceOption({ s }: { s: LogSource }) {
  return (
    <option value={s.path}>
      {s.label} ({s.path.split("/").pop()})
    </option>
  );
}

/**
 * Log picker + live tail with filter, pause/resume and clear.
 * The parent owns which path is selected (URL param or local state).
 */
export function LogViewer({
  sources,
  path,
  onPathChange,
  toolbarExtra,
  heightClass = "h-[65vh]",
}: {
  sources: LogSource[];
  path: string | null;
  onPathChange: (path: string) => void;
  toolbarExtra?: ReactNode;
  heightClass?: string;
}) {
  const [paused, setPaused] = useState(false);
  const [filter, setFilter] = useState("");
  const stream = useLogStream(path, paused);
  const bottom = useRef<HTMLDivElement>(null);
  const groups = groupSources(sources);

  const shown = useMemo(() => {
    const f = filter.trim().toLowerCase();
    return f ? stream.lines.filter((l) => l.toLowerCase().includes(f)) : stream.lines;
  }, [stream.lines, filter]);

  useEffect(() => {
    if (!paused) bottom.current?.scrollIntoView({ block: "end" });
  }, [shown, paused]);

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Select aria-label="Log file" className="w-auto min-w-56" value={path ?? ""} onChange={(e) => onPathChange(e.target.value)}>
          {groups.length > 1
            ? groups.map(([group, items]) => (
                <optgroup key={group} label={group}>
                  {items.map((s) => (
                    <SourceOption key={s.path} s={s} />
                  ))}
                </optgroup>
              ))
            : sources.map((s) => <SourceOption key={s.path} s={s} />)}
        </Select>
        <Input aria-label="Filter" placeholder="Filter lines" className="w-48" value={filter} onChange={(e) => setFilter(e.target.value)} />
        <Button variant="outline" size="sm" onClick={() => setPaused((p) => !p)}>
          {paused ? <Play /> : <Pause />} {paused ? "Resume" : "Pause"}
        </Button>
        <Button variant="ghost" size="sm" onClick={stream.clear}>
          <Eraser /> Clear
        </Button>
        <span className="ml-auto flex items-center gap-3">
          {stream.status === "live" && !paused && <Badge variant="success">live</Badge>}
          {paused && <Badge variant="warning">paused</Badge>}
          {stream.status === "error" && <Badge variant="danger">disconnected</Badge>}
          {toolbarExtra}
        </span>
      </div>

      {stream.error && <p className="text-sm text-destructive">{stream.error}</p>}
      <Card>
        <CardContent className="p-0">
          <div className={cn("overflow-auto rounded-[1.25rem] bg-background p-4 shadow-inset font-mono text-xs leading-relaxed", heightClass)}>
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
    </>
  );
}
