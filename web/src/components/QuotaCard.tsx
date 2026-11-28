import { useQueryClient } from "@tanstack/react-query";
import { HardDrive, LoaderCircle } from "lucide-react";
import { useEffect, useRef } from "react";
import { useDisableQuotas, useEnableQuotas, useQuotaStatus } from "@/api/quota";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { FormError } from "@/pages/AuthShell";

/** Admins: turn Linux disk quotas on or off, so site logins can get a disk limit. */
export function QuotaCard() {
  const st = useQuotaStatus();
  const enable = useEnableQuotas();
  const disable = useDisableQuotas();
  const qc = useQueryClient();
  // The server's state, so it survives leaving the page (or a reload).
  const busy = st.data?.busy ?? (enable.isPending ? "enabling" : disable.isPending ? "disabling" : undefined);

  // When a turn on/off finishes, logins' disk limits may have changed.
  const wasBusy = useRef(busy);
  useEffect(() => {
    if (wasBusy.current && !busy) {
      qc.invalidateQueries({ queryKey: ["users"] });
      qc.invalidateQueries({ queryKey: ["usage"] });
    }
    wasBusy.current = busy;
  }, [busy, qc]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <HardDrive className="size-4" /> Disk quotas
        </CardTitle>
        {busy ? (
          <Badge variant="warning">{busy === "enabling" ? "turning on" : "turning off"}</Badge>
        ) : (
          st.data && (st.data.enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>)
        )}
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {busy ? (
          <Notice>
            <span className="flex items-center gap-2">
              <LoaderCircle className="size-4 animate-spin" />
              <span>
                <strong>{busy === "enabling" ? "Turning disk quotas on…" : "Turning disk quotas off…"}</strong>{" "}
                {busy === "enabling"
                  ? "This runs on the server and can take a few minutes (installing packages, building the quota file)."
                  : "This runs on the server and takes a moment."}{" "}
                You can leave this page; it keeps going and this card shows when it's done.
              </span>
            </span>
          </Notice>
        ) : st.isPending ? (
          <Spinner />
        ) : st.error ? (
          <p className="text-destructive">{st.error.message}</p>
        ) : st.data.enabled ? (
          <>
            <p className="text-muted-foreground">
              On for <code className="font-mono">{st.data.mount}</code> ({st.data.fs_type}). A site login's disk limit is a hard limit: the server stops
              its writes past it (PHP, SFTP, cron and uploads alike).
            </p>
            <Button
              variant="outline"
              size="sm"
              className="self-start text-destructive"
              onClick={() =>
                confirm(
                  "Turn off disk quotas? Every site login's disk limit goes back to none, and usrquota is taken out of /etc/fstab (the file is checked before it's used). You can turn them on again later.",
                ) && disable.mutate()
              }
            >
              Turn off disk quotas
            </Button>
          </>
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
              onClick={() => confirm(`Turn on disk quotas for ${st.data.mount}? This edits /etc/fstab (a copy of the original is kept) and remounts the filesystem.`) && enable.mutate()}
            >
              Turn on disk quotas
            </Button>
          </>
        )}
        {!busy && st.data?.last_error && <FormError message={`The last attempt failed: ${st.data.last_error}`} />}
        <FormError message={enable.error?.message ?? disable.error?.message} />
      </CardContent>
    </Card>
  );
}
