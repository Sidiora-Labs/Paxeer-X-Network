import { useCallback, useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { Box, cn, Column, Flex, TabPanel, Tabs } from "@paxeer-network/ui";
import { CommunityBrokerTabs } from "../../../components/communityBrokerTabs";
import { MarketsListWidget } from "../../../components/marketsList";
import { useMarketsContext } from "../../../components/marketsProvider";
import { RwaSubcategoryTabs } from "../../../components/rwaSubcategoryTabs";
import { RwaIconTab } from "../../../components/rwaTab";
import { SearchInput } from "../../../components/searchInput";
import {
  get24hVolOIColumn,
  getLastAnd24hPercentageColumn,
  getSymbolColumn,
} from "../../../components/shared/column";
import { useFavoritesProps } from "../../../components/shared/hooks/useFavoritesExtraProps";
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
import { FavoriteInstance, MarketsTabName } from "../../../type";
import { UseMarketsDataListScript } from "./marketsDataList.script";

export type MobileMarketsDataListProps = UseMarketsDataListScript;

const TWO_LINE_MARKET_ROW_CLASS = "!oui-h-[54px]";

type BuiltInTabMeta = {
  title: React.ReactNode;
  icon?: React.ReactElement;
  value: string;
  tabName?: MarketsTabName;
};

const SearchWithLowVolumeFilter = ({ className }: { className?: string }) => (
  <Flex
    className={cn(
      "oui-mx-3 oui-min-w-0 oui-items-center oui-gap-x-2",
      className,
    )}
  >
    <SearchInput
      classNames={{
        root: "oui-min-w-0 oui-flex-1",
      }}
    />
    <ShowLowVolumeFilter />
  </Flex>
);

export const MobileMarketsDataList: React.FC<MobileMarketsDataListProps> = (
  props,
) => {
  const { activeTab, onTabChange, tabSort, onTabSort } = props;
  const { t } = useTranslation();
  const { tabs } = useMarketsContext();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const getColumns = useCallback(
    (favorite: FavoriteInstance, isFavoriteList = false) => {
      return [
        getSymbolColumn(favorite, isFavoriteList, {
          stackLeverageInSecondRow: true,
        }),
        get24hVolOIColumn(),
        getLastAnd24hPercentageColumn(favorite, isFavoriteList),
      ] as Column[];
    },
    [t],
  );

  const { getFavoritesProps } = useFavoritesProps();

  const builtInMeta = useMemo<Record<string, BuiltInTabMeta>>(
    () => ({
      favorites: {
        title: <FavoritesIcon />,
        value: "favorites",
        tabName: MarketsTabName.Favorites,
      },
      community: {
        title: t("markets.community"),
        value: "community",
      },
      all: {
        title: t("markets.allMarkets"),
        icon: <AllMarketsIcon />,
        value: "all",
        tabName: MarketsTabName.All,
      },
      crypto: {
        title: t("markets.crypto"),
        value: "crypto",
        tabName: MarketsTabName.Crypto,
      },
      rwa: {
        title: <RwaIconTab />,
        value: "rwa",
        tabName: MarketsTabName.Rwa,
      },
      preTge: {
        title: t("markets.preTge"),
        value: "preTge",
        tabName: MarketsTabName.PreTge,
      },
      newListing: {
        title: t("markets.newListings"),
        icon: <NewListingsIcon />,
        value: "new",
        tabName: MarketsTabName.NewListing,
      },
    }),
    [t],
  );

  const renderTab = (type: MarketsTabName) => {
    const list = (
      <MarketsListWidget
        type={type}
        initialSort={tabSort[type]}
        onSort={onTabSort(type)}
        getColumns={getColumns}
        rowClassName={TWO_LINE_MARKET_ROW_CLASS}
        {...getFavoritesProps(type)}
      />
    );

    return (
      <>
        <SearchWithLowVolumeFilter
          className={cn(
            "oui-mt-5",
            activeTab === MarketsTabName.Favorites ? "oui-mb-4" : "oui-mb-2",
          )}
        />
        {type === MarketsTabName.Rwa ? (
          <RwaSubcategoryTabs
            storageKey="paxeerNetwork_mobile_markets_datalist_rwa_sel_sub_tab"
            size="md"
            classNames={{
              tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
              tabsContent: "oui-h-full",
              scrollIndicator: "oui-mx-3",
            }}
            className="oui-mobileMarketsDataList-rwa-tabs"
            showScrollIndicator
            renderPanel={(_, dataFilter) => (
              <MarketsListWidget
                type={type}
                dataFilter={dataFilter}
                initialSort={tabSort[type]}
                onSort={onTabSort(type)}
                getColumns={getColumns}
                rowClassName={TWO_LINE_MARKET_ROW_CLASS}
              />
            )}
          />
        ) : (
          list
        )}
      </>
    );
  };

  const renderCommunityList = (selected: string) => {
    return (
      <MarketsListWidget
        type={MarketsTabName.All}
        initialSort={tabSort[MarketsTabName.Community]}
        onSort={onTabSort(MarketsTabName.Community)}
        getColumns={getColumns}
        rowClassName={TWO_LINE_MARKET_ROW_CLASS}
        dataFilter={createCommunityBrokerFilter(selected)}
      />
    );
  };

  return (
    <Box id="oui-markets-list" intensity={900} py={3} mt={2} r="2xl">
      <Tabs
        variant="contained"
        size="lg"
        value={activeTab}
        onValueChange={onTabChange}
        classNames={{
          scrollIndicator: "oui-mx-3",
        }}
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
                title={title}
                icon={
                  tab.type === "favorites" || tab.type === "rwa"
                    ? undefined
                    : resolveTabTriggerIcon(tab, meta.icon)
                }
                value={meta.value}
              >
                {tab.type === "community" ? (
                  <>
                    <SearchWithLowVolumeFilter className="oui-mb-2 oui-mt-5" />
                    <CommunityBrokerTabs
                      storageKey="paxeerNetwork_mobile_markets_datalist_community_sel_sub_tab"
                      size="md"
                      classNames={{
                        tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
                        tabsContent: "oui-h-full",
                        scrollIndicator: "oui-mx-3",
                      }}
                      className="oui-mobileMarketsDataList-community-tabs"
                      showScrollIndicator
                      renderPanel={renderCommunityList}
                    />
                  </>
                ) : (
                  renderTab(meta.tabName!)
                )}
              </TabPanel>
            );
          }

          // Custom category
          return (
            <TabPanel
              key={key}
              title={composeTabTitle(tab.name ?? key, {
                icon: resolveTabTriggerIcon(tab),
                suffix: tab.suffix,
              })}
              value={key}
            >
              <SearchWithLowVolumeFilter className="oui-mb-2 oui-mt-5" />
              <MarketsListWidget
                type={MarketsTabName.All}
                dataFilter={(data) => tabDataFilters[key]?.(data) ?? data}
                initialSort={tabSort[key as MarketsTabName]}
                onSort={onTabSort(key as MarketsTabName)}
                getColumns={getColumns}
                rowClassName={TWO_LINE_MARKET_ROW_CLASS}
              />
            </TabPanel>
          );
        })}
      </Tabs>
    </Box>
  );
};
