import { useCron } from "@/api/cron";
import { useSites } from "@/api/sites";
import { AddCronForm, CronRow } from "@/components/Cron";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function CronPage() {
  const cron = useCron();
  const sites = useSites();
  const managed = (sites.data?.sites ?? []).filter((s) => (s.managed || s.adopted) && s.status === "active");

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Cron jobs</h1>
        <p className="text-sm text-muted-foreground">
          Scheduled commands per site. They run as the site's own Linux user from the site folder, and their output goes
          to the site's <code className="font-mono">logs/cron.log</code>.
        </p>
      </div>

      {managed.length > 0 && <AddCronForm sites={managed} wpCron={cron.data?.wp_cron ?? {}} />}

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
                  <CronRow key={j.id} job={j} showSite />
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
