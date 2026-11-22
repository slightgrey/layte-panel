import { ArrowLeft, ChevronRight, CornerLeftUp, Download, File, FileCode, Folder, FolderPlus, Home, Lock, Pencil, Save, Trash2, Upload } from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { useParams, useSearchParams } from "react-router";
import { downloadURL, useFileActions, useFileContent, useFiles } from "@/api/files";
import { useSite } from "@/api/sites";
import type { FileEntry } from "@/api/types";
import { SiteHeader } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes } from "@/lib/format";
import { cn } from "@/lib/utils";
import { FormError } from "./AuthShell";

const join = (dir: string, name: string) => (dir ? `${dir}/${name}` : name);
const editable = /\.(php|html?|css|js|mjs|json|txt|md|xml|svg|ini|conf|htaccess|env|ya?ml|log|sql|csv|ts|tsx|twig|blade\.php)$/i;

export function SiteFilesPage() {
  const siteId = Number(useParams().id);
  const site = useSite(siteId);
  const [params, setParams] = useSearchParams();
  const dir = params.get("path") ?? (site.data?.managed ? "public" : "");
  const files = useFiles(siteId, dir);
  const actions = useFileActions(siteId);
  const readOnly = files.data?.read_only ?? true;
  const [editing, setEditing] = useState<string | null>(null);
  const [renaming, setRenaming] = useState<FileEntry | null>(null);
  const [newFolder, setNewFolder] = useState(false);
  const uploadInput = useRef<HTMLInputElement>(null);

  const go = (p: string) => setParams(p ? { path: p } : { path: "" });
  const crumbs = dir.split("/").filter(Boolean);
  const atRoot = crumbs.length === 0;
  const up = () => go(crumbs.slice(0, -1).join("/"));
  const anyError = actions.upload.error ?? actions.remove.error ?? actions.mkdir.error;

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      {site.data && <SiteHeader site={site.data} />}
      <div className="flex items-center gap-2">
        <h2 className="text-lg font-semibold tracking-tight">Files</h2>
        {readOnly && files.data && <Lock className="size-4 text-muted-foreground" aria-label="read-only" />}
        <span className="truncate font-mono text-xs text-muted-foreground">{files.data?.root}</span>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <Button variant="outline" size="sm" onClick={up} disabled={atRoot} aria-label="Back to the parent folder" title="Back to the parent folder">
          <ArrowLeft /> Back
        </Button>
        <nav aria-label="Folder" className="flex min-w-0 flex-wrap items-center gap-0.5 rounded-[12px] bg-card px-1.5 py-1 text-sm shadow-raised-sm">
          <Crumb current={atRoot} onClick={() => go("")}>
            <Home className="size-3.5" />
            {site.data?.domain ?? "root"}
          </Crumb>
          {crumbs.map((c, i) => (
            <span key={i} className="flex items-center gap-0.5">
              <ChevronRight className="size-3.5 text-muted-foreground" aria-hidden />
              <Crumb current={i === crumbs.length - 1} onClick={() => go(crumbs.slice(0, i + 1).join("/"))}>
                {c}
              </Crumb>
            </span>
          ))}
        </nav>
        {!readOnly && (
          <div className="ml-auto flex gap-2">
            <Button variant="outline" size="sm" onClick={() => setNewFolder(true)}>
              <FolderPlus /> New folder
            </Button>
            <Button size="sm" onClick={() => uploadInput.current?.click()} disabled={actions.upload.isPending}>
              <Upload /> {actions.upload.isPending ? "Uploading…" : "Upload"}
            </Button>
            <input
              ref={uploadInput}
              type="file"
              multiple
              hidden
              onChange={(e) => {
                const list = [...(e.target.files ?? [])];
                if (list.length) actions.upload.mutate({ dir, files: list });
                e.target.value = "";
              }}
            />
          </div>
        )}
      </div>
      {readOnly && files.data && (
        <p className="text-sm text-muted-foreground">This site was imported, so its files are read-only here. You can browse and download them.</p>
      )}
      <FormError message={anyError?.message} />

      <Card>
        <CardContent className="pt-4">
          {files.isPending ? (
            <Spinner />
          ) : files.error ? (
            <p className="text-sm text-destructive">{files.error.message}</p>
          ) : files.data.entries.length === 0 ? (
            <>
              {!atRoot && <ParentRow onClick={up} />}
              <p className="py-8 text-center text-sm text-muted-foreground">This folder is empty.</p>
            </>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead className="text-right">Size</TableHead>
                  <TableHead>Modified</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!atRoot && (
                  <TableRow>
                    <TableCell colSpan={4} className="py-1.5">
                      <ParentRow onClick={up} />
                    </TableCell>
                  </TableRow>
                )}
                {files.data.entries.map((e) => {
                  const p = join(dir, e.name);
                  return (
                    <TableRow key={e.name}>
                      <TableCell>
                        {e.type === "dir" ? (
                          <button className="flex items-center gap-2 font-medium hover:underline" onClick={() => go(p)}>
                            <Folder className="size-4 text-primary" /> {e.name}
                          </button>
                        ) : (
                          <span className="flex items-center gap-2">
                            {editable.test(e.name) ? <FileCode className="size-4 text-muted-foreground" /> : <File className="size-4 text-muted-foreground" />}
                            {e.name}
                            {e.type === "symlink" && <span className="text-xs text-muted-foreground">(link)</span>}
                          </span>
                        )}
                      </TableCell>
                      <TableCell className="text-right text-xs tabular-nums">{e.type === "dir" ? "–" : formatBytes(e.size)}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{new Date(e.mtime).toLocaleString()}</TableCell>
                      <TableCell>
                        <div className="flex justify-end gap-0.5">
                          {e.type === "file" && editable.test(e.name) && (
                            <Button variant="ghost" size="sm" onClick={() => setEditing(p)} aria-label={`Edit ${e.name}`}>
                              <Pencil />
                            </Button>
                          )}
                          {e.type === "file" && (
                            <a href={downloadURL(siteId, p)} aria-label={`Download ${e.name}`} className="inline-flex size-8 items-center justify-center rounded-md hover:bg-muted [&_svg]:size-4">
                              <Download />
                            </a>
                          )}
                          {!readOnly && (
                            <>
                              <Button variant="ghost" size="sm" onClick={() => setRenaming(e)}>
                                Rename
                              </Button>
                              <Button
                                variant="ghost"
                                size="sm"
                                className="text-destructive"
                                aria-label={`Delete ${e.name}`}
                                onClick={() => {
                                  if (confirm(`Delete ${e.name}${e.type === "dir" ? " and everything in it" : ""}?`)) actions.remove.mutate({ path: p });
                                }}
                              >
                                <Trash2 />
                              </Button>
                            </>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      <Editor siteId={siteId} path={editing} readOnly={readOnly} onClose={() => setEditing(null)} />
      <NameDialog
        open={newFolder}
        title="New folder"
        initial=""
        onClose={() => setNewFolder(false)}
        onSubmit={(name) => actions.mkdir.mutateAsync({ path: join(dir, name) })}
      />
      <NameDialog
        open={Boolean(renaming)}
        title={`Rename ${renaming?.name ?? ""}`}
        initial={renaming?.name ?? ""}
        onClose={() => setRenaming(null)}
        onSubmit={(name) => actions.rename.mutateAsync({ path: join(dir, renaming!.name), to: join(dir, name) })}
      />
    </div>
  );
}

/** One breadcrumb step: a chip; the current folder is bold and blue. */
function Crumb({ current, onClick, children }: { current: boolean; onClick: () => void; children: ReactNode }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-current={current ? "location" : undefined}
      className={cn(
        "flex items-center gap-1.5 rounded-[9px] px-2 py-1 transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
        current ? "font-semibold text-primary" : "text-foreground hover:bg-action hover:text-action-foreground",
      )}
    >
      {children}
    </button>
  );
}

/** ".." at the top of the list: back to the parent folder. */
function ParentRow({ onClick }: { onClick: () => void }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className="flex w-full items-center gap-2 rounded-[9px] px-1 py-1 text-left text-sm text-muted-foreground hover:text-primary"
    >
      <CornerLeftUp className="size-4" />
      <span className="font-mono">..</span>
      <span className="text-xs">Parent folder</span>
    </button>
  );
}

function NameDialog(props: { open: boolean; title: string; initial: string; onClose: () => void; onSubmit: (name: string) => Promise<unknown> }) {
  const [name, setName] = useState(props.initial);
  const [error, setError] = useState<string>();
  useEffect(() => {
    setName(props.initial);
    setError(undefined);
  }, [props.initial, props.open]);
  return (
    <Dialog open={props.open} onClose={props.onClose} title={props.title}>
      <form
        className="flex flex-col gap-4"
        onSubmit={async (e: FormEvent) => {
          e.preventDefault();
          try {
            await props.onSubmit(name.trim());
            props.onClose();
          } catch (err) {
            setError((err as Error).message);
          }
        }}
      >
        <Field label="Name" htmlFor="fm-name">
          <Input id="fm-name" required pattern="[^/\\]+" value={name} onChange={(e) => setName(e.target.value)} />
        </Field>
        <FormError message={error} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={props.onClose}>
            Cancel
          </Button>
          <Button type="submit">Save</Button>
        </div>
      </form>
    </Dialog>
  );
}

function Editor({ siteId, path, readOnly, onClose }: { siteId: number; path: string | null; readOnly: boolean; onClose: () => void }) {
  const file = useFileContent(siteId, path);
  const { save } = useFileActions(siteId);
  const [text, setText] = useState("");
  useEffect(() => {
    if (file.data) setText(file.data.content);
  }, [file.data]);
  const dirty = file.data && text !== file.data.content;

  return (
    <Dialog wide open={Boolean(path)} onClose={() => (!dirty || confirm("Discard unsaved changes?")) && onClose()} title={path ?? ""}>
      {file.isPending ? (
        <Spinner />
      ) : file.error ? (
        <p className="text-sm text-destructive">{file.error.message}</p>
      ) : (
        <div className="flex flex-col gap-3">
          <textarea
            aria-label="File content"
            spellCheck={false}
            readOnly={readOnly}
            className="h-[60vh] w-full resize-none rounded-[12px] bg-background p-3 font-mono text-xs leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => {
              if ((e.ctrlKey || e.metaKey) && e.key === "s" && !readOnly) {
                e.preventDefault();
                save.mutate({ path: path!, content: text });
              }
            }}
          />
          <FormError message={save.error?.message} />
          {!readOnly && (
            <div className="flex items-center justify-end gap-2">
              {save.isSuccess && !dirty && <span className="text-xs text-muted-foreground">Saved</span>}
              <Button disabled={!dirty || save.isPending} onClick={() => save.mutate({ path: path!, content: text })}>
                <Save /> Save
              </Button>
            </div>
          )}
        </div>
      )}
    </Dialog>
  );
}
