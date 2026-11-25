import { useQueryClient } from "@tanstack/react-query";
import { Database, Eraser, Play, Power, Wrench } from "lucide-react";
import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { objectCacheKey, useFlushObjectCache, useObjectCache, useResumeObjectCache, useSetObjectCache } from "@/api/objectcache";
import type { Site } from "@/api/types";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { formatBytes } from "@/lib/format";
import { FormError } from "@/pages/AuthShell";

/** Redis object cache for a WordPress site Layte runs (created or adopted). */
export function ObjectCacheCard({ site }: { site: Site }) {
  const oc = useObjectCache(site.id);
  const set = useSetObjectCache(site.id);
  const flush = useFlushObjectCache(site.id);
  const resume = useResumeObjectCache(site.id);
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const s = oc.data;

  // A finished job (the site's status flips back to active) changes the state.
  useEffect(() => {
    if (site.status === "active") qc.invalidateQueries({ queryKey: objectCacheKey(site.id) });
  }, [site.status, params, qc, site.id]);

  const enabled = !!s?.enabled;
  const paused = enabled && !!s?.paused;
  // On in Layte, but the drop-in is the old plugin's (earlier Layte versions), missing, or someone else's.
  const needsUpdate = enabled && !!s && !paused && s.drop_in !== undefined && s.drop_in !== "layte";
  const job = useJobFeedback(`object-cache-${site.id}`, enabled ? "Turning off the object cache" : "Turning on the object cache");
  const fix = useJobFeedback(`object-cache-fix-${site.id}`, s?.drop_in === "redis-cache" ? "Switching to Layte's object cache" : "Repairing the object cache");
  const busy = set.isPending || site.status !== "active" || job.working || fix.working;
  const usedPct = s?.limit_bytes ? Math.round(((s.used_bytes ?? 0) / s.limit_bytes) * 100) : 0;
  const refresh = () => qc.invalidateQueries({ queryKey: objectCacheKey(site.id) });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Database className="size-4" /> Object cache
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {paused ? <Badge variant="warning">paused</Badge> : enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>}
          {paused && (
            <Button size="sm" disabled={busy || resume.isPending} onClick={() => resume.mutate(undefined, { onSuccess: refresh })}>
              <Play /> Resume
            </Button>
          )}
          {enabled && !paused && (
            <Button size="sm" variant="outline" disabled={flush.isPending} onClick={() => flush.mutate()}>
              <Eraser /> {flush.isSuccess && !flush.isPending ? "Flushed" : "Flush"}
            </Button>
          )}
          <Button
            size="sm"
            variant={enabled ? "outline" : "default"}
            disabled={busy || !s}
            onClick={() => set.mutate(!enabled, { onSuccess: job.start })}
          >
            <JobButtonLabel state={job.state}>
              <Power /> {enabled ? "Turn off" : "Turn on"}
            </JobButtonLabel>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Keeps WordPress's database results in Redis, so wp-admin, logged-in pages, search and the REST API (everything the
          page cache doesn't cover) load faster. This site gets its own small Redis that only it can reach, with a memory cap;
          nothing is saved to disk. In WordPress, Settings → Object Cache can flush it or disable it for a while.
        </p>
        {paused && (
          <Notice tone="warning">
            Disabled in WordPress (Settings → Object Cache). The site works as usual, without the speed-up; Redis is idle.
            Resume empties Redis first, so nothing stale is served.
          </Notice>
        )}
        {needsUpdate && (
          <Notice
            tone="warning"
            action={
              <Button size="sm" disabled={busy} onClick={() => set.mutate(true, { onSuccess: fix.start })}>
                <JobButtonLabel state={fix.state}>
                  <Wrench /> {s?.drop_in === "redis-cache" ? "Switch" : "Repair"}
                </JobButtonLabel>
              </Button>
            }
          >
            {s?.drop_in === "redis-cache"
              ? "This site still uses the Redis Object Cache plugin. Switch to Layte's own object cache (the plugin is removed)."
              : "The object cache files are missing from wp-content. Repair puts them back."}
          </Notice>
        )}
        {enabled && s && !paused && (
          <p className="text-sm">
            {s.running ? (
              <>
                {s.keys?.toLocaleString()} keys · {formatBytes(s.used_bytes ?? 0)} of {formatBytes(s.limit_bytes ?? 0)} ({usedPct}%)
              </>
            ) : (
              <span className="text-warning">Redis isn't running for this site. Turn the object cache off and on again.</span>
            )}
          </p>
        )}
        <FormError message={set.error?.message ?? flush.error?.message ?? resume.error?.message ?? oc.error?.message} />
      </CardContent>
    </Card>
  );
}
