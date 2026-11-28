import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link } from "react-router";
import { api } from "@/api/client";
import type { SystemInfo } from "@/api/types";
import { formatBytes } from "@/lib/format";
import { Meter, RefreshButton, Refreshing, UsagePanel } from "./UsageCard";

const box = "mx-3 mb-2 rounded-[12px] px-3 py-2.5 shadow-inset";

/** GET /system/info (admins): the server's CPU, memory and disks, every 30 s. */
function useSystemInfo() {
  return useQuery({ queryKey: ["system", "info"], queryFn: () => api<SystemInfo>("GET", "/system/info"), refetchInterval: 30_000 });
}

/** Sidebar, admins: how busy the whole server is. */
export function ServerStats() {
  const info = useSystemInfo();
  // "Updated" only after a click, not after every 30 s background refresh.
  const [clicked, setClicked] = useState(false);
  const [fetchedAt, setFetchedAt] = useState(0);
  useEffect(() => {
    if (clicked && !info.isFetching) {
      setFetchedAt(info.dataUpdatedAt);
      setClicked(false);
    }
  }, [clicked, info.isFetching, info.dataUpdatedAt]);
  if (!info.data?.memory) return null;
  const s = info.data;
  const root = s.disks?.find((d) => d.path === "/") ?? s.disks?.[0];
  const loadPct = s.cpus > 0 ? (s.load[0] / s.cpus) * 100 : 0;
  return (
    <div className={box}>
      <div className="mb-2 flex items-center justify-between gap-2">
        <Link to="/" className="eyebrow hover:text-foreground" title="Server details on the dashboard">
          This server
        </Link>
        <RefreshButton busy={info.isFetching && clicked} doneAt={fetchedAt} onClick={() => (setClicked(true), info.refetch())} title="Refresh now" />
      </div>
      <Refreshing busy={info.isFetching && clicked}>
      <div className="flex flex-col gap-2">
        <Meter compact label="CPU load" used={loadPct} limit={100} text={`${loadPct.toFixed(0)}% of ${s.cpus} CPU${s.cpus === 1 ? "" : "s"}`} />
        <Meter compact label="RAM" used={s.memory.used} limit={s.memory.total} text={`${formatBytes(s.memory.used)} / ${formatBytes(s.memory.total)}`} />
        {root && <Meter compact label="Disk" used={root.used} limit={root.total} text={`${formatBytes(root.used)} / ${formatBytes(root.total)}`} />}
      </div>
      </Refreshing>
    </div>
  );
}

/** Sidebar, site logins: their plan, wherever they are. */
export function PlanStats() {
  return (
    <div className={box}>
      <UsagePanel compact title={<p className="eyebrow">Your plan</p>} />
    </div>
  );
}
