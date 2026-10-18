import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, Flex, TabPanel, Tabs } from "@paxeer-network/ui";
import { CommunityBrokerTabs } from "../../../components/communityBrokerTabs";
import { FavoritesEmpty } from "../../../components/favoritesEmpty";
import type { MarketsListFullType } from "../../../components/marketsListFull/marketsListFull.script";
import { useMarketsContext } from "../../../components/marketsProvider";
import { RwaSubcategoryTabs } from "../../../components/rwaSubcategoryTabs";
import { RwaIconTab } from "../../../components/rwaTab";
import { ShowLowVolumeFilter } from "../../../components/shared/showLowVolumeFilter";
import {
  composeTabTitle,
  isBuiltInMarketTab,
  tabKey,
  resolveTabTitle,
  resolveTabTriggerIcon,
  useCustomTabDataFilters,
} from "../../../components/shared/tabUtils";
import { createCommunityBrokerFilter } from "../../../hooks/useCommunityTabs";
import { AllMarketsIcon, FavoritesIcon, NewListingsIcon } from "../../../icons";
import { MarketsTabName, SortType } from "../../../type";
import { UseMarketsDataListScript } from "./marketsDataList.script";

const LazySearchInput = React.lazy(() =>
  import("../../../components/searchInput").then((mod) => {
    return { default: mod.SearchInput };
  }),
);

const LazyFavoritesListFullWidget = React.lazy(() =>
  import("../../../components/favoritesListFull").then((mod) => {
    return { default: mod.FavoritesListFullWidget };
  }),
);

const LazyMarketsListFullWidget = React.lazy(() =>
  import("../../../components/marketsListFull").then((mod) => {
    return { default: mod.MarketsListFullWidget };
  }),
);

type BuiltInTabMeta = {
  title: React.ReactNode;
  icon?: React.ReactElement;
  value: string;
  testid?: string;
  /** list widget type — undefined means special handling (favorites) */
  listType?: MarketsListFullType;
  initialSort: SortType;
};

const DEFAULT_SORT: SortType = { sortKey: "24h_amount", sortOrder: "desc" };

export type MarketsDataListProps = UseMarketsDataListScript;

export const MarketsDataList: React.FC<MarketsDataListProps> = (props) => {
  const { searchValue, activeTab, onTabChange } = props;
  const { t } = useTranslation();
  const { tabs } = useMarketsContext();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const builtInMeta = useMemo<Record<string, BuiltInTabMeta>>(
    () => ({
      favorites: {
        title: <FavoritesIcon />,
        value: "favorites",
        testid: "oui-testid-markets-favorites-tab",
        initialSort: DEFAULT_SORT,
      },
      community: {
        title: t("markets.community"),
        value: "community",
        testid: "oui-testid-markets-community-tab",
        initialSort: DEFAULT_SORT,
      },
      all: {
        title: t("markets.allMarkets"),
        icon: <AllMarketsIcon />,
        value: "all",
        testid: "oui-testid-markets-all-tab",
        listType: "all",
        initialSort: DEFAULT_SORT,
      },
      crypto: {
        title: t("markets.crypto"),
        value: "crypto",
        testid: "oui-testid-markets-crypto-tab",
        listType: "crypto",
        initialSort: DEFAULT_SORT,
      },
      rwa: {
        title: <RwaIconTab />,
        value: "rwa",
        testid: "oui-testid-markets-rwa-tab",
        listType: "rwa",
        initialSort: DEFAULT_SORT,
      },
      preTge: {
        title: t("markets.preTge"),
        value: "preTge",
        testid: "oui-testid-markets-preTge-tab",
        listType: "preTge",
        initialSort: DEFAULT_SORT,
      },
      newListing: {
        title: t("markets.newListings"),
        icon: <NewListingsIcon />,
        value: "new",
        testid: "oui-testid-markets-newListing-tab",
        listType: "new",
        initialSort: { sortKey: "created_time", sortOrder: "desc" },
      },
    }),
    [t],
  );

  return (
    <Box
      id="oui-markets-list"
      intensity={900}
      p={6}
      r="2xl"
      className="oui-mt-6"
    >
      <Tabs
        variant="contained"
        size="xl"
        value={activeTab}
        onValueChange={onTabChange}
        trailing={
          <Flex className="oui-my-1 oui-min-w-0 oui-items-center oui-gap-x-3">
            <React.Suspense fallback={null}>
              <LazySearchInput
                classNames={{
                  root: "oui-min-w-0 oui-w-[240px]",
                }}
              />
            </React.Suspense>
            <ShowLowVolumeFilter />
          </Flex>
        }
        classNames={{
          tabsList: "oui-tabs-markets-list",
          tabsContent: "oui-tabs-markets-content",
          scrollIndicator: "oui-me-3",
        }}
        className="oui-markets-list-tabs"
        showScrollIndicator
      >
        {tabs?.map((tab, index) => {
          const key = tabKey(tab, index);
          const isBuiltIn = isBuiltInMarketTab(tab);

          // Built-in tab
          const meta = isBuiltIn ? builtInMeta[tab.type] : undefined;
          if (isBuiltIn && meta) {
            const title =
              tab.type === "favorites"
                ? composeTabTitle(tab.name, {
                    icon: resolveTabTriggerIcon(tab, <FavoritesIcon />),
                    suffix: tab.suffix,
                  })
                : tab.type === "rwa"
                  ? resolveTabTitle(tab, {}, <RwaIconTab />)
                  : composeTabTitle(tab.name ?? meta.title, {
                      suffix: tab.suffix,
                    });

            return (
              <TabPanel
                key={key}
                classNames={{
                  trigger: `oui-tabs-${meta.value}-trigger`,
                  content: `oui-tabs-${meta.value}-content`,
                }}
                title={title}
                icon={
                  tab.type === "favorites" || tab.type === "rwa"
                    ? undefined
                    : resolveTabTriggerIcon(tab, meta.icon)
                }
                value={meta.value}
                testid={meta.testid}
              >
                <React.Suspense fallback={null}>
                  {tab.type === "favorites" ? (
                    <LazyFavoritesListFullWidget
                      emptyView={
                        !searchValue && (
                          <FavoritesEmpty
                            onClick={() => onTabChange(MarketsTabName.All)}
                          />
                        )
                      }
                    />
                  ) : tab.type === "community" ? (
                    <CommunityBrokerTabs
                      storageKey="paxeerNetwork_markets_datalist_community_sel_sub_tab"
                      size="md"
                      classNames={{
                        tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
                        tabsContent: "oui-h-full",
                      }}
                      className="oui-marketsDataList-community-tabs"
                      showScrollIndicator
                      renderPanel={(selected) => (
                        <React.Suspense fallback={null}>
                          <LazyMarketsListFullWidget
                            type="all"
                            initialSort={meta.initialSort}
                            dataFilter={createCommunityBrokerFilter(selected)}
                          />
                        </React.Suspense>
                      )}
                    />
                  ) : tab.type === "rwa" ? (
                    <RwaSubcategoryTabs
                      storageKey="paxeerNetwork_markets_datalist_rwa_sel_sub_tab"
                      size="md"
                      classNames={{
                        tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
                        tabsContent: "oui-h-full",
                      }}
                      className="oui-marketsDataList-rwa-tabs"
                      showScrollIndicator
                      renderPanel={(_, dataFilter) => (
                        <React.Suspense fallback={null}>
                          <LazyMarketsListFullWidget
                            type="rwa"
                            initialSort={meta.initialSort}
                            dataFilter={dataFilter}
                          />
                        </React.Suspense>
                      )}
                    />
                  ) : (
                    <LazyMarketsListFullWidget
                      type={meta.listType!}
                      initialSort={meta.initialSort}
                    />
                  )}
                </React.Suspense>
              </TabPanel>
            );
          }

          // Custom category
          return (
            <TabPanel
              key={key}
              classNames={{
                trigger: `oui-tabs-${key}-trigger`,
                content: `oui-tabs-${key}-content`,
              }}
              title={composeTabTitle(tab.name ?? key, {
                icon: resolveTabTriggerIcon(tab),
                suffix: tab.suffix,
              })}
              value={key}
            >
              <React.Suspense fallback={null}>
                <LazyMarketsListFullWidget
                  type="all"
                  initialSort={DEFAULT_SORT}
                  dataFilter={tabDataFilters[key]}
                />
              </React.Suspense>
            </TabPanel>
          );
        })}
      </Tabs>
    </Box>
  );
};
