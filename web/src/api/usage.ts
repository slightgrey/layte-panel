import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
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
  /** Disk use is measured at most every 5 minutes (or on refresh); RAM and CPU every time. */
  disk_checked_at: string;
  limits: { cpu_percent: number; memory_mb: number; disk_mb: number };
  subdomains: number;
  max_subdomains: number;
}

const usagePath = (userId?: number) => (userId ? `/users/${userId}/usage` : "/account/usage");
const usageKey = (userId?: number) => ["usage", userId ?? "me"] as const;

/**
 * GET /account/usage (a site login's own) or /users/{id}/usage (admins).
 * RAM and CPU refresh every 30 s (cheap); the server reuses the disk reading
 * for 5 minutes. Background tabs don't refresh at all.
 */
export function useUsage(userId?: number) {
  return useQuery({ queryKey: usageKey(userId), queryFn: () => api<Usage>("GET", usagePath(userId)), refetchInterval: 30_000 });
}

/** The refresh button: everything measured now, disk included (?fresh=1). */
export function useRefreshUsage(userId?: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<Usage>("GET", usagePath(userId) + "?fresh=1"),
    onSuccess: (u) => qc.setQueryData(usageKey(userId), u),
  });
}
