import {
  Clock,
  Database,
  FolderOpen,
  Globe,
  KeyRound,
  LayoutDashboard,
  LogOut,
  type LucideIcon,
  ScrollText,
  Shield,
  ShieldCheck,
} from "lucide-react";
import { NavLink, Outlet, useLocation, useNavigate } from "react-router";
import { useLogout, useMe } from "@/api/auth";
import { formatVersion, useHealth } from "@/api/health";
import { ThemeToggle } from "@/components/ThemeToggle";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";

interface NavItem {
  label: string;
  to?: string; // undefined = not built yet
  icon: LucideIcon;
}

const nav: NavItem[] = [
  { label: "Dashboard", to: "/", icon: LayoutDashboard },
  { label: "Sites", to: "/sites", icon: Globe },
  { label: "SSL", to: "/ssl", icon: ShieldCheck },
  { label: "Databases", to: "/databases", icon: Database },
  { label: "Cron", to: "/cron", icon: Clock },
  { label: "Logs", to: "/logs", icon: ScrollText },
  { label: "Files", to: "/files", icon: FolderOpen },
  { label: "Security", to: "/security", icon: Shield },
  { label: "API keys", to: "/keys", icon: KeyRound },
];

export function Layout() {
  const me = useMe();
  const logout = useLogout();
  const navigate = useNavigate();

  const onLogout = () => logout.mutate(undefined, { onSettled: () => navigate("/login", { replace: true }) });

  return (
    <div className="flex min-h-svh gap-6 p-3 md:p-5">
      <aside className="sticky top-5 hidden h-[calc(100svh-2.5rem)] w-60 shrink-0 flex-col rounded-[1.5rem] bg-card shadow-raised md:flex">
        <Brand className="h-16 px-5" />
        <p className="eyebrow px-6 pt-1 pb-2">Server</p>
        <nav className="flex flex-1 flex-col gap-1.5 overflow-y-auto px-3 pb-3" aria-label="Main">
          {nav.map((item) => (
            <NavEntry key={item.label} item={item} />
          ))}
        </nav>
        <VersionFooter />
      </aside>

      <div className="flex min-w-0 flex-1 flex-col gap-6">
        <header className="sticky top-3 z-40 flex h-14 items-center justify-between gap-3 rounded-[1.25rem] bg-card/95 px-4 shadow-raised backdrop-blur md:top-5 md:px-5">
          <Brand className="md:hidden" />
          <PromptPath />
          <div className="flex items-center gap-2 md:gap-3">
            <ThemeToggle />
            <span className="hidden rounded-full px-3 py-1 font-mono text-xs text-muted-foreground shadow-inset sm:inline">
              {me.data?.user.username ?? "…"}
            </span>
            <Button variant="ghost" size="sm" onClick={onLogout} disabled={logout.isPending} aria-label="Log out">
              <LogOut /> <span className="hidden sm:inline">Log out</span>
            </Button>
          </div>
        </header>

        <nav className="-mx-1 flex gap-2 overflow-x-auto px-1 pb-2 md:hidden" aria-label="Main (mobile)">
          {nav.map((item) => (
            <NavEntry key={item.label} item={item} compact />
          ))}
        </nav>

        <main className="flex-1 pb-6 md:px-2">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

/** Terminal-style location, e.g. "~/sites/example.com_". */
function PromptPath() {
  const { pathname } = useLocation();
  const path = pathname === "/" ? "~" : `~${decodeURIComponent(pathname)}`;
  return (
    <p className="hidden min-w-0 truncate font-mono text-sm text-muted-foreground md:block">
      <span className="text-primary">layte</span>
      <span className="opacity-60">@panel</span>
      <span className="opacity-60">:</span>
      <span className="text-foreground">{path}</span>
      <span className="opacity-60">$</span>
      <span className="caret ml-1" />
    </p>
  );
}

function NavEntry({ item, compact = false }: { item: NavItem; compact?: boolean }) {
  const Icon = item.icon;
  const base = compact
    ? "flex shrink-0 items-center gap-2 rounded-full px-3.5 py-1.5 text-sm transition-all"
    : "flex items-center gap-3 rounded-[12px] px-3 py-2 text-sm transition-all";
  if (!item.to) {
    return (
      <span className={cn(base, "cursor-default text-muted-foreground/60")} title="Coming in a later release">
        <Icon className="size-4" />
        {item.label}
        <span className="ml-auto text-[0.6875rem] uppercase tracking-wide">soon</span>
      </span>
    );
  }
  return (
    <NavLink
      to={item.to}
      end={item.to === "/"}
      className={({ isActive }) =>
        cn(
          base,
          isActive
            ? "bg-background font-medium text-primary shadow-inset"
            : cn("text-muted-foreground hover:text-foreground", compact ? "bg-card shadow-raised-sm" : "hover:bg-muted/60"),
        )
      }
    >
      <Icon className="size-4" />
      {item.label}
    </NavLink>
  );
}

const RELEASES_URL = "https://github.com/slightgrey/layte-panel/releases";

/** Bottom of the sidebar: the running version, linked to its release notes. */
function VersionFooter() {
  const health = useHealth();
  const v = health.data?.version;
  if (!v) return null;
  const isRelease = /^\d+\.\d+\.\d+$/.test(v);
  const label = formatVersion(v);
  return (
    <div className="mx-3 mb-3 rounded-[12px] px-3 py-2.5 font-mono text-[0.75rem] text-muted-foreground shadow-inset">
      {isRelease ? (
        <a
          href={`${RELEASES_URL}/tag/${label}`}
          target="_blank"
          rel="noreferrer"
          className="hover:text-foreground hover:underline"
          title="Release notes"
        >
          Layte Panel {label}
        </a>
      ) : (
        <span title="Development build">Layte Panel {label}</span>
      )}
    </div>
  );
}

export function Brand({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-3 font-semibold tracking-tight", className)}>
      <span className="grid size-9 place-items-center rounded-[12px] bg-background shadow-raised-sm">
        <img src="/favicon.svg" alt="" className="size-5" />
      </span>
      Layte Panel
    </div>
  );
}
