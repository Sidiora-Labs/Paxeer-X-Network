import { MarketsType } from "./marketTypes";
import type { Favorite, MarketsItem, NewListing, Recent } from "./useMarkets";

export const filterMarkets = (params: {
  markets: MarketsItem[];
  favorites: Favorite[];
  recent: Recent[];
  newListing: NewListing[];
  type: MarketsType;
}) => {
  const { markets, favorites, recent, newListing, type } = params;
  let curData: MarketsItem[] = [];

  if (type === MarketsType.ALL || type === MarketsType.COMMUNITY) {
    curData = markets;
  } else if (type === MarketsType.CRYPTO) {
    curData = markets.filter((item) => !item.isRwa);
  } else if (type === MarketsType.RWA) {
    curData = markets.filter((item) => item.isRwa);
  } else if (type === MarketsType.PRE_TGE) {
    curData = markets.filter((item) => item.isPreTge);
  } else if (type === MarketsType.NEW_LISTING) {
    curData = markets
      .filter((item) => isNewListing(item.created_time))
      .sort((a, b) => b.created_time - a.created_time);
  } else {
    const storageData =
      type === MarketsType.FAVORITES
        ? favorites
        : type === MarketsType.RECENT
          ? recent
          : newListing;

    const keys = storageData.map((item) => item.name);
    curData = markets?.filter((item) => keys.includes(item.symbol));
  }

  const favoriteKeys = favorites.map((item) => item.name);

  return curData?.map((item) => ({
    ...item,
    isFavorite:
      type === MarketsType.FAVORITES
        ? true
        : favoriteKeys.includes(item.symbol),
  }));
};

const isNewListing = (createdTime: number): boolean => {
  const thirtyDaysInMs = 30 * 24 * 60 * 60 * 1000;
  const now = Date.now();
  return now - createdTime < thirtyDaysInMs;
};
