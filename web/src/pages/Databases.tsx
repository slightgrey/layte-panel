import { KeyRound, Plus, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";
import { useCreateDatabase, useDatabases, useDropDatabase, useResetDBPassword } from "@/api/databases";
import { useSites } from "@/api/sites";
import type { DatabaseRow } from "@/api/types";
import { OneTimeSecrets } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes } from "@/lib/format";
import { FormError } from "./AuthShell";

export function DatabasesPage() {
  const dbs = useDatabases();
  const [secrets, setSecrets] = useState<Record<string, string> | null>(null);
  const [showSystem, setShowSystem] = useState(false);
  const rows = (dbs.data?.databases ?? []).filter((d) => showSystem || !d.system);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Databases</h1>
          <p className="text-sm text-muted-foreground">
            {dbs.data ? `${dbs.data.engine === "mariadb" ? "MariaDB" : "MySQL"} on this server. ` : ""}
            Databases created by Layte Panel can be managed here; others are read-only.
          </p>
        </div>
        <NewDatabase onCreated={setSecrets} />
      </div>

      {secrets && <OneTimeSecrets secrets={secrets} onDismiss={() => setSecrets(null)} />}

      <Card>
        <CardContent className="pt-5">
          {dbs.isPending ? (
            <Spinner />
          ) : dbs.error ? (
            <p className="text-sm text-destructive">{dbs.error.message}</p>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Database</TableHead>
                    <TableHead>Site</TableHead>
                    <TableHead className="text-right">Size</TableHead>
                    <TableHead className="text-right">Actions</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {rows.map((d) => (
                    <DBRow key={d.name} db={d} onSecrets={setSecrets} />
                  ))}
                </TableBody>
              </Table>
              <label className="mt-3 flex items-center gap-2 text-xs text-muted-foreground">
                <input type="checkbox" checked={showSystem} onChange={(e) => setShowSystem(e.target.checked)} /> Show system databases
              </label>
            </>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function DBRow({ db, onSecrets }: { db: DatabaseRow; onSecrets: (s: Record<string, string>) => void }) {
  const reset = useResetDBPassword();
  const [dropOpen, setDropOpen] = useState(false);
  return (
    <TableRow>
      <TableCell>
        <span className="font-mono text-sm">{db.name}</span>
        {db.system && <Badge className="ml-2">system</Badge>}
        {!db.managed && !db.system && <Badge className="ml-2">not Layte</Badge>}
        {db.linked && (
          <Badge className="ml-2" title="Existing database linked to an adopted site: exported and imported there, never dropped or reset">
            linked
          </Badge>
        )}
        {reset.error && <div className="text-xs text-destructive">{reset.error.message}</div>}
      </TableCell>
      <TableCell>
        {db.site_id ? (
          <Link to={`/sites/${db.site_id}`} className="hover:underline">
            {db.site_domain}
          </Link>
        ) : (
          <span className="text-muted-foreground">–</span>
        )}
      </TableCell>
      <TableCell className="text-right text-xs tabular-nums">{formatBytes(db.size)}</TableCell>
      <TableCell>
        {db.managed && !db.linked && (
          <div className="flex justify-end gap-1">
            <Button
              variant="ghost"
              size="sm"
              disabled={reset.isPending || db.wordpress}
              title={db.wordpress ? "Used by WordPress: changing it here would break wp-config.php" : "Generate a new password"}
              onClick={() => reset.mutate(db.name, { onSuccess: (r) => onSecrets(r.secrets) })}
            >
              <KeyRound /> New password
            </Button>
            <Button
              variant="ghost"
              size="sm"
              className="text-destructive"
              disabled={Boolean(db.site_id)}
              title={db.site_id ? "Delete the site to remove its database" : "Drop database"}
              onClick={() => setDropOpen(true)}
            >
              <Trash2 />
            </Button>
            <DropDialog name={db.name} open={dropOpen} onClose={() => setDropOpen(false)} />
          </div>
        )}
      </TableCell>
    </TableRow>
  );
}

function DropDialog({ name, open, onClose }: { name: string; open: boolean; onClose: () => void }) {
  const drop = useDropDatabase();
  const [confirm, setConfirm] = useState("");
  return (
    <Dialog open={open} onClose={onClose} title={`Drop ${name}`}>
      <form
        className="flex flex-col gap-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          drop.mutate({ name, confirm }, { onSuccess: onClose });
        }}
      >
        <p className="text-sm text-muted-foreground">Deletes the database, all its tables and its user. This can't be undone.</p>
        <Field label={`Type ${name} to confirm`} htmlFor={`drop-${name}`}>
          <Input id={`drop-${name}`} autoComplete="off" value={confirm} onChange={(e) => setConfirm(e.target.value)} />
        </Field>
        <FormError message={drop.error?.message} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button type="submit" className="bg-destructive hover:bg-destructive/90" disabled={confirm !== name || drop.isPending}>
            Drop database
          </Button>
        </div>
      </form>
    </Dialog>
  );
}

function NewDatabase({ onCreated }: { onCreated: (s: Record<string, string>) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [siteId, setSiteId] = useState("");
  const create = useCreateDatabase();
  const sites = useSites();
  const managedSites = (sites.data?.sites ?? []).filter((s) => s.managed);
  const close = () => {
    setOpen(false);
    setName("");
    setSiteId("");
    create.reset();
  };
  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <Plus /> New database
      </Button>
      <Dialog open={open} onClose={close} title="New database">
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            create.mutate(
              { name: name.trim(), site_id: siteId ? Number(siteId) : undefined },
              {
                onSuccess: (r) => {
                  onCreated(r.secrets);
                  close();
                },
              },
            );
          }}
        >
          <Field label="Name" htmlFor="db-name" hint="Lowercase letters, digits and _. A user with the same name is created.">
            <Input id="db-name" required pattern="[a-z][a-z0-9_]{0,31}" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Site (optional)" htmlFor="db-site">
            <Select id="db-site" value={siteId} onChange={(e) => setSiteId(e.target.value)}>
              <option value="">No site</option>
              {managedSites.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.domain}
                </option>
              ))}
            </Select>
          </Field>
          <FormError message={create.error?.message} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              Create
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
