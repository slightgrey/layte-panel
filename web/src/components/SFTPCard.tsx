import { Copy, KeyRound, Plus, Power, RefreshCw, Trash2 } from "lucide-react";
import { type ReactNode, useState } from "react";
import { type SFTPState, useNewSFTPPassword, useSetSFTP, useSFTP } from "@/api/sftp";
import type { Site } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Notice } from "@/components/ui/notice";
import { FormError } from "@/pages/AuthShell";

function CopyButton({ text }: { text: string }) {
  const [done, setDone] = useState(false);
  const copy = async () => {
    try {
      await navigator.clipboard.writeText(text);
      setDone(true);
      setTimeout(() => setDone(false), 1500);
    } catch {
      // no clipboard (e.g. plain HTTP): the text is shown, select it by hand
    }
  };
  return (
    <Button size="sm" variant="ghost" onClick={copy}>
      <Copy /> {done ? "Copied" : "Copy"}
    </Button>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-x-3 gap-y-1 py-1.5 text-sm">
      <span className="w-24 shrink-0 text-muted-foreground">{label}</span>
      <span className="min-w-0 flex-1 break-all font-mono">{children}</span>
    </div>
  );
}

/** SFTP login for one site: SSH keys, optional password, connection details. */
export function SFTPCard({ site }: { site: Site }) {
  const sftp = useSFTP(site.id);
  const set = useSetSFTP(site.id);
  const newPassword = useNewSFTPPassword(site.id);
  const [pasted, setPasted] = useState("");
  const [shownPassword, setShownPassword] = useState<string | null>(null);
  const s = sftp.data;
  if (!s) return null;

  const busy = set.isPending || site.status !== "active";
  const keyList = s.keys.map((k) => k.key);
  const save = (patch: Partial<{ enabled: boolean; password: boolean; keys: string[] }>, after?: () => void) =>
    set.mutate({ enabled: s.enabled, password: s.password, keys: keyList, ...patch }, { onSuccess: () => after?.() });
  const addKeys = () => {
    const lines = pasted
      .split(/\r?\n/)
      .map((l) => l.trim())
      .filter(Boolean);
    if (lines.length) save({ keys: [...keyList, ...lines] }, () => setPasted(""));
  };
  const lastKeyLocked = s.enabled && s.keys.length === 1 && !s.password;

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" /> SFTP access
        </CardTitle>
        <div className="flex items-center gap-2">
          {s.enabled ? <Badge variant="success">on</Badge> : <Badge>off</Badge>}
          <Button
            size="sm"
            variant={s.enabled ? "outline" : "default"}
            disabled={busy}
            onClick={() => save({ enabled: !s.enabled, password: s.enabled ? false : s.password || s.keys.length === 0 })}
          >
            <Power /> {s.enabled ? "Turn off" : s.keys.length === 0 ? "Turn on with a password" : "Turn on"}
          </Button>
        </div>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          A login for FileZilla, VS Code, rsync or <code>sftp</code> that only reaches this site's folder and works as the
          site's user, so files keep the right owner. SFTP only: no shell, no tunnels.
        </p>

        {s.enabled && <Details s={s} host={window.location.hostname} />}
        {!!s.warnings?.length && (
          <Notice tone="warning">
            <ul className="list-disc pl-5">
              {s.warnings.map((w) => (
                <li key={w}>{w}</li>
              ))}
            </ul>
          </Notice>
        )}

        <div className="flex flex-col gap-2">
          <p className="text-sm font-medium">SSH keys</p>
          {s.keys.length === 0 && <p className="text-sm text-muted-foreground">No keys yet.</p>}
          {s.keys.map((k) => (
            <div key={k.fingerprint} className="flex items-center gap-2 rounded-[12px] bg-background px-3 py-2 text-sm shadow-inset">
              <div className="min-w-0 flex-1">
                <div className="truncate font-mono text-xs">{k.fingerprint}</div>
                <div className="truncate text-xs text-muted-foreground">{k.key.split(" ").slice(2).join(" ") || k.key.split(" ")[0]}</div>
              </div>
              <Button
                size="icon"
                variant="ghost"
                aria-label="Remove key"
                disabled={busy || lastKeyLocked}
                title={lastKeyLocked ? "The last key can't be removed while SFTP is on without passwords" : undefined}
                onClick={() => save({ keys: s.keys.filter((x) => x.fingerprint !== k.fingerprint).map((x) => x.key) })}
              >
                <Trash2 />
              </Button>
            </div>
          ))}
          <textarea
            aria-label="Public SSH key"
            className="min-h-20 w-full rounded-[12px] bg-background p-3 font-mono text-xs shadow-inset outline-none focus-visible:ring-2 focus-visible:ring-ring"
            placeholder="Paste a public key (the line from id_ed25519.pub: ssh-ed25519 AAAA… you@pc)"
            value={pasted}
            onChange={(e) => setPasted(e.target.value)}
          />
          <div>
            <Button size="sm" variant="outline" disabled={busy || !pasted.trim()} onClick={addKeys}>
              <Plus /> Add key
            </Button>
          </div>
        </div>

        <div className="flex flex-col gap-2 border-t pt-4">
          <label className="flex items-start gap-2 text-sm">
            <input
              type="checkbox"
              className="mt-1"
              checked={s.password}
              disabled={busy || !s.enabled || (s.password && s.keys.length === 0)}
              onChange={(e) => save({ password: e.target.checked }, () => setShownPassword(null))}
            />
            <span>
              Allow password login{" "}
              <span className="text-muted-foreground">(for clients without keys; keys are safer)</span>
            </span>
          </label>
          {s.enabled && s.password && (
            <div className="flex flex-wrap items-center gap-2">
              <Button
                size="sm"
                variant="outline"
                disabled={busy || newPassword.isPending}
                onClick={() => newPassword.mutate(undefined, { onSuccess: (r) => setShownPassword(r.password) })}
              >
                <RefreshCw /> {shownPassword ? "Generate another" : "Generate password"}
              </Button>
              {shownPassword && (
                <>
                  <code className="rounded-[8px] bg-background px-2 py-1 font-mono text-sm shadow-inset">{shownPassword}</code>
                  <CopyButton text={shownPassword} />
                  <span className="text-xs text-muted-foreground">Shown once and not stored. Generate a new one if you lose it.</span>
                </>
              )}
            </div>
          )}
        </div>
        <FormError message={set.error?.message ?? newPassword.error?.message} />
      </CardContent>
    </Card>
  );
}

function Details({ s, host }: { s: SFTPState; host: string }) {
  const cmd = `sftp ${s.username}@${host}`;
  return (
    <div className="rounded-[12px] bg-background px-4 py-2 shadow-inset">
      <Row label="Host">{host}</Row>
      <Row label="Port">22</Row>
      <Row label="Protocol">SFTP</Row>
      <Row label="Username">{s.username}</Row>
      <Row label="Folder">{s.folder}</Row>
      <div className="flex flex-wrap items-center gap-2 py-1.5 text-sm">
        <code className="min-w-0 flex-1 break-all font-mono text-xs">{cmd}</code>
        <CopyButton text={cmd} />
      </div>
      <p className="pb-1 text-xs text-muted-foreground">
        Use the server's own address (like this panel's). A domain behind Cloudflare's proxy doesn't pass SSH through.
      </p>
    </div>
  );
}
