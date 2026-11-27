import { KeyRound, UserPlus } from "lucide-react";
import { type FormEvent, useState } from "react";
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
  const [limit, setLimit] = useState(String(a.max_subdomains));
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
      <div className="flex flex-wrap items-end gap-2">
        <form
          className="flex items-end gap-2"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            update.mutate({ id: a.id, max_subdomains: Number(limit) });
          }}
        >
          <Field label="Subdomain limit" htmlFor={`limit-${a.id}`}>
            <Input id={`limit-${a.id}`} type="number" min={0} max={100} className="w-24" value={limit} onChange={(e) => setLimit(e.target.value)} />
          </Field>
          <Button type="submit" variant="outline" size="sm" disabled={update.isPending || Number(limit) === a.max_subdomains}>
            Save
          </Button>
        </form>
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

function CreateLogin({ site, onCreated }: { site: Site; onCreated: (v: SetupLink) => void }) {
  const [open, setOpen] = useState(false);
  const [username, setUsername] = useState(suggestUsername(site));
  const [limit, setLimit] = useState("5");
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
              { username: username.trim(), site_id: site.id, max_subdomains: Number(limit) },
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
          <Field label="Subdomain limit" htmlFor="login-limit" hint={`How many subdomains of ${site.domain} they may add (0-100).`}>
            <Input id="login-limit" type="number" min={0} max={100} required value={limit} onChange={(e) => setLimit(e.target.value)} />
          </Field>
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
