import {
  Clock,
  Database,
  FolderOpen,
  Globe,
  LayoutDashboard,
  LogOut,
  type LucideIcon,
  ScrollText,
  Shield,
  ShieldCheck,
} from "lucide-react";
import { NavLink, Outlet, useNavigate } from "react-router";
import { useLogout, useMe } from "@/api/auth";
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
  { label: "Files", icon: FolderOpen },
  { label: "Security", icon: Shield },
];

export function Layout() {
  const me = useMe();
  const logout = useLogout();
  const navigate = useNavigate();

  const onLogout = () => logout.mutate(undefined, { onSettled: () => navigate("/login", { replace: true }) });

  return (
    <div className="flex min-h-svh">
      <aside className="hidden w-56 shrink-0 flex-col border-r bg-card md:flex">
        <Brand className="h-14 px-5" />
        <nav className="flex flex-1 flex-col gap-0.5 px-3 py-2" aria-label="Main">
          {nav.map((item) => (
            <NavEntry key={item.label} item={item} />
          ))}
        </nav>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-14 items-center justify-between gap-4 border-b bg-card px-4 md:px-8">
          <Brand className="md:hidden" />
          <span className="hidden md:block" />
          <div className="flex items-center gap-3">
            <span className="text-sm text-muted-foreground">{me.data?.user.username}</span>
            <Button variant="ghost" size="sm" onClick={onLogout} disabled={logout.isPending}>
              <LogOut /> Log out
            </Button>
          </div>
        </header>
        <main className="flex-1 px-4 py-6 md:px-8">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

function NavEntry({ item }: { item: NavItem }) {
  const Icon = item.icon;
  const base = "flex items-center gap-2.5 rounded-md px-2.5 py-1.5 text-sm";
  if (!item.to) {
    return (
      <span className={cn(base, "cursor-default text-muted-foreground/60")} title="Coming in a later release">
        <Icon className="size-4" />
        {item.label}
        <span className="ml-auto text-[10px] uppercase tracking-wide">soon</span>
      </span>
    );
  }
  return (
    <NavLink
      to={item.to}
      end={item.to === "/"}
      className={({ isActive }) =>
        cn(base, isActive ? "bg-muted font-medium text-foreground" : "text-muted-foreground hover:bg-muted hover:text-foreground")
      }
    >
      <Icon className="size-4" />
      {item.label}
    </NavLink>
  );
}

export function Brand({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2 font-semibold tracking-tight", className)}>
      <img src="/favicon.svg" alt="" className="size-6" />
      Layte Panel
    </div>
  );
}
