import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { CertificatesResponse, JobStarted } from "./types";

export const certsKey = ["ssl"] as const;

/** GET /ssl: every certificate on the server, with expiry state. */
export function useCertificates() {
  return useQuery({ queryKey: certsKey, queryFn: () => api<CertificatesResponse>("GET", "/ssl") });
}

/** POST /sites/{id}/ssl: issue a Let's Encrypt certificate (job). */
export function useIssueSSL(siteId: number) {
  return useMutation({
    mutationFn: (email: string) => api<JobStarted>("POST", `/sites/${siteId}/ssl`, { email }),
  });
}

/** DELETE /sites/{id}/ssl: back to HTTP and revoke (job). */
export function useRemoveSSL(siteId: number) {
  return useMutation({ mutationFn: () => api<JobStarted>("DELETE", `/sites/${siteId}/ssl`) });
}

/** POST /sites/{id}/ssl/renew: renew now (forced), or dryRun to test renewal against staging (job). */
export function useRenewSSL(siteId: number) {
  return useMutation({
    mutationFn: (dryRun: boolean) => api<JobStarted>("POST", `/sites/${siteId}/ssl/renew`, { dry_run: dryRun }),
  });
}
