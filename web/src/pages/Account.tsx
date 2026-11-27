import { Check, KeyRound, LogOut, Monitor, ShieldCheck, UserRound } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useAccount, useChangePassword, useConfirm2FAChange, useSignOutSessions, useStart2FAChange, useUpdateAccount } from "@/api/account";
import type { AccountResponse, AccountSession, TOTPSetupResponse } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { FormError } from "./AuthShell";
import { QRImage } from "./TwoFactorSetup";

const MIN_PASSWORD = 12; // auth.MinPasswordLen

/** Your own account: profile, password, two-factor login and signed-in sessions. */
export function AccountPage() {
  const account = useAccount();
  return (
    <div className="mx-auto flex max-w-3xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Account</h1>
        <p className="text-sm text-muted-foreground">Your own login: profile, password, two-factor login and where you're signed in.</p>
      </div>
      {account.isPending ? (
        <Spinner />
      ) : account.error ? (
        <p className="text-sm text-destructive">{account.error.message}</p>
      ) : (
        <>
          <ProfileCard a={account.data} />
          <PasswordCard />
          <TwoFactorCard a={account.data} />
          <SessionsCard sessions={account.data.sessions} />
        </>
      )}
    </div>
  );
}

/** A short "saved" tick after a successful change. */
function Saved({ show, text = "Saved" }: { show: boolean; text?: string }) {
  if (!show) return null;
  return (
    <span className="inline-flex items-center gap-1 text-sm text-success" role="status">
      <Check className="size-4" /> {text}
    </span>
  );
}

function ProfileCard({ a }: { a: AccountResponse }) {
  const update = useUpdateAccount();
  const [name, setName] = useState(a.display_name);
  const [email, setEmail] = useState(a.email);
  const dirty = name.trim() !== a.display_name || email.trim() !== a.email;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <UserRound className="size-4" /> Profile
        </CardTitle>
        <Badge>{a.role === "admin" ? "admin" : "site login"}</Badge>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            update.mutate({ display_name: name.trim(), email: email.trim() });
          }}
        >
          <Field label="Username" htmlFor="acc-username" hint="Used to sign in; it can't be changed.">
            <Input id="acc-username" value={a.username} disabled className="max-w-sm font-mono" />
          </Field>
          <Field label="Display name" htmlFor="acc-name" hint="Shown at the top instead of your username. Optional.">
            <Input id="acc-name" maxLength={64} className="max-w-sm" value={name} onChange={(e) => setName(e.target.value)} />
          </Field>
          <Field label="Email" htmlFor="acc-email" hint="For your own reference. Optional.">
            <Input id="acc-email" type="email" className="max-w-sm" value={email} onChange={(e) => setEmail(e.target.value)} />
          </Field>
          <FormError message={update.error?.message} />
          <div className="flex items-center gap-3">
            <Button type="submit" disabled={!dirty || update.isPending}>
              Save profile
            </Button>
            <Saved show={update.isSuccess && !dirty} />
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function PasswordCard() {
  const change = useChangePassword();
  const [current, setCurrent] = useState("");
  const [next, setNext] = useState("");
  const [again, setAgain] = useState("");
  const mismatch = again !== "" && next !== again;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <KeyRound className="size-4" /> Password
        </CardTitle>
      </CardHeader>
      <CardContent>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e: FormEvent) => {
            e.preventDefault();
            change.mutate(
              { current_password: current, new_password: next },
              {
                onSuccess: () => {
                  setCurrent("");
                  setNext("");
                  setAgain("");
                },
              },
            );
          }}
        >
          <Field label="Current password" htmlFor="pw-current">
            <Input id="pw-current" type="password" autoComplete="current-password" required className="max-w-sm" value={current} onChange={(e) => setCurrent(e.target.value)} />
          </Field>
          <Field label="New password" htmlFor="pw-new" hint={`At least ${MIN_PASSWORD} characters. A passphrase of a few words is easy to remember.`}>
            <Input id="pw-new" type="password" autoComplete="new-password" required minLength={MIN_PASSWORD} className="max-w-sm" value={next} onChange={(e) => setNext(e.target.value)} />
          </Field>
          <Field label="Repeat new password" htmlFor="pw-again" hint={mismatch ? <span className="text-destructive">The passwords don't match.</span> : undefined}>
            <Input id="pw-again" type="password" autoComplete="new-password" required className="max-w-sm" value={again} onChange={(e) => setAgain(e.target.value)} />
          </Field>
          <FormError message={change.error?.message} />
          <div className="flex flex-wrap items-center gap-3">
            <Button type="submit" disabled={change.isPending || !current || next.length < MIN_PASSWORD || next !== again}>
              Change password
            </Button>
            <Saved show={change.isSuccess} text="Password changed. Your other sessions were signed out." />
          </div>
        </form>
      </CardContent>
    </Card>
  );
}

function TwoFactorCard({ a }: { a: AccountResponse }) {
  const start = useStart2FAChange();
  const confirm = useConfirm2FAChange();
  const [current, setCurrent] = useState("");
  const [code, setCode] = useState("");
  const [setup, setSetup] = useState<TOTPSetupResponse | null>(null);
  const [open, setOpen] = useState(false);

  // Forget the secret once it's confirmed (it's in the app now).
  useEffect(() => {
    if (confirm.isSuccess) {
      setSetup(null);
      setOpen(false);
      setCode("");
      setCurrent("");
    }
  }, [confirm.isSuccess]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <ShieldCheck className="size-4" /> Two-factor login
        </CardTitle>
        {a.totp_set_up ? <Badge variant="success">on</Badge> : <Badge variant="warning">off</Badge>}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          Required for every login. Moving to a new phone or app? Set it up again here: your current app keeps working until the new
          one is confirmed.
        </p>
        <Saved show={confirm.isSuccess && !open} text="Two-factor login moved to the new app. Your other sessions were signed out." />
        {!open ? (
          <Button variant="outline" className="self-start" onClick={() => (confirm.reset(), setOpen(true))}>
            Set up on a new device
          </Button>
        ) : !setup ? (
          <form
            className="flex flex-col gap-4"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              start.mutate({ current_password: current }, { onSuccess: setSetup });
            }}
          >
            <Field label="Current password" htmlFor="tfa-current" hint="To make sure it's you.">
              <Input id="tfa-current" type="password" autoComplete="current-password" required autoFocus className="max-w-sm" value={current} onChange={(e) => setCurrent(e.target.value)} />
            </Field>
            <FormError message={start.error?.message} />
            <div className="flex gap-2">
              <Button type="submit" disabled={start.isPending || !current}>
                Continue
              </Button>
              <Button type="button" variant="outline" onClick={() => (setOpen(false), start.reset())}>
                Cancel
              </Button>
            </div>
          </form>
        ) : (
          <form
            className="flex flex-col gap-4"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              confirm.mutate({ code }, { onError: () => setCode("") });
            }}
          >
            <p className="text-sm">Scan this with the new authenticator app, then enter the 6-digit code it shows.</p>
            <div className="self-start">
              <QRImage uri={setup.uri} />
            </div>
            <details className="text-sm text-muted-foreground">
              <summary className="cursor-pointer">Can't scan it?</summary>
              <code className="mt-2 block break-all rounded-[12px] bg-background p-2 font-mono text-xs text-foreground shadow-inset">{setup.secret}</code>
            </details>
            <Field label="Code from the new app" htmlFor="tfa-code">
              <Input
                id="tfa-code"
                inputMode="numeric"
                autoComplete="one-time-code"
                pattern="[0-9]{6}"
                maxLength={6}
                required
                autoFocus
                className="w-40 text-center font-mono text-lg tracking-[0.3em]"
                value={code}
                onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
              />
            </Field>
            <FormError message={confirm.error?.message} />
            <div className="flex gap-2">
              <Button type="submit" disabled={confirm.isPending || code.length !== 6}>
                Confirm
              </Button>
              <Button type="button" variant="outline" onClick={() => (setOpen(false), setSetup(null), start.reset(), confirm.reset())}>
                Cancel
              </Button>
            </div>
          </form>
        )}
      </CardContent>
    </Card>
  );
}

/** "Firefox on Windows" from a user agent, roughly; the full string is in the tooltip. */
function deviceName(ua: string) {
  const browser = /Edg\//.test(ua) ? "Edge" : /Firefox\//.test(ua) ? "Firefox" : /Chrome\//.test(ua) ? "Chrome" : /Safari\//.test(ua) ? "Safari" : "Browser";
  const os = /Windows/.test(ua) ? "Windows" : /Android/.test(ua) ? "Android" : /iPhone|iPad/.test(ua) ? "iOS" : /Mac OS X/.test(ua) ? "macOS" : /Linux/.test(ua) ? "Linux" : "";
  return os ? `${browser} on ${os}` : ua ? browser : "Unknown device";
}

function SessionsCard({ sessions }: { sessions: AccountSession[] }) {
  const signOut = useSignOutSessions();
  const others = sessions.filter((s) => !s.current).length;
  return (
    <Card>
      <CardHeader>
        <CardTitle className="flex items-center gap-2">
          <Monitor className="size-4" /> Signed-in sessions
        </CardTitle>
        <Button variant="outline" size="sm" disabled={others === 0 || signOut.isPending} onClick={() => signOut.mutate(undefined)}>
          <LogOut /> Sign out everywhere else
        </Button>
      </CardHeader>
      <CardContent className="flex flex-col divide-y">
        {sessions.map((s) => (
          <div key={s.id} className="flex flex-wrap items-center gap-x-3 gap-y-1 py-2.5">
            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-center gap-2 text-sm font-medium" title={s.user_agent}>
                {deviceName(s.user_agent)}
                {s.current && <Badge variant="success">this browser</Badge>}
                {s.remember && <Badge>remembered</Badge>}
              </div>
              <div className="text-xs text-muted-foreground">
                {s.ip || "unknown IP"} · signed in {new Date(s.created_at).toLocaleString()} · last active {new Date(s.last_seen_at).toLocaleString()}
              </div>
            </div>
            {!s.current && (
              <Button variant="ghost" size="sm" disabled={signOut.isPending} onClick={() => signOut.mutate(s.id)}>
                Sign out
              </Button>
            )}
          </div>
        ))}
        <FormError message={signOut.error?.message} />
      </CardContent>
    </Card>
  );
}
