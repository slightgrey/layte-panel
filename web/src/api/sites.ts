import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { AdoptPreview, CreateSiteRequest, DeleteSiteRequest, DiskUsage, JobStarted, Site, SitesResponse, ScanResponse, Vhost } from "./types";

/** POST /sites: starts a creation job. */
export function useCreateSite() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateSiteRequest) => api<JobStarted>("POST", "/sites", req),
    onSuccess: () => qc.invalidateQueries({ queryKey: sitesKey }),
  });
}

/** POST /sites/{id}/php: starts a PHP version switch job. */
export function useSwitchPHP(id: number) {
  return useMutation({
    mutationFn: (version: string) => api<JobStarted>("POST", `/sites/${id}/php`, { version }),
  });
}

/** DELETE /sites/{id}: starts a deletion job. */
export function useDeleteSite(id: number) {
  return useMutation({
    mutationFn: (req: DeleteSiteRequest) => api<JobStarted>("DELETE", `/sites/${id}`, req),
  });
}

export const sitesKey = ["sites"] as const;
export const siteKey = (id: number) => ["sites", id] as const;

/** GET /sites */
export function useSites() {
  return useQuery({
    queryKey: sitesKey,
    queryFn: () => api<SitesResponse>("GET", "/sites"),
  });
}

/** GET /sites/{id} */
export function useSite(id: number) {
  return useQuery({
    queryKey: siteKey(id),
    queryFn: () => api<Site>("GET", `/sites/${id}`),
    enabled: id > 0,
  });
}

/** POST /sites/scan: re-read the nginx config and sync imported sites. */
export function useScanSites() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<ScanResponse>("POST", "/sites/scan"),
    onSuccess: (res) => {
      qc.setQueryData<SitesResponse>(sitesKey, { sites: res.sites });
      qc.invalidateQueries({ queryKey: sitesKey, exact: false });
    },
  });
}

/** GET /sites/{id}/vhost: the nginx config, read-only. */
/** GET /sites/{id}/disk: size on disk (du). Walking a big site costs CPU, so it's cached for 5 minutes. */
export function useSiteDisk(id: number, enabled = true) {
  return useQuery({
    queryKey: ["sites", id, "disk"],
    queryFn: () => api<DiskUsage>("GET", `/sites/${id}/disk`),
    enabled: enabled && id > 0,
    staleTime: 5 * 60_000,
    retry: false,
  });
}

export function useSiteVhost(id: number, enabled = true) {
  return useQuery({
    queryKey: ["sites", id, "vhost"],
    queryFn: () => api<Vhost>("GET", `/sites/${id}/vhost`),
    enabled: enabled && id > 0,
    retry: false,
  });
}

/** GET /php/versions: installed PHP-FPM versions. */
export function usePHPVersions() {
  return useQuery({
    queryKey: ["php", "versions"],
    queryFn: () => api<{ versions: string[] }>("GET", "/php/versions"),
    staleTime: 5 * 60_000,
  });
}

/** GET /sites/{id}/adopt: what adopting would change (nothing is changed). */
export function useAdoptPreview(id: number, enabled: boolean) {
  return useQuery({
    queryKey: ["sites", id, "adopt"],
    queryFn: () => api<AdoptPreview>("GET", `/sites/${id}/adopt`),
    enabled,
    retry: false,
    gcTime: 0,
  });
}

/** POST /sites/{id}/adopt: adopt the imported site (job). */
export function useAdoptSite(id: number) {
  return useMutation({
    mutationFn: (v: { username: string; pool_hash: string }) => api<JobStarted>("POST", `/sites/${id}/adopt`, v),
  });
}

