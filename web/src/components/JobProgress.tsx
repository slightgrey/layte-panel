import { CheckCircle2, Copy, KeyRound, LoaderCircle, XCircle } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { useJobEvents, useJobSecret } from "@/api/jobs";
import type { JobEvent } from "@/api/types";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface Step {
  name: string;
  status: "running" | "done" | "failed";
}

/** Steps in order of first appearance, each with its latest status. */
function stepsOf(events: JobEvent[]): Step[] {
  const map = new Map<string, Step>();
  for (const e of events) {
    if (e.type === "step" && e.step) {
      map.set(e.step, { name: e.step, status: (e.status as Step["status"]) ?? "running" });
    }
  }
  return [...map.values()];
}

/** Live progress of a job, streamed over SSE. Calls onDone once it finishes. */
export function JobProgress({ jobId, onDone }: { jobId: string; onDone?: (ok: boolean) => void }) {
  const { events, done, connError } = useJobEvents(jobId);
  const steps = useMemo(() => stepsOf(events), [events]);
  const logs = events.filter((e) => e.type === "log");

  // Keep the latest callback in a ref so the effect only fires when `done` arrives.
  const onDoneRef = useRef(onDone);
  onDoneRef.current = onDone;
  useEffect(() => {
    if (done) onDoneRef.current?.(done.status === "succeeded");
  }, [done]);

  return (
    <div className="flex flex-col gap-4">
      <ol className="flex flex-col gap-2" aria-label="Progress">
        {steps.length === 0 && !done && (
          <li className="flex items-center gap-2 text-sm text-muted-foreground">
            <LoaderCircle className="size-4 animate-spin" /> Starting…
          </li>
        )}
        {steps.map((s) => (
          <li key={s.name} className="flex items-center gap-2 text-sm">
            {s.status === "done" && <CheckCircle2 className="size-4 text-success" />}
            {s.status === "running" && <LoaderCircle className="size-4 animate-spin text-primary" />}
            {s.status === "failed" && <XCircle className="size-4 text-destructive" />}
            <span className={cn(s.status === "running" && "font-medium")}>{s.name}</span>
          </li>
        ))}
      </ol>

      {logs.length > 0 && (
        <pre className="max-h-40 overflow-auto rounded-[12px] bg-background p-3 font-mono text-xs leading-relaxed">
          {logs.map((l) => l.message).join("\n")}
        </pre>
      )}

      {done?.status === "failed" && (
        <p role="alert" className="rounded-[12px] bg-destructive/10 px-3 py-2 text-sm text-destructive">
          {done.message}
        </p>
      )}
      {connError && !done && <p className="text-xs text-muted-foreground">Connection lost, reconnecting…</p>}
      {done?.status === "succeeded" && <Credentials jobId={jobId} />}
    </div>
  );
}

const secretLabels: Record<string, string> = {
  db_name: "Database",
  db_user: "Database user",
  db_password: "Database password",
  wp_admin_url: "WordPress admin",
  wp_admin_user: "WordPress username",
  wp_admin_password: "WordPress password",
  api_key: "API key",
};

/** One-time credentials of a finished job (if it generated any). */
function Credentials({ jobId }: { jobId: string }) {
  const secret = useJobSecret(jobId, true);
  if (secret.isPending || secret.error) return null; // 404 = none, or already shown
  return <CredentialsPanel secrets={secret.data.secrets} />;
}

/** One-time credentials with copy buttons and a "shown once" warning. */
export function CredentialsPanel({ secrets }: { secrets: Record<string, string> }) {
  const entries = Object.entries(secrets);
  if (entries.length === 0) return null;
  return (
    <div className="rounded-[14px] border-l-4 border-warning bg-warning/15 p-4 shadow-raised-sm">
      <div className="mb-3 flex items-center gap-2 text-sm font-medium">
        <KeyRound className="size-4 text-warning" /> Save these credentials now. They are shown only once.
      </div>
      <dl className="flex flex-col gap-2">
        {entries.map(([k, v]) => (
          <SecretRow key={k} label={secretLabels[k] ?? k} value={v} />
        ))}
      </dl>
    </div>
  );
}

/** CredentialsPanel plus a dismiss button; once dismissed the secrets are gone for good. */
export function OneTimeSecrets({ secrets, onDismiss }: { secrets: Record<string, string>; onDismiss: () => void }) {
  return (
    <div className="flex flex-col gap-2">
      <CredentialsPanel secrets={secrets} />
      <Button variant="ghost" size="sm" className="self-end" onClick={onDismiss}>
        I've saved them
      </Button>
    </div>
  );
}

function SecretRow({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);
  const copy = async () => {
    await navigator.clipboard.writeText(value);
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  };
  return (
    <div className="grid grid-cols-[9rem_1fr_auto] items-center gap-2">
      <dt className="text-xs text-muted-foreground">{label}</dt>
      <dd className="truncate font-mono text-xs" title={value}>
        {value}
      </dd>
      <Button variant="ghost" size="sm" onClick={copy} aria-label={`Copy ${label}`}>
        {copied ? <CheckCircle2 /> : <Copy />}
      </Button>
    </div>
  );
}
