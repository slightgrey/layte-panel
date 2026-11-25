import { Eraser, Gauge, Power } from "lucide-react";
import { useState } from "react";
import { usePurgeCache, useSetPageCache } from "@/api/sites";
import type { Site } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select } from "@/components/ui/field";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";

const defaultTTL = 60; // keep in sync with internal/pagecache
const ttlOptions = [10, 30, 60, 180, 720, 1440];

function ttlLabel(m: number) {
  if (m < 60) return `${m} minutes`;
  const h = m / 60;
  return h === 1 ? "1 hour" : Number.isInteger(h) ? `${h} hours` : `${m} minutes`;
}

/** Page cache (nginx FastCGI cache) for WordPress sites Layte created. */
export function PageCacheCard({ site }: { site: Site }) {
  const set = useSetPageCache(site.id);
  const purge = usePurgeCache(site.id);
  const enabled = !!site.page_cache?.enabled;
  const job = useJobFeedback(`page-cache-${site.id}`, "Updating the page cache");
  const current = site.page_cache?.ttl_minutes || defaultTTL;
  const [ttl, setTTL] = useState(current);
  const options = ttlOptions.includes(current) ? ttlOptions : [...ttlOptions, current].sort((a, b) => a - b);
  const busy = set.isPending || site.status !== "active";

  const apply = (on: boolean, minutes = ttl) =>
    set.mutate(on ? { enabled: true, ttl_minutes: minutes } : { enabled: false }, {
      onSuccess: job.start,
    });

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Gauge className="size-4" /> Page cache
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>}
          {enabled && (
            <Button size="sm" variant="outline" disabled={purge.isPending} onClick={() => purge.mutate()}>
              <Eraser /> {purge.isSuccess && !purge.isPending ? "Purged" : "Purge cache"}
            </Button>
          )}
          <Button size="sm" variant={enabled ? "outline" : "default"} disabled={busy || job.working} onClick={() => apply(!enabled)}>
            <JobButtonLabel state={job.state}>
              <Power /> {enabled ? "Turn off" : "Turn on"}
            </JobButtonLabel>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          nginx keeps finished pages and serves them without running PHP, so pages load much faster and the server does far
          less work. Logged-in users, comment authors, carts and checkout, wp-admin, the REST API, form posts and URLs with a
          query string always get fresh pages.
        </p>
        <p className="text-sm text-muted-foreground">
          When you publish or change content, the cache is cleared automatically within about 15 seconds (a small plugin in
          wp-content/mu-plugins tells the panel). Check it with the <span className="font-mono text-xs">X-Layte-Cache</span>{" "}
          response header: HIT, MISS or BYPASS.
        </p>
        <div className="flex flex-wrap items-center gap-2">
          <label htmlFor="cache-ttl" className="text-sm">
            Keep pages for
          </label>
          <Select id="cache-ttl" className="w-40" value={ttl} onChange={(e) => setTTL(Number(e.target.value))}>
            {options.map((m) => (
              <option key={m} value={m}>
                {ttlLabel(m)}
              </option>
            ))}
          </Select>
          {enabled && ttl !== current && (
            <Button size="sm" variant="outline" disabled={busy} onClick={() => apply(true, ttl)}>
              Save
            </Button>
          )}
        </div>
        {(set.error || purge.error) && <p className="text-sm text-destructive">{(set.error ?? purge.error)!.message}</p>}
      </CardContent>
    </Card>
  );
}
