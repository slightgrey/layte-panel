import { FlaskConical, RefreshCw, ShieldCheck, ShieldOff } from "lucide-react";
import { type FormEvent, type ReactNode, useEffect, useState } from "react";
import { layteConfigs } from "@/api/sites";
import { useCertificates, useIssueSSL, useRemoveSSL, useRenewSSL } from "@/api/ssl";
import type { Site } from "@/api/types";
import { CertBadge } from "@/components/CertBadge";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Dialog } from "@/components/ui/dialog";
import { Field } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { SiteShell } from "@/components/SiteShell";
import { FormError } from "./AuthShell";
import { JobButtonLabel, useJobFeedback } from "@/components/JobFeedback";

/** HTTPS card on the site page: status, and issue/remove for managed sites. */
export function SiteSSLCard({ site }: { site: Site }) {
  const certs = useCertificates();
  const cert = certs.data?.certificates.find((c) => c.site_id === site.id);
  const issueJob = useJobFeedback(`ssl-issue-${site.id}`, `Getting a certificate for ${site.domain}`);
  const removeJob = useJobFeedback(`ssl-remove-${site.id}`, `Removing HTTPS from ${site.domain}`);
  const switchJob = useJobFeedback(`ssl-switch-${site.id}`, `Switching ${site.domain} to a Layte certificate`);
  const issue = useIssueSSL(site.id);
  const remove = useRemoveSSL(site.id);
  const [email, setEmail] = useState("");
  useEffect(() => {
    if (certs.data?.email && !email) setEmail(certs.data.email);
  }, [certs.data, email]);

  const busy = site.status !== "active";
  // A taken-over adopted site still on its own certificate can switch to a Layte one.
  const canSwitch = site.ssl && site.adopted && site.nginx_managed && !!cert && !cert.managed;
  const [deleteOld, setDeleteOld] = useState(true);

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
            {layteConfigs(site) && (
              <Button variant="outline" size="sm" disabled={busy || remove.isPending} onClick={() => remove.mutate(undefined, { onSuccess: removeJob.start })}>
                <JobButtonLabel state={removeJob.state}>
                  <ShieldOff /> Remove HTTPS
                </JobButtonLabel>
              </Button>
            )}
          </div>
        ) : layteConfigs(site) ? (
          <form
            className="flex flex-col gap-3"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              issue.mutate({ email: email.trim() }, { onSuccess: issueJob.start });
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
                <JobButtonLabel state={issueJob.state}>
                  <ShieldCheck /> Enable HTTPS
                </JobButtonLabel>
              </Button>
            </div>
            <FormError message={issue.error?.message} />
          </form>
        ) : (
          <p className="text-sm text-muted-foreground">This site serves HTTP only.</p>
        )}
        {canSwitch && (
          <form
            className="flex flex-col gap-3 border-t pt-4"
            onSubmit={(e: FormEvent) => {
              e.preventDefault();
              issue.mutate({ email: email.trim(), delete_old_cert: deleteOld }, { onSuccess: switchJob.start });
            }}
          >
            <p className="text-sm text-muted-foreground">
              This site still uses its own certificate <code className="font-mono">{cert.name}</code>. Switch to a Layte
              certificate so Layte Panel renews it.
            </p>
            <div className="flex flex-wrap items-end gap-3">
              <div className="min-w-64 flex-1">
                <Field label="Email for expiry notices" htmlFor="le-email-switch">
                  <Input id="le-email-switch" type="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
                </Field>
              </div>
              <Button type="submit" disabled={busy || issue.isPending}>
                <JobButtonLabel state={switchJob.state}>
                  <ShieldCheck /> Switch to a Layte certificate
                </JobButtonLabel>
              </Button>
            </div>
            <label className="flex items-start gap-2 text-sm">
              <input type="checkbox" className="mt-1" checked={deleteOld} onChange={(e) => setDeleteOld(e.target.checked)} />
              <span>
                Then delete the old certificate <code className="font-mono">{cert.name}</code>, so certbot stops renewing it.
                It's kept if nginx still uses it anywhere.
              </span>
            </label>
            <FormError message={issue.error?.message} />
          </form>
        )}
        <FormError message={remove.error?.message} />
      </CardContent>
    </Card>
  );
}

/** /sites/:id/ssl — HTTPS status, certificate details and renewal. */
export function SiteSSLPage() {
  return (
    <SiteShell title="SSL certificate">
      {(site) => (
        <>
          <SiteSSLCard site={site} />
          <CertificateDetails site={site} />
        </>
      )}
    </SiteShell>
  );
}

/** Everything about the certificate in use, plus renew / test renewal for Layte certificates. */
function CertificateDetails({ site }: { site: Site }) {
  const certs = useCertificates();
  const cert = certs.data?.certificates.find((c) => c.site_id === site.id);
  if (!cert) return null;
  const date = (s: string) => new Date(s).toLocaleString();
  return (
    <>
      <Card>
        <CardHeader>
          <CardTitle>Certificate</CardTitle>
          {!cert.managed && (
            <span className="text-xs text-muted-foreground">
              not issued by Layte Panel{site.adopted && !site.nginx_managed ? " · renewable" : " · read-only"}
            </span>
          )}
        </CardHeader>
        <CardContent>
          <dl className="divide-y">
            <Row label="Domains">{cert.domains.join(", ")}</Row>
            <Row label="Issuer">{cert.issuer || "–"}</Row>
            <Row label="Valid from">{date(cert.not_before)}</Row>
            <Row label="Expires">
              <span className="flex flex-wrap items-center gap-2">
                {date(cert.not_after)} <CertBadge cert={cert} />
              </span>
            </Row>
            <Row label="Auto-renewal">
              {cert.auto_renew ? "Yes. certbot renews it 30 days before it expires." : <span className="text-warning">No renewal config found</span>}
            </Row>
            <Row label="Key">{cert.key_type}</Row>
            <Row label="Signature">{cert.signature}</Row>
            <Row label="Serial">
              <Mono>{cert.serial}</Mono>
            </Row>
            <Row label="SHA-256">
              <Mono>{cert.fingerprint}</Mono>
            </Row>
            <Row label="Certbot name">
              <Mono>{cert.name}</Mono>
            </Row>
            <Row label="File">
              <Mono>{cert.path}</Mono>
            </Row>
          </dl>
        </CardContent>
      </Card>
      {site.ssl && ((cert.managed && layteConfigs(site)) || (site.adopted && !site.nginx_managed && cert.auto_renew)) && <RenewCard site={site} />}
    </>
  );
}

function RenewCard({ site }: { site: Site }) {
  const renew = useRenewSSL(site.id);
  const job = useJobFeedback(`ssl-renew-${site.id}`, "Renewing the certificate");
  const [confirmOpen, setConfirmOpen] = useState(false);
  const busy = site.status !== "active" || renew.isPending;
  const start = (dryRun: boolean) =>
    renew.mutate(dryRun, {
      onSuccess: (r) => {
        setConfirmOpen(false);
        job.start(r);
      },
    });
  return (
    <Card>
      <CardHeader>
        <CardTitle>Renewal</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4">
        <p className="text-sm text-muted-foreground">
          Renewal is automatic. <strong>Test renewal</strong> asks Let's Encrypt's staging server whether renewing would work,
          without changing anything. <strong>Renew now</strong> gets a fresh certificate right away.
        </p>
        <div className="flex flex-wrap gap-3">
          <Button variant="outline" disabled={busy} onClick={() => start(true)}>
            <FlaskConical /> Test renewal
          </Button>
          <Button variant="outline" disabled={busy} onClick={() => setConfirmOpen(true)}>
            <RefreshCw /> Renew now
          </Button>
        </div>
        <FormError message={renew.error?.message} />
        <Dialog open={confirmOpen} onClose={() => setConfirmOpen(false)} title="Renew the certificate now?">
          <div className="flex flex-col gap-4">
            <p className="text-sm text-muted-foreground">
              Let's Encrypt allows 5 certificates for the same set of domains per week. Only renew by hand if something is
              wrong with the current certificate; otherwise use <strong>Test renewal</strong>.
            </p>
            <FormError message={renew.error?.message} />
            <div className="flex justify-end gap-2">
              <Button type="button" variant="outline" onClick={() => setConfirmOpen(false)}>
                Cancel
              </Button>
              <Button disabled={busy} onClick={() => start(false)}>
                <RefreshCw /> Renew now
              </Button>
            </div>
          </div>
        </Dialog>
      </CardContent>
    </Card>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-center">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="min-w-0 text-sm break-all">{children}</dd>
    </div>
  );
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono text-xs">{children}</span>;
}
