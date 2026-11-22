import type { Site } from "@/api/types";
import { Badge } from "@/components/ui/badge";

/** Status + feature badges shared by the list and the detail page. */
export function SiteBadges({ site }: { site: Site }) {
  return (
    <>
      {site.status === "creating" && <Badge variant="warning">creating…</Badge>}
      {site.status === "deleting" && <Badge variant="warning">deleting…</Badge>}
      {site.status === "failed" && <Badge variant="danger">failed</Badge>}
      {site.adopted && <Badge variant="success">adopted</Badge>}
      {site.wordpress && <Badge>WordPress</Badge>}
      {site.ssl ? <Badge variant="success">HTTPS</Badge> : <Badge>HTTP only</Badge>}
    </>
  );
}
