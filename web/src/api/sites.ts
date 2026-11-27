import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { AdoptPreview, Redirect, TakeoverPreview, CreateSiteRequest, DeleteSiteRequest, DiskUsage, JobStarted, PageCache, PHPSettings, Site, SitesResponse, ScanResponse, Vhost, WPDebugState } from "./types";

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

/** PUT /sites/{id}/php-shell (admins): block or allow PHP's shell functions. */
export function useSetPHPShell(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (block: boolean) => api<Site>("PUT", `/sites/${id}/php-shell`, { block }),
    onSuccess: (site) => qc.setQueryData(siteKey(id), site),
  });
}

/** PUT /sites/{id}/php-settings: starts a job that rewrites the pool (and vhost). */
export function useSetPHPSettings(id: number) {
  return useMutation({
    mutationFn: (settings: PHPSettings) => api<JobStarted>("PUT", `/sites/${id}/php-settings`, settings),
  });
}

/** PUT /sites/{id}/redirects: starts a job that replaces the site's redirects. */
export function useSetRedirects(id: number) {
  return useMutation({
    mutationFn: (redirects: Redirect[]) => api<JobStarted>("PUT", `/sites/${id}/redirects`, { redirects }),
  });
}

/** PUT /sites/{id}/page-cache: starts a job that turns the page cache on/off. */
export function useSetPageCache(id: number) {
  return useMutation({
    mutationFn: (cache: PageCache) => api<JobStarted>("PUT", `/sites/${id}/page-cache`, cache),
  });
}

/** POST /sites/{id}/page-cache/purge: delete the cached pages now. */
export function usePurgeCache(id: number) {
  return useMutation({
    mutationFn: () => api<void>("POST", `/sites/${id}/page-cache/purge`),
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
export function useSite(id: number, enabled = true) {
  return useQuery({
    queryKey: siteKey(id),
    queryFn: () => api<Site>("GET", `/sites/${id}`),
    enabled: enabled && id > 0,
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

/** Layte renders the site's nginx config and PHP pool: sites it created, and adopted sites it took over. */
export function layteConfigs(site: Site) {
  return site.managed || (site.adopted && site.nginx_managed);
}

/** GET /sites/{id}/takeover: what letting Layte manage the nginx config changes (nothing is changed). */
export function useTakeoverPreview(id: number) {
  return useQuery({
    queryKey: ["sites", id, "takeover"],
    queryFn: () => api<TakeoverPreview>("GET", `/sites/${id}/takeover`),
    retry: false,
    gcTime: 0,
  });
}

/** POST /sites/{id}/takeover: let Layte manage the adopted site's nginx config (job). */
export function useTakeover(id: number) {
  return useMutation({
    mutationFn: (v: { vhost_hash: string; email: string; delete_old_cert?: boolean }) => api<JobStarted>("POST", `/sites/${id}/takeover`, v),
  });
}

/** POST /sites/{id}/adopt: adopt the imported site (job). */
export function useAdoptSite(id: number) {
  return useMutation({
    mutationFn: (v: { username: string; pool_hash: string }) => api<JobStarted>("POST", `/sites/${id}/adopt`, v),
  });
}


export const wpDebugKey = (id: number) => ["sites", id, "wp-debug"] as const;

/** GET /sites/{id}/wp-debug: is WP_DEBUG on, and where do its messages go. */
export function useWPDebug(id: number, enabled = true) {
  return useQuery({
    queryKey: wpDebugKey(id),
    queryFn: () => api<WPDebugState>("GET", `/sites/${id}/wp-debug`),
    enabled: enabled && id > 0,
  });
}

/** PUT /sites/{id}/wp-debug: turn WP Debug on or off. */
export function useSetWPDebug(id: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (enabled: boolean) => api<WPDebugState>("PUT", `/sites/${id}/wp-debug`, { enabled }),
    onSuccess: (state) => {
      qc.setQueryData(wpDebugKey(id), state);
      qc.invalidateQueries({ queryKey: ["logs", "sources"] }); // the log file may be new
    },
  });
}
