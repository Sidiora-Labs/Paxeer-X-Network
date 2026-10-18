import { useEffect, useState } from "react";
import { useMarketsContext } from "../../../components/marketsProvider";
import { useTabSort } from "../../../components/shared/hooks/useTabSort";
import { MarketsTabName } from "../../../type";

export type UseMarketsDataListScript = ReturnType<
  typeof useMarketsDataListScript
>;

const MOBILE_MARKETS_TAB_SORT_STORAGE_KEY = "paxeerNetwork_mobile_markets_tab_sort";

export function useMarketsDataListScript() {
  const [activeTab, setActiveTab] = useState<MarketsTabName>(
    MarketsTabName.All,
  );
  const { clearSearchValue, searchValue } = useMarketsContext();

  const { tabSort, onTabSort } = useTabSort({
    storageKey: MOBILE_MARKETS_TAB_SORT_STORAGE_KEY,
  });

  useEffect(() => {
    clearSearchValue?.();
  }, [activeTab]);

  return {
    activeTab,
    onTabChange: (value: string) => setActiveTab(value as MarketsTabName),
    tabSort,
    onTabSort,
    searchValue,
  };
}
