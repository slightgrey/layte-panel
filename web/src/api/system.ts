import { useQuery } from "@tanstack/react-query";
import { api } from "./client";
import type { DiskBreakdown, ProcessesResult } from "./types";

/** GET /system/processes: CPU and memory per process and group. Refreshed every 5 s while open. */
export function useProcesses() {
  return useQuery({
    queryKey: ["system", "processes"],
    queryFn: () => api<ProcessesResult>("GET", "/system/processes"),
    refetchInterval: 5_000,
  });
}

/**
 * GET /system/disk?path=&mode=: a folder's subfolders by size, or (mode
 * "files") its largest files. Scanning is slow, so results are kept for 10
 * minutes and never refetched in the background.
 */
export function useDiskBreakdown(path: string, mode: "" | "files", enabled = true) {
  return useQuery({
    queryKey: ["system", "disk", path, mode],
    queryFn: () => api<DiskBreakdown>("GET", `/system/disk?path=${encodeURIComponent(path)}${mode ? `&mode=${mode}` : ""}`),
    enabled,
    staleTime: 10 * 60_000,
    refetchOnWindowFocus: false,
    retry: false,
  });
}
