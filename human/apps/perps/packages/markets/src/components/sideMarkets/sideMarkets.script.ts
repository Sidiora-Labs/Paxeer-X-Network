import { useCallback, useEffect, useMemo, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { SIDE_MARKETS_TAB_SORT_STORAGE_KEY } from "../../constant";
import { MarketsTabName } from "../../type";
import { useTabSort } from "../shared/hooks/useTabSort";

export type SideMarketsScriptOptions = {
  panelSize?: "small" | "middle" | "large";
  collapsed?: boolean;
  onPanelSizeChange?: React.Dispatch<
    React.SetStateAction<"small" | "middle" | "large">
  >;
};

export type SideMarketsScriptReturn = ReturnType<typeof useSideMarketsScript>;

const SIDE_MARKETS_SEL_TAB_KEY = "paxeerNetwork_side_markets_sel_tab_key";

export const useSideMarketsScript = (options?: SideMarketsScriptOptions) => {
  const [activeTab, setActiveTab] = useLocalStorage(
    SIDE_MARKETS_SEL_TAB_KEY,
    MarketsTabName.All,
  );

  const { tabSort } = useTabSort({
    storageKey: SIDE_MARKETS_TAB_SORT_STORAGE_KEY,
  });

  const panelSize = useMemo(() => {
    if (options?.collapsed) {
      return "small";
    }
    return options?.panelSize || "large";
  }, [options?.collapsed, options?.panelSize]);

  const onPanelSizeChange = useCallback(
    (size: "small" | "middle" | "large") => {
      if (typeof options?.onPanelSizeChange === "function") {
        options.onPanelSizeChange(size);
      }
    },
    [options?.onPanelSizeChange],
  );

  return {
    activeTab: activeTab as MarketsTabName,
    onTabChange: setActiveTab,
    tabSort: tabSort,
    panelSize,
  } as const;
};
