import qrcode from "qrcode-generator";
import { type FormEvent, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useTotpSetup, useTotpSetupConfirm } from "@/api/auth";
import { ApiError } from "@/api/client";
import { Spinner } from "@/components/Spinner";
import { Button } from "@/components/ui/button";
import { Checkbox } from "@/components/ui/field";
import { Input, Label } from "@/components/ui/input";
import { AuthShell, FormError } from "./AuthShell";

/** A QR code of the otpauth:// URI, drawn as an inline SVG data URL. */
function QRImage({ uri }: { uri: string }) {
  const src = useMemo(() => {
    const qr = qrcode(0, "M");
    qr.addData(uri);
    qr.make();
    return qr.createDataURL(5, 2);
  }, [uri]);
  return <img src={src} alt="QR code for your authenticator app" className="mx-auto size-48 rounded-[12px] bg-white p-1 [image-rendering:pixelated]" />;
}

/** First login of a site account (or after an admin reset it): add the panel to an authenticator app. */
export function TwoFactorSetupPage() {
  const setup = useTotpSetup();
  const confirm = useTotpSetupConfirm();
  const navigate = useNavigate();
  const [code, setCode] = useState("");
  const [remember, setRemember] = useState(false);

  const start = setup.mutate;
  useEffect(() => start(), [start]);

  const backToLogin = (err: unknown) =>
    err instanceof ApiError && (err.code === "unauthenticated" || err.code === "invalid_credentials");
  useEffect(() => {
    if (backToLogin(setup.error)) navigate("/login", { replace: true });
    if (setup.error instanceof ApiError && setup.error.code === "totp_set_up") navigate("/login/2fa", { replace: true });
  }, [setup.error, navigate]);

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    confirm.mutate(
      { code, remember },
      {
        onSuccess: () => navigate("/", { replace: true }),
        onError: (err) => {
          setCode("");
          if (backToLogin(err)) navigate("/login", { replace: true });
        },
      },
    );
  };

  return (
    <AuthShell title="Set up two-factor login" description="Scan the code with an authenticator app (1Password, Bitwarden, Google Authenticator…), then enter the 6-digit code it shows.">
      {setup.isPending ? (
        <div className="grid h-48 place-items-center">
          <Spinner />
        </div>
      ) : setup.data ? (
        <form onSubmit={onSubmit} className="flex flex-col gap-4">
          <QRImage uri={setup.data.uri} />
          <details className="text-sm text-muted-foreground">
            <summary className="cursor-pointer">Can't scan it?</summary>
            <p className="mt-2">Add an account by hand with this key (time-based, 6 digits):</p>
            <code className="mt-1 block break-all rounded-[12px] bg-background p-2 font-mono text-xs text-foreground shadow-inset">{setup.data.secret}</code>
          </details>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="code">Code from the app</Label>
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
          <FormError message={confirm.error?.message} />
          <Button type="submit" disabled={confirm.isPending || code.length !== 6}>
            {confirm.isPending ? "Verifying…" : "Turn on and sign in"}
          </Button>
        </form>
      ) : (
        <FormError message={setup.error?.message} />
      )}
      <Link to="/login" className="mt-4 block text-center text-sm text-muted-foreground hover:text-foreground">
        Back to sign in
      </Link>
    </AuthShell>
  );
}
