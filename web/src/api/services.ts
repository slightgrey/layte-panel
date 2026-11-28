import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { Service } from "./types";

/** A row of the Services page: the dashboard's service, plus what it uses and its one action. */
export interface ServiceDetail extends Service {
  since?: string;
  memory_bytes?: number;
  /** reload (safe, after the config test) or restart (no reload exists); none for Layte's own services. */
  action?: { op: "reload" | "restart"; disruptive?: boolean };
}

export const servicesKey = ["system", "services"] as const;

/** GET /system/services (admins), every 15 s. */
export function useServicesDetail() {
  return useQuery({
    queryKey: servicesKey,
    queryFn: () => api<{ services: ServiceDetail[] }>("GET", "/system/services"),
    refetchInterval: 15_000,
  });
}

/** POST /system/services/{name}/{op}: reload or restart one service. */
export function useServiceControl() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; op: string }) => api<{ name: string; active: string }>("POST", `/system/services/${encodeURIComponent(v.name)}/${v.op}`),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: servicesKey });
      qc.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
}

/** Whether the server needs a reboot (updates), and whether one is scheduled. */
export interface RebootStatus {
  required: boolean;
  packages?: string[];
  scheduled?: string;
  hostname: string;
}

export const rebootKey = ["system", "reboot"] as const;

/** GET /system/reboot (admins). Polls every 5 s while a reboot is scheduled. */
export function useRebootStatus() {
  return useQuery({
    queryKey: rebootKey,
    queryFn: () => api<RebootStatus>("GET", "/system/reboot"),
    refetchInterval: (q) => (q.state.data?.scheduled ? 5000 : 60_000),
  });
}

/** POST /system/reboot {confirm: hostname}: reboot in a minute. */
export function useReboot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (confirm: string) => api<RebootStatus>("POST", "/system/reboot", { confirm }),
    onSuccess: (st) => qc.setQueryData(rebootKey, st),
  });
}

/** DELETE /system/reboot: cancel a scheduled reboot. */
export function useCancelReboot() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<RebootStatus>("DELETE", "/system/reboot"),
    onSuccess: (st) => qc.setQueryData(rebootKey, st),
  });
}
