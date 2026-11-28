import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";

/** Whether disk quotas work on the filesystem that holds /home. */
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
}

export const quotaKey = ["quota"] as const;

/** GET /quota (admins). */
export function useQuotaStatus() {
  return useQuery({ queryKey: quotaKey, queryFn: () => api<QuotaStatus>("GET", "/quota"), staleTime: 60_000 });
}

/** POST /quota/enable (admins): installs the tools, adds usrquota to /etc/fstab, remounts. */
export function useEnableQuotas() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<QuotaStatus>("POST", "/quota/enable"),
    onSuccess: (st) => qc.setQueryData(quotaKey, st),
  });
}
