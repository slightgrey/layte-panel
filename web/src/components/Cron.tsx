import { Pause, Play, Plus, Trash2 } from "lucide-react";
import { type FormEvent, useState } from "react";
import { Link } from "react-router";
import { useAddCron, useDeleteCron, useToggleCron } from "@/api/cron";
import type { CronJob } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { TableCell, TableRow } from "@/components/ui/table";
import { FormError } from "@/pages/AuthShell";

export const cronPresets: { label: string; value: string }[] = [
  { label: "Every 5 minutes", value: "*/5 * * * *" },
  { label: "Every 15 minutes", value: "*/15 * * * *" },
  { label: "Hourly", value: "@hourly" },
  { label: "Daily at 03:00", value: "0 3 * * *" },
  { label: "Weekly (Sunday 03:00)", value: "0 3 * * 0" },
  { label: "Custom…", value: "" },
];

/** A short human description of common schedules. */
export function describeSchedule(schedule: string) {
  return cronPresets.find((p) => p.value === schedule)?.label ?? schedule;
}

/** One job row. showSite adds the site column; readOnly drops the actions column. */
export function CronRow({ job, showSite = false, readOnly = false }: { job: CronJob; showSite?: boolean; readOnly?: boolean }) {
  const toggle = useToggleCron();
  const del = useDeleteCron();
  return (
    <TableRow className={job.enabled ? "" : "opacity-60"}>
      {showSite && (
        <TableCell>
          <Link to={`/sites/${job.site_id}`} className="hover:underline">
            {job.site_domain}
          </Link>
        </TableCell>
      )}
      <TableCell>
        <div className="text-sm">{describeSchedule(job.schedule)}</div>
        <div className="font-mono text-xs text-muted-foreground">{job.schedule}</div>
      </TableCell>
      <TableCell className="max-w-md">
        <code className="block truncate font-mono text-xs" title={job.command}>
          {job.command}
        </code>
        {!job.enabled && <Badge className="mt-1">paused</Badge>}
        {(toggle.error || del.error) && <div className="text-xs text-destructive">{(toggle.error ?? del.error)?.message}</div>}
      </TableCell>
      {!readOnly && (
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
      )}
    </TableRow>
  );
}

/**
 * "Add a cron job" form. Shows a site picker, unless siteLocked: then the
 * first site is used and the hint names its Linux user (per-site tab).
 */
export function AddCronForm({
  sites,
  wpCron,
  siteLocked = false,
}: {
  sites: { id: number; domain: string; system_user?: string }[];
  wpCron: Record<string, string>;
  siteLocked?: boolean;
}) {
  const add = useAddCron();
  const [siteId, setSiteId] = useState(String(sites[0]?.id ?? ""));
  const [preset, setPreset] = useState(cronPresets[0]!.value);
  const [custom, setCustom] = useState("");
  const [command, setCommand] = useState("");
  const schedule = preset || custom;
  const wp = wpCron[siteId];
  const pickSite = !siteLocked;
  const runAs = !pickSite ? sites[0]?.system_user : undefined;

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
            {pickSite && (
              <Field label="Site" htmlFor="cron-site">
                <Select id="cron-site" value={siteId} onChange={(e) => setSiteId(e.target.value)}>
                  {sites.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.domain}
                    </option>
                  ))}
                </Select>
              </Field>
            )}
            <Field label="Schedule" htmlFor="cron-preset">
              <Select id="cron-preset" value={preset} onChange={(e) => setPreset(e.target.value)}>
                {cronPresets.map((p) => (
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
              (runAs || wp) && (
              <>
                {runAs && (
                  <>
                    Runs as <code className="font-mono">{runAs}</code> from the site folder.{" "}
                  </>
                )}
                {wp && (
                  <button type="button" className="text-primary hover:underline" onClick={() => setCommand(wp)}>
                    Use WP-Cron for this WordPress site
                  </button>
                )}
              </>
              )
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
