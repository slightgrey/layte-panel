import { type FormEvent, useState } from "react";
import { useNavigate } from "react-router";
import { useLogin } from "@/api/auth";
import { Button } from "@/components/ui/button";
import { Input, Label } from "@/components/ui/input";
import { AuthShell, FormError } from "./AuthShell";

export function LoginPage() {
  const login = useLogin();
  const navigate = useNavigate();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");

  const onSubmit = (e: FormEvent) => {
    e.preventDefault();
    login.mutate({ username, password }, { onSuccess: () => navigate("/login/2fa") });
  };

  return (
    <AuthShell title="Sign in" description="Use your Layte Panel admin account.">
      <form onSubmit={onSubmit} className="flex flex-col gap-4">
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="username">Username</Label>
          <Input
            id="username"
            autoComplete="username"
            autoCapitalize="none"
            spellCheck={false}
            required
            autoFocus
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="flex flex-col gap-1.5">
          <Label htmlFor="password">Password</Label>
          <Input
            id="password"
            type="password"
            autoComplete="current-password"
            required
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        <FormError message={login.error?.message} />
        <Button type="submit" disabled={login.isPending}>
          {login.isPending ? "Signing in…" : "Continue"}
        </Button>
      </form>
    </AuthShell>
  );
}
