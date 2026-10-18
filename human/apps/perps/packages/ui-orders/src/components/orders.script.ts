import { ForwardedRef, useImperativeHandle, useRef } from "react";
import { TabType } from "./orders.widget";
import { OrderListInstance } from "./orderList/orderList.script";
import { SharePnLConfig } from "@paxeer-network/ui-share";

type UseOrdersScriptOptions = {
  current?: TabType;
  pnlNotionalDecimalPrecision?: number;
  ref: ForwardedRef<OrderListInstance>;
  sharePnLConfig?: SharePnLConfig
};

export const useOrdersScript = (props: UseOrdersScriptOptions) => {
  const { current, pnlNotionalDecimalPrecision, sharePnLConfig } = props;

  const orderListRef = useRef<OrderListInstance>(null);

  useImperativeHandle(props.ref, () => ({
    download: () => {
      orderListRef.current?.download?.();
    },
  }));

  return {
    current,
    pnlNotionalDecimalPrecision,
    orderListRef,
    sharePnLConfig,
  };
};

export type OrdersBuilderState = ReturnType<typeof useOrdersScript>;
