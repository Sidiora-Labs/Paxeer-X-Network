import React, { useMemo } from "react";
import {
  useLocalStorage,
  useRwaSymbolsInfoStore,
} from "@paxeer-network/hooks";
import { Box, cn } from "@paxeer-network/ui";
import { createCommunityBrokerFilter } from "../../hooks/useCommunityTabs";
import { MarketsTabName } from "../../type";
import { ExpandMarketsWidget } from "../expandMarkets";
import { MarketsListWidget } from "../marketsList";
import { useMarketsContext } from "../marketsProvider";
import {
  createRwaSubcategoryFilter,
  type RwaSubcategory,
} from "../rwaSubcategoryTabs";
import { useFavoritesProps } from "../shared/hooks/useFavoritesExtraProps";
import type { SideMarketsScriptReturn } from "./sideMarkets.script";

const EXPAND_RWA_SUB_TAB_KEY = "paxeerNetwork_expand_markets_rwa_sel_sub_tab";
const EXPAND_COMMUNITY_SUB_TAB_KEY =
  "paxeerNetwork_expand_markets_community_sel_sub_tab";

export type SideMarketsProps = SideMarketsScriptReturn & {
  className?: string;
} & { panelSize?: "small" | "middle" | "large" };

export const SideMarkets: React.FC<SideMarketsProps> = (props) => {
  const { activeTab, onTabChange, className, tabSort, panelSize } = props;

  const { symbol, onSymbolChange } = useMarketsContext();
  const { getFavoritesProps } = useFavoritesProps();

  // Reuse expand-mode subcategory selection; collapsed UI has no secondary tabs.
  const [rwaSubcategory] = useLocalStorage<RwaSubcategory>(
    EXPAND_RWA_SUB_TAB_KEY,
    "all",
  );
  const [communitySubTab] = useLocalStorage<string>(
    EXPAND_COMMUNITY_SUB_TAB_KEY,
    "all",
  );
  const rwaSymbolsInfo = useRwaSymbolsInfoStore();

  const rwaDataFilter = useMemo(
    () => createRwaSubcategoryFilter(rwaSubcategory, rwaSymbolsInfo),
    [rwaSubcategory, rwaSymbolsInfo],
  );
  const communityDataFilter = useMemo(
    () => createCommunityBrokerFilter(communitySubTab),
    [communitySubTab],
  );

  const renderContent = () => {
    if (panelSize === "large") {
      return (
        <ExpandMarketsWidget
          activeTab={activeTab}
          onTabChange={onTabChange}
          symbol={symbol}
          onSymbolChange={onSymbolChange}
        />
      );
    }

    // Apply persisted show-low-vol preference without rendering the toggle UI.
    if (activeTab === MarketsTabName.Rwa) {
      return (
        <MarketsListWidget
          type={activeTab}
          dataFilter={rwaDataFilter}
          initialSort={tabSort[activeTab]}
          panelSize={"middle"}
        />
      );
    }
    if (activeTab === MarketsTabName.Community) {
      return (
        <MarketsListWidget
          type={MarketsTabName.All}
          dataFilter={communityDataFilter}
          initialSort={tabSort[MarketsTabName.Community]}
          panelSize={"middle"}
        />
      );
    }

    return (
      <MarketsListWidget
        type={activeTab}
        initialSort={tabSort[activeTab]}
        panelSize={"middle"}
        {...getFavoritesProps(activeTab)}
      />
    );
  };

  // Parent trading column uses flex-1/min-h-0; avoid nested calc heights that double-subtract.
  return (
    <Box
      width="100%"
      height="100%"
      className={cn(className, "oui-min-h-0 oui-min-w-0 oui-max-w-full")}
    >
      {renderContent()}
    </Box>
  );
};
