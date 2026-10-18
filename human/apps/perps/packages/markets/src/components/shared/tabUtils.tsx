import { createElement, isValidElement, useMemo } from "react";
import type {
  BuiltInMarketTab,
  CustomMarketTab,
  MarketTabConfig,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { GradientText } from "@paxeer-network/ui";
import { FavoritesIcon } from "../../icons";

export function isBuiltInMarketTab(
  tab: MarketTabConfig,
): tab is BuiltInMarketTab {
  return "type" in tab;
}

export function isCustomMarketTab(
  tab: MarketTabConfig,
): tab is CustomMarketTab {
  return "id" in tab;
}

/** Stable key for a tab — uses type or custom id */
export function tabKey(tab: MarketTabConfig, index: number): string {
  return isBuiltInMarketTab(tab) ? tab.type : tab.id || `category_${index}`;
}

/** Convert string URL to <img>, pass ReactNode through */
export function resolveIcon(
  icon: string | React.ReactNode | undefined,
): React.ReactNode | undefined {
  if (typeof icon === "string") {
    return createElement("img", {
      src: icon,
      alt: "",
      style: { width: 16, height: 16 },
    });
  }
  return icon as React.ReactNode | undefined;
}

export function resolveSuffix(
  suffix: string | React.ReactNode | undefined,
): React.ReactNode | undefined {
  if (typeof suffix === "undefined" || suffix === null) {
    return undefined;
  }

  if (typeof suffix === "string") {
    return (
      <span className="oui-ms-1 oui-inline-flex oui-items-center oui-rounded oui-bg-gradient-to-r oui-from-[rgb(var(--oui-gradient-brand-start)_/_0.12)] oui-to-[rgb(var(--oui-gradient-brand-end)_/_0.12)] oui-px-2">
        <GradientText color="brand">{suffix}</GradientText>
      </span>
    );
  }

  return <span className="oui-ms-1">{suffix}</span>;
}

export function composeTabTitle(
  label: React.ReactNode,
  options?: {
    icon?: React.ReactNode;
    suffix?: string | React.ReactNode;
  },
): React.ReactNode {
  const icon = options?.icon;
  const suffix = resolveSuffix(options?.suffix);
  const hasLabel =
    typeof label !== "undefined" && label !== null && label !== "";

  if (!hasLabel) {
    if (!icon) {
      return suffix ?? null;
    }

    return suffix ? (
      <span className="oui-inline-flex oui-items-center oui-gap-x-1">
        {icon}
        {suffix}
      </span>
    ) : (
      icon
    );
  }

  if (icon) {
    return (
      <span className="oui-inline-flex oui-items-center oui-gap-x-1">
        {icon}
        {label}
        {suffix}
      </span>
    );
  }

  return (
    <>
      {label}
      {suffix}
    </>
  );
}

export function resolveTabTriggerIcon(
  tab: MarketTabConfig,
  fallbackIcon?: React.ReactElement,
): React.ReactElement | undefined {
  const resolvedIcon =
    typeof tab.icon !== "undefined" ? resolveIcon(tab.icon) : fallbackIcon;

  return isValidElement(resolvedIcon) ? resolvedIcon : undefined;
}

export const BUILT_IN_TITLE_MAP: Record<string, string> = {
  community: "markets.community",
  all: "common.all",
  crypto: "markets.crypto",
  preTge: "markets.preTge",
  newListing: "markets.newListings",
  recent: "markets.recent",
};

/**
 * Resolve tab title for TabPanel.
 * @param titleOverrides - pre-resolved i18n strings keyed by built-in type (e.g. { all: "All", newListing: "New Listings" })
 * @param rwaTitle - component-specific RWA title (RwaTab or RwaIconTab)
 */
export function resolveTabTitle(
  tab: MarketTabConfig,
  titleOverrides: Record<string, string>,
  rwaTitle: React.ReactNode,
): React.ReactNode {
  if (isBuiltInMarketTab(tab) && tab.type === "favorites") {
    return <FavoritesIcon />;
  }

  if (isBuiltInMarketTab(tab) && tab.type === "rwa") {
    return rwaTitle;
  }

  if (isBuiltInMarketTab(tab) && titleOverrides[tab.type]) {
    return tab.name ?? titleOverrides[tab.type];
  }

  return composeTabTitle(tab.name, {
    icon: resolveIcon(tab.icon),
    suffix: tab.suffix,
  });
}

/** Pre-resolve built-in title i18n strings */
export function useBuiltInTitles(): Record<string, string> {
  const { t } = useTranslation();
  return useMemo(
    () =>
      Object.fromEntries(
        Object.entries(BUILT_IN_TITLE_MAP).map(([key, i18nKey]) => [
          key,
          t(i18nKey as any) as string,
        ]),
      ),
    [t],
  );
}

export type DataFilter = (data: any[]) => any[];

export function getCustomTabDataFilter(
  tab: CustomMarketTab | undefined,
): DataFilter | undefined {
  if (!tab) return undefined;
  return (data) => data.filter((item) => tab.match(item));
}

/** Build per-tab dataFilter from `match` config */
export function useCustomTabDataFilters(
  tabs: MarketTabConfig[] | undefined,
): Record<string, DataFilter> {
  return useMemo(() => {
    if (!tabs) return {};
    const result: Record<string, DataFilter> = {};
    tabs.forEach((tab, i) => {
      if (isCustomMarketTab(tab)) {
        result[tabKey(tab, i)] = (data) =>
          data.filter((item) => tab.match(item));
      }
    });
    return result;
  }, [tabs]);
}
