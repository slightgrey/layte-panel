import type { Certificate } from "@/api/types";
import { Badge } from "@/components/ui/badge";

/** "expires in 12 days" badge, coloured by state. */
export function CertBadge({ cert }: { cert: Certificate }) {
  if (cert.state === "expired") return <Badge variant="danger">expired</Badge>;
  const text = `${cert.days_left} day${cert.days_left === 1 ? "" : "s"} left`;
  return <Badge variant={cert.state === "expiring" ? "warning" : "success"}>{text}</Badge>;
}
