import { ArrowRight, Download, Signpost, FileDown, Plus, Save, Search, Trash2, Undo2, Upload } from "lucide-react";
import { type ChangeEvent, useRef, useState } from "react";
import { layteConfigs, useSetRedirects } from "@/api/sites";
import type { Redirect, Site } from "@/api/types";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Notice } from "@/components/ui/notice";
import { downloadText, parseCSV, toCSV } from "@/lib/csv";
import { FormError } from "./AuthShell";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";

/** /sites/:id/redirects */
export function SiteRedirectsPage() {
  return (
    <SiteShell title="Redirects">
      {(site) =>
        layteConfigs(site) ? (
          <RedirectsEditor key={JSON.stringify(site.redirects ?? [])} site={site} />
        ) : (
          <Card>
            <CardContent className="pt-6">
              <EmptyState icon={ArrowRight} title="Redirects aren't available for this site">
                {site.adopted
                  ? "Its nginx config is still yours. Let Layte Panel manage it (on the Overview) to add redirects here."
                  : "This site is read-only here. Adopt it, then let Layte Panel manage its nginx config."}
              </EmptyState>
            </CardContent>
          </Card>
        )
      }
    </SiteShell>
  );
}

const header = ["from", "to", "code"];
const sample = toCSV([
  header,
  ["/old-page", "/new-page/", 301],
  ["/blog/2019/hello-world", "/hello-world/", 301],
  ["/summer-sale", "https://shop.example.com/sale", 302],
]);

const same = (a: Redirect[], b: Redirect[]) => JSON.stringify(a) === JSON.stringify(b);
/** "/old/" and "/old" are the same redirect (the server stores it without the slash). */
const key = (from: string) => (from.trim().length > 1 ? from.trim().replace(/\/+$/, "") : from.trim());

/** Reads CSV rows (header optional) into redirects; returns problems by line. */
function fromCSV(text: string): { rows: Redirect[]; problems: string[] } {
  const rows: Redirect[] = [];
  const problems: string[] = [];
  parseCSV(text).forEach((cells, i) => {
    const line = i + 1;
    const [from = "", to = "", code = ""] = cells.map((c) => c.trim());
    if (i === 0 && from.toLowerCase() === "from") return; // header
    if (!from || !to) {
      problems.push(`line ${line}: needs a from path and a to path or URL`);
      return;
    }
    const c = code === "" ? 301 : Number(code);
    if (c !== 301 && c !== 302) {
      problems.push(`line ${line}: code must be 301 or 302 (or empty for 301)`);
      return;
    }
    rows.push({ from, to, code: c });
  });
  return { rows, problems };
}

function RedirectsEditor({ site }: { site: Site }) {
  const saved = site.redirects ?? [];
  const [rows, setRows] = useState<Redirect[]>(saved);
  const [filter, setFilter] = useState("");
  const [imported, setImported] = useState<{ added: number; updated: number; problems: string[] } | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);
  const set = useSetRedirects(site.id);
  const job = useJobFeedback(`redirects-${site.id}`, "Saving redirects");
  const busy = set.isPending || site.status !== "active";
  const dirty = !same(rows, saved);
  const q = filter.trim().toLowerCase();
  const visible = rows.map((r, i) => ({ r, i })).filter(({ r }) => !q || r.from.toLowerCase().includes(q) || r.to.toLowerCase().includes(q));

  const update = (i: number, patch: Partial<Redirect>) => setRows(rows.map((r, j) => (j === i ? { ...r, ...patch } : r)));

  const onImport = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = ""; // the same file can be picked again
    if (!file) return;
    const { rows: incoming, problems } = fromCSV(await file.text());
    const next = [...rows];
    let added = 0;
    let updated = 0;
    for (const r of incoming) {
      const at = next.findIndex((x) => key(x.from) === key(r.from));
      if (at >= 0) {
        next[at] = r;
        updated++;
      } else {
        next.push(r);
        added++;
      }
    }
    setRows(next);
    setImported({ added, updated, problems });
  };

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Signpost className="size-4" /> Redirects
          <span className="text-sm font-normal text-muted-foreground">({saved.length} saved)</span>
        </CardTitle>
        <div className="flex flex-wrap items-center gap-2">
          {dirty && (
            <Button size="sm" variant="ghost" disabled={busy} onClick={() => (setRows(saved), setImported(null))}>
              <Undo2 /> Discard
            </Button>
          )}
          <Button
            size="sm"
            disabled={busy || !dirty}
            onClick={() =>
              set.mutate(
                rows.filter((r) => r.from.trim() || r.to.trim()),
                { onSuccess: job.start },
              )
            }
          >
            <JobButtonLabel state={job.state}>
              <Save /> Save
            </JobButtonLabel>
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          Send an old path to a new page or another site. A path matches with or without a trailing slash; the query string
          is not passed on. Use 301 for permanent moves (browsers and search engines remember it), 302 while testing. Saving
          rewrites the site's redirects file, which its nginx config includes, and tests it with <code>nginx -t</code>.
        </p>

        <div className="flex flex-wrap items-center gap-2">
          <Button size="sm" variant="outline" onClick={() => setRows([...rows, { from: "", to: "", code: 301 }])}>
            <Plus /> Add redirect
          </Button>
          <Button size="sm" variant="outline" onClick={() => fileRef.current?.click()}>
            <Upload /> Import CSV
          </Button>
          <input ref={fileRef} type="file" accept=".csv,text/csv" className="hidden" onChange={onImport} />
          <Button
            size="sm"
            variant="outline"
            disabled={saved.length === 0}
            onClick={() => downloadText(`${site.domain}-redirects.csv`, toCSV([header, ...saved.map((r) => [r.from, r.to, r.code])]))}
          >
            <Download /> Export CSV
          </Button>
          <Button size="sm" variant="ghost" onClick={() => downloadText("redirects-sample.csv", sample)}>
            <FileDown /> Sample CSV
          </Button>
          {rows.length > 10 && (
            <div className="relative ml-auto min-w-48">
              <Search className="pointer-events-none absolute top-1/2 left-3 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input aria-label="Filter redirects" className="pl-9" placeholder="Filter" value={filter} onChange={(e) => setFilter(e.target.value)} />
            </div>
          )}
        </div>

        {imported && (
          <Notice tone={imported.problems.length ? "warning" : undefined}>
            <div>
              Imported {imported.added} new and {imported.updated} changed redirect(s). Review them, then click <strong>Save</strong>.
              {imported.problems.length > 0 && (
                <ul className="mt-1 list-disc pl-5 text-muted-foreground">
                  {imported.problems.slice(0, 10).map((p) => (
                    <li key={p}>Skipped {p}</li>
                  ))}
                  {imported.problems.length > 10 && <li>…and {imported.problems.length - 10} more</li>}
                </ul>
              )}
            </div>
          </Notice>
        )}

        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">No redirects yet.</p>
        ) : (
          <div className="flex flex-col gap-2">
            {visible.map(({ r, i }) => (
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
            {q && visible.length === 0 && <p className="text-sm text-muted-foreground">No redirect matches “{filter}”.</p>}
          </div>
        )}
        <FormError message={set.error?.message} />
      </CardContent>
    </Card>
  );
}
