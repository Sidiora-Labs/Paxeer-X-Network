import type {
  BuiltInMarketTab,
  MarketBuiltInTabType,
  MarketCategoryComponentKey,
  MarketTabConfig,
} from "../type";

/**
 * Pre-built builtIn constants for config function's context.builtIn.
 * Each entry is a built-in tab reference.
 */
export const builtInTabs: Record<MarketBuiltInTabType, BuiltInMarketTab> = {
  favorites: { type: "favorites" },
  community: {
    type: "community",
    isVisible: (symbolList) => {
      return symbolList.some((m) => Boolean(m?.broker_id));
    },
  },
  all: { type: "all" },
  crypto: { type: "crypto" },
  rwa: { type: "rwa" },
  preTge: {
    type: "preTge",
    isVisible: (symbolList, { symbolsInfo }) => {
      return symbolList.some((m) =>
        Boolean(m?.is_pretge ?? symbolsInfo?.[m.symbol]?.("is_pretge")),
      );
    },
  },
  newListing: { type: "newListing" },
  recent: { type: "recent" },
};

/**
 * Default tab sequences for each component (as MarketTabConfig[]).
 */
export const componentDefaultTabs: Record<
  MarketCategoryComponentKey,
  MarketTabConfig[]
> = {
  marketsSheet: [
    { type: "favorites" },
    { type: "all" },
    { ...builtInTabs.crypto },
    { type: "rwa" },
    { ...builtInTabs.community },
    { ...builtInTabs.preTge },
  ],
  expandMarkets: [
    { type: "favorites" },
    { type: "all" },
    { ...builtInTabs.crypto },
    { type: "rwa" },
    { ...builtInTabs.community },
    { type: "newListing" },
    { type: "recent" },
    { ...builtInTabs.preTge },
  ],
  dropDownMarkets: [
    { type: "favorites" },
    { type: "all" },
    { ...builtInTabs.crypto },
    { type: "rwa" },
    { ...builtInTabs.community },
    { type: "newListing" },
    { type: "recent" },
    { ...builtInTabs.preTge },
  ],
  subMenuMarkets: [
    { type: "favorites" },
    { type: "all" },
    { ...builtInTabs.crypto },
    { type: "rwa" },
    { ...builtInTabs.community },
    { type: "newListing" },
    { type: "recent" },
    { ...builtInTabs.preTge },
  ],
  marketsDataList: [
    { type: "favorites" },
    { type: "all" },
    { ...builtInTabs.crypto },
    { type: "rwa" },
    { ...builtInTabs.community },
    { type: "newListing" },
    { ...builtInTabs.preTge },
  ],
  horizontalMarkets: [
    { type: "all" },
    { type: "newListing" },
    { type: "recent" },
    { type: "favorites" },
  ],
};
