import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { CronResponse } from "./types";

export const cronKey = ["cron"] as const;

/** GET /cron */
export function useCron() {
  return useQuery({ queryKey: cronKey, queryFn: () => api<CronResponse>("GET", "/cron") });
}

function useCronMutation<V>(fn: (v: V) => Promise<unknown>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: fn, onSuccess: () => qc.invalidateQueries({ queryKey: cronKey }) });
}

/** POST /sites/{id}/cron */
export function useAddCron() {
  return useCronMutation((v: { siteId: number; schedule: string; command: string }) =>
    api("POST", `/sites/${v.siteId}/cron`, { schedule: v.schedule, command: v.command }),
  );
}

/** PATCH /cron/{id} */
export function useToggleCron() {
  return useCronMutation((v: { id: number; enabled: boolean }) => api("PATCH", `/cron/${v.id}`, { enabled: v.enabled }));
}

/** DELETE /cron/{id} */
export function useDeleteCron() {
  return useCronMutation((id: number) => api("DELETE", `/cron/${id}`));
}
