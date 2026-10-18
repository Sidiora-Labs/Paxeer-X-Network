import { useContext, useMemo, useState } from "react";
import { API, EMPTY_LIST } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { PaxeerNetworkContext } from "../../paxeerNetworkContext";
import { useFundingRates } from "../useFundingRates";
import { useMarketsStream } from "../useMarketsStream";
import { useRwaSymbolsInfo } from "../useRwaSymbolsInfo";
import { useSymbolsInfo } from "../useSymbolsInfo";
import { MarketsType } from "./marketTypes";

export { MarketsType } from "./marketTypes";

export interface FavoriteTab {
  name: string;
  id: number;
}

export interface Favorite {
  name: string;
  tabs: FavoriteTab[];
}

export interface Recent {
  name: string;
}

export interface TabSort {
  sortKey: string;
  sortOrder: string;
}

export type MarketStoreKey =
  | "recent"
  | "favorites"
  | "favoriteTabs"
  | "newListing"
  | "lastSelectedFavoriteTab"
  | "tabSort";

/*
{
  markets: {
      recent: [
          { "name": `${symbol.name}` },
          { "name": `${symbol.name}` },
      ],
      favorites: [
          { "name": `${symbol.name}`, "tabs": [{ "name": "Popular", "id": 1 }] },
      ],
      favoriteTabs: [
          { "name": "Popular", "id": 1 },
      ],
      "lastSelectFavoriteTab": { "name": "Popular", "id": 1 },
      "tabSort": { "all": { "sortKey": "24h_amount", "sortOrder": "desc" }}        
  }
}
*/

const DefaultTab = { name: "Popular", id: 1 };
const marketsKey = "markets";

/** @deprecated use useMarkets instead */
export const useMarket = (type: MarketsType) => {
  const { configStore } = useContext(PaxeerNetworkContext);

  const symbolsInfo = useSymbolsInfo();
  const rwaSymbolsInfo = useRwaSymbolsInfo();
  const fundingRates = useFundingRates();
  const { data: futures } = useMarketsStream();

  const updateStore = (key: MarketStoreKey, data: any) => {
    configStore.set(marketsKey, {
      ...configStore.getOr(marketsKey, {}),
      [key]: data,
    });
  };

  const getStore = <T>(key: MarketStoreKey, defaultValue: T) => {
    return (configStore.get(marketsKey)[key as any] as T) || defaultValue;
  };

  if (!configStore.get(marketsKey)) {
    const jsonStr = localStorage.getItem(marketsKey);
    if (jsonStr) {
      configStore.set(marketsKey, JSON.parse(jsonStr));
    } else {
      configStore.set(marketsKey, {
        recent: [],
        favorites: [
          { name: "PERP_ETH_USDC", tabs: [{ ...DefaultTab }] },
          { name: "PERP_BTC_USDC", tabs: [{ ...DefaultTab }] },
        ],
        favoriteTabs: [{ ...DefaultTab }],
        lastSelectedFavoriteTab: { ...DefaultTab },
      });
    }
  }

  const getFavoriteTabs = useMemo(() => {
    return getStore("favoriteTabs", [{ ...DefaultTab }]);
  }, []);

  const getFavorites = useMemo(() => {
    const curData = getStore<Favorite[]>("favorites", []);
    const tabs = getFavoriteTabs;
    const result: Favorite[] = [];
    const len = curData.length;
    for (let index = 0; index < len; index++) {
      const favData = curData[index];
      const favTabs = favData.tabs.filter(
        (tab) => tabs.findIndex((item) => tab.id === item.id) !== -1,
      );
      if (favTabs.length) {
        result.push({ ...favData, tabs: favTabs });
      }
    }
    updateStore("favorites", result);

    return result;
  }, [configStore]);

  const [favoriteTabs, setFavoriteTabs] = useState(getFavoriteTabs);
  const [favorites, setFavorites] = useState(getFavorites);

  const [recent, setRecent] = useState(
    getStore<Recent[]>("recent", []).filter((e) => e),
  );
  const [tabSort, setTabSort] = useState(
    getStore<Record<string, TabSort>>("tabSort", {}),
  );

  const updateFavoriteTabs = (
    tab: FavoriteTab | FavoriteTab[],
    operator?: {
      add?: boolean;
      update?: boolean;
      delete?: boolean;
    },
  ) => {
    const saveTabs = (tabs: FavoriteTab[]) => {
      setFavoriteTabs(tabs);
      updateStore("favoriteTabs", tabs);
    };

    if (Array.isArray(tab)) {
      saveTabs(tab);
      return;
    }

    const tabs = [...favoriteTabs];
    const index = tabs.findIndex((item) => item.id === tab.id);
    if (operator?.add) {
      tabs.push(tab);
    } else if (operator?.update) {
      if (index !== -1) {
        tabs[index] = tab;
      }
    } else if (operator?.delete) {
      if (index !== -1) {
        tabs.splice(index, 1);
      }
    }
    saveTabs(tabs);
  };

  const updateFavorites = (favorites: Favorite[]) => {
    updateStore("favorites", favorites);
    setFavorites(favorites);
  };

  const addToHistory = (symbol: API.MarketInfoExt) => {
    const curData = [...recent];
    const index = curData.findIndex((item) => item.name == symbol.symbol);
    if (index !== -1) {
      curData.splice(index, 1);
    }
    curData.unshift({ name: symbol.symbol });
    updateStore("recent", curData);
    setRecent(curData);
  };

  const updateSymbolFavoriteState = (
    symbol: API.MarketInfoExt,
    tab: FavoriteTab | FavoriteTab[],
    remove: boolean = false,
  ) => {
    const curData = [...favorites];
    const index = curData.findIndex((item) => item.name == symbol.symbol);

    if (index === -1) {
      // can not find
      if (Array.isArray(tab)) {
        if (tab.length > 0) {
          curData.unshift({ name: symbol.symbol, tabs: tab });
        }
      } else {
        if (!remove) {
          // insert
          curData.unshift({ name: symbol.symbol, tabs: [tab] });
        }
      }
    } else {
      const favorite = curData[index];
      if (Array.isArray(tab)) {
        if (tab.length === 0) {
          // remove
          curData.splice(index, 1);
        } else {
          // overrides
          curData[index] = { ...favorite, tabs: tab };
        }
      } else {
        if (remove) {
          const tabs = favorite.tabs.filter((item) => item.id != tab.id);
          if (tabs.length === 0) {
            // del favorite
            curData.splice(index, 1);
          } else {
            curData[index] = { ...favorite, tabs };
          }
        } else {
          // insert
          const tabs = favorite.tabs;
          tabs.push(tab);
          curData[index] = { ...favorite, tabs };
        }
      }
    }

    updateStore("favorites", curData);
    setFavorites(() => curData);
  };

  const marketsList = useMemo(() => {
    const list = futures?.map((item: any) => {
      const { open_interest = 0, index_price = 0 } = item;

      const info = symbolsInfo[item.symbol];
      const rwaInfo = !rwaSymbolsInfo.isNil
        ? rwaSymbolsInfo[item.symbol]()
        : null;
      const rate = fundingRates[item.symbol];
      const est_funding_rate = rate("est_funding_rate");
      const funding_period = info("funding_period");
      const change =
        item.change === undefined
          ? get24hChange(item["24h_close"], item["24h_open"])
          : item.change;

      return {
        ...item,
        change,
        "8h_funding": get8hFunding(est_funding_rate, funding_period),
        quote_dp: info("quote_dp"),
        created_time: info("created_time"),
        isRwa: !!rwaInfo,
        isPreTge: Boolean(item.is_pretge ?? info("is_pretge")),
        openInterest: new Decimal(open_interest || 0)
          .mul(index_price || 0)
          .toNumber(),
      };
    });
    return list || [];
  }, [symbolsInfo, futures, fundingRates, rwaSymbolsInfo]);

  const getData = (type: MarketsType) => {
    // get data
    const localData =
      type === MarketsType.FAVORITES ? [...favorites] : [...recent];
    // filter
    const keys = localData.map((item) => item.name);
    let filter = marketsList;
    if (type == MarketsType.CRYPTO) {
      filter = marketsList?.filter((item) => !item.isRwa);
    } else if (type == MarketsType.RWA) {
      filter = marketsList?.filter((item) => item.isRwa);
    } else if (type == MarketsType.PRE_TGE) {
      filter = marketsList?.filter((item) => item.isPreTge);
    } else if (type != MarketsType.ALL && type != MarketsType.COMMUNITY) {
      filter = marketsList?.filter((item) => keys.includes(item.symbol));
    }

    const favoritesData = [...favorites];
    const favoriteKeys = favoritesData.map((item) => item.name);
    if (filter) {
      for (let index = 0; index < filter.length; index++) {
        const element = filter[index];
        const isFavorite =
          type == MarketsType.FAVORITES
            ? true
            : favoriteKeys.includes(element.symbol);

        const fIndex = favoritesData.findIndex(
          (item) => item.name === element.symbol,
        );
        const tabs = fIndex === -1 ? [] : favoritesData[fIndex].tabs;

        let imr = undefined;
        if (symbolsInfo) {
          imr = symbolsInfo?.[element.symbol]("base_imr");
        }

        filter[index] = {
          ...filter[index],
          isFavorite,
          tabs,
          leverage: imr ? 1 / imr : undefined,
        };
      }
    }

    return filter;
  };

  const pinToTop = (symbol: API.MarketInfoExt) => {
    const index = favorites.findIndex((item) => item.name === symbol.symbol);
    if (index !== -1) {
      const element = favorites[index];
      const list = [...favorites];
      list.splice(index, 1);
      list.unshift(element);
      updateStore("favorites", list);
      setFavorites(list);
    }
  };

  const getLastSelFavTab = () => {
    return getStore<FavoriteTab>("lastSelectedFavoriteTab", { ...DefaultTab });
  };

  const updateSelectedFavoriteTab = (tab: FavoriteTab) => {
    updateStore("lastSelectedFavoriteTab", tab);
  };

  const updateTabsSortState = (
    tabId: string,
    sortKey: string,
    sortOrder: "desc" | "asc",
  ) => {
    const map = getStore<Record<string, TabSort>>("tabSort", {});

    map[tabId] = {
      sortKey,
      sortOrder,
    };

    updateStore("tabSort", map);
    setTabSort(map);
  };

  const markets = getData(type);

  return [
    markets || EMPTY_LIST,
    {
      favoriteTabs,
      favorites,
      recent,
      tabSort,
      addToHistory,
      updateFavorites,
      updateFavoriteTabs,
      updateSymbolFavoriteState,
      pinToTop,
      getLastSelFavTab,
      updateSelectedFavoriteTab,
      updateTabsSortState,
    },
  ] as const;
};

function get8hFunding(est_funding_rate: number, funding_period: number) {
  let funding8h = 0;

  if (est_funding_rate === undefined || est_funding_rate === null) {
    return null;
  }

  if (funding_period) {
    funding8h = new Decimal(est_funding_rate || 0)
      .mul(funding_period)
      .div(8)
      .toNumber();
  }

  return funding8h;
}

function get24hChange(close: number, open: number) {
  if (close !== undefined && open !== undefined) {
    if (open === 0) {
      return 0;
    }
    return new Decimal(close).minus(open).div(open).toNumber();
  }
}
