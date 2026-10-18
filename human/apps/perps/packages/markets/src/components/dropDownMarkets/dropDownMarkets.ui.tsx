import { PropsWithChildren } from "react";
import {
  Box,
  CloseIcon,
  cn,
  Flex,
  TabPanel,
  Tabs,
  DropdownMenuContent,
  DropdownMenuPortal,
  DropdownMenuRoot,
  DropdownMenuTrigger,
} from "@paxeer-network/ui";
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
import { useDropDownMarketsColumns } from "./column";
import { DropDownMarketsScriptReturn } from "./dropDownMarkets.script";

export type DropDownMarketsProps = DropDownMarketsScriptReturn & {
  contentClassName?: string;
};

export const DropDownMarkets: React.FC<
  PropsWithChildren<DropDownMarketsProps>
> = (props) => {
  return (
    <DropdownMenuRoot open={props.open} onOpenChange={props.onOpenChange}>
      <DropdownMenuTrigger asChild>{props.children}</DropdownMenuTrigger>
      <DropdownMenuPortal>
        <DropdownMenuContent
          onCloseAutoFocus={(e) => e.preventDefault()}
          onClick={(e) => e.stopPropagation()}
          align="start"
          alignOffset={-32}
          sideOffset={20}
          className={cn(
            "oui-markets-dropdown-menu-content oui-bg-base-8 oui-p-0",
            props.contentClassName,
          )}
        >
          <DropDownMarketsConetnt {...props} hide={props.hide} />
        </DropdownMenuContent>
      </DropdownMenuPortal>
    </DropdownMenuRoot>
  );
};

export const DropDownMarketsConetnt: React.FC<DropDownMarketsProps> = (
  props,
) => {
  const { activeTab, onTabChange, tabSort, onTabSort } = props;

  const getColumns = useDropDownMarketsColumns();
  const builtInTitles = useBuiltInTitles();

  const search = (
    <Flex
      className="oui-dropDownMarkets-search oui-items-center"
      mx={3}
      gapX={3}
      pt={3}
      pb={2}
    >
      <SearchInput
        classNames={{
          root: "oui-min-w-0 oui-flex-1",
        }}
      />
      <ShowLowVolumeFilter />
      <CloseIcon
        size={12}
        className="oui-dropDownMarkets-close-btn oui-cursor-pointer oui-text-base-contrast-80"
        onClick={props.hide}
        opacity={1}
      />
    </Flex>
  );

  const cls = "oui-h-[calc(100%_-_36px)]";

  const { getFavoritesProps, renderEmptyView } = useFavoritesProps();
  const { tabs } = useMarketsContext();
  const tabDataFilters = useCustomTabDataFilters(tabs);

  const renderBuiltInContent = (tabType: MarketsTabName) => {
    if (tabType === MarketsTabName.Rwa) {
      return (
        <RwaSubcategoryTabs
          storageKey="paxeerNetwork_dropdown_markets_rwa_sel_sub_tab"
          classNames={{
            tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
            tabsContent: "oui-h-full",
          }}
          className={cn("oui-dropDownMarkets-rwa-tabs", cls)}
          showScrollIndicator
          renderPanel={(_, dataFilter) => (
            <div className={cls}>
              <MarketsListWidget
                type={MarketsTabName.Rwa}
                initialSort={tabSort[tabType]}
                onSort={onTabSort(tabType)}
                getColumns={getColumns}
                tableClassNames={{
                  root: cn("oui-dropDownMarkets-list", "!oui-bg-base-8"),
                  scroll: "oui-pb-5 oui-px-1",
                }}
                rowClassName="!oui-h-[34px]"
                dataFilter={dataFilter}
              />
            </div>
          )}
        />
      );
    }

    return (
      <div className={cls}>
        <MarketsListWidget
          type={tabType}
          initialSort={tabSort[tabType]}
          onSort={onTabSort(tabType)}
          getColumns={getColumns}
          tableClassNames={{
            root: cn("oui-dropDownMarkets-list", "!oui-bg-base-8"),
            scroll: "oui-pb-5 oui-px-1",
          }}
          rowClassName="!oui-h-[34px]"
          {...getFavoritesProps(tabType)}
          emptyView={renderEmptyView({
            type: tabType,
            onClick: () => {
              onTabChange(MarketsTabName.All);
            },
          })}
        />
      </div>
    );
  };

  const renderCommunityContent = () => {
    return (
      <CommunityBrokerTabs
        storageKey="paxeerNetwork_dropdown_markets_community_sel_sub_tab"
        classNames={{
          tabsList: "oui-px-3 oui-pt-1 oui-pb-2",
          tabsContent: "oui-h-full",
        }}
        className={cn("oui-dropDownMarkets-community-tabs", cls)}
        showScrollIndicator
        renderPanel={(selected) => (
          <div className={cls}>
            <MarketsListWidget
              type={MarketsTabName.All}
              initialSort={tabSort[MarketsTabName.Community]}
              onSort={onTabSort(MarketsTabName.Community)}
              getColumns={getColumns}
              tableClassNames={{
                root: cn("oui-dropDownMarkets-list", "!oui-bg-base-8"),
                scroll: "oui-pb-5 oui-px-1",
              }}
              rowClassName="!oui-h-[34px]"
              dataFilter={createCommunityBrokerFilter(selected)}
            />
          </div>
        )}
      />
    );
  };

  const renderCustomContent = (key: string) => {
    return (
      <div className={cls}>
        <MarketsListWidget
          type={MarketsTabName.All}
          dataFilter={(data) => tabDataFilters[key]?.(data) ?? data}
          initialSort={tabSort[key]}
          onSort={onTabSort(key as MarketsTabName)}
          getColumns={getColumns}
          tableClassNames={{
            root: cn("oui-dropDownMarkets-list", "!oui-bg-base-8"),
            scroll: "oui-pb-5 oui-px-1",
          }}
          rowClassName="!oui-h-[34px]"
        />
      </div>
    );
  };

  return (
    <Box
      className={cn(
        "oui-markets-dropDownMarkets",
        "oui-overflow-hidden oui-font-semibold",
      )}
      height="100%"
      intensity={800}
    >
      {search}

      <Tabs
        variant="contained"
        size="md"
        value={activeTab}
        onValueChange={onTabChange}
        classNames={{
          tabsListContainer: "oui-px-3",
          tabsList: "oui-my-[6px]",
          tabsContent: "oui-h-full",
          scrollIndicator: "oui-mx-0",
        }}
        className={cn("oui-dropDownMarkets-tabs", cls)}
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
