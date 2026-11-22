import { cn } from "@/lib/utils";

/** A usage bar that turns amber at 75% and red at 90%. */
export function UsageBar({ value, label }: { value: number; label: string }) {
  const v = Math.max(0, Math.min(100, value));
  const color = v >= 90 ? "bg-destructive" : v >= 75 ? "bg-warning" : "bg-primary";
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuenow={v}
      aria-valuemin={0}
      aria-valuemax={100}
      className="h-2.5 w-full overflow-hidden rounded-full bg-background p-[2px] shadow-inset"
    >
      <div className={cn("h-full rounded-full transition-[width]", color)} style={{ width: `${v}%` }} />
    </div>
  );
}
