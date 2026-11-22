import { FolderOpen, Lock } from "lucide-react";
import { Link } from "react-router";
import { useSites } from "@/api/sites";
import { Spinner } from "@/components/Spinner";
import { Card, CardContent } from "@/components/ui/card";

/** Pick a site to open its file manager. */
export function FilesPage() {
  const sites = useSites();
  const list = (sites.data?.sites ?? []).filter((s) => s.status === "active" && (s.managed || s.root_path));
  return (
    <div className="mx-auto flex max-w-4xl flex-col gap-6">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Files</h1>
        <p className="text-sm text-muted-foreground">
          Browse and edit a site's files. Changes are made as the site's own Linux user, inside the site folder only.
        </p>
      </div>
      <Card>
        <CardContent className="pt-5">
          {sites.isPending ? (
            <Spinner />
          ) : list.length === 0 ? (
            <p className="py-8 text-center text-sm text-muted-foreground">No sites yet.</p>
          ) : (
            <ul className="divide-y">
              {list.map((s) => (
                <li key={s.id}>
                  <Link to={`/sites/${s.id}/files`} className="flex items-center gap-3 py-3 hover:underline">
                    <FolderOpen className="size-4 text-primary" />
                    <span className="font-medium">{s.domain}</span>
                    {!s.managed && (
                      <span className="flex items-center gap-1 text-xs text-muted-foreground">
                        <Lock className="size-3" /> read-only
                      </span>
                    )}
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </CardContent>
      </Card>
    </div>
  );
}
