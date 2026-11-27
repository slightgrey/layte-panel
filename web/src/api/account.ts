import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { meKey } from "./auth";
import { api } from "./client";
import type { AccountResponse, MeResponse, TOTPSetupResponse, User } from "./types";

export const accountKey = ["account"] as const;

/** GET /account: your profile, 2FA state and signed-in sessions. */
export function useAccount() {
  return useQuery({ queryKey: accountKey, queryFn: () => api<AccountResponse>("GET", "/account") });
}

/** PATCH /account: display name and email. */
export function useUpdateAccount() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { display_name: string; email: string }) => api<{ user: User }>("PATCH", "/account", v),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: accountKey });
      qc.setQueryData<MeResponse>(meKey, (me) => (me ? { ...me, user: r.user } : me));
    },
  });
}

/** POST /account/password: other sessions are signed out. */
export function useChangePassword() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { current_password: string; new_password: string }) => api<void>("POST", "/account/password", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: accountKey }),
  });
}

/** POST /account/2fa: a new secret for a new authenticator app. */
export function useStart2FAChange() {
  return useMutation({ mutationFn: (v: { current_password: string }) => api<TOTPSetupResponse>("POST", "/account/2fa", v) });
}

/** POST /account/2fa/confirm: a code from the new app replaces the old secret. */
export function useConfirm2FAChange() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { code: string }) => api<void>("POST", "/account/2fa/confirm", v),
    onSuccess: () => qc.invalidateQueries({ queryKey: accountKey }),
  });
}

/** DELETE /account/sessions[/{id}]: sign out one other session, or all of them. */
export function useSignOutSessions() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id?: string) => api<unknown>("DELETE", id ? `/account/sessions/${id}` : "/account/sessions"),
    onSuccess: () => qc.invalidateQueries({ queryKey: accountKey }),
  });
}
