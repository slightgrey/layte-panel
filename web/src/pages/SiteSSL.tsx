import { ShieldCheck, ShieldOff } from "lucide-react";
import { type FormEvent, useEffect, useState } from "react";
import { useSearchParams } from "react-router";
import { useCertificates, useIssueSSL, useRemoveSSL } from "@/api/ssl";
import type { Site } from "@/api/types";
import { CertBadge } from "@/components/CertBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { SiteShell } from "@/components/SiteShell";
import { FormError } from "./AuthShell";

/** HTTPS card on the site page: status, and issue/remove for managed sites. */
export function SiteSSLCard({ site }: { site: Site }) {
  const certs = useCertificates();
  const cert = certs.data?.certificates.find((c) => c.site_id === site.id);
  const [, setParams] = useSearchParams();
  const issue = useIssueSSL(site.id);
  const remove = useRemoveSSL(site.id);
  const [email, setEmail] = useState("");
  useEffect(() => {
    if (certs.data?.email && !email) setEmail(certs.data.email);
  }, [certs.data, email]);

  const showJob = (r: { job_id: string }) => setParams({ job: r.job_id }, { replace: true });
  const busy = site.status !== "active";

  return (
    <Card>
      <CardHeader>
        <CardTitle>HTTPS</CardTitle>
        {cert && <CertBadge cert={cert} />}
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        {site.ssl ? (
          <div className="flex flex-wrap items-center justify-between gap-3">
            <p className="flex items-center gap-2 text-sm">
              <ShieldCheck className="size-4 text-success" />
              {cert ? (
                <>
                  Certificate for {cert.domains.join(", ")}, valid until {new Date(cert.not_after).toLocaleDateString()}.
                </>
              ) : (
                "HTTPS is enabled."
              )}
            </p>
            {site.managed && (
              <Button variant="outline" size="sm" disabled={busy || remove.isPending} onClick={() => remove.mutate(undefined, { onSuccess: showJob })}>
                <ShieldOff /> Remove HTTPS
              </Button>
            )}
          </div>
        ) : site.managed ? (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              issue.mutate(email.trim(), { onSuccess: showJob });
            }}
          >
            <p className="text-sm text-muted-foreground">
              Get a free Let's Encrypt certificate for {[site.domain, ...site.aliases].join(", ")}. The DNS of every name
              must already point to this server. Renewal is automatic.
            </p>
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-64 flex-1">
                <Field label="Email for expiry notices" htmlFor="le-email">
                  <Input id="le-email" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
                </Field>
              </div>
              <Button type="submit" disabled={busy || issue.isPending}>
                <ShieldCheck /> Enable HTTPS
              </Button>
            </div>
            <FormError message={issue.error?.message} />
          </form>
        ) : (
          <p className="text-sm text-muted-foreground">This site serves HTTP only.</p>
        )}
        <FormError message={remove.error?.message} />
      </CardContent>
    </Card>
  );
}

/** /sites/:id/ssl — the HTTPS card on its own tab. */
export function SiteSSLPage() {
  return <SiteShell title="SSL certificate">{(site) => <SiteSSLCard site={site} />}</SiteShell>;
}
