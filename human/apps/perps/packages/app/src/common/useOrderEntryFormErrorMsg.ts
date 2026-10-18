import { useCallback } from "react";
import {
  ERROR_MSG_CODES,
  OrderValidationItem,
  OrderValidationResult,
} from "@paxeer-network/hooks";
import { useTranslation } from "@paxeer-network/i18n";
import { Decimal } from "@paxeer-network/utils";

type Keys = keyof OrderValidationResult;
type ErrorType = Partial<OrderValidationItem["type"]>;

export function useOrderEntryFormErrorMsg(
  errors: OrderValidationResult | null,
) {
  const { t } = useTranslation();

  const getMessage = (
    key: Keys,
    type: ErrorType,
    params: {
      value?: string | number;
      min?: string | number;
      max?: string | number;
    },
    isZeroMax = false,
  ) => {
    const { value, min, max } = params || {};
    const quantityMaxMsg = isZeroMax
      ? t("orderEntry.orderQuantity.error.insufficientBalance")
      : t("orderEntry.orderQuantity.error.max", { value });
    const map: Partial<Record<Keys, Partial<Record<ErrorType, string>>>> = {
      quantity: {
        required: t("orderEntry.orderQuantity.error.required"),
        min: t("orderEntry.orderQuantity.error.min", { value }),
        max: quantityMaxMsg,
      },
      order_quantity: {
        required: t("orderEntry.orderQuantity.error.required"),
        min: t("orderEntry.orderQuantity.error.min", { value }),
        max: quantityMaxMsg,
      },
      order_price: {
        required: t("orderEntry.orderPrice.error.required"),
        min: t("orderEntry.orderPrice.error.min", { value }),
        max: t("orderEntry.orderPrice.error.max", { value }),
      },
      trigger_price: {
        required: t("orderEntry.triggerPrice.error.required"),
        min: t("orderEntry.triggerPrice.error.min", { value }),
        max: t("orderEntry.triggerPrice.error.max", { value }),
      },
      tp_trigger_price: {
        required: t("tpsl.validate.tpTriggerPrice.error.required"),
        min: t("orderEntry.tpTriggerPrice.error.min", { value }),
        max: t("orderEntry.tpTriggerPrice.error.max", { value }),
        priceErrorMin: t("tpsl.validate.tpTriggerPrice.error.priceErrorMin"),
        priceErrorMax: t("tpsl.validate.tpTriggerPrice.error.priceErrorMax"),
      },
      sl_trigger_price: {
        required: t("tpsl.validate.slTriggerPrice.error.required"),
        min: t("orderEntry.slTriggerPrice.error.min", { value }),
        max: t("orderEntry.slTriggerPrice.error.max", { value }),
        priceErrorMin: t("tpsl.validate.slTriggerPrice.error.priceErrorMin"),
        priceErrorMax: t("tpsl.validate.slTriggerPrice.error.priceErrorMax"),
        [ERROR_MSG_CODES.SL_PRICE_WARNING]: t(
          "tpsl.validate.slTriggerPrice.warning.closeToLiqPrice",
        ),
        [ERROR_MSG_CODES.SL_PRICE_ERROR]: t(
          "tpsl.validate.slTriggerPrice.error.crossLiqPrice",
        ),
      },
      tp_order_price: {
        required: t("tpsl.validate.tpOrderPrice.error.required"),
        min: t("tpsl.validate.tpOrderPrice.error.min", { value }),
        max: t("tpsl.validate.tpOrderPrice.error.max", { value }),
      },
      sl_order_price: {
        required: t("tpsl.validate.slOrderPrice.error.required"),
        min: t("tpsl.validate.slOrderPrice.error.min", { value }),
        max: t("tpsl.validate.slOrderPrice.error.max", { value }),
      },
      total: {
        min: t("orderEntry.total.error.min", { value }),
      },
      // not show form input tooltip
      // slippage: {
      //   max: t("orderEntry.slippage.error.max"),
      // },
      start_price: {
        required: t("orderEntry.startPrice.error.required"),
        min: t("orderEntry.startPrice.error.min", { value }),
        max: t("orderEntry.startPrice.error.max", { value }),
      },
      end_price: {
        required: t("orderEntry.endPrice.error.required"),
        min: t("orderEntry.endPrice.error.min", { value }),
        max: t("orderEntry.endPrice.error.max", { value }),
      },
      total_orders: {
        required: t("orderEntry.totalOrders.error.required"),
        range: t("orderEntry.totalOrders.error.range"),
      },
      skew: {
        required: t("orderEntry.skew.error.required"),
        min: t("orderEntry.skew.error.min", { value }),
        max: t("orderEntry.skew.error.max", { value }),
      },
      activated_price: {
        min: t("orderEntry.triggerPrice.error.min", { value }),
        max: t("orderEntry.triggerPrice.error.max", { value }),
      },
      callback_value: {
        required: t("orderEntry.callbackValue.error.required"),
        min: t("orderEntry.callbackValue.error.min", { value }),
        range: t("orderEntry.callbackValue.error.range", {
          min,
          max,
        }),
      },
      callback_rate: {
        required: t("orderEntry.callbackRate.error.required"),
        range: t("orderEntry.callbackRate.error.range", {
          min,
          max,
        }),
      },
    };

    return map[key]?.[type] || "";
  };

  const getErrorMsg = useCallback(
    (key: Keys, customValue?: string) => {
      const { type, value, min, max } = errors?.[key] || ({} as any);
      if (type) {
        // customValue is a display string that may carry a unit suffix
        // (e.g. "3.5 ETH"); only the raw validation value is safe to parse.
        // Fall back to false if value is not a valid decimal.
        let isZeroMax = false;
        try {
          isZeroMax =
            value !== undefined && value !== "" && new Decimal(value).eq(0);
        } catch {
          isZeroMax = false;
        }
        return getMessage(
          key,
          type,
          { value: customValue || value, min, max },
          isZeroMax,
        );
      }
      return "";
    },
    [errors],
  );

  return {
    getErrorMsg,
  };
}
