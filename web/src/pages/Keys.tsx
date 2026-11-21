import { ExternalLink, Plus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useCreateKey, useKeys, useRevokeKey } from "@/api/keys";
import type { APIKey } from "@/api/types";
import { CredentialsPanel } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Checkbox, Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { FormError } from "./AuthShell";

function keyState(k: APIKey): { text: string; variant: "success" | "neutral" | "danger" } {
  if (k.revoked_at) return { text: "revoked", variant: "neutral" };
  if (k.expires_at && new Date(k.expires_at) < new Date()) return { text: "expired", variant: "danger" };
  return { text: "active", variant: "success" };
}

const date = (s?: string) => (s ? new Date(s).toLocaleDateString() : "–");

export function KeysPage() {
  const keys = useKeys();
  const revoke = useRevokeKey();
  const [created, setCreated] = useState<string | null>(null);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-xl font-semibold tracking-tight">API keys</h1>
          <p className="text-sm text-muted-foreground">
            Read-only keys for other apps (like a dashboard or monitoring). Keys can't change anything on the server.{" "}
            <a href="/api/v1/openapi.json" target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 text-primary hover:underline">
              API reference <ExternalLink className="size-3" />
            </a>
          </p>
        </div>
        {keys.data && <NewKey scopes={keys.data.scopes} onCreated={setCreated} />}
      </div>

      {created && (
        <div className="flex flex-col gap-2">
          <CredentialsPanel secrets={{ api_key: created }} />
          <pre className="overflow-x-auto rounded-md border bg-muted/50 p-3 font-mono text-xs">
            curl -H "Authorization: Bearer {created}" {window.location.origin}/api/v1/sites
          </pre>
          <Button variant="ghost" size="sm" className="self-end" onClick={() => setCreated(null)}>
            I've saved it
          </Button>
        </div>
      )}

      <Card>
        <CardContent className="pt-5">
          {keys.isPending ? (
            <Spinner />
          ) : keys.error ? (
            <p className="text-sm text-destructive">{keys.error.message}</p>
          ) : keys.data.keys.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">No API keys yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Scopes</TableHead>
                  <TableHead>Last used</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.data.keys.map((k) => {
                  const st = keyState(k);
                  return (
                    <TableRow key={k.id} className={k.revoked_at ? "opacity-60" : ""}>
                      <TableCell>
                        <div className="font-medium">{k.name}</div>
                        <div className="font-mono text-xs text-muted-foreground">{k.prefix}…</div>
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-wrap gap-1">
                          {k.scopes.map((s) => (
                            <Badge key={s}>{s}</Badge>
                          ))}
                        </div>
                      </TableCell>
                      <TableCell className="text-xs">{k.last_used_at ? new Date(k.last_used_at).toLocaleString() : "never"}</TableCell>
                      <TableCell className="text-xs">{k.expires_at ? date(k.expires_at) : "never"}</TableCell>
                      <TableCell>
                        <div className="flex items-center justify-end gap-2">
                          <Badge variant={st.variant}>{st.text}</Badge>
                          {!k.revoked_at && (
                            <Button
                              variant="ghost"
                              size="sm"
                              className="text-destructive"
                              disabled={revoke.isPending}
                              onClick={() => confirm(`Revoke "${k.name}"? Apps using it stop working immediately.`) && revoke.mutate(k.id)}
                            >
                              Revoke
                            </Button>
                          )}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          )}
          <FormError message={revoke.error?.message} />
        </CardContent>
      </Card>
    </div>
  );
}

function NewKey({ scopes, onCreated }: { scopes: { name: string; description: string }[]; onCreated: (token: string) => void }) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [chosen, setChosen] = useState<string[]>(["system:read", "sites:read", "ssl:read"]);
  const [days, setDays] = useState("365");
  const create = useCreateKey();
  const close = () => {
    setOpen(false);
    setName("");
    create.reset();
  };
  const toggle = (s: string, on: boolean) => setChosen((c) => (on ? [...c, s] : c.filter((x) => x !== s)));

  return (
    <>
      <Button onClick={() => setOpen(true)}>
        <Plus /> New API key
      </Button>
      <Dialog open={open} onClose={close} title="New API key">
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            create.mutate(
              { name: name.trim(), scopes: chosen, expires_in_days: Number(days) },
              {
                onSuccess: (r) => {
                  onCreated(r.token);
                  close();
                },
              },
            );
          }}
        >
          <Field label="Name" htmlFor="key-name" hint="Which app uses it, e.g. siteherd.">
            <Input id="key-name" required maxLength={64} value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <fieldset className="flex flex-col gap-2.5">
            <legend className="mb-1 text-sm font-medium">Scopes (all read-only)</legend>
            {scopes.map((s) => (
              <Checkbox key={s.name} label={s.name} description={s.description} checked={chosen.includes(s.name)} onChange={(e) => toggle(s.name, e.target.checked)} />
            ))}
          </fieldset>
          <Field label="Expires" htmlFor="key-exp">
            <Select id="key-exp" value={days} onChange={(e) => setDays(e.target.value)}>
              <option value="30">in 30 days</option>
              <option value="90">in 90 days</option>
              <option value="365">in 1 year</option>
              <option value="0">never</option>
            </Select>
          </Field>
          <FormError message={create.error?.message} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending || chosen.length === 0}>
              Create key
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
