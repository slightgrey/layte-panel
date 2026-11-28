import { LoaderCircle, Power } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useCancelReboot, useReboot, useRebootStatus } from "@/api/services";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/pages/AuthShell";

/** Seconds until t (0 once passed), ticking every second. */
function useCountdown(t?: string) {
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    if (!t) return;
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, [t]);
  return t ? Math.max(0, Math.round((new Date(t).getTime() - now) / 1000)) : 0;
}

/** On the dashboard's System card: reboot needed?, Reboot (with the hostname typed), countdown + Cancel. */
export function RebootControl() {
  const st = useRebootStatus();
  const reboot = useReboot();
  const cancel = useCancelReboot();
  const [open, setOpen] = useState(false);
  const [confirmName, setConfirmName] = useState("");
  const left = useCountdown(st.data?.scheduled);
  if (!st.data) return null;
  const s = st.data;

  if (s.scheduled && left === 0) return <RebootingOverlay />;

  return (
    <div className="mt-1 flex flex-wrap items-center gap-2">
      {s.required && (
        <Badge variant="warning" title={s.packages?.length ? `Updated: ${s.packages.join(", ")}` : undefined}>
          reboot needed
        </Badge>
      )}
      {s.scheduled ? (
        <>
          <span className="text-xs font-medium text-warning" role="status">
            Rebooting in {left} s…
          </span>
          <Button variant="outline" size="sm" disabled={cancel.isPending} onClick={() => cancel.mutate()}>
            Cancel
          </Button>
        </>
      ) : (
        <Button variant="outline" size="sm" className="ml-auto" onClick={() => setOpen(true)}>
          <Power /> Reboot
        </Button>
      )}
      <FormError message={cancel.error?.message} />
      <Dialog open={open} onClose={() => (setOpen(false), setConfirmName(""), reboot.reset())} title="Reboot the server">
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            reboot.mutate(confirmName, { onSuccess: () => (setOpen(false), setConfirmName("")) });
          }}
        >
          <p className="text-sm text-muted-foreground">
            Every site is down while the server restarts, usually a minute or two. It starts in one minute, and you can still cancel it until then.
            {s.required && " Updates are waiting for it (new kernel or system libraries)."}
          </p>
          <Field label={`Type ${s.hostname} to confirm`} htmlFor="reboot-confirm">
            <Input id="reboot-confirm" autoComplete="off" spellCheck={false} value={confirmName} onChange={(e) => setConfirmName(e.target.value)} />
          </Field>
          <FormError message={reboot.error?.message} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={() => setOpen(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              className="bg-destructive hover:bg-destructive/90"
              disabled={reboot.isPending || confirmName.trim().toLowerCase() !== s.hostname.toLowerCase()}
            >
              {reboot.isPending ? "Scheduling…" : "Reboot in 1 minute"}
            </Button>
          </div>
        </form>
      </Dialog>
    </div>
  );
}

/** Full screen while the server reboots: waits for it to go down and come back, then reloads the panel. */
function RebootingOverlay() {
  const [down, setDown] = useState(false);
  const [elapsed, setElapsed] = useState(0);
  useEffect(() => {
    let sawDown = false;
    const start = Date.now();
    const id = setInterval(async () => {
      setElapsed(Math.round((Date.now() - start) / 1000));
      let up = false;
      try {
        up = (await fetch("/api/v1/health", { cache: "no-store" })).ok;
      } catch {
        up = false;
      }
      if (!up) {
        sawDown = true;
        setDown(true);
      } else if (sawDown || Date.now() - start > 5 * 60_000) {
        window.location.reload(); // back up (or it never went down): fresh page, fresh login check
      }
    }, 3000);
    return () => clearInterval(id);
  }, []);
  return (
    <div className="fixed inset-0 z-50 grid place-items-center bg-background/95 backdrop-blur" role="alertdialog" aria-live="polite">
      <div className="flex max-w-sm flex-col items-center gap-3 text-center">
        <LoaderCircle className="size-8 animate-spin text-primary" />
        <h2 className="text-xl font-bold">{down ? "Rebooting…" : "Reboot starting…"}</h2>
        <p className="text-sm text-muted-foreground">
          {down ? "The server is restarting. This page reconnects by itself when it's back." : "Waiting for the server to go down."} ({elapsed} s)
        </p>
      </div>
    </div>
  );
}
