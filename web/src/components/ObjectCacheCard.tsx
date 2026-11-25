import { useQueryClient } from "@tanstack/react-query";
import { Database, Eraser, Power } from "lucide-react";
import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { objectCacheKey, useFlushObjectCache, useObjectCache, useSetObjectCache } from "@/api/objectcache";
import type { Site } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { formatBytes } from "@/lib/format";
import { FormError } from "@/pages/AuthShell";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";

/** Redis object cache for a WordPress site Layte runs (created or adopted). */
export function ObjectCacheCard({ site }: { site: Site }) {
  const oc = useObjectCache(site.id);
  const set = useSetObjectCache(site.id);
  const flush = useFlushObjectCache(site.id);
  const qc = useQueryClient();
  const [params] = useSearchParams();
  const s = oc.data;

  // A finished job (the site's status flips back to active) changes the state.
  useEffect(() => {
    if (site.status === "active") qc.invalidateQueries({ queryKey: objectCacheKey(site.id) });
  }, [site.status, params, qc, site.id]);

  const enabled = !!s?.enabled;
  const job = useJobFeedback(`object-cache-${site.id}`, enabled ? "Turning off the object cache" : "Turning on the object cache");
  const busy = set.isPending || site.status !== "active";
  const usedPct = s?.limit_bytes ? Math.round(((s.used_bytes ?? 0) / s.limit_bytes) * 100) : 0;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Database className="size-4" /> Object cache
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>}
          {enabled && (
            <Button size="sm" variant="outline" disabled={flush.isPending} onClick={() => flush.mutate()}>
              <Eraser /> {flush.isSuccess && !flush.isPending ? "Flushed" : "Flush"}
            </Button>
          )}
          <Button
            size="sm"
            variant={enabled ? "outline" : "default"}
            disabled={busy || !s || job.working}
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
          nothing is saved to disk. Uses the Redis Object Cache plugin.
        </p>
        {enabled && s && (
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
        <FormError message={set.error?.message ?? flush.error?.message ?? oc.error?.message} />
      </CardContent>
    </Card>
  );
}
