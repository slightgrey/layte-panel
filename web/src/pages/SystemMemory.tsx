import { useProcesses } from "@/api/system";
import { ShareBar, SystemShell } from "@/components/SystemShell";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, percent } from "@/lib/format";
import { cn } from "@/lib/utils";

export function SystemMemoryPage() {
  const q = useProcesses();
  return (
    <SystemShell title="Memory" description="What is using the server's RAM. Refreshes every 5 seconds.">
      {q.isPending ? (
        <Spinner />
      ) : q.error ? (
        <Notice tone="warning">Could not read memory use: {q.error.message}</Notice>
      ) : (
        <Body data={q.data} />
      )}
    </SystemShell>
  );
}

function Body({ data }: { data: NonNullable<ReturnType<typeof useProcesses>["data"]> }) {
  const m = data.memory;
  const cache = m.cached + m.buffers;
  const parts = [
    { label: "Used by programs", bytes: m.apps_used, className: "bg-primary", hint: "Can't be freed without stopping something." },
    { label: "Cache", bytes: Math.min(cache, Math.max(0, m.total - m.apps_used - m.free)), className: "bg-success", hint: "Recently read files. Linux frees it as soon as programs need RAM." },
    { label: "Free", bytes: m.free, className: "bg-muted-foreground/40", hint: "Not used at all." },
  ];
  const swapUsed = m.swap_total - m.swap_free;
  const groups = data.groups.slice(0, 15);
  const procs = data.processes.slice(0, 20);

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>RAM: {formatBytes(m.total)}</CardTitle>
          <span className="text-sm tabular-nums">{percent(m.apps_used, m.total)}% used by programs</span>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="flex h-4 w-full overflow-hidden rounded-full bg-background p-[2px] shadow-inset" aria-label="Memory split">
            {parts.map((p) => (
              <div key={p.label} className={cn("h-full first:rounded-l-full last:rounded-r-full", p.className)} style={{ width: `${percent(p.bytes, m.total)}%` }} title={`${p.label}: ${formatBytes(p.bytes)}`} />
            ))}
          </div>
          <div className="grid gap-3 sm:grid-cols-3">
            {parts.map((p) => (
              <div key={p.label} className="flex gap-2">
                <span className={cn("mt-1.5 size-2.5 shrink-0 rounded-full", p.className)} />
                <div>
                  <div className="text-sm">
                    {p.label}: <span className="font-semibold tabular-nums">{formatBytes(p.bytes)}</span>
                  </div>
                  <p className="text-xs text-muted-foreground">{p.hint}</p>
                </div>
              </div>
            ))}
          </div>
          {m.swap_total > 0 && (
            <p className="text-sm text-muted-foreground">
              Swap: {formatBytes(swapUsed)} of {formatBytes(m.swap_total)} used
              {swapUsed > m.swap_total / 2 && " — a lot of swap in use means the server is short of RAM."}
            </p>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>By service and site</CardTitle>
          <span className="text-xs text-muted-foreground">PHP pools are per site</span>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>What</TableHead>
                <TableHead className="text-right">Processes</TableHead>
                <TableHead className="text-right">Memory</TableHead>
                <TableHead className="w-1/3">Share of RAM</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.map((g) => (
                <TableRow key={g.name}>
                  <TableCell className="font-medium">{g.name}</TableCell>
                  <TableCell className="text-right tabular-nums">{g.processes}</TableCell>
                  <TableCell className="text-right tabular-nums">{formatBytes(g.rss)}</TableCell>
                  <TableCell>
                    <ShareBar value={percent(g.rss, m.total)} />
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
          <p className="mt-3 text-xs text-muted-foreground">
            Memory per process counts shared parts (like PHP's own code) once for every process, so these numbers can add up to
            more than what is really used.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Top processes</CardTitle>
        </CardHeader>
        <CardContent>
          <ProcessTable procs={procs} metric="rss" total={m.total} />
        </CardContent>
      </Card>
    </>
  );
}

/** Processes with their memory (or CPU) and command. Shared with the Load page. */
export function ProcessTable({
  procs,
  metric,
  total = 0,
}: {
  procs: NonNullable<ReturnType<typeof useProcesses>["data"]>["processes"];
  metric: "rss" | "cpu";
  total?: number;
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Process</TableHead>
          <TableHead>User</TableHead>
          <TableHead className="text-right">{metric === "rss" ? "Memory" : "CPU"}</TableHead>
          <TableHead className="hidden md:table-cell">Command</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {procs.map((p) => (
          <TableRow key={p.pid}>
            <TableCell>
              <div className="font-medium">{p.name}</div>
              <div className="font-mono text-xs text-muted-foreground">
                {p.pid} · {p.group}
              </div>
            </TableCell>
            <TableCell className="text-sm">{p.user}</TableCell>
            <TableCell className="text-right tabular-nums">
              {metric === "rss" ? (
                <>
                  {formatBytes(p.rss)}
                  {total > 0 && <div className="text-xs text-muted-foreground">{percent(p.rss, total)}%</div>}
                </>
              ) : (
                `${p.cpu.toFixed(1)}%`
              )}
            </TableCell>
            <TableCell className="hidden max-w-md md:table-cell">
              <code className="block truncate font-mono text-xs text-muted-foreground" title={p.command}>
                {p.command || "–"}
              </code>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
