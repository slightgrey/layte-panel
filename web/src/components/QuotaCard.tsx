import { HardDrive } from "lucide-react";
import { useEnableQuotas, useQuotaStatus } from "@/api/quota";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { FormError } from "@/pages/AuthShell";

/** Admins: turn on Linux disk quotas, so site logins can get a disk limit. */
export function QuotaCard() {
  const st = useQuotaStatus();
  const enable = useEnableQuotas();
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <HardDrive className="size-4" /> Disk quotas
        </CardTitle>
        {st.data && (st.data.enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>)}
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {st.isPending ? (
          <Spinner />
        ) : st.error ? (
          <p className="text-destructive">{st.error.message}</p>
        ) : st.data.enabled ? (
          <p className="text-muted-foreground">
            On for <code className="font-mono">{st.data.mount}</code> ({st.data.fs_type}). A site login's disk limit is a hard limit: the server stops
            its writes past it (PHP, SFTP, cron and uploads alike).
          </p>
        ) : !st.data.supported ? (
          <p className="text-muted-foreground">Disk quotas can't be turned on here: {st.data.reason}.</p>
        ) : (
          <>
            <p className="text-muted-foreground">
              Turn on Linux user quotas for <code className="font-mono">{st.data.mount}</code> ({st.data.fs_type}) to give site logins a disk limit. This
              {st.data.tools ? "" : " installs the quota package,"}
              {!st.data.kernel && st.data.kernel_packages?.length ? (
                <>
                  {" "}installs the kernel's quota module (<code className="font-mono">{st.data.kernel_packages.join(", ")}</code>, about 110 MB: Ubuntu's cloud kernels leave it out),
                </>
              ) : null}{" "}
              adds <code className="font-mono">usrquota</code> to that filesystem's line in{" "}
              <code className="font-mono">/etc/fstab</code> (the original is kept as <code className="font-mono">/etc/fstab.layte-orig</code>, and the new file is
              checked before it's used), remounts it and builds the quota file. Sites keep running. It can take a few minutes on a large disk.
            </p>
            <Button
              variant="outline"
              className="self-start"
              disabled={enable.isPending}
              onClick={() => confirm(`Turn on disk quotas for ${st.data.mount}? This edits /etc/fstab (a copy of the original is kept) and remounts the filesystem.`) && enable.mutate()}
            >
              {enable.isPending ? "Turning on… (this can take a few minutes)" : "Turn on disk quotas"}
            </Button>
          </>
        )}
        <FormError message={enable.error?.message} />
      </CardContent>
    </Card>
  );
}
