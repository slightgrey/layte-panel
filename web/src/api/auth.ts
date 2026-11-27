import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, setCsrfToken } from "./client";
import type { LoginResponse, MeResponse, TOTPSetupResponse } from "./types";

export const meKey = ["auth", "me"] as const;

/** GET /auth/me: the logged-in user (and the CSRF token). */
export function useMe() {
  return useQuery({
    queryKey: meKey,
    queryFn: () => api<MeResponse>("GET", "/auth/me"),
    retry: false,
    staleTime: 60_000,
  });
}

/** POST /auth/login: step 1, password. */
export function useLogin() {
  return useMutation({
    mutationFn: (v: { username: string; password: string }) => api<LoginResponse>("POST", "/auth/login", v),
  });
}

/** POST /auth/totp: step 2, the authenticator code. remember = stay signed in on this browser for 30 days. */
export function useTotp() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { code: string; remember: boolean }) => api<MeResponse>("POST", "/auth/totp", v),
    onSuccess: (me) => qc.setQueryData(meKey, me),
  });
}

/** POST /auth/totp/setup: step 2 without 2FA yet, a new secret for the authenticator app. */
export function useTotpSetup() {
  return useMutation({ mutationFn: () => api<TOTPSetupResponse>("POST", "/auth/totp/setup") });
}

/** POST /auth/totp/setup/confirm: a code from the app saves the secret and finishes the login. */
export function useTotpSetupConfirm() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { code: string; remember: boolean }) => api<MeResponse>("POST", "/auth/totp/setup/confirm", v),
    onSuccess: (me) => qc.setQueryData(meKey, me),
  });
}

/** The logged-in user is an admin (site accounts only see their own sites). */
export function useIsAdmin() {
  return useMe().data?.user.role === "admin";
}

/** POST /auth/logout */
export function useLogout() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api<void>("POST", "/auth/logout"),
    onSettled: () => {
      setCsrfToken(null);
      qc.clear();
    },
  });
}
