import { Check, LoaderCircle, RefreshCw, RotateCcw } from "lucide-react";
import { useState } from "react";
import { type ServiceDetail, useServiceControl, useServicesDetail } from "@/api/services";
import { Spinner } from "@/components/Spinner";
import { SystemShell } from "@/components/SystemShell";
import { Badge, type BadgeProps } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { formatBytes } from "@/lib/format";
import { FormError } from "./AuthShell";

function stateBadge(s: ServiceDetail): { variant: BadgeProps["variant"]; text: string } {
  if (s.active === "active") return { variant: "success", text: s.sub === "running" ? "running" : s.sub };
  if (s.active === "failed") return { variant: "danger", text: "failed" };
  return { variant: "warning", text: s.active };
}

/** "3 days", "5 h", "12 min" since the service started. */
function since(iso?: string) {
  if (!iso) return "–";
  const s = (Date.now() - new Date(iso).getTime()) / 1000;
  if (s < 3600) return `${Math.max(1, Math.round(s / 60))} min`;
  if (s < 86400) return `${Math.round(s / 3600)} h`;
  return `${Math.round(s / 86400)} days`;
}

const explain: Record<string, string> = {
  reload:
    "Reload: re-reads its config without stopping. Its config test runs first; if the config is broken, nothing changes and you see the error. Open connections and running requests carry on.",
  restart: "Restart: stops and starts it again (it has no reload).",
};

/** Every service on the server, with a soft reload (or restart) each. */
export function SystemServicesPage() {
  const services = useServicesDetail();
  return (
    <SystemShell title="Services" description="What runs this server. Reload a service after changing its config by hand, or restart one that misbehaves.">
      <Card>
        <CardContent className="pt-5">
          {services.isPending ? (
            <Spinner />
          ) : services.error ? (
            <p className="text-sm text-destructive">{services.error.message}</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Service</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead className="text-right">Up for</TableHead>
                  <TableHead className="text-right">Memory</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {services.data.services.map((s) => (
                  <ServiceRow key={s.name} s={s} />
                ))}
              </TableBody>
            </Table>
          )}
          <p className="mt-4 text-xs text-muted-foreground">
            {explain.reload} {explain.restart} Layte Panel's own services have no button: restarting the agent from the panel would cut off the action
            itself (use <code className="font-mono">sudo systemctl restart layte layte-agent</code> on the server).
          </p>
        </CardContent>
      </Card>
    </SystemShell>
  );
}

function ServiceRow({ s }: { s: ServiceDetail }) {
  const control = useServiceControl();
  const [done, setDone] = useState(false);
  const b = stateBadge(s);
  const a = s.action;
  const run = () => {
    if (!a) return;
    if (a.op === "restart" && !confirm(a.disruptive ? `Restart ${s.name}? Every site that uses the database gets errors for a few seconds.` : `Restart ${s.name}?`)) return;
    setDone(false);
    control.mutate({ name: s.name, op: a.op }, { onSuccess: () => (setDone(true), setTimeout(() => setDone(false), 3000)) });
  };
  return (
    <TableRow>
      <TableCell>
        <div className="font-medium">{s.name}</div>
        <div className="text-xs text-muted-foreground">{s.description}</div>
        {control.error && <FormError message={control.error.message} />}
      </TableCell>
      <TableCell>
        <Badge variant={b.variant}>{b.text}</Badge>
        {s.enabled === "enabled" && <div className="mt-1 text-[0.6875rem] text-muted-foreground">starts on boot</div>}
      </TableCell>
      <TableCell className="text-right text-xs tabular-nums">{since(s.since)}</TableCell>
      <TableCell className="text-right text-xs tabular-nums">{s.memory_bytes ? formatBytes(s.memory_bytes) : "–"}</TableCell>
      <TableCell className="text-right">
        {a ? (
          <Button
            variant="outline"
            size="sm"
            className={a.disruptive ? "text-destructive" : undefined}
            disabled={control.isPending}
            onClick={run}
            title={explain[a.op]}
          >
            {control.isPending ? (
              <>
                <LoaderCircle className="animate-spin" /> {a.op === "reload" ? "Reloading…" : "Restarting…"}
              </>
            ) : done ? (
              <>
                <Check className="text-success" /> {a.op === "reload" ? "Reloaded" : "Restarted"}
              </>
            ) : a.op === "reload" ? (
              <>
                <RefreshCw /> Reload
              </>
            ) : (
              <>
                <RotateCcw /> Restart
              </>
            )}
          </Button>
        ) : (
          <span className="text-xs text-muted-foreground">–</span>
        )}
      </TableCell>
    </TableRow>
  );
}
