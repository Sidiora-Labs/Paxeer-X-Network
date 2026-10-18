import React from "react";
import {
  Box,
  cn,
  Tabs,
  TabPanel,
  Flex,
  TokenIcon,
  Badge,
  EmptyDataState,
} from "@paxeer-network/ui";
import { createCommunityBrokerFilter } from "../../hooks/useCommunityTabs";
import {
  MarketsTabName,
  type FavoriteInstance,
  type SortType,
} from "../../type";
import { CommunityBrokerTabs } from "../communityBrokerTabs";
import { FavoritesTabWidget } from "../favoritesTabs";
import { MarketsListWidget } from "../marketsList";
import { useMarketsContext } from "../marketsProvider";
import { RwaSubcategoryTabs } from "../rwaSubcategoryTabs";
import { RwaIconTab } from "../rwaTab";
import { useFavoritesProps } from "../shared/hooks/useFavoritesExtraProps";
import { ShowLowVolumeFilter } from "../shared/showLowVolumeFilter";
import {
  isBuiltInMarketTab,
  tabKey,
  resolveTabTitle,
  useBuiltInTitles,
  useCustomTabDataFilters,
} from "../shared/tabUtils";
import { SymbolDisplay } from "../symbolDisplay";

const LIST_ROW_COMPACT = "oui-h-auto";
const TABLE_CLASSNAMES = {
  scroll: "oui-px-0",
  header: "oui-hidden oui-h-0 oui-p-0",
};

const LazySearchInput = React.lazy(() =>
  import("../searchInput").then((mod) => ({ default: mod.SearchInput })),
);

export type SubMenuMarketsProps = {
  activeTab: MarketsTabName;
  onTabChange: (value: string) => void;
  className?: string;
  tabSort: Record<MarketsTabName, SortType | undefined>;
  onTabSort: (type: MarketsTabName) => (sort?: SortType) => void;
};

const cls = "oui-h-[calc(100%_-_36px)]";

type MarketTabPanelProps = {
  type: MarketsTabName;
  getColumns: (_favorite: FavoriteInstance, _isFavoritesList: boolean) => any[];
  initialSort?: SortType;
  onSort?: (sort?: SortType) => void;
  dataFilter?: any;
  renderHeader?: (favorite: FavoriteInstance) => React.ReactNode;
  emptyView?: React.ReactNode;
};

const MarketTabPanel: React.FC<MarketTabPanelProps> = ({
  type,
  getColumns,
  initialSort,
  onSort,
  dataFilter,
  renderHeader,
  emptyView,
}) => {
  return (
    <div className={cls}>
      <MarketsListWidget
        type={type}
        getColumns={getColumns}
        tableClassNames={TABLE_CLASSNAMES}
        rowClassName={LIST_ROW_COMPACT}
        dataFilter={dataFilter}
        renderHeader={renderHeader}
        initialSort={initialSort}
        onSort={onSort}
        emptyView={emptyView}
      />
    </div>
  );
};

export const SubMenuMarkets: React.FC<SubMenuMarketsProps> = (props) => {
  const { activeTab, onTabChange, className, tabSort, onTabSort } = props;
  const { getFavoritesProps } = useFavoritesProps();
  const builtInTitles = useBuiltInTitles();
  const { tabs } = useMarketsContext();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const getColumns = (
    _favorite: FavoriteInstance,
    _isFavoritesList: boolean,
  ) => {
    return [
      {
        title: "",
        dataIndex: "symbol",
        className: "oui-p-2",
        render: (_: unknown, record: any) => {
          return (
            <div className="oui-mx-[-8px]">
              <Flex gapX={1} itemAlign="center">
                <TokenIcon symbol={record.symbol} className="oui-size-[18px]" />
                <SymbolDisplay formatString="base" size="2xs">
                  {record.symbol}
                </SymbolDisplay>
                <Badge size="xs" color="primary">
                  {record.leverage}x
                </Badge>
              </Flex>
            </div>
          );
        },
      },
    ];
  };

  const renderBuiltInContent = (tabType: MarketsTabName) => {
    if (tabType === MarketsTabName.Rwa) {
      return (
        <RwaSubcategoryTabs
          storageKey="paxeerNetwork_submenu_markets_rwa_sel_sub_tab"
          classNames={{
            tabsList: "oui-px-1 oui-pt-1 oui-pb-2",
            tabsContent: "oui-h-full",
            scrollIndicator: "oui-mx-1",
          }}
          className={cn("oui-subMenuMarkets-rwa-tabs", cls)}
          showScrollIndicator
          renderPanel={(_, dataFilter) => (
            <MarketTabPanel
              type={tabType}
              getColumns={getColumns}
              dataFilter={dataFilter}
              initialSort={tabSort[tabType]}
              onSort={onTabSort(tabType)}
            />
          )}
        />
      );
    }

    if (tabType === MarketsTabName.Favorites) {
      const favProps = getFavoritesProps(MarketsTabName.Favorites) as {
        dataFilter?: any;
      };

      return (
        <MarketTabPanel
          type={MarketsTabName.Favorites}
          getColumns={getColumns}
          dataFilter={favProps?.dataFilter}
          renderHeader={(favorite) => (
            <Box className="oui-px-1 oui-my-1">
              <FavoritesTabWidget favorite={favorite} size="sm" />
            </Box>
          )}
          initialSort={tabSort[MarketsTabName.Favorites]}
          onSort={onTabSort(MarketsTabName.Favorites)}
          emptyView={<EmptyDataState />}
        />
      );
    }

    return (
      <MarketTabPanel
        type={tabType}
        getColumns={getColumns}
        initialSort={tabSort[tabType]}
        onSort={onTabSort(tabType)}
      />
    );
  };

  const renderCommunityContent = () => {
    return (
      <CommunityBrokerTabs
        storageKey="paxeerNetwork_submenu_markets_community_sel_sub_tab"
        classNames={{
          tabsList: "oui-px-1 oui-pt-1 oui-pb-2",
          tabsContent: "oui-h-full",
          scrollIndicator: "oui-mx-1",
        }}
        className={cn("oui-subMenuMarkets-community-tabs", cls)}
        showScrollIndicator
        renderPanel={(selected) => (
          <MarketTabPanel
            type={MarketsTabName.All}
            getColumns={getColumns}
            dataFilter={createCommunityBrokerFilter(selected)}
            initialSort={tabSort[MarketsTabName.Community]}
            onSort={onTabSort(MarketsTabName.Community)}
          />
        )}
      />
    );
  };

  const renderCustomContent = (key: string) => {
    return (
      <div className={cls}>
        <MarketsListWidget
          type={MarketsTabName.All}
          getColumns={getColumns}
          tableClassNames={TABLE_CLASSNAMES}
          rowClassName={LIST_ROW_COMPACT}
          dataFilter={(data: any[]) => tabDataFilters[key]?.(data) ?? data}
          initialSort={tabSort[key as MarketsTabName]}
          onSort={onTabSort(key as MarketsTabName)}
        />
      </div>
    );
  };

  return (
    <Box
      className={cn(
        "oui-subMenuMarkets oui-overflow-hidden oui-bg-base-9 oui-font-semibold",
        className,
      )}
      height="100%"
    >
      <Box className="oui-flex oui-min-w-0 oui-items-center oui-gap-x-2 oui-p-1">
        <React.Suspense fallback={null}>
          <LazySearchInput
            classNames={{
              root: "oui-min-w-0 oui-flex-1",
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
          tabsList: cn("oui-my-1"),
          tabsContent: "oui-h-full",
          scrollIndicator: "oui-mx-1",
        }}
        className={cn(cls, "oui-subMenuMarkets-tabs oui-my-1.5")}
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
              title={resolveTabTitle(tab, builtInTitles, <RwaIconTab />)}
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
