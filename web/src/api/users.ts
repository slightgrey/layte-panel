import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";
import type { Account, UsersResponse } from "./types";

type LinkResponse = { setup_url: string; expires_at: string };

/** A site login's limits as the API takes them (0 = none). */
export type AccountLimitsBody = { max_subdomains: number; cpu_percent: number; memory_mb: number; disk_mb: number };

export const usersKey = ["users"] as const;

/** GET /users: every panel login (admins only). */
export function useUsers() {
  return useQuery({ queryKey: usersKey, queryFn: () => api<UsersResponse>("GET", "/users") });
}

/** The site account of a main site, if it has one. */
export function useSiteAccount(siteId: number) {
  const users = useUsers();
  return { ...users, account: users.data?.users.find((u) => u.site_id === siteId) };
}

/** POST /users: a login for a site; returns its one-time setup link once. */
export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { username: string; site_id: number } & Partial<AccountLimitsBody>) =>
      api<{ user: Account } & LinkResponse>("POST", "/users", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

/** PATCH /users/{id}: change the subdomain limit. */
export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: number } & Partial<AccountLimitsBody>) => api<Account>("PATCH", `/users/${id}`, body),
    // Waits for the fresh list, so "Saved" shows with the new values in place.
    onSuccess: (_, v) => Promise.all([qc.invalidateQueries({ queryKey: usersKey }), qc.invalidateQueries({ queryKey: ["usage", v.id] })]),
  });
}

/** POST /users/{id}/password: a password reset link, shown once. */
export function useResetUserPassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api<LinkResponse>("POST", `/users/${id}/password`),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

/** POST /users/{id}/reset-2fa: 2FA is set up again at the next login. */
export function useResetUser2FA() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api<void>("POST", `/users/${id}/reset-2fa`),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

/** DELETE /users/{id}: removes the login; the sites stay. */
export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: number) => api<void>("DELETE", `/users/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}

/** PUT /users/{id}/password (admins): set a site login's password; it's signed out everywhere. */
export function useSetUserPassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: number; password: string }) => api<void>("PUT", `/users/${v.id}/password`, { password: v.password }),
    onSuccess: () => qc.invalidateQueries({ queryKey: usersKey }),
  });
}
