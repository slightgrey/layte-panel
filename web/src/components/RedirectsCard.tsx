import { ArrowRight, Plus, Save, Trash2, Undo2 } from "lucide-react";
import { useState } from "react";
import { useSearchParams } from "react-router";
import { useSetRedirects } from "@/api/sites";
import type { Redirect, Site } from "@/api/types";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/pages/AuthShell";

const same = (a: Redirect[], b: Redirect[]) => JSON.stringify(a) === JSON.stringify(b);

/** Redirects card for sites whose nginx config Layte manages. Remount (key) when the saved list changes. */
export function RedirectsCard({ site }: { site: Site }) {
  const saved = site.redirects ?? [];
  const [rows, setRows] = useState<Redirect[]>(saved);
  const set = useSetRedirects(site.id);
  const [, setParams] = useSearchParams();
  const busy = set.isPending || site.status !== "active";
  const dirty = !same(rows, saved);

  const update = (i: number, patch: Partial<Redirect>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ArrowRight className="size-4" /> Redirects
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {dirty && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => setRows(saved)}>
              <Undo2 /> Discard
            </Button>
          )}
          <Button
            size="sm"
            disabled={busy || !dirty}
            onClick={() =>
              set.mutate(
                rows.filter((r) => r.from.trim() || r.to.trim()),
                { onSuccess: (r) => setParams({ job: r.job_id }, { replace: true }) },
              )
            }
          >
            <Save /> Save
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Send an old path to a new page or another site. A path matches with or without a trailing slash; the query string
          is not passed on. Use 301 for permanent moves (browsers and search engines remember it), 302 while testing.
        </p>
        {rows.length > 0 && (
          <div className="flex flex-col gap-2">
            {rows.map((r, i) => (
              <div key={i} className="flex flex-wrap items-center gap-2">
                <Input
                  aria-label="From path"
                  className="min-w-40 flex-1 font-mono"
                  placeholder="/old-page"
                  value={r.from}
                  onChange={(e) => update(i, { from: e.target.value })}
                />
                <ArrowRight className="size-4 shrink-0 text-muted-foreground" />
                <Input
                  aria-label="To path or URL"
                  className="min-w-48 flex-[2] font-mono"
                  placeholder="/new-page or https://…"
                  value={r.to}
                  onChange={(e) => update(i, { to: e.target.value })}
                />
                <Select aria-label="Status" className="w-24" value={r.code} onChange={(e) => update(i, { code: Number(e.target.value) as 301 | 302 })}>
                  <option value={301}>301</option>
                  <option value={302}>302</option>
                </Select>
                <Button size="icon" variant="ghost" aria-label="Remove redirect" onClick={() => setRows(rows.filter((_, j) => j !== i))}>
                  <Trash2 />
                </Button>
              </div>
            ))}
          </div>
        )}
        <div>
          <Button size="sm" variant="outline" onClick={() => setRows([...rows, { from: "", to: "", code: 301 }])}>
            <Plus /> Add redirect
          </Button>
        </div>
        <FormError message={set.error?.message} />
      </CardContent>
    </Card>
  );
}
