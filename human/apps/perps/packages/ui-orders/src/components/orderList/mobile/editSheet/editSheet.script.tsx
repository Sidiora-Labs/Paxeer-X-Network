import { useCallback, useMemo, useState } from "react";
import { useLocalStorage } from "@paxeer-network/hooks";
import { API, OrderEntity, OrderType } from "@paxeer-network/types";
import { AlgoOrderRootType } from "@paxeer-network/types";
import { toast } from "@paxeer-network/ui";
import { convertApiOrderTypeToOrderEntryType } from "../../../../utils/util";
import { OrderCellState } from "../orderCell.script";
import { useEditOrderEntry } from "./hooks/useEditOrderEntry";
import { useEditOrderMaxQty } from "./hooks/useEditOrderMaxQty";

export const useEditSheetScript = (props: {
  state: OrderCellState;
  position?: API.PositionTPSLExt;
  editAlgoOrder: (id: string, order: OrderEntity) => Promise<any>;
  editOrder: (id: string, order: OrderEntity) => Promise<any>;
  onClose: () => void;
}) => {
  const { state, editAlgoOrder, editOrder, position, onClose } = props;
  const { item: order } = state;
  const [dialogOpen, setDialogOpen] = useState(false);
  const [submitting, setSubmitting] = useState(false);

  const isAlgoOrder =
    order.algo_order_id !== undefined &&
    order.algo_type !== AlgoOrderRootType.BRACKET;

  const isStopMarket = order?.type === "MARKET" && isAlgoOrder;
  const isTrailingStop = order.algo_type === OrderType.TRAILING_STOP;

  const showTriggerPrice = isAlgoOrder && !isTrailingStop;

  const orderType = useMemo(
    () => convertApiOrderTypeToOrderEntryType(order),
    [order],
  );

  const [orderConfirm, setOrderConfirm] = useLocalStorage(
    "paxeerNetwork_order_confirm",
    true,
  );

  const maxQty = useEditOrderMaxQty(order, position?.position_qty);

  const {
    formattedOrder,
    setOrderValue,
    markPrice,
    errors,
    validate,
    isChanged,
  } = useEditOrderEntry({
    order,
    orderType,
    maxQty,
  });

  const onCloseDialog = useCallback(() => {
    setDialogOpen(false);
  }, []);

  const onSubmit = useCallback(
    async (values: OrderEntity) => {
      let future;
      const isHidden =
        order.visible_quantity !== undefined
          ? order.visible_quantity === 0
          : (order as any).visible !== undefined
            ? (order as any).visible === 0
            : false;

      if (order.order_tag) {
        values.order_tag = order.order_tag;
      }

      if (order.client_order_id) {
        values.client_order_id = order.client_order_id;
      }

      if (order.algo_order_id !== undefined) {
        if (isTrailingStop) {
          if (values.callback_rate) {
            values = {
              ...values,
              callback_rate: (Number(values.callback_rate) / 100).toString(),
            };
          } else if (isStopMarket && "order_price" in values) {
            const { order_price, ...rest } = values;
            values = rest;
          }
        }

        future = editAlgoOrder(order.algo_order_id.toString(), {
          ...values,
        });
      } else {
        future = editOrder((order as any).order_id.toString(), {
          ...values,
          ...(isHidden ? { visible_quantity: 0 } : {}),
        });
      }
      try {
        setSubmitting(true);

        await future;
        onClose();
      } catch (err: any) {
        toast.error(err?.message ?? `${err}`);
      } finally {
        setSubmitting(false);
      }
    },
    [editAlgoOrder, editOrder, isStopMarket, isTrailingStop, onClose, order],
  );

  const onDialogConfirm = () => {
    if (formattedOrder) {
      return onSubmit(formattedOrder as any);
    }
    return Promise.reject();
  };

  const onSheetConfirm = () => {
    validate()
      .then(
        (result) => {
          if (orderConfirm) {
            setDialogOpen(true);
          } else {
            // @ts-ignore
            onSubmit(formattedOrder);
          }
        },
        (error) => {
          // rejected
          if (error?.total?.message) {
            toast.error(error?.total.message);
          }
        },
      )
      .catch((err) => {});
  };

  return {
    ...state,
    markPrice,
    isStopMarket,
    isTrailingStop,
    showTriggerPrice,
    formattedOrder,
    setOrderValue,
    maxQty,
    onClose,
    onSheetConfirm,
    errors,
    isChanged,
    dialogOpen,
    setDialogOpen,
    onDialogConfirm,
    onCloseDialog,
    submitting,

    orderConfirm,
    setOrderConfirm,
  };
};

export type EditSheetState = ReturnType<typeof useEditSheetScript>;
