import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, ApiError, getCsrfToken } from "./client";
import type { FileContent, FileList } from "./types";

/**
 * Area "data" is a site's non-public folder (tmp/, logs/): the site folder
 * for Layte sites, the site user's home for adopted ones. Default: web files.
 */
export type FileArea = "data" | undefined;

const base = (siteId: number) => `/sites/${siteId}/files`;
const q = (path: string, area?: FileArea) => `?path=${encodeURIComponent(path)}${area ? `&area=${area}` : ""}`;

/** GET /sites/{id}/files?path=&area= */
export function useFiles(siteId: number, path: string, area?: FileArea) {
  return useQuery({
    queryKey: ["files", siteId, path, area ?? ""],
    queryFn: () => api<FileList>("GET", base(siteId) + q(path, area)),
    retry: false,
  });
}

/** GET /sites/{id}/files/content?path= (text files up to 2 MB). */
export function useFileContent(siteId: number, path: string | null) {
  return useQuery({
    queryKey: ["files", siteId, "content", path],
    queryFn: () => api<FileContent>("GET", base(siteId) + "/content" + q(path!)),
    enabled: Boolean(path),
    retry: false,
    gcTime: 0,
  });
}

export function downloadURL(siteId: number, path: string, area?: FileArea) {
  return `/api/v1${base(siteId)}/download${q(path, area)}`;
}

/** Mutations that change the folder listing. */
export function useFileActions(siteId: number, area?: FileArea) {
  const qc = useQueryClient();
  const refresh = () => qc.invalidateQueries({ queryKey: ["files", siteId] });
  const areaQ = area ? `?area=${area}` : "";
  const op = (name: "mkdir" | "rename" | "delete") =>
    useMutation({ mutationFn: (v: { path: string; to?: string }) => api<void>("POST", `${base(siteId)}/${name}${areaQ}`, v), onSuccess: refresh });
  return {
    mkdir: op("mkdir"),
    rename: op("rename"),
    remove: op("delete"),
    save: useMutation({
      mutationFn: (v: { path: string; content: string }) => api<{ size: number }>("PUT", base(siteId) + "/content" + q(v.path, area), { content: v.content }),
      onSuccess: refresh,
    }),
    upload: useMutation({
      // multipart/form-data, so this bypasses the JSON helper (still sends CSRF).
      mutationFn: async (v: { dir: string; files: File[] }) => {
        const form = new FormData();
        for (const f of v.files) form.append("files", f, f.name);
        const res = await fetch(`/api/v1${base(siteId)}/upload${q(v.dir, area)}`, {
          method: "POST",
          body: form,
          credentials: "same-origin",
          headers: { "X-CSRF-Token": getCsrfToken() ?? "" },
        });
        const body = await res.json().catch(() => null);
        if (!res.ok) throw new ApiError(res.status, body?.error?.code ?? "upload_failed", body?.error?.message ?? "Upload failed");
        return body as { uploaded: string[] };
      },
      onSuccess: refresh,
    }),
  };
}
