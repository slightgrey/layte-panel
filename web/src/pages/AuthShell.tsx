import type { ReactNode } from "react";
import { Brand } from "@/components/Layout";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Card, CardContent } from "@/components/ui/card";

/** Centered card used by the login and 2FA pages. */
export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="relative grid min-h-svh place-items-center px-4">
      <ThemeToggle className="absolute top-5 right-5" />
      <div className="w-full max-w-sm">
        <Brand className="mb-8 justify-center text-lg" />
        <Card>
          <CardContent className="pt-7">
            <p className="eyebrow mb-2 text-center">Web of D · control panel</p>
            <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
            <p className="mb-5 text-sm text-muted-foreground">{description}</p>
            {children}
          </CardContent>
        </Card>
        <p className="mt-6 text-center font-mono text-xs text-muted-foreground">
          $ layte serve<span className="caret" />
        </p>
      </div>
    </div>
  );
}

export function FormError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-[12px] bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {message}
    </p>
  );
}
