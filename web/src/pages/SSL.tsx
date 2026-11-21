import { AlertTriangle } from "lucide-react";
import { Link } from "react-router";
import { useCertificates } from "@/api/ssl";
import { CertBadge } from "@/components/CertBadge";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

export function SSLPage() {
  const certs = useCertificates();
  const problems = certs.data?.certificates.filter((c) => c.state !== "ok") ?? [];

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div>
        <h1 className="text-xl font-semibold tracking-tight">SSL certificates</h1>
        <p className="text-sm text-muted-foreground">
          Every Let's Encrypt certificate on this server. certbot renews them automatically 30 days before they expire.
          To enable HTTPS for a site, open the site.
        </p>
      </div>

      {problems.length > 0 && (
        <div role="alert" className="flex items-start gap-2 rounded-lg border border-warning/40 bg-warning/10 px-4 py-3 text-sm">
          <AlertTriangle className="mt-0.5 size-4 shrink-0 text-warning" />
          <p>
            {problems.length} certificate{problems.length > 1 ? "s are" : " is"} expiring soon or expired, which usually
            means renewal is failing. Check <code className="font-mono">certbot renew --dry-run</code> and that the
            domain's DNS still points here.
          </p>
        </div>
      )}

      <Card>
        <CardContent className="pt-5">
          {certs.isPending ? (
            <Spinner />
          ) : certs.error ? (
            <p className="text-sm text-destructive">{certs.error.message}</p>
          ) : certs.data.certificates.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">No certificates on this server yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Domains</TableHead>
                  <TableHead>Site</TableHead>
                  <TableHead>Expires</TableHead>
                  <TableHead className="text-right">Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {certs.data.certificates.map((c) => (
                  <TableRow key={c.name}>
                    <TableCell>
                      <div className="font-medium">{c.domains[0]}</div>
                      {c.domains.length > 1 && <div className="text-xs text-muted-foreground">+ {c.domains.slice(1).join(", ")}</div>}
                    </TableCell>
                    <TableCell>
                      {c.site_id ? (
                        <Link to={`/sites/${c.site_id}`} className="hover:underline">
                          {c.site_domain}
                        </Link>
                      ) : (
                        <span className="text-muted-foreground">–</span>
                      )}
                    </TableCell>
                    <TableCell className="text-xs tabular-nums">{new Date(c.not_after).toLocaleDateString()}</TableCell>
                    <TableCell>
                      <div className="flex justify-end gap-1.5">
                        {!c.managed && <Badge>not Layte</Badge>}
                        <CertBadge cert={c} />
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
