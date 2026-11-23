import { ArrowLeft, Cpu, HardDrive, type LucideIcon, MemoryStick } from "lucide-react";
import type { ReactNode } from "react";
import { Link, NavLink } from "react-router";
import { cn } from "@/lib/utils";

const tabs: { label: string; to: string; icon: LucideIcon }[] = [
  { label: "Load", to: "/system/load", icon: Cpu },
  { label: "Memory", to: "/system/memory", icon: MemoryStick },
  { label: "Disk", to: "/system/disk", icon: HardDrive },
];

/** Back to the dashboard, title, and Load / Memory / Disk tabs. */
export function SystemShell({ title, description, actions, children }: { title: string; description: string; actions?: ReactNode; children: ReactNode }) {
  return (
    <div className="mx-auto flex max-w-6xl flex-col gap-6">
      <div className="flex flex-col gap-4">
        <div>
          <Link to="/" className="mb-2 inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground">
            <ArrowLeft className="size-4" /> Dashboard
          </Link>
          <div className="flex flex-wrap items-end justify-between gap-3">
            <div>
              <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
              <p className="text-sm text-muted-foreground">{description}</p>
            </div>
            {actions}
          </div>
        </div>
        <nav aria-label="System" className="flex w-full gap-1 overflow-x-auto rounded-[16px] p-1.5 shadow-inset sm:w-fit">
          {tabs.map(({ label, to, icon: Icon }) => (
            <NavLink
              key={to}
              to={to}
              className={({ isActive }) =>
                cn(
                  "flex shrink-0 items-center gap-2 rounded-[12px] px-3.5 py-2 text-sm transition-all focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring",
                  isActive ? "bg-background font-medium text-primary shadow-raised-sm" : "text-muted-foreground hover:text-foreground",
                )
              }
            >
              <Icon className="size-4" />
              {label}
            </NavLink>
          ))}
        </nav>
      </div>
      {children}
    </div>
  );
}

/** A horizontal bar for a share (0–100) of something. */
export function ShareBar({ value, className }: { value: number; className?: string }) {
  const v = Math.max(0, Math.min(100, value));
  return (
    <div className="h-2 w-full min-w-16 overflow-hidden rounded-full bg-background p-[2px] shadow-inset">
      <div className={cn("h-full rounded-full bg-primary", className)} style={{ width: `${v}%` }} />
    </div>
  );
}
