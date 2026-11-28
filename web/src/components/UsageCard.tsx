import { Gauge } from "lucide-react";
import { type Usage, useUsage } from "@/api/usage";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";

/** One "used of limit" row with a bar (no bar without a limit). */
export function Meter({ label, used, limit, text, compact = false }: { label: string; used: number; limit: number; text: string; compact?: boolean }) {
  const pct = limit > 0 ? Math.min(100, (used / limit) * 100) : 0;
  return (
    <div className="flex flex-col gap-1">
      <div className={cn("flex justify-between gap-3", compact ? "text-[0.75rem]" : "text-sm")}>
        <span className="text-muted-foreground">{label}</span>
        <span className="tabular-nums">{text}</span>
      </div>
      {limit > 0 && (
        <div className={cn("overflow-hidden rounded-full bg-background shadow-inset", compact ? "h-1.5" : "h-2")}  role="meter" aria-label={label} aria-valuenow={Math.round(pct)} aria-valuemin={0} aria-valuemax={100}>
          <div className={cn("h-full rounded-full", pct >= 90 ? "bg-destructive" : pct >= 75 ? "bg-warning" : "bg-primary")} style={{ width: `${pct}%` }} />
        </div>
      )}
    </div>
  );
}

const of = (used: string, limit: number, fmt: (n: number) => string) => (limit > 0 ? `${used} of ${fmt(limit)}` : `${used} (no limit)`);

export function UsageRows({ u, compact = false }: { u: Usage; compact?: boolean }) {
  // Compact (sidebar): short texts, so they fit next to the label.
  const short = (used: string, limit: number, fmt: (n: number) => string) => (limit > 0 ? `${used} / ${fmt(limit)}` : used);
  const t = compact ? short : of;
  return (
    <div className={cn("flex flex-col", compact ? "gap-2" : "gap-3")}>
      <Meter compact={compact} label="Disk" used={u.disk_bytes} limit={u.disk_limit} text={t(formatBytes(u.disk_bytes), u.disk_limit, formatBytes) + (u.disk_exact || compact ? "" : " · measured")} />
      <Meter compact={compact} label={compact ? "RAM" : "RAM now"} used={u.memory_bytes} limit={u.memory_limit} text={u.active ? t(formatBytes(u.memory_bytes), u.memory_limit, formatBytes) : "idle"} />
      <Meter compact={compact} label={compact ? "CPU" : "CPU now"} used={u.cpu_percent} limit={u.cpu_limit} text={u.active ? t(`${u.cpu_percent.toFixed(0)}%`, u.cpu_limit, (n) => `${n}%`) : "idle"} />
      <Meter compact={compact} label="Subdomains" used={u.subdomains} limit={u.max_subdomains} text={`${u.subdomains} / ${u.max_subdomains}`} />
    </div>
  );
}

/** A site login's usage against its limits: "Your plan" for the login, the same for admins. */
export function UsageCard({ userId, title = "Your plan" }: { userId?: number; title?: string }) {
  const usage = useUsage(userId);
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Gauge className="size-4" /> {title}
        </CardTitle>
      </CardHeader>
      <CardContent>
        {usage.isPending ? <Spinner /> : usage.error ? <p className="text-sm text-destructive">{usage.error.message}</p> : <UsageRows u={usage.data} />}
        <p className="mt-3 text-xs text-muted-foreground">
          RAM and CPU count every PHP request and cron job of these sites together. 100% CPU is one full core.
        </p>
      </CardContent>
    </Card>
  );
}
