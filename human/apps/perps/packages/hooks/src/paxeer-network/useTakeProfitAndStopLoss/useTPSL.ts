import { useEffect, useState } from "react";
import { produce } from "immer";
import { omit } from "ramda";
import {
  API,
  AlgoOrderEntity,
  MarginMode,
  OrderSide,
  OrderType,
  PaxeerNetworkOrder,
  PositionType,
  SDKError,
} from "@paxeer-network/types";
import { AlgoOrderRootType } from "@paxeer-network/types";
import { AlgoOrderType } from "@paxeer-network/types";
import {
  getTPSLLeg,
  getTPSLQuantity,
  isActiveTPSLLeg,
  isTPSLTriggered,
} from "@paxeer-network/utils";
import { appendOrderMetadata } from "../../next/useOrderEntry/helper";
import { usePaxeerNetworkContext } from "../../paxeerNetworkContext";
import { OrderFactory } from "../../services/orderCreator/factory";
import { OrderValidationItem } from "../../services/orderCreator/interface";
import { TPSLPositionOrderCreator } from "../../services/orderCreator/tpslPositionOrderCreator";
import { useSubAccountMutation } from "../../subAccount";
import { useMutation } from "../../useMutation";
import { useMarkPrice } from "../useMarkPrice";
import { useSymbolsInfo } from "../useSymbolsInfo";
import { UpdateOrderKey, tpslCalculateHelper } from "./tp_slUtils";

export type TPSLComputedData = {
  /**
   * Computed take profit
   */
  tp_pnl: number;
  tp_offset: number;
  tp_offset_percentage: number;

  /**
   * Computed stop loss
   */
  sl_pnl: number;
  sl_offset: number;
  sl_offset_percentage: number;
};

export type ComputedAlgoOrder = Partial<
  AlgoOrderEntity<AlgoOrderRootType.TP_SL> & TPSLComputedData
>;

export type ValidateError = {
  [P in keyof ComputedAlgoOrder]?: OrderValidationItem;
};

const triggeredTPSLTriggerPriceError = {
  type: -1,
  message: "Trigger price cannot be changed after TP/SL is triggered",
} as const;

// const checkIsEnableTpSL = (
//   order?: API.AlgoOrder,
// ): {
//   tp_enable: boolean;
//   sl_enable: boolean;
// } => {
//   const result = {
//     tp_enable: true,
//     sl_enable: true,
//   };
//   if (!order) {
//     return result;
//   }
//   const tp = order.child_orders.find(
//     (o) => o.algo_type === AlgoOrderType.TAKE_PROFIT && o.is_activated,
//   );
//   const sl = order.child_orders.find(
//     (o) => o.algo_type === AlgoOrderType.STOP_LOSS && o.is_activated,
//   );

//   if (!tp) {
//     result.tp_enable = false;
//   }
//   if (!sl) {
//     result.sl_enable = false;
//   }
//   return result;
// };

/**
 * @hidden
 */
export const useTaskProfitAndStopLossInternal = (
  position: Partial<API.PositionTPSLExt> &
    Pick<API.PositionTPSLExt, "symbol" | "average_open_price" | "position_qty">,
  options?: {
    defaultOrder?: API.AlgoOrder;
    /**
     * If the order is editing, set to true
     * if the isEditing is true, the defaultOrder must be provided
     * Conversely, even if defaultOrder is provided and isEditing is false, a new TPSL order is still created
     */
    isEditing?: boolean;
    // tpslEnable?: {
    //   tp_enable?: boolean;
    //   sl_enable?: boolean;
    // };
    positionType?: PositionType;
  },
): [
  /**
   * return the computed & formatted order
   */
  ComputedAlgoOrder,
  {
    /**
     * Update the take profit and stop loss order, this will merge the new data with the old one
     */
    setValue: (key: string, value: number | string | boolean) => void;
    setValues: (values: Partial<ComputedAlgoOrder>) => void;
    // getOrderEntity: () => AlgoOrderEntity<AlgoOrderRootType.TP_SL|AlgoOrderRootType.POSITIONAL_TP_SL>;
    /**
     * Submit the TP/SL order
     */
    submit: (params?: { accountId?: string }) => Promise<any>;
    deleteOrder: (orderId: number, symbol: string) => Promise<any>;
    // /**
    //  * Create the take profit and stop loss order, auto-detect the order type
    //  */
    // create: () => Promise<void>;
    // update: (orderId: number) => Promise<any>;
    errors: ValidateError | null;
    /**
     *
     */
    validate: (
      otherErrors?: ValidateError,
    ) => Promise<
      AlgoOrderEntity<
        AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
      >
    >;
    metaState: {
      dirty: { [K in keyof PaxeerNetworkOrder]?: boolean };
      submitted: boolean;
      validated: boolean;
      errors: ValidateError | null;
    };
    isCreateMutating: boolean;
    isUpdateMutating: boolean;
  },
] => {
  const isEditing =
    typeof options?.isEditing !== "undefined"
      ? options!.isEditing
      : !!options?.defaultOrder;

  const [order, setOrder] = useState<
    ComputedAlgoOrder & {
      ignoreValidate?: boolean;
    }
  >({
    algo_order_id: options?.defaultOrder?.algo_order_id,
    symbol: position.symbol as string,
    side: Number(position.position_qty) > 0 ? OrderSide.BUY : OrderSide.SELL,
    quantity: isEditing
      ? options?.defaultOrder?.quantity === 0
        ? Math.abs(position.position_qty)
        : options?.defaultOrder?.quantity
      : options?.positionType === PositionType.FULL
        ? Math.abs(position.position_qty)
        : 0,
    // quantity:
    //   options?.positionType === PositionType.PARTIAL
    //     ? 0
    //     : Math.abs(position.position_qty),
    // quantity:
    //   options?.defaultOrder?.quantity || Math.abs(position.position_qty),
    algo_type: options?.defaultOrder?.algo_type as AlgoOrderRootType,
    // tp_enable: isEditing
    //   ? checkIsEnableTpSL(options?.defaultOrder).tp_enable
    //   : options?.tpslEnable?.tp_enable,
    // sl_enable: isEditing
    //   ? checkIsEnableTpSL(options?.defaultOrder).sl_enable
    //   : options?.tpslEnable?.sl_enable,
    position_type: options?.positionType,
    // Use defaultOrder.margin_mode when editing; otherwise position.margin_mode; default CROSS for backward compatibility
    margin_mode:
      options?.defaultOrder?.margin_mode ??
      position?.margin_mode ??
      MarginMode.CROSS,
  });

  const symbolInfo = useSymbolsInfo()[position.symbol!]();
  const { data: markPrice } = useMarkPrice(order.symbol!);

  const [doCreateOrder, { isMutating: isCreateMutating }] =
    useSubAccountMutation("/v1/algo/order");
  const [doUpdateOrder, { isMutating: isUpdateMutating }] =
    useSubAccountMutation("/v1/algo/order", "PUT");
  const [doDeleteOrder] = useMutation("/v1/algo/order", "DELETE");

  const [errors, setErrors] = useState<ValidateError | null>(null);

  const [meta, setMeta] = useState<{
    dirty: { [K in keyof PaxeerNetworkOrder]?: boolean };
    submitted: boolean;
    validated: boolean;
    errors: ValidateError | null;
  }>({
    dirty: {},
    submitted: false,
    validated: false,
    errors: null,
  });

  const { orderMetadata } = usePaxeerNetworkContext();

  useEffect(() => {
    if (!isEditing || !options?.defaultOrder) return;
    const order: ComputedAlgoOrder = {};
    for (const leg of ["tp", "sl"] as const) {
      const child = options.defaultOrder.child_orders?.find(
        (item) =>
          item.algo_type === (leg === "tp" ? "TAKE_PROFIT" : "STOP_LOSS"),
      );
      if (isActiveTPSLLeg(child)) {
        order[`${leg}_trigger_price`] = child!.trigger_price;
      }
      order[`${leg}_order_type`] =
        child?.type === OrderType.LIMIT ? OrderType.LIMIT : OrderType.MARKET;
      order[`${leg}_order_price`] =
        isActiveTPSLLeg(child) && child?.type === OrderType.LIMIT
          ? child.price?.toString()
          : undefined;
    }
    setValues(order);
  }, []);

  const editingOrder = isEditing ? options?.defaultOrder : undefined;
  const _setOrderValue = (
    key: string,
    value: number | string | boolean,
    options?: {
      ignoreValidate?: boolean;
    },
  ) => {
    // console.log("[updateOrder:]", key, value);

    setOrder((prev) => {
      const leg = key.startsWith("tp_")
        ? "tp"
        : key.startsWith("sl_")
          ? "sl"
          : undefined;
      const child =
        editingOrder && leg ? getTPSLLeg(editingOrder, leg) : undefined;
      const triggered = child && isTPSLTriggered(child);
      const quantity = triggered
        ? getTPSLQuantity(child)
        : Number(prev.quantity);
      const side = triggered
        ? child.side === OrderSide.BUY
          ? OrderSide.SELL
          : OrderSide.BUY
        : position.position_qty! > 0
          ? OrderSide.BUY
          : OrderSide.SELL;

      // if (key === "sl_pnl") {
      //   value = value ? `-${value}` : "";
      // }

      const newValue = tpslCalculateHelper(
        key,
        {
          key,
          value,
          entryPrice: position.average_open_price!,
          qty: side === OrderSide.BUY ? (quantity ?? 0) : -(quantity ?? 0),
          orderSide: side,
          markPrice: markPrice ?? position.average_open_price!, // use mark price as the default value
          values: prev as Partial<PaxeerNetworkOrder>,
        },
        {
          symbol: symbolInfo,
        },
      );

      if (triggered && quantity == null && leg) {
        newValue[`${leg}_pnl`] = "";
      }
      const newValueAll = {
        ...prev,
        ...newValue,
        ignoreValidate: options?.ignoreValidate,
      };
      interactiveValidate(
        newValueAll as AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
      );
      return {
        ...prev,
        ...newValue,
        ignoreValidate: options?.ignoreValidate,
      };
    });
  };

  const setOrderValue = async (
    key: string,
    value: number | string | boolean,
    options?: {
      ignoreValidate?: boolean;
    },
  ) => {
    // console.log("-------->>>>>", key, value);
    if (key === "quantity") {
      setOrder((prev) => ({ ...prev, quantity: value as string }));

      if (typeof order.sl_trigger_price !== "undefined") {
        _setOrderValue("sl_trigger_price", order.sl_trigger_price, {
          ignoreValidate: true,
        });
      }
      if (typeof order.sl_order_price !== "undefined") {
        _setOrderValue("sl_order_price", order.sl_order_price, {
          ignoreValidate: true,
        });
      }

      if (typeof order.tp_trigger_price !== "undefined") {
        _setOrderValue("tp_trigger_price", order.tp_trigger_price, {
          ignoreValidate: true,
        });
      }
      if (typeof order.tp_order_price !== "undefined") {
        _setOrderValue("tp_order_price", order.tp_order_price, {
          ignoreValidate: true,
        });
      }

      // TODO: need to optimizations code
      _setOrderValue(key, value, options);

      return;
    }

    _setOrderValue(key, value, options);
  };

  /**
   * calculate value config
   */
  const valueConfig = {
    symbol: symbolInfo,
    maxQty: Math.abs(position.position_qty),
    markPrice,
  };

  // auto validate when order changed
  // useEffect(() => {
  //   requestAnimationFrame(() => {
  //     if (order.ignoreValidate) return;
  //     if (!order.quantity) {
  //       return;
  //     }
  //     const orderCreator = getOrderCreator();
  //     orderCreator
  //       .validate(order as AlgoOrderEntity, valueConfig)
  //       .then((errors) => {
  //         setErrors(errors);
  //       });
  //   });
  // }, [order, valueConfig.markPrice, order.quantity]);

  const interactiveValidate = (
    order: AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
  ) => {
    validateFunc(order).then((errors) => {
      const keys = Object.keys(errors ?? {});
      if (keys.length > 0) {
        setMeta(
          produce((draft) => {
            draft.errors = errors;
          }),
        );
      } else {
        setMeta(
          produce((draft) => {
            draft.errors = null;
          }),
        );
      }
    });
  };

  const validateFunc = async (
    currentOrder: AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
  ) => {
    const creator = getOrderCreator();
    const lockedTriggerErrors: ValidateError = {};
    const skipTPSLTriggerPriceAgainstMark = (["tp", "sl"] as const).reduce<
      Partial<Record<"tp" | "sl", boolean>>
    >((result, leg) => {
      const child = editingOrder?.child_orders?.find(
        (item) =>
          item.algo_type === (leg === "tp" ? "TAKE_PROFIT" : "STOP_LOSS"),
      );
      const currentTriggerPrice = currentOrder[`${leg}_trigger_price`];
      const triggerPriceEmpty =
        currentTriggerPrice === undefined || currentTriggerPrice === "";
      const triggerPriceChanged =
        !!child &&
        (isActiveTPSLLeg(child)
          ? triggerPriceEmpty ||
            Number(currentTriggerPrice) !== Number(child.trigger_price)
          : !triggerPriceEmpty);
      if (
        editingOrder &&
        editingOrder.is_triggered === true &&
        child &&
        !triggerPriceChanged
      ) {
        result[leg] = true;
      }
      if (editingOrder?.is_triggered === true && triggerPriceChanged) {
        lockedTriggerErrors[`${leg}_trigger_price`] =
          triggeredTPSLTriggerPriceError;
      }
      return result;
    }, {});

    const validationConfig = {
      ...valueConfig,
      skipTPSLTriggerPriceAgainstMark:
        Object.keys(skipTPSLTriggerPriceAgainstMark).length > 0
          ? skipTPSLTriggerPriceAgainstMark
          : undefined,
    };
    const errors = await creator.validate(currentOrder, validationConfig);
    return Object.keys(lockedTriggerErrors).length
      ? { ...errors, ...lockedTriggerErrors }
      : errors;
  };

  const setValues = (values: Partial<ComputedAlgoOrder>) => {
    const keys = Object.keys(values);
    keys.forEach((key) => {
      setOrderValue(
        key as UpdateOrderKey,
        values[key as keyof ComputedAlgoOrder] as number | string,
      );
    });
  };

  const validate = (
    otherErrors?: ValidateError,
  ): Promise<
    AlgoOrderEntity<
      AlgoOrderRootType.POSITIONAL_TP_SL | AlgoOrderRootType.TP_SL
    >
  > => {
    const orderCreator = getOrderCreator();

    return new Promise((resolve, reject) => {
      return validateFunc(
        order as AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
      ).then((errors) => {
        if (otherErrors) {
          errors = {
            ...errors,
            ...otherErrors,
          };
        }
        if (errors) {
          const keys = Object.keys(errors);
          if (keys.length > 0) {
            // setErrors(errors);
            setMeta(
              produce((draft) => {
                draft.errors = errors;
              }),
            );
            if (!meta.validated) {
              // setMeta((prev) => ({ ...prev, validated: true }));
              setMeta(
                produce((draft) => {
                  draft.validated = true;
                }),
              );
            }
          }
          setErrors(errors);
          return reject(errors);
        }

        resolve(
          orderCreator.create(
            order as AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
            valueConfig,
          ),
        );
      });
    });
  };

  // useEffect(() => {
  //   // setError(validate());
  // }, [order]);

  const getOrderCreator = () => {
    if (
      isEditing &&
      options?.defaultOrder?.algo_type === AlgoOrderRootType.POSITIONAL_TP_SL
    ) {
      return OrderFactory.create(AlgoOrderRootType.POSITIONAL_TP_SL);
    }
    if (
      isEditing &&
      options?.defaultOrder?.algo_type === AlgoOrderRootType.TP_SL
    ) {
      return OrderFactory.create(AlgoOrderRootType.TP_SL);
    }
    return OrderFactory.create(
      options?.positionType === PositionType.FULL
        ? AlgoOrderRootType.POSITIONAL_TP_SL
        : AlgoOrderRootType.TP_SL,
    );
  };

  const submit = async (params?: {
    /**
     * if you are main account to create tpsl order, you need to provide the accountId
     */
    accountId?: string;
  }) => {
    const defaultOrder = options?.defaultOrder;
    const orderId = defaultOrder?.algo_order_id;

    // A default order does not turn an explicitly non-editing flow into an update.
    if (!isEditing || !orderId) {
      return createOrder(params);
    }

    return updateOrder(orderId!, params);
  };

  const createOrder = (params?: { accountId?: string }) => {
    const orderCreator = getOrderCreator();

    const orderBody = orderCreator.create(
      order as AlgoOrderEntity<AlgoOrderRootType.TP_SL>,
      valueConfig,
    );

    if (orderBody.child_orders.length === 0) {
      throw new SDKError("No child orders");
    }

    // filter the order that is not activated
    orderBody.child_orders = orderBody.child_orders.filter(
      (order: API.AlgoOrderExt) => order.is_activated,
    );

    const body = appendOrderMetadata(orderBody, orderMetadata);
    return doCreateOrder(body, {}, params);
  };

  const deleteOrder = (orderId: number, symbol: string): Promise<any> => {
    return doDeleteOrder(null, {
      order_id: orderId,
      symbol,
    });
  };

  const updateOrder = (
    orderId: number,
    params?: { accountId?: string },
  ): Promise<any> => {
    const orderCreator =
      getOrderCreator() as unknown as TPSLPositionOrderCreator;

    const [updatedOrderEntity, orderEntity] = orderCreator.crateUpdateOrder(
      // @ts-ignore
      order,
      options?.defaultOrder,
      valueConfig,
    );

    if (updatedOrderEntity.child_orders.length === 0) {
      return Promise.resolve("Not any order needs to update");
    }

    const needDelete =
      orderEntity.child_orders.filter(
        (order) =>
          typeof order.is_activated === "boolean" && !order.is_activated,
      ).length === orderEntity.child_orders.length;

    if (needDelete) {
      return deleteOrder(orderId, order.symbol!);
    }

    return doUpdateOrder(
      {
        order_id: orderId,
        ...updatedOrderEntity,
      },
      {},
      params,
    );
  };

  return [
    omit(["ignoreValidate"], order) as ComputedAlgoOrder,
    {
      submit,
      deleteOrder,
      // create: submit,

      // update: updateOrder,/
      setValue: setOrderValue,
      setValues,
      // createPositionTPSL: submit,
      // createTPSL: submit,
      validate,
      metaState: meta,
      errors,
      isCreateMutating,
      isUpdateMutating,
    },
  ];
};
