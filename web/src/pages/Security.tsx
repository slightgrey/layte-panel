import { useQueryClient } from "@tanstack/react-query";
import { Cloud, Download, Lock, Plus, RefreshCw, RotateCw, Shield, ShieldOff, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";
import {
  securityKey,
  useAllowRule,
  useDeleteRule,
  useInstallUpdates,
  useSecurity,
  useSetCloudflare,
  useSetFirewall,
} from "@/api/security";
import type { SSHGuardStatus, SecurityResponse } from "@/api/types";
import { JobProgress } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Notice, progressFrame } from "@/components/ui/notice";
import { cn } from "@/lib/utils";
import { FormError } from "./AuthShell";

export function SecurityPage() {
  const sec = useSecurity();
  if (sec.isPending) return <Spinner />;
  if (sec.error) return <p className="text-sm text-destructive">{sec.error.message}</p>;
  const { overview: ov } = sec.data;

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Security</h1>
        <p className="text-sm text-muted-foreground">Firewall, security updates, SSH logins and brute-force blocking, and Cloudflare.</p>
      </div>
      {ov.reboot_required && (
        <Notice tone="warning" icon={RotateCw}>
          A reboot is required to finish installing updates (e.g. a new kernel).
        </Notice>
      )}
      <div className="grid gap-6 lg:grid-cols-2">
        <UpdatesCard data={sec.data} />
        <FailedLoginsCard data={sec.data} />
      </div>
      <SSHGuardCard status={ov.sshguard} />
      <FirewallCard data={sec.data} />
      <CloudflareCard data={sec.data} />
    </div>
  );
}

function CloudflareCard({ data }: { data: SecurityResponse }) {
  const cf = data.overview.cloudflare;
  const set = useSetCloudflare();
  const blocked = !!cf.problem || (cf.conflicts?.length ?? 0) > 0;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Behind Cloudflare</CardTitle>
        <div className="flex items-center gap-2">
          {cf.enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>}
          <Button
            variant="outline"
            size="sm"
            disabled={set.isPending || (!cf.enabled && blocked)}
            onClick={() => {
              if (cf.enabled && !confirm("Turn off real visitor IPs? Sites behind Cloudflare's proxy will see Cloudflare's addresses again.")) return;
              set.mutate(!cf.enabled);
            }}
          >
            <Cloud /> {cf.enabled ? "Turn off" : "Turn on"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-sm text-muted-foreground">
          Turn this on if any site uses Cloudflare's proxy (orange cloud). nginx, PHP and WordPress then see your visitors' real IP
          addresses instead of Cloudflare's, which logs and login lockouts need. Only requests that really come from Cloudflare are
          trusted, so it's safe for sites that don't use the proxy.
        </p>
        <p className="text-xs text-muted-foreground">
          {cf.ranges} Cloudflare address ranges, list from {cf.list_date}. It's updated with Layte Panel releases.
        </p>
        {cf.enabled && !cf.up_to_date && (
          <Notice
            action={
              <Button size="sm" disabled={set.isPending} onClick={() => set.mutate(true)}>
                <RefreshCw /> Update
              </Button>
            }
          >
            This version of Layte Panel has a newer list of Cloudflare addresses.
          </Notice>
        )}
        {cf.problem && <p className="text-sm text-destructive">Can't turn this on: {cf.problem}.</p>}
        {!cf.enabled && (cf.conflicts?.length ?? 0) > 0 && (
          <p className="text-sm text-muted-foreground">
            Real visitor IPs are already set up in <span className="font-mono text-xs">{cf.conflicts!.join(", ")}</span>. Remove that
            file to let Layte Panel manage it (and keep its list up to date).
          </p>
        )}
        <FormError message={set.error?.message} />
      </CardContent>
    </Card>
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
          <div className={cn("rounded-[14px] p-4", progressFrame)}>
            <JobProgress jobId={jobId} onDone={() => qc.invalidateQueries({ queryKey: securityKey })} />
          </div>
        ) : (
          <div>
            <Button disabled={install.isPending || up?.security === 0} onClick={() => install.mutate(undefined, { onSuccess: (r) => setJobId(r.job_id) })}>
              <Download /> Install security updates
            </Button>
            <p className="mt-2 text-xs text-muted-foreground">
              Runs unattended-upgrade, which installs security updates only. For all updates, use the{" "}
              <Link to="/updates" className="text-primary hover:underline">
                Updates
              </Link>{" "}
              page.
            </p>
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
          From /var/log/auth.log. Key-only SSH (PasswordAuthentication no) makes these attempts harmless; sshguard blocks repeat offenders (below).
        </p>
      </CardContent>
    </Card>
  );
}

/** sshguard: what it's blocking (read-only; its settings stay the admin's). */
function SSHGuardCard({ status: g }: { status?: SSHGuardStatus }) {
  if (!g) return null;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Brute-force blocking (sshguard)</CardTitle>
        {!g.installed ? (
          <Badge variant="warning">not installed</Badge>
        ) : g.running ? (
          <Badge variant="success">running</Badge>
        ) : (
          <Badge variant="danger">stopped</Badge>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {!g.installed ? (
          <p className="text-sm text-muted-foreground">
            sshguard blocks IPs that keep failing to log in over SSH, longer each time they come back. It uses a few MB of
            memory, which suits small servers. Install it on the server with <code className="font-mono">sudo apt install sshguard</code>.
          </p>
        ) : (
          <>
            {!g.running && (
              <Notice tone="warning">
                sshguard is installed but not running, so nothing is being blocked. Start it with{" "}
                <code className="font-mono">sudo systemctl enable --now sshguard</code>.
              </Notice>
            )}
            <div className="grid grid-cols-2 gap-3 sm:grid-cols-4">
              <Stat label="Blocked now" value={g.blocked_now} />
              <Stat label="Blocked for good" value={g.permanent} />
              <Stat label="Attacks, 24 h" value={g.attacks_24h} />
              <Stat label="Blocks, 24 h" value={g.blocks_24h} />
            </div>
            {g.recent.length > 0 && (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Latest blocks</TableHead>
                    <TableHead>When</TableHead>
                    <TableHead className="text-right">For</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {g.recent.map((b) => (
                    <TableRow key={b.time + b.address}>
                      <TableCell className="font-mono text-xs">{b.address}</TableCell>
                      <TableCell className="text-xs text-muted-foreground">{new Date(b.time).toLocaleString()}</TableCell>
                      <TableCell className="text-right text-xs">{b.for}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
            <p className="text-xs text-muted-foreground">
              Blocks start short and grow for repeat attackers; persistent ones go on the permanent list
              (/var/lib/sshguard/enemies). Successful key logins never count. If you lock yourself out, see the Troubleshooting
              docs.
            </p>
          </>
        )}
      </CardContent>
    </Card>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[12px] bg-background px-3 py-2 shadow-inset">
      <div className="text-lg font-semibold tabular-nums">{value.toLocaleString()}</div>
      <div className="text-xs text-muted-foreground">{label}</div>
    </div>
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
