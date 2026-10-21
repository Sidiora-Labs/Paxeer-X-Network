"use client";

import * as React from "react";
import { Home, Bot, Activity, Grid2x2, Search, Bell, Settings, Compass, Plus } from "lucide-react";
import { cn } from "../lib/utils";
import { usePlatform, type PlatformSetting } from "../lib/platform";
import { Avatar } from "./avatar";
import { IconButton } from "./button";

export interface NavItem {
  id: string;
  label: string;
  icon?: React.ReactNode;
  /** Numeric badge (e.g. pending approvals on Activity). */
  badge?: number;
}

const DEFAULT_ICONS: Record<string, React.ReactNode> = {
  home: <Home className="size-[22px]" />,
  agents: <Bot className="size-[22px]" />,
  activity: <Activity className="size-[22px]" />,
  more: <Grid2x2 className="size-[22px]" />,
};

/* ----------------------------------------------------------- BottomTabBar */

/**
 * Mobile navigation: bottom tab bar (Home, Agents, Activity, More) with a
 * raised center action button at thumb reach.
 */
export function BottomTabBar({
  items,
  activeId,
  onNavigate,
  onFab,
  fabIcon,
  className,
}: {
  items: NavItem[];
  activeId: string;
  onNavigate?: (id: string) => void;
  onFab?: () => void;
  fabIcon?: React.ReactNode;
  className?: string;
}) {
  const left = items.slice(0, 2);
  const right = items.slice(2, 4);

  const Tab = ({ item }: { item: NavItem }) => {
    const active = item.id === activeId;
    return (
      <button
        type="button"
        onClick={() => onNavigate?.(item.id)}
        aria-current={active ? "page" : undefined}
        className="relative flex flex-col items-center gap-0.5 py-1 outline-none"
      >
        <span className={cn("transition-colors", active ? "text-accent" : "text-faint-foreground")}>
          {item.icon ?? DEFAULT_ICONS[item.id] ?? <Grid2x2 className="size-[22px]" />}
        </span>
        <span
          className={cn(
            "text-[11px] font-semibold transition-colors",
            active ? "text-accent" : "text-faint-foreground",
          )}
        >
          {item.label}
        </span>
        {!!item.badge && (
          <span className="absolute -top-1 right-1/2 translate-x-4 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-bold text-destructive-foreground">
            {item.badge}
          </span>
        )}
      </button>
    );
  };

  return (
    <nav
      aria-label="Primary"
      className={cn(
        "relative z-30 grid grid-cols-5 items-end border-t border-border bg-surface px-2 pt-1.5 pb-[max(0.5rem,env(safe-area-inset-bottom))]",
        className,
      )}
    >
      <Tab item={left[0]} />
      <Tab item={left[1]} />
      <div className="flex justify-center">
        <button
          type="button"
          onClick={onFab}
          aria-label="New action"
          className="-mt-7 inline-flex size-14 items-center justify-center rounded-full bg-accent text-accent-foreground shadow-overlay transition-transform active:scale-95"
        >
          {fabIcon ?? <Plus className="size-6" />}
        </button>
      </div>
      <Tab item={right[0]} />
      <Tab item={right[1]} />
    </nav>
  );
}

/* ---------------------------------------------------------------- Sidebar */

/**
 * Desktop navigation: left sidebar with product nav (badge support for
 * approvals), an Explorer link, and a settings footer.
 */
export function Sidebar({
  items,
  activeId,
  onNavigate,
  logo,
  explorerLabel = "Explorer",
  onExplorer,
  settingsLabel = "Settings",
  onSettings,
  user,
  className,
}: {
  items: NavItem[];
  activeId: string;
  onNavigate?: (id: string) => void;
  logo?: React.ReactNode;
  explorerLabel?: string;
  onExplorer?: () => void;
  settingsLabel?: string;
  onSettings?: () => void;
  user?: { name: string; subtitle?: string; avatarSrc?: string };
  className?: string;
}) {
  return (
    <aside
      className={cn(
        "flex h-full w-[248px] shrink-0 flex-col border-r border-border bg-surface",
        className,
      )}
    >
      <div className="flex h-16 items-center px-5 text-lg font-extrabold tracking-tight text-foreground">
        {logo ?? (
          <span className="flex items-center gap-2">
            <span className="inline-flex size-7 items-center justify-center rounded-md bg-primary text-[13px] font-black text-primary-foreground">
              LX
            </span>
            LayerX
          </span>
        )}
      </div>

      <nav aria-label="Primary" className="flex flex-1 flex-col gap-1 px-3 py-2">
        {items.map((item) => {
          const active = item.id === activeId;
          return (
            <button
              key={item.id}
              type="button"
              onClick={() => onNavigate?.(item.id)}
              aria-current={active ? "page" : undefined}
              className={cn(
                "flex items-center gap-3 rounded-md px-3 py-2.5 text-[15px] font-semibold transition-colors outline-none focus-visible:ring-2 focus-visible:ring-accent/30",
                active
                  ? "bg-surface-sunken text-foreground"
                  : "text-muted-foreground hover:bg-surface-sunken/60 hover:text-foreground",
              )}
            >
              <span className={cn("[&_svg]:size-5", active ? "text-accent" : "text-faint-foreground")}>
                {item.icon ?? DEFAULT_ICONS[item.id] ?? <Grid2x2 className="size-5" />}
              </span>
              <span className="flex-1 text-left">{item.label}</span>
              {!!item.badge && (
                <span className="inline-flex h-5 min-w-5 items-center justify-center rounded-full bg-destructive px-1.5 text-xs font-bold text-destructive-foreground">
                  {item.badge}
                </span>
              )}
            </button>
          );
        })}

        <button
          type="button"
          onClick={onExplorer}
          className="mt-4 flex items-center gap-3 rounded-md px-3 py-2.5 text-[15px] font-semibold text-muted-foreground transition-colors hover:bg-surface-sunken/60 hover:text-foreground"
        >
          <Compass className="size-5 text-faint-foreground" />
          <span className="flex-1 text-left">{explorerLabel}</span>
        </button>
      </nav>

      <div className="border-t border-border p-3">
        <button
          type="button"
          onClick={onSettings}
          className="flex w-full items-center gap-3 rounded-md px-3 py-2.5 text-[15px] font-semibold text-muted-foreground transition-colors hover:bg-surface-sunken/60 hover:text-foreground"
        >
          <Settings className="size-5 text-faint-foreground" />
          <span className="flex-1 text-left">{settingsLabel}</span>
        </button>
        {user && (
          <div className="mt-1 flex items-center gap-3 rounded-md px-3 py-2">
            <Avatar alt={user.name} src={user.avatarSrc} size="sm" tone="primary" />
            <div className="flex min-w-0 flex-col">
              <span className="truncate text-sm font-semibold text-foreground">{user.name}</span>
              {user.subtitle && (
                <span className="truncate text-xs text-muted-foreground">{user.subtitle}</span>
              )}
            </div>
          </div>
        )}
      </div>
    </aside>
  );
}

/* --------------------------------------------------------------- AppShell */

export interface AppShellProps {
  nav: NavItem[];
  activeNav: string;
  onNavigate?: (id: string) => void;
  /** Center action — mobile FAB / desktop header button. */
  onPrimaryAction?: () => void;
  primaryActionLabel?: string;
  primaryActionIcon?: React.ReactNode;
  /** Header slots */
  user?: { name: string; initials?: string; avatarSrc?: string };
  onSearch?: () => void;
  onNotifications?: () => void;
  notificationCount?: number;
  notificationControl?: React.ReactNode;
  /** Desktop sidebar extras */
  onExplorer?: () => void;
  onSettings?: () => void;
  logo?: React.ReactNode;
  /** Page title shown in the desktop header. */
  title?: React.ReactNode;
  /** Desktop header actions (page-level buttons). */
  headerActions?: React.ReactNode;
  platform?: PlatformSetting;
  className?: string;
  children: React.ReactNode;
}

/**
 * The responsive app frame.
 * Mobile → header (avatar, search, bell) + content + bottom tab bar w/ FAB.
 * Desktop → left sidebar (approval badge, Explorer, settings footer) +
 * top header (title, search field, bell, avatar) + content.
 */
export function AppShell({
  nav,
  activeNav,
  onNavigate,
  onPrimaryAction,
  primaryActionLabel = "New",
  primaryActionIcon,
  user,
  onSearch,
  onNotifications,
  notificationCount,
  notificationControl,
  onExplorer,
  onSettings,
  logo,
  title,
  headerActions,
  platform,
  className,
  children,
}: AppShellProps) {
  const resolved = usePlatform(platform);

  if (resolved === "mobile") {
    return (
      <div className={cn("flex h-dvh flex-col bg-background", className)}>
        <header className="flex items-center gap-3 border-b border-border bg-surface px-4 pt-[max(0.75rem,env(safe-area-inset-top))] pb-3">
          <Avatar alt={user?.name ?? "Account"} src={user?.avatarSrc} initials={user?.initials} size="sm" tone="primary" />
          <button
            type="button"
            onClick={onSearch}
            className="flex h-10 flex-1 items-center gap-2.5 rounded-full border border-border bg-surface px-4 text-[15px] text-faint-foreground transition-colors hover:bg-surface-sunken/50"
          >
            <Search className="size-4" aria-hidden />
            Search
          </button>
          {notificationControl ?? (
            <div className="relative">
              <IconButton variant="outline" size="sm" onClick={onNotifications} aria-label="Notifications">
                <Bell />
              </IconButton>
              {!!notificationCount && (
                <span className="absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-bold text-destructive-foreground">
                  {notificationCount}
                </span>
              )}
            </div>
          )}
        </header>

        <main className="lx-scroll flex-1 overflow-y-auto">{children}</main>

        <BottomTabBar
          items={nav}
          activeId={activeNav}
          onNavigate={onNavigate}
          onFab={onPrimaryAction}
          fabIcon={primaryActionIcon}
        />
      </div>
    );
  }

  return (
    <div className={cn("flex h-dvh bg-background", className)}>
      <Sidebar
        items={nav}
        activeId={activeNav}
        onNavigate={onNavigate}
        logo={logo}
        onExplorer={onExplorer}
        onSettings={onSettings}
        user={
          user
            ? { name: user.name, subtitle: "LayerX account", avatarSrc: user.avatarSrc }
            : undefined
        }
      />
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="flex h-16 shrink-0 items-center gap-4 border-b border-border bg-surface px-6">
          <h1 className="text-lg font-bold text-foreground">{title}</h1>
          <div className="flex-1" />
          <button
            type="button"
            onClick={onSearch}
            className="flex h-10 w-64 items-center gap-2.5 rounded-full border border-border bg-surface px-4 text-sm text-faint-foreground transition-colors hover:bg-surface-sunken/50"
          >
            <Search className="size-4" aria-hidden />
            <span className="flex-1 text-left">Search</span>
            <kbd className="rounded border border-border bg-surface-sunken px-1.5 py-0.5 text-[10px] font-semibold text-muted-foreground">
              ⌘K
            </kbd>
          </button>
          {notificationControl ?? (
            <div className="relative">
              <IconButton variant="outline" size="sm" onClick={onNotifications} aria-label="Notifications">
                <Bell />
              </IconButton>
              {!!notificationCount && (
                <span className="absolute -top-0.5 -right-0.5 inline-flex h-4 min-w-4 items-center justify-center rounded-full bg-destructive px-1 text-[10px] font-bold text-destructive-foreground">
                  {notificationCount}
                </span>
              )}
            </div>
          )}
          {headerActions}
        </header>
        <main className="lx-scroll flex-1 overflow-y-auto">{children}</main>
      </div>
    </div>
  );
}
