export type NavSurface = "desktopPro" | "mobileDrawer";
export type NavGroup = "trade" | "account";

export interface NavItem {
  href: string;
  label: string;
  surfaces: NavSurface[];
  /**
   * Sidebar grouping. Only affects DesktopSidebar rendering — the mobile
   * drawer keys off `surfaces` alone. Items without a `group` are still
   * renderable on mobile surfaces but will not appear in the desktop sidebar.
   */
  group?: NavGroup;
}

export function isSpotOpen(
  value: string | undefined = process.env.NEXT_PUBLIC_RELEASE_SPOT_OPEN,
): boolean {
  return value === "1";
}

const SPOT_NAV_ITEM: NavItem = {
  href: "/spot",
  label: "Spot",
  surfaces: ["desktopPro", "mobileDrawer"],
  group: "trade",
};

export function navItems(spotOpen: boolean = isSpotOpen()): NavItem[] {
  return [
    // ── Trade group ─────────────────────────────────────────────────────
    {
      href: "/",
      label: "Trade",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },
    {
      href: "/perps",
      label: "Perps",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },
    ...(spotOpen ? [SPOT_NAV_ITEM] : []),
    {
      href: "/markets",
      label: "Markets",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },
    {
      href: "/portfolio",
      label: "Portfolio",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },
    {
      href: "/analytics",
      label: "Analytics",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },
    {
      href: "/leaderboard",
      label: "Leaderboard",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "trade",
    },


    // ── Account group ───────────────────────────────────────────────────
    {
      href: "/referrals",
      label: "Referrals",
      surfaces: ["desktopPro"],
      group: "account",
    },
    {
      href: "/explorer",
      label: "Explorer",
      surfaces: ["desktopPro", "mobileDrawer"],
      group: "account",
    },
  ];
}

export const NAV_ITEMS: NavItem[] = navItems();

/**
 * Human-readable headers for each sidebar group. Centralised so
 * `DesktopSidebar` and any future grouped nav surface stay in sync.
 */
export const NAV_GROUP_LABELS: Record<NavGroup, string> = {
  trade: "Trade",
  account: "Account",
};

/**
 * Canonical render order for sidebar groups.
 */
export const NAV_GROUP_ORDER: NavGroup[] = ["trade", "account"];
