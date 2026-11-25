import { useMutation, useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { JobStarted } from "./types";

/** A site's Redis object cache: on/off, plus live numbers while it's on. */
export interface ObjectCacheState {
  enabled: boolean;
  installed_plugin?: boolean;
  running?: boolean;
  /** Disabled in WordPress (Settings → Object Cache). */
  paused?: boolean;
  /** Whose wp-content/object-cache.php: Layte's, the old plugin's, missing, or someone else's. */
  drop_in?: "layte" | "redis-cache" | "missing" | "other";
  keys?: number;
  used_bytes?: number;
  limit_bytes?: number;
}

export const objectCacheKey = (siteId: number) => ["sites", siteId, "object-cache"] as const;

/** GET /sites/{id}/object-cache */
export function useObjectCache(siteId: number) {
  return useQuery({
    queryKey: objectCacheKey(siteId),
    queryFn: () => api<ObjectCacheState>("GET", `/sites/${siteId}/object-cache`),
    refetchInterval: 30_000,
  });
}

/** PUT /sites/{id}/object-cache: starts a job that turns it on (installing Redis if needed) or off. */
export function useSetObjectCache(siteId: number) {
  return useMutation({
    mutationFn: (enabled: boolean) => api<JobStarted>("PUT", `/sites/${siteId}/object-cache`, { enabled }),
  });
}

/** POST /sites/{id}/object-cache/flush */
export function useFlushObjectCache(siteId: number) {
  return useMutation({ mutationFn: () => api<void>("POST", `/sites/${siteId}/object-cache/flush`) });
}

/** POST /sites/{id}/object-cache/resume: on again after it was disabled in WordPress. */
export function useResumeObjectCache(siteId: number) {
  return useMutation({ mutationFn: () => api<void>("POST", `/sites/${siteId}/object-cache/resume`) });
}
