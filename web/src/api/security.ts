import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { JobStarted, SecurityResponse } from "./types";

export const securityKey = ["security"] as const;

/** GET /security */
export function useSecurity() {
  return useQuery({ queryKey: securityKey, queryFn: () => api<SecurityResponse>("GET", "/security") });
}

function useSecurityMutation<V, R = void>(fn: (v: V) => Promise<R>) {
  const qc = useQueryClient();
  return useMutation({ mutationFn: fn, onSuccess: () => qc.invalidateQueries({ queryKey: securityKey }) });
}

export const useAllowRule = () =>
  useSecurityMutation((v: { port: string; proto: string; from: string }) => api<void>("POST", "/security/ufw/rules", v));

export const useDeleteRule = () =>
  useSecurityMutation((v: { num: number; expect: string }) => api<void>("DELETE", `/security/ufw/rules/${v.num}`, { expect: v.expect }));

export const useSetFirewall = () => useSecurityMutation((enable: boolean) => api<void>("POST", "/security/ufw/enabled", { enable }));

/** POST /security/cloudflare: turn "Behind Cloudflare" on (or update its list) or off. */
export const useSetCloudflare = () => useSecurityMutation((enabled: boolean) => api<void>("POST", "/security/cloudflare", { enabled }));

export const useInstallUpdates = () => useMutation({ mutationFn: () => api<JobStarted>("POST", "/security/updates") });
