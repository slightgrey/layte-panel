import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { Dashboard } from "./types";

/** GET /dashboard: refreshed every 30 seconds while the tab is open. */
export function useDashboard() {
  return useQuery({
    queryKey: ["dashboard"],
    queryFn: () => api<Dashboard>("GET", "/dashboard"),
    refetchInterval: 30_000,
  });
}
