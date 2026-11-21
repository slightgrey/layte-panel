import { useQueryClient } from "@tanstack/react-query";
import { Download, Lock, Plus, RotateCw, Shield, ShieldOff, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { securityKey, useAllowRule, useDeleteRule, useInstallUpdates, useSecurity, useSetFirewall } from "@/api/security";
import type { SecurityResponse } from "@/api/types";
import { JobProgress } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { FormError } from "./AuthShell";

export function SecurityPage() {
  const sec = useSecurity();
  if (sec.isPending) return <Spinner />;
  if (sec.error) return <p className="text-sm text-destructive">{sec.error.message}</p>;
  const { overview: ov } = sec.data;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Security</h1>
        <p className="text-sm text-muted-foreground">Firewall, security updates and failed SSH logins.</p>
      </div>
      {ov.reboot_required && (
        <div role="alert" className="flex items-center gap-2 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 text-sm">
          <RotateCw className="size-4 text-warning" /> A reboot is required to finish installing updates (e.g. a new kernel).
        </div>
      )}
      <div className="grid gap-6 lg:grid-cols-2">
        <UpdatesCard data={sec.data} />
        <FailedLoginsCard data={sec.data} />
      </div>
      <FirewallCard data={sec.data} />
    </div>
  );
}

function UpdatesCard({ data }: { data: SecurityResponse }) {
  const install = useInstallUpdates();
  const qc = useQueryClient();
  const [jobId, setJobId] = useState<string | null>(null);
  const up = data.updates;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Updates</CardTitle>
        {up && (up.security > 0 ? <Badge variant="danger">{up.security} security</Badge> : <Badge variant="success">no security updates</Badge>)}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        {data.updates_error ? (
          <p className="text-sm text-muted-foreground">Unavailable: {data.updates_error.message}</p>
        ) : (
          <p className="text-sm">
            {up.total} package{up.total === 1 ? "" : "s"} can be upgraded, {up.security} of them security fixes.
          </p>
        )}
        {jobId ? (
          <JobProgress jobId={jobId} onDone={() => qc.invalidateQueries({ queryKey: securityKey })} />
        ) : (
          <div>
            <Button disabled={install.isPending || up?.security === 0} onClick={() => install.mutate(undefined, { onSuccess: (r) => setJobId(r.job_id) })}>
              <Download /> Install security updates
            </Button>
            <p className="mt-2 text-xs text-muted-foreground">Runs unattended-upgrade, which installs security updates only.</p>
          </div>
        )}
        <FormError message={install.error?.message} />
      </CardContent>
    </Card>
  );
}

function FailedLoginsCard({ data }: { data: SecurityResponse }) {
  const ov = data.overview;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Failed SSH logins</CardTitle>
        <Badge variant={ov.failed_total > 0 ? "warning" : "success"}>{ov.failed_total} recent</Badge>
      </CardHeader>
      <CardContent>
        {ov.failed_logins.length === 0 ? (
          <p className="text-sm text-muted-foreground">No failed logins in the recent auth log.</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>IP</TableHead>
                <TableHead className="text-right">Attempts</TableHead>
                <TableHead>Users tried</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ov.failed_logins.slice(0, 10).map((f) => (
                <TableRow key={f.ip}>
                  <TableCell className="font-mono text-xs" title={`last: ${f.last}`}>
                    {f.ip}
                  </TableCell>
                  <TableCell className="text-right tabular-nums">{f.count}</TableCell>
                  <TableCell className="text-xs text-muted-foreground">{f.users.join(", ") || "–"}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
        <p className="mt-3 text-xs text-muted-foreground">
          From /var/log/auth.log. Key-only SSH (PasswordAuthentication no) makes these attempts harmless; fail2ban can block repeat offenders.
        </p>
      </CardContent>
    </Card>
  );
}

function FirewallCard({ data }: { data: SecurityResponse }) {
  const ufw = data.overview.ufw;
  const setFw = useSetFirewall();
  const del = useDeleteRule();
  const allow = useAllowRule();
  const [port, setPort] = useState("");
  const [proto, setProto] = useState("tcp");
  const [from, setFrom] = useState("");

  if (!ufw.installed) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Firewall</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground">ufw is not installed (apt install ufw).</p>
        </CardContent>
      </Card>
    );
  }
  const ssh = data.overview.ssh_ports.join(", ");
  return (
    <Card>
      <CardHeader>
        <CardTitle>Firewall (ufw)</CardTitle>
        <div className="flex items-center gap-2">
          {ufw.active ? <Badge variant="success">active</Badge> : <Badge variant="danger">inactive</Badge>}
          <Button
            variant="outline"
            size="sm"
            disabled={setFw.isPending}
            onClick={() => {
              if (ufw.active && !confirm("Turn off the firewall? All ports will be reachable.")) return;
              setFw.mutate(!ufw.active);
            }}
          >
            {ufw.active ? <ShieldOff /> : <Shield />} {ufw.active ? "Disable" : "Enable"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          {ufw.active ? `Incoming default: ${ufw.default_incoming || "?"}. ` : "Enabling first allows "}
          SSH ({ssh}), 80 and 443 are always kept open so you can't lock yourself out of the server or this panel.
        </p>
        <FormError message={(setFw.error ?? del.error)?.message} />
        {ufw.active && (
          <>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>#</TableHead>
                  <TableHead>Port</TableHead>
                  <TableHead>Action</TableHead>
                  <TableHead>From</TableHead>
                  <TableHead />
                </TableRow>
              </TableHeader>
              <TableBody>
                {ufw.rules.map((r) => (
                  <TableRow key={r.num}>
                    <TableCell className="text-xs text-muted-foreground">{r.num}</TableCell>
                    <TableCell className="font-mono text-xs">
                      {r.to}
                      {r.v6 && <span className="text-muted-foreground"> (IPv6)</span>}
                    </TableCell>
                    <TableCell>
                      <Badge variant={r.action.startsWith("ALLOW") ? "success" : "danger"}>{r.action.toLowerCase()}</Badge>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{r.from}</TableCell>
                    <TableCell className="text-right">
                      {r.locked ? (
                        <Lock className="ml-auto size-4 text-muted-foreground" aria-label="protected" />
                      ) : (
                        <Button variant="ghost" size="sm" className="text-destructive" aria-label={`Delete rule ${r.num}`} disabled={del.isPending}
                          onClick={() => confirm(`Delete rule ${r.num} (${r.text})?`) && del.mutate({ num: r.num, expect: r.text })}>
                          <Trash2 />
                        </Button>
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
            <form
              className="flex flex-wrap items-end gap-3 border-t pt-4"
              onSubmit={(e: FormEvent) => {
                e.preventDefault();
                allow.mutate({ port: port.trim(), proto, from: from.trim() }, { onSuccess: () => { setPort(""); setFrom(""); } });
              }}
            >
              <Field label="Port" htmlFor="fw-port">
                <Input id="fw-port" required placeholder="8080 or 8000:8100" className="w-44" value={port} onChange={(e) => setPort(e.target.value)} />
              </Field>
              <Field label="Protocol" htmlFor="fw-proto">
                <Select id="fw-proto" className="w-28" value={proto} onChange={(e) => setProto(e.target.value)}>
                  <option value="tcp">TCP</option>
                  <option value="udp">UDP</option>
                  <option value="">Both</option>
                </Select>
              </Field>
              <Field label="From (optional)" htmlFor="fw-from">
                <Input id="fw-from" placeholder="anywhere, or 203.0.113.0/24" className="w-56" value={from} onChange={(e) => setFrom(e.target.value)} />
              </Field>
              <Button type="submit" disabled={allow.isPending}>
                <Plus /> Allow
              </Button>
            </form>
            <FormError message={allow.error?.message} />
          </>
        )}
      </CardContent>
    </Card>
  );
}
