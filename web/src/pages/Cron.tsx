import { Pause, Play, Plus, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";
import { useAddCron, useCron, useDeleteCron, useToggleCron } from "@/api/cron";
import { useSites } from "@/api/sites";
import type { CronJob } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { FormError } from "./AuthShell";

const presets: { label: string; value: string }[] = [
  { label: "Every 5 minutes", value: "*/5 * * * *" },
  { label: "Every 15 minutes", value: "*/15 * * * *" },
  { label: "Hourly", value: "@hourly" },
  { label: "Daily at 03:00", value: "0 3 * * *" },
  { label: "Weekly (Sunday 03:00)", value: "0 3 * * 0" },
  { label: "Custom…", value: "" },
];

/** A short human description of common schedules. */
function describe(schedule: string) {
  return presets.find((p) => p.value === schedule)?.label ?? schedule;
}

export function CronPage() {
  const cron = useCron();
  const sites = useSites();
  const managed = (sites.data?.sites ?? []).filter((s) => s.managed && s.status === "active");

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">Cron jobs</h1>
        <p className="text-sm text-muted-foreground">
          Scheduled commands per site. They run as the site's own Linux user from the site folder, and their output goes
          to the site's <code className="font-mono">logs/cron.log</code>.
        </p>
      </div>

      {managed.length > 0 && <AddCron sites={managed} wpCron={cron.data?.wp_cron ?? {}} />}

      <Card>
        <CardContent className="pt-5">
          {cron.isPending ? (
            <Spinner />
          ) : cron.error ? (
            <p className="text-sm text-destructive">{cron.error.message}</p>
          ) : cron.data.jobs.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              No cron jobs yet.{managed.length === 0 && " Create a site first; imported sites are read-only."}
            </p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Site</TableHead>
                  <TableHead>Schedule</TableHead>
                  <TableHead>Command</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {cron.data.jobs.map((j) => (
                  <CronRow key={j.id} job={j} />
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}

function CronRow({ job }: { job: CronJob }) {
  const toggle = useToggleCron();
  const del = useDeleteCron();
  return (
    <TableRow className={job.enabled ? "" : "opacity-60"}>
      <TableCell>
        <Link to={`/sites/${job.site_id}`} className="hover:underline">
          {job.site_domain}
        </Link>
      </TableCell>
      <TableCell>
        <div className="text-sm">{describe(job.schedule)}</div>
        <div className="font-mono text-xs text-muted-foreground">{job.schedule}</div>
      </TableCell>
      <TableCell className="max-w-md">
        <code className="block truncate font-mono text-xs" title={job.command}>
          {job.command}
        </code>
        {!job.enabled && <Badge className="mt-1">paused</Badge>}
        {(toggle.error || del.error) && <div className="text-xs text-destructive">{(toggle.error ?? del.error)?.message}</div>}
      </TableCell>
      <TableCell>
        <div className="flex justify-end gap-1">
          <Button variant="ghost" size="sm" disabled={toggle.isPending} onClick={() => toggle.mutate({ id: job.id, enabled: !job.enabled })}>
            {job.enabled ? <Pause /> : <Play />} {job.enabled ? "Pause" : "Resume"}
          </Button>
          <Button variant="ghost" size="sm" className="text-destructive" aria-label="Delete" disabled={del.isPending} onClick={() => del.mutate(job.id)}>
            <Trash2 />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  );
}

function AddCron({ sites, wpCron }: { sites: { id: number; domain: string }[]; wpCron: Record<string, string> }) {
  const add = useAddCron();
  const [siteId, setSiteId] = useState(String(sites[0]?.id ?? ""));
  const [preset, setPreset] = useState(presets[0]!.value);
  const [custom, setCustom] = useState("");
  const [command, setCommand] = useState("");
  const schedule = preset || custom;
  const wp = wpCron[siteId];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Add a cron job</CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            add.mutate({ siteId: Number(siteId), schedule, command }, { onSuccess: () => setCommand("") });
          }}
        >
          <div className="grid gap-4 sm:grid-cols-3">
            <Field label="Site" htmlFor="cron-site">
              <Select id="cron-site" value={siteId} onChange={(e) => setSiteId(e.target.value)}>
                {sites.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.domain}
                  </option>
                ))}
              </Select>
            </Field>
            <Field label="Schedule" htmlFor="cron-preset">
              <Select id="cron-preset" value={preset} onChange={(e) => setPreset(e.target.value)}>
                {presets.map((p) => (
                  <option key={p.label} value={p.value}>
                    {p.label}
                  </option>
                ))}
              </Select>
            </Field>
            {preset === "" && (
              <Field label="Cron expression" htmlFor="cron-custom" hint="minute hour day month weekday">
                <Input id="cron-custom" required placeholder="30 2 * * 1-5" className="font-mono" value={custom} onChange={(e) => setCustom(e.target.value)} />
              </Field>
            )}
          </div>
          <Field
            label="Command"
            htmlFor="cron-command"
            hint={
              wp ? (
                <button type="button" className="text-primary hover:underline" onClick={() => setCommand(wp)}>
                  Use WP-Cron for this WordPress site
                </button>
              ) : undefined
            }
          >
            <Input id="cron-command" required className="font-mono" placeholder="/usr/bin/php8.3 public/my-task.php" value={command} onChange={(e) => setCommand(e.target.value)} />
          </Field>
          <FormError message={add.error?.message} />
          <div className="flex justify-end">
            <Button type="submit" disabled={add.isPending || !schedule || !command.trim()}>
              <Plus /> Add job
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  );
}
