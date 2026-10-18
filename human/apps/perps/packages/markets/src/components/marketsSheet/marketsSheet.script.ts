import { useLocalStorage } from "@paxeer-network/hooks";
import { SIDE_MARKETS_TAB_SORT_STORAGE_KEY } from "../../constant";
import { MarketsTabName } from "../../type";
import { useTabSort } from "../shared/hooks/useTabSort";

export type MarketsSheetScriptReturn = ReturnType<typeof useMarketsSheetScript>;

const MARKETS_SHEET_SEL_TAB_KEY = "paxeerNetwork_markets_sheet_sel_tab_key";

export function useMarketsSheetScript() {
  const { tabSort, onTabSort } = useTabSort({
    storageKey: SIDE_MARKETS_TAB_SORT_STORAGE_KEY,
  });

  const [activeTab, setActiveTab] = useLocalStorage(
    MARKETS_SHEET_SEL_TAB_KEY,
    MarketsTabName.All,
  );

  return {
    tabSort,
    onTabSort,
    activeTab,
    onTabChange: setActiveTab,
  };
}
