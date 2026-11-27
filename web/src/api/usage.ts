import { useQuery } from "@tanstack/react-query";
import { api } from "./client";

/** A site login's use of its limits (0 limits = none). Read live from the kernel. */
export interface Usage {
  active: boolean;
  cpu_percent: number;
  cpu_limit: number;
  memory_bytes: number;
  memory_limit: number;
  processes: number;
  disk_bytes: number;
  disk_limit: number;
  disk_exact: boolean;
  limits: { cpu_percent: number; memory_mb: number; disk_mb: number };
  subdomains: number;
  max_subdomains: number;
}

/** GET /account/usage (a site login's own) or /users/{id}/usage (admins). Refreshes every 15 s. */
export function useUsage(userId?: number) {
  const path = userId ? `/users/${userId}/usage` : "/account/usage";
  return useQuery({ queryKey: ["usage", userId ?? "me"], queryFn: () => api<Usage>("GET", path), refetchInterval: 15_000 });
}
