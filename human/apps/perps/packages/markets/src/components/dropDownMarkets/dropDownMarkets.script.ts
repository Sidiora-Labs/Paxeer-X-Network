import { useEffect, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { MarketsTabName } from "../../type";
import { useMarketsContext } from "../marketsProvider";
import { useTabSort } from "../shared/hooks/useTabSort";

export type DropDownMarketsScriptReturn = ReturnType<
  typeof useDropDownMarketsScript
>;

const DROPDOWN_MARKETS_SEL_TAB_KEY = "paxeerNetwork_dropdown_markets_sel_tab_key";

const DROPDOWN_MARKETS_TAB_SORT_STORAGE_KEY =
  "paxeerNetwork_dropdown_markets_tab_sort";

export function useDropDownMarketsScript() {
  const [open, setOpen] = useState(false);

  const [activeTab, setActiveTab] = useLocalStorage(
    DROPDOWN_MARKETS_SEL_TAB_KEY,
    MarketsTabName.All,
  );

  const { tabSort, onTabSort } = useTabSort({
    storageKey: DROPDOWN_MARKETS_TAB_SORT_STORAGE_KEY,
  });

  const { clearSearchValue } = useMarketsContext();

  const hide = () => {
    setOpen(false);
  };

  useEffect(() => {
    clearSearchValue?.();
  }, [activeTab]);

  return {
    activeTab: activeTab as MarketsTabName,
    onTabChange: (value: string) => setActiveTab(value as MarketsTabName),
    open,
    onOpenChange: setOpen,
    hide,
    tabSort,
    onTabSort,
  };
}
