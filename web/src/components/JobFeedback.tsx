import { useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, LoaderCircle, X, XCircle } from "lucide-react";
import { createContext, type ReactNode, useCallback, useContext, useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router";
import { api } from "@/api/client";
import type { JobInfo } from "@/api/types";
import { cn } from "@/lib/utils";

/**
 * Feedback for background jobs started from a button: the button shows
 * "Working…" then "Done"/"Failed", and a flash message at the bottom of the
 * screen follows the job, so nobody has to scroll up to the progress card.
 */

type Status = "running" | "succeeded" | "failed";

interface Tracked {
  id: string;
  key: string; // which button started it
  label: string; // e.g. "Turning on the object cache"
  status: Status;
  error?: string;
  finishedAt?: number;
}

interface Ctx {
  jobs: Tracked[];
  track: (id: string, key: string, label: string) => void;
  dismiss: (id: string) => void;
}

const JobFeedbackContext = createContext<Ctx | null>(null);

const POLL_MS = 1500;
const DONE_VISIBLE_MS = 6000; // flash message after success
const BUTTON_RESULT_MS = 4000; // "Done"/"Failed" on the button

export function JobFeedbackProvider({ children }: { children: ReactNode }) {
  const [jobs, setJobs] = useState<Tracked[]>([]);
  const qc = useQueryClient();
  const jobsRef = useRef(jobs);
  jobsRef.current = jobs;

  const track = useCallback((id: string, key: string, label: string) => {
    setJobs((prev) => [...prev.filter((j) => j.id !== id), { id, key, label, status: "running" }]);
  }, []);
  const dismiss = useCallback((id: string) => setJobs((prev) => prev.filter((j) => j.id !== id)), []);

  // Poll running jobs; drop finished successes after a while.
  useEffect(() => {
    const t = setInterval(async () => {
      const now = Date.now();
      for (const j of jobsRef.current) {
        if (j.status !== "running") continue;
        try {
          const info = await api<JobInfo>("GET", `/jobs/${j.id}`);
          if (info.status !== "running") {
            setJobs((prev) => prev.map((p) => (p.id === j.id ? { ...p, status: info.status, error: info.error, finishedAt: Date.now() } : p)));
            qc.invalidateQueries({ queryKey: ["sites"] }); // the site's state changed
          }
        } catch {
          // offline for a moment: try again next round
        }
      }
      setJobs((prev) => prev.filter((p) => !(p.status === "succeeded" && p.finishedAt && now - p.finishedAt > DONE_VISIBLE_MS)));
    }, POLL_MS);
    return () => clearInterval(t);
  }, [qc]);

  return (
    <JobFeedbackContext.Provider value={{ jobs, track, dismiss }}>
      {children}
      <div aria-live="polite" className="pointer-events-none fixed right-4 bottom-4 z-50 flex w-[min(24rem,calc(100vw-2rem))] flex-col gap-2">
        {jobs.map((j) => (
          <Flash key={j.id} job={j} onClose={() => dismiss(j.id)} />
        ))}
      </div>
    </JobFeedbackContext.Provider>
  );
}

function Flash({ job, onClose }: { job: Tracked; onClose: () => void }) {
  const Icon = job.status === "running" ? LoaderCircle : job.status === "succeeded" ? CheckCircle2 : XCircle;
  return (
    <div
      role="status"
      className={cn(
        "pointer-events-auto flex items-start gap-3 rounded-[14px] bg-card p-3 text-sm shadow-raised",
        job.status === "failed" && "ring-1 ring-destructive/40",
      )}
    >
      <Icon
        className={cn(
          "mt-0.5 size-4 shrink-0",
          job.status === "running" && "animate-spin text-primary",
          job.status === "succeeded" && "text-success",
          job.status === "failed" && "text-destructive",
        )}
      />
      <div className="min-w-0 flex-1">
        <p className="font-medium">
          {job.label}
          {job.status === "running" ? "…" : job.status === "succeeded" ? ": done" : ": failed"}
        </p>
        {job.error && <p className="mt-0.5 break-words text-xs text-muted-foreground">{job.error}</p>}
        <button
          type="button"
          className="mt-1 text-xs text-primary hover:underline"
          onClick={() => window.scrollTo({ top: 0, behavior: "smooth" })}
        >
          View progress
        </button>
      </div>
      <button type="button" aria-label="Close" className="text-muted-foreground hover:text-foreground" onClick={onClose}>
        <X className="size-4" />
      </button>
    </div>
  );
}

export type JobButtonState = "idle" | "working" | "done" | "failed";

/**
 * For a button that starts a job: start(r) shows the job's progress (as
 * before) and follows it; state drives the button's label.
 */
export function useJobFeedback(key: string, label: string) {
  const ctx = useContext(JobFeedbackContext);
  const [, setParams] = useSearchParams();
  const [, rerender] = useState(0);
  const job = ctx?.jobs.filter((j) => j.key === key).at(-1);

  // Re-render when "Done"/"Failed" should turn back into the normal label.
  useEffect(() => {
    if (!job?.finishedAt) return;
    const left = BUTTON_RESULT_MS - (Date.now() - job.finishedAt);
    if (left <= 0) return;
    const t = setTimeout(() => rerender((n) => n + 1), left);
    return () => clearTimeout(t);
  }, [job?.finishedAt]);

  let state: JobButtonState = "idle";
  if (job?.status === "running") state = "working";
  else if (job?.finishedAt && Date.now() - job.finishedAt < BUTTON_RESULT_MS) state = job.status === "succeeded" ? "done" : "failed";

  const start = useCallback(
    (r: { job_id: string }) => {
      setParams({ job: r.job_id }, { replace: true });
      ctx?.track(r.job_id, key, label);
    },
    [ctx, key, label, setParams],
  );
  return { start, state, working: state === "working" };
}

/** The button's content: its normal label, or Working… / Done / Failed. */
export function JobButtonLabel({ state, children }: { state: JobButtonState; children: ReactNode }) {
  switch (state) {
    case "working":
      return (
        <>
          <LoaderCircle className="animate-spin" /> Working…
        </>
      );
    case "done":
      return (
        <>
          <CheckCircle2 /> Done
        </>
      );
    case "failed":
      return (
        <>
          <XCircle /> Failed
        </>
      );
  }
  return <>{children}</>;
}
