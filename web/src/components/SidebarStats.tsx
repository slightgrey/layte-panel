import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router";
import { api } from "@/api/client";
import type { SystemInfo } from "@/api/types";
import { useUsage } from "@/api/usage";
import { formatBytes } from "@/lib/format";
import { Meter, UsageRows } from "./UsageCard";

const box = "mx-3 mb-2 rounded-[12px] px-3 py-2.5 shadow-inset";

/** GET /system/info (admins): the server's CPU, memory and disks, every 30 s. */
function useSystemInfo() {
  return useQuery({ queryKey: ["system", "info"], queryFn: () => api<SystemInfo>("GET", "/system/info"), refetchInterval: 30_000 });
}

/** Sidebar, admins: how busy the whole server is. */
export function ServerStats() {
  const info = useSystemInfo();
  if (!info.data?.memory) return null;
  const s = info.data;
  const root = s.disks?.find((d) => d.path === "/") ?? s.disks?.[0];
  const loadPct = s.cpus > 0 ? (s.load[0] / s.cpus) * 100 : 0;
  return (
    <Link to="/" className={`${box} block hover:bg-muted/40`} title="Server details on the dashboard">
      <p className="eyebrow mb-2">This server</p>
      <div className="flex flex-col gap-2">
        <Meter compact label="CPU load" used={loadPct} limit={100} text={`${loadPct.toFixed(0)}% of ${s.cpus} CPU${s.cpus === 1 ? "" : "s"}`} />
        <Meter compact label="RAM" used={s.memory.used} limit={s.memory.total} text={`${formatBytes(s.memory.used)} / ${formatBytes(s.memory.total)}`} />
        {root && <Meter compact label="Disk" used={root.used} limit={root.total} text={`${formatBytes(root.used)} / ${formatBytes(root.total)}`} />}
      </div>
    </Link>
  );
}

/** Sidebar, site logins: their plan, wherever they are. */
export function PlanStats() {
  const usage = useUsage();
  if (usage.data?.max_subdomains === undefined) return null;
  return (
    <div className={box}>
      <p className="eyebrow mb-2">Your plan</p>
      <UsageRows u={usage.data} compact />
    </div>
  );
}
