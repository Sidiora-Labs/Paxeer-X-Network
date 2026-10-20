import React from "react";
import {
  LayoutDashboard,
  Briefcase,
  ClipboardList,
  Wallet,
  Percent,
  KeyRound,
  Settings as SettingsIcon,
  Receipt,
  ChevronsLeft,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/ui/utils/cn";

export type PortfolioNavKey =
  | "overview"
  | "positions"
  | "orders"
  | "settlements"
  | "assets"
  | "feeTier"
  | "apiKeys"
  | "settings";

interface NavItem {
  key: PortfolioNavKey;
  label: string;
  Icon: LucideIcon;
  group: "workspace" | "account";
}

const NAV_ITEMS: NavItem[] = [
  { key: "overview", label: "Overview", Icon: LayoutDashboard, group: "workspace" },
  { key: "positions", label: "Positions", Icon: Briefcase, group: "workspace" },
  { key: "orders", label: "Orders", Icon: ClipboardList, group: "workspace" },
  { key: "settlements", label: "Settlements", Icon: Receipt, group: "workspace" },
  { key: "assets", label: "Assets", Icon: Wallet, group: "workspace" },
  { key: "feeTier", label: "Fee tier", Icon: Percent, group: "account" },
  { key: "apiKeys", label: "API Keys", Icon: KeyRound, group: "account" },
  { key: "settings", label: "Settings", Icon: SettingsIcon, group: "account" },
];

interface PortfolioSideNavProps {
  active: PortfolioNavKey;
  onChange: (key: PortfolioNavKey) => void;
  collapsed?: boolean;
  onToggleCollapsed?: () => void;
}

/**
 * Portfolio-internal side navigation. Matches the pro-grade trading
 * dashboard reference — grouped items, icon + label, active row gets a
 * brand-blue tint. Desktop only; mobile handles nav through a pill bar.
 */
export function PortfolioSideNav({
  active,
  onChange,
  collapsed,
  onToggleCollapsed,
}: PortfolioSideNavProps) {
  const workspace = NAV_ITEMS.filter((i) => i.group === "workspace");
  const account = NAV_ITEMS.filter((i) => i.group === "account");

  return (
    <aside
      aria-label="Portfolio navigation"
      className={cn(
        "hidden md:flex shrink-0 flex-col bg-surface-1/40 border-r border-white/[0.04] transition-[width] duration-200",
        collapsed ? "w-14" : "w-[220px]",
      )}
    >
      <div
        className={cn(
          "flex items-center gap-2 px-4 py-4 border-b border-white/[0.04]",
          collapsed && "justify-center px-2",
        )}
      >
        {!collapsed ? (
          <>
            <span className="text-sm font-semibold tracking-tight text-foreground">Portfolio</span>
            <button
              type="button"
              onClick={onToggleCollapsed}
              aria-label="Collapse sidebar"
              className="ml-auto p-1 text-muted-foreground hover:text-foreground rounded-md hover:bg-white/[0.04] transition-colors"
            >
              <ChevronsLeft className="h-3.5 w-3.5" />
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={onToggleCollapsed}
            aria-label="Expand sidebar"
            className="p-1 text-muted-foreground hover:text-foreground rounded-md hover:bg-white/[0.04] transition-colors"
          >
            <ChevronsLeft className="h-3.5 w-3.5 rotate-180" />
          </button>
        )}
      </div>

      <nav className="flex-1 overflow-y-auto py-3 space-y-4">
        <NavGroup
          items={workspace}
          active={active}
          onChange={onChange}
          collapsed={Boolean(collapsed)}
          label="Workspace"
        />
        <NavGroup
          items={account}
          active={active}
          onChange={onChange}
          collapsed={Boolean(collapsed)}
          label="Account"
        />
      </nav>
    </aside>
  );
}

function NavGroup({
  items,
  active,
  onChange,
  collapsed,
  label,
}: {
  items: NavItem[];
  active: PortfolioNavKey;
  onChange: (k: PortfolioNavKey) => void;
  collapsed: boolean;
  label: string;
}) {
  return (
    <div className={cn(collapsed ? "px-1" : "px-2")}>
      {!collapsed && (
        <div className="px-2 mb-1 text-[10px] uppercase tracking-[0.14em] text-muted-foreground/60 font-semibold">
          {label}
        </div>
      )}
      <div className={cn("flex flex-col", collapsed ? "gap-1" : "gap-0.5")}>
        {items.map((item) => (
          <NavRow
            key={item.key}
            item={item}
            active={active === item.key}
            onClick={() => onChange(item.key)}
            collapsed={collapsed}
          />
        ))}
      </div>
    </div>
  );
}

function NavRow({
  item,
  active,
  onClick,
  collapsed,
}: {
  item: NavItem;
  active: boolean;
  onClick: () => void;
  collapsed: boolean;
}) {
  const { Icon } = item;
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      title={collapsed ? item.label : undefined}
      className={cn(
        "group flex items-center gap-3 rounded-[var(--radius-md)] transition-colors",
        collapsed ? "justify-center h-9 w-9 mx-auto" : "h-9 px-3",
        "text-sm font-medium",
        "focus:outline-none focus-visible:ring-2 focus-visible:ring-ring/30",
        active
          ? "bg-[var(--brand-blue)]/12 text-[var(--brand-blue)]"
          : "text-muted-foreground hover:bg-white/[0.04] hover:text-foreground",
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {!collapsed && <span className="truncate">{item.label}</span>}
      {!collapsed && active ? (
        <span
          aria-hidden
          className="ml-auto h-1.5 w-1.5 rounded-full bg-[var(--brand-blue)]"
        />
      ) : null}
    </button>
  );
}

export { NAV_ITEMS as PORTFOLIO_NAV_ITEMS };
