import { UTCDateMini } from "@date-fns/utc";
import { format } from "date-fns";
import { AnnouncementType, API } from "@paxeer-network/types";

export const getTimeString = (timestamp: number) => {
  const date = format(new UTCDateMini(timestamp), "MMM dd");
  const time = format(new UTCDateMini(timestamp), "h:mm aa");
  return `${time} (UTC) on ${date}`;
};

export const sortDataByUpdatedTime = (list: API.AnnouncementRow[]) => {
  return list.sort((a, b) => {
    if (a.updated_time && b.updated_time) {
      return b.updated_time - a.updated_time;
    }
    return 0;
  });
};

export const filterDuplicateArrayById = (list: API.AnnouncementRow[]) => {
  const seenIds = new Set<string | number>();
  const newList: API.AnnouncementRow[] = [];

  list.forEach((item) => {
    if (!seenIds.has(item.announcement_id)) {
      // If the item's ID hasn't been seen before, add it and mark as seen
      seenIds.add(item.announcement_id);
      newList.push(item);
    }
  });

  return newList;
};

/** Extract symbols from message (e.g. $GOOGL & $TSLA -> ["GOOGL", "TSLA"]) */
export const extractSymbolsFromMessage = (message: string): string[] => {
  if (!message) return [];
  const matches = message.matchAll(/\$([A-Z0-9]+)/gi);
  const tickers = [...matches].map((m) => m[1].toUpperCase());
  return [...new Set(tickers)];
};

/** Check if ticker exists in market list (PERP_<TICKER>_* match) */
export const isSymbolInMarketList = (
  symbol: string,
  marketList: API.MarketInfoExt[],
): boolean => {
  const prefix = `PERP_${symbol}_`;
  return marketList.some((m) => m.symbol?.startsWith(prefix));
};

/** Only show Listing/Delisting when all mentioned tickers are in market list */
export const shouldShowListingDelistingAnnouncement = (
  item: API.AnnouncementRow,
  marketList: API.MarketInfoExt[],
): boolean => {
  if (
    item.type !== AnnouncementType.Listing &&
    item.type !== AnnouncementType.Delisting
  ) {
    return true;
  }
  const symbols = extractSymbolsFromMessage(item.message ?? "");
  if (symbols.length === 0) {
    return true;
  }

  return symbols.every((s) => isSymbolInMarketList(s, marketList));
};
