import { Clock } from "lucide-react";
import { useCron } from "@/api/cron";
import type { Site } from "@/api/types";
import { AddCronForm, CronRow } from "@/components/Cron";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function SiteCronPage() {
  return <SiteShell title="Cron jobs">{(site) => <CronTab site={site} />}</SiteShell>;
}

function CronTab({ site }: { site: Site }) {
  const cron = useCron();
  // Same rule as the global Cron page: only active sites created or adopted by Layte can be changed.
  const writable = (site.managed || site.adopted) && site.status === "active";
  const jobs = (cron.data?.jobs ?? []).filter((j) => j.site_id === site.id);

  return (
    <>
      {writable && <AddCronForm sites={[site]} wpCron={cron.data?.wp_cron ?? {}} siteLocked />}

      <Card>
        <CardContent className="pt-5">
          {cron.isPending ? (
            <Spinner />
          ) : cron.error ? (
            <p className="text-sm text-destructive">{cron.error.message}</p>
          ) : jobs.length === 0 ? (
            <EmptyState icon={Clock} title="No cron jobs for this site yet">
              {writable ? "Jobs you add here show up in the global Cron page too." : "This site is read-only here, so jobs can't be added."}
            </EmptyState>
          ) : (
            <>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Schedule</TableHead>
                    <TableHead>Command</TableHead>
                    {writable && <TableHead className="text-right">Actions</TableHead>}
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {jobs.map((j) => (
                    <CronRow key={j.id} job={j} readOnly={!writable} />
                  ))}
                </TableBody>
              </Table>
              {!writable && <p className="mt-3 text-xs text-muted-foreground">This site is read-only here, so its jobs can't be changed.</p>}
            </>
          )}
        </CardContent>
      </Card>
    </>
  );
}
