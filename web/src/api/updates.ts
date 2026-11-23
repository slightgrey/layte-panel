import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { JobStarted, UpgradePlan } from "./types";

const planKey = (packages: string[], fix: boolean) => ["updates", "plan", packages.join(","), fix] as const;

/**
 * GET /updates/plan?packages=&fix=1: what installing would do (apt-get
 * --simulate). fix = repairing an interrupted install. An error with code
 * "apt_broken" means a repair is needed first.
 */
export function useUpdatesPlan(packages: string[] = [], enabled = true, fix = false) {
  const qs = fix ? "?fix=1" : packages.length ? `?packages=${encodeURIComponent(packages.join(","))}` : "";
  return useQuery({
    queryKey: planKey(packages, fix),
    queryFn: () => api<UpgradePlan>("GET", `/updates/plan${qs}`),
    enabled,
    staleTime: 60_000,
    retry: false,
  });
}

/** POST /updates/refresh: apt-get update, then the plan and dashboard reload. */
export function useRefreshUpdates() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>("POST", "/updates/refresh"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["updates"] });
      qc.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
}

/** POST /updates/apply: install the previewed updates (job). */
export function useApplyUpdates() {
  return useMutation({
    mutationFn: (v: { plan_hash: string; packages?: string[]; fix?: boolean }) => api<JobStarted>("POST", "/updates/apply", v),
  });
}
