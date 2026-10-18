import { useMarkets } from "@paxeer-network/hooks";
import { SortOrder, Column } from "@paxeer-network/ui";

// Re-export config types from hooks for convenience
export type {
  BuiltInMarketTab,
  CustomMarketTab,
  MarketBuiltInTabType,
  MarketTabConfig,
  MarketCategoryContext,
  MarketCategoryConfig,
  MarketCategoryComponentKey,
} from "@paxeer-network/hooks";

export type FavoriteInstance = ReturnType<typeof useMarkets>[1];

export type SortType = {
  sortKey: string;
  sortOrder: SortOrder;
};

export type GetColumns = (
  favorite: FavoriteInstance,
  isFavoriteList: boolean,
) => Column[];

export enum MarketsPageTab {
  Markets = "markets",
  Funding = "funding",
}

export enum MarketsTabName {
  Favorites = "favorites",
  Recent = "recent",
  All = "all",
  Crypto = "crypto",
  Rwa = "rwa",
  PreTge = "preTge",
  NewListing = "newListing",
  Community = "community",
}

export enum CommunitySubTabName {
  All = "all",
  MyListings = "myListings",
  OtherListings = "otherListings",
}

export enum FundingTabName {
  Overview = "overview",
  Comparison = "comparison",
}
