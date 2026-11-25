import { Info } from "lucide-react";
import type { Site } from "@/api/types";
import { Badge } from "@/components/ui/badge";
import { Tooltip } from "@/components/ui/tooltip";

/** What "adopted" means for this site (the adopted badge's tooltip). */
function adoptedInfo(site: Site): string {
  return site.nginx_managed
    ? `This site runs as ${site.system_user}. Layte Panel manages its nginx config and PHP-FPM pool; its files stay in ${site.root_path}.`
    : `This site runs as ${site.system_user} with a Layte-managed PHP-FPM pool. Its nginx config is still yours and is never rewritten by Layte Panel; its files stay in ${site.root_path}.`;
}

/**
 * Status + feature badges shared by the list and the detail page. In tables
 * (which scroll sideways and would clip a pop-up), the adopted badge uses the
 * browser's own tooltip instead.
 */
export function SiteBadges({ site, inTable = false }: { site: Site; inTable?: boolean }) {
  const adopted = (
    <Badge variant="success" title={inTable ? adoptedInfo(site) : undefined}>
      adopted <Info className="size-3" aria-hidden="true" />
    </Badge>
  );
  return (
    <>
      {site.status === "creating" && <Badge variant="warning">creating…</Badge>}
      {site.status === "deleting" && <Badge variant="warning">deleting…</Badge>}
      {site.status === "failed" && <Badge variant="danger">failed</Badge>}
      {site.adopted && (inTable ? adopted : <Tooltip content={adoptedInfo(site)}>{adopted}</Tooltip>)}
      {site.wordpress && <Badge>WordPress</Badge>}
      {site.ssl ? <Badge variant="success">HTTPS</Badge> : <Badge>HTTP only</Badge>}
    </>
  );
}
