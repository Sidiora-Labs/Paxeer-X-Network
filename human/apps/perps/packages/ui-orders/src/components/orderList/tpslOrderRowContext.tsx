import {
  createContext,
  FC,
  PropsWithChildren,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  unstable_serialize,
  useAccount,
  usePositionStream,
  useMemoizedFn,
  useMutation,
  useSWRConfig,
  utils,
} from "@paxeer-network/hooks";
import {
  findTPSLOrderPriceFromOrder,
  findTPSLFromOrder,
} from "@paxeer-network/hooks";
import { API, MarginMode, OrderType } from "@paxeer-network/types";
import { OrderSide } from "@paxeer-network/types";
import { AlgoOrderType } from "@paxeer-network/types";
import {
  getTPSLLeg,
  getTPSLEstimatePrice,
  getTPSLQuantity,
  matchesTPSLPosition,
} from "@paxeer-network/utils";
import { useSymbolContext } from "../provider/symbolContext";

export type TPSLOrderRowContextState = {
  order: API.AlgoOrderExt;
  tp_trigger_price?: number;
  sl_trigger_price?: number;
  sl_order_price?: number | OrderType;
  tp_order_price?: number | OrderType;
  tpPnL?: number;
  slPnL?: number;

  onCancelOrder: (order: API.AlgoOrderExt) => Promise<void>;
  onUpdateOrder: (order: API.AlgoOrderExt, params: any) => Promise<void>;

  getRelatedPosition: (
    symbol: string,
    marginMode?: MarginMode,
  ) => API.PositionTPSLExt | undefined;

  position?: API.PositionTPSLExt;
};

export const TPSLOrderRowContext = createContext<TPSLOrderRowContextState>(
  {} as TPSLOrderRowContextState,
);

export const useTPSLOrderRowContext = () => {
  return useContext(TPSLOrderRowContext);
};

export const TPSLOrderRowProvider: FC<
  PropsWithChildren<{ order: API.AlgoOrderExt }>
> = (props) => {
  const { order, children } = props;
  const { quote_dp } = useSymbolContext();
  const [{ rows: livePositions }] = usePositionStream(order.symbol);
  const position = livePositions?.find((item) =>
    matchesTPSLPosition(order, item),
  );

  const [doDeleteOrder] = useMutation("/v1/algo/order", "DELETE");
  const [doUpdateOrder] = useMutation("/v1/algo/order", "PUT");

  const config = useSWRConfig();
  const { state } = useAccount();

  const positionKey = useMemo(() => {
    return unstable_serialize(() => ["/v1/positions", state.accountId]);
  }, [state.accountId]);

  const onCancelOrder = useMemoizedFn(async (order: API.AlgoOrderExt) => {
    return doDeleteOrder(null, {
      order_id: order.algo_order_id,
      symbol: order.symbol,
    });
  });

  const onUpdateOrder = useMemoizedFn(
    async (order: API.AlgoOrderExt, params: any) => {
      return doUpdateOrder({
        order_id: order.algo_order_id,
        child_orders: order.child_orders.map((order) => ({
          order_id: order.algo_order_id,
          quantity: params.order_quantity,
        })),
      });
    },
  );

  const getRelatedPosition = useMemoizedFn(
    (symbol: string, marginMode?: MarginMode): API.PositionTPSLExt => {
      const positions = config.cache.get(positionKey);
      return positions?.data?.rows?.find(
        (p: API.PositionTPSLExt) =>
          p.symbol === symbol &&
          (p.margin_mode ?? MarginMode.CROSS) ===
            (marginMode ?? MarginMode.CROSS),
      );
    },
  );

  const {
    sl_trigger_price,
    tp_trigger_price,
    tpPnL,
    slPnL,
    sl_order_price,
    tp_order_price,
  } = calcTPSLPnL({
    order: order,
    position,
    quote_dp,
  });

  const memoizedValue = useMemo<TPSLOrderRowContextState>(() => {
    return {
      order: order,
      sl_trigger_price,
      tp_trigger_price,
      sl_order_price,
      tp_order_price,
      tpPnL,
      slPnL,
      position,
      onCancelOrder,
      onUpdateOrder,
      getRelatedPosition,
    };
  }, [
    order,
    sl_trigger_price,
    tp_trigger_price,
    sl_order_price,
    tp_order_price,
    tpPnL,
    slPnL,
    position,
    onCancelOrder,
    onUpdateOrder,
    getRelatedPosition,
  ]);

  return (
    <TPSLOrderRowContext.Provider value={memoizedValue}>
      {children}
    </TPSLOrderRowContext.Provider>
  );
};

function calcTPSLPnL(props: {
  order: API.AlgoOrderExt;
  position?: API.PositionTPSLExt;
  quote_dp: number;
}) {
  const { order, position, quote_dp } = props;

  const isTPSLOrder = "algo_type" in order && Array.isArray(order.child_orders);

  const { sl_trigger_price, tp_trigger_price } = isTPSLOrder
    ? findTPSLFromOrder(order)
    : {
        sl_trigger_price: undefined,
        tp_trigger_price: undefined,
      };
  const { sl_order_price, tp_order_price } = isTPSLOrder
    ? findTPSLOrderPriceFromOrder(order)
    : {
        sl_order_price: undefined,
        tp_order_price: undefined,
      };

  const tp = getTPSLLeg(order, "tp");
  const sl = getTPSLLeg(order, "sl");
  const tpQuantity = tp
    ? getTPSLQuantity(tp, position?.position_qty)
    : undefined;
  const slQuantity = sl
    ? getTPSLQuantity(sl, position?.position_qty)
    : undefined;
  const tpPrice = tp ? getTPSLEstimatePrice(tp) : undefined;
  const slPrice = sl ? getTPSLEstimatePrice(sl) : undefined;
  const avgOpenPrice = position?.average_open_price;
  const tpPnL =
    typeof tpQuantity === "number" &&
    typeof tpPrice === "number" &&
    typeof avgOpenPrice === "number"
      ? utils.priceToPnl(
          {
            qty: tpQuantity * (tp?.side === "BUY" ? -1 : 1),
            price: tpPrice,
            entryPrice: avgOpenPrice,
            orderSide: order.side as OrderSide,
            orderType: AlgoOrderType.TAKE_PROFIT,
          },
          { symbol: { quote_dp } },
        )
      : undefined;

  const slPnL =
    typeof slQuantity === "number" &&
    typeof slPrice === "number" &&
    typeof avgOpenPrice === "number"
      ? utils.priceToPnl(
          {
            qty: slQuantity * (sl?.side === "BUY" ? -1 : 1),
            price: slPrice,
            entryPrice: avgOpenPrice,
            orderSide: order.side as OrderSide,
            orderType: AlgoOrderType.STOP_LOSS,
          },
          { symbol: { quote_dp } },
        )
      : undefined;

  return {
    sl_trigger_price,
    tp_trigger_price,
    sl_order_price,
    tp_order_price,
    slPnL,
    tpPnL,
  };
}
