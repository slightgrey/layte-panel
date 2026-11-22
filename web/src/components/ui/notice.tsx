import { AlertTriangle, Info, type LucideIcon, XCircle } from "lucide-react";
import type { HTMLAttributes, ReactNode } from "react";
import { cn } from "@/lib/utils";

type Tone = "info" | "warning" | "danger";

// Raised (not inset) with a tint and a solid stripe, so notices stand out
// from the neumorphic page instead of sinking into it.
const tones: Record<Tone, { box: string; icon: string; Icon: LucideIcon }> = {
  info: { box: "border-warning bg-warning/15", icon: "text-warning", Icon: Info },
  warning: { box: "border-warning bg-warning/15", icon: "text-warning", Icon: AlertTriangle },
  danger: { box: "border-destructive bg-destructive/12", icon: "text-destructive", Icon: XCircle },
};

/** A message box that's easy to spot: info and warnings in yellow, blocking problems in red. */
export function Notice({
  tone = "info",
  icon,
  action,
  className,
  children,
  ...props
}: { tone?: Tone; icon?: LucideIcon; action?: ReactNode } & HTMLAttributes<HTMLDivElement>) {
  const t = tones[tone];
  const Icon = icon ?? t.Icon;
  return (
    <div
      role={props.role ?? (tone === "info" ? "status" : "alert")}
      className={cn("flex flex-wrap items-start gap-3 rounded-[14px] border-l-4 px-4 py-3 text-sm shadow-raised-sm", t.box, className)}
      {...props}
    >
      <Icon className={cn("mt-0.5 size-4 shrink-0", t.icon)} />
      <div className="min-w-0 flex-1">{children}</div>
      {action && <div className="shrink-0">{action}</div>}
    </div>
  );
}

/** Frame for live job progress: the primary blue (#3f6f99 in light mode). */
export const progressFrame = "border-l-4 border-primary bg-primary/[0.08]";
