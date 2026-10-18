import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  API,
  MarginMode,
  PaxeerNetworkOrder,
  OrderSide,
  OrderType,
} from "@paxeer-network/types";
import { useMarkPricesStream } from "../../paxeer-network/useMarkPricesStream";
import { useSymbolsInfo } from "../../paxeer-network/useSymbolsInfo";
import { usePaxeerNetworkContext } from "../../paxeerNetworkContext";
import { useSubAccountMutation } from "../../subAccount";
import { calculate } from "../../utils/orderEntryHelper";
import { OrderValidationResult } from "../useOrderEntry";
import { appendOrderMetadata, getOrderCreator } from "../useOrderEntry/helper";

type PositionCloseOptions = {
  position: API.PositionExt;
  order: {
    type: OrderType;
    quantity: string;
    price: string;
  };
};

export const usePositionClose = (options: PositionCloseOptions) => {
  const { position, order: initialOrder } = options;
  const { type, quantity, price } = initialOrder;
  const symbol = position.symbol;

  const [errors, setErrors] = useState<OrderValidationResult | null>(null);

  const symbolsInfo = useSymbolsInfo();
  const { data: markPrices } = useMarkPricesStream();

  const markPricesRef = useRef(markPrices);

  const { orderMetadata } = usePaxeerNetworkContext();

  useEffect(() => {
    markPricesRef.current = markPrices;
  }, [markPrices]);

  const [writeNativeClose, { isMutating }] = useSubAccountMutation(
    "/v1/order",
    "POST",
    {
      accountId: position.account_id,
    },
  );

  const doCreateOrder=useCallback((body:any)=>writeNativeClose({...body,native_position_id:(position as API.PositionExt & {position_id?:string|number}).position_id,native_close_all:Number(body.order_quantity)===Math.abs(position.position_qty)}),[writeNativeClose,position]);

  const side = position.position_qty > 0 ? OrderSide.SELL : OrderSide.BUY;

  const closeOrderData = useMemo(() => {
    const data: Partial<PaxeerNetworkOrder> = {
      order_quantity: quantity,
      symbol,
      order_type: type,
      side,
      reduce_only: true,
      // Use position's margin_mode or default to CROSS for backward compatibility
      margin_mode: position.margin_mode || MarginMode.CROSS,
    };

    if (type === OrderType.LIMIT) {
      data.order_price = price;
    }

    return data;
  }, [symbol, price, type, quantity, position.margin_mode]);

  const maxQty = useMemo(() => {
    if (!position) {
      return 0;
    }

    /**
     * Reduce-only mode handling:
     * For long positions (positionQty > 0):
     * - Buy orders return 0 (cannot increase long position)
     * - Sell orders return absolute position quantity (can close long position)
     * For short positions (positionQty < 0):
     * - Buy orders return absolute position quantity (can close short position)
     * - Sell orders return 0 (cannot increase short position)
     */
    const positionQty = position.position_qty;
    if (positionQty > 0) {
      if (side === OrderSide.BUY) {
        return 0;
      } else {
        return Math.abs(positionQty);
      }
    }

    if (positionQty < 0) {
      if (side === OrderSide.BUY) {
        return Math.abs(positionQty);
      } else {
        return 0;
      }
    }

    return 0;
  }, [position, side]);

  /**
   * validate order
   */
  const validate = useCallback(
    async (data: Partial<PaxeerNetworkOrder>) => {
      const creator = getOrderCreator(data);
      const errors = await creator.validate(data, {
        symbol: symbolsInfo[symbol](),
        maxQty,
        // use ref to avoid re-render when markPrices change
        markPrice: markPricesRef.current[symbol],
      });
      return errors;
    },
    [maxQty, symbol, symbolsInfo],
  );

  useEffect(() => {
    validate(closeOrderData).then((errors) => {
      setErrors(errors);
    });
  }, [validate, closeOrderData]);

  const submit = useCallback(async () => {
    const errors = await validate(closeOrderData);

    if (Object.keys(errors).length > 0) {
      throw errors;
    }

    const body = appendOrderMetadata(closeOrderData, orderMetadata);

    return doCreateOrder(body)
      .then((res) => {
        if (res.success) {
          return res;
        }

        throw res;
      })
      .catch((err) => {
        throw err;
      });
  }, [validate, doCreateOrder, closeOrderData, orderMetadata]);

  return {
    submit,
    isMutating,
    side,
    closeOrderData,
    errors,
    calculate,
  };
};
