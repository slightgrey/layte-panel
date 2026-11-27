import { type FormEvent, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useTotp } from "@/api/auth";
import { ApiError } from "@/api/client";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/field";
import { Input, Label } from "@/components/ui/input";
import { AuthShell, FormError } from "./AuthShell";

export function TwoFactorPage() {
  const totp = useTotp();
  const navigate = useNavigate();
  const [code, setCode] = useState("");
  const [remember, setRemember] = useState(false);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    totp.mutate({ code, remember }, {
      onSuccess: () => navigate("/", { replace: true }),
      onError: (err) => {
        setCode("");
        // The password step expired (5 minutes) or the account got locked.
        if (err instanceof ApiError && (err.code === "unauthenticated" || err.code === "invalid_credentials")) {
          navigate("/login", { replace: true });
        }
        if (err instanceof ApiError && err.code === "totp_setup_required") navigate("/login/2fa/setup", { replace: true });
      },
    });
  };

  return (
    <AuthShell title="Two-factor authentication" description="Enter the 6-digit code from your authenticator app.">
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="code">Code</Label>
          <Input
            id="code"
            inputMode="numeric"
            autoComplete="one-time-code"
            pattern="[0-9]{6}"
            maxLength={6}
            required
            autoFocus
            className="text-center font-mono text-lg tracking-[0.4em]"
            value={code}
            onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
          />
        </div>
        <Checkbox
          label="Remember this browser for 30 days"
          description="Stay signed in here, even after closing the browser. Only on your own computer."
          checked={remember}
          onChange={(e) => setRemember(e.target.checked)}
        />
        <FormError message={totp.error?.message} />
        <Button type="submit" disabled={totp.isPending || code.length !== 6}>
          {totp.isPending ? "Verifying…" : "Verify"}
        </Button>
        <Link to="/login" className="text-center text-sm text-muted-foreground hover:text-foreground">
          Back to sign in
        </Link>
      </form>
    </AuthShell>
  );
}
