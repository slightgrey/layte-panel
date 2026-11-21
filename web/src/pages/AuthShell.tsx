import type { ReactNode } from "react";
import { Brand } from "@/components/Layout";
import { Card, CardContent } from "@/components/ui/card";

/** Centered card used by the login and 2FA pages. */
export function AuthShell({ title, description, children }: { title: string; description: string; children: ReactNode }) {
  return (
    <div className="grid min-h-svh place-items-center px-4">
      <div className="w-full max-w-sm">
        <Brand className="mb-6 justify-center text-lg" />
        <Card>
          <CardContent className="pt-6">
            <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
            <p className="mb-5 text-sm text-muted-foreground">{description}</p>
            {children}
          </CardContent>
        </Card>
      </div>
    </div>
  );
}

export function FormError({ message }: { message?: string }) {
  if (!message) return null;
  return (
    <p role="alert" className="rounded-md border border-destructive/30 bg-destructive/10 px-3 py-2 text-sm text-destructive">
      {message}
    </p>
  );
}
