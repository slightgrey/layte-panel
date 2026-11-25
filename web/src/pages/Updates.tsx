import { useQueryClient } from "@tanstack/react-query";
import { Download, PackageCheck, RefreshCw, RotateCw, Wrench } from "lucide-react";
import { useState } from "react";
import { Link } from "react-router";
import { ApiError } from "@/api/client";
import type { UpgradePlan } from "@/api/types";
import { useApplyUpdates, useRefreshUpdates, useUpdatesPlan } from "@/api/updates";
import { JobProgress } from "@/components/JobProgress";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Notice, progressFrame } from "@/components/ui/notice";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { cn } from "@/lib/utils";
import { FormError } from "./AuthShell";

export function UpdatesPage() {
  const plan = useUpdatesPlan();
  const refresh = useRefreshUpdates();
  const [jobId, setJobId] = useState<string | null>(null);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Updates</h1>
          <p className="text-sm text-muted-foreground">
            Install package updates for this server. Only upgrades: nothing is ever removed, and your config files are kept.
          </p>
        </div>
        <Button variant="outline" size="sm" disabled={refresh.isPending || Boolean(jobId)} onClick={() => refresh.mutate()}>
          <RefreshCw className={cn(refresh.isPending && "animate-spin")} /> {refresh.isPending ? "Checking…" : "Check for updates"}
        </Button>
      </div>
      <FormError message={refresh.error?.message} />

      {jobId && <JobCard jobId={jobId} onClose={() => setJobId(null)} />}

      {plan.isPending ? (
        <Spinner />
      ) : plan.error instanceof ApiError && plan.error.code === "apt_broken" ? (
        <RepairCard message={plan.error.message} busy={Boolean(jobId)} onStarted={setJobId} />
      ) : plan.error ? (
        <Notice tone="warning">Could not work out the pending updates: {plan.error.message}</Notice>
      ) : (
        <>
          <PendingCard plan={plan.data} busy={Boolean(jobId)} onStarted={setJobId} />
          {plan.data.kept_back.length > 0 && <KeptBackCard packages={plan.data.kept_back} busy={Boolean(jobId)} onStarted={setJobId} />}
          {(plan.data.phased ?? []).length > 0 && <KeptBackCard phased packages={plan.data.phased} busy={Boolean(jobId)} onStarted={setJobId} />}
        </>
      )}

      <p className="text-xs text-muted-foreground">
        Only security fixes? The <Link to="/security" className="text-primary hover:underline">Security</Link> page installs just those.
      </p>
    </div>
  );
}

function JobCard({ jobId, onClose }: { jobId: string; onClose: () => void }) {
  const qc = useQueryClient();
  const [done, setDone] = useState(false);
  return (
    <Card className={progressFrame}>
      <CardHeader>
        <CardTitle className="text-primary">Installing updates</CardTitle>
        {done && (
          <Button variant="ghost" size="sm" onClick={onClose}>
            Dismiss
          </Button>
        )}
      </CardHeader>
      <CardContent>
        <JobProgress
          jobId={jobId}
          onDone={() => {
            setDone(true);
            qc.invalidateQueries({ queryKey: ["updates"] });
            qc.invalidateQueries({ queryKey: ["dashboard"] });
          }}
        />
      </CardContent>
    </Card>
  );
}

/** What the preview says will happen, as notices. */
function Effects({ plan }: { plan: UpgradePlan }) {
  const heavy = plan.restarts.some((r) => r === "mysql" || r === "mariadb") || plan.reboot_likely;
  return (
    <div className="flex flex-col gap-3">
      {plan.restarts.length > 0 && (
        <Notice icon={RotateCw}>
          These restart during the install, each for a few seconds: <strong>{plan.restarts.join(", ")}</strong>. Sites may be
          briefly unavailable.
        </Notice>
      )}
      {plan.reboot_likely && (
        <Notice tone="warning">
          This includes a kernel or core system update, so a reboot will probably be needed afterwards. Layte Panel never
          reboots by itself.
        </Notice>
      )}
      {heavy && (
        <p className="text-xs text-muted-foreground">
          Package updates can't be undone automatically. For changes like this, a server snapshot (in your hosting provider's
          panel) beforehand is the real undo button.
        </p>
      )}
    </div>
  );
}

function PackageTable({ pkgs }: { pkgs: UpgradePlan["upgrades"] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Package</TableHead>
          <TableHead>Installed</TableHead>
          <TableHead>New</TableHead>
          <TableHead className="hidden md:table-cell">From</TableHead>
          <TableHead />
        </TableRow>
      </TableHeader>
      <TableBody>
        {pkgs.map((p) => (
          <TableRow key={p.name}>
            <TableCell className="font-medium">{p.name}</TableCell>
            <TableCell className="font-mono text-xs text-muted-foreground">{p.from || "new"}</TableCell>
            <TableCell className="font-mono text-xs">{p.to}</TableCell>
            <TableCell className="hidden text-xs text-muted-foreground md:table-cell">{p.origin}</TableCell>
            <TableCell className="text-right">{p.security && <Badge variant="danger">security</Badge>}</TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}

function PendingCard({ plan, busy, onStarted }: { plan: UpgradePlan; busy: boolean; onStarted: (id: string) => void }) {
  const [confirm, setConfirm] = useState(false);
  const n = plan.upgrades.length;
  const security = plan.upgrades.filter((p) => p.security).length;
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {n === 0 ? "No updates to install" : `${n} update${n === 1 ? "" : "s"} ready${security ? ` · ${security} security` : ""}`}
        </CardTitle>
        {n > 0 && (
          <Button size="sm" disabled={busy} onClick={() => setConfirm(true)}>
            <Download /> Install {n} update{n === 1 ? "" : "s"}
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {n === 0 ? (
          <p className="text-sm text-muted-foreground">
            Everything that can be upgraded safely is up to date.{" "}
            {plan.kept_back.length > 0 && "Some packages are kept back; see below."}{" "}
            {(plan.phased ?? []).length > 0 && "Some updates are still being rolled out by Ubuntu; see below."}
          </p>
        ) : (
          <>
            <Effects plan={plan} />
            <PackageTable pkgs={plan.upgrades} />
          </>
        )}
        <p className="text-xs text-muted-foreground">Based on the package lists as of the last check. Use “Check for updates” to refresh them.</p>
      </CardContent>
      {confirm && (
        <ConfirmDialog
          title={`Install ${n} update${n === 1 ? "" : "s"}?`}
          plan={plan}
          onClose={() => setConfirm(false)}
          onStarted={(id) => {
            setConfirm(false);
            onStarted(id);
          }}
        />
      )}
    </Card>
  );
}

/** Updates a normal upgrade skips: kept back (they need new packages), or phased (Ubuntu's gradual rollout). */
function KeptBackCard({ packages, phased = false, busy, onStarted }: { packages: string[]; phased?: boolean; busy: boolean; onStarted: (id: string) => void }) {
  const [look, setLook] = useState(false);
  const plan = useUpdatesPlan(packages, look);
  return (
    <Card>
      <CardHeader>
        <CardTitle>
          {phased ? "Phased" : "Kept back"} · {packages.length}
        </CardTitle>
        {!look && (
          <Button variant="outline" size="sm" disabled={busy} onClick={() => setLook(true)}>
            <PackageCheck /> Check what installing them needs
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {phased ? (
          <p className="text-sm text-muted-foreground">
            Ubuntu releases these updates to a growing share of servers over a few days, so a problem can be caught before it
            reaches everyone. This server's turn hasn't come yet, so a normal upgrade skips them; they install by themselves
            once it does: <span className="font-mono text-xs">{packages.join(", ")}</span>. You can also install them now.
          </p>
        ) : (
          <p className="text-sm text-muted-foreground">
            apt holds these back because their new version needs a package that isn't installed yet, so a normal upgrade skips
            them: <span className="font-mono text-xs">{packages.join(", ")}</span>.
          </p>
        )}
        {look &&
          (plan.isPending ? (
            <Spinner />
          ) : plan.error ? (
            <Notice tone="warning">{plan.error.message}</Notice>
          ) : (
            <KeptBackPlan plan={plan.data} packages={packages} phased={phased} busy={busy} onStarted={onStarted} />
          ))}
      </CardContent>
    </Card>
  );
}

function KeptBackPlan({
  plan,
  packages,
  phased,
  busy,
  onStarted,
}: {
  plan: UpgradePlan;
  packages: string[];
  phased: boolean;
  busy: boolean;
  onStarted: (id: string) => void;
}) {
  const [confirm, setConfirm] = useState(false);
  if (plan.removals.length > 0) {
    return (
      <Notice tone="danger">
        Installing these would remove <strong>{plan.removals.join(", ")}</strong>. Layte Panel never removes packages, so it
        won't do this. Look into it on the server if you really need these versions.
      </Notice>
    );
  }
  return (
    <>
      {plan.new_packages.length > 0 && (
        <p className="text-sm">
          Also installs {plan.new_packages.length} new package{plan.new_packages.length === 1 ? "" : "s"} they need:{" "}
          <span className="font-mono text-xs">{plan.new_packages.map((p) => p.name).join(", ")}</span>. Nothing is removed.
        </p>
      )}
      <Effects plan={plan} />
      <PackageTable pkgs={[...plan.upgrades, ...plan.new_packages]} />
      <div>
        <Button disabled={busy} onClick={() => setConfirm(true)}>
          <Download /> {phased ? "Install now anyway" : "Install kept-back packages"}
        </Button>
      </div>
      {confirm && (
        <ConfirmDialog
          title={phased ? "Install the phased updates now?" : "Install the kept-back packages?"}
          plan={plan}
          packages={packages}
          onClose={() => setConfirm(false)}
          onStarted={(id) => {
            setConfirm(false);
            onStarted(id);
          }}
        />
      )}
    </>
  );
}

/** An earlier install didn't finish (e.g. interrupted): finish it first. */
function RepairCard({ message, busy, onStarted }: { message: string; busy: boolean; onStarted: (id: string) => void }) {
  const [look, setLook] = useState(false);
  const plan = useUpdatesPlan([], look, true);
  const [confirm, setConfirm] = useState(false);
  return (
    <Card>
      <CardHeader>
        <CardTitle>An earlier install didn't finish</CardTitle>
        {!look && (
          <Button size="sm" disabled={busy} onClick={() => setLook(true)}>
            <Wrench /> Check the repair
          </Button>
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <Notice tone="warning">
          apt can't plan new updates until the interrupted install is finished.
          <pre className="mt-2 max-h-32 overflow-auto whitespace-pre-wrap font-mono text-xs text-muted-foreground">{message}</pre>
        </Notice>
        <p className="text-sm text-muted-foreground">
          The repair runs <code className="font-mono">dpkg --configure -a</code> and then{" "}
          <code className="font-mono">apt-get --fix-broken install</code>, keeping your config files. You see what it would do
          first, and it's refused if it would remove anything.
        </p>
        {look &&
          (plan.isPending ? (
            <Spinner />
          ) : plan.error ? (
            <Notice tone="warning">{plan.error.message}</Notice>
          ) : plan.data.removals.length > 0 ? (
            <Notice tone="danger">
              The repair would remove <strong>{plan.data.removals.join(", ")}</strong>. Layte Panel never removes packages; fix
              this on the server with <code className="font-mono">sudo apt --fix-broken install</code>.
            </Notice>
          ) : (
            <>
              {plan.data.upgrades.length + plan.data.new_packages.length > 0 && (
                <PackageTable pkgs={[...plan.data.upgrades, ...plan.data.new_packages]} />
              )}
              <Effects plan={plan.data} />
              <div>
                <Button disabled={busy} onClick={() => setConfirm(true)}>
                  <Wrench /> Repair now
                </Button>
              </div>
              {confirm && (
                <ConfirmDialog
                  title="Finish the interrupted install?"
                  plan={plan.data}
                  fix
                  onClose={() => setConfirm(false)}
                  onStarted={(id) => {
                    setConfirm(false);
                    onStarted(id);
                  }}
                />
              )}
            </>
          ))}
      </CardContent>
    </Card>
  );
}

function ConfirmDialog({
  title,
  plan,
  packages,
  fix,
  onClose,
  onStarted,
}: {
  title: string;
  plan: UpgradePlan;
  packages?: string[];
  fix?: boolean;
  onClose: () => void;
  onStarted: (jobId: string) => void;
}) {
  const apply = useApplyUpdates();
  const total = plan.upgrades.length + plan.new_packages.length;
  return (
    <Dialog open onClose={() => !apply.isPending && onClose()} title={title} locked={apply.isPending}>
      <div className="flex flex-col gap-4">
        <ul className="flex list-disc flex-col gap-1 pl-5 text-sm">
          <li>
            {total} package{total === 1 ? "" : "s"} upgraded{plan.new_packages.length > 0 && ` (${plan.new_packages.length} new)`}; nothing removed.
          </li>
          <li>Your existing config files are kept, even if a package ships a newer one.</li>
          <li>Every site is checked afterwards, and you'll see if a reboot is needed.</li>
        </ul>
        <Effects plan={plan} />
        <FormError message={apply.error?.message} />
        <div className="flex justify-end gap-2">
          <Button type="button" variant="outline" disabled={apply.isPending} onClick={onClose}>
            Cancel
          </Button>
          <Button disabled={apply.isPending} onClick={() => apply.mutate({ plan_hash: plan.hash, packages, fix }, { onSuccess: (r) => onStarted(r.job_id) })}>
            <Download /> Install now
          </Button>
        </div>
      </div>
    </Dialog>
  );
}
