import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";

/** Whether disk quotas work on the filesystem that holds /home, and what Layte is doing about it. */
export interface QuotaStatus {
  enabled: boolean;
  supported: boolean;
  reason?: string;
  mount: string;
  fs_type: string;
  tools: boolean;
  /** The running kernel has the quota format; if not, kernel_packages adds it (about 110 MB). */
  kernel: boolean;
  kernel_packages?: string[];
  /** Turning quotas on or off runs on the server in the background (minutes): this survives leaving the page. */
  busy?: "enabling" | "disabling";
  /** Why the last turn on/off failed. */
  last_error?: string;
}

export const quotaKey = ["quota"] as const;

/** GET /quota (admins). Polls every 2 s while quotas are being turned on or off. */
export function useQuotaStatus() {
  return useQuery({
    queryKey: quotaKey,
    queryFn: () => api<QuotaStatus>("GET", "/quota"),
    staleTime: 60_000,
    refetchInterval: (q) => (q.state.data?.busy ? 2000 : false),
  });
}

/** POST /quota/enable (admins): starts installing the tools, adding usrquota to /etc/fstab, remounting. */
export function useEnableQuotas() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<QuotaStatus>("POST", "/quota/enable"),
    onSuccess: (st) => qc.setQueryData<QuotaStatus>(quotaKey, (old) => (old ? { ...old, ...st, last_error: undefined } : st)),
    onSettled: () => qc.invalidateQueries({ queryKey: quotaKey }),
  });
}

/** POST /quota/disable (admins): starts turning quotas off; every login's disk limit goes back to none. */
export function useDisableQuotas() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<QuotaStatus>("POST", "/quota/disable"),
    onSuccess: (st) => qc.setQueryData<QuotaStatus>(quotaKey, (old) => (old ? { ...old, ...st, last_error: undefined } : st)),
    onSettled: () => qc.invalidateQueries({ queryKey: quotaKey }),
  });
}
