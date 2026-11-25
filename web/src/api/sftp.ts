import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "./client";

/** A site's SFTP login. Passwords are never stored; `password` says whether password logins are allowed. */
export interface SFTPState {
  enabled: boolean;
  password: boolean;
  keys: { key: string; fingerprint: string }[];
  username: string;
  folder: string; // what the login sees, e.g. /example.com
  warnings?: string[];
}

export interface SFTPRequest {
  enabled: boolean;
  password: boolean;
  keys: string[];
}

const key = (siteId: number) => ["sites", siteId, "sftp"] as const;

/** GET /sites/{id}/sftp */
export function useSFTP(siteId: number) {
  return useQuery({ queryKey: key(siteId), queryFn: () => api<SFTPState>("GET", `/sites/${siteId}/sftp`) });
}

/** PUT /sites/{id}/sftp: on/off, keys, password option. */
export function useSetSFTP(siteId: number) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: SFTPRequest) => api<SFTPState>("PUT", `/sites/${siteId}/sftp`, v),
    onSuccess: (data) => qc.setQueryData(key(siteId), data),
  });
}

/** POST /sites/{id}/sftp/password: a new password, shown once. */
export function useNewSFTPPassword(siteId: number) {
  return useMutation({ mutationFn: () => api<{ password: string }>("POST", `/sites/${siteId}/sftp/password`) });
}
