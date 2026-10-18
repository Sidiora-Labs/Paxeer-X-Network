import { useCallback, useEffect, useState } from "react";
import { SIDE_MARKETS_TAB_SORT_STORAGE_KEY } from "../../constant";
import { MarketsTabName } from "../../type";
import { useMarketsContext } from "../marketsProvider";
import { useTabSort } from "../shared/hooks/useTabSort";

export type SubMenuMarketsScriptOptions = {
  activeTab?: MarketsTabName;
  onTabChange?: (tab: MarketsTabName) => void;
};

export type SubMenuMarketsScriptReturn = ReturnType<
  typeof useSubMenuMarketsScript
>;

export function useSubMenuMarketsScript(options?: SubMenuMarketsScriptOptions) {
  const [activeTab, setActiveTab] = useState<MarketsTabName>(
    (options?.activeTab ?? MarketsTabName.All) as MarketsTabName,
  );

  const { clearSearchValue } = useMarketsContext();

  const { tabSort, onTabSort } = useTabSort({
    storageKey: SIDE_MARKETS_TAB_SORT_STORAGE_KEY,
  });

  const onTabChange = useCallback(
    (value: string) => {
      if (typeof options?.onTabChange === "function") {
        options.onTabChange(value as MarketsTabName);
      } else {
        setActiveTab(value as MarketsTabName);
      }
    },
    [options?.onTabChange],
  );

  useEffect(() => {
    if (options?.activeTab !== undefined) {
      setActiveTab(options.activeTab as MarketsTabName);
    }
  }, [options?.activeTab]);

  useEffect(() => {
    clearSearchValue?.();
  }, [activeTab]);

  return {
    activeTab,
    onTabChange,
    tabSort,
    onTabSort,
  } as const;
}
