import { useQuery } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { api } from "./client";
import type { JobEvent, JobInfo } from "./types";

/** GET /jobs/{id} */
export function useJob(id: string | null) {
  return useQuery({
    queryKey: ["jobs", id],
    queryFn: () => api<JobInfo>("GET", `/jobs/${id}`),
    enabled: Boolean(id),
  });
}

/**
 * GET /jobs/{id}/events (Server-Sent Events): the job's events, live.
 * EventSource reconnects by itself and sends Last-Event-ID, so nothing is
 * missed; the stream is closed once the "done" event arrives.
 */
export function useJobEvents(id: string | null) {
  const [events, setEvents] = useState<JobEvent[]>([]);
  const [connError, setConnError] = useState(false);

  useEffect(() => {
    if (!id) return;
    setEvents([]);
    const es = new EventSource(`/api/v1/jobs/${id}/events`);
    const onEvent = (msg: MessageEvent<string>) => {
      const e = JSON.parse(msg.data) as JobEvent;
      setEvents((prev) => (prev.some((p) => p.seq === e.seq) ? prev : [...prev, e]));
      setConnError(false);
      if (e.type === "done") es.close();
    };
    for (const type of ["step", "log", "done"]) es.addEventListener(type, onEvent as EventListener);
    es.onerror = () => setConnError(true);
    return () => es.close();
  }, [id]);

  const done = events.find((e) => e.type === "done");
  return { events, done, connError };
}

/** GET /jobs/{id}/secret: one-time credentials. Cached forever; never refetched. */
export function useJobSecret(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ["jobs", id, "secret"],
    queryFn: () => api<{ secrets: Record<string, string> }>("GET", `/jobs/${id}/secret`),
    enabled,
    retry: false,
    staleTime: Infinity,
    gcTime: Infinity,
    refetchOnWindowFocus: false,
  });
}
