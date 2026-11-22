import { Clock, Plus } from "lucide-react";
import type { Site } from "@/api/types";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field, Select } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";

// UI only for now: the form is disabled until it's wired to the cron API.
export function SiteCronPage() {
  return <SiteShell title="Cron jobs" preview>{(site) => <CronTab site={site} />}</SiteShell>;
}

function CronTab({ site }: { site: Site }) {
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Add a cron job</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <div className="grid gap-4 sm:grid-cols-[14rem_1fr]">
            <Field label="Schedule" htmlFor="site-cron-schedule">
              <Select id="site-cron-schedule" disabled defaultValue="*/5 * * * *">
                <option value="*/5 * * * *">Every 5 minutes</option>
                <option value="0 * * * *">Every hour</option>
                <option value="0 3 * * *">Daily at 03:00</option>
              </Select>
            </Field>
            <Field
              label="Command"
              htmlFor="site-cron-command"
              hint={
                <>
                  Runs as <code className="font-mono">{site.system_user || "the site user"}</code> from the site folder.
                </>
              }
            >
              <Input id="site-cron-command" disabled placeholder="/usr/bin/php8.3 public/my-task.php" className="font-mono" />
            </Field>
          </div>
          <div className="flex justify-end">
            <Button disabled>
              <Plus /> Add job
            </Button>
          </div>
        </CardContent>
      </Card>

      <Card>
        <CardContent className="pt-5">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Schedule</TableHead>
                <TableHead>Command</TableHead>
                <TableHead>Last run</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody />
          </Table>
          <EmptyState icon={Clock} title="No cron jobs for this site yet">
            Jobs you add here show up in the global Cron page too.
          </EmptyState>
        </CardContent>
      </Card>
    </>
  );
}
