import { useCallback } from "react";
import { useEventEmitter, useOrderStream } from "@paxeer-network/hooks";
import { OrderStatus } from "@paxeer-network/types";
import { Decimal } from "@paxeer-network/utils";
import { BracketAlgoType, TpslAlgoType } from "../renderer/tpsl.util";

export default function useEditOrder(onToast: any) {
  const ee = useEventEmitter();
  const [, { updateOrder, cancelAlgoOrder, updateAlgoOrder, updateTPSLOrder }] =
    useOrderStream({
      status: OrderStatus.INCOMPLETE,
    });
  return useCallback(
    (order: any, lineValue: any) => {
      if (order.algo_order_id) {
        if (TpslAlgoType.includes(order.root_algo_order_algo_type)) {
          const algoParams = [
            {
              order_id: order.algo_order_id,
              trigger_price: new Decimal(lineValue.value).toString(),
            },
          ];

          // @ts-ignore
          return updateTPSLOrder(
            order.root_algo_order_id,
            order.parent_algo_order_id &&
              order.parent_algo_order_id !== order.root_algo_order_id
              ? [
                  {
                    order_id: order.parent_algo_order_id,
                    child_orders: algoParams,
                  },
                ]
              : algoParams,
          )
            .then((res) => {})
            .catch((e) => {
              if (onToast) {
                onToast.error(e.message);
              }
            });
        } else if (BracketAlgoType.includes(order.algo_type)) {
          // @ts-ignore
          return updateAlgoOrder(order.algo_order_id, {
            order_price: new Decimal(lineValue.value).toString(),
          })
            .then((res) => {})
            .catch((e) => {
              if (onToast) {
                onToast.error(e.message);
              }
            });
        } else {
          const values: any = {
            quantity: order.quantity,
            trigger_price: order.trigger_price,
            symbol: order.symbol,
            price: order.price,
            // order_type: order.type,
            // side: order.side,
            // reduce_only: Boolean(order.reduce_only),
            algo_order_id: order.algo_order_id,
          };
          if (order.order_tag) {
            values.order_tag = order.order_tag;
          }
          if (order.client_order_id) {
            values.client_order_id = order.client_order_id;
          }
          if (lineValue.type === "price") {
            values.price = new Decimal(lineValue.value).toString();
          }
          if (lineValue.type === "trigger_price") {
            values.trigger_price = new Decimal(lineValue.value).toString();
          }

          // @ts-ignore
          return updateAlgoOrder(order.algo_order_id, values)
            .then((res) => {})
            .catch((e) => {
              if (onToast) {
                onToast.error(e.message);
              }
            });
        }
      }
      const values: any = {
        order_price: order.price?.toString(),
        order_quantity: order.quantity.toString(),
        symbol: order.symbol,
        order_type: order.type,
        side: order.side,
        visible_quantity: 0,
        reduce_only: order.reduce_only,
        margin_mode: order.margin_mode,
      };
      if (
        new Decimal(order.visible_quantity ?? order.visible ?? 0).eq(
          order.quantity,
        )
      ) {
        delete values.visible_quantity;
      }
      if (!Object.keys(order).includes("reduce_only")) {
        delete values.reduce_only;
      }
      if (order.order_tag) {
        values.order_tag = order.order_tag;
      }
      if (order.client_order_id) {
        values.client_order_id = order.client_order_id;
      }
      if (lineValue.type === "price") {
        values.order_price = new Decimal(lineValue.value).toString();
      }
      return updateOrder(order.order_id, values)
        .then((res) => {})
        .catch((e) => {
          onToast.error(e.message);
        });
    },
    [updateOrder, updateAlgoOrder, updateTPSLOrder, onToast],
  );
}
