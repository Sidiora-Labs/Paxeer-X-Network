import React, { forwardRef } from "react";
import { OrderSide, OrderStatus, API } from "@paxeer-network/types";
import { SharePnLConfig } from "@paxeer-network/ui-share";
import { TabType } from "../orders.widget";
import { OrderListInstance, useOrderListScript } from "./orderList.script";
import { DesktopOrderList, MobileOrderList } from "./orderList.ui";

export type DesktopOrderListWidgetProps = {
  type: TabType;
  ordersStatus?: OrderStatus;
  /** if has value, will be fetch current symbol orders*/
  symbol?: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
  pnlNotionalDecimalPrecision?: number;
  sharePnLConfig?: SharePnLConfig;
  testIds?: {
    tableBody?: string;
  };
};

export const DesktopOrderListWidget = forwardRef<
  OrderListInstance,
  DesktopOrderListWidgetProps
>((props, ref) => {
  const { testIds, ...rest } = props;
  const state = useOrderListScript({ ...rest, ref });
  return <DesktopOrderList {...state} testIds={testIds} />;
});

export const MobileOrderListWidget: React.FC<{
  type: TabType;
  ordersStatus?: OrderStatus;
  /** if has value, will be fetch current symbol orders*/
  symbol?: string;
  onSymbolChange?: (symbol: API.Symbol) => void;
  sharePnLConfig?: SharePnLConfig;
  classNames?: { root?: string; cell?: string; content?: string };
  showFilter?: boolean;
  filterConfig?: {
    side?: OrderSide | "all";
    range?: { from?: Date; to?: Date };
  };
}> = (props) => {
  const state = useOrderListScript({ ...props, enableLoadMore: true });
  return (
    <MobileOrderList
      {...state}
      classNames={props.classNames}
      showFilter={props.showFilter}
    />
  );
};
