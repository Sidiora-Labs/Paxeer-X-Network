import React from "react";
import { Box, cn, TabPanel, Tabs } from "@paxeer-network/ui";
import { createCommunityBrokerFilter } from "../../hooks/useCommunityTabs";
import { MarketsTabName } from "../../type";
import { CommunityBrokerTabs } from "../communityBrokerTabs";
import { useMarketsContext } from "../marketsProvider";
import { RwaSubcategoryTabs } from "../rwaSubcategoryTabs";
import { RwaTab } from "../rwaTab";
import { useFavoritesProps } from "../shared/hooks/useFavoritesExtraProps";
import { ShowLowVolumeFilter } from "../shared/showLowVolumeFilter";
import {
  isBuiltInMarketTab,
  tabKey,
  resolveTabTitle,
  useBuiltInTitles,
  useCustomTabDataFilters,
} from "../shared/tabUtils";
import type { ExpandMarketsScriptReturn } from "./expandMarkets.script";

const LazySearchInput = React.lazy(() =>
  import("../searchInput").then((mod) => {
    return { default: mod.SearchInput };
  }),
);

const LazyMarketsListWidget = React.lazy(() =>
  import("../marketsList").then((mod) => {
    return { default: mod.MarketsListWidget };
  }),
);

export type ExpandMarketsProps = ExpandMarketsScriptReturn;

/** Root classes for MarketsList DataTable so the scroll area can flex inside tab panels. */
const expandListTableClassNames = {
  root: cn(
    "oui-expandMarkets-list",
    /** min-w-0: flex/grid descendants may shrink so table-fixed + w-full respect parent width. */
    "oui-flex oui-min-h-0 oui-w-full oui-min-w-0 oui-max-w-full oui-flex-1 oui-flex-col",
  ),
  scroll: cn("oui-min-h-0 oui-w-full oui-min-w-0 oui-max-w-full oui-flex-1"),
} as const;

/**
 * Wraps lazy list content so Suspense participates in height (flex item), not only intrinsic height.
 * TabsContent stays a simple flex-1 min-h-0 slot; flex column lives here to avoid Radix/layout conflicts.
 */
const ExpandMarketsListPanel: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => (
  <div className="oui-flex oui-size-full oui-min-h-0 oui-min-w-0 oui-flex-col">
    <div className="oui-flex oui-min-h-0 oui-w-full oui-min-w-0 oui-flex-1 oui-flex-col oui-overflow-hidden">
      {children}
    </div>
  </div>
);

export const ExpandMarkets: React.FC<ExpandMarketsProps> = (props) => {
  const { activeTab, onTabChange, tabSort, onTabSort } = props;

  const { getFavoritesProps, renderEmptyView } = useFavoritesProps();
  const builtInTitles = useBuiltInTitles();
  const { tabs } = useMarketsContext();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const renderBuiltInContent = (type: string) => {
    const tabType = type as MarketsTabName;

    if (tabType === MarketsTabName.Rwa) {
      return (
        <RwaSubcategoryTabs
          storageKey="paxeerNetwork_expand_markets_rwa_sel_sub_tab"
          classNames={{
            tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
            tabsListContainer: "oui-shrink-0",
            tabsContent:
              "oui-min-h-0 oui-min-w-0 oui-w-full oui-flex-1 oui-overflow-hidden",
          }}
          className={cn(
            "oui-expandMarkets-rwa-tabs",
            "oui-flex oui-size-full oui-min-h-0 oui-min-w-0 oui-flex-col",
          )}
          showScrollIndicator
          renderPanel={(_, dataFilter) => (
            <ExpandMarketsListPanel>
              <React.Suspense fallback={null}>
                <LazyMarketsListWidget
                  type={tabType}
                  dataFilter={dataFilter}
                  initialSort={tabSort[type]}
                  onSort={onTabSort(tabType)}
                  tableClassNames={{
                    root: expandListTableClassNames.root,
                    scroll: cn(
                      expandListTableClassNames.scroll,
                      "oui-px-1",
                      "oui-pb-2",
                    ),
                  }}
                />
              </React.Suspense>
            </ExpandMarketsListPanel>
          )}
        />
      );
    }

    return (
      <ExpandMarketsListPanel>
        <React.Suspense fallback={null}>
          <LazyMarketsListWidget
            type={tabType}
            initialSort={tabSort[type]}
            onSort={onTabSort(tabType)}
            tableClassNames={{
              root: expandListTableClassNames.root,
              scroll: cn(
                expandListTableClassNames.scroll,
                "oui-px-1",
                tabType === MarketsTabName.Favorites ? "oui-pb-9" : "oui-pb-2",
              ),
            }}
            {...getFavoritesProps(tabType)}
            emptyView={renderEmptyView({
              type: tabType,
              onClick: () => {
                onTabChange(MarketsTabName.All);
              },
            })}
          />
        </React.Suspense>
      </ExpandMarketsListPanel>
    );
  };

  const renderCommunityContent = () => {
    return (
      <CommunityBrokerTabs
        storageKey="paxeerNetwork_expand_markets_community_sel_sub_tab"
        classNames={{
          tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
          tabsListContainer: "oui-shrink-0",
          tabsContent:
            "oui-min-h-0 oui-min-w-0 oui-w-full oui-flex-1 oui-overflow-hidden",
        }}
        className={cn(
          "oui-expandMarkets-community-tabs",
          "oui-flex oui-size-full oui-min-h-0 oui-min-w-0 oui-flex-col",
        )}
        showScrollIndicator
        renderPanel={(selected) => (
          <ExpandMarketsListPanel>
            <React.Suspense fallback={null}>
              <LazyMarketsListWidget
                type={MarketsTabName.All}
                initialSort={tabSort[MarketsTabName.Community]}
                onSort={onTabSort(MarketsTabName.Community)}
                tableClassNames={{
                  root: expandListTableClassNames.root,
                  scroll: cn(
                    expandListTableClassNames.scroll,
                    "oui-px-1",
                    "oui-pb-2",
                  ),
                }}
                dataFilter={createCommunityBrokerFilter(selected)}
              />
            </React.Suspense>
          </ExpandMarketsListPanel>
        )}
      />
    );
  };

  const renderCustomContent = (key: string) => {
    return (
      <ExpandMarketsListPanel>
        <React.Suspense fallback={null}>
          <LazyMarketsListWidget
            type={MarketsTabName.All}
            dataFilter={(data) => tabDataFilters[key]?.(data) ?? data}
            initialSort={tabSort[key]}
            onSort={onTabSort(key as MarketsTabName)}
            tableClassNames={{
              root: expandListTableClassNames.root,
              scroll: cn(
                expandListTableClassNames.scroll,
                "oui-px-1",
                "oui-pb-2",
              ),
            }}
          />
        </React.Suspense>
      </ExpandMarketsListPanel>
    );
  };

  return (
    <Box
      className={cn(
        "oui-markets-expandMarkets",
        /** Search row + tabs/list: second row minmax(0,1fr) passes a bounded height into Tabs. */
        "oui-grid oui-size-full oui-min-h-0 oui-min-w-0 oui-grid-rows-[auto_minmax(0,1fr)] oui-overflow-hidden oui-font-semibold",
      )}
      height="100%"
    >
      <Box
        className="oui-expandMarkets-header oui-flex oui-min-w-0 oui-items-center oui-gap-x-3 oui-pt-px"
        px={3}
        pb={2}
      >
        <React.Suspense fallback={null}>
          <LazySearchInput
            classNames={{
              root: "oui-expandMarkets-search-input oui-min-w-0 oui-flex-1",
            }}
          />
        </React.Suspense>
        <ShowLowVolumeFilter />
      </Box>
      <Tabs
        variant="contained"
        size="md"
        value={activeTab}
        onValueChange={onTabChange}
        classNames={{
          /** Keep tab row from shrinking when Tabs root is a column flex container. */
          tabsListContainer: "oui-shrink-0",
          tabsList: cn("oui-my-[6px]"),
          /** Occupy remaining height under triggers; inner ExpandMarketsListPanel supplies flex column. */
          tabsContent:
            "oui-min-h-0 oui-min-w-0 oui-w-full oui-flex-1 oui-overflow-hidden",
          scrollIndicator: "oui-mx-3",
        }}
        className={cn(
          "oui-expandMarkets-tabs",
          "oui-flex oui-size-full oui-min-h-0 oui-min-w-0 oui-flex-col",
        )}
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
                  ? renderBuiltInContent(tab.type)
                  : renderCustomContent(key)}
            </TabPanel>
          );
        })}
      </Tabs>
    </Box>
  );
};
