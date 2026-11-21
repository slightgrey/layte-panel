import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { Site, SitesResponse, ScanResponse, Vhost } from "./types";

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
