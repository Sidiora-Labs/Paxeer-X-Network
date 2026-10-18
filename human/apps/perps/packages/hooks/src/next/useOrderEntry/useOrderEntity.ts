import { useCallback, useState } from "react";
import { OrderSide, OrderType, SDKError } from "@paxeer-network/types";
import { isPositionalTPSL } from "@paxeer-network/utils";
import { useMarkPriceBySymbol } from "../../paxeer-network/useMarkPrice/useMarkPriceStore";
import { useSymbolsInfo } from "../../paxeer-network/useSymbolsInfo";
import { OrderValidationResult } from "../../services/orderCreator/interface";
import { validateTPSLChild } from "../../services/orderCreator/validateTPSLChild";
import { useMemoizedFn } from "../../shared/useMemoizedFn";
import { getOrderCreator } from "./helper";

export const useOrderEntity = (
  order: {
    symbol: string;
    order_type: OrderType;
    side: OrderSide;
    reduce_only?: boolean;
    [key: string]: any;
  },
  options?: {
    maxQty?: number;
  },
) => {
  const { symbol } = order;
  const { maxQty } = options || {};

  if (!symbol) {
    throw new SDKError("Symbol is required");
  }

  const [errors, setErrors] = useState<OrderValidationResult>({});

  const markPrice = useMarkPriceBySymbol(symbol);
  const symbolsInfo = useSymbolsInfo();

  const prepareData = useCallback(() => {
    return {
      markPrice,
      maxQty: maxQty || 0,
      symbol: symbolsInfo[symbol](),
    };
  }, [maxQty, markPrice, symbolsInfo, symbol]);

  const validate = useMemoizedFn(async () => {
    return new Promise<OrderValidationResult | null>(
      async (resolve, reject) => {
        if (isPositionalTPSL(order)) {
          const errors = validateTPSLChild(
            { ...order, type: order.tpsl_execution_type ?? order.order_type },
            { trigger_price: order.trigger_price, price: order.order_price },
            prepareData(),
          );
          setErrors(errors);
          if (Object.keys(errors).length) reject(errors);
          else resolve(order as any);
          return;
        }
        const creator = getOrderCreator(order);
        const errors = await creator?.validate(order, prepareData());

        const keys = Object.keys(errors);
        if (keys.length > 0) {
          setErrors(errors);
          reject(errors);
        } else {
          setErrors({});
        }
        // create order
        const orderEntity = creator.create(order, prepareData());
        resolve(orderEntity);
      },
    );
  });

  const clearErrors = useCallback(() => {
    setErrors({});
  }, []);

  return {
    errors,
    markPrice,
    symbolsInfo,
    validate,
    clearErrors,
  };
};
