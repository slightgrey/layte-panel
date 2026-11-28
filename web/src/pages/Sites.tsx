import { Lock, Plus, RefreshCw, Search } from "lucide-react";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router";
import { useMe } from "@/api/auth";
import { useScanSites, useSites } from "@/api/sites";
import type { Site } from "@/api/types";
import { Spinner } from "@/components/Spinner";
import { UsageCard } from "@/components/UsageCard";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { SiteBadges } from "./SiteBadges";

export function SitesPage() {
  const sites = useSites();
  const scan = useScanSites();
  const navigate = useNavigate();
  const [query, setQuery] = useState("");
  const autoScanned = useRef(false);
  const user = useMe().data?.user;
  const isAdmin = user?.role === "admin";
  // Site accounts add subdomain sites of their main site, up to their limit.
  const used = (sites.data?.sites ?? []).filter((s) => s.parent_site_id && s.parent_site_id === user?.site_id).length;
  const limit = user?.max_subdomains ?? 0;

  // First visit with an empty list: look for existing sites automatically.
  useEffect(() => {
    if (isAdmin && sites.data && sites.data.sites.length === 0 && !autoScanned.current) {
      autoScanned.current = true;
      scan.mutate();
    }
  }, [isAdmin, sites.data, scan]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    const list = nestSubdomains(sites.data?.sites ?? []);
    return q ? list.filter((s) => s.domain.includes(q) || s.aliases.some((a) => a.includes(q))) : list;
  }, [sites.data, query]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-bold tracking-tight">Sites</h1>
          <p className="text-sm text-muted-foreground">
            {isAdmin
              ? "Sites managed by Layte Panel and sites found in your nginx config."
              : `Your site and its subdomains: ${used} of ${limit} subdomain${limit === 1 ? "" : "s"} used.`}
          </p>
        </div>
        {isAdmin ? (
          <div className="flex gap-2">
            <Button variant="outline" onClick={() => scan.mutate()} disabled={scan.isPending}>
              <RefreshCw className={scan.isPending ? "animate-spin" : ""} /> Scan server
            </Button>
            <Button onClick={() => navigate("/sites/new")}>
              <Plus /> New site
            </Button>
          </div>
        ) : (
          <Button onClick={() => navigate("/sites/new")} disabled={!sites.data || used >= limit} title={used >= limit ? "Your account has reached its subdomain limit" : undefined}>
            <Plus /> Add subdomain
          </Button>
        )}
      </div>

      {!isAdmin && user && (
        <div className="md:hidden">
          <UsageCard />
        </div>
      )}

      {scan.data && (
        <p className="text-sm text-muted-foreground" role="status">
          Scan found {scan.data.found} site{scan.data.found === 1 ? "" : "s"} in nginx
          {scan.data.added > 0 && `, ${scan.data.added} new`}
          {scan.data.removed > 0 && `, ${scan.data.removed} no longer configured`}.
        </p>
      )}
      {scan.error && <p className="text-sm text-destructive">Scan failed: {scan.error.message}</p>}

      <Card>
        <CardContent className="pt-5">
          <div className="relative mb-4 max-w-xs">
            <Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
            <Input placeholder="Filter by domain" className="pl-8" value={query} onChange={(e) => setQuery(e.target.value)} />
          </div>
          {sites.isPending ? (
            <div className="grid h-32 place-items-center">
              <Spinner />
            </div>
          ) : sites.error ? (
            <p className="text-sm text-destructive">{sites.error.message}</p>
          ) : filtered.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">
              {query ? "No sites match your filter." : scan.isPending ? "Scanning…" : "No sites yet."}
            </p>
          ) : (
            <SitesTable sites={filtered} />
          )}
        </CardContent>
      </Card>
    </div>
  );
}

/** Each main site followed by its subdomain sites. */
function nestSubdomains(sites: Site[]): Site[] {
  const ids = new Set(sites.map((s) => s.id));
  const isChild = (s: Site) => Boolean(s.parent_site_id && ids.has(s.parent_site_id));
  return sites.filter((s) => !isChild(s)).flatMap((main) => [main, ...sites.filter((s) => s.parent_site_id === main.id)]);
}

function SitesTable({ sites }: { sites: Site[] }) {
  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>Domain</TableHead>
          <TableHead>PHP</TableHead>
          <TableHead>Web root</TableHead>
          <TableHead className="text-right">Status</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {sites.map((s) => (
          <TableRow key={s.id}>
            <TableCell>
              <Link to={`/sites/${s.id}`} className={s.parent_site_id ? "pl-4 font-medium hover:underline" : "font-medium hover:underline"}>
                {s.parent_site_id ? <span className="mr-1 text-muted-foreground">↳</span> : null}
                {s.domain}
              </Link>
              {s.aliases.length > 0 && <div className="text-xs text-muted-foreground">+ {s.aliases.join(", ")}</div>}
            </TableCell>
            <TableCell className="font-mono text-xs">{s.php_version || "–"}</TableCell>
            <TableCell className="max-w-64 truncate font-mono text-xs text-muted-foreground" title={s.root_path}>
              {s.root_path || "–"}
            </TableCell>
            <TableCell>
              <div className="flex flex-wrap justify-end gap-1.5">
                <SiteBadges site={s} inTable />
                {!s.managed && !s.adopted && (
                  <Badge title="Found on the server. Layte Panel won't change it.">
                    <Lock className="size-3" /> imported
                  </Badge>
                )}
              </div>
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
