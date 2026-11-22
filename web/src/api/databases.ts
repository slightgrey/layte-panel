import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { DatabasesResponse, JobStarted, SecretsResponse } from "./types";

export const databasesKey = ["databases"] as const;

/** GET /databases */
export function useDatabases() {
  return useQuery({ queryKey: databasesKey, queryFn: () => api<DatabasesResponse>("GET", "/databases") });
}

/** POST /databases: returns the password once. */
export function useCreateDatabase() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; site_id?: number }) => api<SecretsResponse>("POST", "/databases", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: databasesKey }),
  });
}

/** DELETE /databases/{name} */
export function useDropDatabase() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; confirm: string }) => api<void>("DELETE", `/databases/${v.name}`, { confirm: v.confirm }),
    onSuccess: () => qc.invalidateQueries({ queryKey: databasesKey }),
  });
}

/** POST /databases/{name}/password: returns the new password once. */
export function useResetDBPassword() {
  return useMutation({ mutationFn: (name: string) => api<SecretsResponse>("POST", `/databases/${name}/password`) });
}

/** POST /databases/{name}/export: dump to the site's tmp/ folder (job). */
export function useExportDatabase() {
  return useMutation({ mutationFn: (name: string) => api<JobStarted>("POST", `/databases/${name}/export`) });
}

/** POST /databases/{name}/import: run tmp/<file>.sql into the database (job). */
export function useImportDatabase() {
  return useMutation({
    mutationFn: (v: { name: string; path: string; confirm: string }) =>
      api<JobStarted>("POST", `/databases/${v.name}/import`, { path: v.path, confirm: v.confirm }),
  });
}
