import { Check, Gauge, RefreshCw } from "lucide-react";
import { type ReactNode, useEffect, useState } from "react";
import { type Usage, useRefreshUsage, useUsage } from "@/api/usage";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent, CardTitle } from "@/components/ui/card";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";

/** true for 2 s after `doneAt` changes (a refresh just finished). */
function useJustDone(doneAt: number) {
  const [show, setShow] = useState(false);
  useEffect(() => {
    if (!doneAt) return;
    setShow(true);
    const t = setTimeout(() => setShow(false), 2000);
    return () => clearTimeout(t);
  }, [doneAt]);
  return show;
}

/** ↻ with a spin while busy, and a short "Updated" after. */
export function RefreshButton({ busy, doneAt, onClick, title }: { busy: boolean; doneAt: number; onClick: () => void; title: string }) {
  const done = useJustDone(doneAt);
  return (
    <span className="inline-flex items-center gap-1.5">
      {busy ? (
        <span className="text-[0.6875rem] text-muted-foreground" role="status">
          Refreshing…
        </span>
      ) : done ? (
        <span className="inline-flex items-center gap-0.5 text-[0.6875rem] text-success" role="status">
          <Check className="size-3" /> Updated
        </span>
      ) : null}
      <button
        type="button"
        onClick={onClick}
        disabled={busy}
        className="inline-flex items-center rounded-md p-0.5 text-muted-foreground hover:text-foreground disabled:opacity-60"
        title={title}
        aria-label={title}
      >
        <RefreshCw className={cn("size-3.5", busy && "animate-spin")} />
      </button>
    </span>
  );
}

/** Dims what it wraps while it's being refreshed. */
export function Refreshing({ busy, children }: { busy: boolean; children: ReactNode }) {
  return <div className={cn("transition-opacity", busy && "pointer-events-none animate-pulse opacity-40")}>{children}</div>;
}

/** A login's usage with its header (title + refresh) — the sidebar box, the plan card, the admin's view. */
export function UsagePanel({ userId, title, compact = false }: { userId?: number; title: ReactNode; compact?: boolean }) {
  const usage = useUsage(userId);
  const refresh = useRefreshUsage(userId);
  const when = usage.data?.disk_checked_at ? new Date(usage.data.disk_checked_at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }) : "";
  if (usage.isPending) return <Spinner />;
  if (usage.error) return <p className="text-xs text-muted-foreground">Usage unavailable: {usage.error.message}</p>;
  if (usage.data.max_subdomains === undefined) return null;
  return (
    <div className="flex flex-col">
      <div className={cn("flex items-center justify-between gap-2", compact ? "mb-2" : "mb-3")}>
        {title}
        <RefreshButton
          busy={refresh.isPending}
          doneAt={refresh.isSuccess ? refresh.submittedAt : 0}
          onClick={() => refresh.mutate()}
          title={`Refresh now${when ? ` (disk checked at ${when})` : ""}`}
        />
      </div>
      <Refreshing busy={refresh.isPending}>
        <UsageRows u={usage.data} compact={compact} />
      </Refreshing>
    </div>
  );
}

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
  return (
    <Card>
      <CardContent className="pt-5">
        <UsagePanel
          userId={userId}
          title={
            <CardTitle className="flex items-center gap-2">
              <Gauge className="size-4" /> {title}
            </CardTitle>
          }
        />
        <p className="mt-3 text-xs text-muted-foreground">
          RAM and CPU count every PHP request and cron job of these sites together. 100% CPU is one full core.
        </p>
      </CardContent>
    </Card>
  );
}
