import { useMutation, useQuery } from "@tanstack/react-query";
import { type FormEvent, useState } from "react";
import { Link, useNavigate, useParams } from "react-router";
import { api } from "@/api/client";
import type { LoginResponse } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { AuthShell, FormError } from "./AuthShell";

const MIN_PASSWORD = 12; // auth.MinPasswordLen

/** GET /setup/{token}: who a setup link is for. */
function useSetupLink(token: string) {
  return useQuery({
    queryKey: ["setup", token],
    queryFn: () => api<{ username: string; expires_at: string; totp_set_up: boolean }>("GET", `/setup/${token}`),
    retry: false,
  });
}

/** A one-time setup link: choose a password, then set up (or enter) the 2FA code. */
export function SetupLinkPage() {
  const token = useParams().token ?? "";
  const link = useSetupLink(token);
  const navigate = useNavigate();
  const use = useMutation({
    mutationFn: (password: string) => api<LoginResponse>("POST", `/setup/${token}`, { password }),
    onSuccess: (r) => navigate(r.totp_setup ? "/login/2fa/setup" : "/login/2fa", { replace: true }),
  });
  const [pw, setPw] = useState("");
  const [again, setAgain] = useState("");
  const mismatch = again !== "" && pw !== again;

  if (link.isPending) {
    return (
      <div className="grid min-h-svh place-items-center">
        <Spinner />
      </div>
    );
  }
  if (link.error) {
    return (
      <AuthShell title="Link not valid" description={link.error.message}>
        <Link to="/login" className="block text-center text-sm text-primary hover:underline">
          Go to sign in
        </Link>
      </AuthShell>
    );
  }
  return (
    <AuthShell
      title={link.data.totp_set_up ? "Choose a new password" : "Welcome to Layte Panel"}
      description={`Choose a password for ${link.data.username}.${link.data.totp_set_up ? "" : " Next, you'll set up two-factor login with an authenticator app."}`}
    >
      <form
        className="flex flex-col gap-4"
        onSubmit={(e: FormEvent) => {
          e.preventDefault();
          use.mutate(pw);
        }}
      >
        <input type="text" name="username" autoComplete="username" value={link.data.username} readOnly hidden />
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="pw">Password</Label>
          <Input id="pw" type="password" autoComplete="new-password" required autoFocus minLength={MIN_PASSWORD} value={pw} onChange={(e) => setPw(e.target.value)} />
          <p className="text-xs text-muted-foreground">At least {MIN_PASSWORD} characters. A few random words make a good one.</p>
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="pw2">Repeat password</Label>
          <Input id="pw2" type="password" autoComplete="new-password" required value={again} onChange={(e) => setAgain(e.target.value)} />
          {mismatch && <p className="text-xs text-destructive">The passwords don't match.</p>}
        </div>
        <FormError message={use.error?.message} />
        <Button type="submit" disabled={use.isPending || pw.length < MIN_PASSWORD || pw !== again}>
          {use.isPending ? "Saving…" : "Continue"}
        </Button>
        <p className="text-center text-xs text-muted-foreground">This link works once, until {new Date(link.data.expires_at).toLocaleString()}.</p>
      </form>
    </AuthShell>
  );
}
