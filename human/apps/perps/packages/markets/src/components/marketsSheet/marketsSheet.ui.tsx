import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, TabPanel, Tabs, Text } from "@paxeer-network/ui";
import { createCommunityBrokerFilter } from "../../hooks/useCommunityTabs";
import { MarketsTabName } from "../../type";
import { CommunityBrokerTabs } from "../communityBrokerTabs";
import { MarketsListWidget } from "../marketsList";
import { useMarketsContext } from "../marketsProvider";
import { RwaSubcategoryTabs } from "../rwaSubcategoryTabs";
import { RwaTab } from "../rwaTab";
import { SearchInput } from "../searchInput";
import { useFavoritesProps } from "../shared/hooks/useFavoritesExtraProps";
import { ShowLowVolumeFilter } from "../shared/showLowVolumeFilter";
import {
  isBuiltInMarketTab,
  tabKey,
  resolveTabTitle,
  useBuiltInTitles,
  useCustomTabDataFilters,
} from "../shared/tabUtils";
import { getMarketsSheetColumns } from "./column";
import { MarketsSheetScriptReturn } from "./marketsSheet.script";

export type MarketsSheetProps = MarketsSheetScriptReturn & {
  className?: string;
};

export const MarketsSheet: React.FC<MarketsSheetProps> = (props) => {
  const { className, tabSort, onTabSort } = props;

  const { t } = useTranslation();

  const { getFavoritesProps, renderEmptyView } = useFavoritesProps();
  const { tabs } = useMarketsContext();
  const builtInTitles = useBuiltInTitles();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const renderBuiltInContent = (tabType: MarketsTabName) => {
    const isFavorites = tabType === MarketsTabName.Favorites;

    if (tabType === MarketsTabName.Rwa) {
      return (
        <RwaSubcategoryTabs
          storageKey="paxeerNetwork_markets_sheet_rwa_sel_sub_tab"
          classNames={{
            tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
            tabsContent: "oui-h-full",
          }}
          className={cn(
            "oui-marketsSheet-rwa-tabs",
            "oui-h-[calc(100%_-_36px)]",
          )}
          showScrollIndicator
          renderPanel={(_, dataFilter) => (
            <MarketsListWidget
              type={tabType}
              dataFilter={dataFilter}
              initialSort={tabSort[tabType]}
              onSort={onTabSort(tabType)}
              getColumns={getMarketsSheetColumns}
              tableClassNames={{
                root: cn("oui-marketsSheet-list", "!oui-bg-base-8"),
                scroll: cn(
                  "oui-pb-[env(safe-area-inset-bottom,_20px)]",
                  "oui-h-[calc(100%_-_40px)]",
                ),
              }}
            />
          )}
        />
      );
    }

    return (
      <MarketsListWidget
        type={tabType}
        initialSort={tabSort[tabType]}
        onSort={onTabSort(tabType)}
        getColumns={getMarketsSheetColumns}
        tableClassNames={{
          root: cn("oui-marketsSheet-list", "!oui-bg-base-8"),
          scroll: cn(
            "oui-pb-[env(safe-area-inset-bottom,_20px)]",
            isFavorites
              ? "oui-h-[calc(100%_-_70px)]"
              : "oui-h-[calc(100%_-_40px)]",
          ),
        }}
        emptyView={renderEmptyView({
          type: tabType,
          onClick: () => {
            props.onTabChange(MarketsTabName.All);
          },
        })}
        {...getFavoritesProps(tabType)}
      />
    );
  };

  const renderCommunityContent = () => {
    return (
      <CommunityBrokerTabs
        storageKey="paxeerNetwork_markets_sheet_community_sel_sub_tab"
        classNames={{
          tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
          tabsContent: "oui-h-full",
        }}
        className={cn(
          "oui-marketsSheet-community-tabs",
          "oui-h-[calc(100%_-_36px)]",
        )}
        showScrollIndicator
        renderPanel={(selected) => (
          <MarketsListWidget
            type={MarketsTabName.All}
            initialSort={tabSort[MarketsTabName.Community]}
            onSort={onTabSort(MarketsTabName.Community)}
            getColumns={getMarketsSheetColumns}
            tableClassNames={{
              root: cn("oui-marketsSheet-list", "!oui-bg-base-8"),
              scroll: cn(
                "oui-pb-[env(safe-area-inset-bottom,_20px)]",
                "oui-h-[calc(100%_-_40px)]",
              ),
            }}
            dataFilter={createCommunityBrokerFilter(selected)}
          />
        )}
      />
    );
  };

  const renderCustomContent = (key: string) => {
    return (
      <MarketsListWidget
        type={MarketsTabName.All}
        dataFilter={(data) => tabDataFilters[key]?.(data) ?? data}
        initialSort={tabSort[key]}
        onSort={onTabSort(key as MarketsTabName)}
        getColumns={getMarketsSheetColumns}
        tableClassNames={{
          root: cn("oui-marketsSheet-list", "!oui-bg-base-8"),
          scroll: cn(
            "oui-pb-[env(safe-area-inset-bottom,_20px)]",
            "oui-h-[calc(100%_-_40px)]",
          ),
        }}
      />
    );
  };

  return (
    <Box
      height="100%"
      className={cn("oui-markets-marketsSheet", "oui-font-semibold", className)}
    >
      <Box className="oui-marketsSheet-header" px={3} mt={3} mb={2}>
        <Text size="base" intensity={80}>
          {t("common.markets")}
        </Text>
        <div className="oui-mt-4 oui-flex oui-min-w-0 oui-items-center oui-gap-x-2">
          <SearchInput
            classNames={{
              root: "oui-marketsSheet-search-input oui-min-w-0 oui-flex-1",
            }}
          />
          <ShowLowVolumeFilter />
        </div>
      </Box>

      <Tabs
        variant="contained"
        size="md"
        value={props.activeTab}
        onValueChange={props.onTabChange as (tab: string) => void}
        classNames={{
          tabsList: cn("oui-my-[6px]"),
          tabsContent: "oui-h-full",
          scrollIndicator: "oui-mx-3",
        }}
        className={cn("oui-marketsSheet-tabs", "oui-h-[calc(100%_-_92px)]")}
        showScrollIndicator
      >
        {tabs?.map((tab, index) => {
          const key = tabKey(tab, index);
          const isBuiltIn = isBuiltInMarketTab(tab);
          const isCommunity =
            isBuiltIn && tab.type === MarketsTabName.Community;

          return (
            <TabPanel
              key={key}
              classNames={{
                trigger: `oui-tabs-${key}-trigger`,
                content: `oui-tabs-${key}-content`,
              }}
              title={resolveTabTitle(tab, builtInTitles, <RwaTab />)}
              value={key}
            >
              {isCommunity
                ? renderCommunityContent()
                : isBuiltIn
                  ? renderBuiltInContent(tab.type as MarketsTabName)
                  : renderCustomContent(key)}
            </TabPanel>
          );
        })}
      </Tabs>
    </Box>
  );
};
