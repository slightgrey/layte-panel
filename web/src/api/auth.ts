import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, setCsrfToken } from "./client";
import type { LoginResponse, MeResponse } from "./types";

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
