import { ArrowLeft, ChevronRight, FileSearch, Folder, HardDrive, RefreshCw } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useSearchParams } from "react-router";
import { useDashboard } from "@/api/dashboard";
import { useDiskBreakdown } from "@/api/system";
import { ShareBar, SystemShell } from "@/components/SystemShell";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { UsageBar } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, percent } from "@/lib/format";
import { cn } from "@/lib/utils";

const base = (p: string) => p.split("/").filter(Boolean).pop() ?? "/";
const parent = (p: string) => (p === "/" ? "/" : "/" + p.split("/").filter(Boolean).slice(0, -1).join("/"));

export function SystemDiskPage() {
  const dash = useDashboard();
  const [params, setParams] = useSearchParams();
  const path = params.get("path") || "/";
  const go = (p: string) => setParams(p === "/" ? {} : { path: p });
  const disks = dash.data?.system.data?.disks ?? [];

  return (
    <SystemShell title="Disk" description="What is taking up space. Click a folder to look inside it.">
      {disks.length > 0 && (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {disks.map((d) => (
            <Card key={d.path}>
              <CardContent className="flex flex-col gap-2 pt-5">
                <div className="flex items-center justify-between text-sm">
                  <span className="flex items-center gap-2 font-mono">
                    <HardDrive className="size-4 text-muted-foreground" /> {d.path}
                  </span>
                  <span className="tabular-nums">{percent(d.used, d.total)}%</span>
                </div>
                <UsageBar value={percent(d.used, d.total)} label={`Disk ${d.path} used`} />
                <p className="text-xs text-muted-foreground">
                  {formatBytes(d.used)} used · {formatBytes(d.free)} free of {formatBytes(d.total)}
                </p>
              </CardContent>
            </Card>
          ))}
        </div>
      )}
      <Breakdown key={path} path={path} go={go} />
    </SystemShell>
  );
}

function Breakdown({ path, go }: { path: string; go: (p: string) => void }) {
  const dirs = useDiskBreakdown(path, "");
  const [wantFiles, setWantFiles] = useState(false);
  const files = useDiskBreakdown(path, "files", wantFiles);
  const crumbs = path.split("/").filter(Boolean);

  return (
    <>
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" disabled={path === "/"} onClick={() => go(parent(path))} aria-label="Back to the parent folder">
          <ArrowLeft /> Back
        </Button>
        <nav aria-label="Folder" className="flex min-w-0 flex-wrap items-center gap-0.5 rounded-[12px] bg-card px-1.5 py-1 font-mono text-sm shadow-raised-sm">
          <Crumb current={crumbs.length === 0} onClick={() => go("/")}>
            /
          </Crumb>
          {crumbs.map((c, i) => (
            <span key={i} className="flex items-center gap-0.5">
              {i > 0 && <ChevronRight className="size-3.5 text-muted-foreground" aria-hidden />}
              <Crumb current={i === crumbs.length - 1} onClick={() => go("/" + crumbs.slice(0, i + 1).join("/"))}>
                {c}
              </Crumb>
            </span>
          ))}
        </nav>
        <Button variant="ghost" size="sm" className="ml-auto" disabled={dirs.isFetching} onClick={() => dirs.refetch()}>
          <RefreshCw className={cn(dirs.isFetching && "animate-spin")} /> Rescan
        </Button>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Folders in {path}</CardTitle>
          {dirs.data && <span className="text-sm tabular-nums">{formatBytes(dirs.data.total)} in total</span>}
        </CardHeader>
        <CardContent>
          {dirs.isPending ? (
            <div className="flex items-center gap-3 py-6 text-sm text-muted-foreground">
              <Spinner /> Measuring {path}… this can take a minute on a full disk.
            </div>
          ) : dirs.error ? (
            <Notice tone="warning">{dirs.error.message}</Notice>
          ) : dirs.data.entries.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">No subfolders here.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Folder</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead className="w-2/5">Share of {base(path)}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {dirs.data.entries.map((e) => (
                  <TableRow key={e.path}>
                    <TableCell>
                      <button type="button" className="flex items-center gap-2 font-mono text-sm hover:text-primary" onClick={() => go(e.path)}>
                        <Folder className="size-4 text-primary" /> {base(e.path)}
                      </button>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{formatBytes(e.bytes)}</TableCell>
                    <TableCell>
                      <ShareBar value={percent(e.bytes, dirs.data.total)} />
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
          {dirs.data?.partial && (
            <p className="mt-3 text-xs text-muted-foreground">Some folders couldn't be read (or changed while measuring); sizes may be a little low.</p>
          )}
          <p className="mt-3 text-xs text-muted-foreground">Only this disk is counted; other mounted disks inside {path} are skipped.</p>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Largest files in {path}</CardTitle>
          {!wantFiles && (
            <Button variant="outline" size="sm" onClick={() => setWantFiles(true)}>
              <FileSearch /> Find large files
            </Button>
          )}
        </CardHeader>
        <CardContent>
          {!wantFiles ? (
            <p className="text-sm text-muted-foreground">Lists files of 20 MB or more anywhere inside {path}, biggest first.</p>
          ) : files.isPending ? (
            <div className="flex items-center gap-3 py-6 text-sm text-muted-foreground">
              <Spinner /> Searching {path} for large files…
            </div>
          ) : files.error ? (
            <Notice tone="warning">{files.error.message}</Notice>
          ) : files.data.entries.length === 0 ? (
            <p className="py-6 text-center text-sm text-muted-foreground">No files of 20 MB or more here.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>File</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {files.data.entries.map((f) => (
                  <TableRow key={f.path}>
                    <TableCell className="font-mono text-xs break-all">
                      <button type="button" className="text-left text-muted-foreground hover:text-primary" title="Open its folder" onClick={() => go(parent(f.path))}>
                        {parent(f.path)}/
                      </button>
                      <span className="font-medium text-foreground">{base(f.path)}</span>
                    </TableCell>
                    <TableCell className="text-right tabular-nums">{formatBytes(f.bytes)}</TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </>
  );
}

function Crumb({ current, onClick, children }: { current: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={current ? "location" : undefined}
      className={cn(
        "rounded-[9px] px-2 py-1 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        current ? "font-semibold text-primary" : "text-foreground hover:bg-action hover:text-action-foreground",
      )}
    >
      {children}
    </button>
  );
}
