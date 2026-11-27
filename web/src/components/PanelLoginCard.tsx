import { KeyRound, UserPlus } from "lucide-react";
import { type FormEvent, useState } from "react";
import { useQuotaStatus } from "@/api/quota";
import type { Account, SetupLink, Site } from "@/api/types";
import { useCreateUser, useDeleteUser, useResetUser2FA, useResetUserPassword, useSiteAccount, useUpdateUser } from "@/api/users";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "@/pages/AuthShell";
import { OneTimeSecrets } from "./JobProgress";

/** A suggested login name for a site: its Linux user. */
const suggestUsername = (site: Site) => site.system_user.slice(0, 32);

/** Admins: the site's panel login (a site account), or a button to create one. */
/** The setup link of a new login (or a reset), shown once with how to pass it on. */
export function SetupLinkPanel({ link, onDismiss }: { link: SetupLink; onDismiss: () => void }) {
  return (
    <div className="flex flex-col gap-2">
      <OneTimeSecrets secrets={{ login_username: link.username, setup_url: link.setup_url }} onDismiss={onDismiss} />
      <p className="text-xs text-muted-foreground">
        Send this link to {link.username} privately. It works <strong>once</strong>, until {new Date(link.expires_at).toLocaleString()}: they
        choose their own password and set up two-factor login. If it reaches the wrong person, issue a new link; the old one stops working.
      </p>
    </div>
  );
}

export function PanelLoginCard({ site, created }: { site: Site; created?: SetupLink }) {
  const acc = useSiteAccount(site.id);
  const [shown, setShown] = useState(created ?? null);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Panel login</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          A login for whoever runs this site. It sees only this site and its subdomains, never the server's other sites or
          settings, and must set up two-factor login the first time.
        </p>
        {shown && <SetupLinkPanel link={shown} onDismiss={() => setShown(null)} />}
        {acc.isPending ? (
          <Spinner />
        ) : acc.error ? (
          <p className="text-sm text-destructive">{acc.error.message}</p>
        ) : acc.account ? (
          <AccountRow account={acc.account} onLink={setShown} />
        ) : (
          <CreateLogin site={site} onCreated={setShown} />
        )}
      </CardContent>
    </Card>
  );
}

/** One site account with its admin actions. */
export function AccountRow({ account: a, onLink }: { account: Account; onLink: (link: SetupLink) => void }) {
  const reset = useResetUserPassword();
  const reset2fa = useResetUser2FA();
  const del = useDeleteUser();
  const update = useUpdateUser();
  const [limits, setLimits] = useState(() => limitsForm(a));
  const dirty = JSON.stringify(limits) !== JSON.stringify(limitsForm(a));
  const err = reset.error ?? reset2fa.error ?? del.error ?? update.error;

  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <KeyRound className="size-4 text-muted-foreground" />
        <span className="font-mono text-sm font-medium">{a.username}</span>
        {a.totp_set_up ? <Badge variant="success">2FA on</Badge> : <Badge variant="warning">2FA at first login</Badge>}
        {a.setup_expires &&
          (new Date(a.setup_expires) > new Date() ? (
            <Badge title="Waiting for the user to open their setup link">link until {new Date(a.setup_expires).toLocaleDateString()}</Badge>
          ) : (
            <Badge variant="danger">setup link expired</Badge>
          ))}
        {a.locked && <Badge variant="danger">locked</Badge>}
        <span className="text-xs text-muted-foreground">
          {a.subdomains} of {a.max_subdomains} subdomains used
        </span>
      </div>
      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          update.mutate({ id: a.id, ...limitsBody(limits) });
        }}
      >
        <LimitsFields id={`acc-${a.id}`} value={limits} onChange={setLimits} compact />
        <Button type="submit" variant="outline" size="sm" disabled={update.isPending || !dirty}>
          Save limits
        </Button>
      </form>
      <div className="flex flex-wrap items-end gap-2">
        <Button
          variant="outline"
          size="sm"
          disabled={reset.isPending}
          onClick={() =>
            confirm(`Make a password reset link for ${a.username}? Any older link stops working. Their current password works until the link is used.`) &&
            reset.mutate(a.id, { onSuccess: (r) => onLink({ username: a.username, ...r }) })
          }
        >
          {a.totp_set_up ? "Password reset link" : "New setup link"}
        </Button>
        <Button variant="outline" size="sm" disabled={reset2fa.isPending || !a.totp_set_up} onClick={() => confirm(`Reset ${a.username}'s two-factor login? They set it up again at the next sign-in.`) && reset2fa.mutate(a.id)}>
          Reset 2FA
        </Button>
        <Button variant="ghost" size="sm" className="text-destructive" disabled={del.isPending} onClick={() => confirm(`Delete the login ${a.username}? The sites and their files stay.`) && del.mutate(a.id)}>
          Delete login
        </Button>
      </div>
      <FormError message={err?.message} />
    </div>
  );
}

/** The limits form's values, as text (inputs); "0" = no limit. */
export type LimitsForm = { subdomains: string; cpu: string; memory: string; disk: string };

export const defaultLimits: LimitsForm = { subdomains: "5", cpu: "100", memory: "512", disk: "0" };

function limitsForm(a: Account): LimitsForm {
  return {
    subdomains: String(a.max_subdomains),
    cpu: String(a.limits?.cpu_percent ?? 0),
    memory: String(a.limits?.memory_mb ?? 0),
    disk: String(Math.round((a.limits?.disk_mb ?? 0) / 1024)),
  };
}

/** The API fields for a limits form (disk is entered in GB). */
export function limitsBody(f: LimitsForm) {
  return { max_subdomains: Number(f.subdomains), cpu_percent: Number(f.cpu), memory_mb: Number(f.memory), disk_mb: Number(f.disk) * 1024 };
}

/** Subdomains, CPU, RAM and disk limits of a site login. */
export function LimitsFields({ id, value: v, onChange, compact = false }: { id: string; value: LimitsForm; onChange: (v: LimitsForm) => void; compact?: boolean }) {
  const set = (k: keyof LimitsForm) => (e: React.ChangeEvent<HTMLInputElement>) => onChange({ ...v, [k]: e.target.value });
  const w = compact ? "w-24" : "";
  const quota = useQuotaStatus();
  const quotaOn = Boolean(quota.data?.enabled);
  return (
    <div className={compact ? "flex flex-wrap items-end gap-2" : "grid gap-4 sm:grid-cols-2"}>
      <Field label="Subdomains" htmlFor={`${id}-sub`} hint={compact ? undefined : "How many subdomain sites they may add (0-100)."}>
        <Input id={`${id}-sub`} type="number" min={0} max={100} required className={w} value={v.subdomains} onChange={set("subdomains")} />
      </Field>
      <Field label="CPU %" htmlFor={`${id}-cpu`} hint={compact ? undefined : "100 = one full CPU core. 0 = no limit."}>
        <Input id={`${id}-cpu`} type="number" min={0} max={800} step={5} required className={w} value={v.cpu} onChange={set("cpu")} />
      </Field>
      <Field label="RAM (MB)" htmlFor={`${id}-mem`} hint={compact ? undefined : "For all their PHP and cron jobs together (64 or more). 0 = no limit."}>
        <Input id={`${id}-mem`} type="number" min={0} max={65536} step={64} required className={w} value={v.memory} onChange={set("memory")} />
      </Field>
      <Field
        label="Disk (GB)"
        htmlFor={`${id}-disk`}
        hint={compact ? undefined : quotaOn ? "Hard limit for all their files and databases' dumps. 0 = no limit." : "Turn on disk quotas first (Users → Disk quotas)."}
      >
        <Input
          id={`${id}-disk`}
          type="number"
          min={0}
          max={16384}
          required
          className={w}
          disabled={!quotaOn}
          title={quotaOn ? undefined : "Turn on disk quotas first (Users → Disk quotas)"}
          value={quotaOn ? v.disk : "0"}
          onChange={set("disk")}
        />
      </Field>
    </div>
  );
}

function CreateLogin({ site, onCreated }: { site: Site; onCreated: (v: SetupLink) => void }) {
  const [open, setOpen] = useState(false);
  const [username, setUsername] = useState(suggestUsername(site));
  const [limits, setLimits] = useState(defaultLimits);
  const create = useCreateUser();
  if (site.parent_site_id) {
    return <p className="text-sm text-muted-foreground">This is a subdomain site: it uses the login of its main site.</p>;
  }
  if (!site.managed && !site.adopted) {
    return <p className="text-sm text-muted-foreground">Adopt the site first to give it a login.</p>;
  }
  const close = () => {
    setOpen(false);
    create.reset();
  };
  return (
    <>
      <Button variant="outline" className="self-start" onClick={() => setOpen(true)}>
        <UserPlus /> Create login
      </Button>
      <Dialog open={open} onClose={close} title={`Login for ${site.domain}`}>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            create.mutate(
              { username: username.trim(), site_id: site.id, ...limitsBody(limits) },
              {
                onSuccess: (r) => {
                  onCreated({ username: r.user.username, setup_url: r.setup_url, expires_at: r.expires_at });
                  close();
                },
              },
            );
          }}
        >
          <Field label="Username" htmlFor="login-user" hint="3-32 lowercase letters, digits, _ or -. You get a one-time setup link to send them; they choose their own password.">
            <Input id="login-user" required pattern="[a-z][a-z0-9_\-]{2,31}" spellCheck={false} value={username} onChange={(e) => setUsername(e.target.value)} />
          </Field>
          <LimitsFields id="login" value={limits} onChange={setLimits} />
          <FormError message={create.error?.message} />
          <div className="flex justify-end gap-2">
            <Button type="button" variant="outline" onClick={close}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending}>
              Create login
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
