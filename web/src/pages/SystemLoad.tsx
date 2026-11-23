import { useProcesses } from "@/api/system";
import { ShareBar, SystemShell } from "@/components/SystemShell";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatUptime } from "@/lib/format";
import { cn } from "@/lib/utils";
import { ProcessTable } from "./SystemMemory";

export function SystemLoadPage() {
  const q = useProcesses();
  return (
    <SystemShell title="Load" description="How busy the CPU is, and with what. Refreshes every 5 seconds.">
      {q.isPending ? (
        <Spinner />
      ) : q.error ? (
        <Notice tone="warning">Could not read CPU use: {q.error.message}</Notice>
      ) : (
        <Body data={q.data} />
      )}
    </SystemShell>
  );
}

function Body({ data }: { data: NonNullable<ReturnType<typeof useProcesses>["data"]> }) {
  const cpus = data.cpus || 1;
  const busy = [...data.groups].filter((g) => g.cpu >= 0.05).sort((a, b) => b.cpu - a.cpu).slice(0, 12);
  const top = [...data.processes].filter((p) => p.cpu >= 0.05).sort((a, b) => b.cpu - a.cpu).slice(0, 15);
  const used = data.groups.reduce((sum, g) => sum + g.cpu, 0);
  const windows: [string, number][] = [
    ["Last minute", data.load[0]],
    ["Last 5 minutes", data.load[1]],
    ["Last 15 minutes", data.load[2]],
  ];

  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Load average · {cpus} vCPU</CardTitle>
          <span className="text-xs text-muted-foreground">up {formatUptime(data.uptime_seconds)}</span>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="grid gap-4 sm:grid-cols-3">
            {windows.map(([label, v]) => {
              const ratio = v / cpus;
              return (
                <div key={label} className="flex flex-col gap-1.5">
                  <span className="eyebrow">{label}</span>
                  <span className={cn("text-2xl font-semibold tabular-nums", ratio >= 1.5 ? "text-destructive" : ratio >= 1 ? "text-warning" : "")}>{v.toFixed(2)}</span>
                  <ShareBar value={Math.min(100, ratio * 100)} className={ratio >= 1.5 ? "bg-destructive" : ratio >= 1 ? "bg-warning" : undefined} />
                </div>
              );
            })}
          </div>
          <p className="text-sm text-muted-foreground">
            The load is how many tasks want the CPU at once. With {cpus} vCPU, up to {cpus.toFixed(0)} is fine; above that, tasks
            wait their turn and pages get slower.
          </p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Using the CPU now</CardTitle>
          <span className="text-sm tabular-nums">{used.toFixed(1)}% of all CPUs</span>
        </CardHeader>
        <CardContent>
          {busy.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">Nothing is using the CPU right now.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>What</TableHead>
                  <TableHead className="text-right">Processes</TableHead>
                  <TableHead className="text-right">CPU</TableHead>
                  <TableHead className="w-1/3">Share</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {busy.map((g) => (
                  <TableRow key={g.name}>
                    <TableCell className="font-medium">{g.name}</TableCell>
                    <TableCell className="text-right tabular-nums">{g.processes}</TableCell>
                    <TableCell className="text-right tabular-nums">{g.cpu.toFixed(1)}%</TableCell>
                    <TableCell>
                      <ShareBar value={g.cpu} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          <p className="mt-3 text-xs text-muted-foreground">Measured over the last {(data.sample_ms / 1000).toFixed(1)} s. 100% = every CPU fully busy.</p>
        </CardContent>
      </Card>

      {top.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Busiest processes</CardTitle>
          </CardHeader>
          <CardContent>
            <ProcessTable procs={top} metric="cpu" />
          </CardContent>
        </Card>
      )}
    </>
  );
}
