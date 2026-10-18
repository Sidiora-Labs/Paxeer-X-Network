import { FC, PropsWithChildren, useMemo } from "react";
import { getMinNotional, useSymbolsInfo } from "@paxeer-network/hooks";
import { useMemoizedFn } from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { API, OrderEntity } from "@paxeer-network/types";
import { OrderListContext, OrderListContextState } from "./orderListContext";

export interface OrderListProviderProps {
  cancelOrder: (orderId: number, symbol: string) => Promise<any>;
  editOrder: (orderId: string, order: OrderEntity) => Promise<any>;
  cancelAlgoOrder: (orderId: number, symbol: string) => Promise<any>;
  editAlgoOrder: (orderId: string, order: OrderEntity) => Promise<any>;
}

export const OrderListProvider: FC<
  PropsWithChildren<OrderListProviderProps>
> = (props) => {
  const { cancelOrder, editOrder, cancelAlgoOrder, editAlgoOrder } = props;
  const { t } = useTranslation();
  const symbolsInfo = useSymbolsInfo();

  const onCancelOrder = useMemoizedFn(
    async (order: API.Order | API.AlgoOrder) => {
      if (order.algo_order_id !== undefined) {
        if (
          "root_algo_order_id" in order &&
          order.root_algo_order_id !== order.algo_order_id
        ) {
          return cancelAlgoOrder(order.root_algo_order_id, order.symbol);
        }

        return cancelAlgoOrder(order.algo_order_id, order.symbol).then(
          () => {},
        );
      }
      // @ts-ignore
      return cancelOrder(order.order_id, order.symbol).then(() => {
        // toast.success("Order canceled successfully");
      });
    },
  );

  const checkMinNotional = useMemoizedFn(
    (symbol: string, price?: string | number, qty?: string | number) => {
      const { min_notional } = symbolsInfo[symbol]();
      const minNotional = getMinNotional({ price, qty, min_notional });
      if (minNotional !== undefined) {
        return t("orderEntry.total.error.min", { value: minNotional });
      }
    },
  );

  const memoizedValue = useMemo<OrderListContextState>(() => {
    return {
      onCancelOrder,
      editOrder,
      editAlgoOrder,
      checkMinNotional,
    };
  }, [onCancelOrder, editOrder, editAlgoOrder, checkMinNotional]);

  return (
    <OrderListContext.Provider value={memoizedValue}>
      {props.children}
    </OrderListContext.Provider>
  );
};
