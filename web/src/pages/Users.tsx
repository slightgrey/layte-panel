import { ChevronDown, ChevronRight, Search } from "lucide-react";
import { Fragment, useMemo, useState } from "react";
import { Link } from "react-router";
import type { Account, SetupLink } from "@/api/types";
import { useUsers } from "@/api/users";
import { AccountRow, SetupLinkPanel, UsageRowsLive } from "@/components/PanelLoginCard";
import { QuotaCard } from "@/components/QuotaCard";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";

/** Admins: every panel login. Site accounts are created from a site's page. */
export function UsersPage() {
  const users = useUsers();
  const [shown, setShown] = useState<SetupLink | null>(null);
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState<number | null>(null);
  const admins = users.data?.users.filter((u) => u.role === "admin") ?? [];
  const all = useMemo(() => (users.data?.users ?? []).filter((u) => u.role === "site"), [users.data]);
  const accounts = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? all.filter((u) => (u.site_domain ?? "").includes(q) || u.username.includes(q)) : all;
  }, [all, query]);

  return (
    <div className="mx-auto flex max-w-5xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Users</h1>
        <p className="text-sm text-muted-foreground">
          Site logins see only their own site and its subdomains. To add one, open the site and use <strong>Panel login</strong>.
          New admins are added on the server with <code className="font-mono">layte admin create</code>; your own password and 2FA are on
          the Account page (gear icon).
        </p>
      </div>

      {shown && <SetupLinkPanel link={shown} onDismiss={() => setShown(null)} />}

      {users.isPending ? (
        <Spinner />
      ) : users.error ? (
        <p className="text-sm text-destructive">{users.error.message}</p>
      ) : (
        <>
          <Card>
            <CardContent className="pt-5">
              <div className="mb-4 flex flex-wrap items-center justify-between gap-3">
                <p className="eyebrow">Site logins ({all.length})</p>
                <div className="relative w-full max-w-xs">
                  <Search className="pointer-events-none absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
                  <Input placeholder="Search site or username" className="pl-8" value={query} onChange={(e) => setQuery(e.target.value)} />
                </div>
              </div>
              {accounts.length === 0 ? (
                <p className="py-8 text-center text-sm text-muted-foreground">{query ? "No site logins match your search." : "No site logins yet."}</p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead className="w-8" />
                      <TableHead>Site</TableHead>
                      <TableHead>Login</TableHead>
                      <TableHead>2FA</TableHead>
                      <TableHead className="text-right">Subdomains</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {accounts.map((a) => (
                      <AccountTableRow key={a.id} a={a} open={open === a.id} onToggle={() => setOpen(open === a.id ? null : a.id)} onLink={setShown} />
                    ))}
                  </TableBody>
                </Table>
              )}
            </CardContent>
          </Card>

          <QuotaCard />

          <Card>
            <CardContent className="flex flex-col gap-2 pt-5">
              <p className="eyebrow">Admins</p>
              {admins.map((a) => (
                <div key={a.id} className="flex items-center gap-2">
                  <span className="font-mono text-sm font-medium">{a.username}</span>
                  <Badge>admin</Badge>
                  {a.locked && <Badge variant="danger">locked</Badge>}
                </div>
              ))}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

/** One site login; ▸ opens its limits, actions and usage (usage loads only then). */
function AccountTableRow({ a, open, onToggle, onLink }: { a: Account; open: boolean; onToggle: () => void; onLink: (l: SetupLink) => void }) {
  const Chevron = open ? ChevronDown : ChevronRight;
  const linkLive = a.setup_expires ? new Date(a.setup_expires) > new Date() : false;
  return (
    <Fragment>
      <TableRow className="cursor-pointer" onClick={onToggle} aria-expanded={open}>
        <TableCell>
          <button
            type="button"
            aria-label={open ? "Hide details" : "Show details"}
            className="grid place-items-center text-muted-foreground"
            onClick={(e) => {
              e.stopPropagation();
              onToggle();
            }}
          >
            <Chevron className="size-4" />
          </button>
        </TableCell>
        <TableCell>
          {a.site_id ? (
            <Link to={`/sites/${a.site_id}`} className="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>
              {a.site_domain ?? `site ${a.site_id}`}
            </Link>
          ) : (
            "–"
          )}
        </TableCell>
        <TableCell>
          <span className="font-mono text-xs">{a.username}</span>
          {a.locked && (
            <Badge variant="danger" className="ml-2">
              locked
            </Badge>
          )}
          {a.setup_expires && (
            <Badge className="ml-2" variant={linkLive ? "neutral" : "danger"}>
              {linkLive ? "setup pending" : "link expired"}
            </Badge>
          )}
        </TableCell>
        <TableCell>{a.totp_set_up ? <Badge variant="success">on</Badge> : <Badge variant="warning">off</Badge>}</TableCell>
        <TableCell className="text-right tabular-nums">
          {a.subdomains} / {a.max_subdomains}
        </TableCell>
      </TableRow>
      {open && (
        <TableRow className="hover:bg-transparent">
          <TableCell colSpan={5} className="bg-muted/30">
            <div className="flex flex-col gap-4 py-2">
              <AccountRow account={a} onLink={onLink} />
              <UsageRowsLive userId={a.id} />
            </div>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  );
}
