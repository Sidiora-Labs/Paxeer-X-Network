/* eslint-disable @typescript-eslint/no-explicit-any */
import React, { useMemo } from "react";
import { useTranslation } from "@paxeer-network/i18n";
import { AssetsModule } from "@paxeer-network/portfolio";
import { OrderStatus } from "@paxeer-network/types";
import {
  Button,
  Checkbox,
  cn,
  Divider,
  Flex,
  TabPanel,
  Tabs,
  Text,
} from "@paxeer-network/ui";
import type { TabPanelProps } from "@paxeer-network/ui";
import { MobileOrderListWidget, TabType } from "@paxeer-network/ui-orders";
import {
  MobileLiquidationWidget,
  MobilePositionHistoryWidget,
  MobilePositionsWidget,
} from "@paxeer-network/ui-positions";
import { formatSymbol } from "@paxeer-network/utils";
import {
  type DataListState,
  DataListTabSubType,
  DataListTabType,
} from "./dataList.script";

const LazyPositionHeaderWidget = React.lazy(() =>
  import("../../base/positionHeader").then((mod) => {
    return { default: mod.PositionHeaderWidget };
  }),
);

const SymbolControlHeader: React.FC<
  DataListState & { type: TabType; ordersStatus?: OrderStatus }
> = (props) => {
  const { pendingOrderCount, tpSlOrderCount, type, symbol } = props;
  const { t } = useTranslation();
  const cancelAllDisabled = useMemo(() => {
    if (type === TabType.pending) {
      return pendingOrderCount === 0;
    }
    if (type === TabType.tp_sl) {
      return tpSlOrderCount === 0;
    }
    return false;
  }, [pendingOrderCount, tpSlOrderCount, type]);
  const formattedSymbol = props.showAllSymbol
    ? undefined
    : symbol
      ? formatSymbol(symbol, "base")
      : symbol;
  return (
    <Flex
      px={2}
      py={2}
      width={"100%"}
      justify={"between"}
      gap={2}
      className="oui-rounded-b-xl oui-bg-base-9"
    >
      <Flex className="oui-cursor-pointer oui-gap-[2px]">
        <Checkbox
          color="white"
          checked={!props.showAllSymbol}
          onCheckedChange={(checked: boolean) => {
            props.setShowAllSymbol(!checked);
          }}
        />
        <Text
          size="2xs"
          intensity={54}
          onClick={() => {
            props.setShowAllSymbol(!props.showAllSymbol);
          }}
        >
          {t("trading.hideOtherSymbols")}
        </Text>
      </Flex>
      <Button
        variant="outlined"
        size="xs"
        color="secondary"
        onClick={() => props.onCloseAll(props.type)}
        disabled={cancelAllDisabled}
      >
        {!props.showAllSymbol
          ? t("orders.cancelAll.ofSymbol", { symbol: formattedSymbol })
          : t("orders.cancelAll")}
      </Button>
    </Flex>
  );
};

const OrdersView: React.FC<
  DataListState & { type: TabType; ordersStatus?: OrderStatus }
> = (props) => {
  return (
    <Flex direction={"column"} pb={2} width={"100%"}>
      <Divider className="oui-w-full" />
      {props.type !== TabType.orderHistory && (
        <SymbolControlHeader {...props} />
      )}
      <MobileOrderListWidget
        symbol={props.showAllSymbol ? undefined : props.symbol}
        onSymbolChange={props.onSymbolChange}
        type={props.type}
        ordersStatus={props.ordersStatus}
        classNames={{
          root: "oui-w-full oui-hide-scrollbar oui-overflow-y-hidden",
          content: "!oui-space-y-1",
          cell: "oui-py-2 oui-bg-base-9 oui-p-2 oui-rounded-xl",
        }}
        sharePnLConfig={props.sharePnLConfig}
        showFilter={props.type === TabType.orderHistory}
        filterConfig={{ range: { from: undefined, to: undefined } }}
      />
    </Flex>
  );
};

const PositionsView: React.FC<DataListState> = (props) => {
  return (
    <Flex direction={"column"} gap={2}>
      <React.Suspense fallback={null}>
        <LazyPositionHeaderWidget
          pnlNotionalDecimalPrecision={props.pnlNotionalDecimalPrecision}
          setPnlNotionalDecimalPrecision={props.setPnlNotionalDecimalPrecision}
          symbol={props.showAllSymbol ? undefined : props.symbol}
          unPnlPriceBasis={props.unPnlPriceBasis}
          setUnPnlPriceBasic={props.setUnPnlPriceBasic}
        />
      </React.Suspense>
      <React.Suspense fallback={null}>
        <MobilePositionsWidget
          symbol={props.showAllSymbol ? undefined : props.symbol}
          onSymbolChange={props.onSymbolChange}
          sharePnLConfig={props.sharePnLConfig}
          pnlNotionalDecimalPrecision={props.pnlNotionalDecimalPrecision}
        />
      </React.Suspense>
    </Flex>
  );
};

const HistoryTab: React.FC<DataListState> = (props) => {
  const { t } = useTranslation();
  return (
    <div className="oui-min-h-[300px]">
      <Tabs
        value={props.subTab}
        onValueChange={(e: any) => props.setSubTab(e)}
        size="md"
        classNames={{ tabsList: "oui-bg-base-9 oui-rounded-t-xl oui-p-2" }}
      >
        <TabPanel
          title={t("positions.positionHistory")}
          value={DataListTabSubType.positionHistory}
        >
          <MobilePositionHistoryWidget
            symbol={props.showAllSymbol ? undefined : props.symbol}
            onSymbolChange={props.onSymbolChange}
            classNames={{ cell: "oui-p-2 oui-bg-base-9 oui-rounded-xl" }}
            sharePnLConfig={props.sharePnLConfig}
          />
        </TabPanel>
        <TabPanel
          title={t("orders.orderHistory")}
          value={DataListTabSubType.orderHistory}
        >
          <OrdersView type={TabType.orderHistory} {...props} />
        </TabPanel>
      </Tabs>
    </div>
  );
};

export const DataList: React.FC<DataListState & { className?: string }> = (
  props,
) => {
  const { t } = useTranslation();
  const {
    positionCount = 0,
    pendingOrderCount = 0,
    tpSlOrderCount = 0,
    showAllSymbol,
    symbol,
    tab,
    setTab,
    className,
  } = props;

  const tabPanelItems: (TabPanelProps & { content?: React.ReactNode })[] = [
    {
      title: `${t("common.positions")} ${positionCount > 0 ? `(${positionCount})` : ""}`,
      value: DataListTabType.position,
      content: <PositionsView {...props} />,
    },
    {
      title: `${t("orders.status.pending")} ${pendingOrderCount > 0 ? `(${pendingOrderCount})` : ""}`,
      value: DataListTabType.pending,
      content: (
        <OrdersView
          type={TabType.pending}
          ordersStatus={OrderStatus.INCOMPLETE}
          {...props}
        />
      ),
    },
    {
      title: `${t("common.tpsl")} ${tpSlOrderCount > 0 ? `(${tpSlOrderCount})` : ""}`,
      value: DataListTabType.tp_sl,
      content: (
        <OrdersView
          type={TabType.tp_sl}
          ordersStatus={OrderStatus.INCOMPLETE}
          {...props}
        />
      ),
    },
    {
      title: t("trading.history"),
      value: DataListTabType.history,
      content: <HistoryTab {...props} />,
    },
    {
      title: t("positions.liquidation"),
      value: DataListTabType.liquidation,
      content: (
        <MobileLiquidationWidget
          enableLoadMore
          symbol={showAllSymbol ? undefined : symbol}
          classNames={{ cell: "oui-p-2 oui-bg-base-9 oui-rounded-xl" }}
        />
      ),
    },
    {
      title: t("common.assets"),
      value: DataListTabType.assets,
      content: <AssetsModule.AssetsWidget />,
    },
  ];

  return (
    <Tabs
      value={tab}
      defaultValue={DataListTabType.position}
      onValueChange={(e) => setTab(e as DataListTabType)}
      size="lg"
      className={cn("oui-trading-dataList-tabs", className)}
      classNames={{
        tabsList:
          "oui-bg-base-9 oui-rounded-t-xl oui-p-2 oui-overflow-x-scroll oui-hide-scrollbar",
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
    </Tabs>
  );
};
