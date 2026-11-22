import { AlertTriangle, ChevronDown, Cpu, HardDrive, MemoryStick, Server } from "lucide-react";
import { type ReactNode, useState } from "react";
import { useDashboard } from "@/api/dashboard";
import type { Section, Service, SystemInfo, Updates, Version } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { UsageBar } from "@/components/ui/progress";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes, formatUptime, percent } from "@/lib/format";

export function DashboardPage() {
  const { data, error, isPending, dataUpdatedAt } = useDashboard();

  if (isPending) {
    return (
      <div className="grid h-64 place-items-center">
        <Spinner />
      </div>
    );
  }
  if (error) return <Notice>Could not load the dashboard: {error.message}</Notice>;

  const agentDown = Object.values(data).every((s) => s.error?.code === "agent_unavailable");

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{data.system.data?.hostname ?? "Dashboard"}</h1>
          <p className="text-sm text-muted-foreground">{data.system.data?.os}</p>
        </div>
        <p className="text-xs text-muted-foreground">Updated {new Date(dataUpdatedAt).toLocaleTimeString()} · refreshes every 30s</p>
      </div>

      {agentDown && (
        <Notice>
          The panel can't reach <code className="font-mono">layte-agent</code>. Check it with{" "}
          <code className="font-mono">systemctl status layte-agent</code>.
        </Notice>
      )}

      <SystemCards section={data.system} />

      <div className="grid gap-6 lg:grid-cols-2">
        <ServicesCard section={data.services} />
        <VersionsCard section={data.versions} />
      </div>
      <UpdatesCard section={data.updates} />
    </div>
  );
}

function Notice({ children }: { children: ReactNode }) {
  return (
    <div role="alert" className="flex items-start gap-2 rounded-[14px] bg-warning/10 shadow-inset px-4 py-3 text-sm">
      <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
      <div>{children}</div>
    </div>
  );
}

/** Shows the section's error instead of its content when the check failed. */
function SectionBody<T>({ section, children }: { section: Section<T>; children: (data: T) => ReactNode }) {
  if (section.error || section.data === undefined) {
    return <p className="text-sm text-muted-foreground">Unavailable: {section.error?.message ?? "no data"}</p>;
  }
  return <>{children(section.data)}</>;
}

function Stat({ icon, label, children }: { icon: ReactNode; label: string; children: ReactNode }) {
  return (
    <Card>
      <CardHeader className="pb-2">
        <CardTitle className="text-muted-foreground font-medium">{label}</CardTitle>
        <span className="text-muted-foreground [&_svg]:size-4">{icon}</span>
      </CardHeader>
      <CardContent className="flex flex-col gap-2">{children}</CardContent>
    </Card>
  );
}

function SystemCards({ section }: { section: Section<SystemInfo> }) {
  const s = section.data;
  if (!s) {
    return (
      <Card>
        <CardContent className="pt-5">
          <SectionBody section={section}>{() => null}</SectionBody>
        </CardContent>
      </Card>
    );
  }
  const mem = percent(s.memory.used, s.memory.total);
  return (
    <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <Stat icon={<Server />} label="System">
        <p className="text-2xl font-semibold tabular-nums">{formatUptime(s.uptime_seconds)}</p>
        <p className="text-xs text-muted-foreground">uptime · kernel {s.kernel}</p>
      </Stat>
      <Stat icon={<Cpu />} label="Load">
        <p className="text-2xl font-semibold tabular-nums">{s.load[0].toFixed(2)}</p>
        <p className="text-xs text-muted-foreground">
          {s.load[1].toFixed(2)} · {s.load[2].toFixed(2)} (5/15 min) · {s.cpus} vCPU
        </p>
      </Stat>
      <Stat icon={<MemoryStick />} label="Memory">
        <p className="text-2xl font-semibold tabular-nums">{mem}%</p>
        <UsageBar value={mem} label="Memory used" />
        <p className="text-xs text-muted-foreground">
          {formatBytes(s.memory.used)} of {formatBytes(s.memory.total)}
          {s.memory.swap_total > 0 && ` · swap ${formatBytes(s.memory.swap_used)}`}
        </p>
      </Stat>
      <Stat icon={<HardDrive />} label="Disk">
        {s.disks.length === 0 && <p className="text-sm text-muted-foreground">No data</p>}
        {s.disks.map((d) => {
          const p = percent(d.used, d.total);
          return (
            <div key={d.path} className="flex flex-col gap-1">
              <div className="flex justify-between text-xs">
                <span className="font-mono">{d.path}</span>
                <span className="tabular-nums text-muted-foreground">
                  {formatBytes(d.used)} / {formatBytes(d.total)}
                </span>
              </div>
              <UsageBar value={p} label={`Disk ${d.path} used`} />
            </div>
          );
        })}
      </Stat>
    </div>
  );
}

function serviceBadge(s: Service): { variant: BadgeProps["variant"]; text: string } {
  if (!s.installed) return { variant: "neutral", text: "not installed" };
  if (s.active === "active") return { variant: "success", text: s.sub === "running" ? "running" : s.sub };
  if (s.active === "failed") return { variant: "danger", text: "failed" };
  return { variant: "warning", text: s.active };
}

function ServicesCard({ section }: { section: Section<Service[]> }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Services</CardTitle>
      </CardHeader>
      <CardContent>
        <SectionBody section={section}>
          {(services) => {
            // Hide services that aren't installed, except nginx (always expected).
            const shown = services.filter((s) => s.installed || s.name === "nginx");
            return (
              <Table>
                <TableBody>
                  {shown.map((s) => {
                    const b = serviceBadge(s);
                    return (
                      <TableRow key={s.name}>
                        <TableCell>
                          <div className="font-medium">{s.name}</div>
                          <div className="text-xs text-muted-foreground">{s.description}</div>
                        </TableCell>
                        <TableCell className="text-xs text-muted-foreground">{s.enabled === "enabled" ? "starts on boot" : s.enabled}</TableCell>
                        <TableCell className="text-right">
                          <Badge variant={b.variant}>{b.text}</Badge>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            );
          }}
        </SectionBody>
      </CardContent>
    </Card>
  );
}

function VersionsCard({ section }: { section: Section<Version[]> }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Software</CardTitle>
      </CardHeader>
      <CardContent>
        <SectionBody section={section}>
          {(versions) => (
            <Table>
              <TableBody>
                {versions.map((v) => (
                  <TableRow key={v.name}>
                    <TableCell className="font-medium">{v.name}</TableCell>
                    <TableCell className="text-right font-mono text-xs">
                      {!v.installed ? (
                        <span className="text-muted-foreground">not installed</span>
                      ) : v.error ? (
                        <span className="text-destructive">error</span>
                      ) : (
                        v.version || "unknown"
                      )}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </SectionBody>
      </CardContent>
    </Card>
  );
}

function UpdatesCard({ section }: { section: Section<Updates> }) {
  const [open, setOpen] = useState(false);
  const u = section.data;
  return (
    <Card>
      <CardHeader>
        <CardTitle>Pending updates</CardTitle>
        {u && (
          <div className="flex gap-2">
            {u.security > 0 && <Badge variant="danger">{u.security} security</Badge>}
            <Badge variant={u.total > 0 ? "warning" : "success"}>{u.total === 0 ? "up to date" : `${u.total} total`}</Badge>
          </div>
        )}
      </CardHeader>
      <CardContent>
        <SectionBody section={section}>
          {(up) =>
            up.total === 0 ? (
              <p className="text-sm text-muted-foreground">All packages are up to date.</p>
            ) : (
              <>
                <Button variant="outline" size="sm" onClick={() => setOpen((o) => !o)} aria-expanded={open}>
                  <ChevronDown className={open ? "rotate-180 transition-transform" : "transition-transform"} />
                  {open ? "Hide packages" : "Show packages"}
                </Button>
                {open && (
                  <div className="mt-4">
                    <Table>
                      <TableHeader>
                        <TableRow>
                          <TableHead>Package</TableHead>
                          <TableHead>Installed</TableHead>
                          <TableHead>Available</TableHead>
                          <TableHead />
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {up.packages.map((p) => (
                          <TableRow key={p.name}>
                            <TableCell className="font-medium">{p.name}</TableCell>
                            <TableCell className="font-mono text-xs text-muted-foreground">{p.from}</TableCell>
                            <TableCell className="font-mono text-xs">{p.version}</TableCell>
                            <TableCell className="text-right">{p.security && <Badge variant="danger">security</Badge>}</TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                )}
                <p className="mt-3 text-xs text-muted-foreground">
                  From the local package lists, checked {new Date(up.checked_at).toLocaleString()}. Installing updates comes in a
                  later release.
                </p>
              </>
            )
          }
        </SectionBody>
      </CardContent>
    </Card>
  );
}
