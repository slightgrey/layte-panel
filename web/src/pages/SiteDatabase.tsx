import { Copy, Database, Eye, KeyRound, Plus } from "lucide-react";
import type { ReactNode } from "react";
import type { Site } from "@/api/types";
import { EmptyState, SiteShell } from "@/components/SiteShell";
import { Button } from "@/components/ui/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

// UI only for now: buttons are disabled until the endpoints exist.
export function SiteDatabasePage() {
  return <SiteShell title="Database" preview>{(site) => (site.db_name ? <DatabaseCards site={site} /> : <NoDatabase />)}</SiteShell>;
}

function NoDatabase() {
  return (
    <Card>
      <CardContent className="pt-6">
        <EmptyState icon={Database} title="No database for this site">
          Static sites don't need one. You can add a MySQL database and user for this site here.
          <div className="mt-4">
            <Button disabled>
              <Plus /> Create database
            </Button>
          </div>
        </EmptyState>
      </CardContent>
    </Card>
  );
}

function DatabaseCards({ site }: { site: Site }) {
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <Card className="lg:col-span-2">
        <CardHeader>
          <CardTitle>Connection</CardTitle>
        </CardHeader>
        <CardContent>
          <dl className="divide-y">
            <Row label="Database">
              <Mono>{site.db_name}</Mono>
            </Row>
            <Row label="User">
              <Mono>{site.db_user}</Mono>
            </Row>
            <Row label="Host">
              <Mono>localhost</Mono>
            </Row>
            <Row label="Password">
              <span className="flex items-center gap-2">
                <Mono>••••••••••••</Mono>
                <Button variant="ghost" size="sm" disabled aria-label="Show password">
                  <Eye />
                </Button>
                <Button variant="ghost" size="sm" disabled aria-label="Copy password">
                  <Copy />
                </Button>
              </span>
            </Row>
          </dl>
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Usage</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <Stat label="Size" value="–" />
          <Stat label="Tables" value="–" />
          <Stat label="Charset" value="utf8mb4" />
        </CardContent>
      </Card>

      <Card className="lg:col-span-3">
        <CardHeader>
          <CardTitle>Actions</CardTitle>
        </CardHeader>
        <CardContent className="flex flex-wrap gap-3">
          <Button variant="outline" disabled>
            <KeyRound /> Reset password
          </Button>
          <Button variant="outline" disabled>
            Export (.sql)
          </Button>
          <Button variant="outline" disabled>
            Import (.sql)
          </Button>
        </CardContent>
      </Card>
    </div>
  );
}

function Row({ label, children }: { label: string; children: ReactNode }) {
  return (
    <div className="grid gap-1 py-2.5 sm:grid-cols-[10rem_1fr] sm:items-center">
      <dt className="text-sm text-muted-foreground">{label}</dt>
      <dd className="text-sm">{children}</dd>
    </div>
  );
}

function Mono({ children }: { children: ReactNode }) {
  return <span className="font-mono text-xs">{children}</span>;
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-baseline justify-between gap-2">
      <span className="eyebrow">{label}</span>
      <span className="font-mono text-sm">{value}</span>
    </div>
  );
}
