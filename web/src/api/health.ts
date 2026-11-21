import { useQuery } from "@tanstack/react-query";
import { api } from "./client";

export interface Health {
  status: string;
  version: string;
}

/** GET /health: public; used for the version shown in the sidebar. */
export function useHealth() {
  return useQuery({
    queryKey: ["health"],
    queryFn: () => api<Health>("GET", "/health"),
    staleTime: Infinity, // the version only changes on an upgrade (which reloads the page)
  });
}

/** "0.1.2" → "v0.1.2"; dev builds ("dev", "v0.1.1-2-gabc") are shown as-is. */
export function formatVersion(v: string): string {
  return /^\d/.test(v) ? `v${v}` : v;
}
