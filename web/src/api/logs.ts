import { useQuery } from "@tanstack/react-query";
import { useEffect, useRef, useState } from "react";
import { api } from "./client";
import type { LogSourcesResponse } from "./types";

/** GET /logs: log files that exist and may be viewed. */
export function useLogSources() {
  return useQuery({ queryKey: ["logs", "sources"], queryFn: () => api<LogSourcesResponse>("GET", "/logs") });
}

const maxLines = 5000;

/**
 * GET /logs/stream (SSE): last lines of a log, then new lines live.
 * While paused, incoming lines are buffered and shown on resume.
 */
export function useLogStream(path: string | null, paused: boolean) {
  const [lines, setLines] = useState<string[]>([]);
  const [status, setStatus] = useState<"idle" | "connecting" | "live" | "error">("idle");
  const [error, setError] = useState<string | null>(null);
  const buffer = useRef<string[]>([]);
  const pausedRef = useRef(paused);
  pausedRef.current = paused;

  const push = (data: string, replace = false) => {
    const incoming = data.split("\n").filter((l, i, a) => l !== "" || i < a.length - 1);
    if (replace) {
      buffer.current = [];
      setLines(incoming.slice(-maxLines));
      return;
    }
    if (pausedRef.current) {
      buffer.current.push(...incoming);
      return;
    }
    setLines((prev) => [...prev, ...incoming].slice(-maxLines));
  };

  // Flush what arrived while paused.
  useEffect(() => {
    if (!paused && buffer.current.length) {
      const b = buffer.current;
      buffer.current = [];
      setLines((prev) => [...prev, ...b].slice(-maxLines));
    }
  }, [paused]);

  useEffect(() => {
    if (!path) return;
    setLines([]);
    setError(null);
    setStatus("connecting");
    const es = new EventSource(`/api/v1/logs/stream?path=${encodeURIComponent(path)}&lines=300`);
    es.addEventListener("tail", (e) => {
      push(JSON.parse((e as MessageEvent<string>).data).data, true);
      setStatus("live");
    });
    es.addEventListener("append", (e) => {
      const msg = JSON.parse((e as MessageEvent<string>).data) as { data: string; rotated: boolean };
      if (msg.rotated) push("--- log rotated ---\n");
      push(msg.data);
    });
    es.addEventListener("error", (e) => {
      const data = (e as MessageEvent<string>).data;
      if (data) setError(JSON.parse(data).message);
      setStatus("error");
    });
    return () => es.close();
  }, [path]); // push only touches refs and state setters, so it isn't a dependency

  return { lines, status, error, clear: () => setLines([]) };
}
