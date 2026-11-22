import { useEffect } from "react";
import { useSearchParams } from "react-router";
import { useLogSources } from "@/api/logs";
import { LogViewer } from "@/components/LogViewer";
import { Spinner } from "@/components/Spinner";

export function LogsPage() {
  const sources = useLogSources();
  const [params, setParams] = useSearchParams();
  const path = params.get("path");

  // Pick a default: ?site=<id> → that site's first log, else the first source.
  useEffect(() => {
    const list = sources.data?.sources;
    if (!list?.length || path) return;
    const site = Number(params.get("site"));
    const pick = (site && list.find((s) => s.site_id === site)) || list[0]!;
    setParams({ path: pick.path }, { replace: true });
  }, [sources.data, path, params, setParams]);

  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-4">
      <div>
        <h1 className="text-2xl font-bold tracking-tight">Logs</h1>
        <p className="text-sm text-muted-foreground">Live view of nginx, PHP-FPM and site logs.</p>
      </div>

      {sources.isPending ? (
        <Spinner />
      ) : sources.error ? (
        <p className="text-sm text-destructive">{sources.error.message}</p>
      ) : (
        <LogViewer sources={sources.data.sources} path={path} onPathChange={(p) => setParams({ path: p })} />
      )}
    </div>
  );
}
