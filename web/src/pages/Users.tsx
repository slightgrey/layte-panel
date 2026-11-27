import { Link } from "react-router";
import { useState } from "react";
import { useUsers } from "@/api/users";
import { OneTimeSecrets } from "@/components/JobProgress";
import { AccountRow } from "@/components/PanelLoginCard";
import { Spinner } from "@/components/Spinner";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";

/** Admins: every panel login. Site accounts are created from a site's page. */
export function UsersPage() {
  const users = useUsers();
  const [shown, setShown] = useState<{ username: string; password: string } | null>(null);
  const admins = users.data?.users.filter((u) => u.role === "admin") ?? [];
  const accounts = users.data?.users.filter((u) => u.role === "site") ?? [];

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

      {shown && <OneTimeSecrets secrets={{ login_username: shown.username, login_password: shown.password, login_url: window.location.origin + "/login" }} onDismiss={() => setShown(null)} />}

      {users.isPending ? (
        <Spinner />
      ) : users.error ? (
        <p className="text-sm text-destructive">{users.error.message}</p>
      ) : (
        <>
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
          <Card>
            <CardContent className="flex flex-col gap-5 pt-5">
              <p className="eyebrow">Site logins</p>
              {accounts.length === 0 ? (
                <p className="py-6 text-center text-sm text-muted-foreground">No site logins yet.</p>
              ) : (
                accounts.map((a) => (
                  <div key={a.id} className="flex flex-col gap-2 border-b pb-5 last:border-b-0 last:pb-0">
                    {a.site_id && (
                      <Link to={`/sites/${a.site_id}`} className="text-sm font-medium text-primary hover:underline">
                        {a.site_domain ?? `site ${a.site_id}`}
                      </Link>
                    )}
                    <AccountRow account={a} onPassword={(password) => setShown({ username: a.username, password })} />
                  </div>
                ))
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
