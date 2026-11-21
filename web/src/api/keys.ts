import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { KeysResponse } from "./types";

export const keysKey = ["keys"] as const;

/** GET /keys */
export function useKeys() {
  return useQuery({ queryKey: keysKey, queryFn: () => api<KeysResponse>("GET", "/keys") });
}

/** POST /keys: returns the token once. */
export function useCreateKey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { name: string; scopes: string[]; expires_in_days: number }) =>
      api<{ id: number; token: string; prefix: string }>("POST", "/keys", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: keysKey }),
  });
}

/** DELETE /keys/{id} */
export function useRevokeKey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api<void>("DELETE", `/keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: keysKey }),
  });
}
