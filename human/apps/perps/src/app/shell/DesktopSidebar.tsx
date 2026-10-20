import Image from "next/image";
import Link from "next/link";
import { useRouter } from "next/router";
import {
  BarChart3,
  Briefcase,
  ChevronLeft,
  ChevronRight,
  Coins,
  Compass,
  Gift,
  LayoutGrid,
  LineChart,
  MessageSquare,
  Plus,
  Radio,
  Receipt,
  Rocket,
  ArrowLeftRight,
  TrendingUp,
  Trophy,
  Users,
  type LucideIcon,
} from "lucide-react";
import { cn } from "@/ui/utils/cn";
import {
  NAV_ITEMS,
  NAV_GROUP_LABELS,
  NAV_GROUP_ORDER,
  type NavGroup,
  type NavItem,
} from "@/core/nav";
import { useSidebar } from "@/hooks/ui/useSidebar";

/**
 * Per-route icon registry. Kept here (not in nav.ts) so the data layer
 * stays presentation-free. All icons are from `lucide-react` (already a
 * first-class dep across the app).
 */
const NAV_ICONS: Record<string, LucideIcon> = {
  "/": LineChart,
  "/perps": TrendingUp,
  "/spot": Coins,
  "/markets": LayoutGrid,
  "/portfolio": Briefcase,
  "/analytics": BarChart3,
  "/leaderboard": Trophy,
  "/launch": Rocket,
  "/create": Plus,
  "/swap": ArrowLeftRight,
  "/rewards": Gift,
  "/pnl": Receipt,
  "/live": Radio,
  "/chat": MessageSquare,
  "/referrals": Users,
  "/explorer": Compass,
};

interface SidebarRowProps {
  item: NavItem;
  active: boolean;
  collapsed: boolean;
}

function SidebarRow({ item, active, collapsed }: SidebarRowProps) {
  const Icon = NAV_ICONS[item.href] ?? LayoutGrid;

  return (
    <Link
      href={item.href}
      title={collapsed ? item.label : undefined}
      className={cn(
        "group flex items-center gap-3 rounded-md text-sm font-medium transition-colors",
        collapsed ? "justify-center h-9 w-9 mx-auto" : "h-9 px-3",
        active
          ? "bg-primary/10 text-primary"
          : "text-muted-foreground hover:bg-muted/30 hover:text-foreground"
      )}
    >
      <Icon className="h-4 w-4 shrink-0" />
      {!collapsed && <span className="truncate">{item.label}</span>}
    </Link>
  );
}

/**
 * Desktop left sidebar — grouped navigation with collapsible icon-rail mode.
 *
 * Modes:
 * - Expanded (220px): group headers visible, full labels alongside icons
 * - Collapsed (56px): icons only, labels surface via native `title` tooltip
 *
 * Filtering:
 * - Items must have a `group` assigned in `nav.ts` (ungrouped items never
 *   render here — by design, so e.g. mobile-only items stay mobile-only)
 * - Items must include `desktopPro` in `surfaces`
 *
 * Not rendered on mobile — `AppShell` gates this behind `!isMobile`.
 */
export function DesktopSidebar() {
  const router = useRouter();
  const { collapsed, toggle } = useSidebar();

  const grouped = new Map<NavGroup, NavItem[]>();
  for (const item of NAV_ITEMS) {
    if (!item.group) continue;
    if (!item.surfaces.includes("desktopPro")) continue;
    const bucket = grouped.get(item.group) ?? [];
    bucket.push(item);
    grouped.set(item.group, bucket);
  }

  return (
    <aside
      aria-label="Primary navigation"
      className={cn(
        "hidden md:flex shrink-0 flex-col border-r border-border/40 bg-surface-0 transition-[width] duration-200",
        collapsed ? "w-14" : "w-[220px]"
      )}
    >
      <nav className="flex-1 overflow-y-auto py-3 space-y-4">
        {NAV_GROUP_ORDER.map((group) => {
          const items = grouped.get(group) ?? [];
          if (items.length === 0) return null;

          return (
            <div key={group} className={cn(collapsed ? "px-1" : "px-3")}>
              {!collapsed && (
                <div className="px-1 mb-1 text-[10px] uppercase tracking-wider text-muted-foreground/70 font-semibold">
                  {NAV_GROUP_LABELS[group]}
                </div>
              )}
              <div className={cn("space-y-0.5", collapsed && "space-y-1")}>
                {items.map((item) => (
                  <SidebarRow
                    key={item.href}
                    item={item}
                    active={router.pathname === item.href}
                    collapsed={collapsed}
                  />
                ))}
              </div>
            </div>
          );
        })}
      </nav>

      {/* Brand banner — only visible in expanded mode */}
      {!collapsed && (
        <div className="px-3 pb-3 shrink-0">
          <a
            href="https://hyperpaxeer.com"
            target="blank"
            rel="noopener noreferrer"
            className="block rounded-lg overflow-hidden opacity-80 hover:opacity-100 transition-opacity"
          >
            <Image
              src="/image.png"
              alt="HyperPax — From Fast to Instant"
              width={194}
              height={97}
              className="w-full h-auto object-cover rounded-lg"
              priority={false}
            />
          </a>
        </div>
      )}

      <button
        type="button"
        onClick={toggle}
        aria-label={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        title={collapsed ? "Expand sidebar" : "Collapse sidebar"}
        className={cn(
          "shrink-0 flex items-center gap-2 border-t border-border/40 px-3 h-9 text-xs text-muted-foreground hover:bg-muted/30 hover:text-foreground transition-colors",
          collapsed && "justify-center px-0"
        )}
      >
        {collapsed ? (
          <ChevronRight className="h-4 w-4" />
        ) : (
          <>
            <ChevronLeft className="h-4 w-4" />
            <span>Collapse</span>
          </>
        )}
      </button>
    </aside>
  );
}
