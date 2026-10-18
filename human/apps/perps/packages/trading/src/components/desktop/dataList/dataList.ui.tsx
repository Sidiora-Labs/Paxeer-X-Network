import React from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AssetsModule } from "@paxeer-network/portfolio";
import { OrderStatus } from "@paxeer-network/types";
import {
  Box,
  Divider,
  Flex,
  InfoCircleIcon,
  ScrollArea,
  TabPanel,
  Tabs,
  Tooltip,
} from "@paxeer-network/ui";
import type { TabPanelProps } from "@paxeer-network/ui";
import { DesktopOrderListWidget, TabType } from "@paxeer-network/ui-orders";
import {
  LiquidationWidget,
  PositionHistoryWidget,
  PositionsWidget,
} from "@paxeer-network/ui-positions";
import { InjectableDataListDesktopTabs } from "./dataList.injectable";
import { DataListState, DataListTabType } from "./dataList.script";

const LazySettingWidget = React.lazy(() =>
  import("./setting").then((mod) => {
    return { default: mod.SettingWidget };
  }),
);

const LazyPositionHeaderWidget = React.lazy(() =>
  import("../../base/positionHeader").then((mod) => {
    return { default: mod.PositionHeaderWidget };
  }),
);

const PositionsView: React.FC<DataListState> = (props) => {
  return (
    <Flex direction="column" width="100%" height="100%">
      <React.Suspense fallback={null}>
        <LazyPositionHeaderWidget
          setPnlNotionalDecimalPrecision={props.setPnlNotionalDecimalPrecision}
          pnlNotionalDecimalPrecision={props.pnlNotionalDecimalPrecision}
          symbol={!!props.showAllSymbol ? undefined : props.symbol}
          setUnPnlPriceBasic={props.setUnPnlPriceBasic}
          unPnlPriceBasis={props.unPnlPriceBasis}
        />
      </React.Suspense>
      <Divider className="oui-w-full" />
      <Box className="oui-h-[calc(100%_-_60px)]" width="100%">
        <PositionsWidget
          symbol={!!props.showAllSymbol ? undefined : props.symbol}
          pnlNotionalDecimalPrecision={props.pnlNotionalDecimalPrecision}
          sharePnLConfig={props.sharePnLConfig}
          calcMode={props.calcMode}
          includedPendingOrder={props.includedPendingOrder}
          onSymbolChange={props.onSymbolChange}
        />
      </Box>
    </Flex>
  );
};

export const LiquidationTab: React.FC = () => {
  const { t } = useTranslation();
  return (
    <div className="oui-flex oui-gap-x-1">
      <span>{t("positions.liquidation")}</span>
      <Tooltip
        className="oui-max-w-[275px] oui-bg-base-6"
        content={
          <div>
            <div className="oui-text-pretty">
              {t("positions.Liquidation.tooltip.liquidation")}
            </div>
            <div>
              <a
                href="https://paxeer-network.network/docs/introduction/trade-on-paxeer-network/perpetual-futures/liquidations"
                target="_blank"
                rel="noopener noreferrer"
                className="oui-text-primary"
              >
                {t("positions.Liquidation.tooltip.viewMore")}
              </a>
            </div>
          </div>
        }
        arrow={{ className: "oui-fill-base-6" }}
      >
        <span className="oui-hidden oui-cursor-pointer group-data-[state=active]:oui-block">
          <InfoCircleIcon />
        </span>
      </Tooltip>
    </div>
  );
};

export const DataList: React.FC<DataListState> = (props) => {
  const { t } = useTranslation();

  const {
    positionCount = 0,
    pendingOrderCount = 0,
    tpSlOrderCount = 0,
    showAllSymbol,
    symbol,
    onSymbolChange,
    pnlNotionalDecimalPrecision,
    sharePnLConfig,
    setShowAllSymbol,
    current,
    unPnlPriceBasis,
    setUnPnlPriceBasic,
    setPnlNotionalDecimalPrecision,
  } = props;

  const tabPanelItems: (TabPanelProps & { content?: React.ReactNode })[] = [
    {
      value: DataListTabType.positions,
      title: `${t("common.positions")} ${positionCount > 0 ? `(${positionCount})` : ""}`,
      content: <PositionsView {...props} />,
    },
    {
      value: DataListTabType.pending,
      title: `${t("orders.status.pending")} ${pendingOrderCount > 0 ? `(${pendingOrderCount})` : ""}`,
      content: (
        <DesktopOrderListWidget
          type={TabType.pending}
          ordersStatus={OrderStatus.INCOMPLETE}
          symbol={showAllSymbol ? undefined : symbol}
          onSymbolChange={onSymbolChange}
          testIds={{ tableBody: "oui-testid-dataList-pending-table-body" }}
        />
      ),
    },
    {
      value: DataListTabType.tp_sl,
      title: `${t("common.tpsl")} ${tpSlOrderCount > 0 ? `(${tpSlOrderCount})` : ""}`,
      content: (
        <DesktopOrderListWidget
          type={TabType.tp_sl}
          ordersStatus={OrderStatus.INCOMPLETE}
          symbol={showAllSymbol ? undefined : symbol}
          onSymbolChange={onSymbolChange}
          testIds={{ tableBody: "oui-testid-dataList-tpsl-table-body" }}
        />
      ),
    },
    {
      value: DataListTabType.filled,
      title: t("orders.status.filled"),
      content: (
        <DesktopOrderListWidget
          type={TabType.filled}
          symbol={showAllSymbol ? undefined : symbol}
          pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
          ordersStatus={OrderStatus.FILLED}
          onSymbolChange={onSymbolChange}
          testIds={{ tableBody: "oui-testid-dataList-filled-table-body" }}
          sharePnLConfig={sharePnLConfig}
        />
      ),
    },
    {
      value: DataListTabType.positionHistory,
      title: t("positions.positionHistory"),
      content: (
        <PositionHistoryWidget
          pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
          symbol={showAllSymbol ? undefined : symbol}
          onSymbolChange={onSymbolChange}
          sharePnLConfig={sharePnLConfig}
        />
      ),
    },
    {
      value: DataListTabType.orderHistory,
      title: t("orders.orderHistory"),
      content: (
        <DesktopOrderListWidget
          type={TabType.orderHistory}
          pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
          symbol={showAllSymbol ? undefined : symbol}
          onSymbolChange={onSymbolChange}
          testIds={{ tableBody: "oui-testid-dataList-orderHistory-table-body" }}
          sharePnLConfig={sharePnLConfig}
        />
      ),
    },
    {
      value: DataListTabType.liquidation,
      title: <LiquidationTab />,
      content: (
        <LiquidationWidget symbol={showAllSymbol ? undefined : symbol} />
      ),
    },
    {
      value: DataListTabType.assets,
      title: t("common.assets"),
      content: (
        <Flex
          width="100%"
          height="100%"
          className="oui-overflow-y-auto oui-hide-scrollbar oui-pt-3"
        >
          <AssetsModule.AssetsDataTableWidget
            classNames={{
              scrollRoot:
                "oui-h-full oui-mb-6 oui-mt-0 oui-p-0 oui-pt-3 oui-h-[calc(100%_-_40px)]",
              root: "oui-gap-4 oui-h-full",
              desc: "oui-ms-1",
            }}
            dataTableClassNames={{
              header: "oui-bg-base-9",
              root: "oui-h-[calc(100%_-_28px)]",
            }}
          />
        </Flex>
      ),
    },
  ];

  return (
    <Tabs
      className="oui-trading-dataList-tabs oui-h-full"
      defaultValue={current || DataListTabType.positions}
      variant="contained"
      trailing={
        <React.Suspense fallback={null}>
          <LazySettingWidget
            pnlNotionalDecimalPrecision={pnlNotionalDecimalPrecision}
            setPnlNotionalDecimalPrecision={setPnlNotionalDecimalPrecision}
            unPnlPriceBasis={unPnlPriceBasis}
            setUnPnlPriceBasic={setUnPnlPriceBasic}
            hideOtherSymbols={!showAllSymbol}
            setHideOtherSymbols={(value) => setShowAllSymbol(!value)}
          />
        </React.Suspense>
      }
      size="lg"
      classNames={{
        trigger: "oui-group",
        tabsContent: "oui-h-[calc(100%_-_32px)]",
      }}
    >
      {tabPanelItems.map((item) => {
        const { content, ...rest } = item;
        return (
          <TabPanel {...rest} key={`item-${rest.value}`}>
            {content}
          </TabPanel>
        );
      })}
      {/* Plugin-appendable tabs. Empty by default; a plugin intercepts
          `Trading.DataList.Desktop.Tabs` and appends `{ id, title, content }`
          entries, which self-register into the Tabs context above. */}
      <InjectableDataListDesktopTabs items={[]} />
    </Tabs>
  );
};
